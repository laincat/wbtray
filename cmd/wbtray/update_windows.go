//go:build windows

package main

import (
	"context"
	"os"
	"time"

	"wbtray/internal/app"
	"wbtray/internal/gateway"
	"wbtray/internal/i18n"
	"wbtray/internal/install"
)

// Running an update, for either project.
//
// The two differ in what has to be stopped. The gateway's executable is locked
// while it runs, so an update stops it, replaces the directory and starts it
// again. wbtray's own executable is locked too, but Windows permits it to be
// renamed, so the new build can be put in place and started while this process is
// still alive — then this process exits, leaving the new one behind.

// updateGateway downloads and installs the newest gateway, then restarts it.
//
// Stopping first is required rather than tidy: the running executable's file is
// locked, and the directory swap would fail on it.
func updateGateway(a *app.App, lc *lifecycle) {
	lang := a.Lang()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	report := func(text string) { a.Notify(i18n.T(lang, "app.name"), text, 0) }
	report(i18n.T(lang, "gw.resolving"))

	client := install.New()
	rel, err := client.Latest(ctx, install.Gateway())
	if err != nil {
		a.Notify(i18n.T(lang, "menu.updates"), i18n.T(lang, "update.failed", err), 2)
		return
	}

	// Whatever is running has to stop before its files can be replaced, whichever
	// of the two ways it got there.
	if info := a.Gateway().PID(); info.Found {
		if err := gateway.Stop(info.PID); err != nil {
			a.Notify(i18n.T(lang, "menu.updates"), i18n.T(lang, "update.failed", err), 2)
			return
		}
		install.WaitForExit()
	}

	result, err := client.InstallGateway(ctx, lc.layout, rel, func(host string) {
		report(i18n.T(lang, "gw.downloading", host))
	}, nil)
	if err != nil {
		// The gateway is stopped at this point, so it is started again from
		// whatever is on disk: leaving the machine with no gateway because an
		// update failed would be a worse outcome than the failed update.
		_, _ = lc.StartInstalled()
		a.Notify(i18n.T(lang, "menu.updates"), i18n.T(lang, "update.failed", err), 2)
		return
	}
	a.SetGatewayVersion(result.Version)
	a.SetGatewayInstalled(true)

	pid, err := lc.StartInstalled()
	if err != nil {
		a.Notify(i18n.T(lang, "menu.updates"), i18n.T(lang, "update.failed", err), 2)
		return
	}
	a.Notify(i18n.T(lang, "menu.updates"), i18n.T(lang, "update.done", result.Version), 0)

	// Wait for it to answer before the next reading, or the tray would report
	// "offline" for a gateway that is simply still starting.
	install.WaitForGateway(ctx, func(ctx context.Context) bool {
		return a.PingGateway(ctx)
	}, 60*time.Second)
	_ = pid
	a.Refresh()
}

// updateTray downloads the newest wbtray, puts it in place and restarts into it.
//
// It returns only on failure. On success it starts the new build and asks the
// caller to exit, because this process is now running from a renamed image and
// anything it did afterwards would be the old version.
func updateTray(a *app.App, lc *lifecycle, quit func()) {
	lang := a.Lang()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	report := func(text string) { a.Notify(i18n.T(lang, "app.name"), text, 0) }
	report(i18n.T(lang, "gw.resolving"))

	client := install.New()
	rel, err := client.Latest(ctx, install.Tray())
	if err != nil {
		a.Notify(i18n.T(lang, "menu.updates"), i18n.T(lang, "update.failed", err), 2)
		return
	}
	staged, err := client.StageTray(ctx, lc.layout, rel, func(host string) {
		report(i18n.T(lang, "gw.downloading", host))
	}, nil)
	if err != nil {
		a.Notify(i18n.T(lang, "menu.updates"), i18n.T(lang, "update.failed", err), 2)
		return
	}
	if err := install.ApplyTray(lc.layout, staged); err != nil {
		a.Notify(i18n.T(lang, "menu.updates"), i18n.T(lang, "update.failed", err), 2)
		return
	}
	a.Notify(i18n.T(lang, "menu.updates"), i18n.T(lang, "update.restarting"), 0)
	// A moment for the balloon to appear before this process goes: the new build
	// is already running, so nothing is lost but the message would be.
	time.Sleep(1200 * time.Millisecond)
	quit()
}

// installGatewayOnDemand brings a gateway in when none is installed, which is
// what the menu offers when the startup bootstrap could not find or make one.
func installGatewayOnDemand(a *app.App, lc *lifecycle) {
	lang := a.Lang()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	report := func(text string) { a.Notify(i18n.T(lang, "app.name"), text, 0) }
	if _, err := os.Stat(lc.layout.GatewayExe()); err != nil {
		report(i18n.T(lang, "gw.resolving"))
	}
	if _, err := lc.Install(ctx, func(stage string) {
		switch {
		case stage == "resolve":
			report(i18n.T(lang, "gw.resolving"))
		case len(stage) > 9 && stage[:9] == "download:":
			report(i18n.T(lang, "gw.downloading", stage[9:]))
		}
	}); err != nil {
		a.Notify(i18n.T(lang, "menu.install_gateway"), i18n.T(lang, "update.failed", err), 2)
		return
	}
	a.SetGatewayVersion(lc.layout.InstalledGatewayVersion())
	a.SetGatewayInstalled(true)
	if _, err := lc.StartInstalled(); err != nil {
		a.Notify(i18n.T(lang, "menu.install_gateway"), i18n.T(lang, "update.failed", err), 2)
		return
	}
	a.Notify(i18n.T(lang, "menu.install_gateway"), i18n.T(lang, "update.done", lc.layout.InstalledGatewayVersion()), 0)
	a.Refresh()
}

// checkUpdatesNow runs a version check on demand.
func checkUpdatesNow(a *app.App) {
	client := install.New()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	state := app.UpdateState{Checked: time.Now()}
	if rel, err := client.Latest(ctx, install.Gateway()); err != nil {
		state.Err = err
	} else {
		state.Gateway = rel.Version
		state.GatewayUpdate = install.CompareVersions(rel.Version, a.GatewayVersion()) > 0
	}
	if rel, err := client.Latest(ctx, install.Tray()); err != nil {
		if state.Err == nil {
			state.Err = err
		}
	} else {
		state.Tray = rel.Version
		state.TrayUpdate = install.CompareVersions(rel.Version, a.TrayVersion()) > 0
	}
	a.SetUpdates(state)

	lang := a.Lang()
	switch {
	case state.Err != nil:
		a.Notify(i18n.T(lang, "menu.updates"), i18n.T(lang, "update.failed", state.Err), 1)
	case state.GatewayUpdate || state.TrayUpdate:
		a.Notify(i18n.T(lang, "menu.updates"), i18n.T(lang, "menu.update_available",
			state.Gateway, state.Tray), 0)
	default:
		a.Notify(i18n.T(lang, "menu.updates"), i18n.T(lang, "menu.no_update"), 0)
	}
}
