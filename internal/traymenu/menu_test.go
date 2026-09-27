package traymenu

import (
	"testing"

	"wbtray/internal/raster"
)

// TestStatusKeepsItsOwnDrawing pins the reason the status rows are painted by the
// front end: the system dims a row that cannot be clicked, and it dims the row's
// icon with it, so a health colour drawn as a bitmap comes out grey exactly where
// it carries the most meaning.
func TestStatusKeepsItsOwnDrawing(t *testing.T) {
	row := Status("wbtray", "127.0.0.1:7863", raster.Hex("#3ddc97"), true)
	if !row.OwnerDraw {
		t.Error("a status row is not a row the front end paints")
	}
	if !row.Disabled {
		t.Error("a status row should not be clickable")
	}
	if row.Kind != ValueRow {
		t.Errorf("a status row is kind %v, want ValueRow", row.Kind)
	}
	if row.Dot.A == 0 {
		t.Error("a status row carries no colour")
	}
}

// TestValueStaysSystemDrawn is the other half: everything that is not a status row
// should be drawn by the shell, because a row drawn by hand is a row that ages
// differently from the rest of the menu.
func TestValueStaysSystemDrawn(t *testing.T) {
	if row := Value("Credits", "48,250"); row.OwnerDraw {
		t.Error("an ordinary value row should be drawn by the system")
	}
	if row := Command(1, "Panel"); row.OwnerDraw {
		t.Error("a command row should be drawn by the system")
	}
}

// TestSubmenuCarriesItsValue checks that a submenu row can show a figure, which is
// how the gateway row carries its pid.
func TestSubmenuCarriesItsValue(t *testing.T) {
	row := Submenu("Gateway", []Item{Command(1, "Start")})
	row.Value = "PID 16780"
	if row.Kind != SubmenuRow {
		t.Fatalf("the row is kind %v, want SubmenuRow", row.Kind)
	}
	if row.Value == "" {
		t.Error("the submenu row lost its value")
	}
}
