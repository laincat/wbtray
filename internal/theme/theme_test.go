package theme

import (
	"testing"

	"wbtray/internal/raster"
)

// TestBothPalettesAreTransparent is the property the whole design now rests on:
// neither palette draws a plate, so the icon sits on the taskbar itself.
func TestBothPalettesAreTransparent(t *testing.T) {
	for _, th := range All(Accent{}) {
		if th.Halo.A == 0 {
			t.Errorf("%s has no halo, so its glyph would have nothing to sit on", th.Name)
		}
		if lum(th.Halo) == 0 && th.Halo.A == 255 {
			t.Errorf("%s has an opaque black halo, which is a plate by another name", th.Name)
		}
	}
}

// TestPalettesAreLegibleOnTheirOwnBackground is the contrast rule: every mark
// the icon draws has to be distinguishable from the halo behind it. A palette
// whose accent matches its halo is one whose icon is invisible.
func TestPalettesAreLegibleOnTheirOwnBackground(t *testing.T) {
	for _, th := range All(Accent{}) {
		background := lum(th.Halo)
		for _, mark := range []struct {
			name  string
			value float64
		}{
			{"Accent", lum(th.Accent)},
			{"OK", lum(th.OK)},
			{"Warn", lum(th.Warn)},
			{"Bad", lum(th.Bad)},
			{"Ink", lum(th.Ink)},
			{"InkDim", lum(th.InkDim)},
		} {
			if diff := abs(mark.value - background); diff < 60 {
				t.Errorf("%s: %s has a luminance difference of only %.0f against the halo",
					th.Name, mark.name, diff)
			}
		}
	}
}

// TestOnLightFlipsPolarity checks that the light rendering really is the
// opposite of the dark one, which is what makes "follow Windows" worth having.
func TestOnLightFlipsPolarity(t *testing.T) {
	for _, th := range All(Accent{}) {
		light := th.OnLight()
		if th.IsLight() {
			t.Errorf("%s reports itself as light before being converted", th.Name)
		}
		if !light.IsLight() {
			t.Errorf("%s.OnLight() is still dark", th.Name)
		}
		if light.Name != th.Name {
			t.Errorf("%s.OnLight() changed the name to %s", th.Name, light.Name)
		}
		if lum(light.Ink) > 128 {
			t.Errorf("%s.OnLight() kept a light ink, which is invisible on white", th.Name)
		}
	}
}

// TestOnLightKeepsTheMarksLegible applies the same contrast rule to the
// converted palette, because the conversion is where a colour is most likely to
// end up too pale to see.
func TestOnLightKeepsTheMarksLegible(t *testing.T) {
	for _, th := range All(Accent{}) {
		light := th.OnLight()
		background := lum(light.Halo)
		for _, mark := range []struct {
			name  string
			value float64
		}{
			{"Accent", lum(light.Accent)},
			{"OK", lum(light.OK)},
			{"Warn", lum(light.Warn)},
			{"Bad", lum(light.Bad)},
			{"Ink", lum(light.Ink)},
		} {
			if diff := abs(mark.value - background); diff < 60 {
				t.Errorf("%s on light: %s differs from the halo by only %.0f",
					th.Name, mark.name, diff)
			}
		}
	}
}

// TestOnLightKeepsTheAccentRecognisable is the property the system palette rests on.
//
// The accent has to survive the conversion to a light taskbar with its hue intact: it is
// the system's colour, and a palette that turned it into a different one would no longer
// be following the system. It is darkened, because pale cyan on white is invisible, but
// its channel order must not change.
func TestOnLightKeepsTheAccentRecognisable(t *testing.T) {
	accent := Accent{Known: true, Colour: raster.Hex("#0078d4")}
	dark := System(accent)
	light := dark.OnLight()

	if light.Accent == dark.Accent {
		t.Error("OnLight left the accent unchanged, which is invisible on a white taskbar")
	}
	if lum(light.Accent) >= lum(dark.Accent) {
		t.Errorf("the light accent (%.0f) is not darker than the dark one (%.0f)",
			lum(light.Accent), lum(dark.Accent))
	}
	// The hue is what makes it the system's accent rather than a colour of this
	// program's own, so the dominant channel has to stay dominant.
	if light.Accent.B < light.Accent.R {
		t.Errorf("the light accent %+v lost its blue, so it is no longer the system's",
			light.Accent)
	}
}

// TestSystemPaletteUsesTheGivenAccent checks the one thing that makes the tray look like
// part of Windows: the mark is drawn in the same colour as the system's own highlights.
func TestSystemPaletteUsesTheGivenAccent(t *testing.T) {
	accent := raster.Hex("#c30052")
	th := System(Accent{Known: true, Colour: accent})

	if th.Accent != accent {
		t.Errorf("the palette's accent is %+v, want the one it was given", th.Accent)
	}
	if th.OK != accent {
		t.Errorf("a healthy mark is %+v, want the system accent %+v", th.OK, accent)
	}

	// And an accent that cannot be read falls back to the default rather than to a
	// zero colour, which would draw nothing at all.
	fallback := System(Accent{})
	if fallback.Accent != DefaultAccent {
		t.Errorf("an unknown accent produced %+v, want the default", fallback.Accent)
	}
}

// TestResolveFollowsTheSystem is the behaviour the appearance setting exists for.
func TestResolveFollowsTheSystem(t *testing.T) {
	if got := Resolve(SystemName, Auto, true, Accent{}); got.IsLight() {
		t.Errorf("the system palette on a dark system resolved to the light rendering")
	}
	if got := Resolve(SystemName, Auto, false, Accent{}); !got.IsLight() {
		t.Errorf("the system palette on a light system resolved to the dark rendering")
	}
	// An explicit mode ignores the system.
	if got := Resolve(MonoName, AlwaysDark, false, Accent{}); got.IsLight() {
		t.Errorf("always-dark on a light system resolved to the light rendering")
	}
	if got := Resolve(MonoName, AlwaysLight, true, Accent{}); !got.IsLight() {
		t.Errorf("always-light on a dark system resolved to the dark rendering")
	}
}

// TestRetiredNamesLandOnAPalette is the upgrade path: a configuration written when there
// were other palettes must still open on one of the two rather than on nothing.
func TestRetiredNamesLandOnAPalette(t *testing.T) {
	for _, name := range []string{"panel", "light", "amber", "candy", "neon", "", "nonsense"} {
		got := ByName(name, Accent{})
		if got.Name != SystemName && got.Name != MonoName {
			t.Errorf("ByName(%q) returned %q", name, got.Name)
		}
	}
	// The three that carried no hue before land on the neutral palette, because that is
	// what they were: a configuration asking for grey should not start following the
	// system accent.
	for _, name := range []string{"light", "plain", "grey"} {
		if got := ByName(name, Accent{}); got.Name != MonoName {
			t.Errorf("the retired %q palette resolved to %q, want the monochrome one", name, got.Name)
		}
	}
	// And the coloured ones follow the system, which is the palette that replaced them.
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

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
