//go:build windows

// Command uidemo answers one question before any work is committed to the tray: what a
// Windows 11 style tray menu looks like on this machine, at this scaling, with this accent
// colour, beside the system menu the tray opens today.
//
// Every ingredient a real implementation would need is used rather than approximated —
// the operator's accent colour read from Windows, the system's own UI font, the Fluent
// icon glyphs, and a supersampled surface — so the picture informs a decision about the
// design rather than about a mock-up.
//
// The surface is drawn into a raster canvas, which is the shape the tray's icon renderer
// already has and what gives the anti-aliased corners and the hairline border: it is drawn
// at four times the size and averaged down. Text cannot come from that renderer, because
// its built-in font is a five-by-seven dot matrix with no room for Chinese or for icons, so
// every string is rasterised by GDI into a mask and composited by hand. That step is the
// one piece of machinery the tray does not currently have.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"syscall"
	"unsafe"

	"wbtray/internal/raster"
	"wbtray/internal/winapi"
)

// ── the palette ─────────────────────────────────────────────────────────────────────

// surface is the Windows 11 flyout palette: the shell's own values for this build rather
// than approximations. The accent is the operator's, because a menu that picks its own
// blue beside the system's own reads as one of the two being wrong.
type surface struct {
	Bg      raster.RGBA
	Hover   raster.RGBA
	Ink     raster.RGBA
	InkDim  raster.RGBA
	Line    raster.RGBA
	Edge    raster.RGBA
	Accent  raster.RGBA
	Desktop raster.RGBA
}

func darkSurface(accent raster.RGBA) surface {
	return surface{
		Bg:      raster.Hex("#2b2b2b"),
		Hover:   raster.Hex("#3d3d3d"),
		Ink:     raster.Hex("#ffffff"),
		InkDim:  raster.Hex("#c5c5c5"),
		Line:    raster.Hex("#3f3f3f"),
		Edge:    raster.Hex("#4a4a4a"),
		Accent:  accent,
		Desktop: raster.Hex("#1f1f1f"),
	}
}

func lightSurface(accent raster.RGBA) surface {
	return surface{
		Bg:      raster.Hex("#f9f9f9"),
		Hover:   raster.Hex("#eaeaea"),
		Ink:     raster.Hex("#1a1a1a"),
		InkDim:  raster.Hex("#5c5c5c"),
		Line:    raster.Hex("#e5e5e5"),
		Edge:    raster.Hex("#d6d6d6"),
		Accent:  accent,
		Desktop: raster.Hex("#d8d8d8"),
	}
}

// ── the menu ────────────────────────────────────────────────────────────────────────

type item struct {
	label string
	value string
	glyph string
	sep   bool
	dot   bool
	sub   bool
	tick  bool
	hover bool
}

// menu is the tray's menu as it stands, so the comparison is with the real thing.
var menu = []item{
	{label: "Laincat", value: "8,362", dot: true},
	{sep: true},
	{label: "网关", value: "PID 34604", glyph: "\uE8D7", sub: true, hover: true},
	{label: "账号", value: "1/1", glyph: "\uE716", sub: true},
	{label: "任务", value: "已启用 5", glyph: "\uE9D5", sub: true},
	{label: "面板", glyph: "\uE774"},
	{label: "其它", glyph: "\uE9D9", sub: true},
	{sep: true},
	{label: "托盘", glyph: "\uE713", sub: true},
	{sep: true},
	{label: "退出", glyph: "\uE7E8"},
}

// ── metrics ─────────────────────────────────────────────────────────────────────────

// The Windows 11 flyout metrics in logical pixels, and the supersample factor the surface
// is drawn at before it is averaged down.
const (
	rowH    = 32.0
	sepH    = 9.0
	padX    = 12.0
	padY    = 5.0
	radius  = 8.0
	itemR   = 4.0
	iconCol = 20.0
	gap     = 10.0
	textPx  = 14.0
	valuePx = 12.0
	width   = 300.0
	super   = 4
)

