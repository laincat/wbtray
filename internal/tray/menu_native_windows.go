//go:build windows

package tray

import (
	"syscall"
	"unsafe"

	"wbtray/internal/raster"
	"wbtray/internal/traymenu"
	"wbtray/internal/winapi"
)

// The system menu.
//
// The menu is the shell's own, built from the same []traymenu.Item the rest of the
// program reasons about. Two kinds of row need care:
//
//   - A style row carries a preview, and a menu bitmap is the one way the system
//     menu can show a picture.
//   - A status row carries a colour, and a colour drawn as a menu bitmap is drawn
//     grey: the shell dims the bitmap of a row it has dimmed, and a status row is
//     dimmed because there is nothing to click. Those rows are painted here instead,
//     through WM_DRAWITEM, which is the only way the colour survives.
//
// Everything else is left to the shell, because a row drawn by hand is a row that
// ages differently from the rest of the menu.

// Owner-draw constants: the item type that arrives in MEASUREITEMSTRUCT, and the
// states that arrive in DRAWITEMSTRUCT.
const (
	odtMenu     = 1
	odsSelected = 0x0001
)

// System colours, by index, as GetSysColor wants them.
const (
	colorMenu      = 4
	colorMenuText  = 7
	colorGrayText  = 17
	colorHighlight = 13
	colorHlText    = 14
)

// dtVCenter is the DrawText flag that centres a label vertically; the label is
// positioned by hand instead, but the constant documents the intent.
const transparentBkMode = 1

// DefaultGuiFont is the stock object the shell draws menus with.
const defaultGuiFont = 17

// measureItemStruct mirrors MEASUREITEMSTRUCT.
type measureItemStruct struct {
	CtlType    uint32
	CtlID      uint32
	ItemID     uint32
	ItemWidth  uint32
	ItemHeight uint32
	ItemData   uintptr
}

// drawItemStruct mirrors DRAWITEMSTRUCT.
type drawItemStruct struct {
	CtlType    uint32
	CtlID      uint32
	ItemID     uint32
	ItemAction uint32
	ItemState  uint32
	HwndItem   uintptr
	HDC        uintptr
	RcItem     winapi.Rect
	ItemData   uintptr
}

// textMetric mirrors TEXTMETRICW.
type textMetric struct {
	Height, Ascent, Descent, ExtLead, Overhang, EMHeight, CharExtra int32
	Weight, Italic, Underlined, StruckOut, PitchAndFamily, CharSet  int32
}

// menuBitmapOwner keeps a menu's bitmaps alive until it closes: AppendMenu does not
// copy them, so releasing them early would leave the menu pointing at a deleted
// object.
type menuBitmapOwner struct{ handles []uintptr }

func (b *menuBitmapOwner) add(c *raster.Canvas) uintptr {
	if c == nil {
		return 0
	}
	// A menu bitmap is drawn at its natural size, so it is rendered at the icon
	// metric: the same size the taskbar uses.
	size := winapi.IconMetric()
	scaled := c
	if c.W != size {
		scaled = c.Scale(size, size)
	}
	hbm, bits := winapi.NewDIBSection(scaled.W, scaled.H)
	if hbm == 0 {
		return 0
	}
	dst := unsafe.Slice((*byte)(bits), scaled.W*scaled.H*4)
	copy(dst, scaled.BGRA())
	b.handles = append(b.handles, hbm)
	return hbm
}

func (b *menuBitmapOwner) release() {
	for _, h := range b.handles {
		winapi.ProcDeleteObject.Call(h)
	}
	b.handles = nil
}

// The spacing an owner-draw row is measured with, in the same terms the painter
// draws it: an inset at each end, a gutter for the colour pip, and a gap between the
// label and the right-aligned value.
const (
	ownerInset     = 12
	ownerDotGutter = 22
	ownerGap       = 18
)

// statusPainter holds what the owner-draw rows need while a menu is open: the rows
// themselves, keyed by the value passed as ItemData, and the metrics of the font the
// shell draws menus with.
type statusPainter struct {
	rows      map[uintptr]traymenu.Item
	rowHeight int32
	ascent    int32
	// width is what every owner-draw row reports through MEASUREITEMSTRUCT.
	//
	// The shell does not size a row it is not drawing; the row states its own
	// width. Reporting a width larger than the row needs is therefore not
	// harmless — a menu is as wide as its widest row, so one generous number
	// widens every row in the menu. It is measured from the strings instead.
	width int32
	// hdc is a screen DC with the menu font selected, kept for the life of the
	// menu because both the metrics and every row's width are measured against it.
	hdc     uintptr
	oldFont uintptr
}

