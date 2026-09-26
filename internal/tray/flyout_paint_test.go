//go:build windows

package tray

import (
	"image/png"
	"os"
	"testing"

	"wbtray/internal/raster"
	"wbtray/internal/theme"
	"wbtray/internal/traymenu"
)

// sampleMenu is a menu with one of every row kind, so the painter is exercised
// across the whole vocabulary rather than only the easy rows.
func sampleMenu() []traymenu.Item {
	return []traymenu.Item{
		{Kind: traymenu.ValueRow, Text: "wbtray · OK", Value: "http://127.0.0.1:7863",
			Dot: raster.Hex("#3ddc97"), Bold: true},
		traymenu.Value("Accounts", "3/4"),
		traymenu.Value("Credits", "48,250"),
		traymenu.Value("Requests · 24h", "128k / 913 err"),
		traymenu.Separator(),
		traymenu.Submenu("Accounts · 3/4", []traymenu.Item{
			{Kind: traymenu.ValueRow, Text: "laincat", Value: "21,400", Disabled: true,
				Dot: raster.Hex("#3ddc97")},
			{Kind: traymenu.ValueRow, Text: "backup", Value: "18,900 · 7m", Disabled: true,
				Dot: raster.Hex("#f5b544")},
			{Kind: traymenu.ValueRow, Text: "spare", Value: "disabled", Disabled: true,
				Dot: raster.Hex("#6b7488")},
		}).Expanded(true),
		traymenu.Command(1, "Open usage chart window"),
		traymenu.Separator(),
		traymenu.Submenu("Tray style", []traymenu.Item{
			{Kind: traymenu.StyleRow, ID: 2000, Text: "Ring", Checked: true,
				Preview: raster.New(20, 20)},
			{Kind: traymenu.StyleRow, ID: 2001, Text: "Bars",
				Preview: raster.New(20, 20)},
			{Kind: traymenu.StyleRow, ID: 2002, Text: "Sparkline",
				Preview: raster.New(20, 20)},
			{Kind: traymenu.StyleRow, ID: 2003, Text: "Mascot",
				Preview: raster.New(20, 20)},
		}).Expanded(true),
		traymenu.Submenu("Tray colours", []traymenu.Item{
			traymenu.Radio(2200, "Panel dark", true),
			traymenu.Radio(2201, "Neon", false),
		}),
		traymenu.Radio(999, "Classic menu (system style)", false),
		traymenu.Separator(),
		traymenu.Submenu("Gateway · running, PID 16780", []traymenu.Item{
			traymenu.Value("PID", "16780"),
			traymenu.Command(2701, "Stop gateway"),
			traymenu.Command(2702, "Restart gateway"),
			traymenu.Command(2703, "Hide gateway console window"),
		}),
		traymenu.Command(2601, "Open console panel"),
		traymenu.Check(2609, "Start with Windows (tray)", true),
		traymenu.Command(2610, "Exit tray"),
	}
}

// TestPaintMenuProducesAPlate checks the painter runs and fills a plausible area
// of the canvas, which is the part a layout error would break silently: a menu
// laid out with a zero width paints nothing and looks, from the outside, exactly
// like a menu that failed to open.
func TestPaintMenuProducesAPlate(t *testing.T) {
	items := sampleMenu()
	f := &flyout{hover: -1, pressed: -1, subOwner: 5}
	// The layout is computed from the model exactly as it is when the menu opens.
	f.mu.Lock()
	f.theme = theme.Neon()
	f.scale = 1
	f.mu.Unlock()
	f.layout(items)

	f.mu.Lock()
	rows, w, h := f.rows, f.width, f.height
	f.mu.Unlock()
	if len(rows) == 0 {
		t.Fatal("layout produced no rows")
	}
	if w < 200 || h < 200 {
		t.Fatalf("layout is %dx%d, which is too small for the model", w, h)
	}

	c := raster.New(w, h)
	paintMenu(c, rows, 1, theme.Neon(), 3, 0)

	painted := 0
	for y := 0; y < c.H; y++ {
		for x := 0; x < c.W; x++ {
			if c.At(x, y).A > 200 {
				painted++
			}
		}
	}
	// The plate is most of the window; anything under half means the paint was
	// clipped or the size disagreed with the layout.
	if area := c.W * c.H; painted < area/2 {
		t.Fatalf("only %d of %d pixels are opaque", painted, area)
	}

	// The inspected sheet is written out so a change in the painter can be
	// looked at rather than only measured. It is a build artefact, not a test
	// fixture: nothing reads it back.
	if os.Getenv("WBTRAY_PAINT_SHEET") != "" {
		size := 2
		big := c.Scale(c.W*size/1, c.H*size/1)
		file, err := os.Create(os.Getenv("WBTRAY_PAINT_SHEET"))
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if err := png.Encode(file, big.Image()); err != nil {
			t.Fatal(err)
		}
	}
}

