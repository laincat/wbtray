//go:build windows

package tray

import (
	"math"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"wbtray/internal/raster"
	"wbtray/internal/theme"
	"wbtray/internal/traymenu"
	"wbtray/internal/textmask"
	"wbtray/internal/winapi"
)

// The drawn menu.
//
// The system menu cannot show a style preview, cannot tint a row, and follows
// the OS theme rather than the one chosen for the tray. Since the point of a
// tray is to be looked at, this menu is drawn here instead: one borderless
// window, a software-rendered bitmap blitted into it, and hit testing against
// the same layout the painter used.
//
// Everything is driven from a single []traymenu.Item, and the rows are laid out once per
// open. Hit testing reads the same geometry as the painting, so the two cannot
// drift apart.

// Layout constants for the flyout, in logical pixels before DPI scaling.
const (
	// The menu's own margin, and the rows' margin inside it.
	//
	// The two are separate on purpose: contentInset is the breathing room
	// between the border and anything drawn, and it is what the first version
	// lacked — rows ran to within a pixel of the edge, which reads as a menu that
	// has overflowed rather than as one that is simply full.
	foEdge   = 8.0
	foInset  = 6.0
	foRadius = 9.0

	foRowH      = 26.0
	foValueRowH = 20.0
	foSepH      = 13.0
	foCheckW    = 16.0
	foDotR      = 3.5
	foArrowW    = 13.0
	foSubIndent = 16.0
	foPreviewW  = 22.0
	foShadowPad = 11.0
	foMinWidth  = 210.0
	foMaxWidth  = 460.0

	// foBorderW is the width of the frame drawn around the menu. One pixel is
	// what a menu needs: a heavier frame looks like a dialog.
	foBorderW = 1.0

	foFontSize   = 13
	foFontSizeSm = 11
	foFontSizeHd = 14
)

// flyout is the drawn menu window.
type flyout struct {
	mu     sync.Mutex
	hwnd   uintptr
	wndRef uintptr
	open   bool

	// theme is the palette in force when the menu opened, so a repaint during a
	// theme change cannot mix two palettes.
	theme theme.Theme
	// rows is the flattened menu, in the order it is drawn.
	rows []row
	// width and height are the window's size in logical pixels.
	width, height int
	// scale is the DPI factor, so the layout is written once in logical pixels.
	scale float64

	// hover is the index of the row under the cursor, -1 for none.
	hover int
	// pressed is the row a mouse-down landed on.
	pressed int
	// subOwner is the index of the row whose submenu is expanded, -1 for none.
	subOwner int
	// scroll is the number of logical pixels the list is scrolled by, which is
	// what keeps a long account list on screen.
	scroll float64

	trackingMouse bool
	icon          *Icon
	// shownAt is when the menu was last made visible, which the dismiss watch
	// uses to leave it alone while the shell settles the foreground.
	shownAt time.Time
	// hadFocus records whether this appearance of the menu ever held the
	// keyboard focus. Windows does not always grant it to a window shown from a
	// background process, so "lost focus" is only meaningful once it was had.
	hadFocus bool
	// buttonWasDown is the previous poll's mouse-button state, which is what
	// turns sampling into a transition test.
	buttonWasDown bool
}

// tookFocus reports whether the current appearance of the menu ever held focus.
func (f *flyout) tookFocus() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hadFocus
}

// row is one laid-out line of the menu.
type row struct {
	item traymenu.Item
	// rect is in logical window coordinates.
	rect winapi.RectF
	// depth is how far the row is indented for being inside a submenu.
	depth float64
	// owner is the index of the submenu row this row belongs to, or -1.
	owner int
}

