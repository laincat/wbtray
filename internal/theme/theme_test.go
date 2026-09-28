package theme

import (
	"math"
	"testing"

	"wbtray/internal/raster"
)

// TestEveryInkIsLegibleOnItsSurface is the claim both tones are built to satisfy,
// and the reason there is no colour arithmetic in this package any more.
//
// Every colour in a tone is read against the surface it is drawn on: the window's
// text against the window's background, a state colour against a card, and a
// sidebar label against the rail. Three to one is the accessibility threshold for a
// graphical object, and it is the right bar for a status mark: below it a shape
// stops being a shape and becomes a smudge, which for a tray icon means the reading
// is not merely unattractive but absent.
//
// The two tones are written down rather than derived, so this is what checks that
// the values that were written down are the ones that work.
func TestEveryInkIsLegibleOnItsSurface(t *testing.T) {
	inks := func(p Palette) map[string]raster.RGBA {
		return map[string]raster.RGBA{
			"text": p.Text, "muted": p.Muted, "faint": p.Faint,
			"green": p.Green, "amber": p.Warn, "red": p.Bad, "blue": p.Blue,
		}
	}
	surfaces := func(p Palette) map[string]raster.RGBA {
		return map[string]raster.RGBA{"bg": p.BG, "surface": p.Surface, "rail": p.Rail}
	}
	for _, light := range []bool{false, true} {
		p := On(light)
		for iname, ink := range inks(p) {
			for sname, sfc := range surfaces(p) {
				if r := Contrast(ink, sfc); r < 3.0 {
					t.Errorf("light=%v: %s on %s is %.2f, want at least 3",
						light, iname, sname, r)
				}
			}
		}
	}
}

// TestTheTonesAreOpposite is what makes following Windows worth having: a mark that
// reads on a dark taskbar is invisible on a light one.
func TestTheTonesAreOpposite(t *testing.T) {
	dark, light := On(false), On(true)
	if Luminance(light.Text) >= Luminance(dark.Text) {
		t.Error("the light tone does not have the darker text")
	}
	if Luminance(light.BG) <= Luminance(dark.BG) {
		t.Error("the light tone does not have the lighter background")
	}
	if dark.Light || !light.Light {
		t.Error("a tone reports the wrong polarity")
	}
}

// TestTheAccentIsNeutral is the property that keeps the tray quiet, and the reason
// there is one palette instead of one that wears the system accent.
//
// Colour in this design is reserved for the states that have to be noticed. The
// accent — the colour of anything an operator can act on — is ink, which is what
// stops a tray full of marks from competing with the system's own icons beside it.
func TestTheAccentIsNeutral(t *testing.T) {
	for _, light := range []bool{false, true} {
		p := On(light)
		if p.Accent != p.Text {
			t.Errorf("light=%v: the accent is %+v, want the text colour %+v",
				light, p.Accent, p.Text)
		}
		// And it is grey: the three channels are within a couple of steps, which
		// is what "neutral" means for a colour that is not exactly equal.
		lo := min(p.Accent.R, min(p.Accent.G, p.Accent.B))
		hi := max(p.Accent.R, max(p.Accent.G, p.Accent.B))
		if hi-lo > 4 {
			t.Errorf("light=%v: the accent %+v is not neutral", light, p.Accent)
		}
	}
}

// TestTheStateColoursAreDistinct checks that the four states are visibly different
// colours, which is the point of spending colour on them at all.
//
// The measure is the red-mean weighted distance rather than the largest
// single-channel difference. That is not a nicety: the largest channel difference
// calls amber #fbbf24 and brown #a67c24 close because their red channels agree,
// and calls amber and red distant because their green ones do not — which is the
// opposite of what the eye reports. The weighting gives the green channel the most
// influence, because that is where most of a colour's perceived lightness lives.
//
// The threshold is deliberately loose. The closest pair in this design is amber and
// red in the light tone, at about 110, and that is a real limitation of choosing
// warm colours for two states: at the size a tray icon draws them they are
// distinguishable side by side and not from memory. It is acceptable here because
// nothing depends on it — the icon shows one state at a time, and everywhere two
// states appear together in the window the state is also spelled out in a word, so
// the colour is the redundant half of the encoding rather than the only half.
func TestTheStateColoursAreDistinct(t *testing.T) {
	for _, light := range []bool{false, true} {
		p := On(light)
		states := map[string]raster.RGBA{
			"green": p.Green, "amber": p.Warn, "red": p.Bad, "blue": p.Blue,
		}
		names := []string{"green", "amber", "red", "blue"}
		for i, a := range names {
			for _, b := range names[i+1:] {
				if d := distance(states[a], states[b]); d < 60 {
					t.Errorf("light=%v: %s and %s differ by only %.0f, so they read as one state",
						light, a, b, d)
				}
			}
		}
	}
}

// TestRetiredNamesLandOnThePalette is the upgrade path: a configuration written
// when there were other palettes must still open.
func TestRetiredNamesLandOnThePalette(t *testing.T) {
	for _, name := range []string{"system", "mono", "neon", "panel", "amber",
		"candy", "light", "plain", "grey", "", "nonsense"} {
		if got := Normalize(name); got != DefaultName {
			t.Errorf("Normalize(%q) = %q, want %q", name, got, DefaultName)
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

// TestResolveFollowsTheSystem is the behaviour the appearance setting exists for.
func TestResolveFollowsTheSystem(t *testing.T) {
	if got := Resolve(DefaultName, Auto, true, Accent{}); got.Light {
		t.Error("a dark system resolved to the light tone")
	}
	if got := Resolve(DefaultName, Auto, false, Accent{}); !got.Light {
		t.Error("a light system resolved to the dark tone")
	}
	if got := Resolve(DefaultName, AlwaysDark, false, Accent{}); got.Light {
		t.Error("always-dark on a light system resolved to the light tone")
	}
	if got := Resolve(DefaultName, AlwaysLight, true, Accent{}); !got.Light {
		t.Error("always-light on a dark system resolved to the dark tone")
	}
}

// TestContrastIsTheWcagRatio pins the arithmetic, because every guarantee above is
// stated in terms of it: if this drifts, the thresholds stop meaning what the
// comments say they mean.
func TestContrastIsTheWcagRatio(t *testing.T) {
	white, black := raster.Hex("#ffffff"), raster.Hex("#000000")
	if got := Contrast(white, black); got < 20.9 || got > 21.1 {
		t.Errorf("white on black is %.2f, want 21", got)
	}
	if got := Contrast(white, white); got < 0.99 || got > 1.01 {
		t.Errorf("white on white is %.2f, want 1", got)
	}
	if Contrast(white, black) != Contrast(black, white) {
		t.Error("Contrast is not symmetric")
	}
}

// distance is the red-mean weighted distance between two colours: a cheap
// approximation of how different they look, which weights the channels by the
// colour they are near rather than treating all three alike.
func distance(a, b raster.RGBA) float64 {
	dr := float64(a.R) - float64(b.R)
	dg := float64(a.G) - float64(b.G)
	db := float64(a.B) - float64(b.B)
	rmean := (float64(a.R) + float64(b.R)) / 2
	return math.Sqrt(
		(2+rmean/256)*dr*dr +
			4*dg*dg +
			(2+(255-rmean)/256)*db*db)
}

func min(a, b uint8) uint8 {
	if a < b {
		return a
	}
	return b
}

func max(a, b uint8) uint8 {
	if a > b {
		return a
	}
	return b
}
