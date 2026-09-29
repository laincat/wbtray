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

// The tray panel: the card that opens from the notification area.
//
// It is a window of this program's own rather than the shell's menu, which is what
// lets it draw a switch and a sparkline. The cost is the whole machinery a menu gets
// for free — creating a window, positioning it against the taskbar, hit-testing it,
// dismissing it when the operator clicks away — and that is what this file is.
//
// It is created once and hidden rather than created per click, because a tray panel
// is opened and closed all day and a window per opening would leak a display handle
// and a class registration each time.
//
// Two styles matter and both were measured rather than assumed. WS_EX_NOACTIVATE
// keeps the panel from taking focus, so the window underneath does not lose its
// active title bar when the panel opens. WS_POPUP with no owner keeps it above the
// taskbar without being in the shell's z-order band, which is what it would join if
// the tray window owned it.

// Window styles and messages for the panel.
// The styles and messages this file needs beyond the ones the tray already names.
//
// The message numbers the tray has are reused rather than restated: the panel is
// handled by a procedure in the same package, and a second set of the same constants
// is a second place for one of them to be wrong.
const (
	// swpNoSize and swpNoMove let the panel be repositioned without being resized,
	// which is what showing it costs nothing but the move.
	swpNoSize = 0x0001
	swpNoMove = 0x0002
	// swpNoZOrder leaves the window's place in the z-order alone, which is what lets a
	// resize happen without the window jumping above everything else on the way.
	swpNoZOrder = 0x0004
	wmActivate  = 0x0006
	// wmKillFocus is the second of the two messages that dismiss the panel. It is kept
	// alongside wmActivate because they arrive together and either one is enough.
	wmKillFocus = 0x0008
	// WA_INACTIVE is the wParam of wmActivate when this window has stopped being the
	// active one, which is the signal a flyout dismisses itself on.
	waInactive = 0
	// waActive is its opposite, and it is recorded rather than acted on: a panel that
	// has been active closes when something else takes the focus, and one that never
	// managed to take it must not, or the dismissal that keeps it open is the dismissal
	// that closes it. See the case for both messages below.
	waActive = 1
	// wsExNoActivate is deliberately absent, and this is the fix for a panel that
	// opened and then vanished. The panel must hold the focus while it is open: that is
	// what "click anywhere else and it closes" is built out of, and it is how every
	// flyout on the desktop behaves. A window created with WS_EX_NOACTIVATE can never
	// hold it, so the WM_KILLFOCUS it dismissed itself on arrived immediately — from
	// whatever window did own the focus — and the panel closed a moment after opening,
	// on some machines and not others depending on what else was in the foreground.
	//
	// WS_EX_TOOLWINDOW still keeps it out of the taskbar and out of Alt+Tab.
	wsExToolWindowPanel = 0x00000080
	wsPopupPanel        = 0x80000000
)

// Panel is the tray's own panel: a drawn card rather than the shell's menu.
// panelCorner is the radius the window is clipped to. It matches the corner the
// layout draws the card with, because a region that disagreed with the drawing would
// either shave the card's edge or leave a sliver of the notch behind.
const panelCorner = 12

type Panel struct {
	mu sync.Mutex

	hwnd uintptr
	// renderer is the GDI text surface, sized to the panel and rebuilt when the
	// layout's height changes.
	renderer *winapi.TextRenderer
	layout   ui.TrayLayout
	// hits is the last frame's clickable rectangles.
	hits []ui.TrayHit
	// hover is the row the pointer is over, so the cursor can change.
	hover string
	// active records whether the panel ever actually held the focus. It is what tells
	// the two arrivals of WM_ACTIVATE(WA_INACTIVE) apart: the one that means the
	// operator clicked somewhere else, and the one that arrives because the panel was
	// never activated at all and the foreground never left the window that had it.
	active bool

	w, h int

	// sources and act are installed by the application.
	snapshot func() status.Snapshot
	palette  func() theme.Palette
	state    func() (paused, auto, installed bool)
	act      func(ui.TrayAction, string)
	// lang is where the panel's language comes from, so switching it in the panel
	// itself takes effect on the next open rather than on the next launch.
	lang func() string

	wndProcRef uintptr
}

var (
	panelOnce   sync.Once
	panelErr    error
	thePanel    *Panel
	panelAnchor func() (int, int, bool)
)

// NewTrayPanel creates the panel window, hidden.
func NewTrayPanel(icon *raster.Canvas) (*Panel, error) {
	panelOnce.Do(func() {
		p := &Panel{w: ui.PanelW, h: 400}
		thePanel = p

		hInst, _, _ := winapi.ProcGetModuleHandleW.Call(0)
		p.wndProcRef = syscall.NewCallback(p.wndProc)
		wc := winapi.WndClassExW{
			CbSize:        uint32(unsafe.Sizeof(winapi.WndClassExW{})),
			LpfnWndProc:   p.wndProcRef,
			HInstance:     hInst,
			LpszClassName: winapi.UTF16Ptr("wbtrayPanelWnd"),
		}
		// The class may already exist in a test process. That is not a failure, and
		// the create call below is the one that would report a real one.
		winapi.ProcRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

		// No owner, deliberately: a popup is kept above its owner, and the tray window
		// is what a menu would be owned by. With no owner the panel is simply topmost,
		// which is the arrangement that was measured to draw above the taskbar.
		hwnd, _, cerr := winapi.ProcCreateWindowExW.Call(
			wsExToolWindowPanel,
			uintptr(unsafe.Pointer(winapi.UTF16Ptr("wbtrayPanelWnd"))),
			uintptr(unsafe.Pointer(winapi.UTF16Ptr("wbtray"))),
			wsPopupPanel,
			0, 0, uintptr(p.w), uintptr(p.h),
			0, 0, hInst, 0)
		if hwnd == 0 {
			panelErr = cerr
			return
		}
		p.hwnd = hwnd
	})
	if panelErr != nil {
		return nil, panelErr
	}
	return thePanel, nil
}

// SetSources installs where the panel's frame comes from.
func (p *Panel) SetSources(
	snapshot func() status.Snapshot,
	palette func() theme.Palette,
	state func() (paused, auto, installed bool),
) {
	p.mu.Lock()
	p.snapshot, p.palette, p.state = snapshot, palette, state
	p.mu.Unlock()
}

// SetActions installs the handler for the panel's clicks.
func (p *Panel) SetActions(fn func(ui.TrayAction, string)) {
	p.mu.Lock()
	p.act = fn
	p.mu.Unlock()
}

// SetLang installs where the panel's language comes from. As with the window, the
// default is English so a panel without one still lays out.
func (p *Panel) SetLang(fn func() string) {
	p.mu.Lock()
	p.lang = fn
	p.mu.Unlock()
}

// Anchor installs the function that says where the tray icon is, so the panel can be
// placed against it. It reports screen coordinates and whether the icon is ours to
// position against; the shell's own notification-area rectangle is not readable, so
// the fallback is the cursor.
func Anchor(fn func() (int, int, bool)) { panelAnchor = fn }

func (p *Panel) wndProc(hwnd uintptr, msg uint32, wparam uintptr, lparam unsafe.Pointer) uintptr {
	// The instance is looked up rather than taken from the receiver: a callback
	// created with syscall.NewCallback cannot carry a Go pointer, so the procedure is
	// a method on nothing and finds its window this way.
	self := thePanel
	if self == nil {
		r, _, _ := winapi.ProcDefWindowProcW.Call(hwnd, uintptr(msg), wparam, uintptr(lparam))
		return r
	}
	switch msg {
	case wmPaint:
		self.paint()
		return 0
	case wmEraseBkgnd:
		return 1
	case wmLButtonDown:
		mx, my := winapi.MousePos(uintptr(lparam))
		x, y := float64(mx), float64(my)
		self.click(x, y)
		return 0
	case wmMouseMove:
		mx, my := winapi.MousePos(uintptr(lparam))
		x, y := float64(mx), float64(my)
		self.move(x, y)
		return 0
	case wmSetCursor:
		if self.hover != "" {
			if c, _, _ := winapi.ProcLoadCursorW.Call(0, idcHand); c != 0 {
				winapi.ProcSetCursor.Call(c)
				return 1
			}
		}
		return 0
	case wmActivate:
		// Clicking anywhere else is how a menu is dismissed, and the panel does the same.
		//
		// The signal only counts if the panel has been active. Windows sends
		// WM_ACTIVATE(WA_INACTIVE) to a window that never held the focus as well — it is
		// the shell telling the new window what its state is, and the state of a window
		// that was never foreground is "not the foreground". Treating that as the
		// operator clicking away closes the panel the instant it opens, which is what
		// happened here, and it is why SetForegroundWindow refusing to grant the focus
		// showed up as a panel that flickered rather than as a panel that would not come
		// forward.
		switch winapi.LowWord(wparam) {
		case waActive:
			self.mu.Lock()
			self.active = true
			self.mu.Unlock()
		case waInactive:
			self.mu.Lock()
			was := self.active
			self.active = false
			self.mu.Unlock()
			if was {
				self.Hide()
			}
		}
		return 0
	}
	r, _, _ := winapi.ProcDefWindowProcW.Call(hwnd, uintptr(msg), wparam, uintptr(lparam))
	return r
}

// paint draws the panel and blits it.
func (p *Panel) paint() {
	p.mu.Lock()
	hwnd, snap, pal, state, act := p.hwnd, p.snapshot, p.palette, p.state, p.act
	lang := p.lang
	p.mu.Unlock()

	var ps winapi.PaintStruct
	hdc, _, _ := winapi.ProcBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	defer winapi.ProcEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))

	if snap == nil {
		return
	}
	paused, auto, installed := false, false, false
	if state != nil {
		paused, auto, installed = state()
	}
	language := "en"
	if lang != nil {
		language = lang()
	}
	layout := ui.BuildTray(ui.TrayView{
		Snap:      snap(),
		Palette:   pal(),
		Lang:      language,
		Paused:    paused,
		Auto:      auto,
		Installed: installed,
	})

	// The window is resized to the layout, because the height is computed from the
	// rows rather than fixed: a panel that kept its old height would clip the last row
	// or leave a band of nothing under it.
	if layout.H != p.h || layout.W != p.w {
		p.mu.Lock()
		if p.renderer != nil {
			p.renderer.Close()
			p.renderer = nil
		}
		p.w, p.h = layout.W, layout.H
		p.mu.Unlock()
		// The size is set, not suppressed: this is the call that makes the window match
		// the layout, and passing swpNoSize here — which the first version did — leaves
		// the window at whatever size it was created with for the rest of its life.
		winapi.ProcSetWindowPos.Call(hwnd, 0,
			0, 0, uintptr(layout.W), uintptr(layout.H),
			swpNoMove|swpNoActivate|swpNoZOrder)
	}

	c := &layout.Ink
	p.mu.Lock()
	if p.renderer == nil {
		p.renderer = winapi.NewTextRenderer(layout.W, layout.H)
	}
	tr := p.renderer
	p.hits = layout.Hits
	p.layout = layout
	p.mu.Unlock()

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
		tr.Draw(c, items, winapi.DPIScaleFor(hwnd))
	}

	// The window is clipped to the card's own outline before it is painted, which is
	// what keeps the four notches outside a rounded card from arriving as black: the
	// pixels there are unpainted, and BitBlt below carries no alpha, so black is what
	// they would be. Clipping the window is what puts them outside it instead.
	winapi.RoundWindow(hwnd, layout.W, layout.H, panelCorner)

	scr, _, _ := winapi.ProcCreateCompatibleDC.Call(hdc)
	bitmap, bits := winapi.NewDIBSection(layout.W, layout.H)
	if bitmap == 0 {
		winapi.ProcDeleteDC.Call(scr)
		return
	}
	defer winapi.ProcDeleteObject.Call(bitmap)
	old, _, _ := winapi.ProcSelectObject.Call(scr, bitmap)
	dst := unsafe.Slice((*byte)(bits), layout.W*layout.H*4)
	copy(dst, c.BGRA())
	winapi.ProcBitBlt.Call(hdc, 0, 0, uintptr(layout.W), uintptr(layout.H), scr, 0, 0, 0x00CC0020)
	winapi.ProcSelectObject.Call(scr, old)
	winapi.ProcDeleteDC.Call(scr)
	_ = act
}

// click runs the action under a point.
func (p *Panel) click(x, y float64) {
	p.mu.Lock()
	hits, act := p.hits, p.act
	p.mu.Unlock()
	// Reverse order: later hits are drawn on top, so they are the ones the pointer is
	// over where two overlap.
	for i := len(hits) - 1; i >= 0; i-- {
		h := hits[i]
		if x < h.X || x >= h.X+h.W || y < h.Y || y >= h.Y+h.H {
			continue
		}
		if h.Action == ui.TrayQuit {
			p.Hide()
		}
		if act != nil {
			go act(h.Action, h.Arg)
		}
		return
	}
}

