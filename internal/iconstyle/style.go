// Package iconstyle draws the tray icon, every size of it.
//
// The icon is generated rather than shipped as a file, for the same reason the
// rest of this program has no assets: one executable that cannot lose a piece of
// itself.
//
// Nothing here draws a background. A tray icon sits on the taskbar beside the
// system's own, and those are bare glyphs; a glyph on a plate reads as a square
// of the wrong colour pasted over the notification area. An earlier version of
// this package drew one — a translucent rounded square, meant as a shadow — and
// on a dark taskbar it came out as exactly the black box the design was trying to
// avoid. Legibility comes from the colours instead: every mark is drawn in an ink
// the palette has already checked against the taskbar it will be read on.
package iconstyle

import (
	"fmt"
	"math"

	"wbtray/internal/cat"
	"wbtray/internal/raster"
	"wbtray/internal/status"
	"wbtray/internal/theme"
)

// super is the supersampling factor: the icon is drawn at this many times the
// final size and averaged down.
const super = 4

// View is everything one icon draw needs.
type View struct {
	Size    int
	Style   string
	Metric  string
	Lang    string
	Palette theme.Palette
	Snap    status.Snapshot
	Paused  bool
}

// Draw renders one icon.
func Draw(v View) *raster.Canvas {
	size := v.Size
	if size < 8 {
		size = 8
	}
	c := raster.New(size*super, size*super)

	mark := markColour(v)
	switch v.Style {
	case "gauge":
		drawGauge(c, v, mark)
	case "bars":
		drawBars(c, v, mark)
	case "spark":
		drawSpark(c, v, mark)
	case "number":
		drawNumber(c, v, mark)
	case "cat":
		drawCat(c, v, mark)
	case "shield", "prompt", "waves", "nodes", "ring":
		// The program's own marks, available as tray styles so they can be looked at
		// live rather than only in a sheet. Drawn bare: a tray icon has no plate, so
		// the mark goes straight onto the taskbar in the ink the palette has already
		// checked against it, with the health colour as its accent.
		drawIdentity(c, v, mark)
	default:
		drawGauge(c, v, mark)
	}
	return c.Downsample(super)
}

// markColour is the colour the whole mark is drawn in.
//
// A paused tray draws in the dim ink, which is the quietest way to say "these
// numbers are not being refreshed" without taking the icon away. Everything else
// is the palette's answer for the current health.
func markColour(v View) raster.RGBA {
	if v.Paused {
		return v.Palette.InkDim
	}
	return v.Palette.State(int(v.Snap.Health()))
}

// drawIdentity draws one of the program's marks as a tray icon.
//
// It is the same geometry as the application icon with the plate left off. The ink
// is the palette's text colour and the accent is the health colour, which is what
// keeps it a reading as well as an identity: the shape says which mark it is, and
// the colour says how the gateway is doing.
//
// The mark is drawn in a box of its own and then drawn onto the icon, because the
// marks fill the canvas they are given and the icon's own box is inset from its
// edge. The sub-canvas is the icon's supersampled size, not the output size, so the
// mark is anti-aliased by the same downsample as everything else.
func drawIdentity(c *raster.Canvas, v View, mark raster.RGBA) {
	x, y, w, h := box(c)
	side := int(w)
	if side < 1 {
		return
	}
	sub := raster.New(side, side)
	DrawMark(sub, v.Style, v.Palette.Ink, mark)
	c.DrawCanvas(sub, int(x), int(y+(h-float64(side))/2), 1)
	pauseBand(c, v)
}

// boxInset is the margin a shape keeps inside the icon, as a fraction of its
// width.
//
// The margin is small and deliberate: at sixteen pixels a mark that leaves a
// three-pixel border is a ten-pixel mark, and the reading it carries stops being
// legible before the margin stops looking tidy.
const boxInset = 0.09

// figureInsetPx is the margin the figure style keeps, in output pixels. It is
// zero, and that is a consequence rather than a preference.
//
// Four characters of the compact face occupy fifteen font pixels, one output
// pixel per font pixel is the smallest that stays legible, and a sixteen-pixel
// slot has sixteen to give. Fifteen plus a margin of even one pixel on each side
// is seventeen, so with a margin the figure is not drawn at all — which is the
// empty box an earlier version shipped. A figure that fills its icon edge to edge
// is the design this style has to be; the alternative is no figure.
//
// It is stated in pixels rather than as a fraction so it cannot shrink below one
// pixel on a small icon, where a fraction would round to nothing or to more than
// the icon has.
const figureInsetPx = 0