// createFlyout registers the flyout window class. The window itself is created
// on the first open, so a tray whose menu is never used allocates no window.
func (t *Icon) createFlyout(hInst uintptr) {
	f := &flyout{hover: -1, pressed: -1, subOwner: -1, icon: t, scale: 1}
	f.wndRef = syscall.NewCallback(f.wndProc)
	wc := winapi.WndClassExW{
		CbSize:        uint32(unsafe.Sizeof(winapi.WndClassExW{})),
		Style:         csHRedraw | csVRedraw | csDblClks,
		LpfnWndProc:   f.wndRef,
		HInstance:     hInst,
		LpszClassName: winapi.UTF16Ptr("wbtrayFlyoutWnd"),
	}
	// A class that already exists is fine, so the result is deliberately not
	// treated as an error.
	winapi.ProcRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	t.mu.Lock()
	t.flyout = f
	t.mu.Unlock()
}

// refresh repaints the menu if it is open, which is what makes the live rows
// update while the menu is being looked at.
func (f *flyout) refresh(t *Icon) {
	f.mu.Lock()
	open, hwnd := f.open, f.hwnd
	f.mu.Unlock()
	if !open || hwnd == 0 {
		return
	}
	winapi.ProcInvalidateRect.Call(hwnd, 0, 0)
}

// show opens the menu next to the cursor.
func (t *Icon) showFlyout() {
	t.mu.Lock()
	f := t.flyout
	t.mu.Unlock()
	if f == nil {
		return
	}
	f.openAt(t)
}

// currentTheme reads the palette and menu style from the application.
func (t *Icon) currentTheme() theme.Theme {
	if themeProvider == nil {
		return theme.Neon()
	}
	th, _ := themeProvider()
	return th
}

// themeProvider is installed by the application, which owns the configuration
// and therefore knows both the palette and which menu style is selected.
var themeProvider func() (theme.Theme, MenuStyle)

// SetThemeProvider installs the callback the flyout uses to learn the palette.
func SetThemeProvider(fn func() (theme.Theme, MenuStyle)) { themeProvider = fn }

// openAt lays the menu out and shows it with the cursor over it.
func (f *flyout) openAt(t *Icon) {
	items := t.menuItems()
	th := t.currentTheme()

	f.mu.Lock()
	f.theme = th
	f.subOwner = -1
	f.hover = -1
	f.pressed = -1
	f.scroll = 0
	f.hadFocus = false
	t.mu.Lock()
	owner := t.hwnd
	t.mu.Unlock()
	f.mu.Unlock()

	f.setScale(winapi.DPIScaleFor(owner))
	f.layout(items)

	var pt winapi.Point
	winapi.ProcGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	x, y := f.place(int(pt.X), int(pt.Y))

	f.mu.Lock()
	hwnd, w, h, scale := f.hwnd, f.width, f.height, f.scale
	f.mu.Unlock()
	ww := int(math.Round(float64(w) * scale))
	wh := int(math.Round(float64(h) * scale))

	if hwnd == 0 {
		hwnd = f.create(owner, ww, wh)
		if hwnd == 0 {
			return
		}
		f.mu.Lock()
		f.hwnd = hwnd
		f.mu.Unlock()
	}
	// Rounding the corners is a DWM attribute; where it is missing the window
	// simply has square corners.
	roundCorners(hwnd)

	f.mu.Lock()
	f.open = true
	f.shownAt = time.Now()
	f.mu.Unlock()

	winapi.ProcSetWindowPos.Call(hwnd, hwndTopmost, uintptr(x), uintptr(y),
		uintptr(ww), uintptr(wh), swpShowWindow)
	// Shown, then brought to the foreground, then placed. The order is the
	// documented dance: a window that is not yet visible cannot take the
	// foreground, and a window that has just taken it has not necessarily been
	// positioned by the time the shell looks at it.
	winapi.ProcSetForegroundWindow.Call(hwnd)
	winapi.ProcSetWindowPos.Call(hwnd, hwndTopmost, uintptr(x), uintptr(y),
		uintptr(ww), uintptr(wh), swpShowWindow)
	winapi.ProcInvalidateRect.Call(hwnd, 0, 1)
	winapi.ProcUpdateWindow.Call(hwnd)

	// The window is created without activation, so a timer watches for the user
	// moving on. It runs only while the menu is open.
	winapi.ProcSetTimer.Call(hwnd, dismissTimerID, dismissTimerMs, 0)
}

