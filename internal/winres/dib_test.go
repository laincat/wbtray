package winres

import (
	"image"
	"image/color"
	"testing"
)

// The DIB encoder is the part of this package that Windows reads directly, and
// getting it wrong fails without any error: the icon simply does not appear. It
// therefore deserves to be checked on its own terms, independently of the
// resource tree around it.

// TestDIBRoundTrips proves the encoder and the decoder in the tests agree, which
// is what makes the read-back checks meaningful. A shared mistake between the two
// would make every test pass and the icon still be wrong, so the decoder reads
// the fields Windows reads and nothing else.
func TestDIBRoundTrips(t *testing.T) {
	const size = 16
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	// A diagonal gradient, so a flipped or transposed image is detectable.
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(x * 16), G: uint8(y * 16), B: 64, A: 255,
			})
		}
	}
	encoded, err := EncodeImage(img, size)
	if err != nil {
		t.Fatal(err)
	}
	back, err := decodeIconDIB(encoded.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if back.Bounds().Dx() != size || back.Bounds().Dy() != size {
		t.Fatalf("round trip returned %v", back.Bounds())
	}

	// Red increases to the right and green increases downward, so a transposed
	// or vertically flipped image shows up as a swapped channel.
	for _, p := range []struct{ x, y int }{{0, 0}, {size - 1, 0}, {0, size - 1}, {size - 1, size - 1}} {
		want := color.NRGBA{R: uint8(p.x * 16), G: uint8(p.y * 16), B: 64, A: 255}
		got := back.At(p.x, p.y).(color.NRGBA)
		if got != want {
			t.Errorf("pixel (%d,%d) is %+v, want %+v", p.x, p.y, got, want)
		}
	}
}

// TestDIBIsTopDownInTheFile checks the row order, which is the single most likely
// mistake in a DIB encoder and the hardest to see: an icon written bottom-up in a
// format that expects top-down is not rejected, it is drawn upside down.
func TestDIBIsTopDownInTheFile(t *testing.T) {
	const size = 8
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	// The top row is red, every other row is transparent.
	for x := 0; x < size; x++ {
		img.SetNRGBA(x, 0, color.NRGBA{R: 255, A: 255})
	}
	encoded, err := EncodeImage(img, size)
	if err != nil {
		t.Fatal(err)
	}
	// In the file, the first pixel row is the *bottom* of the picture, so the red
	// row is the last one.
	rows := encoded.Bytes[DIBHeaderSize:]
	firstPixel := rows[0:4]
	if firstPixel[3] != 0 {
		t.Errorf("the file's first pixel row has alpha %d; a bottom-up DIB should reach "+
			"the red row last", firstPixel[3])
	}
	last := rows[(size-1)*size*4:]
	if last[3] != 255 {
		t.Errorf("the file's last pixel row has alpha %d, want the red row", last[3])
	}
}

// TestDIBHeightIsDoubled checks the field Windows uses to find the AND mask.
func TestDIBHeightIsDoubled(t *testing.T) {
	for _, size := range []int{16, 32, 48, 64, 128} {
		img := image.NewNRGBA(image.Rect(0, 0, size, size))
		encoded, err := EncodeImage(img, size)
		if err != nil {
			t.Fatal(err)
		}
		got := int(int32(encoded.Bytes[8]) | int32(encoded.Bytes[9])<<8 |
			int32(encoded.Bytes[10])<<16 | int32(encoded.Bytes[11])<<24)
		if got != size*2 {
			t.Errorf("the %dx%d image's header says height %d, want %d so the AND mask is found",
				size, size, got, size*2)
		}
	}
}

// TestPNGIsUsedForTheLargestSizeOnly documents the one exception, so a change
// that made every size a DIB — and tripled the binary — would be noticed.
func TestPNGIsUsedForTheLargestSizeOnly(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	encoded, err := EncodeImage(img, 256)
	if err != nil {
		t.Fatal(err)
	}
	if !hasPNGSignature(encoded.Bytes) {
		t.Error("the 256x256 image is not a PNG; that is the one size where it should be, " +
			"and a DIB there is a quarter of a megabyte")
	}
	// And a large DIB is what the smaller sizes have to be.
	small, err := EncodeImage(img, 128)
	if err != nil {
		t.Fatal(err)
	}
	if hasPNGSignature(small.Bytes) {
		t.Error("the 128x128 image is a PNG, which Windows does not accept below 256")
	}
}

func hasPNGSignature(b []byte) bool {
	if len(b) < 8 {
		return false
	}
	return b[0] == 0x89 && b[1] == 'P' && b[2] == 'N' && b[3] == 'G'
}
