package iconstyle

import (
	"math"

	"wbtray/internal/cat"
	"wbtray/internal/raster"
	"wbtray/internal/theme"
)

// The program's own marks.
//
// They are not tray readings, and that is the point. A tray style draws the current
// number in the current health colour, which is what makes a glance at the
// notification area useful and what makes it wrong for a file icon — the icon in
// Explorer is drawn once, at build time, and a reading baked into it would be a lie
// by the next day.
//
// So these are identities. Each is drawn from geometry rather than being a picture
// scaled down, and each takes its colours as arguments so the same definition serves
// both places it appears: the application icon, where it sits on a plate in the
// palette's two inks, and the tray, where it is drawn bare in the health colour and
// has to say how the gateway is doing as well as which mark it is.
//
// They exist as a set because which one is right is a judgement about the product
// rather than about the drawing, and the only way to make that judgement is to see
// them side by side.

// AppVariants are the marks, in the order a chooser shows them.
//
// The cat is first because it is the product's own character: the console's
// stylesheet uses it, the tray can wear it, and a program with a face is one people
// recognise in a folder of twenty icons. The rest are shapes.
var AppVariants = []string{"cat", "gauge", "shield", "prompt", "waves", "nodes", "ring"}

// AppVariantLabel is the mark's name for a chooser.
func AppVariantLabel(name string) string {
	switch name {
	case "cat":
		return "Cat"
	case "gauge":
		return "Gauge"
	case "shield":
		return "Shield"
	case "prompt":
		return "Prompt"
	case "waves":
		return "Waves"
	case "nodes":
		return "Nodes"
	case "ring":
		return "Ring"
	}
	return name
}

// DrawAppMark draws one mark as an application icon at a size.
func DrawAppMark(size int, variant string) *raster.Canvas {
	if size < 8 {
		size = 8
	}
	c := raster.New(size*super, size*super)
	p := theme.On(false)

	// The plate. An application icon is a tile rather than a bare glyph, unlike a
	// tray icon, and it fills its box for the same reason: the shell draws it at half
	// a dozen sizes and one that left a margin would look undersized beside every
	// other icon in the folder.
	side := float64(c.W)
	c.RoundedRect(0, 0, side, side, side*0.22, p.Raised)

	accent := p.Green
	ink := p.Text
	switch variant {
	case "cat":
		markCat(c, ink, accent)
	case "shield":
		markShield(c, ink, accent)
	case "prompt":
		markPrompt(c, ink, accent)
	case "waves":
		markWaves(c, ink, accent)
	case "nodes":
		markNodes(c, ink, accent)
	case "ring":
		markRing(c, ink, accent)
	default:
		markGauge(c, ink, accent)
	}
	return c.Downsample(super)
}

// DrawMark draws one mark bare, filling a canvas that is already the right size.
//
// This is the tray half of the same definition: no plate, one colour for the ink and
// one for the accent, which is what lets the tray draw it in the health colour while
// the file icon keeps its own scheme.
func DrawMark(c *raster.Canvas, variant string, ink, accent raster.RGBA) {
	switch variant {
	case "cat":
		markCat(c, ink, accent)
	case "shield":
		markShield(c, ink, accent)
	case "prompt":
		markPrompt(c, ink, accent)
	case "waves":
		markWaves(c, ink, accent)
	case "nodes":
		markNodes(c, ink, accent)
	case "ring":
		markRing(c, ink, accent)
	default:
		markGauge(c, ink, accent)
	}
}

// The marks. Each is drawn to fill its canvas, so the same code works at sixteen
// pixels and at two hundred and fifty-six.

// markGauge is an open gauge with the reading filled, which is the shape the tray's
// own default style uses and so the one an operator has already seen.
func markGauge(c *raster.Canvas, ink, accent raster.RGBA) {
	s := float64(c.W)
	cx, cy := s/2, s*0.56
	ro := s * 0.32
	ri := ro * 0.56
	mid := (ri + ro) / 2
	width := ro - ri
	c.Arc(cx, cy, mid, width, -215, 35, ink)
	c.Arc(cx, cy, mid, width, -215, -55, accent)
	// The tip of the filled arc, which is what makes the reading readable rather
	// than merely present: without it the arc's end is a guess.
	at := -55 * math.Pi / 180
	c.Circle(cx+mid*math.Cos(at), cy+mid*math.Sin(at), width*0.5, accent)
}

// markShield is a shield with the tick knocked out of it.
//
// The knockout is what makes it work at sixteen pixels: an outlined shield at that
// size is a hairline ring with nothing inside, and a solid one with a hole in the
// middle is still a shield.
func markShield(c *raster.Canvas, ink, accent raster.RGBA) {
	s := float64(c.W)
	at := func(x, y float64) raster.Pt { return raster.Pt{X: s * x, Y: s * y} }
	c.Poly([]raster.Pt{
		at(0.50, 0.10), at(0.85, 0.24), at(0.85, 0.50),
		at(0.50, 0.90), at(0.15, 0.50), at(0.15, 0.24),
	}, accent)
	c.Line([]raster.Pt{at(0.33, 0.46), at(0.45, 0.58), at(0.69, 0.31)}, s*0.08, ink)
}

