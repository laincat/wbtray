//go:build windows

package textmask

import (
	"strconv"
	"sync"
	"unsafe"

	"wbtray/internal/raster"
	"wbtray/internal/winapi"
)

// Text rendering.
//
// The icons are drawn by the software rasteriser in internal/raster, which
// knows nothing about fonts, and the flyout needs both Chinese and Latin text.
// Rather than ship a second, larger rasteriser for glyphs, each string is
// rasterised once by GDI into an off-screen bitmap, read back as a coverage
// mask, and composited by the same code that draws everything else. That keeps
// one rendering path, gets real font hinting and CJK shaping for free, and lets
// the text be blended with the same premultiplied arithmetic as the shapes.

// GDI constants used here.
const (
	antialiasedQuality = 5
	cleartypeQuality   = 6
	variationNormal    = 0

	gmAdvanced = 2

	fwNormal = 400
	fwMedium = 500
	fwBold   = 700
)

// Font identifies a rasterised face. Size is in pixels, which is what a
// DPI-scaled layout wants: the caller multiplies by the window's scale factor.
type Font struct {
	size int
	bold bool
}

// NewFont describes a face: size in pixels, and whether it is bold.
func NewFont(size int, bold bool) Font { return Font{size: size, bold: bold} }

// Size is the face's pixel height.
func (f Font) Size() int { return f.size }

// textMask is one string's coverage: 0 is fully transparent, 255 fully opaque.
type textMask struct {
	w, h int
	cov  []uint8
}

// maskCache keeps rasterised strings around. A flyout repaints on every hover
// change, and re-rasterising the same twenty labels through GDI each time is
// both slow and visible.
var maskCache = struct {
	sync.Mutex
	m map[string]*textMask
}{m: map[string]*textMask{}}

// maxMaskCache bounds the cache; a tray left running for weeks should not grow
// one entry per log line it ever displayed.
const maxMaskCache = 512

func maskKey(s string, spec Font) string {
	if spec.bold {
		return "b" + strconv.Itoa(spec.size) + ":" + s
	}
	return "r" + strconv.Itoa(spec.size) + ":" + s
}

// measure reports the size a string occupies at a given face, which the layout
// needs before anything is drawn.
func Measure(s string, spec Font) (int, int) {
	m := textMaskFor(s, spec)
	if m == nil {
		// A string of only spaces still occupies its advance width; treating it
		// as empty would collapse the gap it was put there to make.
		return spec.size / 3 * len(s), spec.size
	}
	return m.w, m.h
}

// textMaskFor rasterises a string, or returns it from the cache.
func textMaskFor(s string, spec Font) *textMask {
	if s == "" || spec.size < 4 {
		return nil
	}
	key := maskKey(s, spec)
	maskCache.Lock()
	if m, ok := maskCache.m[key]; ok {
		maskCache.Unlock()
		return m
	}
	maskCache.Unlock()

	m := rasteriseString(s, spec)
	if m == nil {
		return nil
	}
	maskCache.Lock()
	// A blunt eviction: the cache is a speed-up, not state, so dropping it is
	// always correct and never worth an LRU.
	if len(maskCache.m) >= maxMaskCache {
		maskCache.m = map[string]*textMask{}
	}
	maskCache.m[key] = m
	maskCache.Unlock()
	return m
}

// rasteriseString draws s in white on black through GDI and keeps the red
// channel as coverage.
//
// Grayscale antialiasing is requested rather than ClearType: subpixel rendering
// only makes sense when the text lands on the actual screen surface, and this
// mask is composited by the rasteriser, where a coloured fringe would show up as
// a colour fringe.
func rasteriseString(s string, spec Font) *textMask {
	const (
		padX = 3
		padY = 3
	)
	hdcScreen, _, _ := winapi.ProcGetDC.Call(0)
	if hdcScreen == 0 {
		return nil
	}
	defer winapi.ProcReleaseDC.Call(0, hdcScreen)

	hdc, _, _ := winapi.ProcCreateCompatibleDC.Call(hdcScreen)
	if hdc == 0 {
		return nil
	}
	defer winapi.ProcDeleteDC.Call(hdc)

	// The bitmap is generously oversized and then cropped to the ink, so a
	// descender or a wide glyph can never be clipped by a wrong metric.
	bw, bh := spec.size*(len([]rune(s))+2), spec.size*3
	hbm, bits := winapi.NewDIBSection(bw, bh)
	if hbm == 0 {
		return nil
	}
	defer winapi.ProcDeleteObject.Call(hbm)
	old, _, _ := winapi.ProcSelectObject.Call(hdc, hbm)
	defer winapi.ProcSelectObject.Call(hdc, old)

	// Black background, white ink: the mask is the luminance of the result.
	black, _, _ := winapi.ProcCreateSolidBrush.Call(0x00000000)
	defer winapi.ProcDeleteObject.Call(black)
	rect := winapi.Rect{Left: 0, Top: 0, Right: int32(bw), Bottom: int32(bh)}
	winapi.ProcFillRect.Call(hdc, uintptr(unsafe.Pointer(&rect)), black)

	font := createFont(spec)
	if font == 0 {
		return nil
	}
	defer winapi.ProcDeleteObject.Call(font)
	oldFont, _, _ := winapi.ProcSelectObject.Call(hdc, font)
	defer winapi.ProcSelectObject.Call(hdc, oldFont)

	winapi.ProcSetTextColor.Call(hdc, 0x00ffffff)
	winapi.ProcSetBkMode.Call(hdc, 1) // TRANSPARENT
	winapi.ProcTextOutW.Call(hdc, uintptr(padX), uintptr(padY),
		uintptr(unsafe.Pointer(winapi.UTF16Ptr(s))), uintptr(len([]rune(s))))

	// Crop to the ink so the mask is exactly the string's box and the layout
	// does not have to know about the padding.
	minX, minY, maxX, maxY := bw, bh, -1, -1
	src := unsafe.Slice((*byte)(bits), bw*bh*4)
	for y := 0; y < bh; y++ {
		for x := 0; x < bw; x++ {
			if src[(y*bw+x)*4] > 8 {
				if x < minX {
					minX = x
				}
				if y < minY {
					minY = y
				}
				if x > maxX {
					maxX = x
				}
				if y > maxY {
					maxY = y
				}
			}
		}
	}
	if maxX < 0 {
		// All space: no ink to crop to. The mask still carries measurements and a
		// coverage buffer that matches them, because a mask whose buffer
		// disagrees with its size is a trap for anything that walks it — which is
		// exactly how the drawn menu crashed the first time it was handed a
		// label of nothing but spaces.
		w := int(float64(spec.size) * 0.3 * float64(len([]rune(s))))
		if w < 1 {
			w = 1
		}
		return &textMask{w: w, h: spec.size, cov: make([]uint8, w*spec.size)}
	}
	w, h := maxX-minX+1, maxY-minY+1
	cov := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			cov[y*w+x] = src[((y+minY)*bw+(x+minX))*4]
		}
	}
	return &textMask{w: w, h: h, cov: cov}
}

