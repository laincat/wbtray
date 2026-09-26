package install

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLiveInstallGateway downloads the real upstream release and installs it.
//
// It is the only test that can prove the three things the updater cannot reason
// about: that the archive name built from the tag is one upstream actually
// publishes, that the archive unpacks to the layout wbtray expects, and that the
// gateway binary inside it is one that runs. Everything else about the updater is
// arithmetic.
//
// It installs into a temporary directory, so it never touches an installation in
// use.
//
//	$env:WBTRAY_LIVE = "1"
//	go test ./internal/install -run LiveInstall -v
func TestLiveInstallGateway(t *testing.T) {
	liveOnly(t)

	root := t.TempDir()
	layout := Layout{Root: root, Gateway: filepath.Join(root, GatewayDirName)}

	c := New()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	rel, err := c.Latest(ctx, Gateway())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("installing %s from %v", rel.Version, rel.URLs)

	var hosts []string
	result, err := c.InstallGateway(ctx, layout, rel, func(host string) {
		hosts = append(hosts, host)
	}, func(done, total int64) {
		if total > 0 && done == total {
			t.Logf("downloaded %d bytes", done)
		}
	})
	if err != nil {
		t.Fatalf("install failed after trying %v: %v", hosts, err)
	}
	t.Logf("installed from %v", hosts)
	t.Logf("result: version=%s fresh=%v kept=%v", result.Version, result.Fresh, result.Kept)

	// The layout the tray expects: the executable it starts, and the example
	// configuration the upstream README points at.
	if !layout.Present() {
		t.Fatalf("%s is not there after the install", layout.GatewayExe())
	}
	info, err := os.Stat(layout.GatewayExe())
	if err != nil {
		t.Fatal(err)
	}
	// The upstream binary is around 9 MB; anything of a few kilobytes is an error
	// page saved under the executable's name.
	if info.Size() < 1<<20 {
		t.Errorf("%s is only %d bytes, which is not the gateway", layout.GatewayExe(), info.Size())
	}
	if v := layout.InstalledGatewayVersion(); v != rel.Version {
		t.Errorf("recorded version is %q, want %q", v, rel.Version)
	}
	if _, err := os.Stat(layout.GatewayExample()); err != nil {
		t.Errorf("the release's example configuration is missing: %v", err)
	}
}

// TestLiveInstallKeepsCredentials is the rule an update must never break: the
// accounts an operator logged into are theirs, and replacing the directory is
// only acceptable if they survive it.
func TestLiveInstallKeepsCredentials(t *testing.T) {
	liveOnly(t)

	root := t.TempDir()
	layout := Layout{Root: root, Gateway: filepath.Join(root, GatewayDirName)}
	if err := layout.EnsureGatewayDir(); err != nil {
		t.Fatal(err)
	}

	// An installation carrying the three things that belong to it rather than to
	// the version: settings, credentials, and runtime state. The literals are
	// built rather than written out so the file has no brace-heavy JSON in it.
	settings := "{" + quote("api_key") + ":" + quote("sk-mine") + "}"
	auth := "{" + quote("uid") + ":" + quote("123") + "}"
	state := "{" + quote("accounts") + ":[]}"
	writeFile(t, layout.GatewayConfig(), settings)
	writeFile(t, filepath.Join(layout.Gateway, "auths", "workbuddy-123.json"), auth)
	writeFile(t, filepath.Join(layout.Gateway, "data", "state.json"), state)
	// And a file that belongs to neither, which must not survive: it is what a
	// stale leftover looks like.
	writeFile(t, filepath.Join(layout.Gateway, "leftover.exe"), "old")

	c := New()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	rel, err := c.Latest(ctx, Gateway())
	if err != nil {
		t.Fatal(err)
	}
	result, err := c.InstallGateway(ctx, layout, rel, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	// The three carried things are still there, unchanged.
	checkFileContains(t, layout.GatewayConfig(), "sk-mine")
	checkFileContains(t, filepath.Join(layout.Gateway, "auths", "workbuddy-123.json"), "123")
	checkFileContains(t, filepath.Join(layout.Gateway, "data", "state.json"), "accounts")
	if len(result.Kept) != 3 {
		t.Errorf("kept %v, want the settings, the credentials and the state", result.Kept)
	}

	// The gateway itself is the new one.
	if !layout.Present() {
		t.Error("the gateway is not installed after the update")
	}
	// And the leftover is gone, because the directory it was in was replaced
	// rather than merged into.
	if _, err := os.Stat(filepath.Join(layout.Gateway, "leftover.exe")); err == nil {
		t.Error("a file from the previous installation survived the update, so the directory was merged rather than replaced")
	}
}

// quote wraps a string in double quotes, for building JSON in a test without
// writing brace-heavy literals that a tool reading this file could mistake for
// something else.
func quote(s string) string { return `"` + s + `"` }

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func checkFileContains(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s did not survive: %v", path, err)
	}
	if !strings.Contains(string(data), want) {
		t.Errorf("%s no longer contains %q: %s", path, want, data)
	}
}