// markPrompt is a caret and the line that follows it.
//
// The strokes are heavier than the other marks' on purpose. A caret is two thin
// diagonals, and at sixteen pixels a thin diagonal is a dotted line: this was drawn
// at the same weight as the rest, and the test that counts the ink an icon puts down
// found it at twenty pixels where the others are at eighty.
func markPrompt(c *raster.Canvas, ink, accent raster.RGBA) {
	s := float64(c.W)
	at := func(x, y float64) raster.Pt { return raster.Pt{X: s * x, Y: s * y} }
	c.Line([]raster.Pt{at(0.22, 0.22), at(0.50, 0.50), at(0.22, 0.78)}, s*0.16, accent)
	c.RoundedRect(s*0.52, s*0.68, s*0.28, s*0.14, s*0.07, ink)
}

// markWaves is three flows, the middle one carrying the accent.
func markWaves(c *raster.Canvas, ink, accent raster.RGBA) {
	s := float64(c.W)
	rows := []struct {
		y, amp float64
		colour raster.RGBA
	}{
		{0.32, 0.055, ink},
		{0.50, 0.075, accent},
		{0.68, 0.055, ink},
	}
	for _, row := range rows {
		const steps = 24
		pts := make([]raster.Pt, 0, steps+1)
		for i := 0; i <= steps; i++ {
			t := float64(i) / steps
			pts = append(pts, raster.Pt{
				X: s * (0.14 + 0.72*t),
				// A half sine, so the line is a wave rather than a wobble.
				Y: s * (row.y - row.amp*math.Sin(t*math.Pi)),
			})
		}
		c.Line(pts, s*0.06, row.colour)
	}
}

// markNodes is the pool: three accounts and the links between them.
func markNodes(c *raster.Canvas, ink, accent raster.RGBA) {
	s := float64(c.W)
	at := func(x, y float64) raster.Pt { return raster.Pt{X: s * x, Y: s * y} }
	top, left, right := at(0.50, 0.20), at(0.20, 0.76), at(0.80, 0.76)
	for _, pair := range [][2]raster.Pt{{top, left}, {top, right}, {left, right}} {
		c.Line([]raster.Pt{pair[0], pair[1]}, s*0.055, ink)
	}
	c.Circle(top.X, top.Y, s*0.13, accent)
	c.Circle(left.X, left.Y, s*0.13, ink)
	c.Circle(right.X, right.Y, s*0.13, ink)
}

// markRing is the quietest of them: a ring with the reading as a filled arc.
func markRing(c *raster.Canvas, ink, accent raster.RGBA) {
	s := float64(c.W)
	cx, cy := s/2, s/2
	ro := s * 0.36
	ri := ro * 0.56
	c.Ring(cx, cy, ri, ro, ink.Mul(0.55))
	c.Arc(cx, cy, (ri+ro)/2, ro-ri, -90, 40, accent)
}

// markCat is the mascot: the same silhouette the console's stylesheet and the tray's
// own mascot style draw, so the three cannot drift apart.
//
// The face is a knockout rather than a second colour, which is why the plate's colour
// is passed as the inner one: the eyes and mouth are whatever is behind the cat, so a
// version of this drawn on a plate has the plate showing through its face and a
// version drawn on the taskbar has the taskbar. A fixed colour there would be a third
// ink in the middle of the mark, and on a status icon a third ink reads as another
// state rather than as a detail.
func markCat(c *raster.Canvas, ink, accent raster.RGBA) {
	s := float64(c.W)
	// The cat is a silhouette rather than a mark, so it is drawn to the corners of its
	// box: a shape has a natural centre and a cat shrunk to the same footprint loses
	// its ears first.
	inner := plateColour(c)
	if inner.A == 0 {
		inner = ink
	}
	cat.Draw(c, s*0.06, s*0.10, s*0.88, s*0.84, cat.Palette{
		Fur:   accent,
		Inner: inner,
		// The cheeks are shading rather than a colour: the coat mixed toward the ink.
		// A fixed pink puts a pair of red spots on the mark, and a red spot on a status
		// icon reads as an alarm.
		Blush:    accent.Mix(ink, 0.45),
		CheeksOn: c.W >= 96,
	})
}

// plateColour is the colour under the mark, read back from the canvas.
//
// It is read rather than passed because the same geometry is drawn in two places — on
// the plate and bare on the taskbar — and the face's knockout has to be whatever is
// actually behind it in each. Reading it from the corner is exact in both cases and
// costs one pixel lookup.
func plateColour(c *raster.Canvas) raster.RGBA { return c.At(1, 1) }
