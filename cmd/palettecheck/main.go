// Command palettecheck prints the palette wbtray resolves on this machine, so the
// colour the icon and the window draw with can be compared against the accent
// Windows is actually using.
package main

import (
	"fmt"

	"wbtray/internal/raster"
	"wbtray/internal/theme"
	"wbtray/internal/winapi"
)

func main() {
	packed, ok := winapi.AccentColor()
	fmt.Printf("registry AccentColor: known=%v packed=%#08x\n", ok, packed)

	accent := theme.Accent{}
	if ok {
		accent = theme.Accent{Known: true, Colour: raster.RGBA{
			R: uint8(packed & 0xff),
			G: uint8((packed >> 8) & 0xff),
			B: uint8((packed >> 16) & 0xff),
			A: 0xff,
		}}
	}
	fmt.Printf("accent as read:       %s  contrast on dark %.2f  on light %.2f\n",
		hex(accent.Colour),
		theme.Contrast(accent.Colour, raster.Hex("#1f1f1f")),
		theme.Contrast(accent.Colour, raster.Hex("#f3f3f3")))

	systemDark := winapi.SystemDark()
	fmt.Printf("system reports dark:  %v\n", systemDark)

	for _, light := range []bool{!systemDark, true, false} {
		p := theme.On(light)
		fmt.Printf("resolved light=%-5v  ink %s  accent %s (contrast %.2f)  ok %s  warn %s\n",
			light, hex(p.Ink), hex(p.Accent),
			theme.Contrast(p.Accent, p.Taskbar()),
			hex(p.OK), hex(p.Warn))
	}
	// And the claim the tones are built to satisfy: every ink is legible against the
	// surface it is drawn on.
	for _, light := range []bool{false, true} {
		p := theme.On(light)
		for _, m := range []struct {
			name string
			c    raster.RGBA
		}{
			{"text", p.Text}, {"muted", p.Muted}, {"faint", p.Faint},
			{"green", p.Green}, {"amber", p.Warn}, {"red", p.Bad}, {"blue", p.Blue},
		} {
			for _, bg := range []struct {
				name string
				c    raster.RGBA
			}{{"bg", p.BG}, {"surface", p.Surface}, {"rail", p.Rail}} {
				if r := theme.Contrast(m.c, bg.c); r < 3.0 {
					fmt.Printf("  LOW  light=%-5v %-6s on %-7s %.2f\n", light, m.name, bg.name, r)
				}
			}
		}
	}
}

func hex(c raster.RGBA) string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }
