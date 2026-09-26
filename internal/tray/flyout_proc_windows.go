//go:build windows

package tray

import (
	"time"
	"unsafe"

	"wbtray/internal/traymenu"
	"wbtray/internal/winapi"
)

// Input handling for the drawn menu.

// Additional messages the flyout handles.
const (
	wmActivate    = 0x0006
	wmKillFocus   = 0x0008
	wmLButtonDown = 0x0201
	wmMouseWheel  = 0x020A
	wmGetDlgCode  = 0x0087
	wmNcDestroy   = 0x0082
	wmSetFocus    = 0x0007

	vkEscape = 0x1B
	vkReturn = 0x0D
	vkUp     = 0x26
	vkDown   = 0x28
	vkHome   = 0x24
	vkEnd    = 0x23
	vkLButton = 0x01

	waInactive = 0
)

// dismissalTimerID drives the "clicked outside" test.
const (
	dismissTimerID = 3
	// Faster than a person can click and release, so a transition cannot be
	// missed between two polls. It is a cheap check — two syscalls — and it runs
	// only while the menu is open.
	dismissTimerMs = 25
)

// dismissGrace is how long the menu is left alone after being shown, whatever
// the foreground window says. It covers the moment between the menu appearing
// and the shell settling who owns the foreground, during which a dismiss check
// would otherwise fire on a technicality and close a menu the operator is
// looking at.
const dismissGrace = 400 * time.Millisecond

// hoverCallback is set by the application so a row can preview itself: hovering
// a style row is what paints it onto the live tray icon.
var hoverCallback func(traymenu.Item)

// SetHoverCallback installs the function called when the hovered row changes.
func SetHoverCallback(fn func(traymenu.Item)) { hoverCallback = fn }

// wndProc handles the flyout window's messages.
func (f *flyout) wndProc(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
	switch msg {
	case wmMouseMove:
		f.onMouseMove(hwnd, lparam)
		return 0

	case wmMouseLeave:
		f.stopTracking()
		f.setHover(hwnd, -1)
		return 0

	case wmLButtonDown:
		f.mu.Lock()
		f.pressed = f.hover
		f.mu.Unlock()
		winapi.ProcSetCapture.Call(hwnd)
		return 0

	case wmLButtonUp:
		winapi.ProcReleaseCapture.Call()
		f.onClick(hwnd)
		return 0

	case wmMouseWheel:
		f.onWheel(hwnd, int32(highWord(wparam)))
		return 0

	case wmKeyDown:
		f.onKey(hwnd, uint32(wparam))
		return 0

	case wmGetDlgCode:
		// The window wants the arrow keys and Tab rather than letting the shell
		// use them to move focus out of the menu.
		const dlgcWantAllKeys = 0x0004
		return dlgcWantAllKeys

	case wmTimer:
		if wparam == dismissTimerID {
			f.checkDismiss(hwnd)
		}
		return 0

	case wmActivate:
		// Only a menu that actually held the foreground can lose it. Windows does
		// not always grant activation to a window shown by a background process —
		// the shell refuses when the caller is not the foreground process, which
		// is the normal state for a tray icon — and a menu that treated "never
		// activated" as "just lost activation" would hide itself the instant it
		// appeared. That is precisely how the first version failed: the window
		// was created, positioned and then hidden again before a frame could be
		// seen.
		if lowWord(wparam) == waInactive && f.tookFocus() {
			f.leave()
		}
		return 0

	case wmKillFocus:
		if f.tookFocus() {
			f.leave()
		}
		return 0

	case wmSetFocus:
		// The menu has the focus, so from here on losing it means the operator
		// moved on rather than that the shell never granted it.
		f.mu.Lock()
		f.hadFocus = true
		f.mu.Unlock()
		return 0

	case wmPaint:
		f.paint(hwnd)
		return 0

	case wmEraseBkgnd:
		// Painting covers every pixel, so erasing first would only flicker.
		return 1

	case wmLButtonUp + 1, wmRButtonUp:
		f.leave()
		return 0
	}
	ret, _, _ := winapi.ProcDefWindowProcW.Call(hwnd, uintptr(msg), wparam, lparam)
	return ret
}

// leave hides the menu and tells the application, which is where a preview began
// on hover is rolled back.
func (f *flyout) leave() {
	f.mu.Lock()
	wasOpen := f.open
	f.mu.Unlock()
	if !wasOpen {
		return
	}
	f.close()
	if hoverCallback != nil {
		hoverCallback(traymenu.Item{})
	}
}

// onMouseMove tracks the cursor and updates the hovered row.
func (f *flyout) onMouseMove(hwnd, lparam uintptr) {
	x, y := mousePoint(lparam)
	f.trackMouse(hwnd)
	f.mu.Lock()
	scale := f.scale
	f.mu.Unlock()
	// The window's coordinates are physical pixels; the layout is logical.
	idx := f.hitRow(float64(x)/scale, float64(y)/scale)
	f.setHover(hwnd, idx)
}

