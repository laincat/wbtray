package install

import (
	"os"
	"path/filepath"
	"strings"
)

// The installation layout.
//
// wbtray owns two directories: the one it runs from, which holds wbtray.exe and
// its settings, and a wb2api subdirectory holding the gateway. Keeping the
// gateway in a subdirectory is what makes an update a replacement of one
// directory rather than a merge into whatever else happens to be beside the
// tray, and it is where an operator already expects an unpacked release to go.

// GatewayDirName is the subdirectory the gateway lives in.
const GatewayDirName = "wb2api"

// Layout is where things are on this machine.
type Layout struct {
	// Root is the directory wbtray runs from.
	Root string
	// Gateway is the directory the gateway is installed into.
	Gateway string
}

// LayoutFor derives the layout from the running executable.
func LayoutFor() (Layout, error) {
	exe, err := os.Executable()
	if err != nil {
		return Layout{}, err
	}
	root := filepath.Dir(exe)
	return Layout{Root: root, Gateway: filepath.Join(root, GatewayDirName)}, nil
}

// GatewayExe is the gateway's executable inside the gateway directory.
func (l Layout) GatewayExe() string { return filepath.Join(l.Gateway, "wb2api.exe") }

// GatewayConfig is the gateway's own configuration file.
func (l Layout) GatewayConfig() string { return filepath.Join(l.Gateway, "config.json") }

// GatewayExample is the example configuration a release ships.
func (l Layout) GatewayExample() string {
	return filepath.Join(l.Gateway, "config.example.json")
}

// Present reports whether a gateway is installed in the directory.
func (l Layout) Present() bool {
	info, err := os.Stat(l.GatewayExe())
	return err == nil && !info.IsDir()
}

// EnsureGatewayDir creates the gateway directory if it is missing.
func (l Layout) EnsureGatewayDir() error {
	return os.MkdirAll(l.Gateway, 0o755)
}

// InstalledGatewayVersion is the version of the gateway on disk.
//
// It is recorded in a small file written at install time rather than asked of the
// gateway, because the gateway only knows its own version while it is running and
// answering, and "what is on disk" has to be answerable when it is not.
func (l Layout) InstalledGatewayVersion() string {
	data, err := os.ReadFile(l.versionFile())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// RecordGatewayVersion notes which version of the gateway is on disk.
func (l Layout) RecordGatewayVersion(version string) error {
	return os.WriteFile(l.versionFile(), []byte(version+"\n"), 0o644)
}

// versionFile is where the installed gateway's version is noted.
func (l Layout) versionFile() string {
	return filepath.Join(l.Gateway, "installed-version.txt")
}