// uiFont is the family Windows 11 itself uses for this scale, and iconsFont is the glyph
// font the shell's own menus draw their icons from.
const (
	uiFont    = "Segoe UI Variable Text"
	uiFallbk  = "Segoe UI"
	iconsFont = "Segoe Fluent Icons"
)

func main() {
	out := flag.String("out", "uidemo.png", "where to write the picture")
	scale := flag.Float64("scale", 1.0, "display scaling to render at")
	dark := flag.Bool("dark", true, "render the dark appearance")
	flag.Parse()

	accent, ok := systemAccent()
	if !ok {
		fmt.Fprintln(os.Stderr, "uidemo: the accent could not be read; using the default")
	}
	s := darkSurface(accent)
	if !*dark {
		s = lightSurface(accent)
	}
	if err := render(*out, s, *scale); err != nil {
		fmt.Fprintf(os.Stderr, "uidemo: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("accent: #%02x%02x%02x\n", accent.R, accent.G, accent.B)
}

func systemAccent() (raster.RGBA, bool) {
	packed, ok := winapi.AccentColor()
	if !ok {
		return raster.Hex("#0078d4"), false
	}
	return raster.RGBA{
		R: uint8(packed & 0xff),
		G: uint8((packed >> 8) & 0xff),
		B: uint8((packed >> 16) & 0xff),
		A: 0xff,
	}, true
}

func render(path string, s surface, scale float64) error {
	height := padY * 2
	for _, it := range menu {
		if it.sep {
			height += sepH
			continue
		}
		height += rowH
	}

	c := newCanvas(width, height, scale)
	drawMenu(c, s)

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := png.Encode(f, c.image()); err != nil {
		return err
	}
	return nil
}

// ── the canvas ──────────────────────────────────────────────────────────────────────

// canvas draws in logical pixels over a supersampled raster canvas, and composites text
// rasterised by GDI.
type canvas struct {
	c     *raster.Canvas
	scale float64
	// s is the factor the raster canvas is larger than the logical coordinates.
	s float64
	// hdc is a memory DC with the text fonts selected, used to produce glyph masks.
	hdc     uintptr
	uiFont  uintptr
	uiSmall uintptr
	icoFont uintptr
	// desktop is the colour the picture is placed on, so the rounded corners are visible
	// against something rather than cropped.
	desktop raster.RGBA
}

func newCanvas(w, h, scale float64) *canvas {
	s := scale * super
	c := &canvas{
		c:     raster.New(int(w*s), int(h*s)),
		scale: scale,
		s:     s,
	}
	c.open()
	return c
}

// px converts a logical coordinate to the raster canvas.
func (c *canvas) px(v float64) float64 { return v * c.s }

// fill paints a rounded rectangle in logical coordinates.
func (c *canvas) rect(x, y, w, h, r float64, col raster.RGBA) {
	c.c.RoundedRect(c.px(x), c.px(y), c.px(w), c.px(h), c.px(r), col)
}

func (c *canvas) rule(x, y, w, h float64, col raster.RGBA) {
	c.c.Rect(c.px(x), c.px(y), c.px(w), c.px(h), col)
}

func (c *canvas) disc(x, y, r float64, col raster.RGBA) {
	c.c.Circle(c.px(x), c.px(y), c.px(r), col)
}

// image averages the supersampled surface down and puts a margin around it, so the rounded
// corners are visible against a desktop colour rather than cropped.
func (c *canvas) image() image.Image {
	out := c.c.Downsample(super)
	const margin = 26
	sheet := image.NewRGBA(image.Rect(0, 0, out.W+margin*2, out.H+margin*2))
	desk := color.RGBA{R: c.desktop.R, G: c.desktop.G, B: c.desktop.B, A: 0xff}
	draw.Draw(sheet, sheet.Bounds(), &image.Uniform{C: desk}, image.Point{}, draw.Src)
	draw.Draw(sheet, image.Rect(margin, margin, margin+out.W, margin+out.H),
		out.Image(), image.Point{}, draw.Over)
	return sheet
}

// ── text ────────────────────────────────────────────────────────────────────────────

// GDI bindings for the text masks. The tray has no text renderer of its own any more: the
// window that needed one is gone, so this demo carries the smallest version that works.
var (
	gdi32 = syscall.NewLazyDLL("gdi32.dll")

	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	procSelectObject       = gdi32.NewProc("SelectObject")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procDeleteDC           = gdi32.NewProc("DeleteDC")
	procCreateFontW        = gdi32.NewProc("CreateFontW")
	procSetTextColor       = gdi32.NewProc("SetTextColor")
	procSetBkMode          = gdi32.NewProc("SetBkMode")
	procTextOutW           = gdi32.NewProc("TextOutW")
	procGetTextExtentPoint = gdi32.NewProc("GetTextExtentPoint32W")
	procBitBlt             = gdi32.NewProc("BitBlt")
)

func (c *canvas) open() {
	hdc, _, _ := procCreateCompatibleDC.Call(0)
	c.hdc = hdc
	c.uiFont = createFont(uiFont, uiFallbk, textPx*c.s)
	c.uiSmall = createFont(uiFont, uiFallbk, valuePx*c.s)
	c.icoFont = createFont(iconsFont, iconsFont, 15*c.s)
}

func createFont(face, fallback string, px float64) uintptr {
	// A negative height asks for a font of that pixel size rather than of that point size,
	// which is what keeps the text the size the metrics were computed for.
	h := int32(-px + 0.5)
	for _, name := range []string{face, fallback} {
		f, _, _ := procCreateFontW.Call(uintptr(h), 0, 0, 0, 400, 0, 0, 0, 1 /* DEFAULT_CHARSET */, 0, 0, 5 /* CLEARTYPE_QUALITY */, 0,
			uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(name))))
		if f != 0 {
			return f
		}
	}
	return 0
}

