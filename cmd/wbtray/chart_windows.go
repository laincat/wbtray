//go:build windows

package main

import (
	"fmt"
	"math"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"wbtray/internal/app"
	"wbtray/internal/cat"
	"wbtray/internal/config"
	"wbtray/internal/i18n"
	"wbtray/internal/raster"
	"wbtray/internal/status"
	"wbtray/internal/theme"
	"wbtray/internal/textmask"
	"wbtray/internal/winapi"
)

// The chart window.
//
// The tray icon can only ever show one number, and a curve squeezed into
// sixteen pixels is a shape rather than a reading. This window is where the
// numbers get room: the same data the icon summarises, drawn at a size where the
// trend and the individual buckets are both legible. It repaints on the tray's
// own refresh, so it is live rather than a snapshot.

// Chart window geometry, in logical pixels.
const (
	chDefaultW = 620.0
	chDefaultH = 380.0
	chMinW     = 420.0
	chMinH     = 260.0
	chPad      = 16.0
	chHeaderH  = 46.0
	chFooterH  = 54.0
	chLegendH  = 24.0
	// chAxisH is the band the time labels sit in, and chAxisW the gutter the
	// value labels need. Both were missing in the first layout, and the result
	// was axis figures clipped at the window edge with the legend drawn over
	// them — the arithmetic placed the chart from chPad without reserving room
	// for the text that describes it.
	chAxisH = 20.0
	chAxisW = 52.0
	chGridRows = 4
	chSamples  = 60

	chFontTitle  = 15
	chFontBody   = 12
	chFontSmall  = 11
	chFontNumber = 19

	chTimerMs = 1000
	chTimerID = 11
)

// chartKind is how the series is drawn.
type chartKind int

// The chart styles.
const (
	chartLine chartKind = iota
	chartArea
	chartBars
)

// chartWindow is the live window.
type chartWindow struct {
	mu     sync.Mutex
	hwnd   uintptr
	wndRef uintptr
	app    chartSource

	kind   chartKind
	metric string
	// samples is the rolling per-refresh history the "live" strip plots, which
	// is what makes the window show the last minute rather than the last day.
	samples []sample
	scale   float64
	// width and height are the window's client size in logical pixels.
	width, height float64
	// hoverLegend is the legend entry under the cursor, or -1.
	hoverLegend int
}

// chartSource is what the chart window reads. It is an interface rather than the
// application itself so the renderer can be exercised with a fixed set of
// numbers, which is the only way to look at the layout without a gateway and a
// desktop.
type chartSource interface {
	Snapshot() status.Snapshot
	Config() config.Config
	Lang() string
	PanelURL() string
	BaseURL() string
}

// sample is one reading of the live strip.
type sample struct {
	at    time.Time
	value float64
}

// openChartWindow shows the window, creating it on first use.
func openChartWindow(a *app.App) {
	chartMu.Lock()
	w := openChart
	chartMu.Unlock()
	if w != nil {
		w.mu.Lock()
		hwnd := w.hwnd
		w.mu.Unlock()
		if hwnd != 0 {
			const swShow = 5
			const swRestore = 9
			showWindow(hwnd, swShow)
			showWindow(hwnd, swRestore)
			setForeground(hwnd)
			repaintChartWindow()
			return
		}
	}

	w = &chartWindow{app: a, metric: a.Metric(), hoverLegend: -1}
	chartMu.Lock()
	openChart = w
	chartMu.Unlock()
	if err := w.create(); err != nil {
		a.Notify("wbtray", err.Error(), 2)
	}
}

// chartMu guards the open window, which is a package-level handle because it is
// created on demand from a menu click.
var (
	chartMu   sync.Mutex
	openChart *chartWindow
)

// repaintChartWindow updates the window if it is open, which is what makes it
// live.
func repaintChartWindow() {
	chartMu.Lock()
	w := openChart
	chartMu.Unlock()
	if w == nil {
		return
	}
	w.mu.Lock()
	hwnd := w.hwnd
	w.mu.Unlock()
	if hwnd != 0 {
		winapi.ProcInvalidateRect.Call(hwnd, 0, 0)
	}
}

