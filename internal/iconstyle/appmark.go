package iconstyle

import (
	"math"

	"wbtray/internal/raster"
	"wbtray/internal/theme"
)

// The program's own mark, for the file, the taskbar button and the alt-tab
// thumbnail.
//
// It is not one of the tray styles, and that is the point. A tray style is a
// reading: it draws the current number in the current health colour, which is what
// makes a glance at the notification area useful and what makes it wrong for a file
// icon — the icon in Explorer is drawn once, at build time, and a reading baked
// into it would be a lie by the next day.
//
// So this is an identity rather than a reading: the same gauge the tray draws, on
// the plate an application icon is expected to have, in the palette's neutral inks
// so it sits on a light desktop and a dark one alike.
func DrawAppMark(size int) *raster.Canvas {
	if size < 8 {
		size = 8
	}
	c := raster.New(size*super, size*super)
	p := theme.Dark()

	// The plate. An application icon is a tile rather than a bare glyph, unlike a
	// tray icon, and it fills its box for the same reason: the shell draws it at
	// half a dozen sizes and one that left a margin would look undersized beside
	// every other icon in the folder.
	side := float64(c.W)
	c.RoundedRect(0, 0, side, side, side*0.22, p.Raised)

	// The mark: an open gauge, which is the shape the tray's default style uses and
	// so the one an operator will have seen in the notification area.
	cx, cy := side/2, side*0.54
	ro := side * 0.30
	ri := ro * 0.60
	c.Arc(cx, cy, (ri+ro)/2, ro-ri, -215, 35, p.Text)

	// And the reading, as a filled arc and a pip: the two things the tray icon
	// carries, so the file icon and the tray icon are recognisably one product.
	c.Arc(cx, cy, (ri+ro)/2, ro-ri, -215, -60, p.Green)
	c.Circle(cx+ro*math.Cos(-60*math.Pi/180), cy+ro*math.Sin(-60*math.Pi/180), side*0.045, p.Green)

	return c.Downsample(super)
}