// Window placement constants.
const (
	hwndTopmost   = ^uintptr(0) // (HWND)-1
	swpNoActivate = 0x0010
	swpShowWindow = 0x0040
	swpNoMove     = 0x0002
	swpNoSize     = 0x0001
	swHide        = 0
)

// create makes the flyout window.
func (f *flyout) create(owner uintptr, w, h int) uintptr {
	hInst, _, _ := winapi.ProcGetModuleHandleW.Call(0)
	const (
		wsPopup         = 0x80000000
		wsExToolWindow  = 0x00000080
		wsExTopmost     = 0x00000008
	)
	// Not layered, and the menu is painted opaque from edge to edge.
	//
	// The window was briefly layered, to carry the soft drop shadow that was
	// drawn around the plate. That shadow was the bug: GDI's BitBlt ignores the
	// alpha channel, so the shadow's near-transparent black arrived on screen as
	// opaque black and the menu appeared inside a thick dark frame. A layered
	// window would have carried it correctly, but it also means every pixel has
	// to be composited by UpdateLayeredWindow rather than painted, and the
	// simpler answer is better: no shadow, no alpha, and an edge that the frame
	// already provides. Native menus have no shadow either.
	// Deliberately activatable, and that is the whole point of not passing
	// WS_EX_NOACTIVATE.
	//
	// The window was created non-activating in the first version, on the theory
	// that a menu should not steal focus. What that actually bought was a menu
	// that could not work: SetForegroundWindow on such a window does nothing, so
	// the dismiss watch — which closes the menu when the foreground window is
	// neither it nor the tray — saw a stranger in the foreground sixty
	// milliseconds after opening and closed the menu again. It flashed and was
	// gone, which is indistinguishable from a click doing nothing at all, and it
	// also meant the keyboard could never reach the menu.
	hwnd, _, _ := winapi.ProcCreateWindowExW.Call(
		wsExToolWindow|wsExTopmost,
		uintptr(unsafe.Pointer(winapi.UTF16Ptr("wbtrayFlyoutWnd"))),
		uintptr(unsafe.Pointer(winapi.UTF16Ptr("wbtray"))),
		wsPopup, 0, 0, uintptr(w), uintptr(h),
		owner, 0, hInst, 0)
	return hwnd
}

// close hides the window without destroying it: the next open reuses it, and a
// destroyed window would have to be recreated on every right-click.
func (f *flyout) close() {
	f.mu.Lock()
	hwnd := f.hwnd
	f.open = false
	f.subOwner = -1
	f.hover = -1
	f.pressed = -1
	f.mu.Unlock()
	if hwnd != 0 {
		winapi.ProcKillTimer.Call(hwnd, dismissTimerID)
		winapi.ProcShowWindow.Call(hwnd, swHide)
	}
}

// place picks a screen position for the menu: beside the cursor, and flipped or
// shifted so it lands inside the work area of the monitor the cursor is on.
func (f *flyout) place(cx, cy int) (int, int) {
	f.mu.Lock()
	w, h, scale := f.width, f.height, f.scale
	f.mu.Unlock()
	ww := int(float64(w) * scale)
	wh := int(float64(h) * scale)

	mi := winapi.MonitorInfo{CbSize: uint32(unsafe.Sizeof(winapi.MonitorInfo{}))}
	packed := uintptr(uint32(cx)) | uintptr(uint32(cy))<<32
	hmon, _, _ := winapi.ProcMonitorFromPoint.Call(packed, winapi.MonitorDefaultToNearest)
	winapi.ProcGetMonitorInfoW.Call(hmon, uintptr(unsafe.Pointer(&mi)))

	x := cx + 8
	if x+ww > int(mi.RcWork.Right) {
		x = cx - ww - 8
	}
	if x < int(mi.RcWork.Left) {
		x = int(mi.RcWork.Left)
	}
	y := cy - 12
	if y+wh > int(mi.RcWork.Bottom) {
		y = int(mi.RcWork.Bottom) - wh
	}
	if y < int(mi.RcWork.Top) {
		y = int(mi.RcWork.Top)
	}
	return x, y
}

