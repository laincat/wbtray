//go:build windows

package tray

import (
	"unsafe"

	"wbtray/internal/traymenu"
	"wbtray/internal/winapi"
)

// The tray window's message handling.

// wndProc handles the tray window's messages.
func (t *Icon) wndProc(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
	switch msg {
	case callbackMsg:
		// This shell uses the version-4 notification-icon protocol, which packs
		// the icon's id into the high word of lParam and the mouse message into
		// the low word. Reading the whole of lParam as the message — which the
		// first version of this did — matches nothing at all, because the value
		// being compared is 0x0001xxxx rather than 0x0205, and the result is a
		// tray icon that silently ignores every click.
		switch winapi.LowWord(lparam) {
		case ninSelect, wmLButtonUp:
			t.handleIconClick()
		case wmLButtonDBL:
			if t.cb.DoubleClick != nil {
				go t.cb.DoubleClick()
			}
		case ninKeySelect, wmRButtonUp, wmContextMenu:
			// NIN_KEYSELECT is the keyboard asking for the menu, and
			// WM_CONTEXTMENU is what a version-4 icon receives for a right
			// click. Both lead to the same place as a right-button message.
			t.showMenuAtCursor()
		}
		return 0

	case wmCommand:
		if t.cb.Select != nil {
			t.cb.Select(traymenu.Event{ID: winapi.LowWord(wparam), Native: true})
		}
		return 0

	case wmTimer:
		switch wparam {
		case tipTimerID:
			if t.cb.Tick != nil {
				go t.cb.Tick()
			}
		}
		return 0

	case wmSettingChange, wmDisplayChange:
		// The taskbar was resized or a display was added or removed: the icon
		// bitmap is the right size for the new DPI only after this.
		t.refreshIconBitmap()
		return 0

	case wmClose:
		winapi.ProcDestroyWindow.Call(hwnd)
		return 0

	case wmDestroy:
		t.mu.Lock()
		hicon := t.hicon
		t.mu.Unlock()
		nid := t.newNID(hwnd, hicon, 0)
		winapi.ProcShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(nid)))
		if hicon != 0 {
			winapi.ProcDestroyIcon.Call(hicon)
		}
		winapi.ProcPostQuitMessage.Call(0)
		return 0
	}
	ret, _, _ := winapi.ProcDefWindowProcW.Call(hwnd, uintptr(msg), wparam, lparam)
	return ret
}

// handleIconClick distinguishes a single click from the first half of a double
// click, so opening the panel on the first click of a double click does not
// happen twice.
func (t *Icon) handleIconClick() {
	now, _, _ := winapi.ProcGetTickCount.Call()
	interval, _, _ := winapi.ProcGetDoubleClickTime.Call()
	t.mu.Lock()
	since := uint32(now) - t.lastClickTime
	t.lastClickTime = uint32(now)
	t.mu.Unlock()
	if since != 0 && since < uint32(interval) {
		return // the double-click message will handle it
	}
	if t.cb.Click != nil {
		go t.cb.Click()
	}
}

// refreshIconBitmap re-renders the icon, which is how a DPI change reaches the
// shell.
func (t *Icon) refreshIconBitmap() {
	if c := t.iconCanvas(); c != nil {
		t.SetIcon(c)
	}
}

// showMenuAtCursor opens whichever menu the operator selected.
func (t *Icon) showMenuAtCursor() {
	var pt winapi.Point
	winapi.ProcGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	t.mu.Lock()
	style := t.menuStyle
	t.mu.Unlock()
	if style == MenuNative {
		t.showNativeMenu(int(pt.X), int(pt.Y))
		return
	}
	t.showFlyout()
}
