// Package raster is the small software renderer the tray icons are drawn with.
//
// Everything is drawn into a canvas that is a few times larger than the final
// icon and then averaged down. That buys antialiasing with no coverage
// accumulator: the only geometry test each shape needs is "is this point
// inside", which is easy to get right and easy to test.
package raster

import (
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"
)

// RGBA is a straight-alpha colour.
type RGBA struct{ R, G, B, A uint8 }

// Hex parses "#rgb", "#rrggbb" or "#rrggbbaa".
func Hex(s string) RGBA {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "#") {
		return RGBA{}
	}
	h := s[1:]
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	if len(h) != 6 && len(h) != 8 {
		return RGBA{}
	}
	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return RGBA{}
	}
	if len(h) == 6 {
		return RGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
	}
	return RGBA{R: uint8(v >> 24), G: uint8(v >> 16), B: uint8(v >> 8), A: uint8(v)}
}

// Mul scales a colour's alpha, which is how a shade of a palette colour is made
// without a second table entry.
func (c RGBA) Mul(t float64) RGBA {
	if t <= 0 {
		return RGBA{}
	}
	if t > 1 {
		t = 1
	}
	return RGBA{c.R, c.G, c.B, uint8(float64(c.A)*t + 0.5)}
}

// Mix returns c blended toward other by t.
func (c RGBA) Mix(other RGBA, t float64) RGBA {
	if t <= 0 {
		return c
	}
	if t > 1 {
		return other
	}
	f := func(a, b uint8) uint8 { return uint8(float64(a) + (float64(b)-float64(a))*t + 0.5) }
	return RGBA{f(c.R, other.R), f(c.G, other.G), f(c.B, other.B), f(c.A, other.A)}
}

// Pt is a point in canvas coordinates.
type Pt struct{ X, Y float64 }

// Canvas is a straight-alpha RGBA raster.
type Canvas struct {
	W, H int
	Pix  []RGBA
}

// New allocates a transparent canvas.
func New(w, h int) *Canvas {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return &Canvas{W: w, H: h, Pix: make([]RGBA, w*h)}
}

// At returns the pixel at x, y.
func (c *Canvas) At(x, y int) RGBA { return c.Pix[y*c.W+x] }

// Set writes the pixel at x, y.
func (c *Canvas) Set(x, y int, v RGBA) { c.Pix[y*c.W+x] = v }

// Fill clears the whole canvas to v.
func (c *Canvas) Fill(v RGBA) {
	for i := range c.Pix {
		c.Pix[i] = v
	}
}

// blend composites s over the pixel at x, y.
func (c *Canvas) blend(x, y int, s RGBA) {
	if s.A == 0 || x < 0 || y < 0 || x >= c.W || y >= c.H {
		return
	}
	i := y*c.W + x
	if s.A == 0xff {
		c.Pix[i] = s
		return
	}
	d := c.Pix[i]
	if d.A == 0 {
		c.Pix[i] = s
		return
	}
	sa := float64(s.A) / 255
	da := float64(d.A) / 255
	oa := sa + da*(1-sa)
	mix := func(a, b uint8) uint8 {
		v := (float64(a)*sa + float64(b)*da*(1-sa)) / oa
		return uint8(math.Min(255, v+0.5))
	}
	c.Pix[i] = RGBA{mix(s.R, d.R), mix(s.G, d.G), mix(s.B, d.B), uint8(math.Min(255, oa*255+0.5))}
}

// BlendAt composites s over the pixel at x, y from outside the package, which is
// how the text rasteriser draws its coverage masks.
func (c *Canvas) BlendAt(x, y int, s RGBA) { c.blend(x, y, s) }

// eachPixel visits every pixel whose centre lies in the bounding box.
func (c *Canvas) eachPixel(x0, y0, x1, y1 float64, f func(x, y int, px, py float64)) {
	ix0 := int(math.Floor(x0))
	iy0 := int(math.Floor(y0))
	ix1 := int(math.Ceil(x1))
	iy1 := int(math.Ceil(y1))
	if ix0 < 0 {
		ix0 = 0
	}
	if iy0 < 0 {
		iy0 = 0
	}
	if ix1 > c.W {
		ix1 = c.W
	}
	if iy1 > c.H {
		iy1 = c.H
	}
	for y := iy0; y < iy1; y++ {
		for x := ix0; x < ix1; x++ {
			f(x, y, float64(x)+0.5, float64(y)+0.5)
		}
	}
}

