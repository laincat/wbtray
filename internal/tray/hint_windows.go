//go:build windows

package tray

import (
	"syscall"
	"unsafe"

	"wbtray/internal/traymenu"
	"wbtray/internal/winapi"
)

// The hint window: the one line of explanation that follows the menu.
//
// A system menu has no tooltips. The shell's tooltip control refuses a tool
// registered against a popup menu — TTM_ADDTOOL returns zero for every combination of
// flags and TTM_GETTOOLCOUNT stays zero, which was measured rather than assumed — so a
// row that needs to explain itself has nowhere of the shell's to put the sentence.
//
// What the shell does provide is the selection. WM_MENUSELECT arrives for every row
// the pointer crosses, carrying the row's id, and that is enough to drive a tip of
// this program's own: a topmost, never-activated window placed beside the menu. It was
// verified to draw above the menu and to leave the menu's window in the foreground,
// which is what makes the menu stay open while the tip is up.
//
// The window is created once per process and hidden between uses, because a tray menu
// is opened and closed all day and creating a window each time would leak a display
// handle per right-click.

// Window styles and flags for the tip.
const (
	wsExToolWindow = 0x00000080
	wsExNoActivate = 0x08000000
	wsPopupStyle   = 0x80000000
	wsBorderStyle  = 0x00800000
	swpNoActivate  = 0x0010
	swpShowWindow  = 0x0040
	// HWND_TOPMOST is the handle value -1, which in an unsigned word is every bit
	// set. HWND_NOTOPMOST, one less, is what a window has to be given to be pushed
	// behind everything — so the value is spelled out rather than adjusted, because
	// an off-by-one here reads as "topmost" and behaves as the opposite.
	hwndTopmost     = ^uintptr(0)
	colorInfoText   = 23
	colorInfoBk     = 24
	transparentMode = 1
)

// hintPad and hintFontSize give the tip its shape: a small inset and one line of text.
const (
	hintPad      = 6
	hintFontSize = 12
	hintMaxWidth = 420
)

// hintOffset is how far the tip is placed from the pointer, which is below and to the
// right of it: far enough not to sit under the cursor, close enough to read as
// belonging to the row being pointed at.
const hintOffset = 18

// hintWindow is the tip that follows the menu.
type hintWindow struct {
	hwnd uintptr
	// text is what the paint handler draws.
	text string
	// font is the size the text is drawn at, which the width is measured with.
	font int
	// hdc is a screen DC with the menu font selected, kept for the life of the tip
	// because every measurement is taken against it.
	hdc     uintptr
	oldFont uintptr
	// wndProcRef pins the callback for the process lifetime: a collected closure
	// would leave Windows calling freed memory.
	wndProcRef uintptr
}

// newHintWindow registers the class and creates the window, hidden.
//
// A failure returns nil rather than an error: a tray without hints is still a tray,
// and the menu itself does not depend on the tip existing.
func newHintWindow() *hintWindow {
	h := &hintWindow{font: hintFontSize}
	hInst, _, _ := winapi.ProcGetModuleHandleW.Call(0)

	h.wndProcRef = syscall.NewCallback(h.wndProc)
	wc := winapi.WndClassExW{
		CbSize:        uint32(unsafe.Sizeof(winapi.WndClassExW{})),
		LpfnWndProc:   h.wndProcRef,
		HInstance:     hInst,
		LpszClassName: winapi.UTF16Ptr("wbtrayHintWnd"),
	}
	// The class may already exist when a second tray runs in the same process, which
	// is what a test does. That is not a failure, and CreateWindowExW below is the
	// call that would report a real one.
	winapi.ProcRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	// No owner, deliberately. A popup window is kept above its owner, and a menu is
	// also owned by the tray window: giving the tip the same owner would put the two
	// in one z-order band, where whichever was activated last wins, and the menu is
	// always activated last because that is what opens it. With no owner the tip is
	// simply topmost, which is the arrangement that was measured to draw above the
	// menu.
	hwnd, _, _ := winapi.ProcCreateWindowExW.Call(
		wsExToolWindow|wsExNoActivate,
		uintptr(unsafe.Pointer(winapi.UTF16Ptr("wbtrayHintWnd"))),
		0,
		wsPopupStyle|wsBorderStyle,
		0, 0, 10, 10,
		0, 0, hInst, 0)
	if hwnd == 0 {
		return nil
	}
	h.hwnd = hwnd
	h.setup()
	return h
}

// setup opens the DC that measurements are taken against.
func (h *hintWindow) setup() {
	hdc, _, _ := winapi.ProcGetDC.Call(0)
	if hdc == 0 {
		return
	}
	font, _, _ := winapi.ProcGetStockObject.Call(defaultGuiFont)
	if font == 0 {
		winapi.ProcReleaseDC.Call(0, hdc)
		return
	}
	h.hdc = hdc
	h.oldFont, _, _ = winapi.ProcSelectObject.Call(hdc, font)
}

