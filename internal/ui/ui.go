// Package ui lays out wbtray's own window.
//
// The tray keeps the shell's own menu, because a native menu is the one control
// that behaves the way Windows users expect at the notification area: keyboard
// navigation, high contrast, screen readers and the platform's own sizing all come
// with it. The window is the opposite decision. It is a readout — panels of
// figures, a trend, a table — and none of that is expressible as a menu, which is
// why it is drawn rather than handed to the shell.
//
// This package decides where everything goes and paints the shapes. It does not
// draw the text. Text is the one thing a window should take from the system rather
// than from this program: Windows knows the operator's font, its size at their
// scaling, their ClearType settings and their language's glyphs, and a bitmap face
// built for a sixteen-pixel icon knows none of that. So the layout is emitted as a
// list of Text items, and the Windows front end draws them with GDI into the same
// buffer the shapes were painted into.
//
// There is still no toolkit and no third-party dependency. The cost is that the
// layout is arithmetic rather than a stylesheet, which is why the constants below
// are named after what they measure rather than after where they are used.
package ui

import (
	"fmt"
	"math"
	"time"

	"wbtray/internal/raster"
	"wbtray/internal/status"
	"wbtray/internal/theme"
)

// The window's spacing, in logical pixels.
const (
	PadX      = 20 // the window's own margin
	gap       = 12 // between panels
	cardPadX  = 14 // inside a panel
	cardPadY  = 12
	HeaderH   = 58
	statH     = 64
	chartH    = 210
	tableRowH = 34
	Corner    = 10
)

// Weight is how emphatically a piece of text is drawn. The names are the three
// levels the design uses rather than the weight numbers, because the numbers are
// a font's business and these are the layout's.
type Weight int

const (
	// Label is a column heading or a caption.
	Label Weight = iota
	// Body is ordinary text.
	Body
	// Figure is a number the eye should land on first.
	Figure
)

// Text is one string and where it goes. Size is in logical pixels; the front end
// scales it by the window's DPI before asking for a font, so the layout stays in
// one unit and the rendering happens in another.
type Text struct {
	S      string
	X, Y   float64
	Size   float64
	Weight Weight
	Colour raster.RGBA
	// Align places the string relative to X. The zero value is left; right is
	// what a column of figures wants, and the front end is the only side that can
	// measure the string in the operator's own font.
	Align Align
}

// Align is how a string sits against its X.
type Align int

const (
	// Left starts the string at X.
	Left Align = iota
	// Right ends the string at X.
	Right
)

// Layout is one frame: the shapes to paint, and the text to draw over them.
type Layout struct {
	W, H  int
	Ink   Canvas
	Texts []Text
}

// Canvas is the drawing surface the front end paints the shapes into. It is the
// renderer's own canvas, so a shape drawn here is the same code that draws the
// tray icon.
type Canvas = raster.Canvas

// Build lays out a frame at the given size.
func Build(v View) Layout {
	l := Layout{W: v.W, H: v.H}
	if v.W < 520 || v.H < 320 {
		// Too small to lay out. The surface is still painted, so the window shows
		// its own colour rather than whatever was behind it.
		l.Ink = *raster.New(v.W, v.H)
		l.Ink.Fill(surface(v.Palette))
		return l
	}
	l.Ink = *raster.New(v.W, v.H)
	l.Ink.Fill(surface(v.Palette))
	header(&l, v)
	stats(&l, v)

	// The chart takes the space the table does not need, which is the right way
	// round: a table of three rows is three rows whatever the window's height, and
	// a trend is the one thing that reads better with more of it. The alternative —
	// a fixed chart and a table that grows — leaves a panel of empty rows under
	// three accounts, which is what the first version of this did.
	tableH := float64(accountsHeight(v))
	top := float64(HeaderH + statH + gap)
	chartH := float64(v.H) - top - float64(PadX) - tableH - gap
	if chartH < 120 {
		chartH = 120
	}
	chart(&l, v, top, chartH)
	accounts(&l, v, top+chartH+gap, tableH)
	return l
}

