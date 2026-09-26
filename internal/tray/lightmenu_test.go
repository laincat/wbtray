//go:build windows

package tray

import (
	"os"
	"testing"

	"wbtray/internal/app"
	"wbtray/internal/config"
	"wbtray/internal/raster"
	"wbtray/internal/status"
)

// TestLightMenuRenders draws the menu in the light appearance, which is the half
// of the design that is easiest to get wrong: every surface has to be converted
// together, and a single un-converted colour shows up as a dark band in a light
// menu.
func TestLightMenuRenders(t *testing.T) {
	server := startFakePanel(t)
	defer server.Close()

	cfg := config.Default()
	cfg.BaseURL = server.URL
	cfg.APIKey = "test-key"
	cfg.Lang = "en"
	cfg.Appearance = "light"

	a := app.New(cfg, "", &stubGateway{pid: status.PIDInfo{Found: true, PID: 16780, Started: true}}, app.Options{})
	a.Refresh()

	th := a.Theme()
	if !th.IsLight() {
		t.Fatal("the application did not resolve to the light appearance")
	}

	items := a.Menu()
	f := &flyout{hover: -1, pressed: -1, subOwner: -1}
	f.mu.Lock()
	f.theme = th
	f.scale = 1
	f.mu.Unlock()
	f.layout(items)

	f.mu.Lock()
	rows, w, h := f.rows, f.width, f.height
	f.mu.Unlock()
	c := raster.New(w, h)
	// A hovered row, so the selection colour is exercised too.
	paintMenu(c, rows, 1, th, 3, 0)

	// The menu has to actually be light: a single opaque pixel darker than the
	// surface would mean a colour was missed in the conversion.
	light := 0
	for y := 0; y < c.H; y++ {
		for x := 0; x < c.W; x++ {
			p := c.At(x, y)
			if p.A > 200 && (int(p.R)+int(p.G)+int(p.B))/3 > 200 {
				light++
			}
		}
	}
	if light < c.W*c.H/4 {
		t.Fatalf("only %d of %d pixels are light; the menu did not convert", light, c.W*c.H)
	}
	if dir := os.Getenv("WBTRAY_MENU_DIR"); dir != "" {
		writeSheet(t, dir, "connected-en-light", c)
	}
}