// onClick runs the hovered row's action.
func (f *flyout) onClick(hwnd uintptr) {
	f.mu.Lock()
	idx := f.hover
	pressed := f.pressed
	rows := f.rows
	f.pressed = -1
	f.mu.Unlock()
	if idx < 0 || idx != pressed || idx >= len(rows) {
		return
	}
	f.activate(hwnd, idx)
}

// activate performs a row's action and decides whether the menu stays open.
//
// Toggles and the style gallery keep it open, because a settings panel that
// closes after every click makes changing two things twice the work. Commands
// close it, because the next thing the operator wants is to see the result.
func (f *flyout) activate(hwnd uintptr, idx int) {
	f.mu.Lock()
	rows := f.rows
	if idx >= len(rows) {
		f.mu.Unlock()
		return
	}
	item := rows[idx].item
	f.mu.Unlock()

	switch item.Kind {
	case traymenu.SubmenuRow:
		f.mu.Lock()
		if f.subOwner == idx {
			f.subOwner = -1
		} else {
			f.subOwner = idx
		}
		f.mu.Unlock()
		f.relayout(hwnd)
		return
	case traymenu.CheckRow, traymenu.RadioRow, traymenu.StyleRow:
		if f.icon != nil && f.icon.cb.Select != nil {
			f.icon.cb.Select(traymenu.Event{ID: item.ID})
		}
		f.relayout(hwnd)
		return
	default:
		if item.Disabled {
			return
		}
		f.leave()
		if f.icon != nil && f.icon.cb.Select != nil {
			f.icon.cb.Select(traymenu.Event{ID: item.ID})
		}
	}
}

// relayout re-reads the model and resizes the window, which is what makes a
// toggle show its new state without reopening the menu.
func (f *flyout) relayout(hwnd uintptr) {
	f.mu.Lock()
	icon := f.icon
	f.mu.Unlock()
	if icon == nil {
		return
	}
	f.layout(icon.menuItems())

	f.mu.Lock()
	w, h, scale := f.width, f.height, f.scale
	rows := f.rows
	subOwner := f.subOwner
	f.mu.Unlock()
	// The expansion state lives in the flyout, but the painter reads it from the
	// item, so it is copied across here rather than in two places.
	for i := range rows {
		if rows[i].item.Kind == traymenu.SubmenuRow {
			rows[i].item = rows[i].item.Expanded(i == subOwner)
		}
	}
	f.mu.Lock()
	f.rows = rows
	f.mu.Unlock()

	ww := int(float64(w) * scale)
	wh := int(float64(h) * scale)
	winapi.ProcSetWindowPos.Call(hwnd, 0, 0, 0, uintptr(ww), uintptr(wh),
		swpNoMove|swpNoActivate)
	winapi.ProcInvalidateRect.Call(hwnd, 0, 1)
}

// onWheel scrolls the list when it is taller than the screen allows.
func (f *flyout) onWheel(hwnd uintptr, delta int32) {
	f.mu.Lock()
	total := 0.0
	if n := len(f.rows); n > 0 {
		last := f.rows[n-1]
		total = last.rect.Y + last.rect.H
	}
	view := f.viewHeight()
	f.scroll -= float64(delta) / 120 * foRowH
	if f.scroll < 0 {
		f.scroll = 0
	}
	if max := total - view + foShadowPad*2; max > 0 && f.scroll > max {
		f.scroll = max
	}
	if total+foShadowPad*2 <= view {
		f.scroll = 0
	}
	f.mu.Unlock()
	winapi.ProcInvalidateRect.Call(hwnd, 0, 1)
}

// viewHeight is how much of the list fits on screen.
func (f *flyout) viewHeight() float64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return float64(f.height)
}

// onKey handles the keyboard, which the shell forwards because the menu took
// the foreground.
func (f *flyout) onKey(hwnd uintptr, vk uint32) {
	f.mu.Lock()
	rows := f.rows
	hover := f.hover
	f.mu.Unlock()
	switch vk {
	case vkEscape:
		f.leave()
	case vkUp:
		f.setHover(hwnd, prevSelectable(rows, hover))
	case vkDown:
		f.setHover(hwnd, nextSelectable(rows, hover))
	case vkHome:
		f.setHover(hwnd, nextSelectable(rows, -1))
	case vkEnd:
		f.setHover(hwnd, prevSelectable(rows, len(rows)))
	case vkReturn:
		if hover >= 0 {
			f.activate(hwnd, hover)
		}
	}
}

// prevSelectable walks backwards to the previous row that can be hovered.
func prevSelectable(rows []row, from int) int {
	for i := from - 1; i >= 0; i-- {
		if rows[i].item.Kind != traymenu.SeparatorRow {
			return i
		}
	}
	return from
}

// nextSelectable walks forwards to the next row that can be hovered.
func nextSelectable(rows []row, from int) int {
	for i := from + 1; i < len(rows); i++ {
		if rows[i].item.Kind != traymenu.SeparatorRow {
			return i
		}
	}
	return from
}

