// Package theme holds the palettes the tray can be dressed in.
//
// The tray follows Windows rather than carrying a look of its own. Windows 11 tints its
// surfaces with the accent the operator chose in Settings, and an icon that sits on the
// taskbar beside the system's own should use that accent rather than a colour picked
// here: two different blues on one taskbar read as one of them being wrong.
//
// Both palettes are transparent. The icon draws its glyph straight onto the taskbar over
// a soft halo rather than sitting on a plate, which is what lets it sit next to the
// system's icons without a block of colour around it.
package theme

import (
	"strings"

	"wbtray/internal/raster"
)

// Appearance is the follow-the-system setting.
//
// Both palettes are authored for a dark taskbar: light ink over a dark halo. That is
// what the icon looks like on the dark theme Windows ships with, and it is the version
// the colours were chosen for. On a light taskbar the same palette is turned inside out
// — dark ink over a light halo — which is what this setting decides, and why "follow
// Windows" is worth having.
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

// Accent is the system's own accent colour, as the tray read it.
//
// It is a value rather than something this package looks up, because reading it is a
// Windows registry call and this package is deliberately free of the platform: it decides
// what a palette looks like, not how the operating system is asked.
type Accent struct {
	// Colour is the accent Windows is using. It is ignored when Known is false.
	Colour raster.RGBA
	// Known is false when the setting could not be read, which happens on a build that
	// predates it. The palette then falls back to the accent Windows 11 ships with
	// rather than to something invented here.
	Known bool
}

// DefaultAccent is the accent Windows 11 uses when nothing else has been chosen.
var DefaultAccent = raster.Hex("#0078d4")

// colour is the accent to draw with, which is never zero.
func (a Accent) colour() raster.RGBA {
	if !a.Known || a.Colour.A == 0 {
		return DefaultAccent
	}
	return a.Colour
}

// Resolve picks the palette to draw with.
//
// The accent reaches the icon as the fill of its shape, so the tray's mark is the same
// colour as the system's selection highlight. A palette whose accent is always its own
// ink — the monochrome one — ignores it by definition.
func Resolve(name string, appearance Appearance, systemDark bool, accent Accent) Theme {
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
	if light {
		return chosen.OnLight()
	}
	return chosen
}

// Names of the palettes, as written to the configuration file.
const (
	// SystemName is the palette that wears the system accent.
	SystemName = "system"
	// MonoName is the neutral palette: no hue at all except where a state needs one.
	MonoName = "mono"
)

// Theme is one complete look.
//
// Halo is the only background a glyph has: a translucent disc drawn behind the mark so it
// stays legible on a taskbar of any colour, which a fully transparent icon otherwise
// cannot guarantee.
type Theme struct {
	// Name is the stable identifier written to the configuration.
	Name string

	// Ink is text and neutral marks; InkDim is the quieter of the two; Accent is the
	// colour of a healthy shape and of anything the operator can click.
	Ink    raster.RGBA
	InkDim raster.RGBA
	Accent raster.RGBA
	// Track is the unfilled part of a gauge, drawn as a faint ink because there is no
	// plate for a plate-coloured track to read against.
	Track raster.RGBA
	// Glow is the soft underlay beneath a line, which is what gives a sparkline weight
	// at sixteen pixels.
	Glow raster.RGBA
	// Halo is the disc behind the mark.
	Halo raster.RGBA

	// Health colours are what the mark uses when a state is worth flagging.
	OK   raster.RGBA
	Warn raster.RGBA
	Bad  raster.RGBA
}

// All returns every theme, in menu order.
func All(accent Accent) []Theme {
	return []Theme{System(accent), Mono()}
}

// Names returns every theme name, in menu order.
func Names() []string {
	return []string{SystemName, MonoName}
}

// ByName returns a theme, falling back to the default one when the name is not known.
func ByName(name string, accent Accent) Theme {
	if Normalize(name) == MonoName {
		return Mono()
	}
	return System(accent)
}

