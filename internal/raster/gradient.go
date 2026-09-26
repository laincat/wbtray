package raster

import "math"

// RoundedRectGradient fills a rounded rectangle with a vertical gradient, which
// is what gives a plate its depth without a second image asset.
func (c *Canvas) RoundedRectGradient(x, y, w, h, r float64, top, bottom RGBA) {
	if w <= 0 || h <= 0 {
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
		if dx*dx+dy*dy > r*r {
			return
		}
		t := (py - y) / h
		c.blend(ix, iy, top.Mix(bottom, t))
	})
}

// StrokeRoundedRect outlines a rounded rectangle with the given line width.
func (c *Canvas) StrokeRoundedRect(x, y, w, h, r, width float64, v RGBA) {
	if w <= 0 || h <= 0 || width <= 0 || v.A == 0 {
		return
	}
	if r > w/2 {
		r = w / 2
	}
	if r > h/2 {
		r = h / 2
	}
	half := width / 2
	c.eachPixel(x-half, y-half, x+w+half, y+h+half, func(ix, iy int, px, py float64) {
		d := roundedRectDistance(px, py, x, y, w, h, r)
		if math.Abs(d) <= half {
			c.blend(ix, iy, v)
		}
	})
}

// roundedRectDistance is the signed distance to a rounded rectangle's edge:
// negative inside, positive outside.
func roundedRectDistance(px, py, x, y, w, h, r float64) float64 {
	cx, cy := x+w/2, y+h/2
	dx := math.Abs(px-cx) - (w/2 - r)
	dy := math.Abs(py-cy) - (h/2 - r)
	outside := math.Hypot(math.Max(dx, 0), math.Max(dy, 0))
	inside := math.Min(math.Max(dx, dy), 0)
	return outside + inside - r
}
