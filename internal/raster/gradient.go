package raster

import "math"

// StrokeRoundedRect outlines a rounded rectangle with the given line width, which
// is how the drawn menu gets the frame that separates it from the desktop.
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
