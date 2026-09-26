//go:build windows

package tray

import (
	"math"

	"wbtray/internal/raster"
	"wbtray/internal/textmask"
	"wbtray/internal/theme"
	"wbtray/internal/traymenu"
)

// Painting the drawn menu.

// paintMenu renders the whole flyout into the canvas. It is pure: the same
// arguments always produce the same pixels, which is what makes the layout
// testable without a window.
func paintMenu(c *raster.Canvas, rows []row, scale float64, th theme.Theme, hover int, scroll float64) {
	// The window is filled to its edges with the menu's background, and there is
	// deliberately no drop shadow.
	//
	// A shadow was drawn here in the first version and it is what produced the
	// thick dark frame: the bitmap is blitted with BitBlt, which ignores the
	// alpha channel, so a shadow pixel of near-transparent black arrived on
	// screen as opaque black. Either the window becomes layered — every pixel
	// composited rather than painted — or the shadow goes, and the shadow is not
	// worth that machinery. Native menus have no shadow either.
	//
	// Filling the whole window also means the border below is a real edge rather
	// than an inset one, and no pixel of the window is left undefined.
	c.Fill(th.MenuBg)

	plateX := foShadowPad * scale
	plateY := foShadowPad * scale
	plateW := float64(c.W) - 2*plateX
	plateH := float64(c.H) - 2*plateY
	c.RoundedRect(plateX, plateY, plateW, plateH, foRadius*scale, th.MenuBg)

	// The frame is drawn inside the plate's own edge, so the menu's outer
	// dimension is exactly what the layout asked for and the border cannot fall
	// outside the window.
	c.StrokeRoundedRect(plateX+foBorderW*scale/2, plateY+foBorderW*scale/2,
		plateW-foBorderW*scale, plateH-foBorderW*scale,
		foRadius*scale, foBorderW*scale, th.MenuEdge)

	for i, r := range rows {
		// The scroll offset moves the rows inside the plate; rows that fall
		// outside are still drawn, and the plate clips nothing, so a row can
		// only be partially visible at the very top or bottom of a scrolled
		// list — which is what a scroll without a viewport would look like.
		y := plateY + (r.rect.Y-foShadowPad-scroll)*scale
		paintRow(c, r, i == hover, plateX, y, plateW, plateH, scale, th)
	}
}