// create makes the window.
func (w *chartWindow) create() error {
	w.wndRef = syscall.NewCallback(w.wndProc)
	hInst, _, _ := winapi.ProcGetModuleHandleW.Call(0)
	className := winapi.UTF16Ptr("wbtrayChartWnd")
	wc := winapi.WndClassExW{
		CbSize:        uint32(unsafe.Sizeof(winapi.WndClassExW{})),
		Style:         0x0002 | 0x0001 | 0x0008, // CS_HREDRAW | CS_VREDRAW | CS_DBLCLKS
		LpfnWndProc:   w.wndRef,
		HInstance:     hInst,
		HCursor:       loadCursor(),
		LpszClassName: className,
	}
	winapi.ProcRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	scale := winapi.DPIScaleFor(0)
	ww := int(chDefaultW * scale)
	wh := int(chDefaultH * scale)
	const (
		wsOverlappedWindow = 0x00CF0000
		wsExAppWindow      = 0x00040000
		cwUseDefault       = 0x80000000
	)
	hwnd, _, err := winapi.ProcCreateWindowExW.Call(
		wsExAppWindow,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(winapi.UTF16Ptr("wbtray — charts"))),
		wsOverlappedWindow,
		cwUseDefault, cwUseDefault, uintptr(ww), uintptr(wh),
		0, 0, hInst, 0)
	if hwnd == 0 {
		return err
	}

	w.mu.Lock()
	w.hwnd = hwnd
	w.scale = scale
	w.width, w.height = chDefaultW, chDefaultH
	w.mu.Unlock()

	const swShow = 5
	showWindow(hwnd, swShow)
	setForeground(hwnd)
	winapi.ProcSetTimer.Call(hwnd, chTimerID, chTimerMs, 0)
	return nil
}

// Chart window messages that are not in the shared table.
const (
	wmSize     = 0x0005
	wmKeyDown  = 0x0100
	wmGetMinMaxInfo = 0x0024
	wmNcDestroy     = 0x0082
	wmDestroy       = 0x0002
	wmPaint         = 0x000f
	wmEraseBkgnd    = 0x0014
	wmTimer         = 0x0113
	wmMouseMove     = 0x0200
	wmMouseLeave    = 0x02A3
	wmLButtonDown   = 0x0201
	wmMouseWheel    = 0x020A
	wmClose         = 0x0010

	vkEscape = 0x1B
	vkLeft   = 0x25
	vkRight  = 0x27
)

// wndProc handles the chart window's messages.
func (w *chartWindow) wndProc(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
	switch msg {
	case wmPaint:
		w.paint(hwnd)
		return 0
	case wmEraseBkgnd:
		return 1
	case wmSize:
		if wparam == 1 { // SIZE_MINIMIZED
			return 0
		}
		wp := int32(lparam & 0xffff)
		hp := int32(lparam >> 16)
		scale := winapi.DPIScaleFor(hwnd)
		w.mu.Lock()
		w.scale = scale
		w.width = float64(wp) / scale
		w.height = float64(hp) / scale
		w.mu.Unlock()
		winapi.ProcInvalidateRect.Call(hwnd, 0, 1)
		return 0
	case wmTimer:
		w.appendSample()
		winapi.ProcInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case wmMouseMove:
		w.onMouseMove(hwnd, lparam)
		return 0
	case wmLButtonDown:
		w.onClick(lparam)
		return 0
	case wmMouseWheel:
		w.onWheel(int32(int16(wparam>>16)))
		return 0
	case wmKeyDown:
		switch uint32(wparam) {
		case vkEscape:
			const swHide = 0
			showWindow(hwnd, swHide)
		case vkLeft:
			w.cycleMetric(-1)
		case vkRight:
			w.cycleMetric(1)
		}
		return 0
	case wmClose:
		// The window is hidden rather than destroyed, so reopening it keeps the
		// live samples that were already collected.
		const swHide = 0
		winapi.ProcKillTimer.Call(hwnd, chTimerID)
		showWindow(hwnd, swHide)
		return 0
	}
	ret, _, _ := winapi.ProcDefWindowProcW.Call(hwnd, uintptr(msg), wparam, lparam)
	return ret
}

// appendSample records one reading of the live strip.
func (w *chartWindow) appendSample() {
	snap := w.app.Snapshot()
	value := snap.MetricValue(w.metricValue())
	w.mu.Lock()
	w.samples = append(w.samples, sample{at: time.Now(), value: value})
	if len(w.samples) > chSamples {
		w.samples = w.samples[len(w.samples)-chSamples:]
	}
	w.mu.Unlock()
}

