//go:build windows

package main

import (
	"os"
	"testing"

	"wbtray/internal/config"
)

// TestDiscoveryAgainstTheInstalledGateway is a manual check, skipped unless the
// environment names a gateway: it reads the machine's real installation, which is
// the only way to confirm that the discovery path works against a configuration
// this program did not write.
//
//	$env:WBTRAY_LIVE_GW = "D:Program Fileswb2api-panelwb2api.exe"
//	go test ./cmd/wbtray -run Live -v
func TestDiscoveryAgainstTheInstalledGateway(t *testing.T) {
	exe := os.Getenv("WBTRAY_LIVE_GW")
	if exe == "" {
		t.Skip("set WBTRAY_LIVE_GW to the gateway executable to run this")
	}
	cfg := config.Default()
	cfg.Command = exe
	got, found := discoverGateway(cfg)
	if !found {
		t.Fatal("no API key was discovered")
	}
	t.Logf("discovered key %q and base URL %q", redact(got.APIKey), got.BaseURL)
	if got.APIKey == "" || got.BaseURL == "" {
		t.Fatal("discovery produced an unusable configuration")
	}
}

// redact keeps a real key out of a test log while still showing its length, so a
// failure can be diagnosed without leaking the credential into a transcript.
func redact(key string) string {
	if len(key) <= 6 {
		return "…"
	}
	return key[:3] + "…" + key[len(key)-2:]
}