// teardown gives the DC back.
func (h *hintWindow) teardown() {
	if h.hdc == 0 {
		return
	}
	if h.oldFont != 0 {
		winapi.ProcSelectObject.Call(h.hdc, h.oldFont)
	}
	winapi.ProcReleaseDC.Call(0, h.hdc)
	h.hdc = 0
	h.oldFont = 0
}

// show places the tip against a point and paints a sentence.
func (h *hintWindow) show(text string, x, y int) {
	if h == nil || h.hwnd == 0 || text == "" {
		return
	}
	h.text = text

	w := h.measure()
	if w > hintMaxWidth {
		w = hintMaxWidth
	}
	height := h.height()

	winapi.ProcSetWindowPos.Call(h.hwnd, hwndTopmost,
		uintptr(x), uintptr(y), uintptr(w), uintptr(height),
		swpNoActivate|swpShowWindow)
	winapi.ProcInvalidateRect.Call(h.hwnd, 0, 1)
	winapi.ProcUpdateWindow.Call(h.hwnd)
}

// hide takes the tip off screen without destroying it.
func (h *hintWindow) hide() {
	if h == nil || h.hwnd == 0 {
		return
	}
	h.text = ""
	winapi.ProcShowWindow.Call(h.hwnd, 0)
}

// destroy releases the window.
func (h *hintWindow) destroy() {
	if h == nil || h.hwnd == 0 {
		return
	}
	winapi.ProcDestroyWindow.Call(h.hwnd)
	h.hwnd = 0
}

// height is the tip's height: one line of text between two insets.
func (h *hintWindow) height() int {
	return h.font + hintPad*3
}

// measure asks GDI how wide the sentence is, which is what decides the window's width.
func (h *hintWindow) measure() int {
	if h.hdc == 0 {
		return 200
	}
	u := syscall.StringToUTF16(h.text)
	var size struct{ cx, cy int32 }
	winapi.ProcGetTextExtentPoint32W.Call(h.hdc,
		uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1),
		uintptr(unsafe.Pointer(&size)))
	return int(size.cx) + hintPad*4
}

// wndProc handles the tip's messages.
func (h *hintWindow) wndProc(hwnd uintptr, msg uint32, wparam uintptr, lparam unsafe.Pointer) uintptr {
	switch msg {
	case wmPaint:
		h.paint(hwnd)
		return 0
	case wmEraseBkgnd:
		// The paint handler covers every pixel, so erasing first would only flicker.
		return 1
	}
	ret, _, _ := winapi.ProcDefWindowProcW.Call(hwnd, uintptr(msg), wparam, uintptr(lparam))
	return ret
}

// paint draws the tip: a filled rectangle and one line of text in the system's own
// tip colours, so it looks like every other tip on the desktop.
func (h *hintWindow) paint(hwnd uintptr) {
	var ps winapi.PaintStruct
	hdc, _, _ := winapi.ProcBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	defer winapi.ProcEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))

	var r winapi.Rect
	winapi.ProcGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))

	bg, _, _ := winapi.ProcGetSysColor.Call(colorInfoBk)
	brush, _, _ := winapi.ProcCreateSolidBrush.Call(bg)
	if brush != 0 {
		winapi.ProcFillRect.Call(hdc, uintptr(unsafe.Pointer(&r)), brush)
		winapi.ProcDeleteObject.Call(brush)
	}

	if h.text == "" {
		return
	}

	// The default GUI font, which is what the rest of the desktop's tips use.
	font, _, _ := winapi.ProcGetStockObject.Call(defaultGuiFont)
	old, _, _ := winapi.ProcSelectObject.Call(hdc, font)
	defer winapi.ProcSelectObject.Call(hdc, old)

	ink, _, _ := winapi.ProcGetSysColor.Call(colorInfoText)
	winapi.ProcSetTextColor.Call(hdc, ink)
	winapi.ProcSetBkMode.Call(hdc, transparentMode)

	// The inset is applied by shrinking the rectangle rather than by positioning the
	// text, so a long sentence is ellipsised at the right edge instead of being
	// clipped mid-character.
	r.Left += hintPad
	r.Top += hintPad
	r.Right -= hintPad
	r.Bottom -= hintPad

	u := syscall.StringToUTF16(h.text)
	winapi.ProcDrawTextW.Call(hdc,
		uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1),
		uintptr(unsafe.Pointer(&r)),
		winapi.DtLeft|winapi.DtVCenter|winapi.DtSingleLine|winapi.DtNoPrefix|winapi.DtEndEllipsis)
}

// hintText returns the sentence for a menu row, looked up in the model the menu was
// built from.
//
// The id is matched against the same []traymenu.Item the menu came from, because a
// menu drawn from one description and explained by another is two descriptions free to
// disagree.
func hintText(items []traymenu.Item, id uint32) string {
	if id == 0 {
		return ""
	}
	for _, it := range items {
		if it.ID == id {
			return it.Hint
		}
		if len(it.Children) > 0 {
			if s := hintText(it.Children, id); s != "" {
				return s
			}
		}
	}
	return ""
}
