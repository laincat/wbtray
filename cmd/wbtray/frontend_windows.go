//go:build windows

package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"wbtray/internal/app"
	"wbtray/internal/autostart"
	"wbtray/internal/config"
	"wbtray/internal/i18n"
	"wbtray/internal/iconstyle"
	"wbtray/internal/install"
	"wbtray/internal/raster"
	"wbtray/internal/theme"
	"wbtray/internal/tray"
	"wbtray/internal/traymenu"
	"wbtray/internal/winapi"
)

// The Windows front end: the icon, the clock, and the window that shows the
// curve.

// run starts the tray and stays until the user quits.
func run(cfg config.Config, cfgPath string) error {
	gw := newGateway(cfg)
	a := app.New(cfg, cfgPath, gw, app.Options{})
	_ = gw

	icon := tray.New("wbtray", tray.Callbacks{})
	// The window is created here, on the thread that will pump its messages, and
	// hidden until it is asked for. A tray's main window is opened and dismissed
	// many times per session, so it is built once and its lifetime is the tray's.
	win, err := tray.NewWindow(renderIcon(a))
	if err != nil {
		// A window that cannot be created is not a reason to refuse to run: the
		// tray is the program and the window is a convenience. The menu still opens
		// the console in the browser, which is the fallback the panel row has always
		// had.
		log.Printf("window: %v", err)
	}
	if win != nil {
		win.SetSources(a.Snapshot, a.Theme)
		win.SetLang(a.Lang)
		defer win.Close()
	}
	// The renamed build an earlier self-update left behind is removed here, which
	// is the first moment at which nothing is holding it.
	install.CleanUpPrevious()
	lc := &lifecycle{app: a, layout: layoutFor()}
	// The tray panel. It is created whether or not the console window was, because
	// they are independent: the panel is what a right click opens and the window is
	// what a left click does, and a machine where one failed should still have the
	// other.
	// Declared outside the if so the settings callback below can ask it to repaint.
	var panel *tray.Panel
	if p, err := tray.NewTrayPanel(renderIcon(a)); err != nil {
		log.Printf("panel: %v", err)
	} else {
		panel = p
		defer panel.Close()
		panel.SetSources(a.Snapshot, a.Theme, panelState(a, lc))
		panel.SetLang(a.Lang)
		panel.SetActions(panelActions(a, lc, win, panel))
	}
	if win != nil {
		// The window is wired here rather than where it was created, because its
		// actions touch the gateway's installation and the lifecycle that describes
		// it does not exist yet at that point.
		win.SetActions(windowActions(a, lc, win))
		tray.SetConfigPath(func() (string, bool) {
			return configPathForGateway(lc.layout), true
		})
		// The window is a view of the gateway, so opening it is the moment to read
		// the pages it shows. Reading on the way in means the operator never sees an
		// empty state for a page that has data.
		win.OnShow(func() {
			ctx, cancel := contextWithTimeout(30 * time.Second)
			defer cancel()
			loadWindowData(ctx, a, win)
		})
	}
	a.SetGatewayVersion(lc.layout.InstalledGatewayVersion())
	a.SetGatewayInstalled(lc.layout.Present())
	a.SetTrayVersion(version)
	a.SetTrayAutoStart(autostart.Enabled(), func(on bool) error {
		return autostart.Set(on)
	})

	a.SetFrontEnd(&frontEnd{icon: icon, app: a})

	stop := make(chan struct{})
	var once sync.Once
	a.SetCallbacks(app.Options{
		Notify:      func(title, text string, level int) { icon.Notify(title, text, level) },
		OpenURL:     openURL,
		OpenPath:    openPath,
		RefreshIcon: func() { icon.SetIcon(renderIcon(a)) },
		SettingsChanged: func() {
			// The icon is redrawn by RefreshIcon; these two draw the palette and the
			// language, so a choice made in the menu shows up while the window is
			// open rather than on the next launch.
			if win != nil {
				win.Invalidate()
			}
			if panel != nil {
				panel.Invalidate()
			}
		},
		RecordGatewayVersion: func(v string) {
			_ = lc.layout.RecordGatewayVersion(v)
		},
		SystemDark: winapi.SystemDark,
		Accent:     systemAccent,
		Quit:       func() { once.Do(func() { close(stop) }); icon.Quit() },
	})
	icon.SetCallbacks(tray.Callbacks{
		Menu:   func() []traymenu.Item { return a.Menu() },
		Select: func(ev traymenu.Event) { handleCommand(a, lc, win, ev) },
		// A left click opens the window, the way the tray beside it does: a
		// single window is the product's main surface, and the console in a
		// browser is one row away in the menu for the operations the window does
		// not carry.
		Click: func() {
			if win != nil {
				win.Toggle()
				return
			}
			_ = openURL(a.PanelClient().PanelURL())
		},
		// The tray's own timer runs every second and only re-reads the tooltip.
		// Polling the gateway from here as well would ignore the configured
		// interval entirely and query the panel once a second.
		Tick:     func() { icon.SetTooltip(a.Tooltip()) },
		IconView: func(size int) *raster.Canvas { return renderIconSized(a, size) },
		StylePreview: func(index, size int) *raster.Canvas {
			if index < 0 || index >= len(config.Styles) {
				return nil
			}
			return iconstyle.Draw(iconstyle.View{
				Size:    size,
				Style:   config.Styles[index],
				Metric:  a.Config().Metric,
				Lang:    a.Lang(),
				Palette: a.Theme(),
				Snap:    a.Snapshot(),
				Paused:  a.Paused(),
			})
		},
	})
	go startTicker(a, stop)
	go startVersionChecks(a, stop)
	// The first reading is taken immediately rather than after one interval, so
	// the icon is correct from the moment it appears instead of showing an empty
	// gauge for the first few seconds.
	go func() {
		// The gateway is brought up before the first reading, so the tray's first
		// state is the real one rather than "offline" for a machine where nothing
		// is wrong.
		bootstrapGateway(a, lc)
	}()

	runErr := icon.Run()
	once.Do(func() { close(stop) })
	return runErr
}