// metricValue is the metric in force, which the window owns independently of the
// tray icon so the two can be looked at together.
func (w *chartWindow) metricValue() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.metric
}

// onMouseMove updates the hovered legend entry.
func (w *chartWindow) onMouseMove(hwnd, lparam uintptr) {
	scale := winapi.DPIScaleFor(hwnd)
	x := float64(int16(lparam&0xffff)) / scale
	y := float64(int16(lparam>>16&0xffff)) / scale
	idx := w.legendAt(x, y)
	w.mu.Lock()
	changed := w.hoverLegend != idx
	w.hoverLegend = idx
	w.mu.Unlock()
	if changed {
		winapi.ProcInvalidateRect.Call(hwnd, 0, 0)
	}
}

// onClick switches metric on a legend click, and chart style on a chart click.
func (w *chartWindow) onClick(lparam uintptr) {
	scale := winapi.DPIScaleFor(0)
	x := float64(int16(lparam&0xffff)) / scale
	y := float64(int16(lparam>>16&0xffff)) / scale
	if idx := w.legendAt(x, y); idx >= 0 {
		w.setMetric(config.Metrics[idx])
		return
	}
	w.mu.Lock()
	w.kind = (w.kind + 1) % 3
	w.mu.Unlock()
	repaintChartWindow()
}

// onWheel switches metric with the wheel, which is quicker than aiming at the
// legend.
func (w *chartWindow) onWheel(delta int32) {
	if delta < 0 {
		w.cycleMetric(1)
		return
	}
	w.cycleMetric(-1)
}

// cycleMetric moves to the next metric in the list.
func (w *chartWindow) cycleMetric(step int) {
	w.mu.Lock()
	current := w.metric
	w.mu.Unlock()
	idx := 0
	for i, m := range config.Metrics {
		if m == current {
			idx = i
			break
		}
	}
	n := len(config.Metrics)
	next := ((idx+step)%n + n) % n
	w.setMetric(config.Metrics[next])
}

// setMetric switches the plotted metric and remembers it for the window's life.
func (w *chartWindow) setMetric(metric string) {
	w.mu.Lock()
	w.metric = metric
	w.samples = nil
	w.mu.Unlock()
	repaintChartWindow()
}

// legendAt returns the legend entry at a point in logical coordinates.
func (w *chartWindow) legendAt(x, y float64) int {
	w.mu.Lock()
	width, height := w.width, w.height
	w.mu.Unlock()
	top := height - chFooterH - chLegendH
	if y < top || y > top+chLegendH {
		return -1
	}
	// The spans are computed the same way the painter computes them, from the
	// same measurements, so a click lands on the entry that was drawn there.
	spans := legendSpans(w.app.Lang(), width, 1)
	for i, sp := range spans {
		if x >= sp[0] && x <= sp[1] {
			return i
		}
	}
	return -1
}

// legendSpans returns the horizontal extent of each legend entry. It exists so
// the painter and the hit test share one definition of where the entries are:
// the first version computed them separately and the click targets drifted away
// from the labels they belonged to.
func legendSpans(lang string, width, scale float64) [][2]float64 {
	widths := make([]float64, len(config.Metrics))
	total := 0.0
	for i, m := range config.Metrics {
		widths[i] = float64(textWidth(i18n.T(lang, "metric."+m), chFontSmall)) + 22*scale
		total += widths[i]
	}
	cursor := chPad * scale
	if slack := width - chPad*2*scale - total; slack > 0 {
		gap := math.Min(slack/float64(len(config.Metrics)), 14*scale)
		extra := slack - gap*float64(len(config.Metrics))
		if extra < 0 {
			extra = 0
		}
		cursor += extra / 2
		for i := range widths {
			widths[i] += gap
		}
	}
	out := make([][2]float64, 0, len(widths))
	for _, w := range widths {
		out = append(out, [2]float64{cursor, cursor + w})
		cursor += w
	}
	return out
}

