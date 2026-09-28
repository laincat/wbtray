// Package app wires the tray together: the icon, the panel client, the gateway
// process and the windows that show what is going on.
//
// The interesting parts — the menu model, the refresh policy, the wording — live
// here rather than in the platform front end, which is what makes them testable
// without a desktop.
package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	"wbtray/internal/config"
	"wbtray/internal/i18n"
	"wbtray/internal/install"
	"wbtray/internal/panel"
	"wbtray/internal/status"
	"wbtray/internal/theme"
)

// Options is what the front end hands the application at startup.
type Options struct {
	// ConfigPath is where the tray's own settings are written back.
	ConfigPath string
	// Notify raises a balloon; the front end provides it because it needs a
	// window handle.
	Notify func(title, text string, level int)
	// OpenURL opens a link in the default browser.
	OpenURL func(url string) error
	// OpenPath opens a file or folder with the shell.
	OpenPath func(path string) error
	// RefreshIcon asks the front end to repaint the tray icon.
	RefreshIcon func()
	// RecordGatewayVersion notes which version of the gateway is on disk. It is
	// how a copy that was unpacked by hand, and therefore has no version file,
	// acquires one.
	RecordGatewayVersion func(version string)
	// SystemDark reports whether Windows is using its dark app theme, so the
	// "follow the system" appearance can be resolved.
	SystemDark func() bool
	// Accent reports the accent colour Windows is using, so the tray's mark can be
	// the same colour as the system's own highlights. A nil function means the
	// setting could not be read, and the palette then uses the default accent.
	Accent func() theme.Accent
	// Quit ends the application.
	Quit func()
}

// Gateway is the process control the application needs from the front end.
type Gateway interface {
	// PID reports where the gateway is and whether it is running.
	PID() status.PIDInfo
	// AdoptProcess claims a gateway started outside the controller, so the menu
	// attributes it to wbtray rather than to somebody else.
	AdoptProcess(pid uint32, exe string)
	// Start launches it.
	Start() (uint32, error)
	// Stop ends it.
	Stop() error
	// SetConsole shows or hides its window.
	SetConsole(show bool) error
	// HasConsole reports whether the gateway currently owns a console window,
	// which is what the menu's show/hide row acts on.
	HasConsole() bool
	// Restart stops and starts it.
	Restart() (uint32, error)
	// AutoStartEnabled reports whether the gateway starts with Windows.
	AutoStartEnabled() bool
	// SetAutoStart writes the gateway's own autostart entry.
	SetAutoStart(on bool) error
}

// App is the running tray application.
type App struct {
	mu     sync.Mutex
	cfg    config.Config
	opts   Options
	client *panel.Client

	snap   status.Snapshot
	snapAt time.Time
	paused bool

	// failures counts consecutive failed refreshes, which is what decides when a
	// gateway that has gone away is worth a notification.
	failures       int
	announcedDown  bool
	announcedEmpty bool
	// trayAutoStart mirrors the registry entry, read once at startup and written
	// whenever the menu changes it.
	trayAutoStart bool
	// setTrayAutoStart writes the registry entry.
	setTrayAutoStart func(on bool) error

	gateway Gateway
	// frontEnd is the running tray, which owns the window and the clock.
	frontEnd FrontEnd
	// bootstrapStep is how the gateway was brought up at startup.
	bootstrapStep install.Step
	// startedPID is the gateway this process started, or 0.
	startedPID uint32
	// startedExe is the path of the gateway this process started.
	startedExe string
	// gatewayVersion is the version of the gateway on disk.
	gatewayVersion string
	// trayVersion is the running build's own version, set at startup.
	trayVersion string
	// updates is what the last version check found.
	updates UpdateState
	// schedule is which of the gateway's scheduled tasks are running, as last read
	// from the gateway itself.
	schedule panel.Schedule
	// gatewayInstalled reports whether wbtray's own copy of the gateway is on
	// disk, which the front end sets and the menu reads.
	gatewayInstalled bool
}

// FrontEnd is what the application asks of the running tray: a tooltip, and a way to
// be told when the numbers change.
//
// The second half of that used to be a window that showed the readings at a size a
// chart needs. It is gone, so the interface is the tooltip alone — and an interface
// with one method is still worth having, because it is what keeps the application
// from reaching into the front end for anything else.
type FrontEnd interface {
	SetTooltip(text string)
}

