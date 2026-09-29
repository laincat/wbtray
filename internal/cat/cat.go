// Package cat draws the mascot used by the tray's cat icon style and by the
// application icon's cat mark.
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
var Box = struct{ MinX, MinY, MaxX, MaxY float64 }{192, 108, 832, 842}

type vertex struct{ x, y, r float64 }

// outline is the silhouette: two ear tips, the notch between them, and a broad
// rounded body.
//
// The ears are a quarter of the height and the notch between them is deep, which is
// what makes the shape read as a cat rather than as a rounded head with bumps. An
// earlier version had them a seventh of the height with a shallow notch, and at two
// hundred pixels it read as an egg: the ear is the whole identity of the silhouette
// and everything else is a blob that could be any animal.
//
// The tips are sharp rather than rounded — a radius of twenty-six against a corner
// span of two hundred and thirty — because a rounded tip is a bump and a sharp one is
// an ear. The cheeks and the chin stay generous, which is what keeps it friendly.
var outline = []vertex{
	{300, 108, 26},  // left ear tip
	{432, 300, 58},  // notch, left side
	{592, 300, 58},  // notch, right side
	{724, 108, 26},  // right ear tip
	{832, 716, 100}, // right cheek
	{700, 842, 118}, // bottom right
	{324, 842, 118}, // bottom left
	{192, 716, 100}, // left cheek
}

// Face features in master units.
//
// They are small and set low, which is the other half of reading as a cat: the ears
// carry the top of the head, so the face belongs in the lower half. Eyes the size of
// the ones an earlier version had filled the head and made it a face with a hat
// rather than an animal.
const (
	eyeDX   = 110.0
	eyeY    = 486.0
	eyeR    = 40.0
	noseY   = 552.0
	noseR   = 15.0
	cheekDX = 206.0
	cheekY  = 660.0
	cheekRX = 42.0
	cheekRY = 15.0
	mouthY  = 606.0
	mouthR  = 40.0
	mouthWd = 24.0
)

// Palette is the colours one drawing uses. Inner is the colour drawn through the
// eyes and mouth: the face is a knockout, so what shows there is whatever is
// behind the cat — inside the tray, the halo the whole mark sits on.
type Palette struct {
	Fur   raster.RGBA
	Inner raster.RGBA
	Blush raster.RGBA
	// Halo is stroked around the silhouette when it is set, which is what keeps a
	// light cat apart from a light taskbar behind it.
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

	// The face, if the cat is big enough to have one.
	//
	// The threshold is the point at which an eye is three pixels across. Below that
	// the features stop being a face and become noise: the eyes are two specks, the
	// nose a third, and the nose-and-mouth is a smudge in the middle of the head. A
	// cat silhouette without a face is still unmistakably a cat — the ears carry it —
	// which is why the small sizes drop the features rather than shrinking them.
	if faceVisible(scale) {
		cx := Master / 2
		for _, sx := range []float64{-1, 1} {
			e := toCanvas(cx+sx*eyeDX, eyeY)
			c.Circle(e.X, e.Y, eyeR*scale, p.Inner)
		}
		drawNose(c, toCanvas, scale, p.Inner)
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
}

// faceVisible reports whether the cat is big enough to carry a face.
//
// The test is on the head's own width rather than on a feature's radius, because the
// caller's canvas may be supersampled and this package has no business knowing by how
// much. A head narrower than a fifth of the master space is a head of about forty
// output pixels at the sizes a tray uses, and at that size an eye is two pixels: the
// features stop being a face and become noise, where the silhouette is still
// unmistakably a cat because the ears carry it.
func faceVisible(scale float64) bool {
	head := (Box.MaxX - Box.MinX) * scale
	return head >= 150
}

// drawNose is the small triangle between the eyes, which is what a cat's face has
// where an animal with a muzzle has a snout.
//
// It is drawn as a triangle rather than a dot because a dot there is a second eye:
// three round marks in a column read as a face turned the wrong way round.
func drawNose(c *raster.Canvas, toCanvas func(float64, float64) raster.Pt, scale float64, colour raster.RGBA) {
	cx := Master / 2
	r := noseR * scale
	if r < 0.7 {
		return
	}
	c.Poly([]raster.Pt{
		toCanvas(cx-r, noseY-r*0.6),
		toCanvas(cx+r, noseY-r*0.6),
		toCanvas(cx, noseY+r),
	}, colour)
}

// drawMouth strokes the two short arcs under the nose, in master coordinates so they
// scale with the face.
//
// Two arcs rather than one, meeting at the nose: a single wide arc is a smile, and a
// smile belongs to an emoji rather than to an animal. The pair reads as a muzzle
// without needing the whole lower half of the face, which is what an earlier version
// spent on it.
func drawMouth(c *raster.Canvas, toCanvas func(float64, float64) raster.Pt, scale float64, colour raster.RGBA) {
	const steps = 10
	cx := Master / 2
	wd := mouthWd * scale
	if wd < 0.6 {
		wd = 0.6
	}
	// Each side is a half circle hanging below the nose, from the nose outward. The
	// sweep matters: an arc that goes outward first and down second bulges upward, and
	// two of those are a frown. Each of these is centred half a radius to the side, so
	// the pair meets exactly under the nose.
	for _, sx := range []float64{-1, 1} {
		pts := make([]raster.Pt, 0, steps+1)
		for i := 0; i <= steps; i++ {
			theta := math.Pi * float64(i) / float64(steps)
			pts = append(pts, toCanvas(
				cx+sx*mouthR*(1-math.Cos(theta)),
				mouthY+mouthR*math.Sin(theta),
			))
		}
		c.Line(pts, wd, colour)
	}
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