// Rect fills an axis-aligned rectangle.
func (c *Canvas) Rect(x, y, w, h float64, v RGBA) {
	if w <= 0 || h <= 0 || v.A == 0 {
		return
	}
	c.eachPixel(x, y, x+w, y+h, func(ix, iy int, px, py float64) {
		if px >= x && px < x+w && py >= y && py < y+h {
			c.blend(ix, iy, v)
		}
	})
}

// RoundedRect fills a rectangle with rounded corners.
func (c *Canvas) RoundedRect(x, y, w, h, r float64, v RGBA) {
	if w <= 0 || h <= 0 || v.A == 0 {
		return
	}
	if r > w/2 {
		r = w / 2
	}
	if r > h/2 {
		r = h / 2
	}
	if r < 0 {
		r = 0
	}
	c.eachPixel(x, y, x+w, y+h, func(ix, iy int, px, py float64) {
		dx := math.Max(math.Max(x+r-px, px-(x+w-r)), 0)
		dy := math.Max(math.Max(y+r-py, py-(y+h-r)), 0)
		if dx*dx+dy*dy <= r*r {
			c.blend(ix, iy, v)
		}
	})
}

// Circle fills a disc.
func (c *Canvas) Circle(cx, cy, r float64, v RGBA) {
	if r <= 0 || v.A == 0 {
		return
	}
	c.eachPixel(cx-r, cy-r, cx+r, cy+r, func(ix, iy int, px, py float64) {
		if dx, dy := px-cx, py-cy; dx*dx+dy*dy <= r*r {
			c.blend(ix, iy, v)
		}
	})
}

// Ring fills the disc of radius ro minus the disc of radius ri.
func (c *Canvas) Ring(cx, cy, ri, ro float64, v RGBA) {
	if ro <= ri || v.A == 0 {
		return
	}
	c.eachPixel(cx-ro, cy-ro, cx+ro, cy+ro, func(ix, iy int, px, py float64) {
		d2 := (px-cx)*(px-cx) + (py-cy)*(py-cy)
		if d2 <= ro*ro && d2 >= ri*ri {
			c.blend(ix, iy, v)
		}
	})
}

// Arc strokes the arc of a circle between two angles in degrees. Screen
// convention: 0° points right and angles grow clockwise, because y grows down.
func (c *Canvas) Arc(cx, cy, r, width, from, to float64, v RGBA) {
	if r <= 0 || width <= 0 || v.A == 0 || to <= from {
		return
	}
	half := width / 2
	c.eachPixel(cx-r-half, cy-r-half, cx+r+half, cy+r+half, func(ix, iy int, px, py float64) {
		dx, dy := px-cx, py-cy
		d := math.Hypot(dx, dy)
		if math.Abs(d-r) > half {
			return
		}
		a := math.Atan2(dy, dx) * 180 / math.Pi
		for a < from {
			a += 360
		}
		if a <= to {
			c.blend(ix, iy, v)
		}
	})
}

// Poly fills a polygon by the even-odd rule.
func (c *Canvas) Poly(pts []Pt, v RGBA) {
	if len(pts) < 3 || v.A == 0 {
		return
	}
	minX, minY := pts[0].X, pts[0].Y
	maxX, maxY := minX, minY
	for _, p := range pts {
		minX, minY = math.Min(minX, p.X), math.Min(minY, p.Y)
		maxX, maxY = math.Max(maxX, p.X), math.Max(maxY, p.Y)
	}
	c.eachPixel(minX, minY, maxX, maxY, func(ix, iy int, px, py float64) {
		if InsidePoly(pts, px, py) {
			c.blend(ix, iy, v)
		}
	})
}

// InsidePoly is an even-odd ray cast.
func InsidePoly(pts []Pt, x, y float64) bool {
	inside := false
	for i, j := 0, len(pts)-1; i < len(pts); j, i = i, i+1 {
		yi, yj := pts[i].Y, pts[j].Y
		if (yi > y) == (yj > y) {
			continue
		}
		xi, xj := pts[i].X, pts[j].X
		if x < (xj-xi)*(y-yi)/(yj-yi)+xi {
			inside = !inside
		}
	}
	return inside
}

// Line strokes a polyline with round caps.
func (c *Canvas) Line(pts []Pt, width float64, v RGBA) {
	if len(pts) == 0 || width <= 0 || v.A == 0 {
		return
	}
	half := width / 2
	segOK := func(px, py float64) bool {
		if len(pts) == 1 {
			return math.Hypot(px-pts[0].X, py-pts[0].Y) <= half
		}
		for i := 1; i < len(pts); i++ {
			if distToSegment(px, py, pts[i-1], pts[i]) <= half {
				return true
			}
		}
		return false
	}
	minX, minY := pts[0].X, pts[0].Y
	maxX, maxY := minX, minY
	for _, p := range pts {
		minX, minY = math.Min(minX, p.X), math.Min(minY, p.Y)
		maxX, maxY = math.Max(maxX, p.X), math.Max(maxY, p.Y)
	}
	c.eachPixel(minX-half, minY-half, maxX+half, maxY+half, func(ix, iy int, px, py float64) {
		if segOK(px, py) {
			c.blend(ix, iy, v)
		}
	})
}