// paint renders the window.
func (w *chartWindow) paint(hwnd uintptr) {
	var ps winapi.PaintStruct
	hdc, _, _ := winapi.ProcBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	defer winapi.ProcEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))

	w.mu.Lock()
	scale, width, height := w.scale, w.width, w.height
	kind, metric := w.kind, w.metric
	samples := append([]sample{}, w.samples...)
	hover := w.hoverLegend
	w.mu.Unlock()

	snap := w.app.Snapshot()
	cfg := w.app.Config()
	lang := cfg.Lang
	th := theme.ByName(cfg.Theme)

	pw, ph := int(width*scale), int(height*scale)
	if pw < 16 || ph < 16 {
		return
	}
	c := raster.New(pw, ph)
	w.render(c, scale, height, snap, th, lang, kind, metric, samples, hover)

	hbm, bits := winapi.NewDIBSection(pw, ph)
	if hbm == 0 {
		return
	}
	defer winapi.ProcDeleteObject.Call(hbm)
	copy(unsafe.Slice((*byte)(bits), pw*ph*4), c.BGRA())

	memDC, _, _ := winapi.ProcCreateCompatibleDC.Call(hdc)
	if memDC == 0 {
		return
	}
	defer winapi.ProcDeleteDC.Call(memDC)
	old, _, _ := winapi.ProcSelectObject.Call(memDC, hbm)
	defer winapi.ProcSelectObject.Call(memDC, old)
	const srccopy = 0x00CC0020
	winapi.ProcBitBlt.Call(hdc, 0, 0, uintptr(pw), uintptr(ph), memDC, 0, 0, srccopy)
}

