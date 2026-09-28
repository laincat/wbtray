// Package theme holds the palettes the tray draws with.
//
// There are two, and they differ by one question: does the tray wear the colour
// Windows is using, or none at all?
//
// A palette names roles, not pixels. It says which colour "healthy" is and which
// colour "failed" is, and every part of the tray — the icon, the status row, the
// account pips — asks for a role. That is what stops a second, unrelated set of
// colours from growing beside it, which is what happened when the account rows
// carried four hex values hard-coded in the menu file that no palette had ever
// heard of.
//
// Nothing here draws a background. The tray's marks sit directly on the taskbar,
// the way the system's own icons do, so a palette is only ever a set of inks. The
// plate an earlier version drew behind every icon is gone: it read as a black
// square on a dark taskbar, which is the opposite of what a tray icon is for.
package theme

import (
	"math"
	"strings"

	"wbtray/internal/raster"
)

// Appearance is the follow-the-system setting.
//
// It decides which way round a palette is drawn, because a mark that reads on a
// dark taskbar is invisible on a light one. "Follow Windows" is worth having for
// that reason alone.
type Appearance string

// The appearance modes.
const (
	// Auto follows the Windows app theme.
	Auto Appearance = "auto"
	// AlwaysDark draws for a dark taskbar regardless of the system.
	AlwaysDark Appearance = "dark"
	// AlwaysLight draws for a light taskbar regardless of the system.
	AlwaysLight Appearance = "light"
)

// Appearances is the menu order.
var Appearances = []Appearance{Auto, AlwaysDark, AlwaysLight}

// ParseAppearance maps a configuration value onto a supported mode.
func ParseAppearance(s string) Appearance {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "dark", "always_dark":
		return AlwaysDark
	case "light", "always_light":
		return AlwaysLight
	default:
		return Auto
	}
}

// AppearanceLabel is the mode's name in the menu's language.
func AppearanceLabel(a Appearance, lang string) string {
	zh := lang == "zh"
	switch a {
	case AlwaysDark:
		if zh {
			return "固定暗色"
		}
		return "Always dark"
	case AlwaysLight:
		if zh {
			return "固定亮色"
		}
		return "Always light"
	default:
		if zh {
			return "跟随系统"
		}
		return "Follow Windows"
	}
}

// Accent is the system's own accent colour, as the tray read it.
//
// It is a value rather than something this package looks up, because reading it
// is a Windows registry call and this package decides what a palette looks like,
// not how the operating system is asked.
type Accent struct {
	// Colour is the accent Windows is using. It is ignored when Known is false.
	Colour raster.RGBA
	// Known is false when the setting could not be read, which happens on a build
	// that predates it. The palette then falls back to the accent Windows 11
	// ships with rather than to something invented here.
	Known bool
}

// DefaultAccent is the accent Windows 11 uses when nothing else has been chosen.
var DefaultAccent = raster.Hex("#4cc2ff")

// colour is the accent to draw with, which is never zero.
func (a Accent) colour() raster.RGBA {
	if !a.Known || a.Colour.A == 0 {
		return DefaultAccent
	}
	out := a.Colour
	out.A = 0xff
	return out
}

// Names of the palettes, as written to the configuration file.
const (
	// SystemName is the palette that wears the system accent.
	SystemName = "system"
	// MonoName is the neutral palette: no hue at all except where a state needs
	// one.
	MonoName = "mono"
)

// Palette is one complete set of inks.
//
// It carries no background, because the tray draws none: every colour here is
// meant to be read against the taskbar behind it.
type Palette struct {
	// Name is the stable identifier written to the configuration.
	Name string

	// Ink is the primary mark; InkDim is the quieter one, for the part of a mark
	// that is context rather than the reading.
	Ink    raster.RGBA
	InkDim raster.RGBA

	// Accent is the colour of a healthy reading and of anything the operator can
	// act on. In the system palette it is Windows' own accent, adjusted only as
	// far as legibility requires; in the monochrome one it is the ink.
	Accent raster.RGBA

	// State is what a mark turns when something is wrong. OK is separate from
	// Accent so the monochrome palette can answer "healthy" with ink and still
	// keep two colours in reserve for the states that need to be noticed.
	OK   raster.RGBA
	Warn raster.RGBA
	Bad  raster.RGBA

	// Light records which way round the palette was built, which is what a mark
	// consults when it needs to know how much contrast it is working with.
	Light bool

	// base is the dark rendering this one was turned from.
	//
	// It exists so On is reversible. The conversion has to move colours along
	// their own lightness to reach the contrast threshold, and that move cannot
	// be undone from the result alone: two different dark accents can land on the
	// same light one. Keeping the source makes the light rendering a view of the
	// dark palette rather than a second palette that has to be maintained in
	// step — which is what stops the two drifting apart, and what lets a caller
	// toggle the setting without losing the original accent.
	base *Palette
}

