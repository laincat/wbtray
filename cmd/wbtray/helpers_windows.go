//go:build windows

package main

import (
	"context"
	"time"
	"unsafe"

	"wbtray/internal/app"
	"wbtray/internal/tray"
	"wbtray/internal/winapi"
)

// frontEnd is the application's handle on the running tray.
type frontEnd struct {
	icon *tray.Icon
	app  *app.App
}

// SetTooltip updates the icon's hover text.
func (f *frontEnd) SetTooltip(text string) { f.icon.SetTooltip(text) }

// RefreshWindows repaints any window that is showing live numbers.
func (f *frontEnd) RefreshWindows() { repaintChartWindow() }

// contextWithTimeout is a one-line context constructor, spelled out so the
// imports at each call site stay readable.
func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// messageBox shows the shell's own message box, which is the right size for an
// about panel and already follows the system's accessibility settings.
func messageBox(title, body string) {
	const mbOK = 0x00000000
	const mbIconInformation = 0x00000040
	winapi.ProcMessageBoxW.Call(0,
		uintptr(unsafe.Pointer(winapi.UTF16Ptr(body))),
		uintptr(unsafe.Pointer(winapi.UTF16Ptr(title))),
		mbOK|mbIconInformation)
}

// showWindow shows, hides or restores a window. The constants are the shell's
// SW_* values, named here so the call sites read as intent.
func showWindow(hwnd uintptr, cmd int) {
	winapi.ProcShowWindow.Call(hwnd, uintptr(cmd))
}

// setForeground brings a window to the front.
func setForeground(hwnd uintptr) {
	winapi.ProcSetForegroundWindow.Call(hwnd)
}

// loadCursor loads the standard arrow, which is the right cursor for a window
// whose contents are not editable.
func loadCursor() uintptr {
	const idcArrow = 32512
	h, _, _ := winapi.ProcLoadCursorW.Call(0, idcArrow)
	return h
}