// setScale records the DPI factor for the monitor the menu is opening on.
func (f *flyout) setScale(scale float64) {
	if scale < 1 {
		scale = 1
	}
	f.mu.Lock()
	f.scale = scale
	f.mu.Unlock()
}

// layout measures the menu and records each row's rectangle.
//
// The width is decided first, from the widest row, and then every row is laid
// out against it. Two passes rather than one, because a right-aligned value
// column cannot be placed until the final width is known.
func (f *flyout) layout(items []traymenu.Item) {
	f.mu.Lock()
	expanded := f.subOwner
	f.mu.Unlock()

	flat := flatten(items, expanded)
	// The plate is inset by the shadow on both sides, and the painter hands each
	// row the plate's width rather than the window's. Sizing the rows to the
	// window instead was worth exactly two shadow paddings — twenty pixels — and
	// it came out of the longest labels, which is why the menu clipped rows its
	// own layout arithmetic said would fit.
	plateWidth := foMinWidth
	for _, fr := range flat {
		plateWidth = math.Max(plateWidth, rowNaturalWidth(fr.item, fr.depth))
	}
	plateWidth = math.Min(plateWidth, foMaxWidth)

	rows := make([]row, 0, len(flat))
	// The rows start below the frame and the top inset, so the first row's
	// highlight has room above it and the border is never overlapped.
	y := foShadowPad + foEdge
	for _, fr := range flat {
		h := foRowH
		switch fr.item.Kind {
		case traymenu.SeparatorRow:
			h = foSepH
		case traymenu.ValueRow:
			h = foValueRowH
		}
		rows = append(rows, row{
			item:  fr.item,
			rect:  winapi.RectF{X: 0, Y: y, W: plateWidth, H: h},
			depth: fr.depth,
			owner: fr.owner,
		})
		y += h
	}
	height := y + foEdge + foShadowPad

	f.mu.Lock()
	f.rows = rows
	f.width = int(math.Ceil(plateWidth + 2*foShadowPad))
	f.height = int(math.Ceil(height))
	f.mu.Unlock()
}

// flatRow is one row after submenus have been expanded, which is what lets one
// list serve both the painting and the hit testing.
type flatRow struct {
	item  traymenu.Item
	depth float64
	owner int
}

// flatten expands the submenu at owner inline.
func flatten(items []traymenu.Item, owner int) []flatRow {
	var out []flatRow
	for _, it := range items {
		// The indices in the flattened list are what the hit test reports, so
		// the owner index is captured as the row is appended.
		index := len(out)
		out = append(out, flatRow{item: it, owner: -1})
		if it.Kind == traymenu.SubmenuRow && index == owner {
			for _, child := range it.Children {
				out = append(out, flatRow{item: child, depth: 1, owner: index})
			}
		}
	}
	return out
}

// drawWidth measures a string in the flyout's font.
func drawWidth(s string, size int) int {
	if s == "" {
		return 0
	}
	w, _ := textmask.Measure(s, textmask.NewFont(size, false))
	return w
}

// Row metrics, in logical pixels. They are named because the layout pass, the
// painter and the tests all have to agree on them: the first drawn menu clipped
// its longest rows because the painter reserved a gap the layout had not, and the
// two disagreed by six pixels.
const (
	// rowGap is the breathing room between a label and the value beside it.
	rowGap = 10.0
	// valueShare caps a value at this fraction of the room a row has, so a long
	// URL cannot squeeze out the name it belongs to.
	valueShare = 0.45
	// minValueWidth is the narrowest a value may become before it is truncated,
	// so a short figure is never reduced to nothing.
	minValueWidth = 30.0
)

