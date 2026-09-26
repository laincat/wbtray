//go:build windows

package gateway

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Reading the gateway's own configuration.
//
// The gateway keeps its settings in config.json beside its executable, including
// the API key it expects. The tray needs that key, and asking the operator to
// copy it across by hand would be the single most likely reason for a tray that
// starts up and then reports "not reachable" forever. So it is read from the same
// file the gateway itself uses, and the tray's own setting remains an override
// for the case where the two are deliberately different.

// gatewayConfig is the part of the gateway's configuration the tray cares about.
type gatewayConfig struct {
	APIKey string `json:"api_key"`
	Listen string `json:"listen"`
}

// DiscoverKey reads the gateway's API key from its configuration file.
//
// An empty result means the file is missing, unreadable, or has no key; all
// three are ordinary, and the tray handles them by simply making unauthenticated
// requests, which is what a gateway with an empty key expects anyway.
func DiscoverKey(exe string) string {
	cfg := ReadConfig(exe)
	return cfg.APIKey
}

// ReadConfig reads the gateway's configuration from the directory its executable
// lives in.
func ReadConfig(exe string) gatewayConfig {
	dir := WorkDir(exe)
	if dir == "" {
		return gatewayConfig{}
	}
	return ReadConfigFrom(filepath.Join(dir, "config.json"))
}

// ReadConfigFrom reads one configuration file.
func ReadConfigFrom(path string) gatewayConfig {
	data, err := os.ReadFile(path)
	if err != nil {
		return gatewayConfig{}
	}
	var cfg gatewayConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return gatewayConfig{}
	}
	return cfg
}

// DiscoverListen reads the gateway's configured listen address, which is what
// lets the tray follow a gateway that was moved off the default port.
func DiscoverListen(exe string) string {
	return ReadConfig(exe).Listen
}
