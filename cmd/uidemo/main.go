//go:build windows

// Command uidemo draws the menu the tray would have if it were rebuilt in the Windows 11
// design language, and is a development tool rather than part of the program.
//
// It exists to answer one question before any of that work is committed: what the result
// looks like on this machine, at this scaling, with the accent Windows is using, beside the
// system menu the tray opens today. Every ingredient a real implementation needs is used
// rather than approximated — the operator's accent, the system's own UI font, the Fluent
// icon glyphs, and a supersampled surface — so the picture informs a decision about the
// design rather than about a mock-up.
//
// Usage:
//
//	go run ./cmd/uidemo                       # the menu, dark, in both languages
//	go run ./cmd/uidemo -scene submenu        # a submenu opened over its parent
//	go run ./cmd/uidemo -scene states         # every row state the menu can draw
//	go run ./cmd/uidemo -accent '#c30052'     # under a chosen accent
//	go run ./cmd/uidemo -out design/x.png -scale 1.25
//
// The output is one sheet per language, so a translation that overflows the plate is
// caught by looking at the picture rather than by reading the code.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
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
	Pressed raster.RGBA
	Ink     raster.RGBA
	InkDim  raster.RGBA
	// InkDisabled is what a row that cannot be clicked is drawn in.
	InkDisabled raster.RGBA
	Line        raster.RGBA
	Edge        raster.RGBA
	Accent      raster.RGBA
	Desktop     raster.RGBA
}

func darkSurface(accent raster.RGBA) surface {
	return surface{
		Bg:          raster.Hex("#2b2b2b"),
		Hover:       raster.Hex("#3d3d3d"),
		Pressed:     raster.Hex("#484848"),
		Ink:         raster.Hex("#ffffff"),
		InkDim:      raster.Hex("#c5c5c5"),
		InkDisabled: raster.Hex("#8a8a8a"),
		Line:        raster.Hex("#3f3f3f"),
		Edge:        raster.Hex("#4a4a4a"),
		Accent:      accent,
		Desktop:     raster.Hex("#1f1f1f"),
	}
}

func lightSurface(accent raster.RGBA) surface {
	return surface{
		Bg:          raster.Hex("#f9f9f9"),
		Hover:       raster.Hex("#eaeaea"),
		Pressed:     raster.Hex("#dcdcdc"),
		Ink:         raster.Hex("#1a1a1a"),
		InkDim:      raster.Hex("#5c5c5c"),
		InkDisabled: raster.Hex("#8e8e8e"),
		Line:        raster.Hex("#e5e5e5"),
		Edge:        raster.Hex("#d6d6d6"),
		Accent:      accent,
		Desktop:     raster.Hex("#d8d8d8"),
	}
}

// ── the menu ────────────────────────────────────────────────────────────────────────

// state is what a row is doing, which decides how it is drawn. The set is the one the
// tray's own model has, so the demo exercises the same problems.
type state int

const (
	plain    state = iota // an ordinary row
	hovered               // the pointer is on it
	pressed               // the pointer is down on it
	checked               // a toggle that is on
	disabled              // present but not clickable
	selected              // one of a single-choice group, and chosen
)

// item is one row.
type item struct {
	label string
	value string
	glyph string
	// dot draws the health pip instead of a glyph, and its colour carries the state.
	dot  raster.RGBA
	sub  bool
	sep  bool
	when state
}

// scene is which part of the tray's menu to draw. A menu is several screens in practice —
// the top level, a submenu over it, and the states a row can be in — and drawing them
// separately is how each is checked without the others in the way.
type scene int

const (
	sceneMenu    scene = iota // the top level
	sceneSubmenu              // a submenu opened over its parent
	sceneStates               // every row state on one plate
)

// menuZh and menuEn are the top level in each language.
//
// They are kept apart rather than translated at draw time so that a label that does not
// fit is visible as a wide row rather than as a mystery: the two languages have different
// lengths for nearly every row, and the layout has to hold for both.
var menuZh = []item{
	{label: "Laincat", value: "8,362", dot: raster.Hex("#587500")},
	{sep: true},
	{label: "网关", value: "PID 34604", glyph: "\uE8D7", sub: true, when: hovered},
	{label: "账号", value: "1/1", glyph: "\uE716", sub: true},
	{label: "任务", value: "已启用 5", glyph: "\uE9D5", sub: true},
	{label: "面板", glyph: "\uE774"},
	{label: "其它", glyph: "\uE9D9", sub: true},
	{sep: true},
	{label: "托盘", glyph: "\uE713", sub: true},
	{sep: true},
	{label: "退出", glyph: "\uE7E8"},
}