// render draws the whole window into a canvas. It is pure, so the layout can be
// exercised without a window.
func (w *chartWindow) render(c *raster.Canvas, scale, height float64, snap status.Snapshot, th theme.Theme, lang string, kind chartKind, metric string, samples []sample, hover int) {
	px := func(v float64) float64 { return v * scale }
	text := func(v float64) int { return int(v + 0.5) }

	c.Fill(th.MenuBg)

	// Header: the mascot, the title, and the address being watched.
	cat.Draw(c, px(chPad), px(8), px(30), px(30), cat.Palette{
		Fur:   th.Accent,
		Inner: th.MenuBg,
		Blush: th.Warn.Mul(0.5),
	})
	title := i18n.T(lang, "chart.title")
	drawTextAt(c, text(px(chPad+38)), text(px(11)), chFontTitle, true, title, th.MenuInk)
	subtitle := fmt.Sprintf("%s · %s", w.app.BaseURL(), stateWordFor(lang, snap))
	drawTextAt(c, text(px(chPad+38)), text(px(29)), chFontSmall, false, subtitle, th.MenuInkDim)

	// A live dot, blinking on the refresh: a still window and a stopped tray look
	// the same otherwise.
	liveX := float64(c.W) - px(chPad) - px(8)
	dot := th.OK
	if !snap.Reachable {
		dot = th.Bad
	}
	if time.Now().Unix()%2 == 0 {
		c.Circle(liveX, px(22), px(4), dot)
	} else {
		c.Circle(liveX, px(22), px(4), dot.Mul(0.35))
	}

	// The chart area.
	//
	// The bands, from the top: header, the plot, the axis labels, the legend, the
	// footer. Each reserves its own height before the next is placed, which is
	// what keeps the value labels from landing outside the window and the legend
	// from being drawn over the time axis.
	chartTop := chHeaderH
	chartBottom := height - chFooterH - chLegendH - chAxisH
	chartLeft := chPad + chAxisW
	chartRight := float64(c.W)/scale - chPad
	if chartBottom-chartTop < 40 {
		return
	}

	series, labels := chartSeries(snap, metric)
	unit := unitFor(metric)

	// Gridlines and the value axis, drawn before the data so the data sits on
	// top of the rules rather than under them.
	//
	// The axis is scaled by the plotted series alone. Including the live strip's
	// samples in the maximum was the first version's mistake: the strip holds
	// window totals, which are an order of magnitude larger than a single
	// bucket, so the curve was squashed against the floor and the axis read
	// 5.0k for data that never left three figures.
	top := raster.Max(series)
	if top <= 0 {
		top = 1
	}
	top = niceCeiling(top)

	for i := 0; i <= chGridRows; i++ {
		y := chartTop + (chartBottom-chartTop)*float64(i)/chGridRows
		c.Rect(px(chartLeft), px(y), px(chartRight-chartLeft), 1, th.MenuLine)
		value := top * (1 - float64(i)/chGridRows)
		// The axis figure is right-aligned into the gutter and vertically centred
		// on its own gridline, which is what makes it read as belonging to it.
		drawTextRightAt(c, text(px(chartLeft)-px(8)), text(px(y)-px(6)), chFontSmall, false,
			i18n.Compact(value)+unit, th.MenuInkDim)
	}

	// The data.
	if len(series) >= 2 {
		points := make([]raster.Pt, len(series))
		stepX := (chartRight - chartLeft) / float64(len(series)-1)
		for i, v := range series {
			points[i] = raster.Pt{
				X: px(chartLeft + stepX*float64(i)),
				Y: px(chartBottom - (chartBottom-chartTop)*(v/top)),
			}
		}
		switch kind {
		case chartBars:
			barW := (px(chartRight-chartLeft) / float64(len(series))) * 0.7
			for i, p := range points {
				if i > 0 {
					barW = math.Min(barW, px(stepX)*0.7)
				}
				h := px(chartBottom) - p.Y
				if h < px(1) {
					h = px(1)
				}
				c.RoundedRect(p.X-barW/2, p.Y, barW, h, barW*0.25, th.Accent)
			}
		case chartArea:
			// A filled area, built from vertical strips rather than a polygon with
			// a clipped curve: at this scale the difference is invisible and the
			// strips cannot self-intersect.
			for x := px(chartLeft); x < px(chartRight); x++ {
				t := (x - px(chartLeft)) / px(chartRight-chartLeft)
				i := t * float64(len(points)-1)
				lo := int(i)
				if lo >= len(points)-1 {
					lo = len(points) - 2
				}
				frac := i - float64(lo)
				y := points[lo].Y + (points[lo+1].Y-points[lo].Y)*frac
				c.Rect(x, y, 1, px(chartBottom)-y, th.Accent.Mul(0.28))
			}
			c.Line(points, px(2), th.Accent)
		default:
			c.Line(points, px(2.2), th.Accent)
		}
		// The newest point is marked, so "now" is unambiguous.
		if kind != chartBars {
			last := points[len(points)-1]
			c.Circle(last.X, last.Y, px(3.5), th.Accent)
			c.Circle(last.X, last.Y, px(6.5), th.Accent.Mul(0.3))
		}
	} else {
		drawTextAt(c, text(px(chartLeft)+px(8)), text(px(chartTop)+px(8)), chFontBody, false,
			i18n.T(lang, "chart.empty"), th.MenuInkDim)
	}

	// The time axis: first and last bucket, which is enough to orient the curve.
	if len(labels) > 1 {
		axisY := chartBottom + chAxisH*0.30
		drawTextAt(c, text(px(chartLeft)), text(px(axisY)), chFontSmall, false,
			shortLabel(labels[0]), th.MenuInkDim)
		last := shortLabel(labels[len(labels)-1])
		drawTextRightAt(c, text(px(chartRight)), text(px(axisY)), chFontSmall, false,
			last, th.MenuInkDim)
	}

	// The legend: one entry per metric, clickable, with the chosen one marked.
	//
	// The entries are laid out in one row and centred as a group, which is what
	// keeps them on the same line as each other: drawing them from the left and
	// wrapping the last one put two of them on a second line.
	legendY := height - chFooterH - chLegendH + 4
	spans := legendSpans(lang, float64(c.W)/scale, scale)
	for i, m := range config.Metrics {
		if i >= len(spans) {
			break
		}
		label := i18n.T(lang, "metric."+m)
		left, right := spans[i][0], spans[i][1]
		if right > float64(c.W)/scale-chPad {
			break
		}
		colour := th.MenuInkDim
		if m == metric {
			colour = th.Accent
		}
		if i == hover {
			c.RoundedRect(px(left)-px(4), px(legendY)-px(4), px(right-left), px(20), px(5), th.MenuSel)
		}
		c.Circle(px(left)+px(4), px(legendY)+px(7), px(3), colour)
		drawTextAt(c, text(px(left)+px(12)), text(px(legendY)), chFontSmall, m == metric, label, colour)
	}

	// The footer: the live strip and the current figures, which is the part the
	// eye goes to when something changes.
	footerTop := height - chFooterH
	c.Rect(px(0), px(footerTop), float64(c.W), px(1), th.MenuLine)

	value := snap.MetricValue(metric)
	drawTextAt(c, text(px(chPad)), text(px(footerTop)+px(8)), chFontNumber, true,
		i18n.Compact(value)+unitFor(metric), th.MenuInk)
	drawTextAt(c, text(px(chPad)), text(px(footerTop)+px(32)), chFontSmall, false,
		i18n.T(lang, "metric."+metric), th.MenuInkDim)

	// The live strip: the last minute of readings, which is what makes the window
	// show change rather than level.
	stripLeft := chPad + 132
	stripRight := float64(c.W)/scale - chPad - 168
	if stripRight-stripLeft > 60 && len(samples) >= 2 {
		// The strip starts below the label rather than under it: the first
		// version drew the two on the same line and the caption sat across the
		// footer's own border.
		stripTop := footerTop + 20
		stripBottom := footerTop + chFooterH - 10
		values := make([]float64, len(samples))
		for i, s := range samples {
			values[i] = s.value
		}
		peak := raster.Max(values)
		if peak <= 0 {
			peak = 1
		}
		points := make([]raster.Pt, len(values))
		for i, v := range values {
			points[i] = raster.Pt{
				X: px(stripLeft + (stripRight-stripLeft)*float64(i)/float64(len(values)-1)),
				Y: px(stripBottom - (stripBottom-stripTop)*(v/peak)),
			}
		}
		c.Line(points, px(1.6), th.Accent.Mul(0.85))
		drawTextAt(c, text(px(stripLeft)), text(px(footerTop)+px(6)), chFontSmall, false,
			fmt.Sprintf("%s · %ds", i18n.T(lang, "chart.live"), len(samples)), th.MenuInkDim)
	}

	// Account and request counts, right-aligned.
	if snap.Reachable && snap.Total > 0 {
		x := float64(c.W)/scale - chPad
		drawTextRightAt(c, text(px(x)), text(px(footerTop)+px(10)), chFontBody, true,
			fmt.Sprintf("%d/%d", snap.Ready(), snap.Total), th.MenuInk)
		drawTextRightAt(c, text(px(x)), text(px(footerTop)+px(32)), chFontSmall, false,
			i18n.T(lang, "metric.accounts"), th.MenuInkDim)
	}
}

