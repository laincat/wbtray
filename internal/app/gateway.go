package app

import (
	"context"
	"fmt"
	"time"

	"wbtray/internal/install"
	"wbtray/internal/i18n"
	"wbtray/internal/status"
)

// The gateway lifecycle, as the application sees it.
//
// The order at startup is: use a gateway that is already running, else start the
// one in wbtray's own directory, else download one and then start it. The
// application does not implement any of that; it asks the front end, which owns
// the process table and the file system, and reports what happened.

// GatewayLifecycle is what the application needs from the front end to bring a
// gateway up.
type GatewayLifecycle interface {
	// Running reports a gateway started by anyone, or nothing.
	Running() install.Running
	// Layout is where wbtray's own copy of the gateway lives.
	Layout() install.Layout
	// Installed reports whether that copy is present.
	Installed() bool
	// Install downloads and unpacks a gateway release.
	Install(ctx context.Context, progress func(stage string)) (install.UpdateResult, error)
	// StartInstalled starts the local copy, and returns its pid.
	StartInstalled() (uint32, error)
}

// UpdateState is what the tray knows about newer versions.
type UpdateState struct {
	// Checked is when the last check happened.
	Checked time.Time
	// Gateway and Tray are the newest versions found, empty when unknown.
	Gateway string
	Tray    string
	// GatewayUpdate is true when the installed gateway is older than the newest.
	GatewayUpdate bool
	// TrayUpdate is true when the running tray is older than the newest.
	TrayUpdate bool
	// Err is why the last check failed, if it did.
	Err error
}

// Bootstrap brings a gateway up, in the documented order.
//
// It runs once at startup and is deliberately blocking: the tray's whole purpose
// is to report on a gateway, and a tray that appeared before the gateway did
// would spend its first seconds saying "offline" about a machine where nothing
// is wrong.
func (a *App) Bootstrap(ctx context.Context, gw GatewayLifecycle, progress func(stage string)) error {
	step, err := install.Bootstrap(ctx,
		func() install.Running { return gw.Running() },
		func() (install.Layout, bool) { return gw.Layout(), gw.Installed() },
		func(ctx context.Context, _ install.Layout) error {
			_, err := gw.Install(ctx, progress)
			return err
		})
	if err != nil {
		return err
	}

	a.mu.Lock()
	a.bootstrapStep = step
	a.mu.Unlock()

	if step.Kind == install.UseRunning {
		// Already up and not ours: nothing to start, and nothing to say.
		return nil
	}
	pid, err := gw.StartInstalled()
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.startedPID = pid
	a.mu.Unlock()
	return nil
}

// NeedsLogin reports whether the gateway has no accounts yet, which is the state
// a fresh install is in and the one where the operator has to be told what to do
// next: the gateway is running, but it can serve nothing until an account is
// added through its own panel.
func (a *App) NeedsLogin() bool {
	a.mu.Lock()
	snap, reachable := a.snap, a.snap.Reachable
	a.mu.Unlock()
	return reachable && snap.Total == 0
}

// GatewayInstalled reports whether wbtray's own copy of the gateway is on disk.
//
// The application does not read the file system, so this is a value the front end
// sets at startup and after every install. It is what decides whether the menu
// offers "start" or "download and install".
func (a *App) GatewayInstalled() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.gatewayInstalled
}

// SetGatewayInstalled records whether the local gateway is present.
func (a *App) SetGatewayInstalled(v bool) {
	a.mu.Lock()
	a.gatewayInstalled = v
	a.mu.Unlock()
}

// BootstrapStep is which step of the startup order applied.
func (a *App) BootstrapStep() install.Step {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.bootstrapStep
}

// UpdateState reports what is known about newer versions.
func (a *App) Updates() UpdateState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.updates
}

// SetUpdates records the result of a version check.
func (a *App) SetUpdates(u UpdateState) {
	a.mu.Lock()
	a.updates = u
	a.mu.Unlock()
}

// GatewayVersion is the version of the gateway on disk, as recorded at install
// time.
func (a *App) GatewayVersion() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.gatewayVersion
}

// SetGatewayVersion records the installed gateway's version.
func (a *App) SetGatewayVersion(v string) {
	a.mu.Lock()
	a.gatewayVersion = v
	a.mu.Unlock()
}

// SetTrayVersion records the running build's version, for the version block.
func (a *App) SetTrayVersion(v string) {
	a.mu.Lock()
	a.trayVersion = v
	a.mu.Unlock()
}

// TrayVersion is the running build's version.
func (a *App) TrayVersion() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.trayVersion == "" {
		return "dev"
	}
	return a.trayVersion
}

// SetGatewayProcess records the gateway this process started, so the menu can
// tell the operator's own instance apart from wbtray's.
func (a *App) SetGatewayProcess(pid uint32, exe string) {
	a.mu.Lock()
	a.startedPID, a.startedExe = pid, exe
	a.mu.Unlock()
}

// GatewayStartedByUs is the process this tray started, or a zero pid.
func (a *App) GatewayStartedByUs() (uint32, string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.startedPID, a.startedExe
}

// PingGateway asks the gateway's own health endpoint whether it is up.
//
// It is deliberately the cheapest question the gateway can be asked: a gateway
// that has just been launched is reading its accounts and binding a port, and the
// only thing the tray wants to know before its first real reading is whether that
// has finished.
func (a *App) PingGateway(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return a.PanelClient().Ping(ctx)
}

// loginHint is the sentence that tells an operator what to do about a gateway
// with no accounts.
func (a *App) loginHint() string {
	return a.T(fmt.Sprintf("status.needs_login"))
}

// GatewayStatusLine is the one-line gateway state for the menu's header row.
func (a *App) GatewayStatusLine(pid status.PIDInfo) string {
	lang := a.Lang()
	switch {
	case pid.Found && a.NeedsLogin():
		return i18n.T(lang, "gw.running_no_accounts")
	case pid.Found:
		return i18n.T(lang, "status.running", pid.PID)
	default:
		return i18n.T(lang, "status.offline")
	}
}