func newStatusPainter() *statusPainter {
	p := &statusPainter{rows: map[uintptr]traymenu.Item{}, rowHeight: 26, ascent: 12}
	p.setup()
	return p
}

// setup opens the DC the painter measures against and reads the menu font's
// metrics from it.
//
// The de-facto menu font is the default GUI font, which is what the DC inside
// WM_DRAWITEM already has selected. Reading its metrics rather than hard-coding a
// row height is what keeps the rows the right size on a machine whose text scaling
// is not the default.
func (p *statusPainter) setup() {
	hdc, _, _ := winapi.ProcGetDC.Call(0)
	if hdc == 0 {
		return
	}
	font, _, _ := winapi.ProcGetStockObject.Call(defaultGuiFont)
	if font == 0 {
		winapi.ProcReleaseDC.Call(0, hdc)
		return
	}
	p.hdc = hdc
	old, _, _ := winapi.ProcSelectObject.Call(hdc, font)
	p.oldFont = old

	var tm textMetric
	if ret, _, _ := winapi.ProcGetTextMetricsW.Call(hdc, uintptr(unsafe.Pointer(&tm))); ret == 0 {
		return
	}
	if tm.Ascent <= 0 || tm.Height <= 0 {
		return
	}
	p.ascent = tm.Ascent
	// The padding is what keeps a row from reading as a table cell.
	p.rowHeight = tm.Height + 12
}

// release gives back the DC the painter borrowed.
func (p *statusPainter) release() {
	if p.hdc == 0 {
		return
	}
	if p.oldFont != 0 {
		winapi.ProcSelectObject.Call(p.hdc, p.oldFont)
	}
	winapi.ProcReleaseDC.Call(0, p.hdc)
	p.hdc = 0
	p.oldFont = 0
}

// add registers a row to be painted and returns the index to pass as ItemData.
func (p *statusPainter) add(it traymenu.Item) uintptr {
	key := uintptr(len(p.rows) + 1)
	p.rows[key] = it
	if w := p.rowWidth(it); w > p.width {
		p.width = w
	}
	return key
}

// rowWidth is the width one owner-draw row has to be given: the insets the painter
// draws with, the colour gutter when there is a pip, the label, and the value.
func (p *statusPainter) rowWidth(it traymenu.Item) int32 {
	// Without a DC the text cannot be measured, so a plausible minimum is
	// reported rather than a zero that would clip every row.
	if p.hdc == 0 {
		return 160
	}
	w := int32(ownerInset + ownerInset)
	if it.Dot.A > 0 {
		w += ownerDotGutter
	}
	w += int32(measureText(p.hdc, it.Text))
	if it.Value != "" {
		w += ownerGap + int32(measureText(p.hdc, it.Value))
	}
	return w
}

func (p *statusPainter) lookup(key uintptr) (traymenu.Item, bool) {
	it, ok := p.rows[key]
	return it, ok
}

// fill paints a solid rectangle.
func fill(hdc uintptr, r winapi.Rect, colour uint32) {
	br, _, _ := winapi.ProcCreateSolidBrush.Call(uintptr(colour))
	if br == 0 {
		return
	}
	defer winapi.ProcDeleteObject.Call(br)
	winapi.ProcFillRect.Call(hdc, uintptr(unsafe.Pointer(&r)), br)
}

// sysColor returns a colour from the system's palette.
func sysColor(index int) uint32 {
	c, _, _ := winapi.ProcGetSysColor.Call(uintptr(index))
	return uint32(c)
}

// measureText returns the width of a string in the DC's current font.
func measureText(hdc uintptr, s string) int {
	utf16 := syscall.StringToUTF16(s)
	if len(utf16) <= 1 {
		return 0
	}
	var size struct{ Cx, Cy int32 }
	winapi.ProcGetTextExtentPoint32W.Call(hdc,
		uintptr(unsafe.Pointer(&utf16[0])), uintptr(len(utf16)-1),
		uintptr(unsafe.Pointer(&size)))
	return int(size.Cx)
}

// drawLabel writes a string at a position in the DC's current font.
func drawLabel(hdc uintptr, x, y int, s string, colour uint32) {
	utf16 := syscall.StringToUTF16(s)
	if len(utf16) <= 1 {
		return
	}
	winapi.ProcSetTextColor.Call(hdc, uintptr(colour))
	winapi.ProcSetBkMode.Call(hdc, transparentBkMode)
	winapi.ProcTextOutW.Call(hdc, uintptr(x), uintptr(y),
		uintptr(unsafe.Pointer(&utf16[0])), uintptr(len(utf16)-1))
}

