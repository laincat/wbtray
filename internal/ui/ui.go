package ui

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"wbtray/internal/i18n"
	"wbtray/internal/panel"
	"wbtray/internal/raster"
	"wbtray/internal/status"
	"wbtray/internal/theme"
)

// Package ui lays out wbtray's window.
//
// The tray keeps the shell's own menu, because a native menu is the one control
// that behaves the way Windows users expect at the notification area: keyboard
// navigation, high contrast, screen readers and the platform's own sizing all come
// with it. The window is the opposite decision. It is the console — an account
// table, a model catalogue, the gateway's own log, the schedule, the settings — and
// none of that is expressible as a menu, which is why it is drawn rather than handed
// to the shell.
//
// This package decides where everything goes, paints the shapes, and reports which
// rectangles are clickable. It does not draw the text. Text is the one thing a
// window should take from the system: Windows knows the operator's font, its size at
// their scaling, their ClearType settings and their language's glyphs, and a bitmap
// face built for a sixteen-pixel icon knows none of that. So the layout emits Text
// items and the Windows front end draws them with GDI into the same buffer the
// shapes were painted into.
//
// There is still no toolkit and no third-party dependency. The cost is that the
// layout is arithmetic rather than a stylesheet, which is why the constants below
// are named after what they measure rather than after where they are used.

// The window's spacing, in logical pixels.
const (
	RailW     = 170
	PadX      = 18
	gap       = 12
	cardPadX  = 14
	cardPadY  = 12
	HeaderH   = 56
	RailRowH  = 34
	FooterH   = 30
	statH     = 62
	tableRowH = 32
	Corner    = 10
)

// Tab is which page the window is showing.
type Tab string

// The pages. The names are the gateway's own words for its own sections, so an
// operator who has used the console in a browser recognises them.
const (
	TabOverview Tab = "overview"
	TabAccounts Tab = "accounts"
	TabModels   Tab = "models"
	TabLogs     Tab = "logs"
	TabSchedule Tab = "schedule"
	TabConfig   Tab = "config"
)

// Tabs is the rail order.
var Tabs = []Tab{TabOverview, TabAccounts, TabModels, TabLogs, TabSchedule, TabConfig}

// tabLabel is the rail's wording.
func tabLabel(t Tab, lang string) string {
	return i18n.T(lang, "page."+string(t))
}

// tr is the message for a key in the language this frame is drawn in.
//
// It is a method rather than a free function so the call sites read as part of the
// layout: every string in this file comes from one table, and a frame filled in with
// literals is what the table exists to prevent.
func (v View) tr(key string, args ...any) string { return i18n.T(v.Lang, key, args...) }

// Weight is how emphatically a piece of text is drawn.
type Weight int

const (
	// Label is a column heading or a caption.
	Label Weight = iota
	// Body is ordinary text.
	Body
	// Figure is a number the eye should land on first.
	Figure
)

// Align is how a string sits against its X.
type Align int

const (
	// Left starts the string at X.
	Left Align = iota
	// Right ends the string at X.
	Right
	// Centre centres the string on X.
	Centre
)

// Text is one string and where it goes. Size is in logical pixels; the front end
// scales it by the window's DPI before asking for a font, so the layout stays in one
// unit and the rendering happens in another.
type Text struct {
	S      string
	X, Y   float64
	Size   float64
	Weight Weight
	Colour raster.RGBA
	Align  Align
}

// Hit is a clickable rectangle and what clicking it means.
//
// The action is an opaque token the front end hands back, so this package does not
// have to know what any of them do — but the vocabulary is stated here rather than
// invented at the call site, because a typo in an action name would be a button that
// silently does nothing.
type Hit struct {
	X, Y, W, H float64
	Action     Action
	// Arg is the action's subject: an account's uid, or a tab's name.
	Arg string
}

// Action is what a click does.
type Action string

// The actions. Every one of them maps to a gateway endpoint or a local setting.
const (
	ActionTab            Action = "tab"
	ActionRefresh        Action = "refresh"
	ActionBalance        Action = "balance_all"
	ActionCheckin        Action = "checkin_all"
	ActionTravel         Action = "travel_all"
	ActionActivity       Action = "activity_all"
	ActionKeepalive      Action = "keepalive_all"
	ActionScan           Action = "scan_all"
	ActionRunQueue       Action = "run_queue"
	ActionAccountCheckin Action = "acct_checkin"
	ActionAccountBalance Action = "acct_balance"
	ActionAccountRevive  Action = "acct_revive"
	ActionOpenPanel      Action = "open_panel"
	ActionOpenGatewayDir Action = "open_dir"
	ActionTogglePause    Action = "pause"
	// ActionCopyAddress and ActionCopyKey put the two values an operator would
	// otherwise go and find on the clipboard. They live in the console rather than in
	// the tray panel, because the panel is for a glance and these are for a task.
	ActionCopyAddress Action = "copy_addr"
	ActionCopyKey     Action = "copy_key"
)

// View is everything one frame needs.
type View struct {
	W, H int
	Tab  Tab
	Snap status.Snapshot
	Lang string
	// Palette is the tone to draw in.
	Palette theme.Palette

	// Action is the last thing the operator did and what came of it, shown in the
	// footer. An action still running says so, because a console that reports
	// nothing for two seconds after a click reads as a console that did not hear
	// the click.
	Action  string
	Pending bool

	Models     []panel.Model
	Logs       []panel.LogEntry
	Schedule   panel.Schedule
	Config     string
	ConfigPath string
	Paused     bool

	// Scroll offsets, per page, in rows. The window has no scrollbar: a wheel is
	// the only way to move, and a page that fits shows the same thing at any
	// offset.
	ScrollLogs   int
	ScrollModels int
	// ScrollAccounts is the first row of the account table shown.
	ScrollAccounts int
}

