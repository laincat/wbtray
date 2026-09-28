package ui

import (
	"fmt"

	"wbtray/internal/raster"
	"wbtray/internal/status"
	"wbtray/internal/theme"
)

// The tray panel: the compact card that opens from the notification area.
//
// It is a drawn panel rather than the shell's own menu, and that is the whole point
// of it. A native menu can list verbs; it cannot draw a switch, a sparkline or a row
// of coloured figures, and those are what let a tray panel answer a question instead
// of offering a command. The trade is the one a menu never makes: it has to be drawn,
// positioned, hit-tested and dismissed by this program rather than by the shell.
//
// Its shape follows the dashboards this was modelled on: a hero line with the state
// and the reading, the trend as a shape, a row of figures, the switches, then the rows
// that lead into the console. Nothing here is a submenu — a row that would need one
// opens the console on the page that has it, because a menu nested inside a panel is
// still a menu, and not being one is the point.

// The panel's metrics, in logical pixels.
const (
	PanelW    = 344
	panelPadX = 14
	panelPadY = 12
	heroH     = 36
	sparkH    = 46
	metricH   = 42
	rowH      = 32
	sepGap    = 7
)

// TrayAction is what a click in the panel does.
//
// The set is deliberately the panel's own rather than the console's: a panel offering
// every verb the console does would be the console in a smaller box, and the reason
// to have both is that they are for different moments.
type TrayAction string

const (
	// TrayOpen opens the console on a page. The argument is the page's name.
	TrayOpen TrayAction = "open"
	// TrayTogglePause stops or resumes the tray's polling.
	TrayTogglePause TrayAction = "pause"
	// TrayToggleAuto flips the tray's own autostart entry.
	TrayToggleAuto TrayAction = "auto"
	// TrayQuit exits the tray.
	TrayQuit TrayAction = "quit"
)

// TrayHit is a clickable rectangle in the panel.
type TrayHit struct {
	X, Y, W, H float64
	Action     TrayAction
	Arg        string
}

// TrayView is everything one panel frame needs.
type TrayView struct {
	Snap    status.Snapshot
	Lang    string
	Palette theme.Palette

	Paused bool
	Auto   bool
	// Installed is whether a gateway is on disk, which the gateway row reports when
	// there is nothing running to report.
	Installed bool
}

// TrayLayout is one panel frame.
type TrayLayout struct {
	W, H  int
	Ink   Canvas
	Texts []Text
	Hits  []TrayHit

	// p is the palette in force, kept so the helpers do not each need it. It is a
	// field rather than a package variable because a layout is built once and read
	// many times, and a package variable would make two frames built in one process
	// disagree about their colours.
	p theme.Palette
}

// BuildTray lays out the panel.
//
// The height is computed rather than fixed: the rows are counted, because a fixed
// height would either clip the last row or leave a band of nothing under it.
func BuildTray(v TrayView) TrayLayout {
	p := v.Palette
	l := TrayLayout{p: p}

	const rows = 5 // console, gateway, accounts, models and logs, quit
	h := panelPadY + heroH + sparkH + 6 + metricH + sepGap +
		1 + 2*rowH + sepGap +
		1 + rows*rowH + panelPadY
	l.W, l.H = PanelW, h

	l.Ink = *raster.New(PanelW, h)
	// The surface and its hairline: the panel is a card, like every panel in the
	// console. It is the same design at a smaller size, which is what keeps the two
	// from looking like two programs.
	l.Ink.RoundedRect(0, 0, float64(PanelW), float64(h), 12, p.Surface)
	strokeRounded(&l.Ink, 0, 0, float64(PanelW), float64(h), 12, p.Border)

	x := float64(panelPadX)
	y := float64(panelPadY)
	w := float64(PanelW - 2*panelPadX)

	// The hero: the state, who is serving, and the reading.
	dot := p.Bad
	state := "离线"
	if v.Snap.Reachable {
		dot, state = p.Green, "正常"
	}
	if v.Paused {
		dot, state = p.Muted, "已暂停"
	}
	l.Ink.Circle(x+5, y+10, 5, dot)
	l.addRight(state, x+w, y, 11.5, Label, p.Muted)

	name := "尚未连接"
	if acct, ok := v.Snap.Current(); ok && v.Snap.Reachable {
		if acct.Nickname != "" {
			name = acct.Nickname
		} else {
			name = acct.UID
		}
	}
	l.add(clip(name, 16), x+16, y+1, 14, Figure, p.Text)
	if v.Snap.Reachable {
		l.add("积分剩余", x+16, y+20, 10.5, Label, p.Faint)
		l.addRight(comma(v.Snap.CreditTotal()), x+w, y+18, 13, Body, p.Text)
	}
	y += heroH

	// The trend. No axes, because the panel is opened beside a tray that already
	// shows the current figure: the question here is the shape.
	if series := v.Snap.SeriesFor("requests"); len(series) >= 2 {
		l.spark(x, y, w, sparkH, series)
	} else {
		l.add("暂无请求数据", x, y+sparkH/2-8, 11.5, Label, p.Muted)
	}
	y += sparkH + 6

	// The figures, three across, each in its own colour.
	third := w / 3
	l.metric(x, y, "请求", compact(float64(v.Snap.Usage.Requests)), p.Blue)
	l.metric(x+third, y, "延迟", oneDecimal(v.Snap.Usage.AvgLatencyMs/1000)+"s", p.Warn)
	l.metric(x+2*third, y, "吐字", fmt.Sprintf("%.0f", v.Snap.Usage.AvgTPS), p.Green)
	y += metricH + sepGap

	// The switches. These are the rows a native menu cannot have, and they are worth
	// the pixels: a state shown as a position is read without being read, where
	// "Pause" and "Resume" are one row with two words that have to be told apart.
	l.separator(x, y, w)
	y += sepGap
	l.boolean(x, y, w, "暂停刷新", v.Paused, TrayTogglePause)
	y += rowH
	l.boolean(x, y, w, "随系统启动", v.Auto, TrayToggleAuto)
	y += rowH + sepGap

	// The rows into the console.
	l.separator(x, y, w)
	y += sepGap
	l.row(x, y, w, "打开控制台", "", TrayOpen, string(TabOverview))
	y += rowH
	gwValue := v.Snap.Version
	if !v.Snap.Process.Found {
		gwValue = "未运行"
		if !v.Installed {
			gwValue = "未安装"
		}
	}
	l.row(x, y, w, "网关控制台", gwValue, TrayOpen, string(TabSchedule))
	y += rowH
	l.row(x, y, w, "账号管理", fmt.Sprintf("%d/%d", v.Snap.Ready(), v.Snap.Total),
		TrayOpen, string(TabAccounts))
	y += rowH
	l.row(x, y, w, "模型与日志", "", TrayOpen, string(TabModels))
	y += rowH
	l.row(x, y, w, "退出", "", TrayQuit, "")

	return l
}

