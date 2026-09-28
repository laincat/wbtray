// Package theme holds the palette the tray draws with.
//
// It is one design in two tones, the way Codex's own interface is: the marks are
// neutral — near-black ink on white, near-white ink on near-black — and colour is
// reserved for the two or three states that have to be noticed. That is the whole
// idea, and it is worth stating because the obvious alternative is the one this
// replaced: a palette that wears the operator's Windows accent looks like it
// belongs beside the system's own icons, until the accent is a muddy brown or a
// dark olive, at which point every mark in the tray is a muddy brown or a dark
// olive and no amount of contrast correction can make it pleasant.
//
// So there is no accent-following palette and no colour arithmetic. Both tones are
// written down, taken from the interface this is meant to sit beside, and each is
// legible by construction rather than by adjustment.
//
// Nothing here draws a background for the icon. The tray's marks sit directly on
// the taskbar; the surfaces below are for the window, which is a surface of its
// own and needs one.
package theme

import (
	"math"
	"strings"

	"wbtray/internal/raster"
)

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
// Nothing in this package uses it any more. It is here because removing the call
// would mean the registry read, the winapi function and its test all had to go at
// once, and the honest state of affairs is that the tray no longer follows the
// system accent. The type stays until that read is removed on its own.
type Accent struct {
	Colour raster.RGBA
	Known  bool
}

// Name is the palette identifier written to the configuration.
//
// There is one palette now. The names of the retired ones are still accepted, and
// map here, so a configuration written by an earlier build opens.
const Name = "codex"

// Palette is one tone of the design: its surfaces, its text, and the four colours
// a state can be.
//
// The field names are the ones the interface this follows uses, because they are
// the names its own designers agreed on and inventing a second vocabulary for the
// same roles is how two parts of a program end up disagreeing about what "muted"
// means.
type Palette struct {
	// Name is the stable identifier written to the configuration.
	Name string
	// Light records which tone this is, which is what a caller asks when it has to
	// choose a colour it cannot get from the palette.
	Light bool

	// Surfaces, from the furthest back to the closest: the window, its sidebar,
	// a card, a control inside a card, and the two hairline strengths.
	BG         raster.RGBA
	Rail       raster.RGBA
	Surface    raster.RGBA
	Raised     raster.RGBA
	RaisedHi   raster.RGBA
	Border     raster.RGBA
	BorderSoft raster.RGBA

	// Text, at three levels: the reading, the label, and the hint.
	Text  raster.RGBA
	Muted raster.RGBA
	Faint raster.RGBA

	// The marks. Ink and InkDim are the tray icon's own names for the two text
	// levels — a glyph on a taskbar is text at heart — and they are set from the
	// same source so the icon and the window cannot drift apart.
	Ink    raster.RGBA
	InkDim raster.RGBA

	// Accent is the colour of anything the operator can act on. In this design it
	// is neutral, which is what keeps the tray quiet beside the system's icons.
	Accent    raster.RGBA
	AccentInk raster.RGBA
	AccentHi  raster.RGBA

	// The four states. OK, Warn and Bad are the tray icon's names for green, amber
	// and red; Blue is for the one state that is busy rather than broken.
	OK   raster.RGBA
	Warn raster.RGBA
	Bad  raster.RGBA
	Blue raster.RGBA
	// Green is the name this design's own stylesheet gives the healthy colour. It
	// is kept beside OK rather than instead of it because the icon asks for OK and
	// the window asks for Green, and they are the same value.
	Green raster.RGBA
}

// All returns every palette, in menu order.
//
// One entry, because the design is one thing. The list remains because the menu and
// the preview sheet both iterate it, and because a second palette is the obvious
// next thing to want.
func All(accent Accent) []Palette { return []Palette{Dark(), Light()} }

// Names returns every palette name.
func Names() []string { return []string{Name} }

// ByName returns the palette for a configured name, ignoring which tone was asked
// for: the tone is the appearance's business, not the name's.
func ByName(name string, accent Accent) Palette { return Dark() }

