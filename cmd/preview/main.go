// Command preview renders the sheets used to choose a look: every style against
// every theme, at the sizes the taskbar actually uses.
//
// It is a development tool rather than part of the program. The tray draws its
// icons at run time, and this is a way to look at all of them at once instead of
// changing a setting and squinting at the notification area.
//
// Usage:
//
//	go run ./cmd/preview [-out design-preview.png]

// A second mode, -text, renders the text styles at every size the taskbar uses,
// which is the only way to judge whether a figure is legible: the whole point of
// those styles is the reading, and a reading that cannot be read is worse than
// the shape it replaced.
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
	"wbtray/internal/i18n"
	"wbtray/internal/iconstyle"
	"wbtray/internal/raster"
	"wbtray/internal/status"
	"wbtray/internal/theme"
)

func main() {
	out := flag.String("out", "preview.png", "where to write the sheet")
	size := flag.Int("size", 32, "icon size in pixels, as the taskbar draws it")
	scale := flag.Int("scale", 5, "magnification of each icon in the sheet")
	flag.Parse()

	if err := write(*out, *size, *scale); err != nil {
		fmt.Fprintf(os.Stderr, "preview: %v\n", err)
		os.Exit(1)
	}
}

func write(path string, size, scale int) error {
	// Both appearances of both palettes: the light rendering is a real mode an
	// operator can be in, and a sheet that showed only the dark one would not
	// show the thing most likely to be wrong.
	var variants []variantTheme
	for _, th := range theme.All() {
		variants = append(variants,
			variantTheme{th.Label("en") + " / dark", th},
			variantTheme{th.Label("en") + " / light", th.OnLight()})
	}
	styles := config.Styles

	// Three states, because an indicator that only looks right when everything
	// works is not an indicator.
	states := []struct {
		name string
		snap status.Snapshot
	}{
		{"ok", sampleSnapshot(3, 3, 48250, 91.5, 38.2, 1284, 12)},
		{"busy", sampleSnapshot(1, 4, 21400, 940, 6.5, 12480, 51)},
		{"down", status.Snapshot{Reachable: false}},
	}

	const (
		cell = 12
		gap  = 6
	)
	iconPx := size * scale
	cellW := iconPx + gap*2
	cellH := iconPx + gap*2

	// Layout: a name column, then one column per state, with a block of rows per
	// theme and a row per style the theme offers.
	const labelW = 132
	width := labelW + len(states)*cellW + cell
	height := cell*gap*2 + len(variants)*(len(styles)*cellH+cell*gap)
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	// A mid grey background: the sheet has to be readable on a light and a dark
	// surface at once, which is what the transparent themes need tested.
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{0x80, 0x84, 0x8c, 0xff}},
		image.Point{}, draw.Src)

	// The state headers, so the three columns are not a guessing game.
	for i, st := range states {
		head := raster.New(cellW, cell*gap)
		head.Text(4, 8, 1.8, strings.ToUpper(st.name),
			raster.RGBA{R: 0x14, G: 0x17, B: 0x1f, A: 0xff})
		paste(img, head, labelW+i*cellW, 0)
	}

	y := cell*gap + cell
	for _, v := range variants {
		th := v.theme
		blockTop := y
		for _, style := range styles {
			x := labelW
			for _, st := range states {
				icon := iconstyle.Draw(iconstyle.View{
					Size:   size,
					Style:  style,
					Metric: config.MetricRequests,
					Lang:   "en",
					Theme:  th,
					Snap:   st.snap,
				})
				big := icon.Scale(iconPx, iconPx)
				paste(img, big, x+gap, y+gap)
				x += cellW
			}
			// The style name, once per row.
			name := raster.New(labelW-cell, cellH)
			name.Text(0, 10, 1.7, style, raster.RGBA{R: 0x14, G: 0x17, B: 0x1f, A: 0xff})
			paste(img, name, cell, y)
			y += cellH
		}
		// The theme name, beside its own block of rows.
		label := raster.New(labelW-cell, cell*gap)
		label.Text(0, 8, 2.2, v.label, raster.RGBA{R: 0x0c, G: 0x0e, B: 0x14, A: 0xff})
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
	fmt.Printf("wrote %s (%dx%d): %d styles x %d palettes x %d states\n",
		path, width, height, len(styles), len(variants), len(states))

	// A second sheet at the size the taskbar actually uses, magnified, because an
	// icon that reads well at 32 pixels can still be mud at 16. It lands beside
	// the first one under a name derived from it, so the pair cannot drift apart
	// through a mistyped path.
	writeSmall(replaceExt(path, "-16px.png"), variants, styles, states[0].snap)
	return nil
}