// accountsHeight is how tall the table wants to be: its chrome plus one row per
// account, capped so a pool of fifty accounts does not push the trend off screen.
func accountsHeight(v View) int {
	rows := len(v.Snap.Accounts)
	if rows < 1 {
		rows = 1
	}
	if rows > 12 {
		rows = 12
	}
	// The caption, the column headings, the rule, the rows, and the panel's own
	// padding, in the order they are drawn.
	return cardPadY + 26 + 20 + 10 + rows*tableRowH + cardPadY
}

// View is everything one frame needs.
type View struct {
	W, H    int
	Palette theme.Palette
	Snap    status.Snapshot
	Lang    string
}

// The colours the window needs beyond a palette's inks.
//
// They are derived from the palette's tone rather than kept in a second table: a
// window is a surface and needs a background, and adding one here rather than to
// Palette keeps the palettes about marks, which is what both the icon and the menu
// need them for.
// The window's surfaces come from the palette rather than from a second table: the
// tones were chosen as a set, and a surface invented here would be one more colour
// to keep in step.
func surface(p theme.Palette) raster.RGBA { return p.BG }

func card(p theme.Palette) raster.RGBA { return p.Surface }

func hairline(p theme.Palette) raster.RGBA { return p.Border }

// add appends a piece of text to the frame.
func (l *Layout) add(s string, x, y, size float64, w Weight, col raster.RGBA) {
	l.Texts = append(l.Texts, Text{S: s, X: x, Y: y, Size: size, Weight: w, Colour: col})
}

// header is the title line and the state pill.
func header(l *Layout, v View) {
	p := v.Palette
	l.add("WorkBuddy2API", PadX, 14, 17, Figure, p.Ink)

	sub := v.Snap.Version
	if v.Snap.Uptime > 0 {
		sub += "   ·   " + uptime(v.Snap.Uptime)
	}
	if sub != "" {
		l.add(sub, PadX, 38, 11.5, Label, p.InkDim)
	}

	// The pill is right-aligned against the window's own margin and carries the
	// one word the whole window is about.
	label, dot := "离线", p.Bad
	if v.Snap.Reachable {
		label, dot = "正常", p.OK
	}
	pillW := 76.0
	px := float64(l.W) - PadX - pillW
	l.Ink.RoundedRect(px, 20, pillW, 26, 13, card(p))
	l.Ink.Circle(px+16, 33, 4, dot)
	l.add(label, px+28, 26, 12, Body, p.Ink)
}

// stats is the row of figures across the top.
func stats(l *Layout, v View) {
	p, snap := v.Palette, v.Snap
	n := 4
	w := float64(l.W-2*PadX-gap*(n-1)) / float64(n)
	figures := []struct {
		label, value, note string
		accent             raster.RGBA
	}{
		{"积分剩余", comma(snap.CreditTotal()), "", p.Accent},
		{"账号可用", fmt.Sprintf("%d/%d", snap.Ready(), snap.Total), "", p.OK},
		{"24 小时请求", compact(float64(snap.Usage.Requests)),
			fmt.Sprintf("%d 失败", snap.Usage.Errors), p.Warn},
		{"Token 合计", compact(float64(snap.Usage.TotalTokens)), "", p.Ink},
	}
	for i, f := range figures {
		x := float64(PadX) + float64(i)*(w+gap)
		y := float64(HeaderH)
		panel(&l.Ink, p, x, y, w, statH)
		// A rule in the figure's own colour, which is what tells four otherwise
		// identical panels apart at a glance.
		l.Ink.RoundedRect(x+cardPadX, y+cardPadY+3, 3, 15, 1.5, f.accent)
		l.add(f.label, x+cardPadX+12, y+cardPadY, 11, Label, p.InkDim)
		l.add(f.value, x+cardPadX+12, y+cardPadY+20, 21, Figure, p.Ink)
		if f.note != "" {
			// The note sits on the figure's own baseline, right-aligned against the
			// panel's edge. It is placed here rather than beside the label because a
			// panel is only as wide as a quarter of the window, and at that width a
			// note beside the label collides with it.
			l.add(f.note, x+w-cardPadX, y+cardPadY+26, 10.5, Label, p.Warn)
			l.Texts[len(l.Texts)-1].Align = Right
		}
	}
}