// New builds the application.
func New(cfg config.Config, cfgPath string, gw Gateway, opts Options) *App {
	opts.ConfigPath = cfgPath
	a := &App{cfg: cfg, opts: opts, gateway: gw}
	a.client = panel.New(cfg.BaseURL, cfg.APIKey, time.Duration(cfg.TimeoutSec)*time.Second)
	return a
}

// SetCallbacks installs the front end's callbacks. They arrive after
// construction because the tray icon needs the application and the application
// needs the icon: one of the two has to be wired second.
func (a *App) SetCallbacks(opts Options) {
	opts.ConfigPath = a.opts.ConfigPath
	a.mu.Lock()
	a.opts = opts
	a.mu.Unlock()
}

// SetFrontEnd attaches the running tray.
func (a *App) SetFrontEnd(f FrontEnd) {
	a.mu.Lock()
	a.frontEnd = f
	a.mu.Unlock()
}

// FrontEnd returns the attached tray, or nil before it is running.
func (a *App) FrontEnd() FrontEnd {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.frontEnd
}

// ConfigPath is where the tray's settings live, for the menu row that opens it.
func (a *App) ConfigPath() string { return a.opts.ConfigPath }

// Gateway is the process controller.
func (a *App) Gateway() Gateway { return a.gateway }

// Notify raises a balloon through the front end.
func (a *App) Notify(title, text string, level int) { a.notify("", title, text, level) }

// Quit asks the front end to exit.
func (a *App) Quit() {
	if a.opts.Quit != nil {
		a.opts.Quit()
	}
}

// HealthLabel is the one-word state, for the about box.
func (a *App) HealthLabel() string {
	a.mu.Lock()
	snap, paused, lang := a.snap, a.paused, a.cfg.Lang
	a.mu.Unlock()
	return healthLabel(lang, healthOf(snap, paused))
}

// SetTrayAutoStart wires the tray's own autostart control, which lives in the
// platform front end because it is a registry entry and a per-user path.
func (a *App) SetTrayAutoStart(enabled bool, set func(bool) error) {
	a.mu.Lock()
	a.trayAutoStart = enabled
	a.setTrayAutoStart = set
	a.mu.Unlock()
}

// TrayAutoStart reports whether the tray starts with the user's session.
func (a *App) TrayAutoStart() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.trayAutoStart
}

// ToggleTrayAutoStart flips the tray's autostart entry.
func (a *App) ToggleTrayAutoStart() error {
	a.mu.Lock()
	next := !a.trayAutoStart
	apply := a.setTrayAutoStart
	a.trayAutoStart = next
	a.mu.Unlock()
	if apply == nil {
		return nil
	}
	if err := apply(next); err != nil {
		// A registry write that failed must not leave the menu claiming it
		// succeeded.
		a.mu.Lock()
		a.trayAutoStart = !next
		a.mu.Unlock()
		return err
	}
	return nil
}

// Config returns the configuration in force.
func (a *App) Config() config.Config {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg
}

// Snapshot returns the most recent reading.
func (a *App) Snapshot() status.Snapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.snap
}

// PanelClient is the client in use, so the front end can open the panel with the
// key already in the URL.
func (a *App) PanelClient() *panel.Client { return a.client }

// PanelURL is the console page the menu's "open panel" row visits. It carries no
// key; the panel client explains why, and the gateway block's copy submenu is how
// the key reaches the console's prompt.
func (a *App) PanelURL() string { return a.client.PanelURL() }

// BaseURL is the gateway root the tray is watching.
func (a *App) BaseURL() string { return a.client.Base() }

// Paused reports whether refreshing is suspended.
func (a *App) Paused() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.paused
}

// SetPaused suspends or resumes refreshing.
func (a *App) SetPaused(paused bool) {
	a.mu.Lock()
	a.paused = paused
	a.mu.Unlock()
	if !paused {
		a.Refresh()
	}
	a.refreshIcon()
}

// Lang is the language in force.
func (a *App) Lang() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg.Lang
}

// Style is the icon style in force.
func (a *App) Style() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg.Style
}

// Metric is the metric in force.
func (a *App) Metric() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg.Metric
}

// SetStyle saves a style as the operator's choice.
func (a *App) SetStyle(style string) {
	a.mu.Lock()
	a.cfg.Style = style
	a.mu.Unlock()
	a.save()
	a.refreshIcon()
}

// SetMetric saves the metric the icon draws.
func (a *App) SetMetric(metric string) {
	a.mu.Lock()
	a.cfg.Metric = metric
	a.mu.Unlock()
	a.save()
	a.refreshIcon()
}

// SetTheme saves the palette.
func (a *App) SetTheme(name string) {
	a.mu.Lock()
	a.cfg.Theme = name
	a.mu.Unlock()
	a.save()
	a.refreshIcon()
}