func box(c *raster.Canvas) (x, y, w, h float64) {
	return boxWithInset(c, boxInset)
}

func boxWithInset(c *raster.Canvas, inset float64) (x, y, w, h float64) {
	m := float64(c.W) * inset
	return m, m, float64(c.W) - 2*m, float64(c.H) - 2*m
}

// figureBox is the drawing area for the figure style: the whole icon, in canvas
// units.
func figureBox(c *raster.Canvas) (x, y, w, h float64) {
	m := float64(figureInsetPx * super)
	return m, m, float64(c.W) - 2*m, float64(c.H) - 2*m
}

// insetFor is the margin a style keeps, as a fraction of the icon. It is how a
// caller asks a style where its own edge is, rather than assuming they all share
// one — the figure style declares none, for the reason above.
func insetFor(style string) float64 {
	if style == "number" {
		return float64(figureInsetPx) / 16 // one pixel at the smallest icon
	}
	return boxInset
}

// pauseBand draws the bar that marks a paused tray.
//
// It is drawn across every style at the same height, because a pause is not part
// of any one design: it is the tray saying it has stopped listening, and that has
// to look the same whichever shape is behind it.
func pauseBand(c *raster.Canvas, v View) {
	if !v.Paused {
		return
	}
	w := float64(c.W) * 0.52
	h := float64(c.W) * 0.10
	c.RoundedRect((float64(c.W)-w)/2, (float64(c.H)-h)/2, w, h, h*0.5, v.Palette.Ink)
}

// Fraction converts a metric into 0..1 for the shapes that fill a gauge.
//
// The ranges are chosen so a glance is meaningful. A percentage is already one;
// a raw count is measured against the same window's own history rather than
// against a fixed ceiling, because a gauge pinned at full for every reading tells
// the reader nothing.
func Fraction(v View) float64 {
	value := v.Snap.MetricValue(v.Metric)
	switch v.Metric {
	case "accounts":
		return clamp01(value / 100)
	case "credits":
		ceiling := creditCeiling(v.Snap)
		if ceiling <= 0 {
			return 0
		}
		return clamp01(value / ceiling)
	case "requests", "tokens", "latency":
		if peak := raster.Max(v.Snap.SeriesFor(v.Metric)); peak > 0 {
			return clamp01(value / peak)
		}
		return provisionalFraction(value, v.Metric)
	case "tps":
		return clamp01(value / 100)
	case "queue":
		return clamp01(value / 6)
	}
	return 0
}

// provisionalFraction is the fallback for a metric with no history yet: the same
// rough ceilings an earlier version used throughout, kept for the first minutes
// of a fresh install, before there is a window to compare against.
func provisionalFraction(value float64, metric string) float64 {
	switch metric {
	case "requests", "tokens":
		return clamp01(value / 1000)
	case "latency":
		return clamp01(value / 5000)
	}
	return 0
}

// creditCeiling is the highest balance any account is known to hold, so a nearly
// empty pool reads as nearly empty rather than as a small number in a huge range.
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

func clamp01(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return math.Max(0, math.Min(1, v))
}

// track is the unfilled part of a gauge or chart: the palette's dim ink at low
// alpha, which reads as "the rest of the range" without competing with the mark
// that carries the reading.
func track(p theme.Palette) raster.RGBA { return p.InkDim.Mul(0.45) }

// drawGauge is the donut: the metric as an arc.
//
// A full ring rather than the broken one an earlier version drew. The break was
// meant to read as a gauge opening, and at sixteen pixels it read as a letter Q
// with a tail — the tick that marked the exact value made it worse by sticking
// out past the rim.
func drawGauge(c *raster.Canvas, v View, mark raster.RGBA) {
	_, _, w, h := box(c)
	cx, cy := float64(c.W)/2, float64(c.H)/2
	d := math.Min(w, h)
	ro := d / 2
	ri := ro * 0.58

	c.Ring(cx, cy, ri, ro, track(v.Palette))
	frac := Fraction(v)
	if frac > 0.002 {
		// From twelve o'clock, clockwise, as a gauge is read. The line caps are
		// flat, which at this size keeps the two ends of the arc from meeting
		// when the value is near full.
		c.Arc(cx, cy, (ri+ro)/2, ro-ri, -90, -90+360*frac, mark)
	}
	pauseBand(c, v)
}

