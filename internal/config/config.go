// Package config holds the tray's own settings, which are deliberately few.
//
// The gateway's settings are edited through its own HTTP API, so nothing here
// duplicates them: this file only holds what the tray process itself needs.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"wbtray/internal/theme"
)

// Style names one of the tray looks. They are identified by name rather than by
// index so a config file written by one build still means the same thing in the
// next.
const (
	StyleRing   = "ring"
	StyleBar    = "bar"
	StyleSpark  = "spark"
	StyleMascot = "mascot"
	StylePlain  = "plain"
)

// Styles is the menu order, which is also the order of the preview sheet.
var Styles = []string{StyleRing, StyleBar, StyleSpark, StyleMascot, StylePlain}

// Metric names what the tray icon draws, and what the menu's live entries show
// as text.
const (
	MetricAccounts = "accounts"
	MetricCredits  = "credits"
	MetricRequests = "requests"
	MetricTokens   = "tokens"
	MetricLatency  = "latency"
	MetricTPS      = "tps"
	MetricQueue    = "queue"
)

// Metrics is the menu and settings order.
var Metrics = []string{MetricAccounts, MetricCredits, MetricRequests, MetricTokens, MetricLatency, MetricTPS, MetricQueue}

// Config is the tray's own configuration.
type Config struct {
	// BaseURL is the gateway root, without a trailing slash. The API key is the
	// one from the gateway's config.json and travels as a Bearer token.
	BaseURL     string
	APIKey      string
	// DiscoveryEnabled lets the tray read the gateway's own config.json for the
	// API key and listen address when this file does not set them, which is what
	// makes a first run work without copying anything by hand.
	DiscoveryEnabled bool
	IntervalSec int
	TimeoutSec  int

	Style  string
	Metric string
	Lang   string // "zh" or "en"
	// Theme is the palette, shared by the icon, the drawn menu and the monitor
	// window.
	Theme string
	// Appearance decides when that palette is used: always, or matching whatever
	// Windows is doing.
	Appearance string
	// MenuStyle is "flyout" for the drawn menu or "native" for the system menu.
	MenuStyle string

	// ShowConsole controls whether the gateway runs with a visible console
	// window when the tray starts it.
	ShowConsole bool
	// ManageProcess allows the tray to start and stop the gateway. Read-only
	// observation works with it off, for a gateway the tray did not start.
	ManageProcess bool
	// Command is the gateway executable; empty means "look for wb2api.exe
	// beside the tray, then in the places a Windows install usually lands".
	Command string
	WorkDir string

	// AutoStart is mirrored into the registry, so it is written back whenever
	// the tray changes it from the menu.
	AutoStart bool
}

// Default returns the configuration a first run uses.
func Default() Config {
	return Config{
		BaseURL:       "http://127.0.0.1:7863",
		DiscoveryEnabled: true,
		IntervalSec:   3,
		TimeoutSec:    5,
		Style:         StyleRing,
		Metric:        MetricAccounts,
		Lang:          "zh",
		Theme:         "neon",
		Appearance:    "auto",
		MenuStyle:     "flyout",
		ShowConsole:   false,
		ManageProcess: true,
	}
}

// Path is where the tray keeps its configuration.
func Path() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "wbtray", "wbtray.conf")
	}
	return "wbtray.conf"
}

// Load reads the config file, falling back to the defaults for anything the
// file does not set. A missing file is not an error: the defaults point at the
// gateway's usual address and port.
func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), "\"")
		apply(&cfg, key, value)
	}
	normalize(&cfg)
	return cfg, sc.Err()
}

func apply(cfg *Config, key, value string) {
	switch strings.ToLower(key) {
	case "base_url":
		cfg.BaseURL = value
	case "api_key":
		cfg.APIKey = value
	case "discover":
		cfg.DiscoveryEnabled = truthy(value)
	case "interval_seconds":
		cfg.IntervalSec = atoi(value, cfg.IntervalSec)
	case "timeout_seconds":
		cfg.TimeoutSec = atoi(value, cfg.TimeoutSec)
	case "style":
		cfg.Style = value
	case "metric":
		cfg.Metric = value
	case "theme":
		cfg.Theme = value
	case "appearance":
		cfg.Appearance = value
	case "menu_style":
		cfg.MenuStyle = value
	case "lang", "language":
		cfg.Lang = value
	case "show_console":
		cfg.ShowConsole = truthy(value)
	case "manage_process":
		cfg.ManageProcess = truthy(value)
	case "command":
		cfg.Command = value
	case "work_dir":
		cfg.WorkDir = value
	case "autostart":
		cfg.AutoStart = truthy(value)
	}
}

func atoi(s string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return fallback
	}
	return n
}

func truthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func normalize(cfg *Config) {
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if cfg.BaseURL == "" {
		cfg.BaseURL = Default().BaseURL
	}
	if cfg.IntervalSec < 1 {
		cfg.IntervalSec = 1
	}
	if cfg.IntervalSec > 60 {
		cfg.IntervalSec = 60
	}
	if cfg.TimeoutSec < 1 {
		cfg.TimeoutSec = 1
	}
	if cfg.TimeoutSec > 60 {
		cfg.TimeoutSec = 60
	}
	if !validStyle(cfg.Style) {
		cfg.Style = StyleRing
	}
	if !validMetric(cfg.Metric) {
		cfg.Metric = MetricAccounts
	}
	cfg.Lang = NormalizeLang(cfg.Lang)
	// A theme name the tray does not know is mapped rather than rejected: a file
	// written when there were six palettes has to keep opening, and the mapping
	// lives in one place so the menu and the loader cannot disagree about it.
	cfg.Theme = NormalizeTheme(cfg.Theme)
	switch strings.ToLower(strings.TrimSpace(cfg.Appearance)) {
	case "dark":
		cfg.Appearance = "dark"
	case "light":
		cfg.Appearance = "light"
	default:
		cfg.Appearance = "auto"
	}
	if cfg.MenuStyle != "native" {
		cfg.MenuStyle = "flyout"
	}
}

func validStyle(s string) bool {
	for _, v := range Styles {
		if v == s {
			return true
		}
	}
	return false
}

func validMetric(m string) bool {
	for _, v := range Metrics {
		if v == m {
			return true
		}
	}
	return false
}

// NormalizeLang maps anything the locale or the file says onto a supported tag.
func NormalizeLang(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch {
	case s == "":
		return "en"
	case strings.HasPrefix(s, "zh"), strings.HasPrefix(s, "cn"), s == "chinese":
		return "zh"
	default:
		return "en"
	}
}

// NormalizeAppearance maps a configuration value onto the appearance the tray
// understands. It is duplicated from the theme package on purpose: config is
// loaded before anything is drawn, and pulling the drawing packages into the
// loader would make the settings file depend on the renderer.
func NormalizeAppearance(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "dark", "always_dark":
		return "dark"
	case "light", "always_light":
		return "light"
	default:
		return "auto"
	}
}

// NormalizeTheme maps a theme name onto one the tray has.
//
// The mapping itself lives in the theme package, which owns the palettes; this is
// a pass-through so a caller of config does not have to reach for two packages to
// answer one question. Two copies of the rule would be two things to update, and
// one of them would be forgotten.
func NormalizeTheme(s string) string { return theme.Normalize(s) }

// Save writes the configuration atomically.
func Save(path string, cfg Config) error {
	normalize(&cfg)
	var b strings.Builder
	b.WriteString("# wbtray - tray for the workbuddy2api panel.\n")
	b.WriteString("# Every tray setting is also reachable from the tray menu, which rewrites this file.\n\n")
	fmt.Fprintf(&b, "base_url = %s\n", cfg.BaseURL)
	fmt.Fprintf(&b, "api_key = %s\n", cfg.APIKey)
	fmt.Fprintf(&b, "# discover reads the gateway's own config.json for the API key and port when\n")
	fmt.Fprintf(&b, "# the two settings above are empty.\n")
	fmt.Fprintf(&b, "discover = %t\n", cfg.DiscoveryEnabled)
	fmt.Fprintf(&b, "interval_seconds = %d\n", cfg.IntervalSec)
	fmt.Fprintf(&b, "timeout_seconds = %d\n\n", cfg.TimeoutSec)
	fmt.Fprintf(&b, "style = %s\n", cfg.Style)
	fmt.Fprintf(&b, "metric = %s\n", cfg.Metric)
	fmt.Fprintf(&b, "theme = %s\n", cfg.Theme)
	fmt.Fprintf(&b, "appearance = %s\n", cfg.Appearance)
	fmt.Fprintf(&b, "lang = %s\n", cfg.Lang)
	fmt.Fprintf(&b, "menu_style = %s\n\n", cfg.MenuStyle)
	fmt.Fprintf(&b, "show_console = %t\n", cfg.ShowConsole)
	fmt.Fprintf(&b, "manage_process = %t\n", cfg.ManageProcess)
	fmt.Fprintf(&b, "autostart = %t\n", cfg.AutoStart)
	if cfg.Command != "" {
		fmt.Fprintf(&b, "command = %s\n", cfg.Command)
	}
	if cfg.WorkDir != "" {
		fmt.Fprintf(&b, "work_dir = %s\n", cfg.WorkDir)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