// SetLang saves the language.
func (a *App) SetLang(lang string) {
	a.mu.Lock()
	a.cfg.Lang = config.NormalizeLang(lang)
	a.mu.Unlock()
	a.save()
	a.refreshIcon()
}

// SetAppearance saves when the palette is used.
func (a *App) SetAppearance(mode string) {
	a.mu.Lock()
	a.cfg.Appearance = config.NormalizeAppearance(mode)
	a.mu.Unlock()
	a.save()
	a.refreshIcon()
}

// Appearance returns the appearance in force.
func (a *App) Appearance() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg.Appearance
}

// Accent is the accent colour Windows is using, as the front end reports it.
func (a *App) Accent() theme.Accent {
	a.mu.Lock()
	fn := a.opts.Accent
	a.mu.Unlock()
	if fn == nil {
		return theme.Accent{}
	}
	return fn()
}

// Theme resolves the palette to draw with right now, which is where "follow
// Windows" turns into a concrete set of colours.
func (a *App) Theme() theme.Theme {
	a.mu.Lock()
	name, appearance := a.cfg.Theme, a.cfg.Appearance
	darkFn, accentFn := a.opts.SystemDark, a.opts.Accent
	a.mu.Unlock()

	systemDark := false
	if darkFn != nil {
		systemDark = darkFn()
	}
	var accent theme.Accent
	if accentFn != nil {
		accent = accentFn()
	}
	return theme.Resolve(name, theme.ParseAppearance(appearance), systemDark, accent)
}

// SetShowConsole remembers whether the gateway should be started with a visible
// window.
func (a *App) SetShowConsole(show bool) {
	a.mu.Lock()
	a.cfg.ShowConsole = show
	a.mu.Unlock()
	a.save()
}

// ShowConsole reports the saved preference.
func (a *App) ShowConsole() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg.ShowConsole
}

// save writes the configuration back, reporting a failure through a balloon
// rather than silently: a setting that does not survive a restart is worse than
// one that was refused.
func (a *App) save() {
	if a.opts.ConfigPath == "" {
		return
	}
	cfg := a.Config()
	if err := config.Save(a.opts.ConfigPath, cfg); err != nil {
		a.notify(config.NormalizeLang(cfg.Lang), "wbtray", err.Error(), 2)
	}
}

// T is the translation helper bound to the current language.
func (a *App) T(key string, args ...any) string {
	return i18n.T(a.Lang(), key, args...)
}

// Refresh reads the panel once and updates everything that shows it.
func (a *App) Refresh() {
	if a.Paused() {
		return
	}
	client := a.PanelClient()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	// Deferred rather than called once the snapshot arrives: everything read from the
	// gateway below shares this context, and one of the paths out of this function
	// returns before reaching an explicit cancel.
	defer cancel()

	snap := client.Fetch(ctx)

	snap.Process = a.gateway.PID()
	if a.Paused() {
		// A pause that arrived while the request was in flight must not be undone
		// by its result.
		return
	}

	a.mu.Lock()
	a.snap = snap
	a.snapAt = snap.At
	a.mu.Unlock()

	a.learnGatewayVersion(snap)
	if snap.Reachable {
		// Which tasks the gateway is running is part of what the menu shows, and it
		// is read from the gateway so the two cannot disagree. It has to share the
		// context the snapshot was taken with — cancelling that context before this
		// read left it with a dead one, and the task switches sat on "not read"
		// while every other figure arrived.
		if s := client.FetchSchedule(ctx); s.Known {
			a.SetSchedule(s)
		}
	}
	a.react(snap)
	a.refreshIcon()
	if fe := a.FrontEnd(); fe != nil {
		fe.SetTooltip(a.Tooltip())
	}
}

// learnGatewayVersion fills in the gateway's version from the gateway itself.
//
// The version is normally recorded in a small file at install time. A copy that was
// unpacked by hand has no such file, and the symptom is not an error: the version
// block simply shows nothing under "installed", forever. The running gateway knows
// what it is, so the first successful reading is the moment to write it down.
func (a *App) learnGatewayVersion(snap status.Snapshot) {
	if snap.Version == "" || !a.GatewayInstalled() {
		return
	}
	a.mu.Lock()
	known := a.gatewayVersion
	if known == "" {
		a.gatewayVersion = snap.Version
	}
	record := a.opts.RecordGatewayVersion
	a.mu.Unlock()

	if known != "" || record == nil {
		return
	}
	// A failure here is not worth a balloon: the version is now known to this
	// process either way, and the next start will try again.
	record(snap.Version)
}