// paintRow draws one row.
func paintRow(c *raster.Canvas, r row, hover bool, plateX, y, plateW, plateH, scale float64, th theme.Theme) {
	x := plateX
	w := plateW
	it := r.item

	if it.Kind == traymenu.SeparatorRow {
		// The rule is inset from both sides so it reads as a division between
		// two groups rather than as a line the menu failed to finish.
		mid := y + foSepH*scale/2
		c.Rect(x+foInset*scale+2*scale, mid, w-(foInset*2+4)*scale, 1, th.MenuLine)
		return
	}

	enabled := !it.Disabled
	if hover && enabled && r.owner < 0 {
		// Only top-level rows take the highlight. A submenu's children are
		// already marked by their indent, and highlighting them too makes the
		// expanded group look like a selection rather than like a list.
		c.RoundedRect(x+foInset*scale/2, y+1*scale,
			w-foInset*scale, (r.rect.H-2)*scale, 5*scale, th.MenuSel)
	} else if hover && enabled {
		c.RoundedRect(x+foInset*scale, y+1*scale,
			w-foInset*2*scale, (r.rect.H-2)*scale, 5*scale, th.MenuSel.Mul(0.65))
	}

	ink := th.MenuInk
	dim := th.MenuInkDim
	if !enabled {
		ink = dim
	}

	// The left cell: a health dot, a check mark, a radio dot, or the preview.
	cellX := x + foInset*scale
	cellW := foCheckW * scale
	centreX := cellX + cellW/2
	centreY := y + r.rect.H*scale/2

	switch {
	case it.Kind == traymenu.StyleRow && it.Preview != nil:
		size := int(math.Round(foPreviewW * scale))
		preview := it.Preview
		if preview.W != size {
			preview = preview.Scale(size, size)
		}
		c.DrawCanvas(preview, int(centreX)-size/2, int(centreY)-size/2, 1)
	case it.Dot.A > 0:
		c.Circle(centreX, centreY, foDotR*scale, it.Dot)
	case it.Kind == traymenu.RadioRow:
		if it.Checked {
			c.Circle(centreX, centreY, foDotR*scale, th.Accent)
		}
	case it.Checked:
		paintCheck(c, cellX+cellW*0.18, y+r.rect.H*scale*0.30, scale, th.Accent)
	}

	// The label.
	textX := x + labelStart(r.depth)*scale
	spec := textmask.NewFont(int(math.Round(foFontSize*scale)), it.Bold)
	textY := centreY - float64(spec.Size())*0.62

	// The right-hand column: a value, a chevron, or nothing.
	rightX := x + w - foInset*scale
	if it.Value != "" {
		vspec := textmask.NewFont(int(math.Round(foFontSizeSm*scale)), false)
		// The label gets the row and the value takes what is left, capped so a
		// long URL cannot squeeze the name it belongs to. A row is identified by
		// its name: a label clipped to "wbtray · …" beside a complete address is
		// the wrong way round, and it is what the first drawn menu did.
		labelRoom := w - (textX - x) - foInset*scale
		valueRoom := math.Min(labelRoom*valueShare, float64(drawWidth(it.Value, int(math.Round(foFontSizeSm*scale)))))
		valueText := textmask.Truncate(it.Value, vspec, int(math.Max(valueRoom, minValueWidth*scale)))
		vw, _ := textmask.Measure(valueText, vspec)
		textmask.DrawRight(c, rightX, textY+1.5*scale, vspec, valueText, dim)
		if rightX-float64(vw)-rowGap*scale > textX {
			rightX -= float64(vw) + rowGap*scale
		}
	}
	// The label gets exactly what the layout reserved for it and no less: the
	// arithmetic here mirrors rowNaturalWidth, so a row that fits by the layout's
	// reckoning fits when it is drawn. A fudge factor between the two is what
	// clipped "Classic menu (system style)" to "Classic menu (system styl…" while
	// the layout insisted it fit.
	maxText := int(rightX - textX)
	if it.Kind == traymenu.SubmenuRow {
		maxText -= int(foArrowW * scale)
	}
	label := textmask.Truncate(it.Text, spec, maxText)
	textmask.Draw(c, textX, textY, spec, label, ink)

	if it.Kind == traymenu.SubmenuRow {
		paintChevron(c, rightX-foArrowW*scale*0.6, centreY, scale, dim, it.IsExpanded())
	}
}

// paintCheck draws a tick mark.
func paintCheck(c *raster.Canvas, x, y, scale float64, colour raster.RGBA) {
	t := 1.9 * scale
	if t < 1.2 {
		t = 1.2
	}
	pts := []raster.Pt{
		{X: x, Y: y + 4.2*scale},
		{X: x + 3.4*scale, Y: y + 7.4*scale},
		{X: x + 9.6*scale, Y: y},
	}
	c.Line(pts, t, colour)
}

// paintChevron draws the submenu arrow, pointing right when closed and down
// when the submenu is expanded inline.
func paintChevron(c *raster.Canvas, x, y, scale float64, colour raster.RGBA, down bool) {
	t := 1.7 * scale
	if t < 1.1 {
		t = 1.1
	}
	const s = 3.6
	var pts []raster.Pt
	if down {
		pts = []raster.Pt{
			{X: x - s*scale, Y: y - s*0.5*scale},
			{X: x, Y: y + s*0.6*scale},
			{X: x + s*scale, Y: y - s*0.5*scale},
		}
	} else {
		pts = []raster.Pt{
			{X: x - s*0.5*scale, Y: y - s*scale},
			{X: x + s*0.6*scale, Y: y},
			{X: x - s*0.5*scale, Y: y + s*scale},
		}
	}
	c.Line(pts, t, colour)
}

// hitRow returns the index of the row containing a point in logical window
// coordinates, or -1.
func (f *flyout) hitRow(x, y float64) int {
	f.mu.Lock()
	rows := f.rows
	scroll := f.scroll
	f.mu.Unlock()
	for i, r := range rows {
		top := r.rect.Y - scroll
		if y >= top && y < top+r.rect.H {
			if r.item.Kind == traymenu.SeparatorRow {
				return -1
			}
			return i
		}
	}
	return -1
}
