package theme

import (
	"testing"

	"wbtray/internal/raster"
)

// TestEveryMarkIsLegibleOnTheTaskbar is the property the whole design rests on.
//
// The icon draws no plate, so every colour in a palette is read directly against
// the taskbar behind it. The rule is the accessibility one for a graphical
// object: three to one, below which a shape stops being a shape and becomes a
// smudge — which for a status icon means the reading is not merely unattractive
// but absent.
//
// This is a stronger test than it looks. The palette adjusts the operator's own
// accent to reach the threshold, and the accent comes from the registry, so this
// is what checks that the adjustment works for a colour nobody chose for this
// purpose.
func TestEveryMarkIsLegibleOnTheTaskbar(t *testing.T) {
	// A spread of accents: the Windows default, the one measured on this machine
	// (a dark olive that fails the threshold unaided), a mid grey, a saturated
	// blue, and white.
	accents := []Accent{
		{},
		{Known: true, Colour: raster.Hex("#587500")},
		{Known: true, Colour: raster.Hex("#808080")},
		{Known: true, Colour: raster.Hex("#0078d4")},
		{Known: true, Colour: raster.Hex("#ffffff")},
		{Known: true, Colour: raster.Hex("#000000")},
	}
	for _, a := range accents {
		for _, p := range All(a) {
			for _, dark := range []bool{true, false} {
				got := p.On(!dark)
				bg := got.Taskbar()
				for _, m := range []struct {
					name string
					c    raster.RGBA
				}{
					{"Ink", got.Ink}, {"InkDim", got.InkDim}, {"Accent", got.Accent},
					{"OK", got.OK}, {"Warn", got.Warn}, {"Bad", got.Bad},
				} {
					if r := Contrast(m.c, bg); r < minContrast {
						t.Errorf("%s (light=%v): %s has contrast %.2f against the taskbar, want %.1f",
							got.Name, !dark, m.name, r, minContrast)
					}
				}
			}
		}
	}
}

// TestMonochromeKeepsOnlyTwoHues is what makes the palette monochrome.
//
// A healthy reading is drawn in the ink rather than in a colour, and the two
// states that have to be noticed keep one. If a third colour appears the palette
// has stopped being the neutral choice, which is the only reason to pick it.
func TestMonochromeKeepsOnlyTwoHues(t *testing.T) {
	for _, light := range []bool{false, true} {
		p := Mono().On(light)
		if p.OK != p.Ink {
			t.Errorf("monochrome (light=%v) draws a healthy mark in %+v, want the ink %+v",
				light, p.OK, p.Ink)
		}
		if p.Accent != p.Ink {
			t.Errorf("monochrome (light=%v) has an accent of %+v, want the ink", light, p.Accent)
		}
		if p.Warn == p.Ink || p.Bad == p.Ink {
			t.Errorf("monochrome (light=%v) lost a state colour, so a problem would look healthy", light)
		}
	}
}

// TestOnFlipsPolarity checks that the light rendering really is the opposite of
// the dark one, which is what makes "follow Windows" worth having.
func TestOnFlipsPolarity(t *testing.T) {
	for _, p := range All(Accent{}) {
		dark := p.On(false)
		light := p.On(true)
		if dark.IsLight() {
			t.Errorf("%s reports itself as light before being turned", p.Name)
		}
		if !light.IsLight() {
			t.Errorf("%s.On(true) is still dark", p.Name)
		}
		if light.Name != p.Name {
			t.Errorf("%s.On(true) changed the name to %s", p.Name, light.Name)
		}
		if Luminance(light.Ink) >= Luminance(dark.Ink) {
			t.Errorf("%s.On(true) kept a light ink, which is invisible on a white taskbar", p.Name)
		}
	}
	// Turning twice is the identity, which is what makes the setting a toggle
	// rather than something that drifts every time it is applied.
	for _, p := range All(Accent{}) {
		back := p.On(true).On(false)
		if back != p.On(false) {
			t.Errorf("%s does not survive a round trip through the light rendering", p.Name)
		}
	}
}

