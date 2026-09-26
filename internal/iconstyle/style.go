// Package iconstyle draws the tray icon, every size of it.
//
// The icon is generated rather than shipped as a file, for the same reason the
// rest of this program has no assets: one executable that cannot lose a piece of
// itself. The styles here are the ones the configuration can select, and each is
// drawn from the live numbers rather than from a fixed picture — the point of a
// tray icon in a monitoring tool is that a glance at the taskbar is enough.
package iconstyle

import (
	"fmt"
	"math"

	"wbtray/internal/cat"
	"wbtray/internal/raster"
	"wbtray/internal/status"
	"wbtray/internal/theme"
)

// trackColour is the unfilled part of a gauge.
//
// It is a faint ink rather than a solid colour, because the icon has no plate
// and therefore no background of its own to read against: a track is only ever
// the *absence* of the metric, and it has to be visible without competing with
// the mark that carries it.
func trackColour(t theme.Theme) raster.RGBA { return t.Track }

// super is the supersampling factor: the icon is drawn at this many times the
// final size and averaged down.
const super = 4

// View is everything one icon draw needs.
type View struct {
	Size   int
	Style  string
	Metric string
	Lang   string
	Theme  theme.Theme
	Snap   status.Snapshot
	Paused bool
	// HaloOnly draws the background and nothing else. It exists so a caller can
	// ask what the halo covers for a given style — which is how the containment
	// rule is checked — without a second implementation of the same decision.
	HaloOnly bool
}

// haloInset is how far the halo's edge sits inside the icon, as a fraction of
// its width. A halo that touches the boundary looks like a plate that failed to
// paint; one with a margin reads as a shadow behind the mark.
const haloInset = 0.04

// Draw renders one icon.
func Draw(v View) *raster.Canvas {
	size := v.Size
	if size < 8 {
		size = 8
	}
	c := raster.New(size*super, size*super)
	t := v.Theme

	// The halo is the only background the icon has. It is drawn as a disc rather
	// than as a rounded square so it reads as depth behind the mark instead of as
	// a small plate, and its alpha is what keeps the taskbar visible through it.
	//
	// Its size is per style because the marks are not the same shape: a gauge is
	// round and wants a round halo behind it, while a bar chart or a cat fills
	// its box and needs the background to reach the corners.
	drawHalo(c, t.Halo, v.Style)

	if v.HaloOnly {
		return c.Downsample(super)
	}

	// The accent carries health. The monochrome palette makes no exception here:
	// a healthy mark in it is simply neutral, which is the point of the palette,
	// and the two states that matter are still coloured.
	health := v.Snap.Health()
	accent := t.Health(int(health))
	if v.Paused {
		accent = t.InkDim
	}

	switch v.Style {
	case "ring":
		DrawRing(c, v, accent)
	case "bar":
		DrawBars(c, v, accent)
	case "spark":
		DrawSpark(c, v, accent)
	case "mascot":
		DrawMascot(c, v, accent)
	default:
		DrawPlain(c, v, accent)
	}
	return c.Downsample(super)
}

// drawHalo paints the translucent disc behind the mark.
//
// It fades toward its own edge, which is what stops the transparent background
// from showing a hard circle: the taskbar is visible through the middle of the
// halo and increasingly so toward the rim.
func drawHalo(c *raster.Canvas, colour raster.RGBA, style string) {
	if colour.A == 0 {
		return
	}
	w := float64(c.W)
	// Four concentric shapes of rising alpha rather than a per-pixel falloff: at
	// this size the banding is invisible and it costs a quarter of the work. They
	// grow outward, so the middle of the halo is densest and the taskbar shows
	// through increasingly toward the rim.
	//
	// The shape follows the mark. A gauge is round and a round halo behind it
	// reads as part of the gauge; a bar chart and a cat fill their box, and a
	// circle behind those is narrowest exactly where they are widest — the first
	// version of this drew a cat whose ears stood outside their own shadow.
	round := style == "ring" || style == "plain"
	for i := 3; i >= 0; i-- {
		f := 0.70 + 0.10*float64(i)
		if round {
			r := w * (0.5 - haloInset) * f
			c.Circle(w/2, w/2, r, colour.Mul(0.25))
			continue
		}
		side := w * (1 - 2*haloInset) * f
		c.RoundedRect(w/2-side/2, w/2-side/2, side, side, side*0.32, colour.Mul(0.25))
	}
}

