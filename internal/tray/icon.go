//go:build windows

// Package tray is the notification-area presence of wbtray: the icon, the menus
// that hang off it, and the windows that show what the gateway is doing.
//
// The icon is the primary readout. Its colour says whether the gateway is well,
// and its shape carries whichever metric the operator chose, so the answer to
// "is anything wrong" is available without opening anything.
package tray

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"wbtray/internal/raster"
	"wbtray/internal/traymenu"
	"wbtray/internal/winapi"
)

// Window messages and tray-protocol constants.
const (
	wmDestroy       = 0x0002
	wmSize          = 0x0005
	wmClose         = 0x0010
	wmPaint         = 0x000f
	wmEraseBkgnd    = 0x0014
	wmCommand       = 0x0111
	wmTimer         = 0x0113
	wmDrawItem      = 0x002B
	wmMeasureItem   = 0x002C
	wmMenuSelect    = 0x011F
	wmLButtonUp     = 0x0202
	wmLButtonDBL    = 0x0203
	wmRButtonUp     = 0x0205
	wmMouseMove     = 0x0200
	wmMouseLeave    = 0x02A3
	wmMouseHover    = 0x02A1
	wmKeyDown       = 0x0100
	wmSysKeyDown    = 0x0104
	wmSettingChange = 0x001A
	wmDisplayChange = 0x007E
	wmDpiChanged    = 0x02E0
	wmContextMenu   = 0x007B

	callbackMsg = 0x8000 + 1 // WM_APP + 1
	trayID      = 1

	nimAdd        = 0
	nimModify     = 1
	nimDelete     = 2
	nimSetVersion = 4

	nifMessage = 0x01
	nifIcon    = 0x02
	nifTip     = 0x04
	nifInfo    = 0x10
	nifShowTip = 0x80

	niifInfo    = 0x01
	niifWarning = 0x02
	niifError   = 0x03

	// The v4 icon carries the guide text the shell adds to the hover tooltip.
	notifyIconVersion4 = 4

	// The notifications the shell sends a version-4 icon instead of raw mouse
	// messages. NIN_SELECT is a left click and NIN_KEYSELECT is the keyboard's
	// equivalent; both arrive as LOWORD(lParam), as the mouse messages do.
	ninSelect    = 0x0400 // WM_USER + 0
	ninKeySelect = 0x0401 // WM_USER + 1

	mfString     = 0x0000
	mfSeparator  = 0x0800
	mfChecked    = 0x0008
	mfGrayed     = 0x0001
	mfDisabled   = 0x0002
	mfDefault    = 0x1000
	mfPopup      = 0x0010
	mfOwnerDraw  = 0x0100
	mfBitmap     = 0x0004
	tpmRightBtn  = 0x0002
	tpmReturnCmd = 0x0100

	idiApp    = 32512
	csHRedraw = 0x0002
	csVRedraw = 0x0001
	csDblClks = 0x0008

	// Timer ids and periods.
	tipTimerID  = 1
	tipTimerMs  = 1000
	animTimerID = 2
	animTimerMs = 80
)

// Callbacks are the actions the tray invokes.
type Callbacks struct {
	// Menu is consulted on every right-click, so toggles always reflect live
	// state rather than a snapshot taken at startup.
	Menu func() []traymenu.Item
	// Select handles a click on a menu row.
	Select func(traymenu.Event)
	// Click handles a click on the icon itself.
	Click func()
	// DoubleClick handles a double click.
	DoubleClick func()
	// Tick runs on the tooltip refresh timer.
	Tick func()
	// IconView renders the tray icon at a given size.
	IconView func(size int) *raster.Canvas
	// StylePreview renders one style at a requested size, so the menu's style
	// gallery draws the icon it is offering rather than rescaling a smaller one.
	StylePreview func(styleIndex, size int) *raster.Canvas
}

// Icon is a running tray presence.
type Icon struct {
	cb    Callbacks
	title string

	mu    sync.Mutex
	hwnd  uintptr
	hicon uintptr
	tip   string
	size  int

	// wndProcRef pins the callback for the process lifetime: if the Go closure
	// were collected, Windows would call freed memory.
	wndProcRef uintptr
	// menuPainter is installed while a menu is open, because that is where
	// WM_DRAWITEM arrives and the rows it has to paint live.
	menuPainter *statusPainter
	// hint is the tip that explains whichever row the pointer is on. It is created
	// with the icon and hidden between menus, because a tray menu opens and closes
	// all day and a window per open would leak a handle per right-click.
	hint *hintWindow
	// menuItemsSnapshot is the model the open menu was built from, so a selection can
	// be turned back into the sentence that belongs to it.
	menuItemsSnapshot []traymenu.Item
	// hintX and hintY are where the open menu was placed, so the tip can be put
	// against it.
	hintX, hintY int
	// lastClickTime implements the double-click test for the icon.
	lastClickTime uint32
}

// New builds a tray icon bound to the given callbacks.
func New(title string, cb Callbacks) *Icon {
	return &Icon{cb: cb, title: title, size: 16}
}

// SetCallbacks installs the callback table after construction, which is what
// breaks the cycle between the icon and the application that owns it.
func (t *Icon) SetCallbacks(cb Callbacks) {
	t.mu.Lock()
	t.cb = cb
	t.mu.Unlock()
}

// menuItems asks the application for the current menu model. It is consulted on
// every open and after every toggle, so what it returns is always live.
func (t *Icon) menuItems() []traymenu.Item {
	if t.cb.Menu == nil {
		return nil
	}
	return t.cb.Menu()
}