// drawDot paints the health pip.
//
// It is drawn as a stack of one-pixel-high rectangles rather than as a bitmap: a
// bitmap would have to be told a background colour to blend against, and the menu
// background is whatever the user's theme says it is.
func drawDot(hdc uintptr, cx, cy, radius int, c raster.RGBA) {
	flat := uint32(c.R) | uint32(c.G)<<8 | uint32(c.B)<<16
	bg := sysColor(colorMenu)
	for y := -radius; y <= radius; y++ {
		dy := float64(y)
		inner := float64(radius*radius) - dy*dy
		if inner <= 0 {
			continue
		}
		half := sqrt(inner)
		left := cx - int(half)
		right := cx + int(half)
		if right <= left {
			// A one-pixel-wide row still needs to exist at the poles.
			right = left + 1
		}
		// The rows near the top and bottom of the disc are faded toward the menu
		// colour, which is what keeps the pip from reading as a hexagon.
		frac := half / float64(radius)
		colour := flat
		if frac < 0.75 {
			colour = blend(flat, bg, 1-frac/0.75)
		}
		row := winapi.Rect{
			Left:   int32(left),
			Top:    int32(cy + y),
			Right:  int32(right),
			Bottom: int32(cy + y + 1),
		}
		fill(hdc, row, colour)
	}
}

// blend mixes two colours by t, which is 0 for a and 1 for b.
func blend(a, b uint32, t float64) uint32 {
	switch {
	case t <= 0:
		return a
	case t >= 1:
		return b
	}
	mix := func(shift uint) uint32 {
		av := float64((a >> shift) & 0xff)
		bv := float64((b >> shift) & 0xff)
		return uint32(av*(1-t)+bv*t+0.5) & 0xff
	}
	return mix(0) | mix(8)<<8 | mix(16)<<16
}

// sqrt is a Newton iteration, kept here so this file does not pull in math for one
// call. The renderer uses the same one.
func sqrt(v float64) float64 {
	if v <= 0 {
		return 0
	}
	x := v
	for i := 0; i < 24; i++ {
		x = 0.5 * (x + v/x)
	}
	return x
}

// drawStatus paints one owner-draw row: an optional colour pip, the label, and an
// optional right-aligned value.
func drawStatus(p *statusPainter, dis *drawItemStruct) {
	it, ok := p.lookup(dis.ItemData)
	if !ok || dis.HDC == 0 {
		return
	}
	hdc := dis.HDC
	r := dis.RcItem

	// The background first: the shell paints nothing behind an owner-draw row, so
	// a row that skipped this would show whatever was on screen before the menu.
	bg := sysColor(colorMenu)
	ink := sysColor(colorMenuText)
	if dis.ItemState&odsSelected != 0 {
		bg = sysColor(colorHighlight)
		ink = sysColor(colorHlText)
	}
	fill(hdc, r, bg)

	centreY := (int(r.Top) + int(r.Bottom)) / 2
	textY := centreY - int(p.ascent)/2 - 1
	x := int(r.Left) + 12

	// The pip column sits where the shell draws a check mark, so the status rows
	// line up with the ticked rows below them.
	if it.Dot.A > 0 {
		drawDot(hdc, x+6, centreY, 5, it.Dot)
		x += 22
	}

	drawLabel(hdc, x, textY, it.Text, ink)

	if it.Value != "" {
		vw := measureText(hdc, it.Value)
		dim := sysColor(colorGrayText)
		if dis.ItemState&odsSelected != 0 {
			dim = ink
		}
		drawLabel(hdc, int(r.Right)-12-vw, textY, it.Value, dim)
	}
}

// showNativeMenu builds and tracks the system menu at a point in screen
// coordinates.
func (t *Icon) showNativeMenu(x, y int) {
	items := t.menuItems()
	if len(items) == 0 {
		return
	}
	bitmaps := &menuBitmapOwner{}
	painter := newStatusPainter()

	menu := buildMenu(items, bitmaps, painter)
	if menu == 0 {
		bitmaps.release()
		return
	}
	defer winapi.ProcDestroyMenu.Call(menu)
	defer bitmaps.release()
	defer painter.release()

	var pt winapi.Point
	winapi.ProcGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	if x != 0 || y != 0 {
		pt = winapi.Point{X: int32(x), Y: int32(y)}
	}

	hwnd := t.hwndForMenu()
	// Without taking the foreground first, the menu fails to dismiss when the user
	// clicks elsewhere — a well-known shell quirk.
	winapi.ProcSetForegroundWindow.Call(hwnd)

	// The tip needs the model the menu was built from, so that a selection can be
	// turned back into the sentence that goes with it, and the place the menu was
	// put so it can sit against it.
	t.mu.Lock()
	t.menuItemsSnapshot = items
	t.hintX, t.hintY = int(pt.X)+hintOffset, int(pt.Y)+hintOffset
	hint := t.hint
	t.mu.Unlock()
	if hint != nil {
		defer hint.hide()
	}

	// The painter has to be reachable from the window procedure while the menu is
	// up, because that is where WM_DRAWITEM arrives.
	t.setMenuPainter(painter)
	defer t.setMenuPainter(nil)

	cmd, _, _ := winapi.ProcTrackPopupMenu.Call(menu,
		tpmRightBtn|tpmReturnCmd, uintptr(pt.X), uintptr(pt.Y), 0, hwnd, 0)
	if cmd != 0 && t.cb.Select != nil {
		t.cb.Select(traymenu.Event{ID: uint32(cmd)})
	}

	// The menu is gone, so the sentences that described it are stale.
	t.mu.Lock()
	t.menuItemsSnapshot = nil
	t.mu.Unlock()
}