func distToSegment(px, py float64, a, b Pt) float64 {
	dx, dy := b.X-a.X, b.Y-a.Y
	l2 := dx*dx + dy*dy
	if l2 == 0 {
		return math.Hypot(px-a.X, py-a.Y)
	}
	t := ((px-a.X)*dx + (py-a.Y)*dy) / l2
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(px-(a.X+t*dx), py-(a.Y+t*dy))
}

// Downsample averages n×n blocks into a canvas n times smaller. The colour
// average is computed premultiplied, so a soft edge does not drag the colour of
// transparent neighbours into the shape.
func (c *Canvas) Downsample(n int) *Canvas {
	if n <= 1 {
		out := New(c.W, c.H)
		copy(out.Pix, c.Pix)
		return out
	}
	w, h := c.W/n, c.H/n
	out := New(w, h)
	area := float64(n * n)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var r, g, b, a float64
			for sy := 0; sy < n; sy++ {
				for sx := 0; sx < n; sx++ {
					p := c.At(x*n+sx, y*n+sy)
					al := float64(p.A) / 255
					r += float64(p.R) * al
					g += float64(p.G) * al
					b += float64(p.B) * al
					a += al
				}
			}
			if a == 0 {
				continue
			}
			out.Set(x, y, RGBA{
				R: uint8(math.Min(255, r/a+0.5)),
				G: uint8(math.Min(255, g/a+0.5)),
				B: uint8(math.Min(255, b/a+0.5)),
				A: uint8(math.Min(255, a/area*255+0.5)),
			})
		}
	}
	return out
}

// Image converts to an image.RGBA, for PNG output.
func (c *Canvas) Image() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, c.W, c.H))
	for y := 0; y < c.H; y++ {
		for x := 0; x < c.W; x++ {
			p := c.At(x, y)
			img.SetRGBA(x, y, color.RGBA{p.R, p.G, p.B, p.A})
		}
	}
	return img
}

// BGRA returns the pixels premultiplied and in BGRA order, which is the layout
// a 32-bit top-down Windows DIB section holds.
func (c *Canvas) BGRA() []byte {
	out := make([]byte, c.W*c.H*4)
	for y := 0; y < c.H; y++ {
		for x := 0; x < c.W; x++ {
			p := c.At(x, y)
			a := float64(p.A)
			i := (y*c.W + x) * 4
			out[i+0] = uint8(float64(p.B)*a/255 + 0.5)
			out[i+1] = uint8(float64(p.G)*a/255 + 0.5)
			out[i+2] = uint8(float64(p.R)*a/255 + 0.5)
			out[i+3] = p.A
		}
	}
	return out
}

// Scale resamples the canvas to a different size by nearest sampling, which is
// only used to preview an icon at larger sizes than the shell ever asks for.
func (c *Canvas) Scale(w, h int) *Canvas {
	out := New(w, h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			sx := x * c.W / w
			sy := y * c.H / h
			out.Set(x, y, c.At(sx, sy))
		}
	}
	return out
}

// DrawCanvas composites src over c with its top-left corner at dx, dy. An alpha
// multiplier lets one shape be shared by a normal draw and a faint glow.
func (c *Canvas) DrawCanvas(src *Canvas, dx, dy int, alpha float64) {
	if src == nil || alpha <= 0 {
		return
	}
	for y := 0; y < src.H; y++ {
		ty := dy + y
		if ty < 0 || ty >= c.H {
			continue
		}
		for x := 0; x < src.W; x++ {
			tx := dx + x
			if tx < 0 || tx >= c.W {
				continue
			}
			p := src.At(x, y)
			if p.A == 0 {
				continue
			}
			c.blend(tx, ty, p.Mul(alpha))
		}
	}
}

// Max returns the largest value in a series, or 0 when it is empty.
func Max(xs []float64) float64 {
	var m float64
	for _, x := range xs {
		if x > m {
			m = x
		}
	}
	return m
}

// Tail returns the last n values of a series, or the whole series when it is
// shorter. A chart in a 16-pixel icon has room for a handful of points, and the
// recent ones are the ones worth seeing.
func Tail(xs []float64, n int) []float64 {
	if n <= 0 || len(xs) <= n {
		return xs
	}
	return xs[len(xs)-n:]
}
