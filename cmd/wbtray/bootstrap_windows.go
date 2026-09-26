//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"wbtray/internal/app"
	"wbtray/internal/gateway"
	"wbtray/internal/i18n"
	"wbtray/internal/install"
)

// Bringing a gateway up, and keeping both projects current.
//
// This is the part of the front end that owns the process table, the file system
// and the network, which is exactly what the application package is written not
// to know about.

// lifecycle is the application's handle on the gateway's installation.
type lifecycle struct {
	app        *app.App
	layout     install.Layout
	rel        install.Release
	hasRelease bool
}

// layoutFor derives the installation layout, falling back to the directory the
// executable is in when it cannot be asked of the operating system.
func layoutFor() install.Layout {
	if l, err := install.LayoutFor(); err == nil {
		return l
	}
	if cwd, err := os.Getwd(); err == nil {
		return install.Layout{Root: cwd, Gateway: cwd + string(os.PathSeparator) + install.GatewayDirName}
	}
	return install.Layout{Root: ".", Gateway: install.GatewayDirName}
}

// Running reports a gateway started by anything at all.
//
// The local installation is checked first only in the sense that a process
// running from it is still "a gateway that is running": the answer to the
// question the bootstrap asks is the process table, not the file system.
func (l *lifecycle) Running() install.Running {
	if p := gateway.FindRunning(); p.Found {
		return install.Running{Found: true, PID: p.PID, Exe: p.Exe}
	}
	return install.Running{}
}

// Layout is where wbtray's own copy of the gateway lives.
func (l *lifecycle) Layout() install.Layout { return l.layout }

// Installed reports whether that copy is present.
func (l *lifecycle) Installed() bool { return l.layout.Present() }

// Install downloads and unpacks a gateway release into the layout.
func (l *lifecycle) Install(ctx context.Context, progress func(stage string)) (install.UpdateResult, error) {
	if progress != nil {
		progress("resolve")
	}
	client := install.New()
	rel, err := client.Latest(ctx, install.Gateway())
	if err != nil {
		return install.UpdateResult{}, err
	}
	l.rel, l.hasRelease = rel, true

	if err := l.layout.EnsureGatewayDir(); err != nil {
		return install.UpdateResult{}, err
	}
	result, err := client.InstallGateway(ctx, l.layout, rel, func(host string) {
		if progress != nil {
			progress("download:" + host)
		}
	}, nil)
	if err != nil {
		return result, err
	}
	// A release ships config.example.json; the gateway wants config.json. Its
	// own first run writes a working one, so nothing is copied — but the example
	// stays beside it as the reference the upstream README points at.
	return result, nil
}

// StartInstalled starts the local copy.
func (l *lifecycle) StartInstalled() (uint32, error) {
	exe := l.layout.GatewayExe()
	if _, err := os.Stat(exe); err != nil {
		return 0, fmt.Errorf("%s is not installed: %w", exe, err)
	}
	port := gateway.PortFromBaseURL(l.app.BaseURL())
	proc, err := gateway.Start(exe, port, l.app.ShowConsole())
	if err != nil {
		return 0, err
	}
	go proc.Reap()
	l.app.SetGatewayProcess(proc.PID, exe)
	// The gateway this tray started is also the one a later update replaces, so
	// the process controller is told about it rather than rediscovering it.
	l.app.Gateway().AdoptProcess(proc.PID, exe)
	return proc.PID, nil
}

// bootstrapGateway runs the startup order and waits for the gateway to answer.
//
// The wait is what stops the tray from reporting "offline" for the first seconds
// of its life: a gateway that has just been started is not yet one that can be
// asked anything.
func bootstrapGateway(a *app.App, lc *lifecycle) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	lang := a.Lang()
	report := func(text string) { a.Notify(i18n.T(lang, "app.name"), text, 0) }

	err := a.Bootstrap(ctx, lc, func(stage string) {
		switch {
		case stage == "resolve":
			report(i18n.T(lang, "gw.resolving"))
		case len(stage) > 9 && stage[:9] == "download:":
			report(i18n.T(lang, "gw.downloading", stage[9:]))
		}
	})
	if err != nil {
		a.Notify(i18n.T(lang, "gw.start_failed", err), "", 2)
		return
	}

	// Wait for the API, then take the first reading. A gateway that has just
	// been installed has never been asked anything, so this is also the moment
	// the tray learns whether there are any accounts.
	install.WaitForGateway(ctx, func(ctx context.Context) bool {
		return a.PingGateway(ctx)
	}, 90*time.Second)

	a.Refresh()
	if a.NeedsLogin() {
		// The one message a fresh install needs: the gateway is up and cannot
		// serve anything until an account is added through its own panel.
		a.Notify(i18n.T(lang, "gw.no_accounts_title"), a.T("gw.no_accounts_body", a.PanelURL()), 1)
	}
}

// startVersionChecks looks for newer releases of both projects.
//
// It runs in the background and on the tray's own interval: checking is cheap,
// but not so cheap that it belongs on the refresh path, which runs every few
// seconds and should stay a single HTTP call to the gateway.
func startVersionChecks(a *app.App, stop <-chan struct{}) {
	client := install.New()
	check := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		state := app.UpdateState{Checked: time.Now()}
		installed := a.GatewayVersion()

		if rel, err := client.Latest(ctx, install.Gateway()); err != nil {
			state.Err = err
		} else {
			state.Gateway = rel.Version
			state.GatewayUpdate = install.CompareVersions(rel.Version, installed) > 0
		}
		if rel, err := client.Latest(ctx, install.Tray()); err != nil {
			if state.Err == nil {
				state.Err = err
			}
		} else {
			state.Tray = rel.Version
			state.TrayUpdate = install.CompareVersions(rel.Version, version) > 0
		}
		a.SetUpdates(state)
	}

	// One check shortly after startup, then on a slow cycle: releases are made
	// in days, not minutes, and a tray that asked every few minutes would be
	// rude to two projects at once.
	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-stop:
			return
		case <-timer.C:
			check()
			timer.Reset(6 * time.Hour)
		}
	}
}

// configPathForGateway is where the gateway's own settings live, for the menu
// row that opens them.
func configPathForGateway(l install.Layout) string {
	if _, err := os.Stat(l.GatewayConfig()); err == nil {
		return l.GatewayConfig()
	}
	return l.GatewayExample()
}
