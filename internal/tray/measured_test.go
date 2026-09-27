//go:build windows

package tray

import (
	"strings"
	"testing"

	"wbtray/internal/app"
	"wbtray/internal/config"
	"wbtray/internal/raster"
	"wbtray/internal/theme"
	"wbtray/internal/traymenu"
)

// pip is the colour the measurement tests give a row. Its value does not matter to a
// width; only whether a row has one.
var pip = theme.Neon().OK

// TestOwnerDrawRowsAreMeasuredNotGuessed is the bug that made the whole menu wide.
//
// A menu is as wide as its widest row, and the shell does not size a row it is not
// drawing: an owner-draw row states its own width through MEASUREITEMSTRUCT. The
// first version reported a flat 320 pixels for those rows regardless of what they
// said, which made every row in the menu — including the one-character ones — as
// wide as that number. The menu came out 430 pixels wide with two words in it.
//
// What the rows report has to be derived from their content, so this test checks the
// property that fixes it: a row with less in it reports less width.
func TestOwnerDrawRowsAreMeasuredNotGuessed(t *testing.T) {
	p := newStatusPainter()
	defer p.release()

	if p.hdc == 0 {
		t.Skip("no screen DC to measure text with")
	}

	short := traymenu.Status("OK", "1/1", pip, true)
	long := traymenu.Status("OK", "127.0.0.1:7863", pip, true)

	p.add(short)
	shortWidth := p.width
	p.width = 0

	p.add(long)
	longWidth := p.width

	if longWidth <= shortWidth {
		t.Fatalf("a row showing an address reports %d px and one showing %q reports %d; "+
			"the width is not coming from the content", longWidth, "1/1", shortWidth)
	}
	// And the number has to be in the range a menu row occupies rather than the
	// flat placeholder that caused the problem.
	if longWidth < 100 || longWidth > 260 {
		t.Errorf("a header row reports %d px, which is outside the range a row "+
			"that says one short word and one address should occupy", longWidth)
	}
}

// TestOwnerDrawRowsLeaveRoomForThePip checks the one thing the arithmetic has to
// remember: a row with a colour pip is drawn with a gutter the text starts after,
// so a row without one must not be measured as if it had it.
func TestOwnerDrawRowsLeaveRoomForThePip(t *testing.T) {
	p := newStatusPainter()
	defer p.release()
	if p.hdc == 0 {
		t.Skip("no screen DC to measure text with")
	}

	withPip := traymenu.Status("OK", "1/1", pip, true)
	withoutPip := traymenu.Status("OK", "1/1", raster.RGBA{}, true)

	p.add(withoutPip)
	plain := p.width
	p.width = 0
	p.add(withPip)
	withDot := p.width

	if withDot-plain != ownerDotGutter {
		t.Errorf("the pip changes the width by %d px, want the %d px gutter it is "+
			"drawn in", withDot-plain, ownerDotGutter)
	}
}

// TestMenuHeaderCarriesNoProgramName is the width decision, asserted.
//
// The header used to read "wbtray · OK". The program's own name in the program's own
// menu is a word that tells the reader nothing, and because a menu is as wide as its
// widest row, that word was setting the width of every other row.
func TestMenuHeaderCarriesNoProgramName(t *testing.T) {
	cfg := config.Default()
	cfg.Lang = "en"
	a := app.New(cfg, "", &stubGateway{}, app.Options{})

	head := a.Menu()[0]
	if strings.Contains(head.Text, "wbtray") {
		t.Errorf("the header reads %q, which repeats the program's own name", head.Text)
	}
}

// The measurement needs a screen DC, so the tests that use it skip rather than fail
// on a machine with no desktop.
