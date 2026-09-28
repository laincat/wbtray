package iconstyle

import (
	"testing"

	"wbtray/internal/config"
	"wbtray/internal/status"
	"wbtray/internal/theme"
)

// TestEveryStyleFitsInsideItsHalo is the property the transparent design
// depends on.
//
// With no plate, the halo is the only thing standing between a light mark and a
// light taskbar. A style that spills past its rim has a piece of itself drawn
// straight onto whatever is behind the icon, which is unreadable on the wrong
// taskbar — and the first version of the mascot did exactly that, its ears
// standing outside their own shadow.
func TestEveryStyleFitsInsideItsHalo(t *testing.T) {
	snap := status.Snapshot{
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
	for _, th := range theme.All(theme.Accent{}) {
		for _, style := range config.Styles {
			for _, size := range []int{16, 20, 24, 32, 48} {
				// The halo is drawn first and the mark over it, so a pixel that
				// is opaque but has no halo beneath it is a pixel of mark drawn
				// on the taskbar.
				haloOnly := Draw(View{
					Size: size, Style: style, Metric: "requests",
					Lang: "en", Theme: th, Snap: snap, HaloOnly: true,
				})
				full := Draw(View{
					Size: size, Style: style, Metric: "requests",
					Lang: "en", Theme: th, Snap: snap,
				})
				for y := 0; y < full.H; y++ {
					for x := 0; x < full.W; x++ {
						if full.At(x, y).A > 40 && haloOnly.At(x, y).A < 8 {
							t.Fatalf("%s/%s at %d: pixel (%d,%d) alpha %d has no halo beneath it",
								th.Name, style, size, x, y, full.At(x, y).A)
						}
					}
				}
			}
		}
	}
}
