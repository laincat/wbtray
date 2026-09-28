// Command themesheet builds a comparison sheet: every palette, in both tones, at a
// size where the difference is visible.
//
// It is a development tool, and it exists because choosing between four greys from
// four separate files is not a choice anyone can make. The palettes are drawn by the
// same code the window uses.
//
// Usage:
//
//	go run ./cmd/themesheet -out design/themes.png
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

	"wbtray/internal/raster"
	"wbtray/internal/theme"
)

func main() {
	out := flag.String("out", "themes.png", "where to write the sheet")
	flag.Parse()

	const (
		cellW = 300
		cellH = 210
		pad   = 14
		head  = 34
		scale = 2 // the window is drawn at half size, then doubled for legibility
	)
	palettes := theme.Names
	cols := len(palettes)
	width := pad + cols*(cellW+pad)
	height := head + 2*(cellH+head) + pad

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{0x8a, 0x8d, 0x94, 0xff}},
		image.Point{}, draw.Src)

	for col, name := range palettes {
		x := pad + col*(cellW+pad)
		title := raster.New(cellW, 16)
		title.Text(0, 0, 1.4, name, raster.RGBA{R: 0x18, G: 0x1a, B: 0x1e, A: 0xff})
		paste(img, title, x, 8)
	}
	for row, light := range []bool{false, true} {
		y := head + row*(cellH+head)
		for col, name := range palettes {
			pal := theme.ForName(name, light)
			c := sample(pal, cellW*scale, cellH*scale)
			small := c.Scale(cellW, cellH)
			x := pad + col*(cellW+pad)
			paste(img, small, x, y)
		}
		label := raster.New(120, 16)
		label.Text(0, 0, 1.3, toneName(light), raster.RGBA{R: 0x18, G: 0x1a, B: 0x1e, A: 0xff})
		paste(img, label, 2, y+cellH/2)
	}

	if dir := filepath.Dir(*out); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "themesheet: %v\n", err)
			os.Exit(1)
		}
	}
	f, err := os.Create(*out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "themesheet: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		fmt.Fprintf(os.Stderr, "themesheet: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%dx%d, %d palettes x 2 tones)\n", *out, width, height, len(palettes))
}

func toneName(light bool) string {
	if light {
		return "light"
	}
	return "dark"
}

// sample draws the parts of the palette a person actually judges it by: the rail, a
// card on the window, the text at its three levels, the state colours and the
// accent. It is a swatch rather than a page because the question is the colours, and
// a page would bury them in layout.
func sample(p theme.Palette, w, h int) *raster.Canvas {
	c := raster.New(w, h)
	railW := w / 4
	c.Fill(p.BG)
	c.Rect(0, 0, float64(railW), float64(h), p.Rail)

	// A card, its hairline, and the two raised steps inside it.
	const m = 18
	cardX, cardY := float64(railW)+m, float64(m)
	cardW, cardH := float64(w-railW)-2*m, float64(h)-2*m
	c.RoundedRect(cardX, cardY, cardW, cardH, 12, p.Surface)
	c.Rect(cardX+12, cardY+12, cardW-24, 1, p.Border)
	c.RoundedRect(cardX+12, cardY+26, cardW-24, 34, 8, p.Raised)
	c.RoundedRect(cardX+12, cardY+68, cardW-24, 34, 8, p.RaisedHi)

	// The text levels and the states, as swatches down the rail.
	y := float64(m)
	for _, sw := range []struct {
		col   raster.RGBA
		label string
	}{
		{p.Text, "text"}, {p.Muted, "muted"}, {p.Faint, "faint"},
		{p.Accent, "accent"}, {p.Green, "green"}, {p.Warn, "warn"},
		{p.Bad, "bad"}, {p.Blue, "blue"},
	} {
		c.RoundedRect(10, y, 8, 8, 2, sw.col)
		label := raster.New(railW-24, 14)
		label.Text(0, 0, 1.0, sw.label, p.Muted)
		c.DrawCanvas(label, 24, int(y), 1)
		y += 15
	}
	return c
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
