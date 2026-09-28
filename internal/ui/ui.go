package ui

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

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
	zh := lang == "zh"
	switch t {
	case TabOverview:
		if zh {
			return "总览"
		}
		return "Overview"
	case TabAccounts:
		if zh {
			return "账号池"
		}
		return "Accounts"
	case TabModels:
		if zh {
			return "模型"
		}
		return "Models"
	case TabLogs:
		if zh {
			return "日志"
		}
		return "Logs"
	case TabSchedule:
		if zh {
			return "调度"
		}
		return "Schedule"
	default:
		if zh {
			return "配置"
		}
		return "Config"
	}
}

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
	// It is clipped to the rail's width because the rail is 170 pixels and the name
	// is longer than that in any font: an earlier version drew it whole, and it ran
	// under the page title on the other side of the divider.
	l.add(clip("WorkBuddy2API", 12), 16, 16, 13.5, Figure, p.Text)
	l.add(orDash(v.Snap.Version), 16, 36, 11, Label, p.Faint)

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
	dot, label := p.Bad, "离线"
	if v.Snap.Reachable {
		dot, label = p.Green, "正常"
	}
	if v.Paused {
		dot, label = p.Muted, "已暂停"
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
		l.add("运行 "+uptime(v.Snap.Uptime),
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
		msg = "正在执行…"
		col = p.Blue
	}
	if msg == "" {
		msg = fmt.Sprintf("账号 %d/%d 可用 · 积分 %s · 在途 %d",
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
	headerLine(l, v, "总览", []button{
		{Label: "打开控制台", Action: ActionOpenPanel},
		{Label: "刷新", Action: ActionRefresh, Primary: true},
	})

	x, y, w, _ := contentBox(l, v)

	n := 4.0
	cw := (w - gap*(n-1)) / n
	figures := []struct {
		label, value, note string
		accent             raster.RGBA
	}{
		{"积分剩余", comma(snap.CreditTotal()), "", p.Blue},
		{"账号可用", fmt.Sprintf("%d/%d", snap.Ready(), snap.Total), "", p.Green},
		{"24 小时请求", compact(float64(snap.Usage.Requests)),
			fmt.Sprintf("%d 失败", snap.Usage.Errors), p.Warn},
		{"Token 合计", compact(float64(snap.Usage.TotalTokens)), "", p.Text},
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
		l.add("账号状态", x, y, 11, Label, p.Muted)
		px := x + textWidth("账号状态", 11) + 12
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
	chartH := float64(l.H) - FooterH - 8 - y - gap - actionsH - 24
	if chartH < 80 {
		chartH = 80
	}
	chartCard(l, v, x, y, w, chartH)
	y += chartH + gap

	l.add("维护操作", x, y, 11, Label, p.Muted)
	y += 18
	acts := []button{
		{Label: "全部签到", Action: ActionCheckin},
		{Label: "旅行巡检", Action: ActionTravel},
		{Label: "活跃上报", Action: ActionActivity},
		{Label: "全部保活", Action: ActionKeepalive},
		{Label: "刷新余额", Action: ActionBalance},
		{Label: "扫描待办", Action: ActionScan},
		{Label: "执行待办", Action: ActionRunQueue},
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
func chartCard(l *Layout, v View, x, y, w, h float64) {
	p := v.Palette
	cardPanel(&l.Ink, p, x, y, w, h)

	innerX := x + cardPadX
	innerY := y + cardPadY
	innerW := w - 2*cardPadX
	l.add("请求趋势", innerX, innerY, 11, Label, p.Muted)

	series := v.Snap.SeriesFor("requests")
	if len(series) < 2 {
		l.add("暂无数据（网关未运行或尚未有请求）", innerX, innerY+40, 12, Body, p.Muted)
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
	// The area, as a column per pixel, each column's height interpolated between
	// the samples: that is what makes a dozen points fill a thousand-pixel panel
	// without either stepping or a scanline filler.
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
	l.addRight(fmt.Sprintf("过去 %d 小时", len(series)), innerX+innerW, innerY+16, 10.5, Label, p.Muted)
}

// accountsPage is the pool, with the per-account actions the console offers.
func accountsPage(l *Layout, v View) {
	p, snap := v.Palette, v.Snap
	headerLine(l, v, "账号池", []button{
		{Label: "添加账号", Action: ActionOpenPanel},
		{Label: "全部签到", Action: ActionCheckin},
		{Label: "刷新余额", Action: ActionBalance, Primary: true},
	})

	x, y, w, h := contentBox(l, v)
	cardPanel(&l.Ink, p, x, y, w, h)
	innerX := x + cardPadX
	innerW := w - 2*cardPadX
	cur := y + cardPadY

	if len(snap.Accounts) == 0 {
		l.add("网关还没有账号。用「添加账号」在控制台里登录一个。", innerX, cur+10, 12.5, Body, p.Muted)
		return
	}

	// The column offsets, as fractions of the table's width, so the columns stay
	// aligned to each other rather than to a pixel count that assumes a width.
	cols := []struct {
		label string
		at    float64
		right bool
	}{
		{"账号", 0, false}, {"状态", 0.22, false}, {"积分", 0.34, true},
		{"请求 / 失败", 0.52, true}, {"延迟", 0.64, true},
		{"吐字", 0.73, true}, {"最近使用", 0.82, true}, {"操作", 1.0, true},
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
		state, sc := "可用", p.Green
		switch {
		case a.Disabled:
			state, sc = "已禁用", p.Muted
		case a.Cooling:
			state, sc = "冷却中", p.Warn
			if left := a.CoolRemaining(); left > 0 {
				state = "冷却 " + duration(left)
			}
		case a.InFlight > 0:
			state, sc = fmt.Sprintf("在途 %d", a.InFlight), p.Blue
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
		l.addRight(since(a.LastUsed), innerX+innerW*0.82, rowY+9, 11, Label, p.Faint)

		// The per-account actions, as small buttons. They are the console's own
		// per-row actions, with one difference: the console confirms an account
		// action with a dialog, and here the button acts and the footer says what
		// happened, because a window that cannot put up a dialog should not pretend
		// to.
		btns := []struct {
			label  string
			action Action
		}{
			{"签到", ActionAccountCheckin},
			{"余额", ActionAccountBalance},
		}
		if a.Disabled {
			btns = append(btns, struct {
				label  string
				action Action
			}{"恢复", ActionAccountRevive})
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
		l.add(fmt.Sprintf("… 已显示 %d/%d 个；滚轮可滚动", first+shown, len(snap.Accounts)),
			innerX, cur+float64(shown)*tableRowH+6, 11, Label, p.Muted)
	}
}

// modelsPage is the catalogue: what the gateway can serve, and what each costs.
func modelsPage(l *Layout, v View) {
	p := v.Palette
	headerLine(l, v, "模型", []button{
		{Label: "打开控制台", Action: ActionOpenPanel},
		{Label: "刷新", Action: ActionRefresh, Primary: true},
	})

	x, y, w, h := contentBox(l, v)
	cardPanel(&l.Ink, p, x, y, w, h)
	innerX := x + cardPadX
	innerW := w - 2*cardPadX
	cur := y + cardPadY

	if len(v.Models) == 0 {
		l.add("暂无模型数据。网关需要至少一个可用账号才能查询模型目录。", innerX, cur+10, 12.5, Body, p.Muted)
		return
	}

	cols := []struct {
		label string
		at    float64
		right bool
	}{
		{"模型", 0, false}, {"计价", 0.26, true}, {"上下文", 0.38, true},
		{"最大输出", 0.51, true}, {"档位", 0.64, false}, {"能力", 0.80, false},
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
			caps = append(caps, "图")
		}
		if m.SupportsReasoning {
			caps = append(caps, "推理")
		}
		if m.SupportsToolCall {
			caps = append(caps, "工具")
		}
		l.add(strings.Join(caps, " · "), innerX+innerW*0.80, rowY+9, 11, Label, p.Muted)
	}
	if shown < len(v.Models) {
		l.add(fmt.Sprintf("… 已显示 %d/%d 个；滚轮可滚动", first+shown, len(v.Models)),
			innerX, cur+float64(shown)*tableRowH+6, 11, Label, p.Muted)
	}
}

// logsPage is the gateway's own log ring.
func logsPage(l *Layout, v View) {
	p := v.Palette
	headerLine(l, v, "运行日志", []button{
		{Label: "刷新", Action: ActionRefresh, Primary: true},
	})

	x, y, w, h := contentBox(l, v)
	cardPanel(&l.Ink, p, x, y, w, h)
	innerX := x + cardPadX
	cur := y + cardPadY

	if len(v.Logs) == 0 {
		l.add("暂无日志。", innerX, cur+10, 12.5, Body, p.Muted)
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
		l.add(fmt.Sprintf("… 已显示 %d/%d 行；滚轮可滚动", first+shown, first+len(logs)),
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
	headerLine(l, v, "调度", []button{
		{Label: "在控制台编辑", Action: ActionOpenPanel},
		{Label: "刷新", Action: ActionRefresh, Primary: true},
	})

	x, y, w, h := contentBox(l, v)
	cardPanel(&l.Ink, p, x, y, w, h)
	innerX := x + cardPadX
	innerW := w - 2*cardPadX
	cur := y + cardPadY

	if !s.Known {
		l.add("尚未读取网关配置。点击「刷新」重新读取。", innerX, cur+10, 12.5, Body, p.Muted)
		return
	}
	l.add("定时任务", innerX, cur, 11, Label, p.Muted)
	cur += 22

	rows := []struct {
		label string
		on    bool
		when  string
	}{
		{"签到", s.Checkin, "09:00 · 21:00"},
		{"旅行", s.Travel, "09:00 · 21:00"},
		{"活跃上报", s.Activity, "10:00"},
		{"保活", s.Keepalive, "22:00"},
		{"余额刷新", s.BalanceRefresh, "每 5 分钟"},
	}
	for i, r := range rows {
		rowY := cur + float64(i)*tableRowH
		if i%2 == 1 {
			l.Ink.Rect(innerX, rowY, innerW, tableRowH, p.Raised.Mul(0.35))
		}
		l.add(r.label, innerX, rowY+8, 12.5, Body, p.Text)
		l.add(r.when, innerX+innerW*0.30, rowY+9, 11.5, Label, p.Muted)
		state, col := "已启用", p.Green
		if !r.on {
			state, col = "已关闭", p.Muted
		}
		l.addRight(state, innerX+innerW, rowY+8, 12, Body, col)
	}
	cur += float64(len(rows))*tableRowH + 14

	l.add("说明", innerX, cur, 11, Label, p.Muted)
	cur += 20
	for _, line := range []string{
		"任务的开关写在网关自己的 config.json 里，在控制台的配置页修改。",
		"这个窗口只读，因为两处可写同一个文件时，后写的一方会静默覆盖前一方。",
		"要立即执行一次，用总览页的维护操作。",
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
	headerLine(l, v, "配置", []button{
		{Label: "复制密钥", Action: ActionCopyKey},
		{Label: "复制地址", Action: ActionCopyAddress},
		{Label: "打开网关目录", Action: ActionOpenGatewayDir},
		{Label: "在控制台编辑", Action: ActionOpenPanel, Primary: true},
	})

	x, y, w, h := contentBox(l, v)
	cardPanel(&l.Ink, p, x, y, w, h)
	innerX := x + cardPadX
	innerW := w - 2*cardPadX
	cur := y + cardPadY

	if v.Config == "" {
		l.add("尚未读取网关配置。点击「刷新」重新读取。", innerX, cur+10, 12.5, Body, p.Muted)
		return
	}
	l.add("网关配置（只读）", innerX, cur, 11, Label, p.Muted)
	if v.ConfigPath != "" {
		l.addRight(v.ConfigPath, innerX+innerW, cur, 11, Label, p.Faint)
	}
	cur += 22

	for _, f := range configFields(v.Config) {
		if cur > y+h-cardPadY-16 {
			l.add("… 其余设置见控制台", innerX, cur, 11, Label, p.Faint)
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
func configFields(raw string) []configField {
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
		return []configField{{"原始内容", clip(raw, 90)}}
	}
	on := func(b bool) string {
		if b {
			return "已启用"
		}
		return "已关闭"
	}
	secret := "未设置"
	if c.APIKey != "" {
		secret = fmt.Sprintf("已设置（%d 位）", len([]rune(c.APIKey)))
	}
	return []configField{
		{"监听地址", c.Listen},
		{"API 密钥", secret},
		{"服务总开关", on(c.Global.Enabled)},
		{"凭证目录", c.AuthDir},
		{"单账号并发", fmt.Sprintf("%d", c.Pool.MaxInFlight)},
		{"全局并发", fmt.Sprintf("%d", c.Pool.MaxInFlightGlobal)},
		{"熔断阈值", fmt.Sprintf("%d 次", c.Pool.BreakerThreshold)},
		{"熔断冷却", c.Pool.BreakerCooldown},
		{"即将过期窗口", c.Pool.ExpiringSoon},
		{"粘性会话", on(c.Sticky.Enabled) + " · " + c.Sticky.TTL},
		{"上游超时", fmt.Sprintf("%d 秒", c.Upstream.TimeoutSeconds)},
		{"首字节超时", fmt.Sprintf("%d 秒", c.Upstream.HeaderTimeoutSeconds)},
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
func uptime(sec int64) string {
	switch {
	case sec < 60:
		return fmt.Sprintf("%d 秒", sec)
	case sec < 3600:
		return fmt.Sprintf("%d 分钟", sec/60)
	case sec < 86400:
		return fmt.Sprintf("%d 小时 %d 分", sec/3600, (sec%3600)/60)
	default:
		return fmt.Sprintf("%d 天 %d 小时", sec/86400, (sec%86400)/3600)
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
