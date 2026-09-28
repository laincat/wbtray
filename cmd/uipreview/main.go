// Command uipreview renders the window to a PNG, so the layout can be looked at
// without building and running the tray.
//
// It is a development tool. The window draws itself at run time from the gateway's
// live numbers, and this is a way to see the result at a chosen size, palette and
// tone instead of resizing a window and squinting at it.
//
// Usage:
//
//	go run ./cmd/uipreview [-out design/window.png] [-size 1180x760] [-light] [-mono]
package main

import (
	"flag"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"wbtray/internal/raster"
	"wbtray/internal/status"
	"wbtray/internal/theme"
	"wbtray/internal/ui"
	"wbtray/internal/winapi"
)

func main() {
	out := flag.String("out", "window.png", "where to write the sheet")
	size := flag.String("size", "1180x760", "window size as WxH")
	accentHex := flag.String("accent", "", "accent colour as #rrggbb")
	mono := flag.Bool("mono", false, "draw the monochrome palette")
	light := flag.Bool("light", false, "draw the light tone")
	dpi := flag.Float64("dpi", 1, "scale factor to render at")
	flag.Parse()

	w, h, err := parseSize(*size)
	if err != nil {
		fmt.Fprintf(os.Stderr, "uipreview: %v\n", err)
		os.Exit(1)
	}

	accent := theme.Accent{}
	if *accentHex != "" {
		accent = theme.Accent{Known: true, Colour: raster.Hex(*accentHex)}
	}
	pal := theme.System(accent)
	if *mono {
		pal = theme.Mono()
	}
	pal = pal.On(*light)

	layout := ui.Build(ui.View{
		W: w, H: h,
		Palette: pal,
		Lang:    "zh",
		Snap:    sample(),
	})
	c := &layout.Ink

	// The text is drawn by GDI, which is the same path the window takes. That is
	// the point of the tool: a preview that drew text its own way would be
	// previewing a different program.
	items := make([]winapi.TextItem, 0, len(layout.Texts))
	for _, t := range layout.Texts {
		items = append(items, winapi.TextItem{
			S: t.S, X: t.X, Y: t.Y, Size: t.Size,
			Weight: weightFor(t.Weight),
			Right:  t.Align == ui.Right,
			Colour: winapi.BGR(t.Colour),
		})
	}
	if tr := winapi.NewTextRenderer(w, h); tr != nil {
		tr.Draw(c, items, *dpi)
		tr.Close()
	}

	if dir := filepath.Dir(*out); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "uipreview: %v\n", err)
			os.Exit(1)
		}
	}
	f, err := os.Create(*out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "uipreview: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()
	if err := png.Encode(f, c.Image()); err != nil {
		fmt.Fprintf(os.Stderr, "uipreview: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%dx%d, %s, %d strings)\n",
		*out, w, h, pal.Label("en"), len(items))
}

func weightFor(w ui.Weight) int {
	switch w {
	case ui.Figure:
		return winapi.FWSemi
	default:
		return winapi.FWNormal
	}
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

// sample is a plausible reading: a window drawn from zeros shows nothing about
// whether the layout works, and the layout is what this tool exists to look at.
func sample() status.Snapshot {
	return status.Snapshot{
		Reachable: true,
		Version:   "1.11.9-panel",
		Uptime:    104760,
		Total:     3,
		Healthy:   3,
		Accounts: []status.Account{
			{UID: "17caa3c8-9718-4ec5-bac0-a8f84c54fce8", Nickname: "Laincat",
				Credits: 7954, Total: 14505, Success: 3510, ErrTotal: 1, Realm: "cn",
				Requests: 3511, LastLatencyMs: 4691, LastTPS: 73.5,
				LastUsed: time.Now().Add(-12 * time.Second)},
			{UID: "b2f1", Nickname: "backup", Credits: 4200, Total: 14505,
				Success: 812, Realm: "cn", Cooling: true,
				Requests: 812, LastLatencyMs: 6120, LastTPS: 41.2,
				LastUsed: time.Now().Add(-4 * time.Minute)},
			{UID: "c3a7", Nickname: "spare", Credits: 0, Total: 14505,
				Success: 104, Disabled: true, Realm: "cn",
				Requests: 104, LastLatencyMs: 2210, LastTPS: 118.4,
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