// TestHitRowMatchesTheLayout checks that the row under the cursor is the row
// that was drawn there, which is the property that keeps the painter and the
// hit test from drifting apart.
func TestHitRowMatchesTheLayout(t *testing.T) {
	f := &flyout{hover: -1, pressed: -1, subOwner: -1}
	f.mu.Lock()
	f.scale = 1
	f.mu.Unlock()
	f.layout(sampleMenu())

	f.mu.Lock()
	rows := f.rows
	f.mu.Unlock()

	for i, r := range rows {
		if r.item.Kind == traymenu.SeparatorRow {
			continue
		}
		// The middle of the row, in logical window coordinates.
		x, y := r.rect.W/2, r.rect.Y+r.rect.H/2
		if got := f.hitRow(x, y); got != i {
			t.Errorf("hitRow(%.1f, %.1f) = %d, want %d", x, y, got, i)
		}
	}
	// A point outside the list belongs to no row.
	if got := f.hitRow(-10, -10); got != -1 {
		t.Errorf("hitRow outside the list = %d, want -1", got)
	}
}

// TestCollapsedSubmenuHidesItsChildren checks the expansion rule: a submenu that
// was not asked to expand contributes its own row and nothing else.
func TestCollapsedSubmenuHidesItsChildren(t *testing.T) {
	items := []traymenu.Item{
		traymenu.Submenu("more", []traymenu.Item{
			traymenu.Command(1, "hidden"),
		}),
	}
	flat := flatten(items, -1)
	if len(flat) != 1 {
		t.Fatalf("a collapsed submenu contributed %d rows, want 1", len(flat))
	}
	flat = flatten(items, 0)
	if len(flat) != 2 {
		t.Fatalf("an expanded submenu contributed %d rows, want 2", len(flat))
	}
	if flat[1].depth == 0 {
		t.Error("a child row is not indented")
	}
}

// TestMenuHasAFrameAndKeepsContentInsideIt is the spacing rule.
//
// The first drawn menu had neither: rows ran to within a pixel of the plate's
// edge, and there was no frame at all, so the menu read as a rectangle of colour
// that happened to have text in it. What is checked here is the two things that
// were wrong — a border is drawn, and nothing else is drawn on top of it.
func TestMenuHasAFrameAndKeepsContentInsideIt(t *testing.T) {
	th := theme.Neon()
	f := &flyout{hover: -1, pressed: -1, subOwner: -1}
	f.mu.Lock()
	f.theme = th
	f.scale = 1
	f.mu.Unlock()
	f.layout(sampleMenu())

	f.mu.Lock()
	rows, w, h := f.rows, f.width, f.height
	f.mu.Unlock()
	c := raster.New(w, h)
	paintMenu(c, rows, 1, th, -1, 0)

	// A frame: some pixel on the plate's edge is the border colour and not the
	// surface colour.
	foundEdge := false
	for x := 0; x < w && !foundEdge; x++ {
		if near(c.At(x, int(foShadowPad)), th.MenuEdge) {
			foundEdge = true
		}
	}
	if !foundEdge {
		t.Error("the menu has no frame on its top edge")
	}

	// And the frame is not painted over: a border pixel replaced by the
	// highlight would mean the selection is escaping the edge.
	// The rows' own rectangles start below the edge, which is the arithmetic the
	// painter relies on.
	for _, r := range rows {
		if r.rect.Y < foShadowPad+foEdge {
			t.Errorf("row %q starts at y=%.0f, inside the frame at %.0f",
				r.item.Text, r.rect.Y, foShadowPad+foEdge)
		}
	}
}

// TestRowNaturalWidthIncludesBothInsets pins the arithmetic the layout and the
// painter share, because a disagreement between them is what clips labels.
func TestRowNaturalWidthIncludesBothInsets(t *testing.T) {
	it := traymenu.Command(1, "Open the console panel")
	got := rowNaturalWidth(it, 0)
	want := foInset + foCheckW +
		float64(drawWidth(it.Text, foFontSize)) + foInset
	if got != want {
		t.Fatalf("rowNaturalWidth = %.0f, want %.0f", got, want)
	}
}

// near reports whether two colours are within a small distance of each other,
// which is how an antialiased edge is matched against the colour it came from.
func near(a, b raster.RGBA) bool {
	diff := func(x, y uint8) int {
		d := int(x) - int(y)
		if d < 0 {
			return -d
		}
		return d
	}
	return diff(a.R, b.R) < 12 && diff(a.G, b.G) < 12 && diff(a.B, b.B) < 12 && a.A > 200
}

// TestRowsFitInsideThePlate is the invariant the first drawn menu broke: the
// window was sized from a width that did not account for every column, so the
// labels that needed the most room were the ones clipped.
//
// It is checked here rather than by eye because the failure is invisible at the
// default font size and obvious only on the row with the longest text.
func TestRowsFitInsideThePlate(t *testing.T) {
	f := &flyout{hover: -1, pressed: -1, subOwner: -1}
	f.mu.Lock()
	f.scale = 1
	f.mu.Unlock()
	f.layout(sampleMenu())

	f.mu.Lock()
	rows, width := append([]row{}, f.rows...), f.width
	f.mu.Unlock()

	for _, r := range rows {
		if r.item.Kind == traymenu.SeparatorRow {
			continue
		}
		need := rowNaturalWidth(r.item, r.depth)
		if need > float64(width)+0.5 {
			t.Errorf("row %q needs %.0f but the plate is %d wide", r.item.Text, need, width)
		}
	}
}
