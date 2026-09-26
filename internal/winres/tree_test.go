package winres

import (
	"encoding/binary"
	"fmt"
	"testing"
)

// A resource directory is a tree of three levels — type, then name or id, then
// language — and what the third level points at is not more directory but a
// 16-byte IMAGE_RESOURCE_DATA_ENTRY. Windows walks the tree by following offsets,
// and getting either half wrong produces a file whose resources exist and cannot
// be found, which is worse than producing no resources at all: the icon is
// silently missing and nothing reports an error.
//
// These tests parse the section this package builds and assert both halves, which
// is the only way to catch an error the format does not report. Earlier versions
// emitted the id in the type position, omitted the root, and pointed the last
// level straight at the bytes; every one of those shipped without an icon.

// entry is one 8-byte directory entry.
type entry struct {
	id     uint32
	offset uint32
	isDir  bool
}

// readDir parses a resource directory at an offset and returns its entries.
func readDir(t *testing.T, b []byte, off int) []entry {
	t.Helper()
	if off+16 > len(b) {
		t.Fatalf("directory at %d runs past the section", off)
	}
	named := binary.LittleEndian.Uint16(b[off+12:])
	ids := binary.LittleEndian.Uint16(b[off+14:])
	total := int(named) + int(ids)
	if off+16+8*total > len(b) {
		t.Fatalf("directory at %d claims %d entries, which runs past the section", off, total)
	}
	out := make([]entry, 0, total)
	for i := 0; i < total; i++ {
		e := b[off+16+8*i:]
		addr := binary.LittleEndian.Uint32(e[4:])
		out = append(out, entry{
			id:     binary.LittleEndian.Uint32(e[0:]),
			offset: addr & 0x7fffffff,
			isDir:  addr&0x80000000 != 0,
		})
	}
	return out
}

// dataEntry is an IMAGE_RESOURCE_DATA_ENTRY: the record the language level points
// at, holding where the bytes are and how long they are.
type dataEntry struct {
	address uint32
	size    uint32
}

// readDataEntry parses the record at an offset.
func readDataEntry(t *testing.T, b []byte, off int) dataEntry {
	t.Helper()
	if off+dataEntryLen > len(b) {
		t.Fatalf("data entry at %d runs past the section", off)
	}
	e := b[off:]
	return dataEntry{
		address: binary.LittleEndian.Uint32(e[0:]),
		size:    binary.LittleEndian.Uint32(e[4:]),
	}
}

// leafData follows the last directory level to the bytes it names, for a section
// that is not yet linked.
//
// The address in a data entry is a virtual address. In the section alone it is
// still the offset the builder wrote, because no linker has placed it yet, which
// is why these tests can read it directly; the linked-image tests subtract the
// section's own address instead.
//
// The base is subtracted only when one is given, so this reads both the object
// the builder produces and a linked image.
func leafData(t *testing.T, b []byte, lang entry) []byte {
	t.Helper()
	return leafDataAt(t, b, lang, 0)
}

// leafDataAt follows the last directory level to the bytes it names, given the
// address the section sits at so a relocated data entry can be resolved too.
func leafDataAt(t *testing.T, b []byte, lang entry, base uint32) []byte {
	t.Helper()
	if lang.isDir {
		t.Fatalf("the language level at %d is a directory, not a data entry", lang.offset)
	}
	e := readDataEntry(t, b, int(lang.offset))
	start := e.address - base
	if e.address < base || int(start) > len(b) || int(start)+int(e.size) > len(b) {
		t.Fatalf("data entry at %d names %d bytes at %#x, which is outside the %d-byte "+
			"section at %#x", lang.offset, e.size, e.address, len(b), base)
	}
	return b[start : start+e.size]
}

// tree renders the section as a nested description, so a failure prints what was
// actually built rather than only what was expected.
func tree(t *testing.T, b []byte) string {
	t.Helper()
	out := ""
	for _, ty := range readDir(t, b, 0) {
		out += fmt.Sprintf("type %d (%s)\n", ty.id, kindOf(ty.id))
		if !ty.isDir {
			out += "  <not a directory>\n"
			continue
		}
		for _, name := range readDir(t, b, int(ty.offset)) {
			out += fmt.Sprintf("  name %d\n", name.id)
			if !name.isDir {
				out += "    <not a directory>\n"
				continue
			}
			for _, lang := range readDir(t, b, int(name.offset)) {
				out += fmt.Sprintf("    lang %d\n", lang.id)
				if lang.isDir {
					out += "      <DIRECTORY, want a data entry>\n"
					continue
				}
				e := readDataEntry(t, b, int(lang.offset))
				out += fmt.Sprintf("      data: %d bytes at %d\n", e.size, e.address)
			}
		}
	}
	return out
}

// kindOf names the standard resource types, so a failure is readable.
func kindOf(id uint32) string {
	switch id {
	case rtIcon:
		return "RT_ICON"
	case rtGroupIcon:
		return "RT_GROUP_ICON"
	}
	return fmt.Sprintf("type %d", id)
}

