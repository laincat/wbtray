package install

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Installing and updating the gateway.
//
// The one rule that matters here: an update must never destroy what the operator
// cannot get back. The gateway keeps its account credentials in auths/, its
// runtime state in data/ and its settings in config.json, all relative to its own
// directory — so replacing that directory wholesale would delete every account
// the operator ever logged into. Updates therefore go through a staging
// directory, and the three things that belong to the installation rather than to
// the version are carried across before the old directory is removed.

// carried lists what an update keeps from the installation it replaces: paths
// relative to the gateway directory, in the order they are copied.
//
// config.json is included even though a release ships config.example.json: the
// operator's own settings are theirs, and the only safe assumption about a new
// version's defaults is that they are not the ones already working.
var carried = []string{"config.json", "auths", "data"}

// UpdateResult describes what an update did.
type UpdateResult struct {
	Version string
	// Kept lists the files and directories carried over from the previous
	// installation.
	Kept []string
	// Fresh is true when there was nothing installed before.
	Fresh bool
}

// InstallGateway downloads a gateway release and puts it in the layout's
// gateway directory, carrying over anything an existing installation owns.
//
// report is told which host a download is being taken from, and onProgress how
// far it has got.
func (c *Client) InstallGateway(ctx context.Context, layout Layout, rel Release, report func(host string), onProgress Progress) (UpdateResult, error) {
	result := UpdateResult{Version: rel.Version}
	if err := layout.EnsureGatewayDir(); err != nil {
		return result, err
	}
	result.Fresh = !layout.Present()

	work, err := os.MkdirTemp(layout.Root, ".wbtray-update-*")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(work)

	archive := filepath.Join(work, rel.AssetName())
	if err := c.Fetch(ctx, rel, archive, report, onProgress); err != nil {
		return result, err
	}
	staged := filepath.Join(work, "unpacked")
	if _, err := Unzip(archive, staged); err != nil {
		return result, fmt.Errorf("could not unpack %s: %w", rel.AssetName(), err)
	}
	if !hasGatewayExe(staged) {
		return result, fmt.Errorf("%s does not contain %s; the release layout is not what wbtray expects",
			rel.AssetName(), filepath.Base(layout.GatewayExe()))
	}

	// Carry the installation's own files into the staged copy, so the swap below
	// replaces the directory in one step and there is no moment when neither the
	// old nor the new one is complete.
	kept, err := carryOver(layout, staged)
	if err != nil {
		return result, err
	}
	result.Kept = kept

	if err := swapDirectory(layout.Gateway, staged); err != nil {
		return result, err
	}
	if err := layout.RecordGatewayVersion(rel.Version); err != nil {
		// The gateway is in place; only the note of which version it is failed,
		// so this is worth reporting but not worth calling the install failed.
		return result, fmt.Errorf("the gateway was installed but its version could not be recorded: %w", err)
	}
	return result, nil
}

// hasGatewayExe reports whether an unpacked release contains the gateway binary.
//
// The check is made rather than assumed because a release layout can change, and
// a version of wbtray that silently installs nothing is worse than one that says
// it does not recognise what it downloaded.
func hasGatewayExe(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, "wb2api.exe"))
	return err == nil && !info.IsDir()
}

// carryOver copies the installation's own files from the current directory into
// the staged one, returning what it copied.
func carryOver(layout Layout, staged string) ([]string, error) {
	var kept []string
	for _, name := range carried {
		src := filepath.Join(layout.Gateway, name)
		info, err := os.Stat(src)
		if err != nil {
			// Absent is the ordinary case on a first install, and the ordinary
			// case on a later one for anything the operator never created.
			continue
		}
		dst := filepath.Join(staged, name)
		if info.IsDir() {
			if err := copyTree(src, dst); err != nil {
				return kept, fmt.Errorf("could not keep %s: %w", name, err)
			}
		} else {
			if err := copyFile(src, dst); err != nil {
				return kept, fmt.Errorf("could not keep %s: %w", name, err)
			}
		}
		kept = append(kept, name)
	}
	return kept, nil
}

// swapDirectory replaces a directory with a staged one.
//
// The replacement is old-out-of-the-way, new-into-place, old-deleted rather than
// a delete-then-copy: if the process dies between the first two steps the
// previous installation is still there under a name that says what it is, which
// is recoverable by hand. Deleting first would leave nothing to recover.
func swapDirectory(current, staged string) error {
	previous := current + ".previous"
	_ = os.RemoveAll(previous)

	exists := false
	if _, err := os.Stat(current); err == nil {
		exists = true
		if err := os.Rename(current, previous); err != nil {
			return fmt.Errorf("could not move the current installation aside: %w", err)
		}
	}
	if err := os.Rename(staged, current); err != nil {
		// Put the old one back rather than leaving the operator with nothing.
		if exists {
			if restoreErr := os.Rename(previous, current); restoreErr != nil {
				return fmt.Errorf("could not install the new version (%v) and could not restore the old one (%v); "+
					"the previous installation is in %s", err, restoreErr, previous)
			}
		}
		return fmt.Errorf("could not install the new version: %w", err)
	}
	_ = os.RemoveAll(previous)
	return nil
}

// copyTree copies a directory recursively.
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

// copyFile copies one file, creating the directory it goes in.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// WaitForExit pauses briefly after stopping a process, because Windows releases
// a running executable's file lock a moment after the process ends and a
// replacement attempted in that window fails for a reason that looks like a
// permissions problem.
func WaitForExit() { time.Sleep(600 * time.Millisecond) }