var menuEn = []item{
	{label: "Laincat", value: "8,362", dot: raster.Hex("#587500")},
	{sep: true},
	{label: "Gateway", value: "PID 34604", glyph: "\uE8D7", sub: true, when: hovered},
	{label: "Accounts", value: "1/1", glyph: "\uE716", sub: true},
	{label: "Tasks", value: "5 enabled", glyph: "\uE9D5", sub: true},
	{label: "Panel", glyph: "\uE774"},
	{label: "Other", glyph: "\uE9D9", sub: true},
	{sep: true},
	{label: "Tray", glyph: "\uE713", sub: true},
	{sep: true},
	{label: "Exit", glyph: "\uE7E8"},
}

// submenuZh is the gateway block, which is the densest of the submenus: a value column,
// two rules, a toggle, and the two copy rows.
var submenuZh = []item{
	{label: "PID", value: "34604"},
	{label: "停止网关", glyph: "\uE71A"},
	{label: "重启网关", glyph: "\uE72C"},
	{label: "控制台", glyph: "\uE756"},
	{sep: true},
	{label: "地址", value: "127.0.0.1:7863", glyph: "\uE8C8"},
	{label: "密钥", glyph: "\uE72E"},
	{sep: true},
	{label: "自启", glyph: "\uE7E8", when: checked},
	{label: "目录", glyph: "\uE8B7"},
	{label: "配置", glyph: "\uE713"},
}

var submenuEn = []item{
	{label: "PID", value: "34604"},
	{label: "Stop gateway", glyph: "\uE71A"},
	{label: "Restart gateway", glyph: "\uE72C"},
	{label: "Console", glyph: "\uE756"},
	{sep: true},
	{label: "Address", value: "127.0.0.1:7863", glyph: "\uE8C8"},
	{label: "API key", glyph: "\uE72E"},
	{sep: true},
	{label: "Autostart", glyph: "\uE7E8", when: checked},
	{label: "Folder", glyph: "\uE8B7"},
	{label: "Config", glyph: "\uE713"},
}

// statesZh and statesEn are one plate showing every state a row can be in, because a state
// that is only drawn in one theme and one context is a state nobody checked.
var statesZh = []item{
	{label: "普通一行", glyph: "\uE8D7"},
	{label: "鼠标悬停", glyph: "\uE8D7", when: hovered},
	{label: "按下", glyph: "\uE8D7", when: pressed},
	{label: "已勾选", glyph: "\uE8D7", when: checked},
	{label: "选项，已选", glyph: "\uE8D7", when: selected},
	{label: "不可点击", glyph: "\uE8D7", when: disabled},
	{label: "带数值", value: "48,250", glyph: "\uE8D7"},
	{label: "带子菜单", value: "1/3", glyph: "\uE8D7", sub: true},
	{label: "状态点", value: "8,362", dot: raster.Hex("#587500")},
	{sep: true},
	{label: "退出", glyph: "\uE7E8"},
}

var statesEn = []item{
	{label: "Row", glyph: "\uE8D7"},
	{label: "Hovered", glyph: "\uE8D7", when: hovered},
	{label: "Pressed", glyph: "\uE8D7", when: pressed},
	{label: "Checked", glyph: "\uE8D7", when: checked},
	{label: "Selected", glyph: "\uE8D7", when: selected},
	{label: "Disabled", glyph: "\uE8D7", when: disabled},
	{label: "With a value", value: "48,250", glyph: "\uE8D7"},
	{label: "Submenu", value: "1/3", glyph: "\uE8D7", sub: true},
	{label: "Status pip", value: "8,362", dot: raster.Hex("#587500")},
	{sep: true},
	{label: "Exit", glyph: "\uE7E8"},
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

	// pipR is the status dot's radius. It is a shape rather than text, so the contrast
	// rule that applies to it is 3:1 against the surface — and because the accent it is
	// drawn in belongs to the operator, a light one on a dark surface can fall below
	// that. It carries a hairline of the surface's own ink, which is what keeps it legible
	// whatever the accent turns out to be.
	pipR = 5.0
)