// drawBars is the metric as a four-bar chart: the newest quarters of the window,
// so it reads as a trend rather than a level.
func drawBars(c *raster.Canvas, v View, mark raster.RGBA) {
	x, y, w, h := box(c)
	bars := 4
	gap := w * 0.13
	bw := (w - gap*float64(bars-1)) / float64(bars)

	values := raster.Tail(v.Snap.SeriesFor(v.Metric), bars)
	if len(values) != bars {
		// No history for this metric: draw the level as equal bars, which still
		// answers "how full is it" without a trend.
		values = make([]float64, bars)
		for i := range values {
			values[i] = Fraction(v)
		}
	}
	top := raster.Max(values)
	if top <= 0 {
		top = 1
	}
	for i, value := range values {
		bx := x + float64(i)*(bw+gap)
		bh := h * clamp01(value/top)
		// A bar too short to see is not a reading of zero, it is a missing bar;
		// the floor keeps every bar present so the shape stays a chart.
		if bh < h*0.10 {
			bh = h * 0.10
		}
		// The newest bar is at full strength and the older ones are dimmed. Drawing
		// the older ones in the track colour instead — which an earlier version did
		// — produces a chart that reads as three empty slots and one full one, the
		// opposite of what the numbers say.
		colour := mark
		if i != len(values)-1 {
			colour = mark.Mul(0.55)
		}
		c.RoundedRect(bx, y+h-bh, bw, bh, bw*0.30, colour)
	}
	pauseBand(c, v)
}

