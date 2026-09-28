// Command iconsheet writes the program's icon marks to PNG, so they can be looked at
// and chosen between without building an executable and finding it in Explorer.
//
// It is a development tool: the marks are drawn by the same code the release uses,
// and this is a way to see them at the sizes a person actually meets them.
//
// Usage:
//
//	go run ./cmd/iconsheet -out design/appmarks.png
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
	out := flag.String("out", "appmarks.png", "where to write the sheet")
	only := flag.String("variant", "", "draw one variant, scaled large; empty draws every one")
	flag.Parse()

	if *only != "" {
		if err := bigSheet(*out, *only); err != nil {
			fmt.Fprintf(os.Stderr, "iconsheet: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("wrote %s\n", *out)
		return
	}
	if err := sheet(*out); err != nil {
		fmt.Fprintf(os.Stderr, "iconsheet: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s\n", *out)
}

// sheet is every variant against the three sizes Windows actually draws: the 16 the
// taskbar and the explorer list use, the 32 the explorer's icon view uses, and the
// 256 the large-thumbnail view uses.
func sheet(path string) error {
	sizes := []int{16, 32, 256}
	scale := map[int]int{16: 6, 32: 4, 256: 1}
	variants := iconstyle.AppVariants

	const (
		pad     = 18
		labelW  = 96
		labelH  = 26
		rowGap  = 16
		headGap = 8
	)
	rowH := labelH
	for _, s := range sizes {
		if h := s*scale[s] + pad; h > rowH {
			rowH = h
		}
	}
	width := labelW + len(sizes)*(256+pad) + pad
	height := headGap + len(variants)*(rowH+rowGap) + pad

	// Two backgrounds, because the plate has to read on the light desktop it will
	// mostly sit on and on the dark one beside it.
	img := image.NewRGBA(image.Rect(0, 0, width, height*2))
	draw.Draw(img, image.Rect(0, 0, width, height),
		&image.Uniform{color.RGBA{0xf3, 0xf3, 0xf3, 0xff}}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(0, height, width, height*2),
		&image.Uniform{color.RGBA{0x1f, 0x1f, 0x1f, 0xff}}, image.Point{}, draw.Src)

	for row := 0; row < 2; row++ {
		y0 := row*height + headGap
		for _, v := range variants {
			bottom := y0 + rowH
			x := labelW
			for _, s := range sizes {
				c := iconstyle.DrawAppMark(s, v).Scale(s*scale[s], s*scale[s])
				// Bottom-aligned within the row, so the sizes read as a row of the
				// same thing rather than a ragged stack.
				paste(img, c, x, bottom-s*scale[s])
				x += 256 + pad
			}
			label := raster.New(labelW-pad, labelH)
			label.Text(0, 2, 1.5, v, raster.RGBA{R: 0x30, G: 0x33, B: 0x3a, A: 0xff})
			paste(img, label, pad, bottom-labelH)
			y0 += rowH + rowGap
		}
	}
	return save(path, img)
}

// bigSheet draws one variant at the sizes an icon actually gets drawn at, magnified,
// so the geometry can be judged rather than guessed at.
func bigSheet(path, variant string) error {
	sizes := []int{16, 20, 24, 32, 48, 64, 128, 256}
	const (
		pad = 20
		top = 30
	)
	var width int
	at := make([]int, len(sizes))
	for i, s := range sizes {
		at[i] = width + pad
		width += s + pad
	}
	height := top + 256 + pad
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{0x86, 0x8a, 0x92, 0xff}},
		image.Point{}, draw.Src)

	for i, s := range sizes {
		c := iconstyle.DrawAppMark(s, variant)
		paste(img, c, at[i], top+(256-s)/2)
		label := raster.New(60, 12)
		label.Text(0, 0, 1.0, fmt.Sprintf("%d", s),
			raster.RGBA{R: 0x20, G: 0x23, B: 0x28, A: 0xff})
		paste(img, label, at[i], 6)
	}
	return save(path, img)
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

// paste blends a canvas onto the sheet, so the plate's rounded corners show the
// background through them — which is the thing being judged.
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
