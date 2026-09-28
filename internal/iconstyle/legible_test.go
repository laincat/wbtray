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

// TestIconSheet writes every style against every palette and tone, magnified, so
// the icons can be looked at rather than only measured.
//
// It is a test rather than a command because what it guards is a property: the
// styles exist to be read at a glance, and whether a given size is readable is a
// judgement that has to be made by eye at least once. It is skipped unless the
// output directory is named.
//
//	$env:WBTRAY_SHEET_DIR = "design"
//	go test ./internal/iconstyle -run TestIconSheet -v
func TestIconSheet(t *testing.T) {
	dir := os.Getenv("WBTRAY_SHEET_DIR")
	if dir == "" {
		t.Skip("set WBTRAY_SHEET_DIR to write the sheet")
	}

	snap := bigPool()
	sizes := []int{16, 20, 24, 32, 48}
	styles := config.Styles

	const (
		scale = 8
		pad   = 12
		label = 26
	)
	cell := sizes[len(sizes)-1]*scale + pad*2
	// Two tones for each of the two palettes.
	// Two palettes, each in two tones.
	const variants = 4
	width := cell*len(sizes) + pad*2
	height := label + variants*(label+len(styles)*cell+pad)
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	// A mid grey sheet, so an icon that only reads on one tone is visible as such.
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{0x86, 0x8a, 0x92, 0xff}},
		image.Point{}, draw.Src)

	y := label
	for _, p := range theme.All(theme.Accent{}) {
		for _, light := range []bool{false, true} {
			pal := p.On(light)
			head := raster.New(width-pad*2, label-6)
			head.Text(0, 4, 1.7, pal.Label("en")+" / dark="+boolText(!light),
				raster.RGBA{R: 0x18, G: 0x1b, B: 0x22, A: 0xff})
			pasteIcon(img, head, pad, y)
			y += label
			for _, style := range styles {
				x := pad
				for _, size := range sizes {
					icon := Draw(View{
						Size: size, Style: style, Metric: config.MetricRequests,
						Lang: "en", Palette: pal, Snap: snap,
					}).Scale(size*scale, size*scale)
					pasteIcon(img, icon, x+pad, y+pad)
					x += cell
				}
				name := raster.New(cell-pad, cell-label)
				name.Text(0, 4, 1.5, style, raster.RGBA{R: 0x18, G: 0x1b, B: 0x22, A: 0xff})
				pasteIcon(img, name, 1, y)
				y += cell
			}
			y += pad
		}
	}

	path := filepath.Join(dir, "icon-styles.png")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s (%dx%d)", path, width, height)
}

func boolText(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// pasteIcon composites a magnified icon onto the sheet.
//
// The transparent pixels are skipped rather than blended, so what shows through is
// the sheet's own grey — which is the point of the sheet: these icons are drawn
// with no background of their own and have to read against a tone nobody chose
// for them.
func pasteIcon(dst *image.RGBA, src *raster.Canvas, x, y int) {
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