// glyphBox is the drawing area inside the halo, in supersampled units.
func glyphBox(c *raster.Canvas) (x, y, w float64) {
	// The mark sits inside the halo's rim, so the two never share an edge.
	m := float64(c.W) * 0.24
	return m, m, float64(c.W) - 2*m
}

// labelPx is the per-font-pixel size that makes a string fit a given width.
//
// The bitmap font is five pixels wide per glyph with a one-pixel gap, so a
// string of n characters occupies 6n-1 font pixels. Scaling by the height of the
// box instead — which is the obvious thing to write — overflows by a factor of
// about six, and the result is a smear rather than a number.
func labelPx(label string, boxWidth float64) float64 {
	n := len([]rune(label))
	if n == 0 {
		return 0
	}
	return boxWidth / float64(6*n-1)
}

// badge draws the small status pip in the mark's bottom-right corner: filled
// while the tray is listening, hollow while it is paused. It is small on
// purpose — the colour of the main shape already carries the state.
func badge(c *raster.Canvas, v View, accent raster.RGBA) {
	r := float64(c.W) * 0.10
	// The pip sits inside the halo rather than on its rim. Two constraints meet
	// here: a pip at the rim reads as a second, unattached mark, and a pip that
	// reaches past the rim is drawn on the taskbar with no shadow behind it. The
	// distance below is the largest that satisfies both for a disc halo, which is
	// the tighter of the two shapes.
	cx, cy := float64(c.W)*0.5+float64(c.W)*0.23, float64(c.H)*0.5+float64(c.H)*0.23
	if v.Paused {
		// A hollow pip: it needs a background of its own or the halo shows
		// through the hole and it reads as a filled dot of the wrong colour.
		c.Circle(cx, cy, r, v.Theme.Halo)
		c.Ring(cx, cy, r*0.55, r, v.Theme.InkDim)
		return
	}
	c.Circle(cx, cy, r, accent)
}

// fraction converts a metric into 0..1 for the shapes that fill a gauge. The
// ranges are chosen so a glance is meaningful: a percentage is already one, and
// a raw count is measured against the same window's own history rather than
// against a fixed ceiling. A gauge pinned at full for every reading tells the
// reader nothing, and that is exactly what a fixed ceiling produces on a busy
// gateway.
func Fraction(v View) float64 {
	value := v.Snap.MetricValue(v.Metric)
	switch v.Metric {
	case "accounts":
		return Clamp01(value / 100)
	case "credits":
		// Scale against the busiest account's own ceiling when it is known, so
		// a nearly empty pool reads as nearly empty rather than as a small
		// number in a huge range.
		ceiling := creditCeiling(v.Snap)
		if ceiling <= 0 {
			return 0
		}
		return Clamp01(value / ceiling)
	case "requests", "tokens", "latency":
		// The present reading against the busiest bucket in the window: "how
		// busy is it now, compared with how busy it has been".
		if peak := raster.Max(v.Snap.SeriesFor(v.Metric)); peak > 0 {
			return Clamp01(value / peak)
		}
		return provisionalFraction(value, v.Metric)
	case "tps":
		return Clamp01(value / 100)
	case "queue":
		return Clamp01(value / 6)
	}
	return 0
}

// provisionalFraction is the fallback for a metric with no history yet: the same
// rough ceilings the fixed version used, kept only for the first minutes of a
// fresh install, before there is a window to compare against.
func provisionalFraction(value float64, metric string) float64 {
	switch metric {
	case "requests", "tokens":
		return Clamp01(value / 1000)
	case "latency":
		return Clamp01(value / 5000)
	}
	return 0
}

// creditCeiling is the highest balance any account has ever been seen holding,
// taken from the accounts' own totals; a pool with no known ceiling falls back
// to a round number so the gauge still moves.
func creditCeiling(s status.Snapshot) float64 {
	var top float64
	for _, a := range s.Accounts {
		if a.Total > 0 {
			if f := float64(a.Total); f > top {
				top = f
			}
		}
		if f := float64(a.Credits); f*3 > top {
			top = f * 3
		}
	}
	if top <= 0 {
		return 5000
	}
	return top
}

func Clamp01(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return math.Max(0, math.Min(1, v))
}

