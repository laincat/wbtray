package winres

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
)

// Encoding icon images.
//
// An icon resource holds its images in one of two formats, and which one is
// allowed depends on the size:
//
//   - 256x256 may be a PNG. It usually should be: a 256x256 uncompressed image
//     is a quarter of a megabyte, and several of those in one executable is a
//     size nobody wants to ship.
//   - Every smaller size must be a DIB — a BITMAPINFOHEADER followed by pixels,
//     with an AND mask under them.
//
// This is a rule about the format rather than a preference, and breaking it fails
// quietly. Windows does not reject an icon whose 32x32 image is a PNG: Explorer,
// the taskbar and the alt-tab list simply fall back to the generic application
// icon, which looks exactly like an executable that has no icon at all.

// iconImage is one image ready to be packed into a resource.
type iconImage struct {
	// Size is the image's edge in pixels.
	Size int
	// Bytes is the encoded image, PNG or DIB.
	Bytes []byte
}

// DefaultSizes are the image sizes an application icon should carry. They cover
// the taskbar at every display scale Windows uses, the alt-tab list, and the
// large views in Explorer.
var DefaultSizes = []int{16, 20, 24, 32, 48, 64, 128, 256}

// EncodeImage encodes one image at one size, choosing the format the size
// requires.
func EncodeImage(img image.Image, size int) (iconImage, error) {
	if size <= 0 {
		return iconImage{}, fmt.Errorf("winres: image size %d", size)
	}
	if img == nil {
		return iconImage{}, fmt.Errorf("winres: no image for size %d", size)
	}
	if size >= 256 {
		// The one size PNG is for.
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return iconImage{}, fmt.Errorf("winres: encoding %dx%d png: %w", size, size, err)
		}
		return iconImage{Size: size, Bytes: buf.Bytes()}, nil
	}
	return iconImage{Size: size, Bytes: encodeDIB(img, size)}, nil
}

// DIBLayout is the header and mask sizes of an icon DIB, exposed so the two
// places that need the arithmetic — the encoder and its test — use one copy.
const (
	// DIBHeaderSize is sizeof(BITMAPINFOHEADER).
	DIBHeaderSize = 40
	// AndMaskRowBytes is the stride of the AND mask: one bit per pixel, padded
	// to a four-byte boundary.
	andMaskRowBytes = 4
)

// encodeDIB writes an icon DIB: a BITMAPINFOHEADER, the colour image bottom-up,
// then the AND mask.
func encodeDIB(img image.Image, size int) []byte {
	// The height is doubled because a DIB icon holds two images stacked: the XOR
	// image (the colour) and the AND mask (the transparency). Windows reads the
	// height to know how much of the data is which.
	const header = DIBHeaderSize
	xorStride := size * 4
	maskStride := ((size + 31) / 32) * andMaskRowBytes
	total := header + xorStride*size + maskStride*size

	out := make([]byte, total)
	binary.LittleEndian.PutUint32(out[0:], header)                  // biSize
	binary.LittleEndian.PutUint32(out[4:], uint32(size))            // biWidth
	binary.LittleEndian.PutUint32(out[8:], uint32(size*2))          // biHeight: image + mask
	binary.LittleEndian.PutUint16(out[12:], 1)                      // biPlanes
	binary.LittleEndian.PutUint16(out[14:], 32)                     // biBitCount
	binary.LittleEndian.PutUint32(out[16:], 0)                      // biCompression: BI_RGB
	binary.LittleEndian.PutUint32(out[20:], uint32(xorStride*size)) // biSizeImage

	// The colour image, bottom-up: a DIB's first row is the bottom of the
	// picture, which is the opposite of every other raster in this program.
	bounds := img.Bounds()
	for y := 0; y < size; y++ {
		srcY := bounds.Min.Y + (size - 1 - y)
		row := out[header+y*xorStride:]
		for x := 0; x < size; x++ {
			// The image is scaled to the icon size by the caller, so a sample
			// outside it means the caller passed the wrong size; clamping keeps
			// that from reading out of bounds.
			srcX := bounds.Min.X + x
			if srcX >= bounds.Max.X {
				srcX = bounds.Max.X - 1
			}
			if srcY < bounds.Min.Y {
				srcY = bounds.Min.Y
			}
			r, g, b, a := img.At(srcX, srcY).RGBA()
			// Premultiplied BGRA, which is what a 32-bit DIB with alpha holds.
			row[x*4+0] = uint8(b >> 8)
			row[x*4+1] = uint8(g >> 8)
			row[x*4+2] = uint8(r >> 8)
			row[x*4+3] = uint8(a >> 8)
		}
	}

	// The AND mask stays zero — fully opaque — because the alpha channel above
	// carries the transparency. Windows uses the mask only for images without
	// alpha, and an all-zero mask is what every modern icon carries.
	return out
}

// EncodeIconData encodes a set of images into the form the resource builder
// takes, in the order given.
//
// The directory's width and height fields are one byte each and encode 256 as
// zero, which is the one place this format fails to represent its own range.
func EncodeIconData(images map[int]image.Image, sizes []int) ([]IconData, error) {
	out := make([]IconData, 0, len(sizes))
	for _, size := range sizes {
		img, ok := images[size]
		if !ok {
			return nil, fmt.Errorf("winres: no image for size %d", size)
		}
		encoded, err := EncodeImage(img, size)
		if err != nil {
			return nil, err
		}
		out = append(out, IconData{
			Width:  byteOf(size),
			Height: byteOf(size),
			Bytes:  encoded.Bytes,
		})
	}
	return out, nil
}

// byteOf encodes a dimension the way an icon directory does.
func byteOf(size int) byte {
	if size >= 256 {
		return 0
	}
	return byte(size)
}