// chart is the trend: the window's requests as an area under a line.
//
// No axes and no grid, which is a decision rather than an omission. The window is
// opened from a tray that already shows the current figure, so the question this
// answers is "is it busy now compared with recently" — and for that the shape is
// the whole answer. The exact figures are in the panels above.
func chart(l *Layout, v View, y, h float64) {
	p := v.Palette
	x := float64(PadX)
	w := float64(l.W - 2*PadX)
	panel(&l.Ink, p, x, y, w, h)

	innerX := x + cardPadX
	innerY := y + cardPadY
	innerW := w - 2*cardPadX

	l.add("请求趋势", innerX, innerY, 11, Label, p.InkDim)

	series := v.Snap.SeriesFor("requests")
	if len(series) < 2 {
		l.add("暂无数据（网关未运行或尚未有请求）", innerX, innerY+46, 12, Body, p.InkDim)
		return
	}

	top := innerY + 30
	height := h - cardPadY - 40
	base := top + height
	peak := 1.0
	for _, s := range series {
		if s > peak {
			peak = s
		}
	}

	// The area first, as a column per pixel. Each column's height comes from the
	// curve interpolated between its samples, which is what makes a dozen points
	// fill a thousand-pixel panel without either stepping or a scanline filler.
	at := func(t float64) float64 {
		pos := t * float64(len(series)-1)
		i := int(pos)
		if i >= len(series)-1 {
			return series[len(series)-1]
		}
		f := pos - float64(i)
		return series[i]*(1-f) + series[i+1]*f
	}
	fill := p.Accent.Mul(0.20)
	width := int(innerW)
	for px := 0; px < width; px++ {
		t := float64(px) / float64(width-1)
		py := int(base - height*at(t)/peak)
		for yy := py; yy < int(base); yy++ {
			l.Ink.BlendAt(int(innerX)+px, yy, fill)
		}
	}
	// Then the line, over it: two pixels, which at this size reads as a curve
	// rather than as a hairline.
	pts := make([]raster.Pt, len(series))
	for i, s := range series {
		pts[i] = raster.Pt{
			X: innerX + innerW*float64(i)/float64(len(series)-1),
			Y: base - height*s/peak,
		}
	}
	l.Ink.Line(pts, 2.0, p.Accent)
	last := pts[len(pts)-1]
	l.Ink.Circle(last.X, last.Y, 3.5, p.Accent)

	// The peak, labelled: it is the one figure worth putting on a chart with no
	// axis, because it is what the rest of the shape is compared against. And the
	// span, so the shape has a scale in time if not in value.
	l.add(compact(peak), innerX, innerY+16, 10.5, Label, p.InkDim)
	// The span is right-aligned inside the panel rather than at its edge, so it
	// cannot be clipped by the panel's own border.
	l.add(fmt.Sprintf("过去 %d 小时", len(series)),
		innerX+innerW-cardPadX, innerY+16, 10.5, Label, p.InkDim)
	l.Texts[len(l.Texts)-1].Align = Right
}

