package iconstyle

import (
	"testing"

	"wbtray/internal/config"
	"wbtray/internal/raster"
	"wbtray/internal/status"
	"wbtray/internal/theme"
)

// bigPool is a reading where every metric has a figure worth shortening.
func bigPool() status.Snapshot {
	return status.Snapshot{
		Reachable: true, Total: 3, Healthy: 3,
		Accounts: []status.Account{
			{UID: "a", Credits: 48250, Total: 60000},
			{UID: "b", Credits: 48250, Total: 60000},
			{UID: "c", Credits: 48250, Total: 60000},
		},
		Usage: status.Usage{
			Requests: 128400, TotalTokens: 1_602_000, AvgLatencyMs: 812, AvgTPS: 41,
			Series: []float64{10, 90, 40, 70, 20, 95, 30, 60},
		},
	}
}

// testView is a view for one style, metric and size.
func testView(style, metric string, size int, snap status.Snapshot) View {
	return View{
		Size: size, Style: style, Metric: metric, Lang: "en",
		Palette: theme.Dark(), Snap: snap,
	}
}

// testFigureBox is the space the renderer gives a figure, in canvas units.
//
// It is derived from the view the same way the renderer derives it, so a test
// cannot pass against a box the icon does not actually have.
func testFigureBox(v View) (w, h float64) {
	box := float64(v.Size * super)
	m := float64(figureInsetPx * super)
	return box - 2*m, box - 2*m
}

// chosenFigure is the variant the renderer will draw: the first one that fits, or
// empty when none does.
func chosenFigure(v View) string {
	w, h := testFigureBox(v)
	for _, text := range figureVariants(v) {
		if fitsFigure(text, w, h) {
			return text
		}
	}
	return ""
}

// TestTheNumberStyleAlwaysDrawsAFigure is the bug the first version shipped.
//
// The figure box was drawn at sixty per cent of the icon, which at sixteen pixels
// left nine pixels for characters needing fifteen — so no figure was drawn at
// all and the style showed an empty box. Every metric has to yield a figure at
// every size a taskbar uses, for every palette.
func TestTheNumberStyleAlwaysDrawsAFigure(t *testing.T) {
	snap := bigPool()
	for _, p := range theme.All(theme.Accent{}) {
		for _, metric := range config.Metrics {
			for _, size := range []int{16, 20, 24, 32} {
				v := View{
					Size: size, Style: config.StyleNumber, Metric: metric,
					Lang: "en", Palette: p, Snap: snap,
				}
				w, h := testFigureBox(v)
				if chosen := chosenFigure(v); chosen == "" {
					t.Errorf("%s/%s at %d: none of %v fits a %.0fx%.0f box",
						p.Name, metric, size, figureVariants(v), w, h)
				}
			}
		}
	}
}

// TestTheFigureIsDrawnNotJustChosen is the stronger check, and the one that would
// have caught the empty box: a chosen figure still has to put ink on the canvas.
func TestTheFigureIsDrawnNotJustChosen(t *testing.T) {
	snap := bigPool()
	for _, size := range []int{16, 20, 24, 32} {
		v := testView(config.StyleNumber, config.MetricCredits, size, snap)
		text := chosenFigure(v)
		if text == "" {
			continue // covered by the test above
		}
		// Drawn onto a blank canvas, which is the only way to ask whether the
		// figure itself puts ink down: measuring it inside the finished icon
		// cannot separate the text from anything else.
		box := float64(size * super)
		m := float64(figureInsetPx * super)
		c := raster.New(int(box), int(box))
		drawFigure(c, text, m, m, box-2*m, box-2*m, theme.Dark().Ink)
		if inkCount(c) == 0 {
			t.Errorf("at %d: the figure %q was chosen but drew nothing", size, text)
		}
	}
}

// inkCount counts the pixels a drawing put down.
func inkCount(c *raster.Canvas) int {
	n := 0
	for y := 0; y < c.H; y++ {
		for x := 0; x < c.W; x++ {
			if c.At(x, y).A > 110 {
				n++
			}
		}
	}
	return n
}

// TestFiguresShortenRatherThanOverflow checks the rule the variants exist for: a
// figure that does not fit is replaced by a shorter one, never by one that
// overflows the icon or shrinks below legibility.
func TestFiguresShortenRatherThanOverflow(t *testing.T) {
	v := testView(config.StyleNumber, config.MetricRequests, 16, bigPool())
	chosen := chosenFigure(v)
	if chosen == "" {
		t.Fatal("no figure was chosen for a reading that has one")
	}
	w, h := testFigureBox(v)
	if !fitsFigure(chosen, w, h) {
		t.Errorf("the chosen figure %q does not fit %.0fx%.0f", chosen, w, h)
	}
	if len(chosen) > len("128400") {
		t.Errorf("the chosen figure %q is longer than the reading it stands for", chosen)
	}
}

// TestFiguresAreNotNonsense guards the shortening arithmetic. A figure of "0M"
// for a value under a million is not a shortened figure, it is a wrong one, and
// it is what dividing without looking produces.
func TestFiguresAreNotNonsense(t *testing.T) {
	for _, tc := range []struct {
		value float64
		bad   string
	}{
		{128400, "0M"},
		{48250, "0M"},
		{999, "0k"},
		{0, "0k"},
	} {
		v := testView(config.StyleNumber, config.MetricCredits, 32, status.Snapshot{
			Reachable: true, Total: 1, Healthy: 1,
			Accounts: []status.Account{{UID: "a", Credits: int64(tc.value)}},
		})
		for _, variant := range figureVariants(v) {
			if variant == tc.bad {
				t.Errorf("a reading of %.0f offers the variant %q", tc.value, variant)
			}
		}
	}
}
