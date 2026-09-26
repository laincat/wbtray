//go:build windows

package install

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// Replacing wbtray while it is running.
//
// Windows refuses to overwrite a running executable but allows it to be renamed:
// the lock is on the file, not on the name. That one fact is what makes this
// whole procedure a few lines rather than a helper script — rename the running
// image out of the way, move the new one into its place, and start it. The old
// image is left on disk as wbtray.exe.old because it cannot be deleted while it
// is still the running one; the next start removes it, by which time nothing is
// holding it.

// StagedTray is a downloaded wbtray archive unpacked and ready to be put in place.
type StagedTray struct {
	// Version is what was downloaded.
	Version string
	// Exe is the new wbtray.exe, already unpacked.
	Exe string
	// Work is the directory holding it, removed once the swap has happened.
	Work string
}

// StageTray downloads and unpacks a wbtray release without installing it.
//
// Staging rather than installing is deliberate: the swap ends with this process
// being replaced, so everything that can fail — the download, the unpack, the
// check that the archive really does contain wbtray — happens and finishes
// before anything irreversible is touched.
func (c *Client) StageTray(ctx context.Context, layout Layout, rel Release, report func(host string), onProgress Progress) (StagedTray, error) {
	work, err := os.MkdirTemp(layout.Root, ".wbtray-self-*")
	if err != nil {
		return StagedTray{}, err
	}
	archive := filepath.Join(work, rel.AssetName())
	if err := c.Fetch(ctx, rel, archive, report, onProgress); err != nil {
		_ = os.RemoveAll(work)
		return StagedTray{}, err
	}
	unpacked := filepath.Join(work, "unpacked")
	if _, err := Unzip(archive, unpacked); err != nil {
		_ = os.RemoveAll(work)
		return StagedTray{}, fmt.Errorf("could not unpack %s: %w", rel.AssetName(), err)
	}
	exe := filepath.Join(unpacked, "wbtray.exe")
	if info, err := os.Stat(exe); err != nil || info.IsDir() {
		_ = os.RemoveAll(work)
		return StagedTray{}, fmt.Errorf("%s does not contain wbtray.exe", rel.AssetName())
	}
	return StagedTray{Version: rel.Version, Exe: exe, Work: work}, nil
}

// ApplyTray puts a staged wbtray in place of the running one and starts it.
//
// It returns once the new process has been started, and the caller is expected
// to exit: this process is running from an image that has just been renamed, so
// anything it does from here on is the old version pretending to be the new one.
func ApplyTray(layout Layout, staged StagedTray) error {
	target, err := os.Executable()
	if err != nil {
		return err
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return err
	}
	if sameFile(target, staged.Exe) {
		return fmt.Errorf("the staged build is the running one")
	}

	// Any leftover from an earlier update is removed now, while nothing is
	// holding it. A failure here is not fatal: the file is dead weight, not a
	// reason to refuse the update.
	previous := target + ".old"
	_ = os.Remove(previous)

	if err := os.Rename(target, previous); err != nil {
		return fmt.Errorf("could not move the running %s aside: %w", filepath.Base(target), err)
	}
	if err := moveFile(staged.Exe, target); err != nil {
		// Put the old one back rather than leaving the operator without a tray.
		if restoreErr := os.Rename(previous, target); restoreErr != nil {
			return fmt.Errorf("could not install the new version (%v) and could not restore the old one (%v); "+
				"the previous build is at %s", err, restoreErr, previous)
		}
		return fmt.Errorf("could not install the new version: %w", err)
	}

	cmd := exec.Command(target)
	cmd.Dir = layout.Root
	// Detached, and with no console: the new tray has to outlive this process,
	// and a console window appearing during an update is exactly what
	// "unobtrusive" is meant to rule out.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNewProcessGroup | detachedProcess | createNoWindow,
		HideWindow:    true,
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("the new version is in place but could not be started: %w", err)
	}
	// The staged copy has served its purpose; the running process does not need
	// it and it is several megabytes.
	_ = os.RemoveAll(staged.Work)
	return nil
}

// CleanUpPrevious removes the renamed build a previous self-update left behind.
//
// It is called at startup, which is the first moment at which the file is not
// held by a running process. A failure is ignored: the file is dead weight, and
// a tray that refused to start because it could not tidy up would be worse than
// one with a spare file beside it.
func CleanUpPrevious() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	_ = os.Remove(exe + ".old")
}

// moveFile moves a file, falling back to a copy when the move crosses a volume.
//
// The staged build and the installed one are normally on the same volume, and a
// plain rename is atomic. A staged copy can still end up elsewhere — a temporary
// directory redirected to another drive — and a copy is the correct answer there
// rather than an error.
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, data, 0o755); err != nil {
		return err
	}
	return os.Remove(src)
}

// sameFile reports whether two paths are the same file.
func sameFile(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// Process flags, repeated here so this file does not reach into the package that
// supervises the gateway for two constants.
const (
	createNewProcessGroup = 0x00000200
	detachedProcess       = 0x00000008
	createNoWindow        = 0x08000000
)
