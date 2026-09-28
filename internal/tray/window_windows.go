//go:build windows

package tray

import (
	"sync"
	"syscall"
	"time"
	"unsafe"

	"wbtray/internal/panel"
	"wbtray/internal/raster"
	"wbtray/internal/status"
	"wbtray/internal/theme"
	"wbtray/internal/ui"
	"wbtray/internal/winapi"
)

// The window the tray opens: the console.
//
// It is one window, created once and hidden rather than destroyed on close. A tray's
// main window is opened and dismissed many times per session, and building it each
// time would put a class registration and a GDI surface on a path the operator takes
// constantly. What it is not is a message loop of its own: the window is created on
// the same thread that pumps the tray's messages, so there is one loop and one
// thread, which is what keeps this from being the start of a toolkit.
//
// The interior is laid out by internal/ui and its text drawn by GDI, so the window
// follows the operator's font and scaling while the shapes stay this program's own.

// The window styles this file needs. The message numbers are the tray's own
// constants: this window is handled by the same procedure as the tray's, so a second
// set would be one more way to be wrong.
const (
	wsOverlappedWindow = 0x00CF0000
	cwUseDefault       = 0x80000000
	swShow             = 5
	swHide             = 0
	vkEscape           = 0x1B
	wmSetCursor        = 0x0020
	wmMouseWheel       = 0x020A
	wmLButtonDown      = 0x0201
	idcArrow           = 32512
	idcHand            = 32649
	wheelDelta         = 120
)

// minWidth and minHeight are the smallest window the layout can draw, and they match
// the guard in ui.Build: below them the window would show a surface with nothing on
// it, and a window that cannot show anything should not be shrinkable to that size.
const (
	minWidth  = 780
	minHeight = 460
)

// Window is the tray's main window.
type Window struct {
	mu sync.Mutex

	hwnd uintptr
	// renderer is the GDI text surface, sized to the window and rebuilt on resize.
	renderer *winapi.TextRenderer
	// layout is the frame drawn last, kept so a click can be matched against the
	// rectangles that were on screen when it happened.
	layout ui.Layout

	w, h int
	dpi  float64

	// The page and its scroll offsets, which are the window's own state rather than
	// the application's: they describe what the operator is looking at.
	tab       ui.Tab
	scroll    map[ui.Tab]int
	hover     string
	action    string
	pending   bool
	pendingAt time.Time

	// The data the extra pages need, fetched off the message thread and guarded
	// because a repaint can happen while a fetch is in flight.
	models   []panel.Model
	logs     []panel.LogEntry
	schedule panel.Schedule
	config   string

	// snapshot and palette are where the frame's live numbers come from, installed
	// by the application.
	snapshot func() status.Snapshot
	palette  func() theme.Palette
	// act runs one of the layout's actions. It is called on a goroutine so a
	// network round trip cannot block the message thread.
	act func(ui.Action, string)
	// onShow runs when the window is raised.
	onShow func()
}

var (
	windowClassOnce sync.Once
	windowClassErr  error
	// theWindow is the single instance, found by the window procedure, which has no
	// way to receive a pointer of its own.
	theWindow *Window
)

