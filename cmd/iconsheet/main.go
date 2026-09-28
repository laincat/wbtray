// Command iconsheet writes the icon marks to PNG, so they can be looked at
// without building an executable and finding it in Explorer.
//
// It is a development tool: the marks are drawn by the same code the release uses,
// and this is a way to see them at the sizes a person actually meets them.
//
// Usage:
//
//	go run ./cmd/iconsheet -out design/appmark.png
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

	"wbtray/internal/iconstyle"
	"wbtray/internal/raster"
)

func main() {
	out := flag.String("out", "appmark.png", "where to write the sheet")
	flag.Parse()

	sizes := []int{16, 24, 32, 48, 64, 128, 256}
	scale := map[int]int{16: 8, 24: 6, 32: 5, 48: 4, 64: 3, 128: 2, 256: 1}

	const pad = 16
	width := pad
	for _, s := range sizes {
		width += s*scale[s] + pad
	}
	height := 256 + pad*2

	// Two backgrounds, because the mark is drawn on a plate and has to read on the
	// light desktop it will mostly sit on and on the dark one beside it.
	img := image.NewRGBA(image.Rect(0, 0, width, height*2))
	draw.Draw(img, image.Rect(0, 0, width, height),
		&image.Uniform{color.RGBA{0xf3, 0xf3, 0xf3, 0xff}}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(0, height, width, height*2),
		&image.Uniform{color.RGBA{0x1f, 0x1f, 0x1f, 0xff}}, image.Point{}, draw.Src)

	for row := 0; row < 2; row++ {
		x := pad
		for _, s := range sizes {
			c := iconstyle.DrawAppMark(s).Scale(s*scale[s], s*scale[s])
			paste(img, c, x, height*row+pad+(256-s*scale[s])/2)
			x += s*scale[s] + pad
		}
	}

	if dir := filepath.Dir(*out); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "iconsheet: %v\n", err)
			os.Exit(1)
		}
	}
	f, err := os.Create(*out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "iconsheet: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		fmt.Fprintf(os.Stderr, "iconsheet: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%dx%d)\n", *out, width, height*2)
}

// paste composites a canvas onto the sheet, blending its alpha so the mark's own
// rounded corners show the background through them.
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
