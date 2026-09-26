//go:build windows

package main

import (
	"fmt"
	"sync"
	"time"

	"wbtray/internal/app"
	"wbtray/internal/autostart"
	"wbtray/internal/config"
	"wbtray/internal/gateway"
	"wbtray/internal/status"
)

// The gateway controller: the application's view of the wb2api process.
//
// The tray is careful here about one distinction: a gateway it started and a
// gateway someone else started are both running gateways, but only the first is
// the tray's to stop. The panel's own HTTP API is the source of truth for
// whether it is *healthy*; this is the source of truth for whether it exists.
type gatewayController struct {
	mu  sync.Mutex
	cfg config.Config
	app *app.App
	// owned is the process this tray started, if any.
	owned *gateway.Process
	// found is the last process-list result, cached so a menu open does not walk
	// the process table.
	found gateway.Process
	at    time.Time
}

// newGateway builds the controller.
func newGateway(cfg config.Config) *gatewayController {
	return &gatewayController{cfg: cfg}
}

// gatewayAutoStartName is the registry value the gateway gets under Run.
const gatewayAutoStartName = autostart.GatewayValueName

// autostartEnabled reports whether the gateway's Run entry exists.
func autostartEnabled(name string) bool { return autostart.EnabledValue(name) }

// autostartEnable writes a Run entry that starts the executable in its own
// directory, which is what the gateway needs to find its configuration.
func autostartEnable(name, exe, dir string) error {
	// A Run entry is a command line, not a directory-aware launcher, so the
	// working directory is set with cmd's own /d switch rather than left to the
	// shell's default of C:WindowsSystem32.
	command := fmt.Sprintf(`cmd /c start "" /d "%s" "%s"`, dir, exe)
	return autostart.EnableCommand(name, command)
}

// autostartDisable removes a Run entry.
func autostartDisable(name string) error { return autostart.DisableValue(name) }

// PID reports the gateway process.
//
// A process the tray started is reported from its own handle, which is exact; a
// process it did not start is found by walking the process list, and that result
// is cached for a second so a burst of menu opens does not each walk the table.
func (g *gatewayController) PID() status.PIDInfo {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.owned != nil {
		if code, exited := g.owned.ExitCode(); exited {
			// The process this tray started has finished: forget it, so the next
			// reading reports whatever is running now.
			_ = code
			g.owned = nil
		} else {
			return status.PIDInfo{
				Found:   true,
				PID:     g.owned.PID,
				Exe:     g.owned.Exe,
				Started: true,
			}
		}
	}

	if time.Since(g.at) > time.Second {
		g.found = gateway.FindRunning()
		g.at = time.Now()
	}
	if !g.found.Found {
		return status.PIDInfo{}
	}
	return status.PIDInfo{Found: true, PID: g.found.PID, Exe: g.found.Exe}
}

// Start launches the gateway.
func (g *gatewayController) Start() (uint32, error) {
	g.mu.Lock()
	if g.owned != nil {
		g.mu.Unlock()
		return 0, fmt.Errorf("the gateway is already running")
	}
	exe := gateway.Find(g.cfg.Command)
	port := gateway.PortFromBaseURL(g.cfg.BaseURL)
	show := g.cfg.ShowConsole
	g.mu.Unlock()

	proc, err := gateway.Start(exe, port, show)
	if err != nil {
		return 0, err
	}
	go proc.Reap()

	g.mu.Lock()
	owned := proc
	g.owned = &owned
	// The process-list cache now describes the moment before this start, so it is
	// discarded rather than consulted.
	g.at = time.Time{}
	g.mu.Unlock()
	return proc.PID, nil
}

// AdoptProcess tells the controller about a gateway that was started outside it.
//
// The startup bootstrap starts the local copy through the install package, which
// knows nothing about this controller; without being told, the controller would
// find the process by walking the table and would then believe it belongs to
// somebody else — and the menu would offer "stop" with the wrong attribution.
func (g *gatewayController) AdoptProcess(pid uint32, exe string) {
	g.mu.Lock()
	g.owned = &gateway.Process{Found: true, Started: true, PID: pid, Exe: exe}
	g.at = time.Time{}
	g.mu.Unlock()
}

// Stop ends the gateway. A process the tray did not start is stopped all the
// same: an operator who asked for "stop" means it.
func (g *gatewayController) Stop() error {
	info := g.PID()
	if !info.Found {
		return nil
	}
	if err := gateway.Stop(info.PID); err != nil {
		return err
	}
	g.mu.Lock()
	g.owned = nil
	g.at = time.Time{}
	g.mu.Unlock()
	return nil
}

// Restart stops the gateway and starts it again.
func (g *gatewayController) Restart() (uint32, error) {
	if err := g.Stop(); err != nil {
		return 0, err
	}
	// The old process needs a moment to release the listening socket, or the new
	// one fails to bind and the operator sees a start error that is really a
	// timing artefact.
	time.Sleep(700 * time.Millisecond)
	return g.Start()
}

// HasConsole reports whether the gateway owns a console window.
func (g *gatewayController) HasConsole() bool {
	info := g.PID()
	if !info.Found {
		return false
	}
	return gateway.HasConsole(info.PID)
}

// SetConsole shows or hides the gateway's console window.
func (g *gatewayController) SetConsole(show bool) error {
	info := g.PID()
	if !info.Found {
		return fmt.Errorf("the gateway is not running")
	}
	if show {
		return gateway.ShowConsole(info.PID)
	}
	return gateway.HideConsole(info.PID)
}

// AutoStartEnabled reports whether the gateway starts with the user's session.
//
// Windows' own Run key is checked first, because that is where a gateway started
// by an installer or by the tray's earlier self would be registered. A gateway
// launched by a scheduled task instead is invisible to this check, which is why
// the menu row is a convenience rather than the only way to arrange it.
func (g *gatewayController) AutoStartEnabled() bool {
	return autostartEnabled(gatewayAutoStartName)
}

// SetAutoStart writes the gateway's autostart entry.
func (g *gatewayController) SetAutoStart(on bool) error {
	exe := gateway.Find(g.cfg.Command)
	if exe == "" {
		return fmt.Errorf("gateway executable not found")
	}
	if on {
		// The entry names the executable and its working directory, because the
		// gateway resolves config.json relative to the process's current
		// directory when it is started by the shell.
		return autostartEnable(gatewayAutoStartName, exe, gateway.WorkDir(exe))
	}
	return autostartDisable(gatewayAutoStartName)
}
