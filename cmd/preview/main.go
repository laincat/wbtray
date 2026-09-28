// Command preview renders the sheet used to choose a look: every style against
// every palette and tone, at the sizes the taskbar actually uses.
//
// It is a development tool rather than part of the program. The tray draws its
// icons at run time, and this is a way to look at all of them at once instead of
// changing a setting and squinting at the notification area.
//
// Usage:
//
//	go run ./cmd/preview [-out design/styles.png] [-size 32] [-scale 5]
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"strings"

	"wbtray/internal/config"
	"wbtray/internal/iconstyle"
	"wbtray/internal/raster"
	"wbtray/internal/status"
	"wbtray/internal/theme"
)

func main() {
	out := flag.String("out", "preview.png", "where to write the sheet")
	size := flag.Int("size", 32, "icon size in pixels, as the taskbar draws it")
	scale := flag.Int("scale", 5, "magnification of each icon in the sheet")
	accentHex := flag.String("accent", "", "accent colour as #rrggbb; the default one is used when empty")
	flag.Parse()

	if err := write(*out, *size, *scale, *accentHex); err != nil {
		fmt.Fprintf(os.Stderr, "preview: %v\n", err)
		os.Exit(1)
	}
}

// variant is one row group of the sheet: a name, and the palette it draws with.
//
// The two travel together because a sheet whose labels say "System" twice is a
// sheet nobody can read.
type variant struct {
	label   string
	palette theme.Palette
}

func write(path string, size, scale int, accentHex string) error {
	// The accent is a parameter rather than something read from the system,
	// because this is a development tool and the point of it is to draw the icon
	// for a chosen colour: what a palette looks like under an operator's own
	// accent is exactly the thing worth seeing before choosing one.
	accent := theme.Accent{}
	if accentHex != "" {
		accent = theme.Accent{Known: true, Colour: raster.Hex(accentHex)}
	}

	// Both tones. The light one is a real mode an operator can be in, and a sheet
	// that showed only the dark one would not show the thing most likely to be
	// wrong.
	var variants []variant
	for _, p := range theme.All(accent) {
		variants = append(variants,
			variant{p.Label("en"), p},
			variant{p.Label("en"), theme.On(true)})
	}

	// Three states, because an indicator that only looks right when everything
	// works is not an indicator. Plus a paused tray, which is a fourth thing the
	// icon has to say.
	states := []struct {
		name   string
		snap   status.Snapshot
		paused bool
	}{
		{"ok", sampleSnapshot(3, 3, 48250, 91.5, 38.2, 1284, 12), false},
		{"busy", sampleSnapshot(1, 4, 21400, 940, 6.5, 12480, 51), false},
		{"down", status.Snapshot{Reachable: false}, false},
		{"paused", sampleSnapshot(3, 3, 48250, 91.5, 38.2, 1284, 12), true},
	}

	if err := sheet(path, variants, states, size, scale, false); err != nil {
		return err
	}
	// A second sheet at the size the taskbar really uses, magnified, because an
	// icon that reads well at 32 pixels can still be mud at 16. It lands beside
	// the first one under a name derived from it, so the pair cannot drift apart
	// through a mistyped path.
	return sheet(replaceExt(path, "-16px.png"), variants, states, 16, 16, true)
}

// sheet writes one grid: a row per variant, a column per state, and the styles
// laid out within each variant's block.
func sheet(path string, variants []variant, states []struct {
	name   string
	snap   status.Snapshot
	paused bool
}, size, scale int, compact bool) error {
	const (
		gap    = 6
		cell   = 12
		labelW = 132
	)
	styles := config.Styles
	iconPx := size * scale
	cellW := iconPx + gap*2
	cellH := iconPx + gap*2

	width := labelW + len(states)*cellW + cell
	height := cell*gap*2 + len(variants)*(len(styles)*cellH+cell*gap)
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	// A mid grey background: the sheet has to be readable on a light and a dark
	// surface at once, which is what a transparent icon needs tested.
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{0x80, 0x84, 0x8c, 0xff}},
		image.Point{}, draw.Src)
	ink := raster.RGBA{R: 0x14, G: 0x17, B: 0x1f, A: 0xff}

	for i, st := range states {
		head := raster.New(cellW, cell*gap)
		head.Text(4, 8, 1.8, strings.ToUpper(st.name), ink)
		paste(img, head, labelW+i*cellW, 0)
	}

	y := cell*gap + cell
	for _, v := range variants {
		blockTop := y
		for _, style := range styles {
			x := labelW
			for _, st := range states {
				icon := iconstyle.Draw(iconstyle.View{
					Size:    size,
					Style:   style,
					Metric:  config.MetricRequests,
					Lang:    "en",
					Palette: v.palette,
					Snap:    st.snap,
					Paused:  st.paused,
				})
				paste(img, icon.Scale(iconPx, iconPx), x+gap, y+gap)
				x += cellW
			}
			if !compact {
				name := raster.New(labelW-cell, cellH)
				name.Text(0, 10, 1.7, style, ink)
				paste(img, name, cell, y)
			}
			y += cellH
		}
		label := raster.New(labelW-cell, cell*gap)
		label.Text(0, 8, 2.2, v.label, ink)
		paste(img, label, cell, blockTop-cell)
		y += cell * gap
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		return err
	}
	fmt.Printf("wrote %s (%dx%d): %d styles x %d variants x %d states\n",
		path, width, height, len(styles), len(variants), len(states))
	return nil
}

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
			bg := dst.RGBAAt(dx, dy)
			a := float64(p.A) / 255
			dst.SetRGBA(dx, dy, color.RGBA{
				R: uint8(float64(p.R)*a + float64(bg.R)*(1-a)),
				G: uint8(float64(p.G)*a + float64(bg.G)*(1-a)),
				B: uint8(float64(p.B)*a + float64(bg.B)*(1-a)),
				A: 0xff,
			})
		}
	}
}

func replaceExt(path, suffix string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '.' {
			return path[:i] + suffix
		}
		if path[i] == '\\' || path[i] == '/' {
			break
		}
	}
	return path + suffix
}

// sampleSnapshot builds a plausible reading so the icons are drawn with real
// numbers rather than zeros: a gauge at zero and a gauge at half are different
// drawings, and the sheet is meant to show the second.
func sampleSnapshot(ready, total int, credits int64, latency, tps float64, tokens int64, inflight int) status.Snapshot {
	accounts := make([]status.Account, 0, total)
	for i := 0; i < total; i++ {
		acct := status.Account{
			UID:      fmt.Sprintf("acct-%d", i),
			Nickname: fmt.Sprintf("account %d", i+1),
			Credits:  credits / int64(total),
			Total:    credits,
		}
		switch {
		case i >= ready && i < ready+1:
			acct.Cooling = true
			acct.CoolRemain = 420
		case i >= ready+1:
			acct.Disabled = true
		default:
			acct.InFlight = inflight / max(ready, 1)
		}
		accounts = append(accounts, acct)
	}
	series := make([]float64, 24)
	for i := range series {
		series[i] = 40 + float64((i*37)%90)
	}
	return status.Snapshot{
		Reachable: true,
		Healthy:   ready,
		Total:     total,
		Version:   "1.11.9-panel",
		Uptime:    7325,
		Accounts:  accounts,
		Usage: status.Usage{
			Requests:     128400,
			Errors:       913,
			TotalTokens:  tokens,
			AvgLatencyMs: latency,
			AvgTPS:       tps,
			Series:       series,
		},
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