// labelStart is the x offset where a row's text begins, relative to the plate.
func labelStart(depth float64) float64 { return foInset + foCheckW + depth*foSubIndent }

// rowNaturalWidth is the width a row would like: its fixed columns, its indent,
// and both text columns at full size.
func rowNaturalWidth(it traymenu.Item, depth float64) float64 {
	w := labelStart(depth) + float64(drawWidth(it.Text, foFontSize)) + foInset
	if it.Value != "" {
		w += rowGap + float64(drawWidth(it.Value, foFontSizeSm))
	}
	if it.Kind == traymenu.SubmenuRow {
		w += foArrowW
	}
	if it.Kind == traymenu.StyleRow {
		w += foPreviewW
	}
	return w
}

// roundCorners asks DWM to round the window, which is what makes the flyout look
// like part of Windows 11 rather than a rectangle from 1995.
func roundCorners(hwnd uintptr) {
	const (
		dwmwaWindowCornerPreference = 33
		dwmcpRound                  = 2
	)
	if winapi.ProcDwmSetWindowAttribute.Find() != nil {
		return
	}
	var pref int32 = dwmcpRound
	winapi.ProcDwmSetWindowAttribute.Call(hwnd, dwmwaWindowCornerPreference,
		uintptr(unsafe.Pointer(&pref)), unsafe.Sizeof(pref))
}

// paint renders the whole menu into a bitmap and blits it, which is what lets
// the icons and the previews be drawn by the same rasteriser as the tray icon.
func (f *flyout) paint(hwnd uintptr) {
	var ps winapi.PaintStruct
	hdc, _, _ := winapi.ProcBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	defer winapi.ProcEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))

	f.mu.Lock()
	rows := append([]row{}, f.rows...)
	w, h, scale := f.width, f.height, f.scale
	th, hover, scroll := f.theme, f.hover, f.scroll
	f.mu.Unlock()

	pw, ph := int(math.Round(float64(w)*scale)), int(math.Round(float64(h)*scale))
	if pw <= 0 || ph <= 0 || len(rows) == 0 {
		return
	}
	c := raster.New(pw, ph)
	// The style gallery's previews are re-rendered at the size they will be drawn
	// rather than scaled from whatever size the model carried.
	if f.icon != nil && f.icon.cb.StylePreview != nil {
		for i := range rows {
			if rows[i].item.Kind != traymenu.StyleRow {
				continue
			}
			size := int(math.Round(foPreviewW * scale))
			if p := f.icon.cb.StylePreview(int(rows[i].item.ID-styleIDBase), size); p != nil {
				rows[i].item.Preview = p
			}
		}
	}
	paintMenu(c, rows, scale, th, hover, scroll)

	// The bitmap is created per paint and blitted once. A menu repaints only on
	// hover changes, so this is not a hot path, and it keeps the painter free of
	// a device-dependent cache that would have to be invalidated.
	hbm, bits := winapi.NewDIBSection(pw, ph)
	if hbm == 0 {
		return
	}
	defer winapi.ProcDeleteObject.Call(hbm)
	copy(unsafe.Slice((*byte)(bits), pw*ph*4), c.BGRA())

	memDC, _, _ := winapi.ProcCreateCompatibleDC.Call(hdc)
	if memDC == 0 {
		return
	}
	defer winapi.ProcDeleteDC.Call(memDC)
	old, _, _ := winapi.ProcSelectObject.Call(memDC, hbm)
	defer winapi.ProcSelectObject.Call(memDC, old)
	const srccopy = 0x00CC0020
	winapi.ProcBitBlt.Call(hdc, 0, 0, uintptr(pw), uintptr(ph), memDC, 0, 0, srccopy)
}