func (t *Icon) hwndForMenu() uintptr {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.hwnd
}

// setMenuPainter installs the painter the window procedure uses while a menu is
// open, and removes it when the menu closes.
func (t *Icon) setMenuPainter(p *statusPainter) {
	t.mu.Lock()
	t.menuPainter = p
	t.mu.Unlock()
}

// buildMenu turns the model into an HMENU.
func buildMenu(items []traymenu.Item, bitmaps *menuBitmapOwner, painter *statusPainter) uintptr {
	hMenu, _, _ := winapi.ProcCreatePopupMenu.Call()
	if hMenu == 0 {
		return 0
	}
	for _, it := range items {
		switch it.Kind {
		case traymenu.SeparatorRow:
			winapi.ProcAppendMenuW.Call(hMenu, mfSeparator, 0, 0)

		case traymenu.SubmenuRow:
			sub := buildMenu(it.Children, bitmaps, painter)
			if sub == 0 {
				continue
			}
			// The value column is spelled as a tab, which is how a menu row
			// carries a right-aligned figure without being drawn by hand.
			label := it.Text
			if it.Value != "" {
				label += "	" + it.Value
			}
			winapi.ProcAppendMenuW.Call(hMenu, mfPopup, sub,
				uintptr(unsafe.Pointer(winapi.UTF16Ptr(label))))

		case traymenu.StyleRow:
			hbm := bitmaps.add(it.Preview)
			flags := uintptr(mfString | mfBitmap)
			if it.Checked {
				flags |= mfChecked
			}
			if hbm != 0 {
				winapi.ProcAppendMenuW.Call(hMenu, flags, uintptr(it.ID), hbm)
			} else {
				winapi.ProcAppendMenuW.Call(hMenu, uintptr(mfString), uintptr(it.ID),
					uintptr(unsafe.Pointer(winapi.UTF16Ptr(it.Text))))
			}

		default:
			if it.OwnerDraw {
				// A row whose colour is information. MF_DISABLED keeps it inert
				// without taking the colour away, which is the whole point.
				key := painter.add(it)
				flags := uintptr(mfOwnerDraw)
				if it.Disabled {
					flags |= mfDisabled
				}
				winapi.ProcAppendMenuW.Call(hMenu, flags, uintptr(it.ID), key)
				continue
			}
			flags := uintptr(mfString)
			if it.Checked {
				flags |= mfChecked
			}
			if it.Bold {
				flags |= mfDefault
			}
			if it.Disabled {
				flags |= mfGrayed
			}
			label := it.Text
			if it.Value != "" {
				label += "	" + it.Value
			}
			winapi.ProcAppendMenuW.Call(hMenu, flags, uintptr(it.ID),
				uintptr(unsafe.Pointer(winapi.UTF16Ptr(label))))
		}
	}
	return hMenu
}

// updateHint shows or hides the tip for a selection message.
//
// WM_MENUSELECT carries the row's id in the low word and its kind in the high word.
// The kind is what distinguishes a command from a separator or a submenu — rows that
// have no sentence of their own and would otherwise leave the previous row's sentence
// on screen.
func (t *Icon) updateHint(hint *hintWindow, items []traymenu.Item, wparam uintptr, x, y int) bool {
	const (
		mfPopup     = 0x00000010
		mfSeparator = 0x00000800
		mfSysMenu   = 0x00002000
	)
	id := winapi.LowWord(wparam)
	flags := uint32(wparam >> 16)
	if flags&mfSeparator != 0 || flags&mfPopup != 0 || flags&mfSysMenu != 0 || id == 0 {
		hint.hide()
		return true
	}
	text := hintText(items, id)
	if text == "" {
		hint.hide()
		return true
	}
	hint.show(text, x, y)
	return true
}
