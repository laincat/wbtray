package cat

import (
	"testing"

	"wbtray/internal/raster"
)

// TestDrawPaintsInsideTheBox guards the two mistakes that make a mascot look
// wrong rather than missing: drawing nothing at all, and drawing outside the box
// it was given.
func TestDrawPaintsInsideTheBox(t *testing.T) {
	const size = 96
	c := raster.New(size, size)
	Draw(c, 8, 8, 80, 80, Palette{Fur: raster.RGBA{R: 0x4f, G: 0x90, B: 0xfd, A: 0xff}})

	painted := 0
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if c.At(x, y).A > 0 {
				painted++
			}
		}
	}
	if painted == 0 {
		t.Fatal("the cat was not drawn")
	}
	for x := 0; x < size; x++ {
		for y := 0; y < 8; y++ {
			if c.At(x, y).A > 0 {
				t.Fatalf("painted at y=%d, outside the box", y)
			}
		}
	}
}

// TestFaceIsKnockedOut checks that the eyes really are holes rather than dark
// spots: what shows through them has to be the colour behind the cat, which is
// what makes the tray's halo visible inside the face.
func TestFaceIsKnockedOut(t *testing.T) {
	const size = 200
	bg := raster.RGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xff}
	c := raster.New(size, size)
	c.Fill(bg)
	Draw(c, 0, 0, size, size, Palette{
		Fur:   raster.RGBA{R: 0xff, A: 0xff},
		Inner: bg,
	})

	boxW := Box.MaxX - Box.MinX
	scale := float64(size) / boxW
	// The left eye is at master (411, 446).
	mx := (411 - Box.MinX) * scale
	my := (446 - Box.MinY) * scale
	if got := c.At(int(mx), int(my)); got != bg {
		t.Fatalf("eye centre = %+v, want the background %+v", got, bg)
	}
	// The middle of the chest is fur, not background.
	chestX := (Master/2 - Box.MinX) * scale
	chestY := (720 - Box.MinY) * scale
	if got := c.At(int(chestX), int(chestY)); got.R != 0xff {
		t.Fatalf("chest = %+v, want the fur colour", got)
	}
}
