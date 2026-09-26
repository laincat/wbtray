package winres

import (
	"bytes"
	"encoding/binary"
	"image"
	"testing"
)

// TestSmallImagesAreNotPNG is a rule about the format rather than about taste.
//
// Windows accepts a PNG-compressed image in an icon resource for one size only:
// 256x256. Every smaller image has to be a DIB, and a resource that breaks this
// is not rejected — Explorer, the taskbar and the alt-tab list simply fall back
// to the system's generic icon, which looks exactly like an icon that was never
// embedded at all. That is where this project spent a while: the resource tree
// was correct, the images were present, and the file still showed no icon.
//
// The check is on the bytes rather than on a flag because the bytes are what
// Windows reads: a DIB starts with a BITMAPINFOHEADER whose first field is 40,
// and a PNG starts with the PNG signature.
func TestSmallImagesAreNotPNG(t *testing.T) {
	pngSignature := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}

	for _, size := range DefaultSizes {
		data := encodeIconImage(t, size)
		isPNG := bytes.HasPrefix(data, pngSignature)
		if size >= 256 {
			// The one size where PNG is allowed, and the reason it is worth
			// using: a 256x256 DIB is 256 KB against a few for a PNG.
			continue
		}
		if isPNG {
			t.Errorf("the %dx%d image is a PNG; Windows only accepts PNG at 256x256 "+
				"and silently shows its generic icon for the rest", size, size)
		}
		// And a DIB has to start the way a DIB does.
		if len(data) < 4 {
			t.Errorf("the %dx%d image is %d bytes", size, size, len(data))
			continue
		}
		if got := binary.LittleEndian.Uint32(data[0:]); got != 40 {
			t.Errorf("the %dx%d image starts with %d, want a 40-byte BITMAPINFOHEADER",
				size, size, got)
		}
		// The header's own width field has to agree with the box, and its height
		// has to be twice that: a DIB icon carries the XOR image and the AND mask
		// stacked.
		if w := int32(binary.LittleEndian.Uint32(data[4:])); w != int32(size) {
			t.Errorf("the %dx%d image's header says %d wide", size, size, w)
		}
		if h := int32(binary.LittleEndian.Uint32(data[8:])); h != int32(size*2) {
			t.Errorf("the %dx%d image's header says %d tall, want %d (image and mask)",
				size, size, h, size*2)
		}
	}
}

// encodeIconImage returns the bytes this package writes for one size.
func encodeIconImage(t *testing.T, size int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	encoded, err := EncodeImage(img, size)
	if err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes
}
