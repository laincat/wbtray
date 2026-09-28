//go:build windows

package tray

import (
	"sync"
	"syscall"
	"unsafe"

	"wbtray/internal/raster"
	"wbtray/internal/status"
	"wbtray/internal/theme"
	"wbtray/internal/ui"
	"wbtray/internal/winapi"
)

// The window the tray opens.
//
// It is one window, created once and hidden rather than destroyed on close. A
// tray's main window is opened and dismissed many times per session, and building
// it each time would put a class registration and a GDI surface on a path the
// operator takes constantly. What it is not is a message-loop of its own: the
// window is created on the same thread that pumps the tray's messages, so there is
// one loop and one thread, which is what keeps this from being the start of a
// toolkit.
//
// The interior is drawn by internal/ui and its text by GDI, so the window follows
// the operator's font and scaling while the shapes stay this program's own.

// The window styles this file needs. The message numbers are the tray's own
// constants: this window is handled by the same procedure as the tray's, so it
// would be one more way to be wrong to add a second set.
const (
	wsOverlappedWindow = 0x00CF0000
	cwUseDefault       = 0x80000000
	swShow             = 5
	swHide             = 0
	vkEscape           = 0x1B
)

// minWidth and minHeight are the smallest window the layout can draw, and they
// match the guard in ui.Build: below them the window would show a surface with
// nothing on it, and a window that cannot show anything should not be shrinkable
// to that size.
const (
	minWidth  = 620
	minHeight = 420
)

// Window is the tray's main window.
type Window struct {
	mu       sync.Mutex
	hwnd     uintptr
	icon     uintptr
	renderer *winapi.TextRenderer
	// frame is the canvas the shapes were last painted into, kept so a repaint
	// after an exposure does not have to re-lay-out and re-draw everything.
	frame *raster.Canvas
	w, h  int

	// snapshot is where the window's numbers come from, installable by the
	// application so the window can redraw itself without the front end having to
	// reach into the tray.
	snapshot func() status.Snapshot
	palette  func() theme.Palette
}

var (
	windowClassOnce sync.Once
	windowClassErr  error
	// theWindow is the single instance, found by the window procedure which has no
	// way to receive a pointer of its own.
	theWindow *Window
)

// NewWindow creates the window class and the window, hidden.
func NewWindow(icon *raster.Canvas) (*Window, error) {
	if err := registerWindowClass(icon); err != nil {
		return nil, err
	}
	w := &Window{w: 1100, h: 720}
	theWindow = w

	hInst, _, _ := winapi.ProcGetModuleHandleW.Call(0)
	// The size the layout was designed for is the client area, not the window: the
	// frame's thickness is the shell's business and changes with the DPI and the
	// window's style, so it is asked for rather than guessed.
	ww, hh := winapi.WindowSizeForClient(w.w, w.h, wsOverlappedWindow)
	hwnd, _, err := winapi.ProcCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(winapi.UTF16Ptr("wbtrayMainWnd"))),
		uintptr(unsafe.Pointer(winapi.UTF16Ptr("WorkBuddy2API"))),
		wsOverlappedWindow,
		cwUseDefault, cwUseDefault, uintptr(ww), uintptr(hh),
		0, 0, hInst, 0)
	if hwnd == 0 {
		return nil, err
	}
	w.hwnd = hwnd
	return w, nil
}

// registerWindowClass registers the class once.
//
// It borrows the tray's own window procedure and icons rather than defining its
// own: the two windows belong to one program and one thread, and a second class
// would be a second place for the same mistakes.
func registerWindowClass(icon *raster.Canvas) error {
	windowClassOnce.Do(func() {
		hInst, _, _ := winapi.ProcGetModuleHandleW.Call(0)
		hicon := iconFromCanvas(icon)
		cursor, _, _ := winapi.ProcLoadCursorW.Call(0, 32512) // IDC_ARROW
		wc := winapi.WndClassExW{
			CbSize:        uint32(unsafe.Sizeof(winapi.WndClassExW{})),
			Style:         csHRedraw | csVRedraw,
			LpfnWndProc:   windowProcRef,
			HInstance:     hInst,
			HIcon:         hicon,
			HIconSm:       hicon,
			HCursor:       cursor,
			HbrBackground: 0,
			LpszClassName: winapi.UTF16Ptr("wbtrayMainWnd"),
		}
		ret, _, err := winapi.ProcRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
		if ret == 0 {
			windowClassErr = err
		}
	})
	return windowClassErr
}

// windowProcRef is the callback the class is registered with. It is created once
// because a callback address must not move.
var windowProcRef = syscall.NewCallback(windowProc)

func windowProc(hwnd uintptr, msg uint32, wparam uintptr, lparam unsafe.Pointer) uintptr {
	w := theWindow
	if w == nil {
		r, _, _ := winapi.ProcDefWindowProcW.Call(hwnd, uintptr(msg), wparam, uintptr(lparam))
		return r
	}
	switch msg {
	case wmSize:
		w.mu.Lock()
		w.w, w.h = int(winapi.LowWord(wparam)), int(winapi.HighWord(wparam))
		// The text surface is sized to the window, so a resize rebuilds it. The
		// old one is released first: a DIB per resize with nothing freeing them
		// would leak a megabyte for a minute of dragging.
		if w.renderer != nil {
			w.renderer.Close()
			w.renderer = nil
		}
		w.frame = nil
		w.mu.Unlock()
		return 0

	case wmEraseBkgnd:
		// Claiming the erase is what keeps the window from flashing white before
		// the first paint lands.
		return 1

	case wmPaint:
		w.paint()
		return 0

	case wmKeyDown:
		if wparam == vkEscape {
			w.Hide()
			return 0
		}

	case wmClose:
		// Closing hides. The tray owns the window's lifetime, the same way it owns
		// the process's.
		w.Hide()
		return 0
	}
	r, _, _ := winapi.ProcDefWindowProcW.Call(hwnd, uintptr(msg), wparam, uintptr(lparam))
	return r
}