func (l *TrayLayout) add(s string, x, y, size float64, w Weight, col raster.RGBA) {
	if s == "" {
		return
	}
	l.Texts = append(l.Texts, Text{S: s, X: x, Y: y, Size: size, Weight: w, Colour: col})
}

func (l *TrayLayout) addRight(s string, x, y, size float64, w Weight, col raster.RGBA) {
	if s == "" {
		return
	}
	l.Texts = append(l.Texts, Text{S: s, X: x, Y: y, Size: size, Weight: w, Colour: col, Align: Right})
}

func (l *TrayLayout) hit(x, y, w, h float64, a TrayAction, arg string) {
	l.Hits = append(l.Hits, TrayHit{X: x, Y: y, W: w, H: h, Action: a, Arg: arg})
}

func (l *TrayLayout) separator(x, y, w float64) {
	l.Ink.Rect(x, y, w, 1, l.p.BorderSoft)
}

// metric draws one figure with its caption and its colour rule.
func (l *TrayLayout) metric(x, y float64, label, value string, accent raster.RGBA) {
	l.Ink.RoundedRect(x, y+3, 3, 16, 1.5, accent)
	l.add(label, x+10, y+2, 10.5, Label, l.p.Muted)
	l.add(value, x+10, y+16, 15, Figure, l.p.Text)
}

// boolean draws a two-state row: the label, and the switch that shows its position.
func (l *TrayLayout) boolean(x, y, w float64, label string, on bool, action TrayAction) {
	l.add(label, x, y+7, 12.5, Body, l.p.Text)

	const swW, swH = 34.0, 18.0
	sx, sy := x+w-swW, y+5
	track := l.p.RaisedHi
	if on {
		track = l.p.Green
	}
	l.Ink.RoundedRect(sx, sy, swW, swH, swH/2, track)
	knob := swH - 4
	kx := sx + 2
	if on {
		kx = sx + swW - knob - 2
	}
	// The knob is the surface's colour rather than white, so it belongs to the panel
	// it sits on in either tone.
	l.Ink.Circle(kx+knob/2, sy+swH/2, knob/2, l.p.Surface)

	l.hit(x, y, w, rowH-2, action, "")
}

// row draws a row that leads somewhere.
func (l *TrayLayout) row(x, y, w float64, label, value string, action TrayAction, arg string) {
	// The chevron at the right marks a row that goes somewhere, which is what tells it
	// from the two switches above without a heading to explain the difference.
	chev := 4.5
	cx := x + w - chev - 1
	cy := y + rowH/2 - 2
	l.Ink.Line([]raster.Pt{
		{X: cx, Y: cy - chev}, {X: cx + chev, Y: cy}, {X: cx, Y: cy + chev},
	}, 1.4, l.p.Muted)

	l.add(label, x, y+7, 12.5, Body, l.p.Text)
	if value != "" {
		l.addRight(value, x+w-16, y+8, 11, Label, l.p.Muted)
	}
	l.hit(x, y, w, rowH-2, action, arg)
}

// spark draws the trend as an area under a line.
func (l *TrayLayout) spark(x, y, w, h float64, series []float64) {
	p := l.p
	peak := 1.0
	for _, s := range series {
		if s > peak {
			peak = s
		}
	}
	base, top := y+h-2, y+2
	interp := func(t float64) float64 {
		pos := t * float64(len(series)-1)
		i := int(pos)
		if i >= len(series)-1 {
			return series[len(series)-1]
		}
		f := pos - float64(i)
		return series[i]*(1-f) + series[i+1]*f
	}
	// The area as a column per pixel, each column's height interpolated between the
	// samples: that is what makes a dozen points fill three hundred pixels without
	// either stepping or a scanline filler.
	fill := p.Blue.Mul(0.18)
	n := int(w)
	for px := 0; px < n; px++ {
		py := int(base - (base-top)*interp(float64(px)/float64(n-1))/peak)
		for yy := py; yy < int(base); yy++ {
			l.Ink.BlendAt(int(x)+px, yy, fill)
		}
	}
	pts := make([]raster.Pt, len(series))
	for i, s := range series {
		pts[i] = raster.Pt{
			X: x + w*float64(i)/float64(len(series)-1),
			Y: base - (base-top)*s/peak,
		}
	}
	l.Ink.Line(pts, 1.6, p.Blue)
	last := pts[len(pts)-1]
	l.Ink.Circle(last.X, last.Y, 2.6, p.Blue)
}