// SetTooltip updates the hover text. Safe to call from any goroutine.
func (t *Icon) SetTooltip(text string) {
	t.mu.Lock()
	t.tip = text
	hwnd, hicon := t.hwnd, t.hicon
	t.mu.Unlock()
	if hwnd == 0 {
		return
	}
	nid := t.newNID(hwnd, hicon, nifTip)
	winapi.CopyUTF16(nid.SzTip[:], text)
	winapi.ProcShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(nid)))
}

// SetIcon swaps the bitmap the shell shows.
func (t *Icon) SetIcon(c *raster.Canvas) {
	if c == nil {
		return
	}
	t.mu.Lock()
	hwnd := t.hwnd
	t.mu.Unlock()
	if hwnd == 0 {
		return
	}
	hicon := iconFromCanvas(c)
	if hicon == 0 {
		return
	}
	t.mu.Lock()
	old := t.hicon
	t.hicon = hicon
	t.mu.Unlock()

	nid := t.newNID(hwnd, hicon, nifIcon)
	winapi.ProcShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(nid)))
	// The previous icon is ours, so releasing it after the swap avoids a leak
	// without ever leaving the shell pointing at freed memory.
	if old != 0 && old != hicon {
		winapi.ProcDestroyIcon.Call(old)
	}
}

// Notify raises a balloon notification. level is 0 for information, 1 for a
// warning and 2 for an error, which is what picks the shell's own icon.
func (t *Icon) Notify(title, text string, level int) {
	t.mu.Lock()
	hwnd, hicon := t.hwnd, t.hicon
	t.mu.Unlock()
	if hwnd == 0 {
		return
	}
	nid := t.newNID(hwnd, hicon, nifInfo)
	winapi.CopyUTF16(nid.SzInfoTitle[:], title)
	winapi.CopyUTF16(nid.SzInfo[:], text)
	nid.DwInfoFlags = uint32(niifInfo + level)
	winapi.ProcShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(nid)))
}

// Quit asks the message loop to exit.
func (t *Icon) Quit() {
	t.mu.Lock()
	hwnd := t.hwnd
	t.mu.Unlock()
	if hwnd != 0 {
		winapi.ProcPostMessageW.Call(hwnd, wmClose, 0, 0)
	}
}

func (t *Icon) newNID(hwnd, hicon uintptr, flags uint32) *winapi.NotifyIconData {
	return &winapi.NotifyIconData{
		CbSize:           uint32(unsafe.Sizeof(winapi.NotifyIconData{})),
		Hwnd:             hwnd,
		UID:              trayID,
		UFlags:           flags,
		UCallbackMessage: callbackMsg,
		HIcon:            hicon,
	}
}

// Run creates the icon and pumps messages until Quit. It must be called on a
// locked OS thread, which is what main does.
func (t *Icon) Run() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hInst, _, _ := winapi.ProcGetModuleHandleW.Call(0)
	className := winapi.UTF16Ptr("wbtrayTrayWnd")

	t.wndProcRef = syscall.NewCallback(t.wndProc)
	wc := winapi.WndClassExW{
		CbSize:        uint32(unsafe.Sizeof(winapi.WndClassExW{})),
		Style:         csHRedraw | csVRedraw,
		LpfnWndProc:   t.wndProcRef,
		HInstance:     hInst,
		LpszClassName: className,
	}
	if ret, _, err := winapi.ProcRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); ret == 0 {
		return fmt.Errorf("RegisterClassExW: %v", err)
	}

	hwnd, _, err := winapi.ProcCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(winapi.UTF16Ptr(t.title))),
		0, 0, 0, 0, 0, 0, 0, hInst, 0)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowExW: %v", err)
	}

	hicon := iconFromCanvas(t.iconCanvas())
	if hicon == 0 {
		hicon, _, _ = winapi.ProcLoadIconW.Call(0, idiApp)
	}

	t.mu.Lock()
	t.hwnd, t.hicon = hwnd, hicon
	tip := t.tip
	t.mu.Unlock()
	if tip == "" {
		tip = t.title
	}

	nid := t.newNID(hwnd, hicon, nifMessage|nifIcon|nifTip|nifShowTip)
	winapi.CopyUTF16(nid.SzTip[:], tip)
	if ret, _, err := winapi.ProcShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(nid))); ret == 0 {
		return fmt.Errorf("Shell_NotifyIcon: %v", err)
	}
	// Version 4 is what lets the tooltip carry a second line. A shell that does
	// not understand it simply keeps the first line.
	nidv := t.newNID(hwnd, hicon, 0)
	nidv.UVersion = notifyIconVersion4
	winapi.ProcShellNotifyIconW.Call(nimSetVersion, uintptr(unsafe.Pointer(nidv)))

	// The tip that explains menu rows. A failure leaves it nil, and the menu works
	// without it.
	if h := newHintWindow(); h != nil {
		t.mu.Lock()
		t.hint = h
		t.mu.Unlock()
	}

	// One-second timer drives the tooltip; the callback decides how much work
	// that costs, so the icon itself stays responsive.
	winapi.ProcSetTimer.Call(hwnd, tipTimerID, tipTimerMs, 0)

	var msg winapi.Msg
	for {
		ret, _, _ := winapi.ProcGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		winapi.ProcTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		winapi.ProcDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
	return nil
}

// iconCanvas renders the icon at the size the shell's small-icon metric asks
// for, which is what makes a high-DPI display show a crisp icon rather than a
// stretched 16-pixel one.
func (t *Icon) iconCanvas() *raster.Canvas {
	if t.cb.IconView == nil {
		return nil
	}
	size := winapi.IconMetric()
	t.mu.Lock()
	t.size = size
	t.mu.Unlock()
	return t.cb.IconView(size)
}