// react turns a changed state into a notification, once per transition rather
// than once per refresh: a gateway that has been down for an hour is not news.
func (a *App) react(snap status.Snapshot) {
	lang := a.Lang()

	if !snap.Reachable {
		a.mu.Lock()
		a.failures++
		notify := a.failures >= 3 && !a.announcedDown
		if notify {
			a.announcedDown = true
		}
		a.mu.Unlock()
		if notify {
			a.notify(lang, i18n.T(lang, "notify.offline_title"),
				i18n.T(lang, "notify.offline", a.PanelClient().Base()), 1)
		}
		return
	}

	a.mu.Lock()
	recovered := a.announcedDown
	a.announcedDown = false
	a.failures = 0
	a.mu.Unlock()
	if recovered {
		a.notify(lang, i18n.T(lang, "notify.ready_title"),
			i18n.T(lang, "notify.ready", snap.Ready()), 0)
	}

	// A pool with accounts but no credits left is the failure that costs the
	// operator a bad afternoon, so it is worth a word.
	if snap.Total > 0 && snap.CreditTotal() <= 0 {
		a.mu.Lock()
		warn := !a.announcedEmpty
		a.announcedEmpty = true
		a.mu.Unlock()
		if warn {
			a.notify(lang, i18n.T(lang, "notify.credit_empty_title"),
				i18n.T(lang, "notify.credit_empty"), 1)
		}
		return
	}
	if snap.CreditTotal() > 0 {
		a.mu.Lock()
		a.announcedEmpty = false
		a.mu.Unlock()
	}
}

func (a *App) notify(lang, title, text string, level int) {
	if a.opts.Notify != nil {
		a.opts.Notify(title, text, level)
	}
}

func (a *App) refreshIcon() {
	if a.opts.RefreshIcon != nil {
		a.opts.RefreshIcon()
	}
}

// Interval is how often the panel is polled.
func (a *App) Interval() time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	return time.Duration(a.cfg.IntervalSec) * time.Second
}

// Reload re-reads the configuration file, which is what the menu's reload row
// does after the operator edits it by hand.
func (a *App) Reload() error {
	cfg, err := config.Load(a.opts.ConfigPath)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.cfg = cfg
	a.mu.Unlock()
	a.client = panel.New(cfg.BaseURL, cfg.APIKey, time.Duration(cfg.TimeoutSec)*time.Second)
	a.Refresh()
	return nil
}

// Tooltip is the hover text.
func (a *App) Tooltip() string {
	a.mu.Lock()
	snap, paused, cfg := a.snap, a.paused, a.cfg
	a.mu.Unlock()

	lang := cfg.Lang
	state := snap.Health().Label(lang)
	if paused {
		state = i18n.T(lang, "health.paused")
	} else if snap.Reachable && snap.Total > 0 && snap.Ready() == 0 {
		state = i18n.T(lang, "health.warn")
	}

	lines := []string{fmt.Sprintf("%s — %s", i18n.T(lang, "app.name"), state)}
	switch {
	case !snap.Reachable:
		if paused {
			lines = append(lines, cfg.BaseURL)
		} else {
			lines = append(lines, i18n.T(lang, "status.unreachable", cfg.BaseURL))
		}
	case snap.Total == 0:
		lines = append(lines, i18n.T(lang, "status.no_accounts"))
	default:
		lines = append(lines,
			i18n.T(lang, "status.accounts", snap.Ready(), snap.Total),
			i18n.T(lang, "status.credits", i18n.Num(snap.CreditTotal())),
			fmt.Sprintf("%s · %s",
				i18n.T(lang, "status.requests",
					i18n.Compact(float64(snap.Usage.Requests)),
					i18n.Compact(float64(snap.Usage.Errors))),
				i18n.T(lang, "status.inflight", snap.InFlight())),
		)
		if snap.Version != "" {
			lines = append(lines, i18n.T(lang, "status.version", snap.Version))
		}
		if snap.Process.Found {
			lines = append(lines, fmt.Sprintf("PID %d", snap.Process.PID))
		}
	}
	return joinTooltip(lines)
}

// joinTooltip joins the tooltip's lines. The shell shows a version-4
// notification icon's tip either as a NUL-separated pair or as one multi-line
// string depending on the build, so the separator is applied here and the shell
// does what it likes with the newlines.
func joinTooltip(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n"
		}
		out += l
		if len(out) > 120 {
			break
		}
	}
	return out
}
