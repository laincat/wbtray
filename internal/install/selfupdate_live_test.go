package install

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestLiveStageTrayDownloadsTheRealRelease is the self-update path against the
// real release, short of the swap.
//
// Staging is everything that can fail — resolving the newest tag, building the
// download address, fetching it, unpacking it, finding the executable inside it —
// and it is where a released archive that does not match what the updater expects
// would show up. Applying is deliberately not tested here: it renames and
// replaces the running executable, and a test that did that would replace the
// test binary and end the run.
//
//	$env:WBTRAY_LIVE = "1"
//	go test ./internal/install -run LiveStageTray -v
func TestLiveStageTrayDownloadsTheRealRelease(t *testing.T) {
	liveOnly(t)

	root := t.TempDir()
	layout := Layout{Root: root, Gateway: filepath.Join(root, GatewayDirName)}

	c := New()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	rel, err := c.Latest(ctx, Tray())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("staging wbtray %s from %v", rel.Version, rel.URLs)

	var hosts []string
	staged, err := c.StageTray(ctx, layout, rel, func(host string) {
		hosts = append(hosts, host)
	}, nil)
	if err != nil {
		t.Fatalf("staging failed after trying %v: %v", hosts, err)
	}
	t.Logf("staged from %v", hosts)

	if staged.Version != rel.Version {
		t.Errorf("staged version %q, want %q", staged.Version, rel.Version)
	}
	info, err := os.Stat(staged.Exe)
	if err != nil {
		t.Fatalf("the staged executable is not there: %v", err)
	}
	// The tray is several megabytes; anything of a few kilobytes is an error page
	// saved under the executable's name.
	if info.Size() < 1<<20 {
		t.Errorf("the staged executable is only %d bytes", info.Size())
	}
	// The archive unpacks to the executable alone, so the staged build has to be
	// a real PE image rather than a directory or a symlink.
	if info.IsDir() {
		t.Error("the staged executable is a directory")
	}
	head := make([]byte, 2)
	f, err := os.Open(staged.Exe)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Read(head); err != nil {
		t.Fatal(err)
	}
	if head[0] != 'M' || head[1] != 'Z' {
		t.Errorf("the staged file starts with %q, which is not a Windows executable", head)
	}
}