// drawRing is the gauge: an arc that fills with the metric and a dash grid that
// makes small differences readable at 16 pixels.
func DrawRing(c *raster.Canvas, v View, accent raster.RGBA) {
	_, _, w := glyphBox(c)
	cx, cy := float64(c.W)/2, float64(c.H)/2
	ro := w * 0.5
	ri := ro * 0.62
	c.Ring(cx, cy, ri, ro, trackColour(v.Theme))
	frac := Fraction(v)
	if frac > 0.001 {
		c.Arc(cx, cy, (ri+ro)/2, ro-ri, -90, -90+360*frac, accent)
	}
	// The hub is deliberately empty. A number there would need four glyphs of a
	// five-pixel font, which in a sixteen-pixel icon is under a third of a pixel
	// per font pixel — a grey smudge in the middle of a gauge, which is worse
	// than no number at all. The figures live in the tooltip and the menu, where
	// there is room to read them; a glance at the icon only has to answer
	// "how full, and is it well".
	drawTick(c, cx, cy, (ri+ro)/2, ro-ri, frac, v.Theme.Ink)
	badge(c, v, accent)
}

// drawTick marks the exact value on the gauge. An arc alone leaves the reader
// estimating where it ends; a mark at the end of it is what makes two nearby
// readings distinguishable.
func drawTick(c *raster.Canvas, cx, cy, r, width, frac float64, colour raster.RGBA) {
	if frac <= 0.002 {
		return
	}
	// A short arc at the value's angle, in the same degrees-clockwise convention
	// the ring is drawn in, so the two cannot disagree about where zero is.
	at := -90 + 360*frac
	c.Arc(cx, cy, r, width*0.5, at-3.2, at+3.2, colour)
}

// drawBars is the same metric as a four-bar mini bar chart: the newest quarters
// of the window, so it reads as a trend rather than a level.
func DrawBars(c *raster.Canvas, v View, accent raster.RGBA) {
	x, y, w := glyphBox(c)
	h := w
	bars := 4
	gap := w * 0.10
	bw := (w - gap*float64(bars-1)) / float64(bars)

	series := raster.Tail(v.Snap.SeriesFor(v.Metric), bars)
	values := make([]float64, 0, bars)
	if len(series) >= bars {
		values = append(values, series...)
	} else if len(series) > 0 {
		values = append(values, series...)
	} else {
		// No history for this metric: draw the level as equal bars, which still
		// answers "how full is it" even without a trend.
		for i := 0; i < bars; i++ {
			values = append(values, Fraction(v))
		}
	}
	top := raster.Max(values)
	if top <= 0 {
		top = 1
	}
	for i, value := range values {
		bx := x + float64(i)*(bw+gap)
		bh := h * Clamp01(value/top)
		if bh < h*0.08 {
			bh = h * 0.08
		}
		// Every bar carries the metric; the newest one is at full strength and
		// the rest are dimmed. Drawing the older bars in the unfilled track
		// colour instead — the first version of this style did — makes a bar
		// chart that reads as three empty slots and one full one, which is the
		// opposite of what the numbers say.
		colour := accent
		if i != len(values)-1 {
			colour = accent.Mul(0.55)
		}
		c.RoundedRect(bx, y+h-bh, bw, bh, bw*0.35, colour)
	}
	badge(c, v, accent)
}

// drawSpark is the trend line: the recent history as a polyline, with the last
// point marked. It is the style that makes a burst of traffic visible at a
// glance.
func DrawSpark(c *raster.Canvas, v View, accent raster.RGBA) {
	x, y, w := glyphBox(c)
	series := raster.Tail(v.Snap.SeriesFor(v.Metric), 12)
	if len(series) < 2 {
		// Nothing to plot: fall back to a level bar, which is honest about
		// there being no history yet.
		h := w * 0.22
		c.RoundedRect(x, y+w-h, w*Fraction(v), h, h*0.4, accent)
		c.RoundedRect(x+w*Fraction(v), y+w-h, w*(1-Fraction(v)), h, h*0.4, v.Theme.Track)
		badge(c, v, accent)
		return
	}
	top := raster.Max(series)
	if top <= 0 {
		top = 1
	}
	// The points are smoothed before they are drawn. A twelve-point polyline in a
	// sixteen-pixel icon puts a corner every pixel and a half, and corners at that
	// spacing read as noise rather than as a trend; averaging each point with its
	// neighbours is what turns it back into a curve.
	smoothed := smooth(series, 2)
	pts := make([]raster.Pt, len(smoothed))
	for i, value := range smoothed {
		px := x + w*float64(i)/float64(len(smoothed)-1)
		py := y + w*0.88 - w*0.74*Clamp01(value/top)
		pts[i] = raster.Pt{X: px, Y: py}
	}
	// A soft underlay gives the line weight without a fill under it, which at
	// this size would just look like a smudge.
	c.Line(pts, w*0.30, v.Theme.Glow)
	c.Line(pts, w*0.16, accent)
	last := pts[len(pts)-1]
	c.Circle(last.X, last.Y, w*0.12, accent)
	badge(c, v, accent)
}