// NewWindow creates the window class and the window, hidden.
func NewWindow(icon *raster.Canvas) (*Window, error) {
	if err := registerWindowClass(icon); err != nil {
		return nil, err
	}
	w := &Window{
		w:      1180,
		h:      760,
		tab:    ui.TabOverview,
		scroll: map[ui.Tab]int{},
		dpi:    1,
	}
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
// It borrows the tray's own icons rather than defining its own, because the two
// windows belong to one program and one thread and a second class would be a second
// place for the same mistakes.
func registerWindowClass(icon *raster.Canvas) error {
	windowClassOnce.Do(func() {
		hInst, _, _ := winapi.ProcGetModuleHandleW.Call(0)
		hicon := iconFromCanvas(icon)
		cursor, _, _ := winapi.ProcLoadCursorW.Call(0, idcArrow)
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
		// The text surface is sized to the window, so a resize rebuilds it. The old
		// one is released first: a DIB per resize with nothing freeing them would
		// leak a megabyte for a minute of dragging.
		if w.renderer != nil {
			w.renderer.Close()
			w.renderer = nil
		}
		w.mu.Unlock()
		return 0

	case wmEraseBkgnd:
		// Claiming the erase is what keeps the window from flashing white before
		// the first paint lands.
		return 1

	case wmPaint:
		w.paint()
		return 0

	case wmLButtonDown:
		x, y := float64(int32(winapi.LowWord(wparam))), float64(int32(winapi.HighWord(wparam)))
		w.click(x, y)
		return 0

	case wmMouseWheel:
		// The wheel scrolls the page. Nothing else in the window moves, and the page
		// is the only thing long enough to need it.
		delta := int(int16(winapi.HighWord(wparam)))
		w.wheel(delta / wheelDelta)
		return 0

	case wmMouseMove:
		x, y := float64(int32(winapi.LowWord(wparam))), float64(int32(winapi.HighWord(wparam)))
		w.hoverAt(x, y)
		return 0

	case wmSetCursor:
		if w.over(ui.ActionRefresh) || w.hover != "" {
			cursor, _, _ := winapi.ProcLoadCursorW.Call(0, idcHand)
			winapi.ProcSetCursor.Call(cursor)
			return 1
		}
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

// over reports whether an action is anywhere in the last frame. It exists because
// the mouse-move handler runs on every pixel of movement and must not allocate.
func (w *Window) over(a ui.Action) bool {
	for _, h := range w.layout.Hits {
		if h.Action == a {
			return true
		}
	}
	return false
}

// hitAt finds the action at a point.
func (w *Window) hitAt(x, y float64) (ui.Hit, bool) {
	// Reverse order: later hits are drawn on top, so they are the ones the pointer
	// is over where two overlap.
	for i := len(w.layout.Hits) - 1; i >= 0; i-- {
		h := w.layout.Hits[i]
		if x >= h.X && x < h.X+h.W && y >= h.Y && y < h.Y+h.H {
			return h, true
		}
	}
	return ui.Hit{}, false
}

// click runs the action under a point.
func (w *Window) click(x, y float64) {
	h, ok := w.hitAt(x, y)
	if !ok {
		return
	}
	if h.Action == ui.ActionTab {
		w.mu.Lock()
		w.tab = ui.Tab(h.Arg)
		w.mu.Unlock()
		w.Invalidate()
		return
	}
	// A pending action is not repeated: the buttons are cheap to click twice and the
	// gateway is not, and a second balance refresh started by an impatient second
	// click is a second sweep of every account.
	w.mu.Lock()
	if w.pending {
		w.mu.Unlock()
		return
	}
	act := w.act
	w.pending = true
	w.pendingAt = time.Now()
	w.action = ""
	w.mu.Unlock()
	w.Invalidate()

	if act == nil {
		w.finish("", nil)
		return
	}
	go func() {
		// The action runs off the message thread, because every one of them is a
		// network round trip and the window has to stay responsive while it happens.
		act(h.Action, h.Arg)
	}()
}

// finish reports what an action did. It is called by the front end when the action
// returns, which is what the footer's message comes from.
func (w *Window) finish(msg string, err error) {
	w.mu.Lock()
	w.pending = false
	if err != nil {
		w.action = "失败：" + err.Error()
	} else if msg == "" {
		w.action = "完成"
	} else {
		w.action = msg
	}
	w.mu.Unlock()
	w.Invalidate()
}

// wheel scrolls the page that is showing.
func (w *Window) wheel(notches int) {
	w.mu.Lock()
	tab := w.tab
	w.mu.Unlock()
	if notches == 0 {
		return
	}
	// A notch is three rows, which is what a wheel does everywhere else.
	w.mu.Lock()
	cur := w.scroll[tab] - notches*3
	if cur < 0 {
		cur = 0
	}
	w.scroll[tab] = cur
	w.mu.Unlock()
	w.Invalidate()
}

// hoverAt records what the pointer is over, so the cursor can change.
func (w *Window) hoverAt(x, y float64) {
	h, ok := w.hitAt(x, y)
	key := ""
	if ok {
		key = string(h.Action) + "|" + h.Arg
	}
	w.mu.Lock()
	changed := key != w.hover
	w.hover = key
	w.mu.Unlock()
	if changed {
		winapi.ProcSetCursor.Call(0)
	}
}

// paint draws a frame and blits it.
//
// The whole window is redrawn rather than only the invalid region. The panels sit on
// one another and a partial repaint would have to know which panel a damaged
// rectangle belongs to, which is more state than a window that repaints a few times
// a second is worth.
func (w *Window) paint() {
	w.mu.Lock()
	hwnd, snap, pal := w.hwnd, w.snapshot, w.palette
	ww, hh := w.w, w.h
	tab := w.tab
	models, logs, sched, cfg := w.models, w.logs, w.schedule, w.config
	action, pending := w.action, w.pending
	paused := false
	scrollLogs, scrollModels, scrollAccts := w.scroll[ui.TabLogs], w.scroll[ui.TabModels], w.scroll[ui.TabAccounts]
	w.mu.Unlock()

	if snap == nil || pal == nil {
		return
	}
	p := pal()
	cfgPath := ""
	if cm, ok := configPathHolder(); ok {
		cfgPath = cm
	}
	layout := ui.Build(ui.View{
		W: ww, H: hh,
		Tab:            tab,
		Snap:           snap(),
		Lang:           "zh",
		Palette:        p,
		Action:         action,
		Pending:        pending,
		Models:         models,
		Logs:           logs,
		Schedule:       sched,
		Config:         cfg,
		ConfigPath:     cfgPath,
		Paused:         paused,
		ScrollLogs:     scrollLogs,
		ScrollModels:   scrollModels,
		ScrollAccounts: scrollAccts,
	})

	// BeginPaint requires a real PAINTSTRUCT: passing zero makes it fail and return
	// nothing, which draws a window with no content at all — which is exactly what
	// the first version of this did.
	var ps winapi.PaintStruct
	hdc, _, _ := winapi.ProcBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	defer winapi.ProcEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))

	c := &layout.Ink
	w.mu.Lock()
	if w.renderer == nil {
		w.renderer = winapi.NewTextRenderer(ww, hh)
	}
	tr := w.renderer
	dpi := winapi.DPIScaleFor(hwnd)
	w.dpi = dpi
	w.layout = layout
	w.mu.Unlock()

	if tr != nil {
		items := make([]winapi.TextItem, 0, len(layout.Texts))
		for _, t := range layout.Texts {
			items = append(items, winapi.TextItem{
				S: t.S, X: t.X, Y: t.Y, Size: t.Size,
				Weight: textWeight(t.Weight),
				Right:  t.Align == ui.Right,
				Centre: t.Align == ui.Centre,
				Colour: winapi.BGR(t.Colour),
			})
		}
		tr.Draw(c, items, dpi)
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

// configPathHolder is where the front end puts the gateway's config path, so the
// footer can show it without the window knowing about the panel client.
var configPathHolder = func() (string, bool) { return "", false }

// SetConfigPath installs where the gateway's settings live.
func SetConfigPath(fn func() (string, bool)) { configPathHolder = fn }

// SetSources installs where the window's frame comes from.
func (w *Window) SetSources(snapshot func() status.Snapshot, palette func() theme.Palette) {
	w.mu.Lock()
	w.snapshot, w.palette = snapshot, palette
	w.mu.Unlock()
}

// SetActions installs the handler for the layout's actions.
func (w *Window) SetActions(fn func(ui.Action, string)) {
	w.mu.Lock()
	w.act = fn
	w.mu.Unlock()
}

// Finish reports what an action did, for the footer.
func (w *Window) Finish(msg string, err error) { w.finish(msg, err) }

// SetExtra installs the data the pages beyond the overview need.
func (w *Window) SetExtra(models []panel.Model, logs []panel.LogEntry, schedule panel.Schedule, config string) {
	w.mu.Lock()
	w.models, w.logs, w.schedule, w.config = models, logs, schedule, config
	w.mu.Unlock()
	w.Invalidate()
}

// Tab reports the page showing, so the front end can fetch what it needs.
func (w *Window) Tab() ui.Tab {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.tab
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
	tr, hwnd := w.renderer, w.hwnd
	w.renderer, w.hwnd = nil, 0
	w.mu.Unlock()
	if tr != nil {
		tr.Close()
	}
	if hwnd != 0 {
		winapi.ProcDestroyWindow.Call(hwnd)
	}
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
	if w.onShow != nil {
		go w.onShow()
	}
}

// OnShow installs a callback for when the window is raised, so the front end can
// refresh the page the operator is about to look at.
func (w *Window) OnShow(fn func()) {
	w.mu.Lock()
	w.onShow = fn
	w.mu.Unlock()
}