// setHover records the hovered row, notifies the application and repaints only
// when something actually changed.
func (f *flyout) setHover(hwnd uintptr, idx int) {
	f.mu.Lock()
	if f.hover == idx {
		f.mu.Unlock()
		return
	}
	f.hover = idx
	var item traymenu.Item
	rows := f.rows
	scaleReported := idx
	f.mu.Unlock()
	_ = scaleReported
	if idx >= 0 && idx < len(rows) {
		item = rows[idx].item
	}
	if hoverCallback != nil {
		hoverCallback(item)
	}
	winapi.ProcInvalidateRect.Call(hwnd, 0, 0)
}

// trackMouse asks Windows for the mouse-leave message, which is what clears the
// highlight at the edge of the window.
func (f *flyout) trackMouse(hwnd uintptr) {
	f.mu.Lock()
	tracking := f.trackingMouse
	f.trackingMouse = true
	f.mu.Unlock()
	if tracking {
		return
	}
	const (
		tmeLeave     = 0x00000002
		hoverDefault = 0xFFFFFFFF
	)
	tme := winapi.TrackMouseEventStruct{
		CbSize:      uint32(unsafe.Sizeof(winapi.TrackMouseEventStruct{})),
		DwFlags:     tmeLeave,
		HwndTrack:   hwnd,
		DwHoverTime: hoverDefault,
	}
	winapi.ProcTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme)))
}

func (f *flyout) stopTracking() {
	f.mu.Lock()
	f.trackingMouse = false
	f.mu.Unlock()
}

// checkDismiss closes the menu when the user clicked somewhere else. The
// window is created without activation so that the shell's own handling of
// "click outside" does not apply, and this is the replacement: a click outside
// the menu's rectangle, or a button press anywhere with the cursor away from it.
func (f *flyout) checkDismiss(hwnd uintptr) {
	var rect winapi.Rect
	winapi.ProcGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rect)))
	var pt winapi.Point
	winapi.ProcGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	inside := pt.X >= rect.Left && pt.X < rect.Right && pt.Y >= rect.Top && pt.Y < rect.Bottom

	// A click is a transition, not a state, and this is the whole reason the
	// check does anything beyond reading the keyboard: a quick click is already
	// released by the time a sixty-millisecond timer next asks, so sampling
	// "is the button down" misses almost every click a person makes. The
	// previous version sampled, and the result was a menu that ignored clicks
	// outside it and stayed open until the operator clicked somewhere that took
	// the focus away.
	down := isKeyDown(vkLButton)
	f.mu.Lock()
	wasDown := f.buttonWasDown
	f.buttonWasDown = down
	f.mu.Unlock()
	if down && !wasDown {
		if inside {
			// A click inside is the menu's own business: the row under the
			// cursor handles it, or nothing does.
			return
		}
		f.leave()
		return
	}

	// A window that is no longer foreground and has no button held means the
	// user moved on: another application took focus.
	//
	// There is a grace period, and it is not decoration. The shell can take a
	// moment to settle the foreground after a menu is shown — and a menu that
	// closed itself during that moment would look like a click that did nothing,
	// which is exactly how the first version failed. Nothing here can be trusted
	// until the window has been on screen long enough to be seen.
	f.mu.Lock()
	showing, hadFocus := f.shownAt, f.hadFocus
	f.mu.Unlock()
	if !hadFocus {
		// The menu never held the foreground, so whatever holds it now is not a
		// change the operator made. The click-outside test below is the one that
		// applies in that case.
		return
	}
	if !showing.IsZero() && time.Since(showing) < dismissGrace {
		return
	}
	fg, _, _ := winapi.ProcGetForegroundWindow.Call()
	if fg != hwnd && fg != f.ownerWindow() {
		f.leave()
	}
}

// isKeyDown reports whether a virtual key is currently held.
func isKeyDown(vk uintptr) bool {
	state, _, _ := winapi.ProcGetAsyncKeyState.Call(vk)
	return state&0x8000 != 0
}

// ownerWindow is the tray window, which is a legitimate foreground owner while
// the menu is open.
func (f *flyout) ownerWindow() uintptr {
	if f.icon == nil {
		return 0
	}
	f.icon.mu.Lock()
	defer f.icon.mu.Unlock()
	return f.icon.hwnd
}

// mousePoint unpacks a mouse message's coordinates, which are signed.
func mousePoint(lparam uintptr) (int32, int32) {
	x := int32(lparam & 0xffff)
	y := int32((lparam >> 16) & 0xffff)
	return signExtend16(x), signExtend16(y)
}

func signExtend16(v int32) int32 {
	if v >= 0x8000 {
		v -= 0x10000
	}
	return v
}

// highWord extracts the high 16 bits of a message parameter.
func highWord(v uintptr) uint16 { return uint16(v >> 16) }

// lowWord extracts the low 16 bits of a message parameter.
func lowWord(v uintptr) uint32 { return uint32(v & 0xffff) }