// Layout is one frame: the shapes painted, the text to draw over them, and the
// rectangles a click can land on.
type Layout struct {
	W, H  int
	Ink   Canvas
	Texts []Text
	Hits  []Hit
}

// Canvas is the drawing surface, which is the renderer's own: a shape drawn here is
// the same code that draws the tray icon.
type Canvas = raster.Canvas

// Build lays out a frame at the given size.
func Build(v View) Layout {
	l := Layout{W: v.W, H: v.H}
	if v.W < 720 || v.H < 420 {
		// Too small to lay out. The surface is still painted, so the window shows
		// its own colour rather than whatever was behind it.
		l.Ink = *raster.New(v.W, v.H)
		l.Ink.Fill(v.Palette.BG)
		return l
	}
	l.Ink = *raster.New(v.W, v.H)
	l.Ink.Fill(v.Palette.BG)
	rail(&l, v)
	content(&l, v)
	footer(&l, v)
	return l
}

// The surfaces come from the palette rather than from a second table: the tones were
// chosen as a set, and a surface invented here would be one more colour to keep in
// step.
func card(p theme.Palette) raster.RGBA { return p.Surface }

func hairline(p theme.Palette) raster.RGBA { return p.Border }

// add appends a piece of text to the frame.
func (l *Layout) add(s string, x, y, size float64, w Weight, col raster.RGBA) {
	if s == "" {
		return
	}
	l.Texts = append(l.Texts, Text{S: s, X: x, Y: y, Size: size, Weight: w, Colour: col})
}

// addRight appends right-aligned text, which a column of figures needs.
func (l *Layout) addRight(s string, x, y, size float64, w Weight, col raster.RGBA) {
	if s == "" {
		return
	}
	l.Texts = append(l.Texts, Text{S: s, X: x, Y: y, Size: size, Weight: w, Colour: col, Align: Right})
}

// addCentre appends centred text, which a button label needs.
func (l *Layout) addCentre(s string, x, y, size float64, w Weight, col raster.RGBA) {
	if s == "" {
		return
	}
	l.Texts = append(l.Texts, Text{S: s, X: x, Y: y, Size: size, Weight: w, Colour: col, Align: Centre})
}

// hit registers a clickable rectangle.
func (l *Layout) hit(x, y, w, h float64, a Action, arg string) {
	l.Hits = append(l.Hits, Hit{X: x, Y: y, W: w, H: h, Action: a, Arg: arg})
}

// rail is the navigation down the left edge.
func rail(l *Layout, v View) {
	p := v.Palette
	l.Ink.Rect(0, 0, RailW, float64(l.H), p.Rail)
	l.Ink.Rect(RailW-1, 0, 1, float64(l.H), hairline(p))

	// The brand, which is also where the version goes: a console that does not say
	// which build it is talking to is a console whose screenshots cannot be
	// compared.
	//
	// The name is the program's own and the line under it is the gateway's build, in
	// that order and with those labels. An earlier version drew the upstream
	// project's name here, where the rail is 170 pixels wide and no font fits it, so
	// the name every operator saw was "WorkBuddy2A…" — a truncation of something that
	// was not this program's name to begin with.
	l.add(v.tr("app.name"), 16, 16, 13.5, Figure, p.Text)
	version := v.tr("ui.gateway_version", orDash(v.Snap.Version))
	l.add(clip(version, 22), 16, 36, 11, Label, p.Faint)

	y := float64(66)
	for _, t := range Tabs {
		active := t == v.Tab
		if active {
			l.Ink.RoundedRect(8, y, RailW-16, RailRowH-4, 6, p.Raised)
		}
		col := p.Muted
		w := Body
		if active {
			col = p.Text
			w = Figure
		}
		l.add(tabLabel(t, v.Lang), 20, y+7, 12.5, w, col)
		// A count beside the two pages that have one, which is the pattern the
		// dashboards this follows use: the number is what says whether the page is
		// worth opening, so it belongs on the navigation rather than inside.
		if n := tabCount(t, v); n != "" {
			l.addRight(n, RailW-16, y+9, 10.5, Label, p.Faint)
		}
		l.hit(8, y, RailW-16, RailRowH-4, ActionTab, string(t))
		y += RailRowH
	}

	// The state, at the bottom of the rail: it belongs to the program rather than
	// to the page.
	by := float64(l.H) - FooterH - 52
	l.Ink.Rect(1, by-8, RailW-1, 1, hairline(p))
	dot, label := p.Bad, v.tr("health.down")
	if v.Snap.Reachable {
		dot, label = p.Green, v.tr("health.ok")
	}
	if v.Paused {
		dot, label = p.Muted, v.tr("health.paused")
	}
	l.Ink.Circle(20, by+8, 4, dot)
	l.add(label, 32, by+2, 12, Body, p.Text)
	if v.Snap.Process.Found {
		l.add(fmt.Sprintf("PID %d", v.Snap.Process.PID), 20, by+22, 10.5, Label, p.Faint)
	}
}

// tabCount is the badge beside a navigation row.
func tabCount(t Tab, v View) string {
	switch t {
	case TabAccounts:
		if n := len(v.Snap.Accounts); n > 0 {
			return fmt.Sprintf("%d", n)
		}
	case TabModels:
		if n := len(v.Models); n > 0 {
			return fmt.Sprintf("%d", n)
		}
	case TabLogs:
		if n := len(v.Logs); n > 0 {
			return fmt.Sprintf("%d", n)
		}
	}
	return ""
}

// content dispatches to the page.
func content(l *Layout, v View) {
	switch v.Tab {
	case TabAccounts:
		accountsPage(l, v)
	case TabModels:
		modelsPage(l, v)
	case TabLogs:
		logsPage(l, v)
	case TabSchedule:
		schedulePage(l, v)
	case TabConfig:
		configPage(l, v)
	default:
		overviewPage(l, v)
	}
}

