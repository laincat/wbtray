//go:build windows

package winapi

import (
	"syscall"
	"unsafe"

	"wbtray/internal/raster"
)

// Drawing text with GDI.
//
// Text is the one part of the window this program should not draw itself. Windows
// knows the operator's font, its size at their scaling, their ClearType settings and
// their language's glyphs; the bitmap face the tray icon uses knows none of that,
// and it was built for a sixteen-pixel icon rather than for a window. So the shapes
// come from the renderer and the text comes from the shell, and they are composited
// in one DIB.

// The font weights the layout asks for, as CreateFontIndirectW wants them.
const (
	FWNormal = 400
	FWSemi   = 600
)

// transparentBkMode keeps the text from painting a background over the shapes
// already in the buffer.
const transparentBkMode = 1

// TextItem is one string to draw, in the units the layout uses.
type TextItem struct {
	S      string
	X, Y   float64
	Size   float64
	Weight int
	Right  bool
	// Centre centres the string on X, which is what a button label needs. Right and
	// Centre are exclusive; a caller that sets both gets Right, which is at least
	// deterministic.
	Centre bool
	// Colour is the text colour as 0x00BBGGRR, which is the order GDI takes.
	Colour uint32
}

// TextRenderer draws text into a canvas through GDI.
//
// The canvas is copied into a DIB once, every string is drawn into it, and the
// result is copied back. Doing it in one pass matters: creating a font per string
// would be hundreds of GDI objects per frame, and a window that repaints on a timer
// cannot afford that.
type TextRenderer struct {
	hdc    uintptr
	hbmp   uintptr
	oldBmp uintptr
	// bits is kept as a pointer rather than as a uintptr, so the garbage collector
	// can see that this foreign block is still referenced. Storing an integer and
	// converting back on every frame is what vet flags, and the warning is about a
	// real hazard: an integer is not a reference.
	bits  unsafe.Pointer
	w, h  int
	fonts map[fontKey]uintptr
}

type fontKey struct {
	size   int
	weight int
}

// NewTextRenderer prepares a surface of the given size.
func NewTextRenderer(w, h int) *TextRenderer {
	if w <= 0 || h <= 0 {
		return nil
	}
	hdc, _, _ := ProcCreateCompatibleDC.Call(0)
	if hdc == 0 {
		return nil
	}
	hbmp, bits := NewDIBSection(w, h)
	if hbmp == 0 {
		ProcDeleteDC.Call(hdc)
		return nil
	}
	old, _, _ := ProcSelectObject.Call(hdc, hbmp)
	// Transparent background: the panel colours and the shapes behind the text are
	// already in the buffer, and a filled background would erase them.
	ProcSetBkMode.Call(hdc, transparentBkMode)
	return &TextRenderer{
		hdc: hdc, hbmp: hbmp, oldBmp: old, bits: bits,
		w: w, h: h, fonts: map[fontKey]uintptr{},
	}
}

// Close releases everything the renderer holds.
func (t *TextRenderer) Close() {
	if t == nil {
		return
	}
	for _, f := range t.fonts {
		ProcDeleteObject.Call(f)
	}
	if t.oldBmp != 0 {
		ProcSelectObject.Call(t.hdc, t.oldBmp)
	}
	if t.hbmp != 0 {
		ProcDeleteObject.Call(t.hbmp)
	}
	if t.hdc != 0 {
		ProcDeleteDC.Call(t.hdc)
	}
	t.fonts = nil
}

