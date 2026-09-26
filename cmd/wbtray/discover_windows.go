//go:build windows

package main

import (
	"strings"

	"wbtray/internal/config"
	"wbtray/internal/gateway"
)

// discoverGateway fills in the settings the tray can work out for itself.
//
// The gateway already knows its API key and its port, and it keeps both in the
// config.json beside its executable. Reading them is the difference between a
// tray that works when it is unpacked and a tray that demands a manual copy of a
// 24-character key before it will say anything. An explicit setting in the
// tray's own file always wins, so pointing the tray at a remote gateway is still
// one line of configuration.
func discoverGateway(cfg config.Config) (config.Config, bool) {
	// An address that is not the default is an explicit choice — a remote
	// gateway, or another port on purpose — and is left alone along with the key
	// that belongs with it.
	if cfg.BaseURL != config.Default().BaseURL {
		return cfg, false
	}
	exe := gateway.Find(cfg.Command)
	if exe == "" {
		return cfg, false
	}
	remote := gateway.ReadConfig(exe)
	found := false
	if cfg.APIKey == "" {
		cfg.APIKey = remote.APIKey
		found = remote.APIKey != ""
	}
	// The listen address is ":7863" or "0.0.0.0:7863" or "127.0.0.1:7863"; only
	// the port is wanted, and it is always addressed on the loopback interface,
	// because a gateway bound to every interface is still reached through
	// 127.0.0.1 from this machine.
	if port := portOf(remote.Listen); port != "" {
		cfg.BaseURL = "http://127.0.0.1:" + port
	}
	return cfg, found
}

// portOf extracts the port from a listen address such as ":7863" or
// "0.0.0.0:7863". A bare port is accepted too: it is not a listen address Go
// would take, but reading it as "the port" is the only sensible meaning and
// refusing it would leave the tray pointed at the wrong gateway for a reason the
// operator cannot see.
func portOf(listen string) string {
	i := strings.LastIndex(listen, ":")
	if i < 0 {
		i = -1
	}
	port := listen[i+1:]
	if port == "" {
		return ""
	}
	for _, r := range port {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return port
}
