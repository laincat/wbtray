//go:build windows

package main

import (
	"image/png"
	"os"
	"testing"
	"time"

	"wbtray/internal/config"
	"wbtray/internal/raster"
	"wbtray/internal/status"
	"wbtray/internal/theme"
)

// TestChartWindowRenders draws the window at three sizes and for two states,
// which is the only way to see the layout without a desktop.
//
// It is written as a test rather than a command because what it is really
// checking is that the layout arithmetic holds at the smallest size the window
// allows: a chart that draws its axis over its own footer is a bug that is
// invisible in a large window and obvious at the minimum size.
func TestChartWindowRenders(t *testing.T) {
	dir := os.Getenv("WBTRAY_CHART_DIR")

	for _, tc := range []struct {
		name   string
		width  float64
		height float64
		snap   status.Snapshot
		metric string
	}{
		{"default", chDefaultW, chDefaultH, richSnapshot(), "requests"},
		{"minimum", chMinW, chMinH, richSnapshot(), "tokens"},
		{"latency", chDefaultW, chDefaultH, richSnapshot(), "latency"},
		{"offline", chDefaultW, chDefaultH, status.Snapshot{Reachable: false}, "requests"},
		{"empty", chDefaultW, chDefaultH, status.Snapshot{Reachable: true, Total: 1}, "credits"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := &chartWindow{app: fixedSource{snap: tc.snap}, metric: tc.metric, scale: 1}
			w.width, w.height = tc.width, tc.height
			// A minute of live samples, which is what the strip draws.
			now := time.Now()
			for i := 0; i < chSamples; i++ {
				w.samples = append(w.samples, sample{
					at:    now.Add(time.Duration(i-chSamples) * time.Second),
					value: tc.snap.MetricValue(tc.metric) * (0.75 + 0.25*float64(i%7)/7),
				})
			}

			c := raster.New(int(tc.width), int(tc.height))
			th := theme.Neon()
			w.render(c, 1, tc.height, tc.snap, th, "en", chartLine, tc.metric, w.samples, -1)

			// The window has to be painted, not merely allocated.
			drawn := 0
			for y := 0; y < c.H; y++ {
				for x := 0; x < c.W; x++ {
					if c.At(x, y).A > 200 {
						drawn++
					}
				}
			}
			if drawn < c.W*c.H/2 {
				t.Fatalf("only %d of %d pixels were painted", drawn, c.W*c.H)
			}

			if dir != "" {
				writeChartPNG(t, dir, tc.name, c)
			}
		})
	}
}

// TestChartSeriesFallBackForMetricsWithoutHistory checks the documented
// trade-off: a metric the gateway keeps no history for is charted from the
// request series instead of from an empty list.
func TestChartSeriesFallBackForMetricsWithoutHistory(t *testing.T) {
	snap := richSnapshot()
	for _, metric := range config.Metrics {
		series, _ := chartSeries(snap, metric)
		if len(series) == 0 {
			t.Errorf("metric %q plotted nothing", metric)
		}
	}
}

// TestNiceCeilingRoundsUp is the axis rule: the top gridline is a round number
// at or above the data, never below it.
func TestNiceCeilingRoundsUp(t *testing.T) {
	for _, tc := range []struct{ in, want float64 }{
		{0, 1},
		{0.5, 0.5},
		{9, 10},
		{11, 15},
		{1284, 1500},
		{49000, 50000},
	} {
		if got := niceCeiling(tc.in); got < tc.in {
			t.Errorf("niceCeiling(%v) = %v, which is below the data", tc.in, got)
		}
		if got := niceCeiling(tc.in); got != tc.want {
			t.Errorf("niceCeiling(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// TestShortLabelTrimsBucketKeys checks that the axis labels are the readable
// part of a bucket key rather than the key itself.
func TestShortLabelTrimsBucketKeys(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"2026-09-26T14", "14:00"},
		{"2026-09-26", "09-26"},
		{"x", "x"},
	} {
		if got := shortLabel(tc.in); got != tc.want {
			t.Errorf("shortLabel(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// fixedSource is a chart source with a snapshot that does not change, which is
// what lets the renderer be exercised without a gateway or a desktop.
type fixedSource struct{ snap status.Snapshot }

func (f fixedSource) Snapshot() status.Snapshot { return f.snap }
func (f fixedSource) Config() config.Config     { return config.Default() }
func (f fixedSource) Lang() string              { return "en" }
func (f fixedSource) PanelURL() string          { return "http://127.0.0.1:7863/panel/" }
func (f fixedSource) BaseURL() string           { return "http://127.0.0.1:7863" }

func writeChartPNG(t *testing.T, dir, name string, c *raster.Canvas) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := dir + string(os.PathSeparator) + name + ".png"
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, c.Image()); err != nil {
		t.Fatal(err)
	}
}

// richSnapshot is a plausible reading with history in every series.
func richSnapshot() status.Snapshot {
	accounts := []status.Account{
		{UID: "u1", Nickname: "laincat", Credits: 21400, Total: 24000, InFlight: 2},
		{UID: "u2", Nickname: "backup", Credits: 9300, Total: 24000, Cooling: true, CoolRemain: 420},
		{UID: "u3", Nickname: "spare", Credits: 1200, Total: 24000},
		{UID: "u4", Nickname: "retired", Disabled: true},
	}
	const buckets = 24
	reqs := make([]float64, buckets)
	toks := make([]float64, buckets)
	lats := make([]float64, buckets)
	labels := make([]string, buckets)
	for i := 0; i < buckets; i++ {
		// A daytime shape, so the curve has something to show.
		shape := 0.15 + 0.85*float64(i)/float64(buckets-1)
		if i%5 == 0 {
			shape *= 0.4
		}
		reqs[i] = 40 + 260*shape
		toks[i] = reqs[i] * 380
		lats[i] = 600 + 900*shape
		labels[i] = "2026-09-26T" + padHour(i)
	}
	return status.Snapshot{
		Reachable:  true,
		Healthy:    3,
		Total:      4,
		Version:    "1.11.6-panel",
		Uptime:     90061,
		Sticky:     2,
		Accounts:   accounts,
		UsageHours: buckets,
		Usage: status.Usage{
			Requests: 4218, Errors: 37, TotalTokens: 1_602_000,
			AvgLatencyMs: 812.5, AvgTPS: 41.7,
			Series: reqs, Tokens: toks, Latency: lats, Labels: labels,
		},
		LogTotal: 128, LogErrors: 3, LogTasks: 41,
	}
}

func padHour(i int) string {
	if i < 10 {
		return "0" + string(rune('0'+i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}