// drawSpark is the trend line: recent history as a smoothed polyline, with the
// newest point marked.
func drawSpark(c *raster.Canvas, v View, mark raster.RGBA) {
	x, y, w, h := box(c)
	series := raster.Tail(v.Snap.SeriesFor(v.Metric), 12)
	if len(series) < 2 {
		// Nothing to plot. A level bar is honest about there being no history yet,
		// where a line drawn through one point would be a fiction.
		bh := h * 0.34
		frac := Fraction(v)
		c.RoundedRect(x, y+h-bh, w*frac, bh, bh*0.4, mark)
		if frac < 1 {
			c.RoundedRect(x+w*frac, y+h-bh, w*(1-frac), bh, bh*0.4, track(v.Palette))
		}
		pauseBand(c, v)
		return
	}
	top := raster.Max(series)
	if top <= 0 {
		top = 1
	}
	// The points are smoothed before they are drawn. A twelve-point polyline in a
	// sixteen-pixel icon puts a corner every pixel and a half, and corners at that
	// spacing read as noise rather than as a trend.
	smoothed := smooth(series, 2)
	pts := make([]raster.Pt, len(smoothed))
	// The line stops short of the box at both ends, by the radius of the dot that
	// marks its newest point. Drawing from edge to edge and then stamping a dot on
	// the last point puts half that dot outside the margin, which is ink on the
	// taskbar with nothing behind it.
	r := w * 0.08
	span := w - 2*r
	for i, value := range smoothed {
		px := x + r + span*float64(i)/float64(len(smoothed)-1)
		py := y + h*0.90 - h*0.78*clamp01(value/top)
		pts[i] = raster.Pt{X: px, Y: py}
	}
	// The line is stroked twice: a dim underlay for weight, then the mark. A
	// single stroke at sixteen pixels is a hairline that disappears on a busy
	// taskbar.
	c.Line(pts, w*0.18, mark.Mul(0.45))
	c.Line(pts, w*0.10, mark)
	last := pts[len(pts)-1]
	c.Circle(last.X, last.Y, r, mark)
	pauseBand(c, v)
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

// drawNumber is the figure itself.
//
// It is the one thing a shape cannot do. A gauge shows a level and a chart shows a
// trend, and neither says how many credits are left; this style says exactly that
// and nothing else, which is what makes it worth having at a size where there is
// only room for one idea.
func drawNumber(c *raster.Canvas, v View, mark raster.RGBA) {
	x, y, w, h := figureBox(c)
	for _, text := range figureVariants(v) {
		if fitsFigure(text, w, h) {
			drawFigure(c, text, x, y, w, h, mark)
			break
		}
	}
	pauseBand(c, v)
}

// drawCat is the identity: the same cat the console uses, wearing the palette's
// colours and carrying the health in its coat.
func drawCat(c *raster.Canvas, v View, mark raster.RGBA) {
	x, y, w, h := box(c)
	// The cat is a silhouette rather than a mark, so it is drawn to the corners of
	// its box: the gauge and the chart have a natural centre, and a cat shrunk to
	// the same footprint loses its ears first.
	size := math.Min(w, h)
	cx := x + (w-size)/2
	cy := y + (h-size)/2

	cat.Draw(c, cx, cy, size, size, cat.Palette{
		Fur: mark,
		// The face is a knockout: the eyes and mouth are the taskbar showing
		// through. That is why the palette states which tone it was built for —
		// without it the face would have to be drawn in a colour of its own, and
		// on a status icon a third colour in the middle of the mark reads as
		// another state rather than as a detail.
		Inner: v.Palette.Taskbar(),
		// The cheeks are shading rather than a colour: the coat mixed toward the
		// dim ink. A fixed pink puts a pair of red spots on a status icon, where a
		// red spot reads as an alarm.
		Blush:    mark.Mix(v.Palette.InkDim, 0.45),
		HaloW:    0,
		CheeksOn: c.W >= 64,
	})
	pauseBand(c, v)
}

// fitters: the figure styles.

// figureVariants is a figure from most to least precise.
//
// The icon does not know its own final size while the style is being chosen — the
// same style is drawn at sixteen pixels for the taskbar and at twenty-six for the
// menu's preview — so the decision about how much to shorten is made by trying the
// precise form first and falling back until one fits. A figure shortened to fit is
// worth more than one shrunk until it cannot be read.
func figureVariants(v View) []string {
	value := v.Snap.MetricValue(v.Metric)
	switch v.Metric {
	case "accounts":
		return []string{
			fmt.Sprintf("%d/%d", v.Snap.Ready(), v.Snap.Total),
			fmt.Sprintf("%d", v.Snap.Ready()),
		}
	case "latency":
		return []string{
			fmt.Sprintf("%.1fs", value/1000),
			fmt.Sprintf("%.0fs", value/1000),
			fmt.Sprintf("%.0f", value/1000),
		}
	case "tps", "queue":
		return []string{fmt.Sprintf("%.0f", value)}
	default:
		// A count, longest form first. Only the forms that mean something are
		// offered: "0M" for a value under a million is not a shortened figure, it
		// is a wrong one.
		out := []string{fmt.Sprintf("%.0f", value)}
		if value >= 1000 {
			out = append(out, fmt.Sprintf("%.1fk", value/1000))
			if value >= 10000 {
				out = append(out, fmt.Sprintf("%.0fk", value/1000))
			}
		}
		if value >= 1e6 {
			out = append(out, fmt.Sprintf("%.1fM", value/1e6), fmt.Sprintf("%.0fM", value/1e6))
		}
		if value >= 1e9 {
			out = append(out, fmt.Sprintf("%.0fG", value/1e9))
		}
		return out
	}
}

// drawFigure centres a figure in a box at the largest whole-pixel size that fits
// both its width and the height it is given.
//
// Whole output pixels per font pixel is what makes the figure legible rather than
// a grey smear, and it is the whole reason the size is quantised: the canvas is
// supersampled by four, so a font pixel of 2.56 output pixels becomes a five-pixel
// square averaging down to sixty per cent coverage at every edge. At one output
// pixel per font pixel the glyph is exactly the shape the table describes, and a
// 3×5 face at sixteen pixels is the classic taskbar readout.
func drawFigure(c *raster.Canvas, text string, x, y, w, h float64, colour raster.RGBA) {
	if text == "" || w <= 0 || h <= 0 {
		return
	}
	// w and h arrive in canvas units, which are already the supersampled ones, so
	// the font size is divided by the supersampling once to reach output pixels per
	// font pixel. Multiplying by it here — which the first version of this did —
	// asks for a figure four times too large, and the result is ink outside the box.
	byWidth := raster.FitCompact(text, w) / super
	byHeight := h / float64(raster.CompactGlyphHeight) / super
	scale := math.Floor(math.Min(byWidth, byHeight))
	if scale < 1 {
		return
	}
	px := scale * super
	tw := raster.CompactTextWidth(text, px)
	th := raster.CompactTextHeight(px)
	c.CompactText(x+(w-tw)/2, y+(h-th)/2, px, text, colour)
}

// fitsFigure reports whether a figure can be drawn at one output pixel per font
// pixel inside the box. Its arguments are canvas units, as box reports them.
func fitsFigure(text string, w, h float64) bool {
	byWidth := raster.FitCompact(text, w) / super
	byHeight := h / float64(raster.CompactGlyphHeight) / super
	return math.Min(byWidth, byHeight) >= 1
}