// accounts is the table.
func accounts(l *Layout, v View, y, h float64) {
	p, snap := v.Palette, v.Snap
	x := float64(PadX)
	w := float64(l.W - 2*PadX)
	if h < 80 {
		return
	}
	panel(&l.Ink, p, x, y, w, h)

	innerX := x + cardPadX
	innerW := w - 2*cardPadX
	cur := y + cardPadY

	l.add("账号池", innerX, cur, 11, Label, p.InkDim)
	cur += 26

	// Column offsets, as fractions of the table's width, so the columns stay
	// aligned to each other rather than to a pixel count that assumes a width.
	cols := []struct {
		label string
		at    float64
		right bool
	}{
		{"账号", 0, false}, {"状态", 0.27, false}, {"积分", 0.42, true},
		{"请求", 0.58, true}, {"延迟", 0.74, true},
		{"吐字", 0.86, true}, {"最近使用", 1.0, true},
	}
	for _, col := range cols {
		cx := innerX + innerW*col.at
		if col.right {
			l.add(col.label, cx, cur, 10.5, Label, p.InkDim)
			// A right-aligned heading is drawn at its own right edge; the front
			// end measures the string and shifts it, which is the only way it can
			// know the width in the operator's font.
			l.Texts[len(l.Texts)-1].X = cx
			l.Texts[len(l.Texts)-1].Align = Right
		} else {
			l.add(col.label, cx, cur, 10.5, Label, p.InkDim)
		}
	}
	cur += 20
	l.Ink.Rect(innerX, cur, innerW, 1, hairline(p))
	cur += 10

	if len(snap.Accounts) == 0 {
		l.add("网关还没有账号。打开面板添加一个。", innerX, cur+8, 12, Body, p.InkDim)
		return
	}
	for i, a := range snap.Accounts {
		// The height was computed to fit exactly this many rows, so the test is
		// against the panel's own bottom edge rather than a guessed margin: an
		// earlier version subtracted four pixels of slack and dropped the last row
		// of a three-row table.
		if cur+tableRowH > y+h-cardPadY {
			l.add(fmt.Sprintf("… 另有 %d 个", len(snap.Accounts)-i),
				innerX, cur+8, 11.5, Label, p.InkDim)
			break
		}
		name := a.Nickname
		if name == "" {
			name = a.UID
		}
		l.add(clip(name, 22), innerX, cur+7, 13, Body, p.Ink)

		// The state is read before the numbers, because a disabled account's
		// credits are not available whatever the figure says.
		state, sc := "可用", p.OK
		switch {
		case a.Disabled:
			state, sc = "已禁用", p.InkDim
		case a.Cooling:
			state, sc = "冷却中", p.Warn
			if left := a.CoolRemaining(); left > 0 {
				state = "冷却 " + duration(left)
			}
		case a.InFlight > 0:
			state, sc = fmt.Sprintf("在途 %d", a.InFlight), p.Accent
		}
		l.add(state, innerX+innerW*0.27, cur+7, 12, Body, sc)

		credits := comma(a.Credits)
		if a.Total > 0 {
			credits += "  /" + comma(a.Total)
		}
		l.add(credits, innerX+innerW*0.42, cur+7, 12.5, Body, p.Ink)
		l.Texts[len(l.Texts)-1].Align = Right
		// The account's own request count, from its own token usage. It is not the
		// same figure as the pool's, and showing the pool's here would make three
		// rows agree with each other and with nothing else.
		l.add(compact(float64(a.Requests)), innerX+innerW*0.58, cur+7, 12.5, Body, p.Ink)
		l.Texts[len(l.Texts)-1].Align = Right
		if a.LastLatencyMs > 0 {
			l.add(oneDecimal(a.LastLatencyMs/1000)+"s", innerX+innerW*0.74, cur+7, 12.5, Body, p.Ink)
		} else {
			l.add("—", innerX+innerW*0.74, cur+7, 12.5, Body, p.InkDim)
		}
		l.Texts[len(l.Texts)-1].Align = Right
		if a.LastTPS > 0 {
			l.add(fmt.Sprintf("%.0f", a.LastTPS), innerX+innerW*0.86, cur+7, 12.5, Body, p.Ink)
		} else {
			l.add("—", innerX+innerW*0.86, cur+7, 12.5, Body, p.InkDim)
		}
		l.Texts[len(l.Texts)-1].Align = Right
		l.add(since(a.LastUsed), innerX+innerW, cur+8, 11.5, Label, p.InkDim)
		l.Texts[len(l.Texts)-1].Align = Right

		cur += tableRowH
		if i < len(snap.Accounts)-1 {
			l.Ink.Rect(innerX, cur-10, innerW, 1, hairline(p).Mul(0.55))
		}
	}
}