// All returns every palette, in menu order.
func All(accent Accent) []Palette {
	return []Palette{System(accent), Mono()}
}

// Names returns every palette name, in menu order.
func Names() []string {
	return []string{SystemName, MonoName}
}

// ByName returns a palette, falling back to the default one when the name is not
// known.
func ByName(name string, accent Accent) Palette {
	if Normalize(name) == MonoName {
		return Mono()
	}
	return System(accent)
}

// Normalize maps a configuration value onto a supported palette name, so a file
// written when there were other palettes still opens on one of these two.
func Normalize(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "mono", "monochrome", "plain", "grey", "gray", "white", "light":
		return MonoName
	default:
		// Everything else — including the retired neon, panel, amber and candy
		// palettes — lands on the one that follows the system.
		return SystemName
	}
}

// Label is the palette's name in the menu's language.
func (p Palette) Label(lang string) string {
	zh := lang == "zh"
	if p.Name == MonoName {
		if zh {
			return "单色"
		}
		return "Monochrome"
	}
	if zh {
		return "跟随系统"
	}
	return "System"
}

// State returns the colour a health reading is drawn in.
func (p Palette) State(s int) raster.RGBA {
	switch s {
	case 1:
		return p.Warn
	case 2:
		return p.Bad
	default:
		return p.OK
	}
}

// Resolve picks the palette to draw with and turns it the right way round.
func Resolve(name string, appearance Appearance, systemDark bool, accent Accent) Palette {
	chosen := ByName(name, accent)
	light := false
	switch appearance {
	case AlwaysLight:
		light = true
	case AlwaysDark:
		light = false
	default:
		light = !systemDark
	}
	return chosen.On(light)
}

// System is the palette that wears the system's own accent.
//
// The inks are the ones Windows 11 uses for its own glyphs, so a mark drawn in
// them reads as part of the shell rather than as an application's badge.
func System(accent Accent) Palette {
	ac := accent.colour()
	p := Palette{
		Name:   SystemName,
		Ink:    raster.Hex("#ffffff"),
		InkDim: raster.Hex("#b9c2cf"),
		Accent: ac,
		OK:     ac,
		Warn:   raster.Hex("#ffc44d"),
		Bad:    raster.Hex("#ff6b6b"),
	}
	// The accent is the operator's choice, so it may well be a colour that
	// disappears against the taskbar — a dark olive reads at a little over two to
	// one against the dark surface, which is below the three a shape needs. It is
	// adjusted rather than replaced: the hue is the whole point of it, so it is
	// moved along its own lightness until it can be seen.
	p.Accent = ensureContrast(ac, taskbarDark, minContrast)
	p.OK = p.Accent
	return p
}

// Mono is the neutral palette: white ink, and nothing else.
//
// A healthy reading is drawn in the ink rather than in a colour, which is what
// makes it monochrome. The two states that have to be noticed keep a hue;
// everything else is the absence of one.
//
// It is the one to pick when the operator's accent clashes with the health
// colours, or when the tray should be present without drawing the eye.
func Mono() Palette {
	return Palette{
		Name:   MonoName,
		Ink:    raster.Hex("#f5f7fa"),
		InkDim: raster.Hex("#9aa3b2"),
		Accent: raster.Hex("#f5f7fa"),
		OK:     raster.Hex("#f5f7fa"),
		Warn:   raster.Hex("#ffc44d"),
		Bad:    raster.Hex("#ff6b6b"),
	}
}

