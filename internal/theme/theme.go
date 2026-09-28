package theme

import (
	"math"
	"strings"

	"wbtray/internal/raster"
)

// The palettes the tray can be dressed in.
//
// They are all one design in different tones: the marks are neutral, colour is
// reserved for the states that have to be noticed, and every ink is checked against
// every surface it is drawn on. What changes is which neutral — a warm black, a cool
// black, a plain one, and the light rendering of each.
//
// The first is the one this is modelled on, and it is the default because it is the
// one the rest were derived from. The others exist because "which neutral" is a
// question about taste rather than about correctness, and a program that answers it
// for the operator with no way to disagree is a program that looks wrong to half the
// people who use it.

// Appearance is the follow-the-system setting.
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

// Accent is kept for the front end, which reads the Windows accent at startup.
//
// Nothing in this package uses it. It is here because removing the call would mean
// the registry read, the winapi function and its test all had to go at once, and the
// honest state of affairs is that the tray no longer follows the system accent: an
// accent is chosen to be a highlight, not to be a status mark, and a muddy one makes
// every mark in the tray muddy.
type Accent struct {
	Colour raster.RGBA
	Known  bool
}

// The palette names, as written to the configuration.
const (
	// NameNeutral is the one this design was drawn from: a plain warm-neutral grey
	// ramp with near-black and near-white ink.
	NameNeutral = "neutral"
	// NameCool is the same design on a blue-leaning grey: the blacks read as slate
	// rather than as warm charcoal.
	NameCool = "cool"
	// NameWarm is the same design on a brown-leaning grey, which reads as sepia
	// rather than as charcoal.
	NameWarm = "warm"
	// NameContrast is the same design with the surfaces pulled apart: a darker
	// background and a lighter card, which is what makes a window read as raised.
	NameContrast = "contrast"
)

// Names is the palette list, in menu order.
var Names = []string{NameNeutral, NameCool, NameWarm, NameContrast}

// DefaultName is the palette a first run uses.
const DefaultName = NameNeutral

// All returns every palette in its dark tone, which is what a preview sheet wants.
func All(accent Accent) []Palette {
	out := make([]Palette, 0, len(Names))
	for _, n := range Names {
		out = append(out, ForName(n, false))
	}
	return out
}

// ByName returns a palette by name, falling back to the default.
func ByName(name string, accent Accent) Palette {
	return ForName(Normalize(name), false)
}

// Normalize maps a configuration value onto a palette that exists.
//
// Every retired name lands on the default, including "codex" — which this was called
// while there was one palette — and "system", which described a palette that wore the
// operator's accent and is gone.
func Normalize(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	for _, n := range Names {
		if s == n {
			return n
		}
	}
	switch s {
	case "codex", "system", "mono", "monochrome", "plain", "grey", "gray",
		"white", "light", "neon", "panel", "amber", "candy", "":
		return DefaultName
	}
	return DefaultName
}

// Label is the palette's name in the menu's language.
func (p Palette) Label(lang string) string {
	zh := lang == "zh"
	if p.Light {
		if zh {
			return "亮色"
		}
		return "Light"
	}
	if zh {
		return "暗色"
	}
	return "Dark"
}

