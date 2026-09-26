package winres

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sort"
)

// Building a resource tree.
//
// A PE resource directory is a three-level tree: type, then name-or-id, then
// language. Every node is a 16-byte header followed by one 8-byte entry per
// child; an entry either points at a leaf (the actual bytes) or at the next
// directory down, distinguished by the high bit of its offset word. Windows
// walks the tree by following those offsets, so the layout has to be exact:
// every directory comes before any leaf data, and each offset is measured from
// the start of the section.
//
// The tree here is built for one resource type and one id per leaf, but it is
// expressed generally because the offset arithmetic is far easier to get right
// when it is written against a tree than against three nested loops.

// leaf is one resource's bytes, tagged with where it belongs in the tree.
type leaf struct {
	typ  uint16 // RT_ICON, RT_GROUP_ICON
	id   uint16 // the resource id, e.g. 1 for the first icon image
	data []byte
}

// dir is one directory node: a (level, type, id) position in the tree.
type dir struct {
	level  int // 0 = type, 1 = id, 2 = language
	typeID uint16
	nameID uint16
	offset int // filled in while laying the section out
}

// resourceTree collects leaves and serialises them.
type resourceTree struct {
	leaves []*leaf
}

func newResourceTree() *resourceTree { return &resourceTree{} }

// add appends a leaf and returns it so its data can be filled in.
func (t *resourceTree) add(typ, id uint16) *leaf {
	l := &leaf{typ: typ, id: id}
	t.leaves = append(t.leaves, l)
	return l
}

func (l *leaf) setData(data []byte) { l.data = data }

// bytes serialises the tree as the contents of a .rsrc section.
//
// Types and ids are emitted in ascending order. Windows does not require a
// particular order, but sorting makes the output deterministic, and a resource
// section that rebuilds identically is one less thing to diff.
func (t *resourceTree) bytes() ([]byte, error) {
	if len(t.leaves) == 0 {
		return nil, fmt.Errorf("winres: empty resource tree")
	}

	// Group the leaves: type -> id -> leaves.
	byType := map[uint16]map[uint16][]*leaf{}
	var types []uint16
	for _, l := range t.leaves {
		ids, ok := byType[l.typ]
		if !ok {
			ids = map[uint16][]*leaf{}
			byType[l.typ] = ids
			types = append(types, l.typ)
		}
		ids[l.id] = append(ids[l.id], l)
	}
	sort.Slice(types, func(i, j int) bool { return types[i] < types[j] })

	// The three levels, in the order they will be written: one type directory,
	// then one directory per id, then one language directory per id.
	var l0, l1, l2 []*dir
	for _, typ := range types {
		l0 = append(l0, &dir{level: 0, typeID: typ})
		ids := sortedIDs(byType[typ])
		for _, id := range ids {
			l1 = append(l1, &dir{level: 1, typeID: typ, nameID: id})
		}
	}
	for _, d := range l1 {
		l2 = append(l2, &dir{level: 2, typeID: d.typeID, nameID: d.nameID})
	}
	dirs := append(append(append([]*dir{}, l0...), l1...), l2...)

	// Lay out the directories first, then the leaves, so no offset written into
	// an entry is disturbed by data added later.
	offset := 0
	for _, d := range dirs {
		d.offset = offset
		offset += 16 + 8*childCount(d, byType)
	}
	leafOffset := map[*leaf]int{}
	for _, l := range t.leaves {
		offset = align4(offset)
		leafOffset[l] = offset
		offset += len(l.data)
	}

	var buf bytes.Buffer
	for _, d := range dirs {
		// Directory header: size and offsets are zeroed, and only the entry
		// counts are filled in. Windows ignores the header's own size fields on
		// load, and Go's linker never reads them.
		var hdr [16]byte
		binary.LittleEndian.PutUint16(hdr[14:], uint16(childCount(d, byType)))
		buf.Write(hdr[:])

		for _, e := range entriesOf(d, l1, l2, byType, leafOffset) {
			buf.Write(e)
		}
	}
	for _, l := range t.leaves {
		for buf.Len() < leafOffset[l] {
			buf.WriteByte(0)
		}
		buf.Write(l.data)
	}
	return buf.Bytes(), nil
}

// childCount reports how many entries a directory holds.
func childCount(d *dir, byType map[uint16]map[uint16][]*leaf) int {
	switch d.level {
	case 0:
		return len(byType[d.typeID])
	case 1, 2:
		return len(byType[d.typeID][d.nameID])
	default:
		return 0
	}
}

// entriesOf builds the 8-byte entries of one directory.
//
// Level 0 points at the id directories, level 1 at the language directories,
// and level 2 at the leaf data. Subdirectory offsets carry the high bit so the
// loader knows to follow them rather than reading a leaf.
func entriesOf(d *dir, l1, l2 []*dir, byType map[uint16]map[uint16][]*leaf, leafOffset map[*leaf]int) [][]byte {
	var out [][]byte
	entry := func(id, off uint32, isDir bool) {
		var e [8]byte
		binary.LittleEndian.PutUint32(e[0:], id)
		if isDir {
			off |= 0x80000000
		}
		binary.LittleEndian.PutUint32(e[4:], off)
		out = append(out, e[:])
	}

	switch d.level {
	case 0:
		for _, child := range l1 {
			if child.typeID == d.typeID {
				entry(uint32(child.nameID), uint32(child.offset), true)
			}
		}
	case 1:
		for _, child := range l2 {
			if child.typeID == d.typeID && child.nameID == d.nameID {
				entry(uint32(langID), uint32(child.offset), true)
			}
		}
	case 2:
		for _, l := range byType[d.typeID][d.nameID] {
			entry(uint32(langID), uint32(leafOffset[l]), false)
		}
	}
	return out
}

// sortedIDs returns a type's ids in ascending order.
func sortedIDs(ids map[uint16][]*leaf) []uint16 {
	out := make([]uint16, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// align4 rounds an offset up to the next four-byte boundary.
func align4(n int) int { return (n + 3) &^ 3 }
