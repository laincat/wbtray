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
	"wbtray/internal/traymenu"
)

// TestStyleGalleryShowsEveryStyle draws the style submenu expanded, which is the
// view an operator uses to choose a look: every row has to carry a preview, and
// the previews have to differ from each other.
func TestStyleGalleryShowsEveryStyle(t *testing.T) {
	server := startFakePanel(t)
	defer server.Close()

	cfg := config.Default()
	cfg.BaseURL = server.URL
	cfg.APIKey = "test-key"
	cfg.Lang = "en"

	a := app.New(cfg, "", &stubGateway{pid: status.PIDInfo{Found: true, PID: 1}}, app.Options{})
	a.Refresh()
	if !a.Snapshot().Reachable {
		t.Fatalf("the fake panel was not reached: %v", a.Snapshot().Err)
	}

	items := a.Menu()
	// Find the style submenu by its rows rather than by its position: a test that
	// hard-codes the index breaks every time a row is added above it.
	styleIndex := -1
	for i, it := range items {
		if it.Kind == traymenu.SubmenuRow && len(it.Children) > 0 &&
			it.Children[0].Kind == traymenu.StyleRow {
			styleIndex = i
			break
		}
	}
	if styleIndex < 0 {
		t.Fatal("the menu has no style gallery")
	}

	f := &flyout{hover: -1, pressed: -1, subOwner: styleIndex, scale: 1}
	f.mu.Lock()
	f.theme = theme.Neon()
	f.mu.Unlock()
	f.layout(items)

	f.mu.Lock()
	rows, w, h := f.rows, f.width, f.height
	f.mu.Unlock()

	// Every style row in the flattened menu must carry a preview.
	seen := map[string]bool{}
	for _, r := range rows {
		if r.item.Kind != traymenu.StyleRow {
			continue
		}
		if r.item.Preview == nil {
			t.Errorf("style row %q has no preview", r.item.Text)
			continue
		}
		seen[r.item.Text] = true
	}
	if len(seen) != len(config.Styles) {
		t.Errorf("the gallery offers %d styles, want %d: %v", len(seen), len(config.Styles), seen)
	}

	c := raster.New(w, h)
	// A hovered row, so the test sheet shows the highlight's inset as well as the
	// plain state. It is the piece of the menu most likely to look wrong: a
	// highlight that reaches the border reads as a second frame.
	hover := -1
	for i, r := range rows {
		if r.item.Kind == traymenu.CommandRow {
			hover = i
			break
		}
	}
	paintMenu(c, rows, 1, theme.Neon(), hover, 0)
	if dir := os.Getenv("WBTRAY_MENU_DIR"); dir != "" {
		writeSheet(t, dir, "gallery", c)
	}
}