// uiFont is the family Windows 11 itself uses at this scale, and iconsFont is the glyph
// font the shell's own menus draw their icons from.
const (
	uiFont    = "Segoe UI Variable Text"
	uiFallbk  = "Segoe UI"
	iconsFont = "Segoe Fluent Icons"
)

func main() {
	out := flag.String("out", "", "file to write; a sheet beside it per language when empty")
	scale := flag.Float64("scale", 1.0, "display scaling to render at")
	dark := flag.Bool("dark", true, "render the dark appearance")
	accentHex := flag.String("accent", "", "accent colour as #rrggbb; the system's when empty")
	sc := flag.String("scene", "menu", "which screen to draw: menu, submenu, or states")
	flag.Parse()

	accent, source := accentFor(*accentHex)
	s := darkSurface(accent)
	if !*dark {
		s = lightSurface(accent)
	}

	sceneVal, ok := parseScene(*sc)
	if !ok {
		fmt.Fprintf(os.Stderr, "uidemo: unknown scene %q; want menu, submenu or states\n", *sc)
		os.Exit(2)
	}

	fmt.Printf("accent %s (%s)\n", hex(accent), source)

	// One sheet per language. The two languages have different lengths for nearly every
	// row, so a layout proven in one is not proven at all.
	sheets := []struct {
		lang string
		rows []item
	}{
		{"zh", rowsFor(sceneVal, "zh")},
		{"en", rowsFor(sceneVal, "en")},
	}

	for _, sheet := range sheets {
		path := *out
		if path == "" {
			path = fmt.Sprintf("uidemo-%s.png", sheet.lang)
		} else if len(sheets) > 1 {
			ext := filepath.Ext(path)
			path = path[:len(path)-len(ext)] + "-" + sheet.lang + ext
		}
		if err := render(path, s, sheet.rows, *scale, sceneVal == sceneSubmenu, sheet.lang == "zh"); err != nil {
			fmt.Fprintf(os.Stderr, "uidemo: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("wrote %s\n", path)
	}
}

func parseScene(s string) (scene, bool) {
	switch s {
	case "menu":
		return sceneMenu, true
	case "submenu":
		return sceneSubmenu, true
	case "states":
		return sceneStates, true
	}
	return 0, false
}

func rowsFor(sc scene, lang string) []item {
	zh := lang == "zh"
	switch sc {
	case sceneSubmenu:
		if zh {
			return submenuZh
		}
		return submenuEn
	case sceneStates:
		if zh {
			return statesZh
		}
		return statesEn
	}
	if zh {
		return menuZh
	}
	return menuEn
}

// accentFor picks the accent to draw with: the one asked for, else the system's, else the
// default. The second result says where it came from, so the output can be trusted.
func accentFor(asked string) (raster.RGBA, string) {
	if asked != "" {
		if c := raster.Hex(asked); c.A != 0 {
			return c, "from -accent"
		}
		fmt.Fprintf(os.Stderr, "uidemo: %q is not a colour; using the system's\n", asked)
	}
	packed, ok := winapi.AccentColor()
	if !ok {
		return raster.Hex("#0078d4"), "default; the system's could not be read"
	}
	return raster.RGBA{
		R: uint8(packed & 0xff),
		G: uint8((packed >> 8) & 0xff),
		B: uint8((packed >> 16) & 0xff),
		A: 0xff,
	}, "from Windows"
}

func hex(c raster.RGBA) string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

// render draws one sheet.
//
// The canvas is sized to what the scene actually needs, which for a cascade is wider than
// one plate: a submenu drawn past the edge of the picture is a submenu nobody sees.
func render(path string, s surface, rows []item, scale float64, withParent bool, zh bool) error {
	w, h := width, plateHeight(rows)
	if withParent {
		// The submenu starts at submenuX and is one plate wide, so that is the canvas.
		w = submenuX + width
		h = plateHeight(rows) + padY
	}
	c := newCanvas(w, h, scale)
	c.desktop = s.Desktop
	if withParent {
		drawSubmenuScene(c, s, rows, zh)
	} else {
		drawPlate(c, s, rows, 0, 0, true)
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, c.image())
}

// plateHeight is how tall a plate of these rows is.
func plateHeight(rows []item) float64 {
	h := padY * 2
	for _, it := range rows {
		if it.sep {
			h += sepH
			continue
		}
		h += rowH
	}
	return h
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
	c := &canvas{c: raster.New(int(w*s+0.5), int(h*s+0.5)), scale: scale, s: s}
	c.open()
	return c
}

// px converts a logical coordinate to the raster canvas.
func (c *canvas) px(v float64) float64 { return v * c.s }

func (c *canvas) rect(x, y, w, h, r float64, col raster.RGBA) {
	c.c.RoundedRect(c.px(x), c.px(y), c.px(w), c.px(h), c.px(r), col)
}

func (c *canvas) rule(x, y, w, h float64, col raster.RGBA) {
	c.c.Rect(c.px(x), c.px(y), c.px(w), c.px(h), col)
}

func (c *canvas) disc(x, y, r float64, col raster.RGBA) {
	c.c.Circle(c.px(x), c.px(y), c.px(r), col)
}

func (c *canvas) ring(x, y, ri, ro float64, col raster.RGBA) {
	c.c.Ring(c.px(x), c.px(y), c.px(ri), c.px(ro), col)
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

// GDI bindings for the text masks. The tray has no text renderer of its own any more — the
// window that needed one is gone — so this carries the smallest version that works. Its
// size is the clearest statement of what building a drawn menu for real would cost.
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
)

func (c *canvas) open() {
	hdc, _, _ := procCreateCompatibleDC.Call(0)
	c.hdc = hdc
	c.uiFont = createFont(uiFont, uiFallbk, textPx*c.s)
	c.uiSmall = createFont(uiFont, uiFallbk, valuePx*c.s)
	c.icoFont = createFont(iconsFont, iconsFont, 15*c.s)
}

// createFont opens a font at a pixel size rather than at a point size, which is what keeps
// the text the size the metrics were computed for. The fallback is tried when the named
// family is not on this machine, so a missing Segoe Fluent Icons shows boxes rather than
// nothing.
func createFont(face, fallback string, px float64) uintptr {
	h := int32(-px + 0.5)
	const weight = 400
	for _, name := range []string{face, fallback} {
		f, _, _ := procCreateFontW.Call(uintptr(h), 0, 0, 0, uintptr(weight), 0, 0, 0,
			1 /* DEFAULT_CHARSET */, 0, 0, 5 /* CLEARTYPE_QUALITY */, 0,
			uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(name))))
		if f != 0 {
			return f
		}
	}
	return 0
}

