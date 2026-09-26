//go:build windows

package tray

import (
	"os"
	"testing"

	"wbtray/internal/app"
	"wbtray/internal/config"
	"wbtray/internal/raster"
	"wbtray/internal/status"
	"wbtray/internal/theme"
)

// liveGateway is a fake panel server that answers the four endpoints the tray
// reads, so the connected state can be drawn and inspected without a gateway.
//
// It is deliberately a real HTTP server rather than a stubbed client: the point
// is to exercise the path from the gateway's JSON through the client into the
// menu, and a stub at the client boundary would skip the parsing as well as the
// layout.
func TestConnectedMenuDrawsLiveNumbers(t *testing.T) {
	server := newFakePanel(t)
	defer server.Close()

	cfg := config.Default()
	cfg.BaseURL = server.URL
	cfg.APIKey = "test-key"
	cfg.Lang = "en"

	a := app.New(cfg, "", &stubGateway{pid: status.PIDInfo{Found: true, PID: 16780, Started: true}}, app.Options{})
	a.Refresh()

	snap := a.Snapshot()
	if !snap.Reachable {
		t.Fatalf("the tray did not reach the fake panel: %v", snap.Err)
	}
	// The fixture is one ready account, one cooling and one disabled, so each
	// aggregate is checked against a different number: a single assertion on the
	// total would pass even if the three classifications were collapsed into one.
	if snap.Total != 3 {
		t.Fatalf("total = %d, want 3", snap.Total)
	}
	if snap.Ready() != 1 {
		t.Errorf("ready = %d, want 1 (one account is cooling and one disabled)", snap.Ready())
	}
	if snap.Cooling() != 1 {
		t.Errorf("cooling = %d, want 1", snap.Cooling())
	}
	if snap.Disabled() != 1 {
		t.Errorf("disabled = %d, want 1", snap.Disabled())
	}
	if snap.InFlight() != 1 {
		t.Errorf("in flight = %d, want 1", snap.InFlight())
	}
	if snap.CreditTotal() != 21000 {
		t.Fatalf("credits = %d, want 21000 (the disabled account is excluded)", snap.CreditTotal())
	}
	if snap.Usage.Requests != 1284 {
		t.Fatalf("requests = %d, want 1284", snap.Usage.Requests)
	}
	if len(snap.Usage.Series) != 24 {
		t.Fatalf("series has %d buckets, want 24", len(snap.Usage.Series))
	}

	items := a.Menu()
	f := &flyout{hover: -1, pressed: -1, subOwner: -1}
	f.mu.Lock()
	f.theme = theme.Neon()
	f.scale = 1
	f.mu.Unlock()
	f.layout(items)
	f.mu.Lock()
	rows, w, h := f.rows, f.width, f.height
	f.mu.Unlock()

	c := raster.New(w, h)
	paintMenu(c, rows, 1, theme.Neon(), -1, 0)
	if dir := os.Getenv("WBTRAY_MENU_DIR"); dir != "" {
		writeSheet(t, dir, "connected-en", c)
	}

	// The same menu in Chinese, which is the other half of the promise: a
	// translation that overflows the plate is a layout failure, and the two
	// languages have different lengths for almost every row.
	cfgZh := cfg
	cfgZh.Lang = "zh"
	aZh := app.New(cfgZh, "", &stubGateway{pid: status.PIDInfo{Found: true, PID: 16780, Started: true}}, app.Options{})
	aZh.Refresh()
	fZh := &flyout{hover: -1, pressed: -1, subOwner: -1}
	fZh.mu.Lock()
	fZh.theme = theme.Neon()
	fZh.scale = 1
	fZh.mu.Unlock()
	fZh.layout(aZh.Menu())
	fZh.mu.Lock()
	rowsZh, wZh, hZh := fZh.rows, fZh.width, fZh.height
	fZh.mu.Unlock()
	cZh := raster.New(wZh, hZh)
	paintMenu(cZh, rowsZh, 1, theme.Neon(), -1, 0)
	if dir := os.Getenv("WBTRAY_MENU_DIR"); dir != "" {
		writeSheet(t, dir, "connected-zh", cZh)
	}

	// The tooltip is the other surface the same numbers reach.
	tip := a.Tooltip()
	if len(tip) == 0 {
		t.Fatal("the tooltip is empty")
	}
	if !contains(tip, "21,000") {
		t.Errorf("the tooltip does not carry the credit total: %q", tip)
	}
}

// TestIconRendersForEveryStyleAndTheme is the cheap version of the sheet in
// cmd/preview, run as a test so a style that panics on a missing metric is
// caught by the ordinary test command.
func TestIconRendersForEveryStyleAndTheme(t *testing.T) {
	server := newFakePanel(t)
	defer server.Close()

	cfg := config.Default()
	cfg.BaseURL = server.URL
	// The fake panel requires the key, so the tray has to be given it: without it
	// every request is a 401 and the icons below would all be drawn for an
	// unreachable gateway, which is a different drawing entirely.
	cfg.APIKey = "test-key"
	a := app.New(cfg, "", &stubGateway{pid: status.PIDInfo{Found: true, PID: 1}}, app.Options{})
	a.Refresh()
	snap := a.Snapshot()
	if !snap.Reachable {
		t.Fatalf("the tray did not reach the fake panel: %v", snap.Err)
	}

	for _, th := range theme.All() {
		for _, style := range config.Styles {
			for _, metric := range config.Metrics {
				for _, size := range []int{16, 20, 32} {
					c := renderForTest(t, style, metric, size, th, snap)
					if c == nil || c.W != size {
						t.Fatalf("%s/%s/%s at %d produced no icon", th.Name, style, metric, size)
					}
					opaque := 0
					for y := 0; y < c.H; y++ {
						for x := 0; x < c.W; x++ {
							if c.At(x, y).A > 32 {
								opaque++
							}
						}
					}
					// Every style paints something: a halo, a ring, a glyph.
					if opaque < size*size/12 {
						t.Errorf("%s/%s/%s at %d is nearly empty (%d pixels)",
							th.Name, style, metric, size, opaque)
					}
					if dir := os.Getenv("WBTRAY_ICON_DIR"); dir != "" && size == 32 {
						writeIconSheet(t, dir, th.Name, style, metric, c)
					}
				}
			}
		}
	}
}

// TestIconAdaptsToHealth checks the property the whole design rests on: an icon
// for a broken gateway does not look like an icon for a healthy one.
func TestIconAdaptsToHealth(t *testing.T) {
	healthy := status.Snapshot{
		Reachable: true, Total: 2, Healthy: 2,
		Accounts: []status.Account{{UID: "a", Credits: 100}, {UID: "b", Credits: 100}},
	}
	down := status.Snapshot{Reachable: false}

	th := theme.Neon()
	a := iconCanvasFor(t, healthy, th)
	b := iconCanvasFor(t, down, th)
	if samePixels(a, b) {
		t.Fatal("a reachable and an unreachable gateway render identically")
	}
}

func iconCanvasFor(t *testing.T, snap status.Snapshot, th theme.Theme) *raster.Canvas {
	t.Helper()
	return renderForTest(t, config.StyleRing, config.MetricAccounts, 32, th, snap)
}

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

// fakePanel answers the tray's four endpoints.
func newFakePanel(t *testing.T) *fakeServer {
	t.Helper()
	return startFakePanel(t)
}