// text draws a string with its left edge and its vertical centre at the given point.
//
// The mask is produced once and composited by hand, rather than drawn straight onto the
// surface, because the surface is a 32-bit image the program owns: GDI would have to draw
// into it through a DC it does not have.
func (c *canvas) text(x, centreX float64, s string, font uintptr, col raster.RGBA) float64 {
	w, h := c.measure(s, font)
	if w == 0 || h == 0 {
		return 0
	}
	mask := c.mask(s, font, w, h)
	if mask == nil {
		return 0
	}
	// The mask is a greyscale coverage map: each byte is how much of the glyph is there,
	// which is what makes the edges smooth once it is blended.
	top := centreX - float64(h)/c.s/2
	for my := 0; my < h; my++ {
		for mx := 0; mx < w; mx++ {
			cov := mask[my*w+mx]
			if cov == 0 {
				continue
			}
			dx := int(c.px(x)) + mx
			dy := int(c.px(top)) + my
			px := raster.RGBA{
				R: col.R,
				G: col.G,
				B: col.B,
				A: uint8(int(col.A) * int(cov) / 255),
			}
			if dx >= 0 && dy >= 0 && dx < c.c.W && dy < c.c.H {
				c.c.BlendAt(dx, dy, px)
			}
		}
	}
	return float64(w) / c.s
}

// width measures a string in logical pixels.
func (c *canvas) width(s string, font uintptr) float64 {
	w, _ := c.measure(s, font)
	return float64(w) / c.s
}

func (c *canvas) measure(s string, font uintptr) (int, int) {
	if c.hdc == 0 || font == 0 || s == "" {
		return 0, 0
	}
	old, _, _ := procSelectObject.Call(c.hdc, font)
	defer procSelectObject.Call(c.hdc, old)

	u := syscall.StringToUTF16(s)
	var sz struct{ cx, cy int32 }
	procGetTextExtentPoint.Call(c.hdc, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1),
		uintptr(unsafe.Pointer(&sz)))
	return int(sz.cx), int(sz.cy)
}