// text draws a string with its left edge and its vertical centre at the given point, and
// returns the width it occupied.
func (c *canvas) text(x, centre float64, s string, font uintptr, col raster.RGBA) float64 {
	w, h := c.measure(s, font)
	if w == 0 || h == 0 {
		return 0
	}
	mask := c.mask(s, font, w, h)
	if mask == nil {
		return 0
	}
	// The mask is a coverage map: each byte is how much of the glyph is there, which is
	// what makes the edges smooth once it is blended.
	top := centre - float64(h)/c.s/2
	for my := 0; my < h; my++ {
		for mx := 0; mx < w; mx++ {
			cov := mask[my*w+mx]
			if cov == 0 {
				continue
			}
			dx := int(c.px(x)) + mx
			dy := int(c.px(top)) + my
			if dx < 0 || dy < 0 || dx >= c.c.W || dy >= c.c.H {
				continue
			}
			c.c.BlendAt(dx, dy, raster.RGBA{
				R: col.R, G: col.G, B: col.B,
				A: uint8(int(col.A) * int(cov) / 255),
			})
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

	// A blank surface, so the coverage is what the glyph added rather than what a fresh
	// DIB happens to contain.
	src := unsafe.Slice((*byte)(bits), w*h*4)
	out := make([]byte, w*h)
	for i := 0; i < w*h; i++ {
		out[i] = src[i*4+1]
	}
	return out
}

// ── drawing ─────────────────────────────────────────────────────────────────────────

// submenuX is where a submenu's left edge sits: one plate wide, less a few pixels of
// overlap. Windows draws a cascade as two plates that touch, and a gap between them reads as
// two unrelated menus rather than as one that opened another.
const submenuX = width - 4.0

// drawSubmenuScene draws a parent menu with a submenu opened over it, which is the state a
// menu is in for most of the time it is on screen and the one where two plates have to agree
// about their alignment.
//
// The parent is the top level in the same language as the submenu, because a cascade drawn
// in two languages is a picture of nothing that can happen.
func drawSubmenuScene(c *canvas, s surface, rows []item, zh bool) {
	parent := menuEn
	if zh {
		parent = menuZh
	}
	// The parent is drawn first and the submenu over it, so the submenu's shadow falls on
	// the parent rather than the other way round.
	drawPlate(c, s, parent, 0, 0, true)
	drawPlate(c, s, rows, submenuX, padY, true)
}

// drawPlate paints one menu surface with its rows.
//
// The surface is three rounded rectangles: the outer is the window's background, the middle
// is the hairline border, and the inner is the background again inset by one pixel. Drawing
// a stroke instead would need a stroked path, and this is the same thing with two fills.
func drawPlate(c *canvas, s surface, rows []item, x, y float64, withSurface bool) {
	w := width
	h := plateHeight(rows)

	if withSurface {
		c.rect(x, y, w, h, radius, s.Bg)
		c.rect(x+0.5, y+0.5, w-1, h-1, radius, s.Edge)
		c.rect(x+1.5, y+1.5, w-3, h-3, radius-1, s.Bg)
	}

	rowY := y + padY
	for _, it := range rows {
		if it.sep {
			c.rule(x+padX, rowY+sepH/2, w-padX*2, 1, s.Line)
			rowY += sepH
			continue
		}
		drawRow(c, s, it, x, rowY)
		rowY += rowH
	}
}

// drawRow paints one row: its highlight, its leading mark, its label, and its value.
func drawRow(c *canvas, s surface, it item, x, y float64) {
	const inset = 4.0
	w := width

	// The row's background, in the precedence a menu uses: a press outranks a hover, and
	// both sit under everything else.
	switch it.when {
	case pressed:
		c.rect(x+inset, y, w-inset*2, rowH, itemR, s.Pressed)
	case hovered, selected:
		c.rect(x+inset, y, w-inset*2, rowH, itemR, s.Hover)
	}

	// A row that cannot be clicked is drawn in a dimmer ink, but its shape stays: an
	// operator has to be able to read what is unavailable and why.
	ink, inkDim := s.Ink, s.InkDim
	accent := s.Accent
	if it.when == disabled {
		ink = s.InkDisabled
		inkDim = s.InkDisabled
		// The pip is the one thing that must not be dimmed into invisibility: it carries
		// the state a disabled row is reporting.
		accent = mix(s.Accent, s.Ink, 0.25)
	}

	centre := y + rowH/2
	markX := x + padX
	textX := x + padX + iconCol + gap

	switch {
	case it.dot.A != 0:
		// The health pip sits in the icon column. It is filled with the accent and ringed
		// in the surface's own ink, because the accent belongs to the operator and a light
		// one on a dark surface is only 2.7:1 — below the 3:1 a shape needs to be seen.
		// The ring is what makes it legible whatever the accent turns out to be.
		cx := markX + iconCol/2 - gap/2
		c.disc(cx, centre, pipR, accent)
		c.ring(cx, centre, pipR, pipR+1.0, mix(s.Bg, s.Ink, 0.45))
	case it.when == checked:
		c.text(markX+2, centre, "\uE73E", c.icoFont, accent)
	case it.glyph != "":
		c.text(markX+2, centre, it.glyph, c.icoFont, ink)
	}

	if it.when == selected {
		// A single-choice row carries a dot in the glyph column, which is what separates
		// "this is the one" from "this is on" — a tick and a dot mean different things.
		c.disc(markX+iconCol/2-gap/2, centre, 3, accent)
	}

	c.text(textX, centre, it.label, c.uiFont, ink)

	// The right-hand end of the row is laid out from the outside in: the chevron a submenu
	// row carries claims the last column, and the value is placed before it. Letting the two
	// be positioned independently is how they end up touching.
	right := x + w - padX
	if it.sub {
		const chevron = 12.0
		c.text(right-chevron, centre, "\uE76C", c.icoFont, inkDim)
		right -= chevron + gap
	}
	if it.value != "" {
		vw := c.width(it.value, c.uiSmall)
		c.text(right-vw, centre, it.value, c.uiSmall, inkDim)
	}
}

// mix blends two colours, which is how the ring and the dimmed pip are derived from the
// surface they are drawn on rather than from constants that only hold for one theme.
func mix(a, b raster.RGBA, t float64) raster.RGBA { return a.Mix(b, t) }