// On turns the palette the right way round for a taskbar of the given tone.
//
// It swaps the two things that carry contrast against the taskbar — the ink and
// the dim ink — and leaves the hues alone, so the mark is recognisably the same
// palette rather than a different one. The state colours are moved along their
// own lightness rather than inverted: a pale green on a white taskbar is
// invisible, and a "healthy" state that cannot be seen is the one failure this
// design cannot tolerate.
func (p Palette) On(light bool) Palette {
	if p.Light == light {
		return p
	}
	// Turning a light rendering back to dark returns the palette it came from,
	// rather than trying to reconstruct it.
	if !light && p.base != nil {
		return *p.base
	}
	out := p
	out.Light = light
	if !light {
		return out
	}
	// Remember the dark rendering so the turn can be undone exactly.
	dark := p
	out.base = &dark
	out.Ink = raster.Hex("#101418")
	out.InkDim = raster.Hex("#5b6572")
	if p.Name == MonoName {
		// The monochrome palette's accent and healthy colour are its ink, so they
		// follow it rather than going through the adjustment below.
		out.Accent = out.Ink
		out.OK = out.Ink
		out.Warn = ensureContrast(p.Warn, taskbarLight, minContrast)
		out.Bad = ensureContrast(p.Bad, taskbarLight, minContrast)
		return out
	}
	out.Accent = ensureContrast(p.Accent, taskbarLight, minContrast)
	out.OK = out.Accent
	out.Warn = ensureContrast(p.Warn, taskbarLight, minContrast)
	out.Bad = ensureContrast(p.Bad, taskbarLight, minContrast)
	return out
}

// IsLight reports whether the palette has been turned round for a light taskbar.
func (p Palette) IsLight() bool { return p.Light }

// The two taskbar tones, as the colours Windows 11 draws them.
//
// They are stated here rather than sampled from the screen, because a palette is
// a decision about contrast and a decision needs to be made against a known
// value. Sampling would make the icon's colours depend on what window happened
// to be open behind the taskbar.
var (
	taskbarDark  = raster.Hex("#1f1f1f")
	taskbarLight = raster.Hex("#f3f3f3")
)

// minContrast is the contrast ratio a mark has to reach against the taskbar.
//
// Three to one is the threshold the accessibility guidance sets for a graphical
// object, and it is the right bar here: below it a shape stops being a shape and
// becomes a smudge, which for a status icon means the reading is not merely
// unattractive but absent.
const minContrast = 3.0

// Taskbar returns the tone the palette expects to be read against, which is what
// a caller measures a colour against when it needs to check its own contrast.
func (p Palette) Taskbar() raster.RGBA {
	if p.Light {
		return taskbarLight
	}
	return taskbarDark
}

// ensureContrast moves a colour along its own lightness until it clears a
// contrast ratio against a background, keeping the hue where it can.
//
// It moves toward white on a dark background and toward black on a light one,
// which is the direction that increases contrast while disturbing the hue as
// little as possible.
func ensureContrast(c, bg raster.RGBA, want float64) raster.RGBA {
	if Contrast(c, bg) >= want {
		return c
	}
	toward := raster.Hex("#ffffff")
	if Luminance(bg) > 0.5 {
		toward = raster.Hex("#000000")
	}
	// A dozen steps is finer than the eye can follow and costs nothing: this runs
	// when a palette is built, not when an icon is drawn.
	best := c
	bestRatio := Contrast(c, bg)
	for i := 1; i <= 12; i++ {
		mixed := c.Mix(toward, float64(i)/12)
		mixed.A = c.A
		ratio := Contrast(mixed, bg)
		if ratio > bestRatio {
			best, bestRatio = mixed, ratio
		}
		if ratio >= want {
			return mixed
		}
	}
	return best
}

// Contrast is the WCAG contrast ratio between two colours, from 1 to 21.
//
// It is computed on the colours as drawn, ignoring alpha: a mark is meant to be
// opaque, and a translucent one has no fixed contrast to state.
func Contrast(a, b raster.RGBA) float64 {
	la, lb := relativeLuminance(a), relativeLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// Luminance is the perceptual luminance of a colour, from 0 to 1. It is exported
// because the menu picks its text colour by it.
func Luminance(c raster.RGBA) float64 { return relativeLuminance(c) }

// relativeLuminance is the WCAG definition: the channels are linearised before
// they are weighted, because the eye does not see brightness proportionally.
func relativeLuminance(c raster.RGBA) float64 {
	f := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*f(c.R) + 0.7152*f(c.G) + 0.0722*f(c.B)
}
