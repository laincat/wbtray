//go:build windows

package main

import (
	"context"
	"log"
	"time"

	"wbtray/internal/app"
	"wbtray/internal/tray"
	"wbtray/internal/ui"
)

// The tray panel's actions.
//
// They are a smaller set than the console's on purpose. The panel is opened for a
// glance and one decision; everything that needs a second look is a row that opens
// the console on the page that has it, because a panel that carried every verb would
// be the console in a smaller box.

func panelActions(a *app.App, lc *lifecycle, win *tray.Window, panel *tray.Panel) func(ui.TrayAction, string) {
	return func(action ui.TrayAction, arg string) {
		switch action {
		case ui.TrayOpen:
			// The console is a window of this program's, and the page is chosen before
			// it is shown so it appears on the right one rather than switching after.
			if win != nil {
				win.SetTab(ui.Tab(arg))
				win.Show()
				return
			}
			if err := openURL(a.PanelClient().PanelURL()); err != nil {
				log.Printf("panel: %v", err)
			}

		case ui.TrayTogglePause:
			a.SetPaused(!a.Paused())
			panel.Invalidate()

		case ui.TrayToggleAuto:
			if err := a.ToggleTrayAutoStart(); err != nil {
				a.Notify("wbtray", err.Error(), 2)
			}
			panel.Invalidate()

		case ui.TrayQuit:
			a.Quit()
		}
	}
}

// panelState is the small state the panel owns: the two switches.
func panelState(a *app.App, lc *lifecycle) func() (paused, auto, installed bool) {
	return func() (bool, bool, bool) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = ctx
		return a.Paused(), a.TrayAutoStart(), lc.layout.Present()
	}
}
