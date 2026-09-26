// Command wbtray is the tray front end for the workbuddy2api console.
//
// It shows the gateway's accounts, credits and traffic in the notification area,
// lets the important switches be flipped from the icon's menu, and can start,
// stop and hide the gateway itself. Everything it displays comes from the
// gateway's own HTTP API, so the two can never disagree about the numbers.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"wbtray/internal/config"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	var (
		flagConfig  = flag.String("config", "", "path to wbtray.conf (default: beside the binary, else %APPDATA%)")
		flagBaseURL = flag.String("url", "", "gateway base URL (overrides the configuration file)")
		flagKey     = flag.String("api-key", "", "gateway API key (overrides the configuration file)")
		flagVersion = flag.Bool("version", false, "print the version and exit")
	)
	flag.Parse()

	if *flagVersion {
		fmt.Println("wbtray", version)
		return
	}

	cfgPath := *flagConfig
	if cfgPath == "" {
		cfgPath = defaultConfigPath()
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if *flagBaseURL != "" {
		cfg.BaseURL = *flagBaseURL
	}
	if *flagKey != "" {
		cfg.APIKey = *flagKey
	}
	// Discovery fills in whatever the file left empty, which is what makes a
	// first run work without copying a key out of the gateway's configuration by
	// hand. An explicit setting always wins.
	if cfg.DiscoveryEnabled {
		var found bool
		cfg, found = discoverGateway(cfg)
		if found {
			log.Printf("config: read the gateway API key from its own config.json")
		}
	}
	// A first run writes the file, so the settings the tray is about to use are
	// visible and editable rather than implied.
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		if err := config.Save(cfgPath, cfg); err != nil {
			log.Printf("config: could not write %s: %v", cfgPath, err)
		} else {
			log.Printf("config: wrote %s", cfgPath)
		}
	}

	if err := run(cfg, cfgPath); err != nil {
		log.Fatalf("wbtray: %v", err)
	}
}

// defaultConfigPath prefers a file beside the executable, because that is what
// an unpacked copy implies, and falls back to the per-user configuration
// directory when the program is installed somewhere it cannot write.
func defaultConfigPath() string {
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		if writable(dir) {
			return filepath.Join(dir, "wbtray.conf")
		}
	}
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "wbtray", "wbtray.conf")
	}
	return "wbtray.conf"
}

// writable reports whether a directory may be written to, which is what decides
// where the configuration file goes.
func writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".wbtray-probe-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	_ = os.Remove(name)
	return true
}