// startTicker refreshes on the configured interval.
//
// The interval is re-read every time, so changing it in the configuration file
// and reloading takes effect without a restart.
func startTicker(a *app.App, stop <-chan struct{}) {
	for {
		interval := a.Interval()
		select {
		case <-stop:
			return
		case <-time.After(interval):
			a.Refresh()
		}
	}
}

// renderIcon draws the tray icon at the shell's small-icon metric.
func renderIcon(a *app.App) *raster.Canvas {
	return renderIconSized(a, winapi.IconMetric())
}

func renderIconSized(a *app.App, size int) *raster.Canvas {
	cfg := a.Config()
	return iconstyle.Draw(iconstyle.View{
		Size:    size,
		Style:   a.Style(),
		Metric:  cfg.Metric,
		Lang:    cfg.Lang,
		Palette: a.Theme(),
		Snap:    a.Snapshot(),
		Paused:  a.Paused(),
	})
}

// handleCommand routes a menu click.
//
// The lifecycle travels with the application rather than being looked up, so the
// commands that touch the gateway's installation have the layout and the release
// state that belong to this run.
func handleCommand(a *app.App, lc *lifecycle, win *tray.Window, ev traymenu.Event) {
	switch {
	case ev.ID >= app.IDStyleBase && ev.ID < app.IDMetricBase:
		if name := styleNameForID(ev.ID); name != "" {
			a.SetStyle(name)
		}
		return
	case ev.ID >= app.IDMetricBase && ev.ID < app.IDThemeBase:
		if i := int(ev.ID - app.IDMetricBase); i < len(config.Metrics) {
			a.SetMetric(config.Metrics[i])
		}
		return
	case ev.ID >= app.IDThemeBase && ev.ID < app.IDLangBase:
		if i := int(ev.ID - app.IDThemeBase); i < len(theme.Names) {
			a.SetTheme(theme.Names[i])
		}
		return
	case ev.ID >= app.IDAppIconBase && ev.ID < app.IDAppIconBase+uint32(len(config.AppIcons)):
		if i := int(ev.ID - app.IDAppIconBase); i < len(config.AppIcons) {
			a.SetAppIcon(config.AppIcons[i])
		}
		return
	case ev.ID >= app.IDLangBase && ev.ID < app.IDLangBase+2:
		if ev.ID == app.IDLangBase {
			a.SetLang("zh")
		} else {
			a.SetLang("en")
		}
		return
	case ev.ID >= app.IDTaskBase && ev.ID < app.IDTaskBase+uint32(len(app.Tasks)):
		task := app.Tasks[ev.ID-app.IDTaskBase]
		go runTask(a, task)
		return
	case ev.ID == app.IDOpenPanel:
		// The window is the panel now, and the browser is the fallback for a
		// machine where it could not be created.
		if win != nil {
			win.Show()
			return
		}
		_ = openURL(a.PanelClient().PanelURL())
		return
	case ev.ID == app.IDCopyURL:
		copyAndSay(a, i18n.T(a.Lang(), "copy.addr"), a.PanelClient().Base())
		return
	case ev.ID == app.IDCopyKey:
		key := a.PanelClient().Key()
		if key == "" {
			a.Notify(i18n.T(a.Lang(), "copy.key"), i18n.T(a.Lang(), "copy.none"), 1)
			return
		}
		copyAndSay(a, i18n.T(a.Lang(), "copy.key"), key)
		return
	case ev.ID == app.IDOpenConfig:
		_ = openPath(a.ConfigPath())
		return
	case ev.ID == app.IDOpenFolder:
		if exe, err := os.Executable(); err == nil {
			_ = openPath(filepath.Dir(exe))
		}
		return
	case ev.ID == app.IDLogin:
		_ = openURL(a.PanelClient().PanelURL())
		return
	case ev.ID == app.IDOpenGatewayDir:
		_ = openPath(lc.layout.Gateway)
		return
	case ev.ID == app.IDOpenGatewayConfig:
		_ = openPath(configPathForGateway(lc.layout))
		return
	case ev.ID == app.IDInstallGateway:
		go installGatewayOnDemand(a, lc)
		return
	case ev.ID == app.IDUpdateGateway:
		go updateGateway(a, lc)
		return
	case ev.ID == app.IDUpdateTray:
		go updateTray(a, lc, a.Quit)
		return
	case ev.ID == app.IDCheckUpdates:
		go checkUpdatesNow(a)
		return
	case ev.ID == app.IDReload:
		if err := a.Reload(); err != nil {
			a.Notify("wbtray", err.Error(), 2)
		}
		return
	case ev.ID == app.IDRefresh:
		go a.Refresh()
		return
	case ev.ID == app.IDPause:
		a.SetPaused(!a.Paused())
		a.Notify("wbtray", a.Tooltip(), 0)
		return
	case ev.ID >= app.IDAppearanceBase && ev.ID < app.IDAppearanceBase+8:
		if i := int(ev.ID - app.IDAppearanceBase); i < len(theme.Appearances) {
			a.SetAppearance(string(theme.Appearances[i]))
		}
		return
	case ev.ID == app.IDTrayAutoStart:
		if err := a.ToggleTrayAutoStart(); err != nil {
			a.Notify("wbtray", err.Error(), 2)
		}
		return
	case ev.ID == app.IDAbout:
		showAbout(a)
		return
	case ev.ID == app.IDQuit:
		a.Quit()
		return

	case ev.ID == app.IDGatewayStart:
		go gatewayAction(a, "start")
	case ev.ID == app.IDGatewayStop:
		go gatewayAction(a, "stop")
	case ev.ID == app.IDGatewayRestart:
		go gatewayAction(a, "restart")
	case ev.ID == app.IDConsoleToggle:
		go toggleConsole(a)
	case ev.ID == app.IDGatewayAutoStart:
		go toggleGatewayAutoStart(a)
	}
}