// paint draws a frame and blits it.
//
// The whole window is redrawn rather than only the invalid region. The panels are
// translucent over one another and a partial repaint would have to know which
// panel a damaged rectangle belongs to, which is more state than a window that
// repaints a few times a second is worth.
func (w *Window) paint() {
	w.mu.Lock()
	hwnd, snap, pal := w.hwnd, w.snapshot, w.palette
	ww, hh := w.w, w.h
	w.mu.Unlock()

	var ps winapi.PaintStruct
	hdc, _, _ := winapi.ProcBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	defer winapi.ProcEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))

	if snap == nil || pal == nil {
		return
	}
	layout := ui.Build(ui.View{
		W: ww, H: hh,
		Palette: pal(),
		Snap:    snap(),
		Lang:    "zh",
	})
	c := &layout.Ink

	// The text, through GDI, into the same buffer the shapes went into.
	w.mu.Lock()
	if w.renderer == nil {
		w.renderer = winapi.NewTextRenderer(ww, hh)
	}
	tr := w.renderer
	w.mu.Unlock()
	if tr != nil {
		items := make([]winapi.TextItem, 0, len(layout.Texts))
		for _, t := range layout.Texts {
			items = append(items, winapi.TextItem{
				S: t.S, X: t.X, Y: t.Y, Size: t.Size,
				Weight: textWeight(t.Weight),
				Right:  t.Align == ui.Right,
				Colour: winapi.BGR(t.Colour),
			})
		}
		tr.Draw(c, items, winapi.DPIScaleFor(hwnd))
	}

	// And the result onto the window, in one blit.
	scr, _, _ := winapi.ProcCreateCompatibleDC.Call(hdc)
	bitmap, bits := winapi.NewDIBSection(ww, hh)
	if bitmap == 0 {
		winapi.ProcDeleteDC.Call(scr)
		return
	}
	defer winapi.ProcDeleteObject.Call(bitmap)
	old, _, _ := winapi.ProcSelectObject.Call(scr, bitmap)
	dst := unsafe.Slice((*byte)(bits), ww*hh*4)
	copy(dst, c.BGRA())
	winapi.ProcBitBlt.Call(hdc, 0, 0, uintptr(ww), uintptr(hh), scr, 0, 0, 0x00CC0020)
	winapi.ProcSelectObject.Call(scr, old)
	winapi.ProcDeleteDC.Call(scr)
}

// textWeight maps the layout's three levels onto GDI's font weights.
func textWeight(w ui.Weight) int {
	if w == ui.Figure {
		return winapi.FWSemi
	}
	return winapi.FWNormal
}

// SetSources installs where the window's frame comes from.
func (w *Window) SetSources(snapshot func() status.Snapshot, palette func() theme.Palette) {
	w.mu.Lock()
	w.snapshot, w.palette = snapshot, palette
	w.mu.Unlock()
}

// Show raises the window, creating its surface if the size changed.
func (w *Window) Show() {
	w.mu.Lock()
	hwnd := w.hwnd
	w.mu.Unlock()
	if hwnd == 0 {
		return
	}
	// Being the foreground window is what lets the window take the keyboard, and a
	// window opened from a tray icon is otherwise left behind whatever was focused.
	winapi.ProcSetForegroundWindow.Call(hwnd)
	winapi.ProcShowWindow.Call(hwnd, swShow)
	w.Invalidate()
}

// Hide takes the window away without destroying it.
func (w *Window) Hide() {
	w.mu.Lock()
	hwnd := w.hwnd
	w.mu.Unlock()
	if hwnd != 0 {
		winapi.ProcShowWindow.Call(hwnd, swHide)
	}
}

// Toggle shows the window if it is hidden and hides it if it is not.
func (w *Window) Toggle() {
	if w.Visible() {
		w.Hide()
		return
	}
	w.Show()
}

// Visible reports whether the window is on screen.
func (w *Window) Visible() bool {
	w.mu.Lock()
	hwnd := w.hwnd
	w.mu.Unlock()
	if hwnd == 0 {
		return false
	}
	ret, _, _ := winapi.ProcIsWindowVisible.Call(hwnd)
	return ret != 0
}

// Invalidate asks for a repaint on the next message.
func (w *Window) Invalidate() {
	w.mu.Lock()
	hwnd := w.hwnd
	w.mu.Unlock()
	if hwnd != 0 {
		winapi.ProcInvalidateRect.Call(hwnd, 0, 0)
	}
}

// Close releases the window's GDI objects. It is called once, on the way out.
func (w *Window) Close() {
	w.mu.Lock()
	tr, hwnd, hicon := w.renderer, w.hwnd, w.icon
	w.renderer, w.hwnd = nil, 0
	w.mu.Unlock()
	if tr != nil {
		tr.Close()
	}
	if hicon != 0 {
		winapi.ProcDestroyIcon.Call(hicon)
	}
	if hwnd != 0 {
		winapi.ProcDestroyWindow.Call(hwnd)
	}
}