// contentBox is the area the page occupies, below the header and above the footer.
func contentBox(l *Layout, v View) (x, y, w, h float64) {
	// The left edge is past the rail, not at the window's own margin: an earlier
	// version started every page at PadX, which drew the tables on top of the
	// navigation.
	x = RailW + PadX
	return x, HeaderH, float64(l.W-RailW-2*PadX) - 2, float64(l.H-HeaderH-FooterH-8) - 2
}

// button is one action in a page header.
type button struct {
	Label   string
	Action  Action
	Arg     string
	Primary bool
}

// headerLine is the page title and its action buttons.
func headerLine(l *Layout, v View, title string, buttons []button, extra ...button) {
	p := v.Palette
	l.add(title, RailW+PadX, 16, 15, Figure, p.Text)
	if v.Snap.Uptime > 0 {
		l.add(v.tr("ui.running", uptime(v.Snap.Uptime, v.Lang)),
			RailW+PadX+textWidth(title, 15)+10, 19, 11, Label, p.Faint)
	}
	all := append(append([]button{}, buttons...), extra...)
	right := float64(l.W - PadX)
	for i := len(all) - 1; i >= 0; i-- {
		b := all[i]
		w := textWidth(b.Label, 12) + 22
		if w < 54 {
			w = 54
		}
		x := right - w
		bg, col := p.Raised, p.Text
		if b.Primary {
			bg, col = p.Accent, p.AccentInk
		}
		l.Ink.RoundedRect(x, 11, w, 26, 6, bg)
		l.addCentre(b.Label, x+w/2, 18, 12, Body, col)
		l.hit(x, 11, w, 26, b.Action, b.Arg)
		right = x - 6
	}
	// A rule under the header, so the page reads as a page.
	l.Ink.Rect(RailW, HeaderH-1, float64(l.W-RailW), 1, hairline(p))
}

