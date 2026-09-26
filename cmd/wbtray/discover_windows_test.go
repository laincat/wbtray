//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"

	"wbtray/internal/config"
	"wbtray/internal/gateway"
)

// TestPortOf covers the address shapes a gateway's configuration uses.
func TestPortOf(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{":7863", "7863"},
		{"0.0.0.0:8080", "8080"},
		{"127.0.0.1:7863", "7863"},
		{"", ""},
		{"7863", "7863"},
		{":", ""},
		{":abc", ""},
	} {
		if got := portOf(tc.in); got != tc.want {
			t.Errorf("portOf(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestFindPrefersAnExplicitPath checks that a configured path is used when it
// exists, and that a path that does not exist does not silently fall back to a
// different gateway: the operator named one, and starting another would be a
// surprising answer.
func TestFindPrefersAnExplicitPath(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, gateway.DefaultName)
	if err := os.WriteFile(exe, []byte("not really a program"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := gateway.Find(exe); got != exe {
		t.Errorf("Find(explicit) = %q, want %q", got, exe)
	}
	if got := gateway.Find(filepath.Join(dir, "missing.exe")); got != "" {
		t.Errorf("Find(missing) = %q, want an empty result", got)
	}
}

// TestDiscoveryDoesNotOverrideAnExplicitAddress is the rule that keeps the tray
// usable against a remote gateway: discovery only fills in a default address.
func TestDiscoveryDoesNotOverrideAnExplicitAddress(t *testing.T) {
	cfg := config.Default()
	cfg.BaseURL = "http://192.0.2.10:9000"
	cfg.APIKey = "explicit"
	got, _ := discoverGateway(cfg)
	if got.BaseURL != cfg.BaseURL || got.APIKey != cfg.APIKey {
		t.Fatalf("discovery changed an explicit setting: %+v", got)
	}
}

// TestDiscoveryLeavesAnEmptyPortAlone checks that a gateway whose configuration
// names no port does not cause a bogus URL.
func TestDiscoveryLeavesAnEmptyPortAlone(t *testing.T) {
	cfg := config.Default()
	cfg.Command = filepath.Join(t.TempDir(), "missing.exe")
	got, _ := discoverGateway(cfg)
	if got.BaseURL != config.Default().BaseURL {
		t.Fatalf("BaseURL = %q, want the default", got.BaseURL)
	}
}

// TestDiscoveryReadsARealGatewayConfig is the behaviour the tray relies on in
// practice: the key and the port come out of the file the gateway itself wrote.
func TestDiscoveryReadsARealGatewayConfig(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, gateway.DefaultName)
	if err := os.WriteFile(exe, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	body := `{"api_key": "sk-from-the-gateway", "listen": ":8080"}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.Command = exe
	got, found := discoverGateway(cfg)
	if !found {
		t.Fatal("discovery reported no key")
	}
	if got.APIKey != "sk-from-the-gateway" {
		t.Errorf("APIKey = %q", got.APIKey)
	}
	if got.BaseURL != "http://127.0.0.1:8080" {
		t.Errorf("BaseURL = %q, want the port from the gateway's own file", got.BaseURL)
	}
}