// runTask posts a maintenance action and reports what came back.
func runTask(a *app.App, task app.Task) {
	lang := a.Lang()
	name := i18n.T(lang, task.Key)
	ctx, cancel := contextWithTimeout(20 * time.Second)
	defer cancel()
	summary, err := a.PanelClient().Trigger(ctx, task.Path)
	if err != nil {
		a.Notify(name, i18n.T(lang, "task.failed", err), 2)
		return
	}
	a.Notify(name, i18n.T(lang, "task.done", summary), 0)
}

// gatewayAction runs one of the three process commands.
func gatewayAction(a *app.App, action string) {
	lang := a.Lang()
	switch action {
	case "start":
		pid, err := a.Gateway().Start()
		if err != nil {
			a.Notify(i18n.T(lang, "gw.start"), i18n.T(lang, "gw.start_failed", err), 2)
			return
		}
		a.Notify(i18n.T(lang, "gw.start"), i18n.T(lang, "gw.started", pid), 0)
	case "stop":
		if err := a.Gateway().Stop(); err != nil {
			a.Notify(i18n.T(lang, "gw.stop"), i18n.T(lang, "task.failed", err), 2)
			return
		}
		a.Notify(i18n.T(lang, "gw.stop"), i18n.T(lang, "gw.stopped"), 0)
	case "restart":
		pid, err := a.Gateway().Restart()
		if err != nil {
			a.Notify(i18n.T(lang, "gw.restart"), i18n.T(lang, "gw.start_failed", err), 2)
			return
		}
		a.Notify(i18n.T(lang, "gw.restart"), i18n.T(lang, "gw.started", pid), 0)
	}
	// The first reading after a process change is the one that shows whether it
	// worked, so it is taken immediately rather than at the next tick.
	time.Sleep(900 * time.Millisecond)
	a.Refresh()
}