// Normalize maps a configuration value onto a supported theme name, so a file written
// when there were other palettes still opens on one of these two.
func Normalize(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "mono", "monochrome", "plain", "grey", "gray", "white", "light":
		return MonoName
	default:
		// Everything else — including neon, which this replaced, and the retired panel,
		// amber and candy palettes — lands on the palette that follows the system.
		return SystemName
	}
}

// Label is the palette's name in the menu's language.
func (t Theme) Label(lang string) string {
	zh := lang == "zh"
	if t.Name == MonoName {
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
// It inverts the two things that carry contrast against the taskbar — the ink and the
// halo — and leaves the hues alone, so the mark is recognisably the same palette rather
// than a different one. The health colours are darkened rather than inverted: a pale
// green on a white taskbar is invisible, and a "healthy" state that cannot be seen is the
// one failure this design cannot tolerate.
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
	// The accent is darkened rather than replaced: it has to stay the system's hue,
	// because that is the whole point of it, while gaining the contrast a white taskbar
	// needs. Windows does the same thing to its own accent in light mode.
	out.Accent = darken(t.Accent, 0.28)
	if t.Name == MonoName {
		// The monochrome palette's healthy colour is its own ink, so it comes out dark
		// with the rest of it rather than through the darkening rule.
		out.Accent = out.Ink
		out.OK = out.Ink
		out.Track = out.Ink.Mul(0.22)
	}
	return out
}

// darken moves a colour toward black by t, which is what keeps a hue recognisable while
// giving it enough contrast to sit on white.
func darken(c raster.RGBA, t float64) raster.RGBA {
	mix := func(v uint8) uint8 { return uint8(float64(v) * (1 - t)) }
	return raster.RGBA{R: mix(c.R), G: mix(c.G), B: mix(c.B), A: c.A}
}

// IsLight reports whether a palette has been turned inside out for a light taskbar.
func (t Theme) IsLight() bool {
	return lum(t.Halo) > 128
}

// lum is the usual perceptual luminance of a colour.
func lum(c raster.RGBA) float64 {
	return 0.299*float64(c.R) + 0.587*float64(c.G) + 0.114*float64(c.B)
}

// System is the palette that wears the system's own accent.
//
// The halo is the near-black Windows 11 uses behind its taskbar flyouts and the ink is
// its primary text colour, so the mark reads as part of the shell rather than as an
// application's badge. The accent is the one the operator chose, which is what makes the
// icon look like it belongs next to the system's own.
func System(accent Accent) Theme {
	ac := accent.colour()
	return Theme{
		Name:   SystemName,
		Halo:   raster.Hex("#1c1c1ce6"),
		Ink:    raster.Hex("#ffffff"),
		InkDim: raster.Hex("#c8c8c8"),
		Accent: ac,
		Track:  ac.Mul(0.28),
		Glow:   ac.Mul(0.35),
		OK:     ac,
		Warn:   raster.Hex("#ffd335"),
		Bad:    raster.Hex("#ff99a4"),
	}
}

// Mono is the neutral palette: white ink on a dark halo, and nothing else. A healthy pool
// is drawn in the ink rather than in a colour, which is what makes it monochrome — the two
// states that need to be noticed keep a hue, and everything else is the absence of one.
//
// It is the one to pick when the operator's accent clashes with the health colours, or when
// the tray should be present without drawing the eye.
func Mono() Theme {
	return Theme{
		Name:   MonoName,
		Halo:   raster.Hex("#000000a8"),
		Ink:    raster.Hex("#f5f7fa"),
		InkDim: raster.Hex("#b9c0cc"),
		Accent: raster.Hex("#f5f7fa"),
		Track:  raster.Hex("#f5f7fa44"),
		Glow:   raster.Hex("#ffffff33"),
		OK:     raster.Hex("#f5f7fa"),
		Warn:   raster.Hex("#ffd166"),
		Bad:    raster.Hex("#ff8b7a"),
	}
}
