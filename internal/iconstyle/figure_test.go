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
	return View{Size: size, Style: style, Metric: metric, Lang: "en", Theme: theme.Neon(), Snap: snap}
}

// boxW and boxH are the figure box in canvas units, which is the space the
// renderer fits the figure into. They are derived from the view the same way the
// renderer derives it, so a test cannot pass against a box the icon does not
// have.
func boxW(v View) float64 {
	_, _, w, _ := figureRect(float64(v.Size * super))
	return w
}

func boxH(v View) float64 {
	_, _, _, h := figureRect(float64(v.Size * super))
	return h
}

// chosenFigure is the variant the renderer will draw: the first one that fits,
// or empty when none does.
func chosenFigure(v View) string {
	w, h := boxW(v), boxH(v)
	for _, text := range figureVariants(v) {
		if fitsFigure(text, w, h) {
			return text
		}
	}
	return ""
}

// TestTextStylesAlwaysDrawAFigure is the bug the first version shipped.
//
// The figure box was drawn at sixty per cent of the icon, which at sixteen
// pixels leaves nine pixels for characters that need fifteen — so no figure was
// drawn at all, and the three text styles showed an empty box. Every metric has
// to yield a figure at every size a taskbar uses, for every theme.
func TestTextStylesAlwaysDrawAFigure(t *testing.T) {
	snap := bigPool()
	for _, th := range theme.All() {
		for _, style := range []string{config.StyleText, config.StyleBarText, config.StyleMascotText} {
			for _, metric := range config.Metrics {
				for _, size := range []int{16, 20, 24, 32} {
					v := View{Size: size, Style: style, Metric: metric, Lang: "en", Theme: th, Snap: snap}
					w, h := boxW(v), boxH(v)
					if chosen := chosenFigure(v); chosen == "" {
						t.Errorf("%s/%s/%s at %d: no variant of %v fits a %.0fx%.0f box",
							th.Name, style, metric, size, figureVariants(v), w, h)
					}
				}
			}
		}
	}
}

// TestTheFigureIsDrawnNotJustChosen is the stronger check, and the one that
// would have caught the empty box: a chosen figure still has to put ink on the
// canvas.
func TestTheFigureIsDrawnNotJustChosen(t *testing.T) {
	snap := bigPool()
	for _, style := range []string{config.StyleText, config.StyleBarText, config.StyleMascotText} {
		for _, size := range []int{16, 20, 24, 32} {
			v := testView(style, config.MetricCredits, size, snap)
			text := chosenFigure(v)
			if text == "" {
				continue // covered by the test above
			}
			// Drawn onto a blank canvas, which is the only way to ask whether the
			// figure itself puts ink down: measuring it inside the finished icon
			// cannot separate the text from the chart or the cat behind it.
			box := float64(size * super)
			c := raster.New(int(box), int(box))
			x, y, w, h := figureRect(box)
			drawFigure(c, text, x, y, w, h, theme.Neon().Ink)
			if inkCount(c) == 0 {
				t.Errorf("%s at %d: the figure %q was chosen but drew nothing",
					style, size, text)
			}
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
	v := testView(config.StyleText, config.MetricRequests, 16, bigPool())
	chosen := chosenFigure(v)
	if chosen == "" {
		t.Fatal("no figure was chosen for a reading that has one")
	}
	w, h := boxW(v), boxH(v)
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
		v := testView(config.StyleText, config.MetricCredits, 32, status.Snapshot{
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
