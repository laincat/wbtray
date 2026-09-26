package winres

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
)

// TestBuiltExecutableCarriesTheIcon reads the resource directory out of a built
// wbtray.exe and checks the icon is reachable through it exactly as Windows would
// reach it.
//
// This is the end-to-end check the earlier bugs slipped past. The unit tests
// verify the section this package builds; this one verifies that the section
// survives the linker and lands in the executable's data directory, which is
// where it was previously dropped — first because the section was marked
// discardable, and then because the tree had no root. Neither produced an error;
// the executable simply had no icon.
//
// It is skipped unless WBTRAY_BUILT_EXE names an executable, because the test
// cannot build one itself without a source tree to build.
//
//	$env:WBTRAY_BUILT_EXE = "dist\wbtray.exe"
//	go test ./internal/winres -run TestBuiltExecutable -v
func TestBuiltExecutableCarriesTheIcon(t *testing.T) {
	path := os.Getenv("WBTRAY_BUILT_EXE")
	if path == "" {
		t.Skip("set WBTRAY_BUILT_EXE to the built executable")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	section, base := rsrcSection(t, b)
	root := readDir(t, section, 0)

	// The root has to name resource types. This is the check that fails when the
	// root directory is missing: the section then begins with a type directory,
	// and the root's entries are whatever that directory happened to hold.
	var iconNode, groupNode *entry
	for i := range root {
		switch root[i].id {
		case rtIcon:
			iconNode = &root[i]
		case rtGroupIcon:
			groupNode = &root[i]
		}
	}
	if groupNode == nil {
		t.Fatalf("the executable's resource root names %v, with no RT_GROUP_ICON; "+
			"the icon is not embedded", ids(root))
	}
	if iconNode == nil {
		t.Fatalf("the executable's resource root names %v, with no RT_ICON", ids(root))
	}

	// Walk down to the group's data the way the loader does: the group's type node
	// holds one name, that name holds a language, and the language names a data
	// entry holding the bytes.
	groupData := dataOf(t, section, *groupNode, base)
	if len(groupData) < 6 {
		t.Fatalf("the group icon's data is %d bytes, too short for a header", len(groupData))
	}
	count := int(binary.LittleEndian.Uint16(groupData[4:]))
	if count < 2 {
		t.Errorf("the group lists %d images; a taskbar icon should have several sizes", count)
	}

	// Every image the group names has to exist as an RT_ICON leaf and decode at
	// the size the group claims.
	images := map[uint32][]byte{}
	for _, nm := range readDir(t, section, int(iconNode.offset)) {
		images[nm.id] = dataOf(t, section, nm, base)
	}
	for i := 0; i < count; i++ {
		e := groupData[6+14*i:]
		id := uint32(binary.LittleEndian.Uint16(e[12:]))
		pixels, ok := images[id]
		if !ok {
			t.Errorf("the group refers to image %d, which the resource tree does not hold", id)
			continue
		}
		img, err := decodeIconImage(pixels)
		if err != nil {
			t.Errorf("image %d does not decode: %v", id, err)
			continue
		}
		want := int(e[0])
		if want == 0 {
			want = 256
		}
		if img.Bounds().Dx() != want {
			t.Errorf("the group says image %d is %d wide; the image is %d",
				id, want, img.Bounds().Dx())
		}
	}
}

// TestBuiltExecutableDataEntriesAreRelocated checks the field the linker is
// supposed to fill in.
//
// A resource data entry holds the address of the resource, and an address cannot
// be known until the linker has placed the section. The object therefore carries
// a section-relative offset plus a relocation, and a linked image must carry an
// address in the section's range instead. When that relocation is missing — or
// when it is applied to the directory entries rather than to the data entries —
// the tree still parses perfectly and every leaf points somewhere plausible that
// is wrong, so Windows shows its generic icon.
//
// The check is worth having because it is the one failure mode that looks right
// from every other angle.
func TestBuiltExecutableDataEntriesAreRelocated(t *testing.T) {
	path := os.Getenv("WBTRAY_BUILT_EXE")
	if path == "" {
		t.Skip("set WBTRAY_BUILT_EXE to the built executable")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	section, rva := rsrcSection(t, b)
	if rva == 0 {
		t.Fatal("the resource directory is at address zero")
	}

	checked := 0
	// Every type below the root, then every name, then the language level, which
	// is where the data entries are.
	for _, ty := range readDir(t, section, 0) {
		if !ty.isDir {
			continue
		}
		for _, name := range readDir(t, section, int(ty.offset)) {
			if !name.isDir {
				continue
			}
			for _, lang := range readDir(t, section, int(name.offset)) {
				if lang.isDir {
					t.Errorf("%s/%d's language entry is a directory, not a data entry",
						kindOf(ty.id), name.id)
					continue
				}
				e := readDataEntry(t, section, int(lang.offset))
				if e.size == 0 {
					t.Errorf("%s/%d's data entry is empty", kindOf(ty.id), name.id)
				}
				if e.address < rva || e.address+uint32(e.size) > rva+uint32(len(section)) {
					t.Errorf("%s/%d's data entry names %d bytes at %#x, outside the "+
						"resource section at %#x for %d bytes; the linker did not "+
						"relocate it", kindOf(ty.id), name.id, e.size, e.address,
						rva, len(section))
				}
				checked++
			}
		}
	}
	if checked == 0 {
		t.Fatal("the executable's tree holds no data entries")
	}
	t.Logf("checked %d data entries in a section at %#x for %d bytes", checked, rva, len(section))
}

// rsrcSection returns the .rsrc section of a PE image and the address it sits at.
//
// The address matters: a data entry inside the section holds a virtual address,
// so it can only be read by subtracting the section's own address from it.
func rsrcSection(t *testing.T, b []byte) ([]byte, uint32) {
	t.Helper()
	pe := int(binary.LittleEndian.Uint32(b[0x3C:]))
	magic := binary.LittleEndian.Uint16(b[pe+24:])
	dd := pe + 24 + 112
	if magic == 0x10b {
		dd = pe + 24 + 96
	}
	rva := binary.LittleEndian.Uint32(b[dd+2*8:])
	size := binary.LittleEndian.Uint32(b[dd+2*8+4:])
	if rva == 0 || size == 0 {
		t.Fatal("the executable has no resource directory")
	}

	// Map the address to a file offset through the section table.
	nsec := int(binary.LittleEndian.Uint16(b[pe+6:]))
	optSize := int(binary.LittleEndian.Uint16(b[pe+20:]))
	st := pe + 24 + optSize
	for i := 0; i < nsec; i++ {
		s := st + i*40
		vsize := binary.LittleEndian.Uint32(b[s+8:])
		va := binary.LittleEndian.Uint32(b[s+12:])
		raw := binary.LittleEndian.Uint32(b[s+20:])
		if rva >= va && rva < va+vsize {
			off := int(raw + (rva - va))
			if off+int(size) > len(b) {
				t.Fatalf("the resource directory runs past the file")
			}
			return b[off : off+int(size)], rva
		}
	}
	t.Fatal("the resource directory's address is not inside any section")
	return nil, 0
}

// dataOf follows a resource node down to the bytes it holds.
//
// It descends while the entries it finds are directories and stops at the one
// that points at a data entry, rather than assuming a fixed depth. That matters
// because the caller hands it either a type node or a name node depending on what
// it is looking for, and counting levels by hand is how this reader first went
// wrong: one readDir too many, and a few bytes of image decoded as a directory
// header claiming thousands of entries.
func dataOf(t *testing.T, section []byte, node entry, base uint32) []byte {
	t.Helper()
	cur := node
	// The format fixes the depth, so this cannot loop for ever; the bound also
	// turns a malformed section into a failure rather than a hang.
	for level := 0; level < 3; level++ {
		entries := readDir(t, section, int(cur.offset))
		if len(entries) == 0 {
			t.Fatalf("resource %d has an empty directory at level %d", node.id, level)
		}
		// Resources are addressed by type, name and language, so this walk has no
		// choice to make at any level.
		next := entries[0]
		if next.isDir {
			cur = next
			continue
		}
		return leafDataAt(t, section, next, base)
	}
	t.Fatalf("resource %d is nested deeper than a resource tree can be", node.id)
	return nil
}

// ids lists the resource types a root names, for a failure message.
func ids(root []entry) []uint32 {
	out := make([]uint32, 0, len(root))
	for _, e := range root {
		out = append(out, e.id)
	}
	return out
}

// TestBuiltExecutableIconIsNotBlank decodes the images out of a built executable
// and checks they carry the artwork rather than a flat colour.
//
// It is the check that distinguishes "the icon is embedded" from "the icon is the
// right icon". An executable whose resource tree is correct but whose images
// Windows cannot decode shows the system's generic application icon — blue
// rectangles on a white page — which is indistinguishable, in a file manager,
// from a binary that never had an icon at all.
func TestBuiltExecutableIconIsNotBlank(t *testing.T) {
	path := os.Getenv("WBTRAY_BUILT_EXE")
	if path == "" {
		t.Skip("set WBTRAY_BUILT_EXE to the built executable")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	section, base := rsrcSection(t, b)
	root := readDir(t, section, 0)
	var iconNode *entry
	for i := range root {
		if root[i].id == rtIcon {
			iconNode = &root[i]
		}
	}
	if iconNode == nil {
		t.Fatal("the executable has no RT_ICON")
	}

	checked := 0
	for _, nm := range readDir(t, section, int(iconNode.offset)) {
		img, err := decodeIconImage(dataOf(t, section, nm, base))
		if err != nil {
			t.Errorf("image %d does not decode: %v", nm.id, err)
			continue
		}
		colours := map[uint32]bool{}
		bounds := img.Bounds()
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				r, g, bl, a := img.At(x, y).RGBA()
				if a == 0 {
					continue
				}
				colours[uint32(r>>8)<<16|uint32(g>>8)<<8|uint32(bl>>8)] = true
			}
		}
		// The mascot is drawn in a palette with anti-aliased edges, so a real
		// image has dozens of colours. A blank or flat one has a handful, which
		// means the artwork did not survive the encoding.
		if len(colours) < 8 {
			t.Errorf("image %d has only %d distinct colours; the artwork did not survive",
				nm.id, len(colours))
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no images were checked")
	}
	t.Logf("checked %d images", checked)
}

// decodeIconImage decodes one icon image, PNG or DIB.
func decodeIconImage(data []byte) (image.Image, error) {
	if bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G'}) {
		return png.Decode(bytes.NewReader(data))
	}
	return decodeIconDIB(data)
}

// decodeIconDIB reads a 32-bit icon DIB back into an image, which is what Windows
// does with it.
func decodeIconDIB(data []byte) (image.Image, error) {
	if len(data) < DIBHeaderSize {
		return nil, fmt.Errorf("only %d bytes, too short for a DIB header", len(data))
	}
	w := int(int32(binary.LittleEndian.Uint32(data[4:])))
	h := int(int32(binary.LittleEndian.Uint32(data[8:])))
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("the header says %dx%d", w, h)
	}
	// The height covers the colour image and the AND mask stacked, so the picture
	// is half of it.
	half := h / 2
	if half != w {
		return nil, fmt.Errorf("the header is %dx%d, which is not a square icon with a mask", w, h)
	}
	rows := data[DIBHeaderSize:]
	if len(rows) < w*4*half {
		return nil, fmt.Errorf("the pixel data is %d bytes, want %d", len(rows), w*4*half)
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, half))
	// Bottom-up, as a DIB is.
	for y := 0; y < half; y++ {
		row := rows[(half-1-y)*w*4:]
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: row[x*4+2], G: row[x*4+1], B: row[x*4+0], A: row[x*4+3],
			})
		}
	}
	return img, nil
}
