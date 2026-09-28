//go:build windows

package tray

import (
	"testing"

	"wbtray/internal/app"
	"wbtray/internal/config"
	"wbtray/internal/i18n"
	"wbtray/internal/raster"
	"wbtray/internal/status"
	"wbtray/internal/theme"
	"wbtray/internal/traymenu"
)

// TestConnectedMenuDrawsLiveNumbers exercises the path from the gateway's JSON
// through the client into the menu, against a real HTTP server rather than a
// stubbed client: a stub at that boundary would skip the parsing as well as the
// menu.
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

	// The state row shows the credit total, and the account row the ready pair: the
	// two figures the header is worth opening for.
	items := a.Menu()
	if items[0].Value == "" {
		t.Errorf("the state row carries no value:\n%s", describe(items))
	}
	var accountRow traymenu.Item
	for _, it := range items {
		if it.Text == i18n.T("en", "menu.accounts") {
			accountRow = it
		}
	}
	if !contains(accountRow.Value, "1/3") {
		t.Errorf("the account row shows %q, want the ready/total pair", accountRow.Value)
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
// cmd/preview, run as a test so a style that panics on a missing metric is caught by
// the ordinary test command.
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
				}
			}
		}
	}
}

// TestIconAdaptsToHealth checks the property the whole design rests on: an icon for
// a broken gateway does not look like an icon for a healthy one.
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

// samePixels reports whether two icons are identical, which is how a test tells
// "the icon reacted to the state" from "the icon is drawn from the state in name
// only".
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

// fakePanel answers the endpoints the tray reads.
func newFakePanel(t *testing.T) *fakeServer {
	t.Helper()
	return startFakePanel(t)
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (len(needle) == 0 || indexOfSub(haystack, needle) >= 0)
}

func indexOfSub(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
