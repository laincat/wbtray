package iconstyle

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"wbtray/internal/config"
	"wbtray/internal/raster"
	"wbtray/internal/theme"
)

// TestFigureLegibilitySheet writes the text styles at every size a taskbar uses,
// magnified, so the figures can be looked at rather than only measured.
//
// It is a test rather than a command because what it guards is a property: the
// figure styles exist to be read, and whether a given size is readable is a
// judgement that has to be made by eye at least once. It is skipped unless the
// output directory is named.
//
//	$env:WBTRAY_SHEET_DIR = "design"
//	go test ./internal/iconstyle -run TestFigureLegibilitySheet -v
func TestFigureLegibilitySheet(t *testing.T) {
	dir := os.Getenv("WBTRAY_SHEET_DIR")
	if dir == "" {
		t.Skip("set WBTRAY_SHEET_DIR to write the legibility sheet")
	}

	snap := bigPool()
	sizes := []int{16, 20, 24, 32}
	styles := []string{config.StyleText, config.StyleBarText, config.StyleMascotText}
	metrics := config.Metrics

	const (
		scale = 10 // magnification in the sheet
		pad   = 14
	)
	cell := sizes[len(sizes)-1]*scale + pad*2
	img := newSheet(cell*len(sizes)+pad, cell*len(metrics)*len(styles)+pad, theme.Neon().Halo)

	y := pad
	for _, style := range styles {
		for _, metric := range metrics {
			x := pad
			for _, size := range sizes {
				v := View{Size: size, Style: style, Metric: metric, Lang: "en", Theme: theme.Neon(), Snap: snap}
				icon := Draw(v).Scale(size*scale, size*scale)
				pasteIcon(img, icon, x, y)
				x += cell
			}
			y += cell
		}
	}

	path := filepath.Join(dir, "figure-legibility.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s", path)
}

// newSheet allocates the sheet's background.
func newSheet(w, h int, bg raster.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{toColor(bg)}, image.Point{}, draw.Src)
	return img
}

// pasteIcon composites a magnified icon onto the sheet.
//
// The sheet's background is the palette's halo, so the figures are judged against
// the colour they will actually sit on in a light taskbar rather than against
// whatever a screenshot happened to capture.
func pasteIcon(dst *image.RGBA, src *raster.Canvas, x, y int) {
	for j := 0; j < src.H; j++ {
		for i := 0; i < src.W; i++ {
			p := src.At(i, j)
			if p.A == 0 {
				continue
			}
			dx, dy := x+i, y+j
			if dx >= dst.Rect.Dx() || dy >= dst.Rect.Dy() {
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

func toColor(c raster.RGBA) color.RGBA { return color.RGBA{c.R, c.G, c.B, c.A} }