// font returns a font for a size and weight, creating it once and reusing it.
//
// The size arrives in logical pixels and is scaled by the window's DPI here, so the
// layout can be written in one unit while the rendering happens in another.
func (t *TextRenderer) font(size float64, weight int, dpi float64) uintptr {
	px := int(size*dpi + 0.5)
	if px < 1 {
		px = 1
	}
	key := fontKey{size: px, weight: weight}
	if f, ok := t.fonts[key]; ok {
		return f
	}
	lf := LogFont{
		// A negative height asks for a font with that character height rather than
		// that cell height, which is what makes two sizes one step apart look one
		// step apart.
		Height: -int32(px),
		Weight: int32(weight),
		// ClearType, so text matches every other window on the screen. The tray
		// icon is deliberately aliased; text is deliberately not.
		Quality: 5, // CLEARTYPE_QUALITY
	}
	copy(lf.FaceName[:], syscall.StringToUTF16("Segoe UI"))
	f, _, _ := ProcCreateFontIndirectW.Call(uintptr(unsafe.Pointer(&lf)))
	if f != 0 {
		t.fonts[key] = f
	}
	return f
}

// Draw paints every string into a copy of the canvas and returns the result.
//
// The canvas is not modified in place: the DIB is the working surface, and the
// shapes are copied into it first, so a caller can keep its own canvas and ask for
// a rendered frame as often as it likes.
func (t *TextRenderer) Draw(c *raster.Canvas, items []TextItem, dpi float64) *raster.Canvas {
	if t == nil || c == nil || c.W != t.w || c.H != t.h {
		return c
	}
	dst := unsafe.Slice((*byte)(t.bits), t.w*t.h*4)
	copy(dst, c.BGRA())

	curFont := uintptr(0)
	for _, it := range items {
		if it.S == "" {
			continue
		}
		f := t.font(it.Size, it.Weight, dpi)
		if f != 0 && f != curFont {
			ProcSelectObject.Call(t.hdc, f)
			curFont = f
		}
		x := int(it.X*dpi + 0.5)
		y := int(it.Y*dpi + 0.5)
		if it.Right {
			// The string is right-aligned against X, and its width can only be
			// measured once the font is selected.
			w := MeasureText(t.hdc, it.S)
			x -= w
		} else if it.Centre {
			// The same measurement, half of it: a label centred on a button's
			// midpoint, which is where the layout put the click target.
			w := MeasureText(t.hdc, it.S)
			x -= w / 2
		}
		// The Y the layout states is the top of the text box; TextOutW takes the
		// top of the character cell, which is what a caller writing a layout wants.
		DrawTextColoured(t.hdc, x, y, it.S, it.Colour)
	}
	return copyBack(c, dst)
}

// copyBack fills a canvas from a BGRA buffer.
func copyBack(c *raster.Canvas, b []byte) *raster.Canvas {
	for i := 0; i+3 < len(b) && i/4 < c.W*c.H; i += 4 {
		px := i / 4
		c.Pix[px] = raster.RGBA{R: b[i+2], G: b[i+1], B: b[i], A: 0xff}
	}
	return c
}

// MeasureText returns the width of a string in the DC's current font.
func MeasureText(hdc uintptr, s string) int {
	utf16 := syscall.StringToUTF16(s)
	if len(utf16) <= 1 {
		return 0
	}
	var size struct{ Cx, Cy int32 }
	ProcGetTextExtentPoint32W.Call(hdc,
		uintptr(unsafe.Pointer(&utf16[0])), uintptr(len(utf16)-1),
		uintptr(unsafe.Pointer(&size)))
	return int(size.Cx)
}

// DrawTextColoured writes a string at a position in the DC's current font.
func DrawTextColoured(hdc uintptr, x, y int, s string, colour uint32) {
	utf16 := syscall.StringToUTF16(s)
	if len(utf16) <= 1 {
		return
	}
	ProcSetTextColor.Call(hdc, uintptr(colour))
	ProcSetBkMode.Call(hdc, transparentBkMode)
	ProcTextOutW.Call(hdc, uintptr(x), uintptr(y),
		uintptr(unsafe.Pointer(&utf16[0])), uintptr(len(utf16)-1))
}

// BGR packs a colour for GDI, which takes 0x00BBGGRR.
func BGR(c raster.RGBA) uint32 {
	return uint32(c.R) | uint32(c.G)<<8 | uint32(c.B)<<16
}
