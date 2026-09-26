package winres

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"os"
	"testing"
)

// TestBuiltExecutableCarriesTheIcon reads the resource directory out of a built
// wbtray.exe and checks the icon is reachable through it exactly as Windows
// would reach it.
//
// This is the end-to-end check the earlier bug slipped past. The unit tests below
// verify the section this package builds; this one verifies that the section
// survives the linker and lands in the executable's data directory, which is
// where it was previously dropped — first because the section was marked
// discardable, and then because the tree had no root. Neither produced an error;
// the executable simply had no icon.
//
// It is skipped unless WBTRAY_BUILT_EXE names an executable, because the test
// cannot build one itself without a Go toolchain on the path and a source tree
// to build.
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

	section := rsrcSection(t, b)
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

	// Walk down to the group's data the way the loader does: the group's node
	// holds one name, that name holds a language, and the language's single entry
	// points at the bytes.
	groupData := dataOf(t, section, *groupNode)
	if len(groupData) < 6 {
		t.Fatalf("the group icon's data is %d bytes, too short for a header", len(groupData))
	}
	count := int(binary.LittleEndian.Uint16(groupData[4:]))
	if count < 2 {
		t.Errorf("the group lists %d images; a taskbar icon should have several sizes", count)
	}

	// Every image the group names has to exist as an RT_ICON leaf and decode as
	// the PNG it was written as.
	images := map[uint32][]byte{}
	for _, nm := range readDir(t, section, int(iconNode.offset)) {
		images[nm.id] = dataOf(t, section, nm)
	}
	for i := 0; i < count; i++ {
		e := groupData[6+14*i:]
		id := uint32(binary.LittleEndian.Uint16(e[12:]))
		pixels, ok := images[id]
		if !ok {
			t.Errorf("the group refers to image %d, which the resource tree does not hold", id)
			continue
		}
		img, err := png.Decode(bytes.NewReader(pixels))
		if err != nil {
			t.Errorf("image %d does not decode as a PNG: %v", id, err)
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

// rsucSection returns the .rsrc section of a PE image.
func rsrcSection(t *testing.T, b []byte) []byte {
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

	// Map the RVA to a file offset through the section table.
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
			return b[off : off+int(size)]
		}
	}
	t.Fatal("the resource directory's address is not inside any section")
	return nil
}

// dataOf follows a resource node down to the bytes it holds.
//
// A node's children are directories until the last level: a type holds names, a
// name holds languages, and a language holds the single entry that points at the
// data. Reading a language's entry as if it were another directory is how the
// first version of this reader failed.
// dataOf follows a resource node down to the bytes it holds.
//
// It descends while the entries it finds are directories and stops at the one
// that points at data, rather than assuming a fixed depth. That matters because
// the caller hands it either a type node or a name node depending on what it is
// looking for, and counting levels by hand is how this reader first went wrong:
// one readDir too many, and a few bytes of PNG decoded as a directory header
// claiming 39565 entries.
func dataOf(t *testing.T, section []byte, node entry) []byte {
	t.Helper()
	cur := node
	// The format fixes the depth at four levels below the root, so this cannot
	// loop for ever; the bound also turns a malformed section into a failure
	// rather than a hang.
	for level := 0; level < 4; level++ {
		entries := readDir(t, section, int(cur.offset))
		if len(entries) == 0 {
			t.Fatalf("resource %d has an empty directory at level %d", node.id, level)
		}
		// One child per level: resources are addressed by type, name and
		// language, and this walk has no choice to make.
		next := entries[0]
		if !next.isDir {
			start := next.offset
			// The leaf's length is the next leaf's offset: the resource
			// directory does not record it, and Windows reads it the same way.
			end := uint32(len(section))
			if after := findNextLeaf(section, start); after > start {
				end = after
			}
			if int(start) > len(section) || int(end) > len(section) || end < start {
				t.Fatalf("resource %d's data runs past the section", node.id)
			}
			return section[start:end]
		}
		cur = next
	}
	t.Fatalf("resource %d is nested deeper than a resource tree can be", node.id)
	return nil
}

// findNextLeaf returns the lowest data offset above off, which bounds a leaf
// whose length is not recorded.
func findNextLeaf(section []byte, off uint32) uint32 {
	best := uint32(len(section))
	// The walk is bounded by depth because only three levels of the tree are
	// directories. Below that an entry points at data, and reading that data as
	// though it were another directory is how this went wrong the first time: a
	// few bytes of PNG decoded as an entry count and the walk ran off the end.
	var walk func(nodeOff, depth int)
	walk = func(nodeOff, depth int) {
		if depth >= 3 || nodeOff+16 > len(section) {
			return
		}
		named := binary.LittleEndian.Uint16(section[nodeOff+12:])
		ids := binary.LittleEndian.Uint16(section[nodeOff+14:])
		for i := 0; i < int(named)+int(ids); i++ {
			at := nodeOff + 16 + 8*i
			if at+8 > len(section) {
				return
			}
			e := section[at:]
			addr := binary.LittleEndian.Uint32(e[4:])
			target := addr & 0x7fffffff
			if addr&0x80000000 != 0 {
				walk(int(target), depth+1)
				continue
			}
			if target > off && target < best {
				best = target
			}
		}
	}
	walk(0, 0)
	return best
}

// ids lists the resource types a root names, for a failure message.
func ids(root []entry) []uint32 {
	out := make([]uint32, 0, len(root))
	for _, e := range root {
		out = append(out, e.id)
	}
	return out
}
