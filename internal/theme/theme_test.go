package theme

import "testing"

// TestBothPalettesAreTransparent is the property the whole design now rests on:
// neither palette draws a plate, so the icon sits on the taskbar itself.
func TestBothPalettesAreTransparent(t *testing.T) {
	for _, th := range All() {
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
	for _, th := range All() {
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
	for _, th := range All() {
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
	for _, th := range All() {
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

// TestOnLightGivesTheMenuASurfaceOfItsOwn checks that the menu is converted along
// with the icon.
//
// A drawn menu is a window on the desktop, and one that stayed dark while the
// system's own menus went light would be the most conspicuous thing on screen.
// The rule is that the menu has to remain distinguishable from a white desktop,
// which means its border cannot also be white.
func TestOnLightGivesTheMenuASurfaceOfItsOwn(t *testing.T) {
	for _, th := range All() {
		light := th.OnLight()
		if background := lum(light.MenuBg); background < 200 {
			t.Errorf("%s.OnLight() left a dark menu background (%.0f)", th.Name, background)
		}
		if ink := lum(light.MenuInk); ink > 100 {
			t.Errorf("%s.OnLight() left a light menu ink (%.0f)", th.Name, ink)
		}
		// The border must differ from the surface, or the window has no edge.
		if diff := abs(lum(light.MenuEdge) - lum(light.MenuBg)); diff < 12 {
			t.Errorf("%s.OnLight(): the menu border differs from its surface by only %.0f", th.Name, diff)
		}
		// And the selected row must differ from the surface it sits on.
		if diff := abs(lum(light.MenuSel) - lum(light.MenuBg)); diff < 8 {
			t.Errorf("%s.OnLight(): the selection differs from the surface by only %.0f", th.Name, diff)
		}
	}
}

// TestResolveFollowsTheSystem is the behaviour the appearance setting exists for.
func TestResolveFollowsTheSystem(t *testing.T) {
	if got := Resolve(NeonName, Auto, true); got.IsLight() {
		t.Errorf("neon on a dark system resolved to the light rendering")
	}
	if got := Resolve(NeonName, Auto, false); !got.IsLight() {
		t.Errorf("neon on a light system resolved to the dark rendering")
	}
	// An explicit mode ignores the system.
	if got := Resolve(MonoName, AlwaysDark, false); got.IsLight() {
		t.Errorf("always-dark on a light system resolved to the light rendering")
	}
	if got := Resolve(MonoName, AlwaysLight, true); !got.IsLight() {
		t.Errorf("always-light on a dark system resolved to the dark rendering")
	}
}

// TestRetiredNamesLandOnAPalette is the upgrade path: a configuration written
// when there were six palettes must still open on one of the two rather than on
// nothing.
func TestRetiredNamesLandOnAPalette(t *testing.T) {
	for _, name := range []string{"panel", "light", "amber", "candy", "", "nonsense"} {
		got := ByName(name)
		if got.Name != NeonName && got.Name != MonoName {
			t.Errorf("ByName(%q) returned %q", name, got.Name)
		}
	}
	if got := ByName("light"); got.Name != MonoName {
		t.Errorf("the retired light palette resolved to %q, want the monochrome one", got.Name)
	}
	if got := ByName("panel"); got.Name != NeonName {
		t.Errorf("the retired panel palette resolved to %q, want neon", got.Name)
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
