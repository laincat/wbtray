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
//
// There are five, and the short list is the point. An earlier version offered
// eight, of which three were the same three shapes with a number scribbled over
// them; at sixteen pixels that number was a grey smudge, so the "choice" was
// between three legible marks and three illegible ones.
const (
	// StyleGauge is the donut: the reading as a filled arc.
	StyleGauge = "gauge"
	// StyleBars is the four-bar chart: the reading as a trend over the window.
	StyleBars = "bars"
	// StyleSpark is the trend line: the recent history as a curve.
	StyleSpark = "spark"
	// StyleNumber is the figure itself, which is the one thing a shape cannot do.
	StyleNumber = "number"
	// StyleCat is the mascot, wearing the palette and carrying the health in its
	// coat.
	StyleCat = "cat"
)

// Styles is the menu order, which is also the order of the preview sheet.
//
// The three shape styles come first, because a shape is what a sixteen-pixel icon
// is best at. Then the figure, which says a number and nothing else. Then the
// mascot, which is an identity rather than a reading and is the one to pick when
// the tray should be recognisable rather than informative.
var Styles = []string{StyleGauge, StyleBars, StyleSpark, StyleNumber, StyleCat}

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
	BaseURL string
	APIKey  string
	// DiscoveryEnabled lets the tray read the gateway's own config.json for the
	// API key and listen address when this file does not set them, which is what
	// makes a first run work without copying anything by hand.
	DiscoveryEnabled bool
	IntervalSec      int
	TimeoutSec       int

	Style  string
	Metric string
	Lang   string // "zh" or "en"
	// Theme is the palette, shared by the icon, the drawn menu and the monitor
	// window.
	Theme string
	// Appearance decides when that palette is used: always, or matching whatever
	// Windows is doing.
	Appearance string

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
		BaseURL:          "http://127.0.0.1:7863",
		DiscoveryEnabled: true,
		IntervalSec:      3,
		TimeoutSec:       5,
		Style:            StyleGauge,
		Metric:           MetricAccounts,
		Lang:             "zh",
		Theme:            "system",
		Appearance:       "auto",
		ShowConsole:      false,
		ManageProcess:    true,
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
		// Retired: the menu is the system menu, and there is no other. The key is
		// accepted and ignored rather than rejected, so a configuration file
		// written by an older build still opens.
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
	// A style name from an earlier build is mapped rather than rejected, so a
	// configuration file written when there were eight styles still opens on the
	// one that means the same thing.
	cfg.Style = NormalizeStyle(cfg.Style)
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
}

func validStyle(s string) bool {
	for _, v := range Styles {
		if v == s {
			return true
		}
	}
	return false
}

// NormalizeStyle maps a style name onto one the tray has.
//
// The renames are not cosmetic: the gauge used to be called "ring", the chart
// "bar", and three styles that drew a figure over a shape are now the one figure
// style. A configuration file written by an earlier build has to keep opening, and
// a person reading it should find a name that matches what the menu calls the
// style today.
func NormalizeStyle(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "gauge", "ring", "plain":
		// "plain" was a ring with a break cut out of it; it read as a letter Q at
		// sixteen pixels, so it is the gauge now.
		return StyleGauge
	case "bars", "bar":
		return StyleBars
	case "spark":
		return StyleSpark
	case "number", "text", "bartext", "mascottext":
		// The three figure styles collapsed into one: the shape behind a
		// four-character figure at sixteen pixels was never visible, so the two
		// that had one were drawing a smudge behind a smudge.
		return StyleNumber
	case "cat", "mascot":
		return StyleCat
	default:
		return StyleGauge
	}
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
	fmt.Fprintf(&b, "lang = %s\n\n", cfg.Lang)
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
