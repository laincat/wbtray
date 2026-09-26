// Package cat draws the mascot used by the tray's mascot icon style and by the
// chart window's title.
//
// The geometry is a rounded-polygon cat expressed in a 1024-unit master space,
// so a size change is a scale change rather than a redraw. It is deliberately a
// small amount of code: a mascot that has to be re-drawn by hand at every size
// is a mascot that stops matching itself.
package cat

import (
	"math"

	"wbtray/internal/raster"
)

// Master is the coordinate space the outline is authored in.
const Master = 1024.0

// Box is the cat's extent inside the master space. The outline does not use the
// whole canvas, and fitting this box rather than the canvas is what keeps the
// proportions right at every size.
var Box = struct{ MinX, MinY, MaxX, MaxY float64 }{204, 150, 820, 830}

type vertex struct{ x, y, r float64 }

// outline is the silhouette: two ear tips, the notch between them, and a broad
// rounded body.
var outline = []vertex{
	{318, 150, 34},  // left ear tip
	{448, 245, 46},  // notch, left side
	{576, 245, 46},  // notch, right side
	{706, 150, 34},  // right ear tip
	{820, 734, 104}, // right cheek
	{696, 830, 116}, // bottom right
	{328, 830, 116}, // bottom left
	{204, 650, 104}, // left cheek
}

// Face features in master units.
const (
	eyeDX   = 101.0
	eyeY    = 446.0
	eyeR    = 49.0
	cheekDX = 216.0
	cheekY  = 588.0
	cheekRX = 60.0
	cheekRY = 23.0
	mouthY  = 540.0
	mouthR  = 52.0
	mouthWd = 34.0
)

// Palette is the colours one drawing uses. Inner is the colour drawn through
// the eyes and mouth: the face is a knockout, so what shows there is whatever
// is behind the cat, which inside the tray is the plate.
type Palette struct {
	Fur   raster.RGBA
	Inner raster.RGBA
	Blush raster.RGBA
	// Halo is stroked around the silhouette when it is set. A cat drawn on a
	// transparent taskbar needs it; one drawn on a plate does not.
	Halo     raster.RGBA
	HaloW    float64
	CheeksOn bool
}

// Draw renders the cat into the given box of the canvas.
func Draw(c *raster.Canvas, x, y, w, h float64, p Palette) {
	if w <= 0 || h <= 0 {
		return
	}
	boxW := Box.MaxX - Box.MinX
	boxH := Box.MaxY - Box.MinY
	// Uniform scale, so the cat keeps its proportions instead of stretching to
	// whatever box it is handed.
	scale := math.Min(w/boxW, h/boxH)
	offX := x + (w-boxW*scale)/2
	offY := y + (h-boxH*scale)/2

	toCanvas := func(mx, my float64) raster.Pt {
		return raster.Pt{
			X: offX + (mx-Box.MinX)*scale,
			Y: offY + (my-Box.MinY)*scale,
		}
	}

	shape := flatten(outline)
	pts := make([]raster.Pt, len(shape))
	for i, v := range shape {
		pts[i] = toCanvas(v[0], v[1])
	}

	if p.HaloW > 0 {
		// The halo is the silhouette stroked in the halo colour, which is what
		// keeps a light cat readable on a light taskbar.
		c.Line(append(append([]raster.Pt{}, pts...), pts[0]), p.HaloW, p.Halo)
	}
	c.Poly(pts, p.Fur)

	// The knockout: eyes, mouth, then the blush over the coat.
	cx := Master / 2
	for _, sx := range []float64{-1, 1} {
		e := toCanvas(cx+sx*eyeDX, eyeY)
		c.Circle(e.X, e.Y, eyeR*scale, p.Inner)
	}
	drawMouth(c, toCanvas, scale, p.Inner)
	if p.CheeksOn {
		for _, sx := range []float64{-1, 1} {
			b := toCanvas(cx+sx*cheekDX, cheekY)
			// A circle is not an ellipse, and the cheeks are wider than they are
			// tall: drawing three overlapping circles along the x axis gives the
			// right proportions without an ellipse rasteriser.
			for _, dx := range []float64{-cheekRX * 0.5, 0, cheekRX * 0.5} {
				c.Circle(b.X+dx*scale, b.Y, cheekRY*scale, p.Blush)
			}
		}
	}
}

// drawMouth strokes the lower arc of the mouth, in master coordinates so it
// scales with the face.
func drawMouth(c *raster.Canvas, toCanvas func(float64, float64) raster.Pt, scale float64, colour raster.RGBA) {
	const steps = 12
	cx := Master / 2
	wd := mouthWd * scale
	if wd < 0.6 {
		wd = 0.6
	}
	pts := make([]raster.Pt, 0, steps+1)
	for i := 0; i <= steps; i++ {
		theta := math.Pi * float64(i) / steps
		pts = append(pts, toCanvas(cx+mouthR*math.Cos(theta), mouthY+mouthR*math.Sin(theta)))
	}
	c.Line(pts, wd, colour)
}

// flatten turns the rounded polygon into the point list the fill test uses: a
// straight run into each corner, then a quadratic whose control point is the
// original vertex.
func flatten(verts []vertex) [][2]float64 {
	const perCorner = 10
	n := len(verts)
	out := make([][2]float64, 0, n*(perCorner+1))
	unit := func(dx, dy float64) (float64, float64) {
		l := math.Hypot(dx, dy)
		if l == 0 {
			return 0, 0
		}
		return dx / l, dy / l
	}
	for i, v := range verts {
		prev := verts[(i-1+n)%n]
		next := verts[(i+1)%n]
		// A corner may eat at most half of its shorter edge, or adjacent corners
		// would overlap and the outline would cross itself.
		span := math.Min(math.Hypot(prev.x-v.x, prev.y-v.y), math.Hypot(next.x-v.x, next.y-v.y))
		r := math.Min(v.r, span/2-1)
		uax, uay := unit(prev.x-v.x, prev.y-v.y)
		ubx, uby := unit(next.x-v.x, next.y-v.y)
		enterX, enterY := v.x+uax*r, v.y+uay*r
		exitX, exitY := v.x+ubx*r, v.y+uby*r
		out = append(out, [2]float64{enterX, enterY})
		for s := 1; s <= perCorner; s++ {
			t := float64(s) / perCorner
			u := 1 - t
			out = append(out, [2]float64{
				u*u*enterX + 2*u*t*v.x + t*t*exitX,
				u*u*enterY + 2*u*t*v.y + t*t*exitY,
			})
		}
	}
	return out
}