// mask rasterises a string into a coverage map: one byte per pixel, 255 where the glyph is
// solid and less toward the edges.
func (c *canvas) mask(s string, font uintptr, w, h int) []byte {
	if w <= 0 || h <= 0 {
		return nil
	}
	// A one-bit bitmap is enough: the glyph is drawn in white on black and the coverage is
	// read back from the grey levels GDI produces.
	var bmi [40]byte
	*(*uint32)(unsafe.Pointer(&bmi[0])) = 40
	*(*int32)(unsafe.Pointer(&bmi[4])) = int32(w)
	*(*int32)(unsafe.Pointer(&bmi[8])) = -int32(h)
	*(*uint16)(unsafe.Pointer(&bmi[12])) = 1
	*(*uint16)(unsafe.Pointer(&bmi[14])) = 32
	var bits unsafe.Pointer
	hbm, _, _ := procCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&bmi[0])), 0,
		uintptr(unsafe.Pointer(&bits)), 0, 0)
	if hbm == 0 {
		return nil
	}
	defer procDeleteObject.Call(hbm)

	mem, _, _ := procCreateCompatibleDC.Call(0)
	defer procDeleteDC.Call(mem)
	oldBmp, _, _ := procSelectObject.Call(mem, hbm)
	defer procSelectObject.Call(mem, oldBmp)
	oldFont, _, _ := procSelectObject.Call(mem, font)
	defer procSelectObject.Call(mem, oldFont)

	procSetBkMode.Call(mem, 1) // TRANSPARENT
	procSetTextColor.Call(mem, 0x00ffffff)
	u := syscall.StringToUTF16(s)
	procTextOutW.Call(mem, 0, 0, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1))

	src := unsafe.Slice((*byte)(bits), w*h*4)
	out := make([]byte, w*h)
	for i := 0; i < w*h; i++ {
		// The green channel carries the coverage for a white glyph on black.
		out[i] = src[i*4+1]
	}
	return out
}

// ── the menu layout ─────────────────────────────────────────────────────────────────

func drawMenu(c *canvas, s surface) {
	c.desktop = s.Desktop
	w := width
	h := padY * 2
	for _, it := range menu {
		if it.sep {
			h += sepH
			continue
		}
		h += rowH
	}

	// Three rounded rectangles: the surface, the hairline border, and the surface inset by
	// one pixel inside it.
	c.rect(0, 0, w, h, radius, s.Bg)
	c.rect(0.5, 0.5, w-1, h-1, radius, s.Edge)
	c.rect(1.5, 1.5, w-3, h-3, radius-1, s.Bg)

	y := padY
	for _, it := range menu {
		if it.sep {
			c.rule(padX, y+sepH/2, w-padX*2, 1, s.Line)
			y += sepH
			continue
		}
		drawItem(c, s, it, y)
		y += rowH
	}
}

func drawItem(c *canvas, s surface, it item, y float64) {
	const inset = 4.0
	if it.hover {
		c.rect(inset, y, width-inset*2, rowH, itemR, s.Hover)
	}

	centre := y + rowH/2
	textX := padX + iconCol + gap

	if it.dot {
		c.disc(padX+iconCol/2-gap/2, centre, 5, s.Accent)
	}
	if it.tick {
		c.text(padX+2, centre, "\uE73E", c.icoFont, s.Accent)
	} else if it.glyph != "" {
		c.text(padX+2, centre, it.glyph, c.icoFont, s.Ink)
	}
	c.text(textX, centre, it.label, c.uiFont, s.Ink)

	// The right-hand end of the row is laid out from the outside in: the chevron a
	// submenu row carries claims the last column, and the value is placed before it.
	// Letting the two be positioned independently is how they end up touching.
	right := width - padX
	if it.sub {
		const chevron = 12.0
		c.text(right-chevron, centre, "\uE76C", c.icoFont, s.InkDim)
		right -= chevron + gap
	}
	if it.value != "" {
		vw := c.width(it.value, c.uiSmall)
		c.text(right-vw, centre, it.value, c.uiSmall, s.InkDim)
	}
}
