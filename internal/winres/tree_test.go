package winres

import (
	"encoding/binary"
	"fmt"
	"testing"
)

// A resource directory is a tree of three levels — type, then name or id, then
// language — and Windows walks it by following offsets. Getting the levels wrong
// produces a file whose resources exist but cannot be found, which is worse than
// producing no resources at all: the icon is silently missing and nothing
// reports an error.
//
// These tests parse the section this package builds and assert the tree, which
// is the only way to catch a level error. An earlier version emitted the id in
// the type position, and every build shipped without an icon.

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
				if !lang.isDir {
					out += "      <not a directory>\n"
					continue
				}
				for _, data := range readDir(t, b, int(lang.offset)) {
					where := "leaf"
					if data.isDir {
						where = "DIRECTORY, not a leaf"
					}
					out += fmt.Sprintf("      data (id %d): %s\n", data.id, where)
				}
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
// each type, then the language, and finally the entries that point at the data.
// Four levels in total, and the root is the one that is easy to leave out — a
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

	// The fourth level points at the data, and it must not be a directory.
	for _, ty := range types {
		for _, name := range byType[ty.id] {
			for _, lang := range readDir(t, section, int(name.offset)) {
				if !lang.isDir {
					t.Errorf("%s/%d's language entry is not a directory; the tree is:\n%s",
						kindOf(ty.id), name.id, tree(t, section))
					continue
				}
				data := readDir(t, section, int(lang.offset))
				if len(data) != 1 {
					t.Errorf("%s/%d/lang %d has %d data entries, want 1; the tree is:\n%s",
						kindOf(ty.id), name.id, lang.id, len(data), tree(t, section))
				}
				for _, d := range data {
					if d.isDir {
						t.Errorf("%s/%d's data entry points at a directory; the tree is:\n%s",
							kindOf(ty.id), name.id, tree(t, section))
					}
				}
			}
		}
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
				for _, data := range readDir(t, section, int(lang.offset)) {
					if int(data.offset)+14*len(images) > len(section) {
						t.Fatalf("the group's data runs past the section")
					}
					group = section[data.offset:]
				}
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
