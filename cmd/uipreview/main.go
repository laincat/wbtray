// Command uipreview renders the window to a PNG, so the layout can be looked at
// without building and running the tray.
//
// It is a development tool. The window draws itself at run time from the gateway's
// live numbers, and this is a way to see the result at a chosen size, tone and page
// instead of resizing a window and squinting at it. It goes through the same GDI text
// path the window does, because a preview that drew its text another way would be
// previewing a different program.
//
// Usage:
//
//	go run ./cmd/uipreview -out design/console.png -tab all
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"wbtray/internal/panel"
	"wbtray/internal/raster"
	"wbtray/internal/status"
	"wbtray/internal/theme"
	"wbtray/internal/ui"
	"wbtray/internal/winapi"
)

func main() {
	out := flag.String("out", "window.png", "where to write the sheet")
	size := flag.String("size", "1180x760", "window size as WxH")
	tab := flag.String("tab", "all", "which page: a tab name, or all for a sheet")
	light := flag.Bool("light", false, "draw the light tone")
	palette := flag.String("palette", theme.DefaultName,
		"which palette: "+strings.Join(theme.Names, ", "))
	dpi := flag.Float64("dpi", 1, "scale factor to render at")
	flag.Parse()

	w, h, err := parseSize(*size)
	if err != nil {
		fmt.Fprintf(os.Stderr, "uipreview: %v\n", err)
		os.Exit(1)
	}
	pal := theme.ForName(*palette, *light)

	if *tab != "all" {
		if err := writePage(*out, ui.Tab(*tab), w, h, pal, *dpi); err != nil {
			fmt.Fprintf(os.Stderr, "uipreview: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Every page, one above the other, at half scale: a sheet that shows them one at
	// a time is six files to open, and what is being looked at here is whether they
	// agree with each other.
	scale := 2
	cols := 2
	rows := (len(ui.Tabs) + cols - 1) / cols
	sw, sh := w/scale, h/scale
	img := image.NewRGBA(image.Rect(0, 0, sw*cols+gap*(cols+1), sh*rows+gap*(rows+1)))
	draw.Draw(img, img.Bounds(), &image.Uniform{rasterToColor(pal.BG)}, image.Point{}, draw.Src)
	for i, t := range ui.Tabs {
		page := render(t, w, h, pal, *dpi)
		small := page.Scale(sw, sh)
		x := gap + (i%cols)*(sw+gap)
		y := gap + (i/cols)*(sh+gap)
		paste(img, small, x, y)
	}
	if err := save(*out, img); err != nil {
		fmt.Fprintf(os.Stderr, "uipreview: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%dx%d, all %d pages)\n", *out, img.Rect.Dx(), img.Rect.Dy(), len(ui.Tabs))
}

const gap = 10

func writePage(path string, tab ui.Tab, w, h int, pal theme.Palette, dpi float64) error {
	c := render(tab, w, h, pal, dpi)
	if err := save(path, c.Image()); err != nil {
		return err
	}
	fmt.Printf("wrote %s (%dx%d, %s)\n", path, w, h, tab)
	return nil
}

// render lays out one page and draws its text through GDI, which is the path the
// window itself takes.
func render(tab ui.Tab, w, h int, pal theme.Palette, dpi float64) *raster.Canvas {
	layout := ui.Build(ui.View{
		W: w, H: h,
		Tab:        tab,
		Palette:    pal,
		Lang:       "zh",
		Snap:       sampleSnapshot(),
		Models:     sampleModels(),
		Logs:       sampleLogs(),
		Schedule:   sampleSchedule(),
		Config:     sampleConfig(),
		ConfigPath: "config.json",
		Action:     "全部签到：完成 3 个，失败 0 个",
	})
	c := &layout.Ink
	items := make([]winapi.TextItem, 0, len(layout.Texts))
	for _, t := range layout.Texts {
		items = append(items, winapi.TextItem{
			S: t.S, X: t.X, Y: t.Y, Size: t.Size,
			Weight: weightFor(t.Weight),
			Right:  t.Align == ui.Right,
			Centre: t.Align == ui.Centre,
			Colour: winapi.BGR(t.Colour),
		})
	}
	if tr := winapi.NewTextRenderer(w, h); tr != nil {
		tr.Draw(c, items, dpi)
		tr.Close()
	}
	return c
}

func weightFor(w ui.Weight) int {
	if w == ui.Figure {
		return winapi.FWSemi
	}
	return winapi.FWNormal
}

func parseSize(s string) (int, int, error) {
	a, b, ok := strings.Cut(strings.ToLower(s), "x")
	if !ok {
		return 0, 0, fmt.Errorf("size %q is not WxH", s)
	}
	w, err := strconv.Atoi(a)
	if err != nil {
		return 0, 0, err
	}
	h, err := strconv.Atoi(b)
	if err != nil {
		return 0, 0, err
	}
	return w, h, nil
}

func save(path string, img image.Image) error {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func rasterToColor(c raster.RGBA) color.RGBA { return color.RGBA{c.R, c.G, c.B, 0xff} }

func paste(dst *image.RGBA, src *raster.Canvas, x, y int) {
	for j := 0; j < src.H; j++ {
		for i := 0; i < src.W; i++ {
			p := src.At(i, j)
			if p.A == 0 {
				continue
			}
			dx, dy := x+i, y+j
			if dx < 0 || dy < 0 || dx >= dst.Rect.Dx() || dy >= dst.Rect.Dy() {
				continue
			}
			dst.SetRGBA(dx, dy, color.RGBA{p.R, p.G, p.B, 0xff})
		}
	}
}

// sampleSnapshot is a plausible reading: a window drawn from zeros shows nothing
// about whether the layout works, and the layout is what this tool exists to look at.
func sampleSnapshot() status.Snapshot {
	return status.Snapshot{
		Reachable: true,
		Version:   "1.11.9-panel",
		Uptime:    104760,
		Total:     3,
		Healthy:   2,
		Accounts: []status.Account{
			{UID: "17caa3c8-9718-4ec5-bac0-a8f84c54fce8", Nickname: "Laincat",
				Credits: 7954, Total: 14505, Success: 3510, ErrTotal: 1, Realm: "cn",
				Requests: 3511, LastLatencyMs: 4691, LastTPS: 73.5,
				LastUsed: time.Now().Add(-12 * time.Second)},
			{UID: "b2f1a", Nickname: "backup", Credits: 4200, Total: 14505,
				Success: 812, Realm: "cn", Cooling: true, Requests: 812,
				LastLatencyMs: 6120, LastTPS: 41.2,
				LastUsed: time.Now().Add(-4 * time.Minute)},
			{UID: "c3a7b", Nickname: "工作号", Credits: 0, Total: 14505,
				Success: 104, Disabled: true, Realm: "cn", Requests: 104,
				LastLatencyMs: 2210, LastTPS: 118.4,
				LastUsed: time.Now().Add(-3 * time.Hour)},
		},
		Usage: status.Usage{
			Requests: 1214, Errors: 1,
			PromptTokens: 607_800_000, CompletionTok: 1_200_000, TotalTokens: 609_000_000,
			AvgLatencyMs: 8219, AvgTPS: 92.6,
			Series: []float64{23, 103, 427, 233, 147, 62, 197, 240, 180, 320, 210, 96},
		},
	}
}

func sampleModels() []panel.Model {
	return []panel.Model{
		{ID: "cn:deepseek-v4.1-flash", Name: "DeepSeek V4.1 Flash", Credits: "x0.14",
			ContextLength: 1_000_000, MaxOutputTokens: 128_000, SupportsReasoning: true,
			SupportsToolCall: true, SupportsImages: true, DefaultEffort: "high",
			SupportedEfforts: []string{"low", "high", "max"}},
		{ID: "cn:kimi-k3-2", Name: "Kimi-K3", Credits: "x1.62",
			ContextLength: 1_000_000, MaxOutputTokens: 1_048_576, SupportsReasoning: true,
			SupportsToolCall: true, SupportsImages: true, DefaultEffort: "medium"},
		{ID: "cn:glm-5.3-flashx", Name: "GLM-5.3-FlashX", Credits: "x0.22",
			ContextLength: 1_000_000, MaxOutputTokens: 128_000, SupportsReasoning: true,
			SupportsToolCall: true, SupportsImages: true, DefaultEffort: "high",
			SupportedEfforts: []string{"low", "high", "max"}},
		{ID: "cn:minimax-m3", Name: "MiniMax M3", Credits: "x0.31",
			ContextLength: 512_000, MaxOutputTokens: 64_000, SupportsToolCall: true,
			SupportsImages: true},
		{ID: "cn:qwen3-max", Name: "Qwen3 Max", Credits: "x0.48",
			ContextLength: 262_144, MaxOutputTokens: 32_768, SupportsToolCall: true,
			DefaultEffort: "medium"},
		{ID: "cn:deepseek-v3.2", Name: "DeepSeek V3.2", Credits: "x0.09",
			ContextLength: 128_000, MaxOutputTokens: 16_384, SupportsToolCall: true},
		{ID: "cn:claude-sonnet-4.6", Name: "Claude Sonnet 4.6", Credits: "x2.40",
			ContextLength: 200_000, MaxOutputTokens: 64_000, SupportsReasoning: true,
			SupportsToolCall: true, SupportsImages: true, DefaultEffort: "high"},
		{ID: "cn:gpt-5.6-luna", Name: "GPT-5.6 Luna", Credits: "x1.05",
			ContextLength: 400_000, MaxOutputTokens: 128_000, SupportsToolCall: true,
			SupportsImages: true, IsDefault: true, DefaultEffort: "medium"},
	}
}

func sampleLogs() []panel.LogEntry {
	lines := []struct{ ch, text string }{
		{"chat", "| #1350 | 15:53:41 | cn:deepseek-v4.1-flash | stream | 200 | Laincat(17caa3c8) | TTFB=3424ms | tok=158 | 40.5tok/s | total=3.9s |"},
		{"chat", "| #1349 | 15:53:12 | cn:deepseek-v4.1-flash | stream | 200 | Laincat(17caa3c8) | TTFB=2980ms | tok=412 | 61.2tok/s | total=6.7s |"},
		{"task", "checkin u1 成功 credits=7954"},
		{"chat", "| #1348 | 15:52:40 | cn:deepseek-v4.1-flash | stream | 200 | Laincat(17caa3c8) | TTFB=4110ms | tok=91 | 28.4tok/s | total=3.2s |"},
		{"sys", "balance refresh failed once, retrying"},
		{"chat", "| #1347 | 15:51:58 | cn:kimi-k3-2 | stream | 200 | Laincat(17caa3c8) | TTFB=5220ms | tok=1204 | 88.1tok/s | total=13.7s |"},
		{"task", "keepalive u1 token refreshed"},
		{"chat", "| #1346 | 15:51:02 | cn:deepseek-v4.1-flash | stream | 200 | Laincat(17caa3c8) | TTFB=3340ms | tok=220 | 52.9tok/s | total=4.2s |"},
		{"sys", "pool: 2 ready, 1 cooling"},
		{"chat", "| #1345 | 15:50:11 | cn:deepseek-v4.1-flash | stream | 200 | Laincat(17caa3c8) | TTFB=2870ms | tok=76 | 22.1tok/s | total=3.4s |"},
	}
	out := make([]panel.LogEntry, 0, len(lines))
	for i, l := range lines {
		out = append(out, panel.LogEntry{
			TS:   time.Now().Add(-time.Duration(i) * 45 * time.Second).Format(time.RFC3339Nano),
			Ch:   l.ch,
			Text: l.text,
		})
	}
	return out
}

func sampleSchedule() panel.Schedule {
	return panel.Schedule{
		Known:          true,
		Checkin:        true,
		Travel:         true,
		Activity:       true,
		Keepalive:      true,
		BalanceRefresh: true,
	}
}

func sampleConfig() string {
	return `{"listen":":7863","api_key":"sk-probe","auth_dir":"./auths","state_file":"./data/state.json","global":{"enabled":true},"pool":{"max_in_flight":3,"max_in_flight_global":2,"breaker_threshold":3,"breaker_cooldown":"30m","expiring_soon":"168h"},"session_sticky":{"enabled":true,"ttl":"30m"},"upstream":{"timeout_seconds":120,"header_timeout_seconds":120}}`
}