// chartSeries returns the series a metric plots, and the labels for its buckets.
func chartSeries(snap status.Snapshot, metric string) ([]float64, []string) {
	series := snap.SeriesFor(metric)
	if len(series) == 0 {
		// No history for this metric: the hourly request series is still a
		// truthful picture of how busy the gateway has been.
		return snap.Usage.Series, snap.Usage.Labels
	}
	return series, snap.Usage.Labels
}

// unitFor is the suffix a metric's numbers carry.
func unitFor(metric string) string {
	switch metric {
	case "latency":
		return " ms"
	case "tps":
		return " t/s"
	case "credits":
		return ""
	}
	return ""
}

// niceCeiling rounds a maximum up to a readable axis top.
func niceCeiling(v float64) float64 {
	if v <= 0 {
		return 1
	}
	magnitude := math.Pow(10, math.Floor(math.Log10(v)))
	for _, step := range []float64{1, 1.5, 2, 2.5, 3, 4, 5, 7.5, 10} {
		if v <= step*magnitude {
			return step * magnitude
		}
	}
	return 10 * magnitude
}

// shortLabel trims a bucket label to the part that identifies it: an hour bucket
// is "2006-01-02T15" and the hour is what matters on an axis.
func shortLabel(label string) string {
	if len(label) >= 13 {
		return label[11:13] + ":00"
	}
	if len(label) >= 10 {
		return label[5:]
	}
	return label
}

// stateWordFor is the one-word state for the subtitle.
func stateWordFor(lang string, snap status.Snapshot) string {
	switch {
	case !snap.Reachable:
		return i18n.T(lang, "health.down")
	case snap.Total > 0 && snap.Ready() == 0:
		return i18n.T(lang, "health.warn")
	default:
		return i18n.T(lang, "health.ok")
	}
}

// Text helpers. They are thin wrappers so the drawing code reads as layout
// rather than as calls with five positional arguments.
func drawTextAt(c *raster.Canvas, x, y, size int, bold bool, s string, colour raster.RGBA) {
	textmask.Draw(c, float64(x), float64(y), textmask.NewFont(size, bold), s, colour)
}

func drawTextRightAt(c *raster.Canvas, x, y, size int, bold bool, s string, colour raster.RGBA) {
	textmask.DrawRight(c, float64(x), float64(y), textmask.NewFont(size, bold), s, colour)
}

func textWidth(s string, size int) int {
	w, _ := textmask.Measure(s, textmask.NewFont(size, false))
	return w
}