// toggleConsole shows or hides the gateway's console window.
func toggleConsole(a *app.App) {
	lang := a.Lang()
	show := !a.Gateway().HasConsole()
	if err := a.Gateway().SetConsole(show); err != nil {
		a.Notify(i18n.T(lang, "menu.gateway"), i18n.T(lang, "task.failed", err), 2)
		return
	}
	a.SetShowConsole(show)
	a.Refresh()
}

// toggleGatewayAutoStart flips the gateway's own autostart entry.
func toggleGatewayAutoStart(a *app.App) {
	lang := a.Lang()
	next := !a.Gateway().AutoStartEnabled()
	if err := a.Gateway().SetAutoStart(next); err != nil {
		a.Notify(i18n.T(lang, "menu.gw_autostart"), i18n.T(lang, "task.failed", err), 2)
		return
	}
	a.Refresh()
}

// styleNameForID maps a style row's id back to its name.
func styleNameForID(id uint32) string {
	i := int(id - app.IDStyleBase)
	if i < 0 || i >= len(config.Styles) {
		return ""
	}
	return config.Styles[i]
}

// showAbout raises the shell's own message box: an about panel is not worth a
// window of its own, and the shell's version is the one the system's
// accessibility settings already cover.
func showAbout(a *app.App) {
	snap := a.Snapshot()
	body := i18n.T(a.Lang(), "dlg.about_body",
		a.PanelClient().Base(),
		a.HealthLabel(),
		snap.Ready(), snap.Total, i18n.Num(snap.CreditTotal()),
		snap.Version)
	messageBox(i18n.T(a.Lang(), "dlg.about_title"), body)
}

// openURL opens a link in the default browser.
func openURL(url string) error {
	return shellOpen(url, "")
}

// openPath opens a file or folder with the shell.
func openPath(path string) error {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return shellOpen(path, filepath.Dir(path))
}

// shellOpen launches the shell's default handler.
func shellOpen(target, dir string) error {
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	if dir != "" {
		cmd.Dir = dir
	}
	return cmd.Start()
}

// copyToClipboard puts a string on the clipboard.
//
// It goes through the clipboard API rather than `cmd /c clip`, which is the
// obvious implementation and the wrong one for a program with no console: it
// starts a console host on every copy, and it resolves `clip` through the current
// directory before the system one, so a file of that name beside the tray would be
// run instead.
func copyToClipboard(s string) error {
	return winapi.SetClipboardText(s)
}

// copyAndSay copies a value and reports the outcome, without printing the value.
//
// The balloon names what was copied rather than echoing it: the whole reason the
// api key is a menu verb instead of a menu label is that an open menu, and the
// toast confirming it, are both things that end up in screenshots.
func copyAndSay(a *app.App, what, value string) {
	if err := copyToClipboard(value); err != nil {
		a.Notify(what, i18n.T(a.Lang(), "task.failed", err), 2)
		return
	}
	a.Notify(what, i18n.T(a.Lang(), "copy.done", what), 0)
}

func init() {
	// The tray is a GUI program: a stray log line has nowhere to go, so a fatal
	// error from the standard logger is written to a file beside the config
	// rather than lost.
	log.SetFlags(log.Ltime)
	log.SetPrefix("wbtray: ")
}
