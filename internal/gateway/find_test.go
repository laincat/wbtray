//go:build windows

package gateway

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCandidateDirsIncludesTheSubdirectory is the bug that made the tray look
// broken next to its own installation.
//
// The tray installs the gateway into a wb2api subdirectory beside itself, and the
// search for an installed gateway did not look there. The symptom was not an error:
// the tray could stop a gateway it could see in the process list, and then could not
// find one to start, so "stop" followed by "start" reported a missing executable for
// a gateway sitting exactly where the tray had put it. Gateway autostart and first-run
// key discovery failed the same way.
func TestCandidateDirsIncludesTheSubdirectory(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Skipf("no executable path: %v", err)
	}
	own := filepath.Dir(self)

	var hasSub bool
	for _, dir := range CandidateDirs() {
		if dir == filepath.Join(own, GatewaySubdir) {
			hasSub = true
		}
	}
	if !hasSub {
		t.Errorf("the search does not include %s, so a gateway the tray installed is invisible",
			filepath.Join(own, GatewaySubdir))
	}
}

// TestSubdirectoryIsSearchedBeforeTheDirectoryBeside checks the order, which is what
// decides which gateway wins when both are present.
func TestSubdirectoryIsSearchedBeforeTheDirectoryBeside(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Skipf("no executable path: %v", err)
	}
	own := filepath.Dir(self)
	dirs := CandidateDirs()
	if len(dirs) < 2 {
		t.Fatalf("only %d candidates", len(dirs))
	}
	if dirs[0] != filepath.Join(own, GatewaySubdir) {
		t.Errorf("the first candidate is %s, want the tray's own wb2api subdirectory", dirs[0])
	}
	if dirs[1] != own {
		t.Errorf("the second candidate is %s, want the tray's own directory", dirs[1])
	}
}

// TestFindLocatesAGatewayInTheSubdirectory is the end-to-end version: a gateway file
// placed where the tray installs one has to be found with no explicit path.
func TestFindLocatesAGatewayInTheSubdirectory(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Skipf("no executable path: %v", err)
	}
	root := filepath.Dir(self)
	sub := filepath.Join(root, GatewaySubdir)

	// The test binary runs from a temporary directory Go owns, so creating the
	// subdirectory here cannot disturb an installation.
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Skipf("cannot create %s: %v", sub, err)
	}
	defer os.RemoveAll(sub)

	// A file with the gateway's name, not a working gateway: Find is a search, and
	// what it is being asked is whether the name is found where the tray puts it.
	exe := filepath.Join(sub, DefaultName)
	if err := os.WriteFile(exe, []byte("not a real executable"), 0o644); err != nil {
		t.Skipf("cannot write %s: %v", exe, err)
	}

	if got := Find(""); got != exe {
		t.Errorf("Find returned %q, want %q", got, exe)
	}
	// And an explicit path still wins over the search, which is what the
	// configuration option is for.
	elsewhere := filepath.Join(parentDir(root), "wb2api-elsewhere.exe")
	if err := os.WriteFile(elsewhere, []byte("x"), 0o644); err == nil {
		defer os.Remove(elsewhere)
		if got := Find(elsewhere); got != elsewhere {
			t.Errorf("an explicit path returned %q, want %q", got, elsewhere)
		}
	}
}

// parentDir is filepath.Dir spelled out where the intent is "one level up".
func parentDir(p string) string { return filepath.Dir(p) }