// panel draws a card: its background, then its hairline edge.
func panel(c *raster.Canvas, p theme.Palette, x, y, w, h float64) {
	c.RoundedRect(x, y, w, h, Corner, card(p))
	strokeRounded(c, x, y, w, h, Corner, hairline(p))
}

// strokeRounded draws a one-pixel rounded outline.
//
// It is four rules and four corner runs rather than a stroked path, because the
// renderer has no path stroker and one is not worth writing for a rectangle. The
// corners are the part that has to be right: a square corner on a rounded panel is
// the single detail that makes a drawn window read as drawn.
func strokeRounded(c *raster.Canvas, x, y, w, h, r float64, col raster.RGBA) {
	c.Rect(x+r, y, w-2*r, 1, col)
	c.Rect(x+r, y+h-1, w-2*r, 1, col)
	c.Rect(x, y+r, 1, h-2*r, col)
	c.Rect(x+w-1, y+r, 1, h-2*r, col)
	const steps = 8
	for i := 0; i <= steps; i++ {
		ang := float64(i) / steps * math.Pi / 2
		dx := r - r*math.Cos(ang)
		dy := r - r*math.Sin(ang)
		c.Rect(x+dx, y+dy, 1, 1, col)
		c.Rect(x+w-1-dx, y+dy, 1, 1, col)
		c.Rect(x+dx, y+h-1-dy, 1, 1, col)
		c.Rect(x+w-1-dx, y+h-1-dy, 1, 1, col)
	}
}

// The formatters. They are here rather than in i18n because they are about what
// fits a column, not about what language the column is in.

// comma groups thousands, which is how a balance is read.
func comma(v int64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	s := fmt.Sprintf("%d", v)
	out := make([]byte, 0, len(s)+len(s)/3)
	for i := 0; i < len(s); i++ {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, s[i])
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}

// compact shortens a large count so it fits a column: 128400 becomes 128.4k.
func compact(v float64) string {
	switch {
	case v >= 1e9:
		return fmt.Sprintf("%.1fG", v/1e9)
	case v >= 1e6:
		return fmt.Sprintf("%.1fM", v/1e6)
	case v >= 1000:
		return fmt.Sprintf("%.1fk", v/1000)
	case v == math.Trunc(v):
		return fmt.Sprintf("%d", int64(v))
	default:
		return fmt.Sprintf("%.1f", v)
	}
}

func oneDecimal(v float64) string { return fmt.Sprintf("%.1f", v) }

// uptime renders a process lifetime the way a header wants it.
func uptime(sec int64) string {
	switch {
	case sec < 60:
		return fmt.Sprintf("%ds", sec)
	case sec < 3600:
		return fmt.Sprintf("%dm", sec/60)
	case sec < 86400:
		return fmt.Sprintf("%dh %02dm", sec/3600, (sec%3600)/60)
	default:
		return fmt.Sprintf("%dd %02dh", sec/86400, (sec%86400)/3600)
	}
}

// duration renders a cooldown, which is short and wants seconds.
func duration(d time.Duration) string {
	s := int64(d.Seconds())
	switch {
	case s < 60:
		return fmt.Sprintf("%ds", s)
	case s < 3600:
		return fmt.Sprintf("%dm", s/60)
	default:
		return fmt.Sprintf("%dh", s/3600)
	}
}

// since renders how long ago something happened, which is the one column in the
// table whose meaning is a duration rather than a count.
func since(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "刚刚"
	case d < time.Hour:
		return fmt.Sprintf("%d 分钟前", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d 小时前", int(d.Hours()))
	default:
		return fmt.Sprintf("%d 天前", int(d.Hours()/24))
	}
}

// clip truncates a label to the column it has to fit, so one long nickname does
// not run into the column beside it.
func clip(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