// smooth averages each point with its neighbours, keeping the ends fixed so the
// curve still spans the whole box.
func smooth(xs []float64, passes int) []float64 {
	out := append([]float64{}, xs...)
	for p := 0; p < passes; p++ {
		next := append([]float64{}, out...)
		for i := 1; i < len(out)-1; i++ {
			next[i] = (out[i-1] + out[i]*2 + out[i+1]) / 4
		}
		out = next
	}
	return out
}

// drawPlain is the quiet style: a ring around a centre mark, for an operator who
// wants the tray to be present but not loud.
func DrawPlain(c *raster.Canvas, v View, accent raster.RGBA) {
	_, _, w := glyphBox(c)
	cx, cy := float64(c.W)/2, float64(c.H)/2
	// A ring whose bottom quarter is cut away reads as a gauge opening.
	c.Arc(cx, cy, w*0.36, w*0.13, -90, 270, accent)
	if v.Metric == "accounts" {
		frac := Fraction(v)
		c.Arc(cx, cy, w*0.36, w*0.13, -90, -90+360*frac, v.Theme.Accent)
	}
	c.Circle(cx, cy, w*0.13, v.Theme.Ink)
}

// shortLabel is the ring's centre readout: the metric in the fewest characters
// that still mean something.
func ShortLabel(v View) string {
	switch v.Metric {
	case "accounts":
		return fmt.Sprintf("%d", v.Snap.Ready())
	case "credits":
		n := v.Snap.CreditTotal()
		switch {
		case n >= 10000:
			return fmt.Sprintf("%dk", n/1000)
		case n >= 1000:
			return fmt.Sprintf("%.1fk", float64(n)/1000)
		default:
			return fmt.Sprintf("%d", n)
		}
	case "requests", "tokens":
		n := v.Snap.MetricValue(v.Metric)
		switch {
		case n >= 1e6:
			return fmt.Sprintf("%.0fM", n/1e6)
		case n >= 1000:
			return fmt.Sprintf("%.0fk", n/1000)
		default:
			return fmt.Sprintf("%.0f", n)
		}
	case "latency":
		n := v.Snap.MetricValue(v.Metric)
		if n >= 1000 {
			return fmt.Sprintf("%.1fs", n/1000)
		}
		return fmt.Sprintf("%.0f", n)
	case "tps":
		return fmt.Sprintf("%.0f", v.Snap.MetricValue(v.Metric))
	case "queue":
		return fmt.Sprintf("%d", v.Snap.InFlight())
	}
	return ""
}

// drawMascot is the identity style: the same cat the console uses, wearing the
// theme's colours and carrying the health state in its coat.
func DrawMascot(c *raster.Canvas, v View, accent raster.RGBA) {
	t := v.Theme
	// The face is a knockout, so what shows through it is the halo the whole mark
	// sits on, which is what keeps the eyes and mouth legible against the taskbar
	// without drawing them in a colour of their own.
	inner := t.Halo
	if inner.A == 0 {
		inner = t.InkDim
	}
	box := float64(c.W)
	// The cat gets more room than the gauge styles do: it is a silhouette rather
	// than a mark, and at the gauge's size its features collapse into a blob. It
	// still stops short of the halo's rim, so the shadow reads as behind it.
	glyph := box * 0.84
	cat.Draw(c, box/2-glyph/2, box/2-glyph/2+box*0.01, glyph, glyph, cat.Palette{
		Fur:      accent,
		Inner:    inner,
		// The cheeks are shading rather than a colour: the coat mixed toward the
		// palette's own dim ink. A fixed pink — which the first version used —
		// put a pair of red spots on the monochrome cat, and on a status icon a
		// red spot reads as an alarm. Deriving them from the palette means they
		// can only ever be a lighter or darker version of what the cat already
		// is, which is what a cheek should be.
		Blush:    accent.Mix(t.InkDim, 0.55),
		Halo:     t.Halo,
		HaloW:    box * 0.045,
		CheeksOn: c.W >= 64,
	})
	if v.Paused {
		// A pause band across the face is unmistakable at any size, and it does
		// not disturb the silhouette the way a colour change would.
		c.Rect(box/2-glyph/2, box*0.47, glyph, box*0.06, t.InkDim)
	}
	badge(c, v, accent)
}