// panel draws a card: its background, then its hairline edge.
func cardPanel(c *raster.Canvas, p theme.Palette, x, y, w, h float64) {
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

// footer is the line at the bottom: what the gateway is, and what the last action
// did.
func footer(l *Layout, v View) {
	p := v.Palette
	y := float64(l.H) - FooterH
	l.Ink.Rect(RailW, y, float64(l.W-RailW), 1, hairline(p))

	msg := v.Action
	col := p.Muted
	if v.Pending {
		msg = v.tr("ui.pending")
		col = p.Blue
	}
	if msg == "" {
		msg = v.tr("ui.summary",
			v.Snap.Ready(), v.Snap.Total, comma(v.Snap.CreditTotal()), v.Snap.InFlight())
	}
	l.add(clip(msg, 110), RailW+PadX, y+9, 11.5, Label, col)

	// The address, right-aligned: it is the fact an operator needs when something
	// is wrong and they are looking at this window.
	l.addRight(v.ConfigPath, float64(l.W-PadX), y+9, 11, Label, p.Faint)
}

// overviewPage is the figures page.
func overviewPage(l *Layout, v View) {
	p, snap := v.Palette, v.Snap
	headerLine(l, v, v.tr("page.overview"), []button{
		{Label: v.tr("btn.open_panel"), Action: ActionOpenPanel},
		{Label: v.tr("btn.refresh"), Action: ActionRefresh, Primary: true},
	})

	x, y, w, _ := contentBox(l, v)

	n := 4.0
	cw := (w - gap*(n-1)) / n
	figures := []struct {
		label, value, note string
		accent             raster.RGBA
	}{
		{v.tr("fig.credits"), comma(snap.CreditTotal()), "", p.Blue},
		{v.tr("fig.accounts"), fmt.Sprintf("%d/%d", snap.Ready(), snap.Total), "", p.Green},
		{v.tr("fig.requests24"), compact(float64(snap.Usage.Requests)),
			v.tr("fig.errors", snap.Usage.Errors), p.Warn},
		{v.tr("fig.tokens"), compact(float64(snap.Usage.TotalTokens)), "", p.Text},
	}
	for i, f := range figures {
		fx := x + float64(i)*(cw+gap)
		cardPanel(&l.Ink, p, fx, y, cw, statH)
		l.Ink.RoundedRect(fx+cardPadX, y+cardPadY+3, 3, 15, 1.5, f.accent)
		l.add(f.label, fx+cardPadX+12, y+cardPadY, 11, Label, p.Muted)
		l.add(f.value, fx+cardPadX+12, y+cardPadY+20, 21, Figure, p.Text)
		if f.note != "" {
			l.addRight(f.note, fx+cw-cardPadX, y+cardPadY+26, 10.5, Label, p.Warn)
		}
	}
	y += statH + gap

	// The pool as a row of pips, one per account: the pattern the dashboards this
	// follows use, and the fastest reading in the window. A bar of figures says how
	// many are well; a row of dots says which, and where the cooling one sits in the
	// order is what an operator is looking for.
	if n := len(snap.Accounts); n > 0 {
		cap := v.tr("cap.account_pips")
		l.add(cap, x, y, 11, Label, p.Muted)
		px := x + textWidth(cap, 11) + 12
		shown := n
		if room := int((w - (px - x)) / 16); shown > room {
			shown = room
		}
		for i := 0; i < shown; i++ {
			a := snap.Accounts[i]
			col := p.Green
			switch {
			case a.Disabled:
				col = p.Faint
			case a.Cooling:
				col = p.Warn
			case a.InFlight > 0:
				col = p.Blue
			}
			l.Ink.Circle(px+float64(i)*16+5, y+6, 5, col)
		}
		if shown < n {
			l.add(fmt.Sprintf("+%d", n-shown), px+float64(shown)*16+2, y, 10.5, Label, p.Faint)
		}
		y += 22
	}

	// The actions row, at the bottom of the page, because these are what an
	// operator reaches for while something is already wrong.
	actionsH := 30.0
	// The trend gets the room that is left, up to a ceiling. Filling the whole page
	// with it, which is what this did, draws twelve numbers as a meter-tall shape: the
	// extra height adds no resolution — there are only twelve samples — and it pushes
	// the maintenance row so far from the figures that the page reads as a chart with
	// two strips of chrome rather than as a dashboard.
	chartH := float64(l.H) - FooterH - 8 - y - gap - actionsH - 24
	if chartH > 220 {
		chartH = 220
	}
	if chartH < 80 {
		chartH = 80
	}
	chartCard(l, v, x, y, w, chartH)
	y += chartH + gap

	// What is left below the trend goes to the newest log lines. The chart is capped,
	// so without this the page carries a band of nothing between the trend and the
	// maintenance row — and the lines are the one thing an operator wants next to a
	// shape that says something is happening: the shape says how much, the lines say
	// what. They are the gateway's own ring, already fetched for the logs page.
	actsTop := float64(l.H) - FooterH - 8 - 26 - 18
	if activityH := actsTop - y - gap - 18; activityH >= 70 {
		activityCard(l, v, x, y, w, activityH)
	}

	// The maintenance row is anchored to the bottom of the page rather than dropped
	// under the trend: the rows above it change height with the data, and a row that
	// moved with them would be in a different place every time the window opened.
	y = actsTop

	l.add(v.tr("cap.maintenance"), x, y, 11, Label, p.Muted)
	y += 18
	acts := []button{
		{Label: v.tr("btn.checkin_all"), Action: ActionCheckin},
		{Label: v.tr("btn.travel_all"), Action: ActionTravel},
		{Label: v.tr("btn.activity_all"), Action: ActionActivity},
		{Label: v.tr("btn.keepalive_all"), Action: ActionKeepalive},
		{Label: v.tr("btn.balance_all"), Action: ActionBalance},
		{Label: v.tr("btn.scan_all"), Action: ActionScan},
		{Label: v.tr("btn.run_queue"), Action: ActionRunQueue},
	}
	bx := x
	for _, b := range acts {
		bw := textWidth(b.Label, 12) + 22
		if bw < 76 {
			bw = 76
		}
		if bx+bw > x+w {
			break
		}
		l.Ink.RoundedRect(bx, y, bw, 26, 6, p.Raised)
		l.addCentre(b.Label, bx+bw/2, y+7, 12, Body, p.Text)
		l.hit(bx, y, bw, 26, b.Action, b.Arg)
		bx += bw + 6
	}
}

// chartCard is the trend, drawn as an area under a line.
//
// No axes and no grid, which is a decision rather than an omission: the window is
// opened from a tray that already shows the current figure, so the question this
// answers is "is it busy now compared with recently", and for that the shape is the
// whole answer. The exact figures are in the panels above.
//
// It is also capped in height by its caller, because twelve samples do not need a
// meter of window and the page has a second card to fit under it.
func chartCard(l *Layout, v View, x, y, w, h float64) {
	p := v.Palette
	cardPanel(&l.Ink, p, x, y, w, h)

	innerX := x + cardPadX
	innerY := y + cardPadY
	innerW := w - 2*cardPadX
	l.add(v.tr("cap.trend"), innerX, innerY, 11, Label, p.Muted)

	series := v.Snap.SeriesFor("requests")
	if len(series) < 2 {
		l.add(v.tr("chart.empty"), innerX, innerY+40, 12, Body, p.Muted)
		return
	}
	top := innerY + 26
	height := h - cardPadY - 34
	base := top + height
	peak := 1.0
	for _, s := range series {
		if s > peak {
			peak = s
		}
	}
	// The line joins the samples as they are, with no smoothing, and that is a decision
	// rather than an omission. Averaging each point against its neighbours was tried and
	// removed: with a dozen samples it flattens the peaks out of existence — a spike of
	// four hundred requests became a gentle rise — and a curve that hides the one event
	// worth looking at is worse than a jagged one. What the shape is for is comparing
	// this hour with the last few, and that comparison needs the values, not a trend line
	// drawn through them.
	//
	// The area, as a column per pixel, each column's height interpolated between the
	// samples: that is what makes a dozen points fill a thousand-pixel panel without
	// either stepping or a scanline filler.
	at := func(t float64) float64 {
		pos := t * float64(len(series)-1)
		i := int(pos)
		if i >= len(series)-1 {
			return series[len(series)-1]
		}
		f := pos - float64(i)
		return series[i]*(1-f) + series[i+1]*f
	}
	fill := p.Blue.Mul(0.18)
	width := int(innerW)
	for px := 0; px < width; px++ {
		py := int(base - height*at(float64(px)/float64(width-1))/peak)
		for yy := py; yy < int(base); yy++ {
			l.Ink.BlendAt(int(innerX)+px, yy, fill)
		}
	}
	pts := make([]raster.Pt, len(series))
	for i, s := range series {
		pts[i] = raster.Pt{
			X: innerX + innerW*float64(i)/float64(len(series)-1),
			Y: base - height*s/peak,
		}
	}
	l.Ink.Line(pts, 2.0, p.Blue)
	last := pts[len(pts)-1]
	l.Ink.Circle(last.X, last.Y, 3.5, p.Blue)
	l.add(compact(peak), innerX, innerY+16, 10.5, Label, p.Muted)
	l.addRight(v.tr("ui.last_hours", len(series)), innerX+innerW, innerY+16, 10.5, Label, p.Muted)
}

// activityCard is the newest of the gateway's own log lines, under the trend.
//
// The two belong together and this is why they are one page: a shape that says how
// much is happening and a list that says what is happening answer the two halves of
// one question, and an operator who sees a spike in the first wants the second
// immediately. The lines are the gateway's own ring, already read for the logs page.
func activityCard(l *Layout, v View, x, y, w, h float64) {
	p := v.Palette
	cardPanel(&l.Ink, p, x, y, w, h)
	innerX := x + cardPadX
	innerW := w - 2*cardPadX
	cur := y + cardPadY

	l.add(v.tr("cap.activity"), innerX, cur, 11, Label, p.Muted)
	if len(v.Logs) > 0 {
		// A way through to the full ring, which is where the rest of it is.
		More := button{Label: v.tr("btn.all_logs"), Action: ActionTab, Arg: string(TabLogs)}
		bw := textWidth(More.Label, 11) + 18
		bx := innerX + innerW - bw
		l.Ink.RoundedRect(bx, cur-4, bw, 20, 5, p.Raised)
		l.addCentre(More.Label, bx+bw/2, cur+1, 11, Body, p.Text)
		l.hit(bx, cur-4, bw, 20, More.Action, More.Arg)
	}
	cur += 22

	if len(v.Logs) == 0 {
		l.add(v.tr("empty.no_logs"), innerX, cur, 11.5, Body, p.Muted)
		return
	}
	// The newest first, the way the logs page reads, and only what fits: this is a
	// summary of the ring rather than a second copy of it.
	logs := make([]panel.LogEntry, len(v.Logs))
	copy(logs, v.Logs)
	for i, j := 0, len(logs)-1; i < j; i, j = i+1, j-1 {
		logs[i], logs[j] = logs[j], logs[i]
	}
	lineH := 18.0
	room := int((y + h - cardPadY - cur) / lineH)
	shown := len(logs)
	if shown > room {
		shown = room
	}
	for i := 0; i < shown; i++ {
		e := logs[i]
		rowY := cur + float64(i)*lineH
		col := p.Muted
		switch e.Ch {
		case "task":
			col = p.Green
		case "sys":
			col = p.Warn
		case "chat":
			col = p.Text
		}
		when := e.TS
		if len(when) >= 19 {
			when = when[11:19]
		}
		l.add(when, innerX, rowY+1, 10.5, Label, p.Faint)
		l.add(clip(e.Text, 118), innerX+56, rowY+1, 11, Body, col)
	}
}

// accountsPage is the pool, with the per-account actions the console offers.
func accountsPage(l *Layout, v View) {
	p, snap := v.Palette, v.Snap
	headerLine(l, v, v.tr("page.accounts"), []button{
		{Label: v.tr("btn.add_account"), Action: ActionOpenPanel},
		{Label: v.tr("btn.checkin_all"), Action: ActionCheckin},
		{Label: v.tr("btn.balance_all"), Action: ActionBalance, Primary: true},
	})

	x, y, w, h := contentBox(l, v)
	cardPanel(&l.Ink, p, x, y, w, h)
	innerX := x + cardPadX
	innerW := w - 2*cardPadX
	cur := y + cardPadY

	if len(snap.Accounts) == 0 {
		l.add(v.tr("empty.no_accounts"), innerX, cur+10, 12.5, Body, p.Muted)
		return
	}

	// The column offsets, as fractions of the table's width, so the columns stay
	// aligned to each other rather than to a pixel count that assumes a width.
	cols := []struct {
		label string
		at    float64
		right bool
	}{
		{v.tr("col.account"), 0, false}, {v.tr("col.state"), 0.22, false},
		{v.tr("col.credits"), 0.34, true}, {v.tr("col.requests"), 0.52, true},
		{v.tr("col.latency"), 0.64, true}, {v.tr("col.tps"), 0.73, true},
		{v.tr("col.last_used"), 0.82, true}, {v.tr("col.actions"), 1.0, true},
	}
	for _, c := range cols {
		cx := innerX + innerW*c.at
		if c.right {
			l.addRight(c.label, cx, cur, 10.5, Label, p.Faint)
		} else {
			l.add(c.label, cx, cur, 10.5, Label, p.Faint)
		}
	}
	cur += 20
	l.Ink.Rect(innerX, cur, innerW, 1, hairline(p))
	cur += 6

	room := int((y + h - cardPadY - cur) / tableRowH)
	first := v.ScrollAccounts
	if first > len(snap.Accounts) {
		first = 0
	}
	visible := snap.Accounts[first:]
	shown := len(visible)
	if shown > room {
		shown = room
	}
	for i := 0; i < shown; i++ {
		a := visible[i]
		rowY := cur + float64(i)*tableRowH
		if i%2 == 1 {
			l.Ink.Rect(innerX, rowY, innerW, tableRowH, p.Raised.Mul(0.30))
		}
		name := a.Nickname
		if name == "" {
			name = a.UID
		}
		l.add(clip(name, 16), innerX, rowY+8, 12.5, Body, p.Text)

		// The state, coloured: it is read before the numbers, because a disabled
		// account's credits are not available whatever the figure says.
		state, sc := v.tr("acct.ready"), p.Green
		switch {
		case a.Disabled:
			state, sc = v.tr("acct.disabled"), p.Muted
		case a.Cooling:
			state, sc = v.tr("acct.cooling"), p.Warn
			if left := a.CoolRemaining(); left > 0 {
				state = v.tr("acct.cooling_left", duration(left))
			}
		case a.InFlight > 0:
			state, sc = v.tr("acct.inflight", a.InFlight), p.Blue
		}
		l.add(state, innerX+innerW*0.22, rowY+8, 12, Body, sc)

		credits := comma(a.Credits)
		if a.Total > 0 {
			credits += " /" + comma(a.Total)
		}
		l.addRight(credits, innerX+innerW*0.34, rowY+8, 12, Body, p.Text)
		l.addRight(fmt.Sprintf("%s / %s", compact(float64(a.Requests)), compact(float64(a.ErrTotal))),
			innerX+innerW*0.52, rowY+8, 12, Body, p.Text)
		if a.LastLatencyMs > 0 {
			l.addRight(oneDecimal(a.LastLatencyMs/1000)+"s", innerX+innerW*0.64, rowY+8, 12, Body, p.Text)
		} else {
			l.addRight("—", innerX+innerW*0.64, rowY+8, 12, Body, p.Faint)
		}
		if a.LastTPS > 0 {
			l.addRight(fmt.Sprintf("%.0f", a.LastTPS), innerX+innerW*0.73, rowY+8, 12, Body, p.Text)
		} else {
			l.addRight("—", innerX+innerW*0.73, rowY+8, 12, Body, p.Faint)
		}
		l.addRight(since(a.LastUsed, v.Lang), innerX+innerW*0.82, rowY+9, 11, Label, p.Faint)

		// The per-account actions, as small buttons. They are the console's own
		// per-row actions, with one difference: the console confirms an account
		// action with a dialog, and here the button acts and the footer says what
		// happened, because a window that cannot put up a dialog should not pretend
		// to.
		btns := []struct {
			label  string
			action Action
		}{
			{v.tr("btn.acct_checkin"), ActionAccountCheckin},
			{v.tr("btn.acct_balance"), ActionAccountBalance},
		}
		if a.Disabled {
			btns = append(btns, struct {
				label  string
				action Action
			}{v.tr("btn.acct_revive"), ActionAccountRevive})
		}
		btnRight := innerX + innerW
		for _, b := range btns {
			bw := textWidth(b.label, 10.5) + 14
			bx := btnRight - bw
			l.Ink.RoundedRect(bx, rowY+5, bw, 21, 5, p.RaisedHi)
			l.addCentre(b.label, bx+bw/2, rowY+10, 10.5, Body, p.Text)
			l.hit(bx, rowY+5, bw, 21, b.action, a.UID)
			btnRight = bx - 4
		}
	}
	if shown < len(snap.Accounts) {
		l.add(v.tr("ui.more_rows", first+shown, len(snap.Accounts)),
			innerX, cur+float64(shown)*tableRowH+6, 11, Label, p.Muted)
	}
}

// modelsPage is the catalogue: what the gateway can serve, and what each costs.
func modelsPage(l *Layout, v View) {
	p := v.Palette
	headerLine(l, v, v.tr("page.models"), []button{
		{Label: v.tr("btn.open_panel"), Action: ActionOpenPanel},
		{Label: v.tr("btn.refresh"), Action: ActionRefresh, Primary: true},
	})

	x, y, w, h := contentBox(l, v)
	cardPanel(&l.Ink, p, x, y, w, h)
	innerX := x + cardPadX
	innerW := w - 2*cardPadX
	cur := y + cardPadY

	if len(v.Models) == 0 {
		l.add(v.tr("empty.no_models"), innerX, cur+10, 12.5, Body, p.Muted)
		return
	}

	cols := []struct {
		label string
		at    float64
		right bool
	}{
		{v.tr("col.model"), 0, false}, {v.tr("col.price"), 0.26, true},
		{v.tr("col.context"), 0.38, true}, {v.tr("col.max_output"), 0.51, true},
		{v.tr("col.effort"), 0.64, false}, {v.tr("col.caps"), 0.80, false},
	}
	for _, c := range cols {
		cx := innerX + innerW*c.at
		if c.right {
			l.addRight(c.label, cx, cur, 10.5, Label, p.Faint)
		} else {
			l.add(c.label, cx, cur, 10.5, Label, p.Faint)
		}
	}
	cur += 20
	l.Ink.Rect(innerX, cur, innerW, 1, hairline(p))
	cur += 6

	// The catalogue is long — thirty-odd models — and the window has no scrollbar,
	// so it shows what fits and says how many it is not showing.
	models := SortedModels(v.Models)
	first := v.ScrollModels
	if first > len(models) {
		first = 0
	}
	models = models[first:]
	room := int((y + h - cardPadY - cur) / tableRowH)
	shown := len(models)
	if shown > room {
		shown = room
	}
	for i := 0; i < shown; i++ {
		m := models[i]
		rowY := cur + float64(i)*tableRowH
		if i%2 == 1 {
			l.Ink.Rect(innerX, rowY, innerW, tableRowH, p.Raised.Mul(0.35))
		}
		name := m.Name
		if name == "" {
			name = m.ID
		}
		l.add(clip(name, 20), innerX, rowY+8, 12.5, Body, p.Text)
		l.addRight(m.Credits, innerX+innerW*0.26, rowY+8, 12, Body, p.Text)
		l.addRight(contextSize(m.ContextLength), innerX+innerW*0.38, rowY+9, 11.5, Label, p.Muted)
		l.addRight(contextSize(m.MaxOutputTokens), innerX+innerW*0.51, rowY+9, 11.5, Label, p.Muted)

		effort := m.DefaultEffort
		if effort == "" {
			effort = "—"
		} else if len(m.SupportedEfforts) > 0 {
			effort = strings.Join(m.SupportedEfforts, "/")
		}
		l.add(effort, innerX+innerW*0.64, rowY+9, 11, Label, p.Muted)

		var caps []string
		if m.SupportsImages {
			caps = append(caps, v.tr("cap.img"))
		}
		if m.SupportsReasoning {
			caps = append(caps, v.tr("cap.reasoning"))
		}
		if m.SupportsToolCall {
			caps = append(caps, v.tr("cap.tools"))
		}
		l.add(strings.Join(caps, " · "), innerX+innerW*0.80, rowY+9, 11, Label, p.Muted)
	}
	if shown < len(v.Models) {
		l.add(v.tr("ui.more_rows", first+shown, len(v.Models)),
			innerX, cur+float64(shown)*tableRowH+6, 11, Label, p.Muted)
	}
}

// logsPage is the gateway's own log ring.
func logsPage(l *Layout, v View) {
	p := v.Palette
	headerLine(l, v, v.tr("page.logs"), []button{
		{Label: v.tr("btn.refresh"), Action: ActionRefresh, Primary: true},
	})

	x, y, w, h := contentBox(l, v)
	cardPanel(&l.Ink, p, x, y, w, h)
	innerX := x + cardPadX
	cur := y + cardPadY

	if len(v.Logs) == 0 {
		l.add(v.tr("empty.no_logs"), innerX, cur+10, 12.5, Body, p.Muted)
		return
	}
	// The newest line first, which is how a log is read when something has just
	// gone wrong — and the reason the ring's own oldest-first order is reversed
	// here rather than at the source.
	logs := make([]panel.LogEntry, len(v.Logs))
	copy(logs, v.Logs)
	for i, j := 0, len(logs)-1; i < j; i, j = i+1, j-1 {
		logs[i], logs[j] = logs[j], logs[i]
	}
	first := v.ScrollLogs
	if first > len(logs) {
		first = 0
	}
	logs = logs[first:]

	lineH := 20.0
	room := int((y + h - cardPadY - cur) / lineH)
	shown := len(logs)
	if shown > room {
		shown = room
	}
	for i := 0; i < shown; i++ {
		e := logs[i]
		rowY := cur + float64(i)*lineH
		col := p.Muted
		switch e.Ch {
		case "task":
			col = p.Green
		case "sys":
			col = p.Warn
		case "chat":
			col = p.Text
		}
		when := e.TS
		if len(when) >= 19 {
			when = when[11:19]
		}
		l.add(when, innerX, rowY+2, 11, Label, p.Faint)
		l.add(clip(e.Text, 150), innerX+62, rowY+2, 11.5, Body, col)
	}
	if shown < len(logs)+first {
		l.add(v.tr("ui.more_lines", first+shown, first+len(logs)),
			innerX, cur+float64(shown)*lineH+6, 11, Label, p.Muted)
	}
}

// schedulePage is what the gateway runs on a schedule.
//
// It is read-only, and it says so. The switches live in the gateway's own
// configuration, which the console edits; a second editor here would be a second
// place for the same file to be written from, and the one that lost the race would
// be silently overwritten.
func schedulePage(l *Layout, v View) {
	p, s := v.Palette, v.Schedule
	headerLine(l, v, v.tr("page.schedule"), []button{
		{Label: v.tr("btn.edit_in_console"), Action: ActionOpenPanel},
		{Label: v.tr("btn.refresh"), Action: ActionRefresh, Primary: true},
	})

	x, y, w, h := contentBox(l, v)
	cardPanel(&l.Ink, p, x, y, w, h)
	innerX := x + cardPadX
	innerW := w - 2*cardPadX
	cur := y + cardPadY

	if !s.Known {
		l.add(v.tr("empty.no_config"), innerX, cur+10, 12.5, Body, p.Muted)
		return
	}
	l.add(v.tr("cap.schedule"), innerX, cur, 11, Label, p.Muted)
	cur += 22

	rows := []struct {
		label string
		on    bool
		when  string
	}{
		{v.tr("sched.checkin"), s.Checkin, "09:00 · 21:00"},
		{v.tr("sched.travel"), s.Travel, "09:00 · 21:00"},
		{v.tr("sched.activity"), s.Activity, "10:00"},
		{v.tr("sched.keepalive"), s.Keepalive, "22:00"},
		{v.tr("sched.balance"), s.BalanceRefresh, v.tr("sched.every5min")},
	}
	for i, r := range rows {
		rowY := cur + float64(i)*tableRowH
		if i%2 == 1 {
			l.Ink.Rect(innerX, rowY, innerW, tableRowH, p.Raised.Mul(0.35))
		}
		l.add(r.label, innerX, rowY+8, 12.5, Body, p.Text)
		l.add(r.when, innerX+innerW*0.30, rowY+9, 11.5, Label, p.Muted)
		state, col := v.tr("sched.on"), p.Green
		if !r.on {
			state, col = v.tr("sched.off"), p.Muted
		}
		l.addRight(state, innerX+innerW, rowY+8, 12, Body, col)
	}
	cur += float64(len(rows))*tableRowH + 14

	l.add(v.tr("cap.notes"), innerX, cur, 11, Label, p.Muted)
	cur += 20
	for _, line := range []string{
		v.tr("note.schedule_1"),
		v.tr("note.schedule_2"),
		v.tr("note.schedule_3"),
	} {
		if cur > y+h-cardPadY-16 {
			break
		}
		l.add(line, innerX, cur, 11.5, Body, p.Muted)
		cur += 18
	}
}

// configPage shows the gateway's settings.
//
// It shows them rather than editing them, for the same reason the schedule is
// read-only, and it shows the settings an operator looks for rather than the whole
// file: the console is the editor, and a window that dumped four kilobytes of JSON
// would be a file viewer rather than a console.
func configPage(l *Layout, v View) {
	p := v.Palette
	headerLine(l, v, v.tr("page.config"), []button{
		{Label: v.tr("btn.copy_key"), Action: ActionCopyKey},
		{Label: v.tr("btn.copy_addr"), Action: ActionCopyAddress},
		{Label: v.tr("btn.open_gateway_dir"), Action: ActionOpenGatewayDir},
		{Label: v.tr("btn.edit_in_console"), Action: ActionOpenPanel, Primary: true},
	})

	x, y, w, h := contentBox(l, v)
	cardPanel(&l.Ink, p, x, y, w, h)
	innerX := x + cardPadX
	innerW := w - 2*cardPadX
	cur := y + cardPadY

	if v.Config == "" {
		l.add(v.tr("empty.no_config"), innerX, cur+10, 12.5, Body, p.Muted)
		return
	}
	l.add(v.tr("cap.gateway_config"), innerX, cur, 11, Label, p.Muted)
	if v.ConfigPath != "" {
		l.addRight(v.ConfigPath, innerX+innerW, cur, 11, Label, p.Faint)
	}
	cur += 22

	for _, f := range configFields(v.Config, v.Lang) {
		if cur > y+h-cardPadY-16 {
			l.add(v.tr("ui.more_settings"), innerX, cur, 11, Label, p.Faint)
			break
		}
		l.add(f.label, innerX, cur, 12, Body, p.Text)
		l.add(clip(f.value, 70), innerX+innerW*0.28, cur, 11.5, Body, p.Muted)
		cur += 20
	}
}

// configField is one line of the settings page.
type configField struct{ label, value string }

// configFields picks the settings worth showing, by reading them out of the JSON
// the gateway sent.
//
// It is parsed rather than pattern-matched: the gateway's configuration is nested a
// long way down — the schedule is four levels in — and a regular expression over the
// raw text would report the first "enabled" it found for every question that has one.
func configFields(raw, lang string) []configField {
	var c struct {
		Listen  string `json:"listen"`
		AuthDir string `json:"auth_dir"`
		APIKey  string `json:"api_key"`
		Global  struct {
			Enabled bool `json:"enabled"`
		} `json:"global"`
		Pool struct {
			MaxInFlight       int    `json:"max_in_flight"`
			MaxInFlightGlobal int    `json:"max_in_flight_global"`
			BreakerThreshold  int    `json:"breaker_threshold"`
			BreakerCooldown   string `json:"breaker_cooldown"`
			ExpiringSoon      string `json:"expiring_soon"`
		} `json:"pool"`
		Sticky struct {
			Enabled bool   `json:"enabled"`
			TTL     string `json:"ttl"`
		} `json:"session_sticky"`
		Upstream struct {
			TimeoutSeconds       int `json:"timeout_seconds"`
			HeaderTimeoutSeconds int `json:"header_timeout_seconds"`
		} `json:"upstream"`
	}
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		// Unparseable: show it as it came rather than showing nothing, because the
		// text is still the answer to "what is in there".
		return []configField{{i18n.T(lang, "cfg.raw"), clip(raw, 90)}}
	}
	on := func(b bool) string {
		if b {
			return i18n.T(lang, "cfg.on")
		}
		return i18n.T(lang, "cfg.off")
	}
	secret := i18n.T(lang, "cfg.unset")
	if c.APIKey != "" {
		secret = i18n.T(lang, "cfg.set_length", len([]rune(c.APIKey)))
	}
	return []configField{
		{i18n.T(lang, "cfg.listen"), c.Listen},
		{i18n.T(lang, "cfg.api_key"), secret},
		{i18n.T(lang, "cfg.global"), on(c.Global.Enabled)},
		{i18n.T(lang, "cfg.auth_dir"), c.AuthDir},
		{i18n.T(lang, "cfg.max_inflight"), fmt.Sprintf("%d", c.Pool.MaxInFlight)},
		{i18n.T(lang, "cfg.max_inflight_global"), fmt.Sprintf("%d", c.Pool.MaxInFlightGlobal)},
		{i18n.T(lang, "cfg.breaker_threshold"), i18n.T(lang, "cfg.times", c.Pool.BreakerThreshold)},
		{i18n.T(lang, "cfg.breaker_cooldown"), c.Pool.BreakerCooldown},
		{i18n.T(lang, "cfg.expiring_soon"), c.Pool.ExpiringSoon},
		{i18n.T(lang, "cfg.sticky"), on(c.Sticky.Enabled) + " · " + c.Sticky.TTL},
		{i18n.T(lang, "cfg.upstream_timeout"), i18n.T(lang, "cfg.seconds", c.Upstream.TimeoutSeconds)},
		{i18n.T(lang, "cfg.header_timeout"), i18n.T(lang, "cfg.seconds", c.Upstream.HeaderTimeoutSeconds)},
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

// contextSize renders a token count the way a model catalogue states one.
func contextSize(n int64) string {
	switch {
	case n <= 0:
		return "—"
	case n >= 1_000_000:
		return fmt.Sprintf("%.0fM", float64(n)/1e6)
	case n >= 1000:
		return fmt.Sprintf("%dk", n/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func oneDecimal(v float64) string { return fmt.Sprintf("%.1f", v) }

// uptime renders a process lifetime the way a header wants it.
func uptime(sec int64, lang string) string {
	switch {
	case sec < 60:
		return i18n.T(lang, "unit.sec", sec)
	case sec < 3600:
		return i18n.T(lang, "unit.min", sec/60)
	case sec < 86400:
		return i18n.T(lang, "unit.hour_min", sec/3600, (sec%3600)/60)
	default:
		return i18n.T(lang, "unit.day_hour", sec/86400, (sec%86400)/3600)
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
// account table whose meaning is a duration rather than a count.
func since(t time.Time, lang string) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return i18n.T(lang, "unit.just_now")
	case d < time.Hour:
		return i18n.T(lang, "unit.min_ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return i18n.T(lang, "unit.hour_ago", int(d.Hours()))
	default:
		return i18n.T(lang, "unit.day_ago", int(d.Hours()/24))
	}
}

// clip truncates a label to the column it has to fit, so one long nickname does not
// run into the column beside it.
func clip(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return string(r[:max])
	}
	return string(r[:max-1]) + "…"
}

// textWidth estimates how much room a string needs.
//
// It is an estimate and it has to be: the real width depends on the operator's font
// and its size, and the layout runs before GDI has been asked. Two characters per
// CJK rune and a little over one per Latin one is close enough to place a button,
// and the front end measures the string properly before it draws it.
func textWidth(s string, size float64) float64 {
	var units float64
	for _, r := range s {
		if r > 0x2e80 {
			units += 2
		} else {
			units += 1.1
		}
	}
	return units * size * 0.5
}

// orDash renders an unknown value, because a blank column reads as a rendering
// fault where a dash reads as "not known".
func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// SortedModels returns the catalogue in the order the page shows it: the default
// first, then by cost, because the question a catalogue answers is "which should I
// use", and cost is what decides it.
func SortedModels(models []panel.Model) []panel.Model {
	out := make([]panel.Model, len(models))
	copy(out, models)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IsDefault != out[j].IsDefault {
			return out[i].IsDefault
		}
		return out[i].Credits < out[j].Credits
	})
	return out
}
