//go:build windows

// Package gateway finds and supervises the wb2api process.
//
// The tray treats the gateway as something it observes first and controls
// second: a gateway started by another program, or by a scheduled task, is a
// perfectly normal arrangement, and the tray must not confuse "I did not start
// it" with "it is not there".
package gateway

import (
	"os"
	"path/filepath"
	"strings"
)

// DefaultName is the executable the gateway ships as.
const DefaultName = "wb2api.exe"

// GatewaySubdir is the directory the tray installs the gateway into, beside its
// own executable.
//
// It is duplicated from the install package rather than imported, because the
// gateway package is the one that answers "where is a gateway" and the install
// package answers "where do I put one"; importing the second from the first would
// make the search depend on the installer.
const GatewaySubdir = "wb2api"

// CandidateDirs are the places an installed gateway usually lives, in the order
// they are checked.
//
// The tray's own directory comes first, and within it the wb2api subdirectory
// before the directory itself, because that is where the tray installs a gateway
// and therefore where it is most likely to be. Leaving the subdirectory out — which
// is what this did — made "stop, then start" fail on the tray's own installation:
// the tray could stop a gateway it could see in the process list and then could not
// find it on disk, which read as a gateway that had vanished.
func CandidateDirs() []string {
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		dirs = append(dirs, filepath.Join(dir, GatewaySubdir), dir)
	}
	if cwd, err := os.Getwd(); err == nil {
		dirs = append(dirs, filepath.Join(cwd, GatewaySubdir), cwd)
	}
	dirs = append(dirs,
		filepath.Join(os.Getenv("ProgramFiles"), "wb2api-panel"),
		filepath.Join(os.Getenv("ProgramFiles"), "wb2api"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "wb2api-panel"),
		`D:\Program Files\wb2api-panel`,
	)
	return dirs
}

// Find locates the gateway executable. An explicit path wins; otherwise the
// candidates are tried in order, and an empty result means it was not found.
func Find(explicit string) string {
	if explicit != "" {
		if fileExists(explicit) {
			return explicit
		}
		return ""
	}
	for _, dir := range CandidateDirs() {
		if dir == "" {
			continue
		}
		p := filepath.Join(dir, DefaultName)
		if fileExists(p) {
			return p
		}
	}
	return ""
}

// WorkDir returns the directory the gateway should run in: beside its own
// executable, because that is where it keeps config.json, auths/ and data/.
func WorkDir(exe string) string {
	if exe == "" {
		return ""
	}
	return filepath.Dir(exe)
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// PortFromBaseURL extracts the port from a base URL such as
// http://127.0.0.1:7863, which the gateway is told to listen on when the tray
// starts it. An empty result means "let the gateway use its own configuration".
func PortFromBaseURL(base string) string {
	i := strings.LastIndex(base, ":")
	if i < 0 {
		return ""
	}
	port := base[i+1:]
	if port == "" || strings.ContainsAny(port, "/?") {
		return ""
	}
	for _, r := range port {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return port
}
