package iconstyle

import (
	"testing"

	"wbtray/internal/config"
	"wbtray/internal/raster"
	"wbtray/internal/status"
	"wbtray/internal/theme"
)

// sampleReading is a pool with something to show, used by the tests that ask
// whether a style draws at all.
func sampleReading() status.Snapshot {
	return status.Snapshot{
		Reachable: true, Total: 3, Healthy: 3,
		Accounts: []status.Account{
			{UID: "a", Credits: 9000, Total: 12000},
			{UID: "b", Credits: 9000, Total: 12000},
			{UID: "c", Credits: 9000, Total: 12000},
		},
		Usage: status.Usage{
			TotalTokens: 49000, AvgLatencyMs: 812, AvgTPS: 41,
			Series: []float64{10, 90, 40, 70, 20, 95, 30, 60},
		},
	}
}

// TestEveryStyleStaysInsideItsBox is the property the transparent design rests on.
//
// The icon draws no plate, so a mark that runs past its edge is drawn straight
// onto the taskbar with nothing behind it — which is unreadable on the wrong
// taskbar, and is what the first version of the mascot did: its ears stood
// outside their own shadow. The box is the margin the marks are supposed to keep,
// so ink outside it is ink that has escaped.
//
// The margin is per style, because one of them declares none: the figure style
// fills its icon edge to edge, since a four-character figure at the smallest size
// has no pixels to spare for a margin. Asking each style where its own edge is
// keeps that a decision rather than an oversight.
func TestEveryStyleStaysInsideItsBox(t *testing.T) {
	snap := sampleReading()
	for _, light := range []bool{false, true} {
		pal := theme.On(light)
		for _, style := range config.Styles {
			for _, size := range []int{16, 20, 24, 32, 48} {
				c := Draw(View{
					Size: size, Style: style, Metric: config.MetricRequests,
					Lang: "en", Palette: pal, Snap: snap,
				})
				limit := 0
				if inset := insetFor(style); inset > 0 {
					limit = int(float64(size) * inset)
				}
				for y := 0; y < c.H; y++ {
					for x := 0; x < c.W; x++ {
						if c.At(x, y).A <= 40 {
							continue
						}
						if x < limit || y < limit || x >= c.W-limit || y >= c.H-limit {
							t.Fatalf("%s/%s light=%v at %d: ink at (%d,%d) is inside the %dpx margin, so it is drawn on the taskbar",
								pal.Name, style, light, size, x, y, limit)
						}
					}
				}
			}
		}
	}
}

// TestEveryStyleDrawsSomething is the other half: a style that stays inside its
// box by drawing nothing is not passing the test above, it is failing this one.
func TestEveryStyleDrawsSomething(t *testing.T) {
	snap := sampleReading()
	for _, p := range theme.All(theme.Accent{}) {
		for _, style := range config.Styles {
			for _, metric := range config.Metrics {
				for _, size := range []int{16, 32} {
					c := Draw(View{
						Size: size, Style: style, Metric: metric,
						Lang: "en", Palette: p, Snap: snap,
					})
					if inkCount(c) == 0 {
						t.Errorf("%s/%s at %d drew nothing", style, metric, size)
					}
				}
			}
		}
	}
}

// TestTheIconReactsToHealth is why the icon is drawn rather than shipped: a
// glance has to distinguish a working gateway from a broken one.
func TestTheIconReactsToHealth(t *testing.T) {
	pal := theme.Dark()
	healthy := sampleReading()
	down := status.Snapshot{Reachable: false}
	for _, style := range config.Styles {
		a := Draw(View{Size: 32, Style: style, Metric: config.MetricAccounts, Lang: "en", Palette: pal, Snap: healthy})
		b := Draw(View{Size: 32, Style: style, Metric: config.MetricAccounts, Lang: "en", Palette: pal, Snap: down})
		if samePixels(a, b) {
			t.Errorf("%s draws a reachable and an unreachable gateway identically", style)
		}
	}
}

// TestPausedLooksDifferentFromHealthy checks the third state the icon carries.
func TestPausedLooksDifferentFromHealthy(t *testing.T) {
	pal := theme.Dark()
	snap := sampleReading()
	for _, style := range config.Styles {
		a := Draw(View{Size: 32, Style: style, Metric: config.MetricAccounts, Lang: "en", Palette: pal, Snap: snap})
		b := Draw(View{Size: 32, Style: style, Metric: config.MetricAccounts, Lang: "en", Palette: pal, Snap: snap, Paused: true})
		if samePixels(a, b) {
			t.Errorf("%s draws a paused tray the same as a running one", style)
		}
	}
}

// samePixels reports whether two icons are identical.
func samePixels(a, b *raster.Canvas) bool {
	if a == nil || b == nil || a.W != b.W || a.H != b.H {
		return false
	}
	for y := 0; y < a.H; y++ {
		for x := 0; x < a.W; x++ {
			if a.At(x, y) != b.At(x, y) {
				return false
			}
		}
	}
	return true
}
