// Package theme holds the two palettes the tray can be dressed in.
//
// Both are transparent: the icon draws its glyph straight onto the taskbar with
// a soft halo behind it rather than sitting on a plate. That is what lets the
// tray sit next to the system's own icons without a block of colour around it,
// and it is the reason there are two palettes rather than six — with no plate,
// the only real question is whether the glyph should be light or dark.
package theme

import (
	"strings"

	"wbtray/internal/raster"
)

// Appearance is the follow-the-system setting.
//
// Both palettes are authored for a dark taskbar: light ink over a dark halo.
// That is what the icon looks like on the dark theme Windows ships with, and it
// is the version the colours were chosen for. On a light taskbar the same
// palette is turned inside out — dark ink over a light halo — which is what this
// setting decides, and why "follow Windows" is worth having.
type Appearance string

// The appearance modes.
const (
	// Auto follows the Windows app theme.
	Auto Appearance = "auto"
	// AlwaysDark keeps the dark-taskbar rendering regardless of the system.
	AlwaysDark Appearance = "dark"
	// AlwaysLight keeps the light-taskbar rendering regardless of the system.
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

// Resolve picks the palette to draw with.
func Resolve(name string, appearance Appearance, systemDark bool) Theme {
	chosen := ByName(name)
	light := false
	switch appearance {
	case AlwaysLight:
		light = true
	case AlwaysDark:
		light = false
	default:
		light = !systemDark
	}
	if light {
		return chosen.OnLight()
	}
	return chosen
}

// Names of the palettes, as written to the configuration file.
const (
	// Neon is the cyan-on-dark palette.
	NeonName = "neon"
	// Mono is the neutral monochrome palette.
	MonoName = "mono"
)

// Theme is one complete look.
//
// Halo is the only background a glyph has: a translucent disc drawn behind the
// mark so it stays legible on a taskbar of any colour, which a fully transparent
// icon otherwise cannot guarantee.
type Theme struct {
	// Name is the stable identifier written to the configuration.
	Name string

	// Ink is text and neutral marks; InkDim is the quieter of the two; Accent is
	// the colour of a healthy shape and of anything the operator can click.
	Ink    raster.RGBA
	InkDim raster.RGBA
	Accent raster.RGBA
	// Track is the unfilled part of a gauge, drawn as a faint ink because there
	// is no plate for a plate-coloured track to read against.
	Track raster.RGBA
	// Glow is the soft underlay beneath a line, which is what gives a sparkline
	// weight at sixteen pixels.
	Glow raster.RGBA
	// Halo is the disc behind the mark.
	Halo raster.RGBA

	// Health colours are what the mark uses when a state is worth flagging.
	OK   raster.RGBA
	Warn raster.RGBA
	Bad  raster.RGBA

	// Menu colours dress the drawn menu. The menu is opaque whatever the icon
	// is, because a menu drawn over a desktop needs contrast with whatever is
	// behind it, and there is no way to know what that is.
	MenuBg     raster.RGBA
	MenuSel    raster.RGBA
	MenuInk    raster.RGBA
	MenuInkDim raster.RGBA
	// MenuLine draws the separators and the border. MenuEdge is the border
	// alone, which has to be lighter than a separator: it runs against the
	// desktop rather than against the menu's own background.
	MenuLine raster.RGBA
	MenuEdge raster.RGBA
}

// All returns every theme, in menu order.
func All() []Theme {
	return []Theme{Neon(), Mono()}
}

// Names returns every theme name, in menu order.
func Names() []string {
	return []string{NeonName, MonoName}
}

// ByName returns a theme, falling back to the default one.
func ByName(name string) Theme {
	if Normalize(name) == MonoName {
		return Mono()
	}
	return Neon()
}

// Normalize maps a configuration value onto a supported theme name, so a file
// written when there were six palettes still opens on one of the two.
func Normalize(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "mono", "monochrome", "plain", "grey", "gray", "light", "white":
		return MonoName
	default:
		// Everything else, including the retired panel, amber, candy and light
		// palettes, lands on the default rather than on a missing theme.
		return NeonName
	}
}

// Label is the theme's name in the menu's language.
func (t Theme) Label(lang string) string {
	if lang == "zh" {
		if t.Name == MonoName {
			return "单色"
		}
		return "霓虹"
	}
	if t.Name == MonoName {
		return "Monochrome"
	}
	return "Neon"
}

// Health returns the colour a state is drawn in.
func (t Theme) Health(state int) raster.RGBA {
	switch state {
	case 0:
		return t.OK
	case 1:
		return t.Warn
	default:
		return t.Bad
	}
}