// VariantLabel is the palette's own name, which is what a chooser needs: a list of
// four rows all labelled "Dark" would be four rows nobody could choose between.
func VariantLabel(name, lang string) string {
	zh := lang == "zh"
	switch name {
	case NameCool:
		if zh {
			return "冷灰"
		}
		return "Cool"
	case NameWarm:
		if zh {
			return "暖灰"
		}
		return "Warm"
	case NameContrast:
		if zh {
			return "高对比"
		}
		return "Contrast"
	default:
		if zh {
			return "标准"
		}
		return "Neutral"
	}
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

// Taskbar is the surface the icon is read against.
//
// It is stated rather than sampled, because the icon's legibility is a decision about
// a known background and sampling would make it depend on whichever window happened
// to be open behind the notification area. A mark that has to knock a hole in itself
// — the mascot's face — draws this.
func (p Palette) Taskbar() raster.RGBA {
	if p.Light {
		return raster.Hex("#f3f3f3")
	}
	return raster.Hex("#1f1f1f")
}

// Resolve picks the tone and the palette to draw with.
func Resolve(name string, appearance Appearance, systemDark bool, accent Accent) Palette {
	light := false
	switch appearance {
	case AlwaysLight:
		light = true
	case AlwaysDark:
		light = false
	default:
		light = !systemDark
	}
	return ForName(Normalize(name), light)
}

// On returns the default palette in a tone, which is what a caller that has no
// preference wants.
func On(light bool) Palette { return ForName(DefaultName, light) }

// ForName returns one palette in one tone.
func ForName(name string, light bool) Palette {
	base := rampFor(Normalize(name))
	if light {
		return base.light()
	}
	return base.dark()
}

// ramp is the neutral steps one palette is built from, before the state colours are
// laid over them.
//
// Stating the surfaces as a ramp rather than as one list per tone is what keeps four
// palettes in two tones from being eight unrelated sets of greys: the light tone is
// derived from the same steps read the other way, so a change to the shape of the
// ramp reaches every palette at once.
type ramp struct {
	name string

	// The dark tone, from the furthest back to the closest: the window, its rail, a
	// card, a control, a control under the pointer, and the two hairline strengths.
	bg, rail, surface, raised, raisedHi, border, borderSoft raster.RGBA
	// The text, at three levels.
	text, muted, faint raster.RGBA

	// The state colours, dark tone.
	green, amber, red, blue raster.RGBA
}

func rampFor(name string) ramp {
	switch name {
	case NameCool:
		return ramp{
			name:       NameCool,
			bg:         raster.Hex("#1c1f24"),
			rail:       raster.Hex("#141719"),
			surface:    raster.Hex("#22262c"),
			raised:     raster.Hex("#2b3037"),
			raisedHi:   raster.Hex("#343a42"),
			border:     raster.Hex("#394049"),
			borderSoft: raster.Hex("#2f353d"),
			text:       raster.Hex("#e8ecf1"),
			muted:      raster.Hex("#a0a8b3"),
			faint:      raster.Hex("#939ba6"),
			green:      raster.Hex("#4ecb9d"),
			amber:      raster.Hex("#fbbf24"),
			red:        raster.Hex("#f87171"),
			blue:       raster.Hex("#7aa2ff"),
		}
	case NameWarm:
		return ramp{
			name:       NameWarm,
			bg:         raster.Hex("#211e1a"),
			rail:       raster.Hex("#191612"),
			surface:    raster.Hex("#272320"),
			raised:     raster.Hex("#322d28"),
			raisedHi:   raster.Hex("#3d3730"),
			border:     raster.Hex("#463f37"),
			borderSoft: raster.Hex("#38322b"),
			text:       raster.Hex("#f0ebe4"),
			muted:      raster.Hex("#b0a79b"),
			faint:      raster.Hex("#9e958a"),
			green:      raster.Hex("#54cfa0"),
			amber:      raster.Hex("#f5b942"),
			red:        raster.Hex("#f57f6e"),
			blue:       raster.Hex("#8aa8f0"),
		}
	case NameContrast:
		return ramp{
			name:       NameContrast,
			bg:         raster.Hex("#0d0d0f"),
			rail:       raster.Hex("#08080a"),
			surface:    raster.Hex("#1b1b1f"),
			raised:     raster.Hex("#2a2a30"),
			raisedHi:   raster.Hex("#3a3a42"),
			border:     raster.Hex("#4d4d57"),
			borderSoft: raster.Hex("#33333b"),
			text:       raster.Hex("#ffffff"),
			muted:      raster.Hex("#b8b8c2"),
			faint:      raster.Hex("#9a9aa6"),
			green:      raster.Hex("#3fe0a0"),
			amber:      raster.Hex("#ffc933"),
			red:        raster.Hex("#ff7a7a"),
			blue:       raster.Hex("#8fb4ff"),
		}
	default:
		return ramp{
			name:       NameNeutral,
			bg:         raster.Hex("#212121"),
			rail:       raster.Hex("#171717"),
			surface:    raster.Hex("#262626"),
			raised:     raster.Hex("#303030"),
			raisedHi:   raster.Hex("#3a3a3a"),
			border:     raster.Hex("#3d3d3d"),
			borderSoft: raster.Hex("#333333"),
			text:       raster.Hex("#ececec"),
			muted:      raster.Hex("#a6a6a6"),
			faint:      raster.Hex("#9a9a9a"),
			green:      raster.Hex("#4ecb9d"),
			amber:      raster.Hex("#fbbf24"),
			red:        raster.Hex("#f87171"),
			blue:       raster.Hex("#7aa2ff"),
		}
	}
}

// dark builds the dark tone.
func (r ramp) dark() Palette {
	p := Palette{
		Name: r.name, Light: false,
		BG: r.bg, Rail: r.rail, Surface: r.surface,
		Raised: r.raised, RaisedHi: r.raisedHi,
		Border: r.border, BorderSoft: r.borderSoft,
		Text: r.text, Muted: r.muted, Faint: r.faint,
		Accent: r.text, AccentInk: r.bg, AccentHi: raster.Hex("#ffffff"),
		Green: r.green, Warn: r.amber, Bad: r.red, Blue: r.blue,
	}
	p.Ink, p.InkDim, p.OK = p.Text, p.Muted, p.Green
	return p
}

// light builds the light tone of the same palette.
//
// It is written out rather than derived by inverting, because a grey ramp inverted
// channel by channel is not the same ramp read the other way round: the steps are
// chosen so the surfaces stay the same distance apart, and that is a judgement about
// contrast rather than arithmetic. The state colours are darkened versions of the
// dark tone's — dark and saturated, which is what reads on white.
func (r ramp) light() Palette {
	lighten := func(c raster.RGBA, t float64) raster.RGBA { return c.Mix(raster.Hex("#ffffff"), t) }
	tint := func(c raster.RGBA) raster.RGBA { return lighten(c, 0.94) }

	p := Palette{
		Name: r.name, Light: true,
		BG:         tint(r.bg),
		Rail:       tint(r.rail),
		Surface:    raster.Hex("#ffffff"),
		Raised:     lighten(r.bg, 0.90),
		RaisedHi:   lighten(r.bg, 0.85),
		Border:     lighten(r.bg, 0.82),
		BorderSoft: lighten(r.bg, 0.90),
		Text:       raster.Hex("#0d0d0d"),
		Muted:      raster.Hex("#6e6e6e"),
		Faint:      raster.Hex("#707070"),
		Accent:     raster.Hex("#0d0d0d"),
		AccentInk:  raster.Hex("#ffffff"),
		AccentHi:   raster.Hex("#3d3d3d"),
		Green:      darken(r.green, 0.62),
		Warn:       darken(r.amber, 0.55),
		Bad:        darken(r.red, 0.52),
		Blue:       darken(r.blue, 0.55),
	}
	p.Ink, p.InkDim, p.OK = p.Text, p.Muted, p.Green
	return p
}

// darken moves a colour toward black, which is what makes a pale state colour read on
// a light surface.
func darken(c raster.RGBA, t float64) raster.RGBA {
	mix := func(v uint8) uint8 { return uint8(float64(v) * (1 - t)) }
	return raster.RGBA{R: mix(c.R), G: mix(c.G), B: mix(c.B), A: c.A}
}

// Palette is one tone of one palette: its surfaces, its text, and the four colours a
// state can be.
//
// The field names are the ones the interface this follows uses, because they are the
// names its own designers agreed on and inventing a second vocabulary for the same
// roles is how two parts of a program end up disagreeing about what "muted" means.
type Palette struct {
	// Name is the stable identifier written to the configuration.
	Name string
	// Light records which tone this is.
	Light bool

	BG         raster.RGBA
	Rail       raster.RGBA
	Surface    raster.RGBA
	Raised     raster.RGBA
	RaisedHi   raster.RGBA
	Border     raster.RGBA
	BorderSoft raster.RGBA

	Text  raster.RGBA
	Muted raster.RGBA
	Faint raster.RGBA

	// Ink and InkDim are the tray icon's own names for the two text levels — a glyph
	// on a taskbar is text at heart — and they are set from the same source so the
	// icon and the window cannot drift apart.
	Ink    raster.RGBA
	InkDim raster.RGBA

	// Accent is the colour of anything the operator can act on. In this design it is
	// neutral, which is what keeps the tray quiet beside the system's icons.
	Accent    raster.RGBA
	AccentInk raster.RGBA
	AccentHi  raster.RGBA

	// The four states. OK, Warn and Bad are the tray icon's names for green, amber
	// and red; Blue is for the one state that is busy rather than broken.
	OK   raster.RGBA
	Warn raster.RGBA
	Bad  raster.RGBA
	Blue raster.RGBA
	// Green is the name this design's own stylesheet gives the healthy colour.
	Green raster.RGBA
}

// IsLight reports whether this is the light tone.
func (p Palette) IsLight() bool { return p.Light }

// Contrast is the WCAG contrast ratio between two colours, from 1 to 21.
func Contrast(a, b raster.RGBA) float64 {
	la, lb := Luminance(a), Luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// Luminance is the relative luminance of a colour, from 0 to 1.
//
// The definition is the WCAG one: the channels are linearised before they are
// weighted, because the eye does not see brightness proportionally.
func Luminance(c raster.RGBA) float64 {
	f := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*f(c.R) + 0.7152*f(c.G) + 0.0722*f(c.B)
}