// Normalize maps a configuration value onto the palette that exists.
//
// Every retired name lands here, including "system": the palette that wore the
// operator's accent is gone, and a configuration asking for it gets this design
// rather than a broken one.
func Normalize(name string) string { return Name }

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
// It is stated rather than sampled, because the icon's legibility is a decision
// about a known background and sampling would make it depend on whichever window
// happened to be open behind the notification area. A mark that has to knock a hole
// in itself — the mascot's face — draws this.
func (p Palette) Taskbar() raster.RGBA {
	if p.Light {
		return raster.Hex("#f3f3f3")
	}
	return raster.Hex("#1f1f1f")
}

// Resolve picks the tone to draw with.
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
	return On(light)
}

// On returns the palette for a tone.
func On(light bool) Palette {
	if light {
		return Light()
	}
	return Dark()
}

// Dark is the dark tone.
//
// The greys are a warm-neutral ramp from a near-black rail to a raised control,
// and the ink is near-white rather than white: pure white on a near-black surface
// glares at the small sizes a tray uses.
//
// The four state colours are light and desaturated, because they are read as small
// marks on a dark background where a saturated colour at full strength reads as a
// warning when it is only a reading.
func Dark() Palette {
	p := Palette{
		Name:       Name,
		Light:      false,
		BG:         raster.Hex("#212121"),
		Rail:       raster.Hex("#171717"),
		Surface:    raster.Hex("#262626"),
		Raised:     raster.Hex("#303030"),
		RaisedHi:   raster.Hex("#3a3a3a"),
		Border:     raster.Hex("#3d3d3d"),
		BorderSoft: raster.Hex("#333333"),
		Text:       raster.Hex("#ececec"),
		Muted:      raster.Hex("#a6a6a6"),
		Faint:      raster.Hex("#9a9a9a"),
		Accent:     raster.Hex("#ececec"),
		AccentInk:  raster.Hex("#0d0d0d"),
		AccentHi:   raster.Hex("#ffffff"),
		Green:      raster.Hex("#4ecb9d"),
		Warn:       raster.Hex("#fbbf24"),
		Bad:        raster.Hex("#f87171"),
		Blue:       raster.Hex("#7aa2ff"),
	}
	p.Ink, p.InkDim = p.Text, p.Muted
	p.OK = p.Green
	return p
}

// Light is the light tone: the same design the other way round, written out rather
// than derived. The greys are cool-neutral and the state colours are dark and
// saturated, which is what makes them read on white.
func Light() Palette {
	p := Palette{
		Name:       Name,
		Light:      true,
		BG:         raster.Hex("#ffffff"),
		Rail:       raster.Hex("#f9f9f9"),
		Surface:    raster.Hex("#ffffff"),
		Raised:     raster.Hex("#f4f4f4"),
		RaisedHi:   raster.Hex("#ececec"),
		Border:     raster.Hex("#e6e6e6"),
		BorderSoft: raster.Hex("#f0f0f0"),
		Text:       raster.Hex("#0d0d0d"),
		Muted:      raster.Hex("#6e6e6e"),
		Faint:      raster.Hex("#707070"),
		Accent:     raster.Hex("#0d0d0d"),
		AccentInk:  raster.Hex("#ffffff"),
		AccentHi:   raster.Hex("#3d3d3d"),
		Green:      raster.Hex("#0a7d5c"),
		Warn:       raster.Hex("#9a4a08"),
		Bad:        raster.Hex("#b91c1c"),
		Blue:       raster.Hex("#1d4ed8"),
	}
	p.Ink, p.InkDim = p.Text, p.Muted
	p.OK = p.Green
	return p
}

// Contrast is the WCAG contrast ratio between two colours, from 1 to 21.
//
// It is still exported and still tested, because both tones make a claim the tests
// check: every ink in them is legible against the surface it is drawn on. The claim
// is now about two written-down palettes rather than about an adjustment applied at
// runtime, but it is the same claim.
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