// OnLight returns the same palette turned inside out for a light taskbar.
//
// It inverts the two things that carry contrast against the taskbar — the ink
// and the halo — and leaves the hues alone, so the mark is recognisably the same
// palette rather than a different one. The health colours are darkened rather
// than inverted: a pale cyan on a white taskbar is invisible, and a "healthy"
// state that cannot be seen is the one failure this design cannot tolerate.
//
// The menu is converted too. A menu is a surface like any other on the desktop,
// and one that stayed dark while every other menu on the system went light would
// be the most conspicuous thing on screen rather than the least.
func (t Theme) OnLight() Theme {
	out := t
	out.Halo = raster.Hex("#ffffffd0")
	out.Ink = raster.Hex("#0d1117")
	out.InkDim = raster.Hex("#4a5566")
	out.Track = out.Ink.Mul(0.22)
	out.Glow = raster.Hex("#0d11172e")
	out.OK = darken(t.OK, 0.45)
	out.Warn = darken(t.Warn, 0.42)
	out.Bad = darken(t.Bad, 0.45)
	if t.Name == MonoName {
		// The monochrome palette's healthy colour is its own ink, so it comes out
		// dark with the rest of it rather than through the darkening rule.
		out.Accent = out.Ink
		out.OK = out.Ink
		out.Track = out.Ink.Mul(0.22)
	} else {
		out.Accent = darken(t.Accent, 0.42)
	}
	// The menu keeps its own contrast ratio rather than inverting outright: a
	// pure white menu on a white desktop has no edge, so the surface is a light
	// grey and the border does the separating.
	out.MenuBg = raster.Hex("#fbfcfe")
	out.MenuSel = raster.Hex("#e3eaf8")
	out.MenuInk = raster.Hex("#11151c")
	out.MenuInkDim = raster.Hex("#5c6676")
	out.MenuLine = raster.Hex("#dfe4ec")
	out.MenuEdge = raster.Hex("#c3cad6")
	return out
}

// darken moves a colour toward black by t, which is what keeps a hue
// recognisable while giving it enough contrast to sit on white.
func darken(c raster.RGBA, t float64) raster.RGBA {
	mix := func(v uint8) uint8 { return uint8(float64(v) * (1 - t)) }
	return raster.RGBA{R: mix(c.R), G: mix(c.G), B: mix(c.B), A: c.A}
}

// IsLight reports whether a palette has been turned inside out for a light
// taskbar. The menu is unaffected either way, so this is about the icon.
func (t Theme) IsLight() bool {
	return lum(t.Halo) > 128
}

// lum is the usual perceptual luminance of a colour.
func lum(c raster.RGBA) float64 {
	return 0.299*float64(c.R) + 0.587*float64(c.G) + 0.114*float64(c.B)
}

// Neon is cyan on a faint dark halo: the default, and the one that reads best on
// the dark taskbar Windows ships with.
func Neon() Theme {
	return Theme{
		Name:       NeonName,
		Halo:       raster.Hex("#04121be0"),
		Ink:        raster.Hex("#eaf6ff"),
		InkDim:     raster.Hex("#7fb6d6"),
		Accent:     raster.Hex("#22d3ee"),
		Track:      raster.Hex("#22d3ee40"),
		Glow:       raster.Hex("#22d3ee55"),
		OK:         raster.Hex("#2ff5c0"),
		Warn:       raster.Hex("#ffc857"),
		Bad:        raster.Hex("#ff6b81"),
		MenuBg:     raster.Hex("#0a1420"),
		MenuSel:    raster.Hex("#123449"),
		MenuInk:    raster.Hex("#eaf6ff"),
		MenuInkDim: raster.Hex("#7fb6d6"),
		MenuLine:   raster.Hex("#1d3d52"),
		MenuEdge:   raster.Hex("#24506b"),
	}
}

// Mono is the neutral palette: white ink on a dark halo, with colour reserved
// for the two states that need it. It is the one to pick when the tray should
// look like part of the system rather than like part of an application.
func Mono() Theme {
	return Theme{
		Name:       MonoName,
		Halo:       raster.Hex("#000000a8"),
		Ink:        raster.Hex("#f5f7fa"),
		InkDim:     raster.Hex("#b9c0cc"),
		Accent:     raster.Hex("#f5f7fa"),
		Track:      raster.Hex("#f5f7fa44"),
		Glow:       raster.Hex("#ffffff33"),
		OK:         raster.Hex("#f5f7fa"),
		Warn:       raster.Hex("#ffd166"),
		Bad:        raster.Hex("#ff8b7a"),
		MenuBg:     raster.Hex("#1b1f27"),
		MenuSel:    raster.Hex("#323844"),
		MenuInk:    raster.Hex("#f5f7fa"),
		MenuInkDim: raster.Hex("#a6adbb"),
		MenuLine:   raster.Hex("#333a46"),
		MenuEdge:   raster.Hex("#454e5d"),
	}
}