// TestIconResourcesAreATypeNameLanguageTree checks the shape of the tree the icon
// resources are stored in.
//
// The levels are: a root naming the resource types, then the names or ids within
// each type, then the language, and the language holds the offset of a data entry
// rather than of the bytes. The root is the one that is easy to leave out — a
// section that starts with a type directory has Windows read that as the root,
// and the resources become unreachable without anything reporting an error.
func TestIconResourcesAreATypeNameLanguageTree(t *testing.T) {
	images := make([]IconData, 3)
	for i := range images {
		images[i] = IconData{Width: byte(16 * (i + 1)), Height: byte(16 * (i + 1)), Bytes: []byte{1, 2, 3, 4}}
	}
	section, err := BuildIconResources(images, 1)
	if err != nil {
		t.Fatal(err)
	}

	types := readDir(t, section, 0)
	if len(types) != 2 {
		t.Fatalf("the section has %d resource types, want 2 (RT_ICON and RT_GROUP_ICON):\n%s",
			len(types), tree(t, section))
	}

	byType := map[uint32][]entry{}
	for _, ty := range types {
		byType[ty.id] = readDir(t, section, int(ty.offset))
	}

	icons, ok := byType[rtIcon]
	if !ok {
		t.Fatalf("no RT_ICON (%d) type; the tree is:\n%s", rtIcon, tree(t, section))
	}
	if len(icons) != len(images) {
		t.Errorf("RT_ICON holds %d images, want %d; the tree is:\n%s", len(icons), len(images), tree(t, section))
	}
	// The images are numbered from one, and the group refers to them by those
	// numbers, so the numbering is part of the contract rather than a detail.
	for i, e := range icons {
		if e.id != uint32(i+1) {
			t.Errorf("RT_ICON entry %d has id %d, want %d", i, e.id, i+1)
		}
	}

	groups, ok := byType[rtGroupIcon]
	if !ok {
		t.Fatalf("no RT_GROUP_ICON (%d) type; the tree is:\n%s", rtGroupIcon, tree(t, section))
	}
	if len(groups) != 1 || groups[0].id != 1 {
		t.Errorf("RT_GROUP_ICON should be a single group with id 1, got %v", groups)
	}

	// The language level names a data entry for every resource, and the entry has
	// to be reachable and non-empty.
	seen := 0
	for _, ty := range types {
		for _, name := range byType[ty.id] {
			langs := readDir(t, section, int(name.offset))
			if len(langs) != 1 {
				t.Errorf("%s/%d has %d languages, want 1; the tree is:\n%s",
					kindOf(ty.id), name.id, len(langs), tree(t, section))
			}
			for _, lang := range langs {
				if data := leafData(t, section, lang); len(data) == 0 {
					t.Errorf("%s/%d/lang %d holds no data; the tree is:\n%s",
						kindOf(ty.id), name.id, lang.id, tree(t, section))
				}
				seen++
			}
		}
	}
	// Two types, and the group has one name while RT_ICON has one per image.
	if want := len(images) + 1; seen != want {
		t.Errorf("the tree has %d resources, want %d; the tree is:\n%s", seen, want, tree(t, section))
	}
}

// TestGroupIconListsEveryImage checks the group's contents, which is what the
// loader reads to know which images an icon has.
func TestGroupIconListsEveryImage(t *testing.T) {
	sizes := []int{16, 32, 48}
	images := make([]IconData, len(sizes))
	for i, s := range sizes {
		images[i] = IconData{Width: byte(s), Height: byte(s), Bytes: []byte{9, 9, 9}}
	}
	section, err := BuildIconResources(images, 1)
	if err != nil {
		t.Fatal(err)
	}

	// Find the group's data through the tree rather than assuming an offset.
	var group []byte
	for _, ty := range readDir(t, section, 0) {
		if ty.id != rtGroupIcon {
			continue
		}
		for _, name := range readDir(t, section, int(ty.offset)) {
			for _, lang := range readDir(t, section, int(name.offset)) {
				group = leafData(t, section, lang)
			}
		}
	}
	if group == nil {
		t.Fatal("the group icon's data was not found through the tree")
	}

	if got := binary.LittleEndian.Uint16(group[2:]); got != 1 {
		t.Errorf("group type is %d, want 1 (icon)", got)
	}
	if got := binary.LittleEndian.Uint16(group[4:]); int(got) != len(sizes) {
		t.Fatalf("the group lists %d images, want %d", got, len(sizes))
	}
	for i, want := range sizes {
		e := group[6+14*i:]
		if int(e[0]) != want {
			t.Errorf("group entry %d is %d wide, want %d", i, e[0], want)
		}
		// The entry points at the image by number, which is what RT_ICON/<n> is.
		if got := binary.LittleEndian.Uint16(e[12:]); int(got) != i+1 {
			t.Errorf("group entry %d refers to image %d, want %d", i, got, i+1)
		}
	}
}
