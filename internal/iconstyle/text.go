package iconstyle

import (
	"fmt"
	"math"

	"wbtray/internal/cat"
	"wbtray/internal/raster"
	"wbtray/internal/theme"
)

// The text styles: an icon that says a number.
//
// A shape is what a sixteen-pixel icon is naturally good at, and the three shape
// styles do that well. They cannot do the one thing an operator sometimes wants,
// which is the figure itself — how many credits are left, how many accounts are
// serving. These styles draw it, in a face small enough to fit and large enough
// to read.

// figureBox is the space a figure is given inside the icon, as fractions of its
// width and height.
//
// It is nearly the whole icon on purpose, and the constraint is arithmetic
// rather than taste. Four characters of the compact face need fifteen font
// pixels across; one output pixel per font pixel is the smallest that stays
// legible; so a four-character figure needs fifteen of the sixteen pixels the
// icon has. Asking for less does not produce a smaller figure — it produces no
// figure at all, which is exactly what the first version of these styles drew.
const (
	figureBoxW = 0.96
	figureBoxH = 0.62
)

// figureRect is the box a figure is drawn in, given the icon's side.
func figureRect(box float64) (x, y, w, h float64) {
	return box * (1 - figureBoxW) / 2, box * (1 - figureBoxH) / 2,
		box * figureBoxW, box * figureBoxH
}

// figureVariants is a figure from most to least precise.
//
// The icon does not know its own size while the style is being chosen — the same
// style is drawn at sixteen pixels for the taskbar and at twenty-six for the
// menu's preview — so the choice of how much to shorten is made by trying the
// precise form first and falling back until one fits. A figure that has been
// shortened to fit is worth more than one that has been shrunk until it cannot
// be read.
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
		// is a wrong one, and it appeared here before this list was built by
		// inspecting the value rather than by dividing it.
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

// drawFigure centres the figure in a box, at the largest size that fits both its
// width and the height it is given.
//
// Fitting both is what keeps a one-character figure from becoming a giant while
// a four-character one stays inside its column: the smaller of the two scales
// wins, and the result is a figure that fills its space in whichever direction
// it is short.
func drawFigure(c *raster.Canvas, text string, x, y, w, h float64, colour raster.RGBA) {
	if text == "" || w <= 0 || h <= 0 {
		return
	}
	// Each font pixel is drawn at a whole number of output pixels.
	//
	// This is what makes the figure legible rather than a grey smear, and it is
	// the whole reason the size is quantised: the canvas is supersampled by four,
	// so a font pixel of 2.56 output pixels becomes a five-pixel square that
	// averages down to sixty per cent coverage at every edge. At one output pixel
	// per font pixel the glyph is exactly the shape the table describes, and a
	// 3×5 face at sixteen pixels is the classic taskbar readout.
	byWidth := raster.FitCompact(text, w) / super
	byHeight := h / float64(raster.CompactGlyphHeight) / super
	scale := math.Floor(math.Min(byWidth, byHeight))
	if scale < 1 {
		// Not even one output pixel per font pixel: the style is being asked to
		// draw a figure too long for the space, and a smear is worse than an
		// empty icon.
		return
	}
	px := scale * super
	tw := raster.CompactTextWidth(text, px)
	th := raster.CompactTextHeight(px)
	c.CompactText(x+(w-tw)/2, y+(h-th)/2, px, text, colour)
}

// fitsFigure reports whether a figure can be drawn at one output pixel per font
// pixel inside the box.
func fitsFigure(text string, w, h float64) bool {
	byWidth := raster.FitCompact(text, w) / super
	byHeight := h / float64(raster.CompactGlyphHeight) / super
	return math.Min(byWidth, byHeight) >= 1
}

// DrawBarText is the bars style with the figure drawn over the middle of the
// chart.
//
// The bars stay in the background because they cost nothing to keep and they
// answer the question the figure cannot — whether the reading is high or low for
// this gateway. They are dimmed, so the figure has the contrast.
func DrawBarText(c *raster.Canvas, v View, accent raster.RGBA) {
	box := float64(c.W)

	// The chart, drawn behind and dimmed so the figure has the contrast.
	drawBarsIn(c, v, accent, 0.45)

	// And the figure over it, on a soft backing so it is legible against the
	// bars rather than lost among them.
	drawFigureBacking(c, box, v.Theme)
	x, y, w, h := figureRect(box)
	// The figure carries the health colour, as in the text-only style, so the
	// reading and the state are one mark rather than two competing for the same
	// handful of pixels.
	drawBestFigure(c, v, x, y, w, h, accent)
}

// drawBestFigure draws the most precise form of the figure that fits the box.
func drawBestFigure(c *raster.Canvas, v View, x, y, w, h float64, colour raster.RGBA) {
	for _, text := range figureVariants(v) {
		if fitsFigure(text, w, h) {
			drawFigure(c, text, x, y, w, h, colour)
			return
		}
	}
	// Nothing fits, not even the shortest form.
}

// DrawText is the figure alone: no chart, no gauge, just the number at the
// largest size the icon allows.
//
// It is the only style here with nothing else in it, and it exists because at
// sixteen pixels a figure and a chart compete for the same handful of pixels.
// When the number is what matters, this is the style that shows it best.
func DrawText(c *raster.Canvas, v View, accent raster.RGBA) {
	box := float64(c.W)
	x, y, w, h := figureRect(box)
	// The figure is drawn in the health colour rather than in the palette's ink,
	// and there is no status pip. That is the trade this style makes: it has no
	// room for a second mark, and a pip beside a four-character figure lands on
	// top of it. The colour of the reading carries the state instead, which it
	// does better than a dot would — the whole figure is coloured, not a corner.
	drawBestFigure(c, v, x, y, w, h, accent)
}

// DrawMascotText is the cat with the figure beside it.
//
// The cat is the identity and the figure is the reading, so neither is dropped:
// the cat takes the left two thirds and the number the right, and the cat keeps
// the health colour in its coat so the state is still visible at a glance.
func DrawMascotText(c *raster.Canvas, v View, accent raster.RGBA) {
	t := v.Theme
	box := float64(c.W)

	// The cat, smaller and to the left.
	catW := box * 0.46
	catH := box * 0.40
	catX := box * 0.02
	catY := (box - catH) / 2
	inner := t.Halo
	if inner.A == 0 {
		inner = t.InkDim
	}
	cat.Draw(c, catX, catY, catW, catH, cat.Palette{
		Fur:   accent,
		Inner: inner,
		Blush: accent.Mix(t.InkDim, 0.55),
		Halo:  t.Halo,
		HaloW: box * 0.03,
	})

	// The figure to its right, in the space that is left.
	textX := catX + catW + box*0.01
	textW := box - textX - box*0.02
	drawBestFigure(c, v, textX, box*(1-figureBoxH)/2, textW, box*figureBoxH, accent)

	if v.Paused {
		c.Rect(catX, box*0.47, catW, box*0.06, t.InkDim)
	}
}

// drawFigureBacking paints a soft plate behind a figure so it reads against
// whatever the style has drawn underneath.
//
// It is the theme's own halo colour rather than a new one, which keeps the
// backing consistent with the mark's background everywhere else in the icon.
func drawFigureBacking(c *raster.Canvas, box float64, t theme.Theme) {
	colour := t.Halo
	if colour.A == 0 {
		// A palette with no halo has nothing to lean on, so the backing is taken
		// from the background the icon is drawn on: transparent stays transparent
		// and the dim ink does the separating instead.
		return
	}
	c.RoundedRect(box*0.17, box*0.22, box*0.66, box*0.56, box*0.14, colour.Mul(0.85))
}