// variantTheme is one entry of the sheet: a name for the row group and the
// palette it draws with. The two travel together because a sheet whose labels
// say "Neon" twice is a sheet nobody can read.
type variantTheme struct {
	label string
	theme theme.Theme
}

// writeSmall writes the same gallery at 16 pixels, magnified sixteen times, so
// the smallest rendering can be judged too.
func writeSmall(path string, variants []variantTheme, styles []string, snap status.Snapshot) {
	const (
		size  = 16
		scale = 16
		gap   = 4
		// Label bands. A sheet a person has to choose from has to say which row
		// is which: the icons are the point, but a grid of fifteen of them with
		// no names is a puzzle rather than a choice.
		labelW = 150
		labelH = 34
	)
	iconPx := size * scale
	cellW, cellH := iconPx+gap*2, iconPx+gap*2
	width := labelW + len(styles)*cellW
	height := labelH + len(variants)*cellH
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{0x7a, 0x7e, 0x86, 0xff}},
		image.Point{}, draw.Src)

	for r, v := range variants {
		th := v.theme
		label := raster.New(labelW-gap*2, labelH-8)
		label.Text(0, 0, 2, v.label, raster.RGBA{R: 0x14, G: 0x17, B: 0x1f, A: 0xff})
		paste(img, label, gap, labelH+r*cellH+8)

		for c, style := range styles {
			if r == 0 {
				name := raster.New(cellW, labelH-8)
				name.Text(4, 2, 1.6, style, raster.RGBA{R: 0x14, G: 0x17, B: 0x1f, A: 0xff})
				paste(img, name, labelW+c*cellW, 4)
			}
			icon := iconstyle.Draw(iconstyle.View{
				Size:   size,
				Style:  style,
				Metric: config.MetricRequests,
				Lang:   "en",
				Theme:  th,
				Snap:   snap,
			})
			paste(img, icon.Scale(iconPx, iconPx),
				labelW+c*cellW+gap, labelH+r*cellH+gap)
		}
	}
	// The path is already the small sheet's own name; the caller derived it.
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		return
	}
	fmt.Printf("wrote %s (%dx%d)\n", path, width, height)
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
			blend := color.RGBA{
				R: uint8(float64(p.R)*a + float64(bg.R)*(1-a)),
				G: uint8(float64(p.G)*a + float64(bg.G)*(1-a)),
				B: uint8(float64(p.B)*a + float64(bg.B)*(1-a)),
				A: 0xff,
			}
			dst.SetRGBA(dx, dy, blend)
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
	labels := make([]string, 24)
	for i := range labels {
		labels[i] = fmt.Sprintf("2026-09-26T%02d", i)
	}
	return status.Snapshot{
		Reachable: true,
		Healthy:   ready,
		Total:     total,
		Version:   "1.11.6-panel",
		Uptime:    7325,
		Accounts:  accounts,
		Usage: status.Usage{
			Requests:     128400,
			Errors:       913,
			TotalTokens:  tokens,
			AvgLatencyMs: latency,
			AvgTPS:       tps,
			Series:       series,
			Labels:       labels,
		},
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// keep the i18n import honest: the sheet is rendered in English, and a key that
// disappears would otherwise go unnoticed until the tray is run.
var _ = i18n.T