// move records what the pointer is over, so the cursor can change.
func (p *Panel) move(x, y float64) {
	p.mu.Lock()
	key := ""
	for _, h := range p.hits {
		if x >= h.X && x < h.X+h.W && y >= h.Y && y < h.Y+h.H {
			key = string(h.Action) + "|" + h.Arg
			break
		}
	}
	changed := key != p.hover
	p.hover = key
	p.mu.Unlock()
	if changed {
		winapi.ProcSetCursor.Call(0)
	}
}

// Show places the panel against the tray icon and shows it.
func (p *Panel) Show() {
	p.mu.Lock()
	hwnd := p.hwnd
	p.mu.Unlock()
	if hwnd == 0 {
		return
	}
	x, y := p.place()
	p.mu.Lock()
	w, h := p.w, p.h
	p.mu.Unlock()
	// The size is stated rather than suppressed. The layout's height is computed from
	// the rows, and the window has to be that size before it is shown or the first
	// frame is drawn into a window that is not the size it was laid out for.
	winapi.ProcSetWindowPos.Call(hwnd, hwndTopmost,
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		swpNoActivate|swpShowWindow)
	// The focus is taken on purpose. A flyout that dismisses itself when it loses the
	// focus has to hold the focus, or the first click anywhere at all — including the
	// click that is opening it — takes it away again and it closes on the same frame it
	// opened. SetForegroundWindow is the API for that; it can refuse, which leaves a
	// panel that stays open until the next click somewhere else, and that is a better
	// failure than one that will not stay open.
	winapi.ProcSetForegroundWindow.Call(hwnd)
	p.Invalidate()
}

// place works out where the panel goes.
//
// It is anchored to the notification area rather than to the pointer, because a panel
// that opened under the cursor would be under the hand that just clicked. The x is
// clamped to the work area, which is what keeps a panel opened from a tray near the
// right edge of the screen from running off it.
func (p *Panel) place() (int, int) {
	pt := winapi.Point{}
	if panelAnchor != nil {
		if ax, ay, ok := panelAnchor(); ok {
			pt.X, pt.Y = int32(ax), int32(ay)
		}
	}
	if pt.X == 0 && pt.Y == 0 {
		winapi.ProcGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	}
	p.mu.Lock()
	w, h := p.w, p.h
	p.mu.Unlock()

	work := winapi.WorkArea(int(pt.X), int(pt.Y))
	const m = 8
	// Above the taskbar if the icon is in the bottom half of its monitor, below it
	// otherwise: that is what the shell does with its own flyouts, and it is what keeps
	// the panel off the taskbar.
	x := int(pt.X) - w + 24
	y := int(pt.Y) - h - 8
	if int(pt.Y) < (int(work.Top)+int(work.Bottom))/2 {
		y = int(pt.Y) + 8
	}
	if x < int(work.Left)+m {
		x = int(work.Left) + m
	}
	if x+w > int(work.Right)-m {
		x = int(work.Right) - w - m
	}
	if y < int(work.Top)+m {
		y = int(work.Top) + m
	}
	if y+h > int(work.Bottom)-m {
		y = int(work.Bottom) - h - m
	}
	return x, y
}

// Hide takes the panel away without destroying it.
func (p *Panel) Hide() {
	p.mu.Lock()
	hwnd := p.hwnd
	p.mu.Unlock()
	if hwnd != 0 {
		winapi.ProcShowWindow.Call(hwnd, swHide)
	}
}

// Toggle shows the panel if it is hidden and hides it if it is not.
func (p *Panel) Toggle() {
	if p.Visible() {
		p.Hide()
		return
	}
	p.Show()
}

// Visible reports whether the panel is on screen.
func (p *Panel) Visible() bool {
	p.mu.Lock()
	hwnd := p.hwnd
	p.mu.Unlock()
	if hwnd == 0 {
		return false
	}
	ret, _, _ := winapi.ProcIsWindowVisible.Call(hwnd)
	return ret != 0
}

// Invalidate asks for a repaint.
func (p *Panel) Invalidate() {
	p.mu.Lock()
	hwnd := p.hwnd
	p.mu.Unlock()
	if hwnd != 0 {
		winapi.ProcInvalidateRect.Call(hwnd, 0, 0)
	}
}

// Close releases the panel's GDI objects.
func (p *Panel) Close() {
	p.mu.Lock()
	tr, hwnd := p.renderer, p.hwnd
	p.renderer, p.hwnd = nil, 0
	p.mu.Unlock()
	if tr != nil {
		tr.Close()
	}
	if hwnd != 0 {
		winapi.ProcDestroyWindow.Call(hwnd)
	}
}