// TestTheAccentStaysRecognisable is the property the system palette rests on.
//
// The accent is the system's colour, and a palette that replaced it with one of
// its own would no longer be following the system. It may be moved along its own
// lightness to reach the contrast threshold — Windows does the same to its own
// accent in light mode — but its hue has to survive.
func TestTheAccentStaysRecognisable(t *testing.T) {
	accent := Accent{Known: true, Colour: raster.Hex("#0078d4")}
	dark := System(accent)
	// An accent that already reads needs no adjustment at all, so it comes
	// through untouched.
	if dark.Accent != accent.Colour {
		t.Errorf("the dark palette changed a legible accent from %+v to %+v",
			accent.Colour, dark.Accent)
	}
	// A middling blue already reads on a white taskbar, so it comes through
	// untouched: the adjustment exists to rescue an unusable accent, not to
	// repaint a usable one.
	light := dark.On(true)
	if Contrast(accent.Colour, taskbarLight) >= minContrast {
		if light.Accent != accent.Colour {
			t.Errorf("a legible accent was altered from %+v to %+v", accent.Colour, light.Accent)
		}
	}
	// A pale one does not read, and has to move — while staying blue, because the
	// hue is what makes it the system's accent rather than a colour of this
	// program's own.
	pale := System(Accent{Known: true, Colour: raster.Hex("#9ecbff")})
	if Contrast(pale.Accent, taskbarLight) >= minContrast {
		t.Fatalf("the pale accent %+v already reads, so this case proves nothing", pale.Accent)
	}
	fixed := pale.On(true)
	if fixed.Accent == pale.Accent {
		t.Error("On(true) left an unreadable accent unchanged")
	}
	if fixed.Accent.B < fixed.Accent.R || fixed.Accent.B < fixed.Accent.G {
		t.Errorf("the rescued accent %+v lost its blue", fixed.Accent)
	}
	if r := Contrast(fixed.Accent, taskbarLight); r < minContrast {
		t.Errorf("the rescued accent still only reaches %.2f", r)
	}

	// An accent that cannot be read falls back to the default rather than to a
	// zero colour, which would draw nothing at all.
	if fallback := System(Accent{}); fallback.Accent != DefaultAccent {
		t.Errorf("an unknown accent produced %+v, want the default", fallback.Accent)
	}
}

// TestResolveFollowsTheSystem is the behaviour the appearance setting exists for.
func TestResolveFollowsTheSystem(t *testing.T) {
	if got := Resolve(SystemName, Auto, true, Accent{}); got.IsLight() {
		t.Error("the system palette on a dark system resolved to the light rendering")
	}
	if got := Resolve(SystemName, Auto, false, Accent{}); !got.IsLight() {
		t.Error("the system palette on a light system resolved to the dark rendering")
	}
	// An explicit mode ignores the system.
	if got := Resolve(MonoName, AlwaysDark, false, Accent{}); got.IsLight() {
		t.Error("always-dark on a light system resolved to the light rendering")
	}
	if got := Resolve(MonoName, AlwaysLight, true, Accent{}); !got.IsLight() {
		t.Error("always-light on a dark system resolved to the dark rendering")
	}
}

// TestRetiredNamesLandOnAPalette is the upgrade path: a configuration written
// when there were other palettes must still open on one of the two rather than
// on nothing.
func TestRetiredNamesLandOnAPalette(t *testing.T) {
	for _, name := range []string{"panel", "light", "amber", "candy", "neon", "", "nonsense"} {
		if got := ByName(name, Accent{}); got.Name != SystemName && got.Name != MonoName {
			t.Errorf("ByName(%q) returned %q", name, got.Name)
		}
	}
	// The three that carried no hue land on the neutral palette, because that is
	// what they were: a configuration asking for grey should not start following
	// the system accent.
	for _, name := range []string{"light", "plain", "grey"} {
		if got := ByName(name, Accent{}); got.Name != MonoName {
			t.Errorf("the retired %q palette resolved to %q, want the monochrome one", name, got.Name)
		}
	}
	// And the coloured ones follow the system, which is the palette that replaced
	// them.
	for _, name := range []string{"panel", "amber", "candy", "neon"} {
		if got := ByName(name, Accent{}); got.Name != SystemName {
			t.Errorf("the retired %q palette resolved to %q, want the system one", name, got.Name)
		}
	}
}

func TestParseAppearance(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Appearance
	}{
		{"", Auto},
		{"auto", Auto},
		{"dark", AlwaysDark},
		{"DARK", AlwaysDark},
		{"light", AlwaysLight},
		{"nonsense", Auto},
	} {
		if got := ParseAppearance(tc.in); got != tc.want {
			t.Errorf("ParseAppearance(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestContrastIsTheWcagRatio pins the arithmetic, because every guarantee above
// is stated in terms of it: if this drifts, the thresholds stop meaning what the
// comments say they mean.
func TestContrastIsTheWcagRatio(t *testing.T) {
	white, black := raster.Hex("#ffffff"), raster.Hex("#000000")
	if got := Contrast(white, black); got < 20.9 || got > 21.1 {
		t.Errorf("white on black is %.2f, want 21", got)
	}
	if got := Contrast(white, white); got < 0.99 || got > 1.01 {
		t.Errorf("white on white is %.2f, want 1", got)
	}
	// It is symmetric, which is what lets a palette be checked either way round.
	if Contrast(white, black) != Contrast(black, white) {
		t.Error("Contrast is not symmetric")
	}
}