// createFont builds a font of the requested size and weight.
//
// Segoe UI is asked for by name; the CJK glyphs come from the system's own font
// linking, which is what makes one call serve both languages.
func createFont(spec Font) uintptr {
	weight := int32(fwNormal)
	if spec.bold {
		weight = fwBold
	}
	lf := winapi.LogFont{
		Height:         -int32(spec.size),
		Weight:         weight,
		CharSet:        1, // DEFAULT_CHARSET, so the linker picks CJK when needed
		OutPrecision:   0,
		Quality:        antialiasedQuality,
		PitchAndFamily: 0,
	}
	name := winapi.UTF16Ptr("Segoe UI")
	copy(lf.FaceName[:], unsafe.Slice(name, len("Segoe UI")+1))
	// CreateFontIndirectW takes the LOGFONT itself. CreateFontW is the fourteen
	// argument version of the same call, and reaching for it with a single
	// pointer silently puts that pointer in nHeight and leaves the face name to
	// whatever was in the register — which is how the first version of this
	// function produced a perfect layout with no glyphs in it.
	font, _, _ := winapi.ProcCreateFontIndirectW.Call(uintptr(unsafe.Pointer(&lf)))
	return font
}

// drawText composites a string into the canvas at x, y with its top-left corner
// there. It returns the width it occupied, so a caller can lay out a row from
// left to right without measuring first.
func Draw(c *raster.Canvas, x, y float64, spec Font, s string, colour raster.RGBA) float64 {
	if c == nil || s == "" || colour.A == 0 {
		return 0
	}
	m := textMaskFor(s, spec)
	if m == nil {
		return 0
	}
	DrawMask(c, x, y, m, colour)
	return float64(m.w)
}

// drawMask blends a coverage mask into the canvas in one colour.
func DrawMask(c *raster.Canvas, x, y float64, m *textMask, colour raster.RGBA) {
	// Nothing to draw is not an error: an empty label, or one that rasterised to
	// no ink, is an ordinary outcome for a menu with a blank cell in it.
	if m == nil || m.w <= 0 || m.h <= 0 || len(m.cov) < m.w*m.h {
		return
	}
	ox, oy := int(x+0.5), int(y+0.5)
	for row := 0; row < m.h; row++ {
		ty := oy + row
		if ty < 0 || ty >= c.H {
			continue
		}
		for col := 0; col < m.w; col++ {
			cov := m.cov[row*m.w+col]
			if cov == 0 {
				continue
			}
			tx := ox + col
			if tx < 0 || tx >= c.W {
				continue
			}
			c.BlendAt(tx, ty, colour.Mul(float64(cov)/255))
		}
	}
}

// drawTextRight draws a string with its right edge at x, which is how a value
// column is aligned.
func DrawRight(c *raster.Canvas, x, y float64, spec Font, s string, colour raster.RGBA) {
	if s == "" {
		return
	}
	w, _ := Measure(s, spec)
	Draw(c, x-float64(w), y, spec, s, colour)
}

// drawTextCenter draws a string centred on x.
func DrawCenter(c *raster.Canvas, x, y float64, spec Font, s string, colour raster.RGBA) {
	if s == "" {
		return
	}
	w, _ := Measure(s, spec)
	Draw(c, x-float64(w)/2, y, spec, s, colour)
}

// truncate shortens a string until it fits in maxWidth, with an ellipsis, which
// is what keeps a long nickname or a log line from running off a panel edge.
func Truncate(s string, spec Font, maxWidth int) string {
	if s == "" {
		return s
	}
	if w, _ := Measure(s, spec); w <= maxWidth {
		return s
	}
	runes := []rune(s)
	for len(runes) > 1 {
		runes = runes[:len(runes)-1]
		candidate := string(runes) + "…"
		if w, _ := Measure(candidate, spec); w <= maxWidth {
			return candidate
		}
	}
	return ""
}
