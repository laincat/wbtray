package winres

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sort"
)

// Building a resource directory.
//
// A PE resource section is a three-level tree of directories followed by a list
// of data entries, and the two halves are different kinds of thing:
//
//	root                     one entry per resource type
//	  type                   one entry per name or id within that type
//	    language             points at an IMAGE_RESOURCE_DATA_ENTRY
//
//	IMAGE_RESOURCE_DATA_ENTRY  a 16-byte record: the data's address, its size,
//	                           its code page and a reserved word
//
// The data entry is the part that is easy to leave out, and leaving it out does
// not fail. An entry that points straight at the bytes produces a section whose
// tree looks completely correct — every directory, every type, every name — and
// whose resources Windows cannot read, because it follows every leaf to a data
// entry and finds image data instead. The executable then shows the system's
// generic icon, which is indistinguishable from having no icon at all.
//
// Two things follow from the format and are worth stating because both were
// wrong here:
//
//   - The tree has three levels of *directory*, not four. The language entry is
//     the last directory entry; what it points at is a data entry, not a
//     directory.
//   - Directory entries hold offsets from the start of the section and are not
//     relocated. Only a data entry's address is, because only it holds a virtual
//     address rather than an offset.

// leaf is one resource: its bytes and where it belongs in the tree.
type leaf struct {
	typ  uint16 // RT_ICON, RT_GROUP_ICON
	id   uint16 // the resource id, e.g. 1 for the first icon image
	data []byte
}

// dataEntryLen is sizeof(IMAGE_RESOURCE_DATA_ENTRY).
const dataEntryLen = 16

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

// node is one directory in the tree, identified by the path that reaches it.
//
// The path is what makes the arithmetic checkable: a node's children are the
// leaves whose leading coordinates match its path, and its depth is the path's
// length. Written this way the root is a node like any other — an empty path with
// every type below it — which is why it cannot be left out by accident.
type node struct {
	// path is empty for the root, then the type, then the type and name.
	path []uint16
	// offset is filled in while the section is laid out.
	offset int
}

// depth is how many levels below the root this node sits: 0, 1 or 2.
func (n *node) depth() int { return len(n.path) }

// leafCoord is the coordinate a leaf contributes at a given depth.
func leafCoord(l *leaf, depth int) uint16 {
	switch depth {
	case 0:
		return l.typ
	case 1:
		return l.id
	case 2:
		return langID
	}
	return 0
}

// children returns the leaves directly below this node, which are those whose
// leading coordinates match its path.
func (n *node) children(leaves []*leaf) []*leaf {
	var out []*leaf
	for _, l := range leaves {
		if matches(l, n.path) {
			out = append(out, l)
		}
	}
	return out
}

// matches reports whether a leaf hangs below a path.
func matches(l *leaf, path []uint16) bool {
	for i, p := range path {
		if i >= 3 || leafCoord(l, i) != p {
			return false
		}
	}
	return true
}

// entries returns the distinct children of a node at its own level: the
// coordinates to emit, each with one leaf that reaches it.
//
// Distinctness is the point. A directory has one entry per child, not one per
// leaf below it, so a root with four images under two types has two entries.
// Emitting one per leaf produced a root that listed the same type twice, which is
// a tree Windows cannot resolve.
func (n *node) entries(leaves []*leaf) []*leaf {
	seen := map[uint16]bool{}
	var out []*leaf
	for _, l := range n.children(leaves) {
		c := leafCoord(l, n.depth())
		if seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool {
		return leafCoord(out[i], n.depth()) < leafCoord(out[j], n.depth())
	})
	return out
}

// buildTree returns every directory, parents before children, so laying them out
// in order puts each directory before the data entries and the data it points at.
func buildTree(leaves []*leaf) []*node {
	root := &node{}
	nodes := []*node{root}
	seen := map[string]bool{}

	var walk func(n *node)
	walk = func(n *node) {
		// A node's children are the distinct coordinates its leaves contribute at
		// its depth. Below the language level there is nothing left to group, so
		// the walk stops there: the entries at that level are data entries.
		if n.depth() >= 2 {
			return
		}
		for _, l := range n.entries(leaves) {
			child := &node{path: append(append([]uint16{}, n.path...), leafCoord(l, n.depth()))}
			key := fmt.Sprint(child.path)
			if seen[key] {
				continue
			}
			seen[key] = true
			nodes = append(nodes, child)
			walk(child)
		}
	}
	walk(root)
	return nodes
}

// bytes serialises the tree as the contents of a .rsrc section.
func (t *resourceTree) bytes() ([]byte, error) {
	if len(t.leaves) == 0 {
		return nil, fmt.Errorf("winres: empty resource tree")
	}

	dirs := buildTree(t.leaves)
	imageLeaves, groupLeaves := splitLeaves(t.leaves)

	// Layout: every directory, then every data entry, then the resource image and
	// the group icon.
	//
	// The order matters because every offset is measured from the start of the
	// section and nothing may be moved once an offset has been written. Data
	// entries come before the resource image so the image's own offset can be
	// written into them after the fact.
	offset := 0
	for _, d := range dirs {
		d.offset = offset
		offset += 16 + 8*len(d.entries(t.leaves))
	}
	dataEntryOffset := map[*leaf]int{}
	for _, l := range groupLeaves {
		offset = align4(offset)
		dataEntryOffset[l] = offset
		offset += dataEntryLen
	}
	for _, l := range imageLeaves {
		offset = align4(offset)
		dataEntryOffset[l] = offset
		offset += dataEntryLen
	}
	// The image data and the group icon, which the data entries will point at.
	dataOffset := map[*leaf]int{}
	for _, l := range groupLeaves {
		offset = align4(offset)
		dataOffset[l] = offset
		offset += len(l.data)
	}
	for _, l := range imageLeaves {
		offset = align4(offset)
		dataOffset[l] = offset
		offset += len(l.data)
	}

	var buf bytes.Buffer
	for _, d := range dirs {
		var hdr [16]byte
		// IMAGE_RESOURCE_DIRECTORY: characteristics, timestamp, versions, then the
		// counts. Everything but the counts is zero, and Windows ignores the rest.
		binary.LittleEndian.PutUint16(hdr[14:], uint16(len(d.entries(t.leaves))))
		buf.Write(hdr[:])

		for _, l := range d.entries(t.leaves) {
			var e [8]byte
			binary.LittleEndian.PutUint32(e[0:], uint32(leafCoord(l, d.depth())))
			if d.depth() == 2 {
				// The last directory level: the entry names the language and
				// points at a data entry, which is not a directory, so the high
				// bit stays clear.
				binary.LittleEndian.PutUint32(e[4:], uint32(dataEntryOffset[l]))
			} else {
				child := &node{path: append(append([]uint16{}, d.path...), leafCoord(l, d.depth()))}
				binary.LittleEndian.PutUint32(e[4:], uint32(findDir(dirs, child).offset)|0x80000000)
			}
			buf.Write(e[:])
		}
	}

	// The data entries. Their first field is the address of the data, which the
	// linker will relocate; the rest is the size, the code page and a reserved
	// word.
	writeEntry := func(l *leaf) {
		for buf.Len() < dataEntryOffset[l] {
			buf.WriteByte(0)
		}
		var e [dataEntryLen]byte
		// A placeholder: the address is only known once the linker has placed the
		// section, and WriteObject writes the relocation that fills it in.
		binary.LittleEndian.PutUint32(e[0:], uint32(dataOffset[l]))
		binary.LittleEndian.PutUint32(e[4:], uint32(len(l.data)))
		binary.LittleEndian.PutUint32(e[8:], 0) // code page: unicode
		binary.LittleEndian.PutUint32(e[12:], 0)
		buf.Write(e[:])
	}
	for _, l := range groupLeaves {
		writeEntry(l)
	}
	for _, l := range imageLeaves {
		writeEntry(l)
	}

	for _, l := range groupLeaves {
		for buf.Len() < dataOffset[l] {
			buf.WriteByte(0)
		}
		buf.Write(l.data)
	}
	for _, l := range imageLeaves {
		for buf.Len() < dataOffset[l] {
			buf.WriteByte(0)
		}
		buf.Write(l.data)
	}
	return buf.Bytes(), nil
}

// splitLeaves separates the icon images from the group icon.
//
// They are laid out in two groups rather than interleaved so a failure to write
// either one is visible as a gap rather than as a plausible-looking sequence of
// bytes in the wrong place.
func splitLeaves(leaves []*leaf) (images, groups []*leaf) {
	for _, l := range leaves {
		if l.typ == rtIcon {
			images = append(images, l)
		} else {
			groups = append(groups, l)
		}
	}
	return images, groups
}

// findDir returns the directory with a given path, which the builder has already
// created: entries are written from the parents, and every parent knows its
// children because the tree was built from them.
func findDir(dirs []*node, want *node) *node {
	for _, d := range dirs {
		if len(d.path) != len(want.path) {
			continue
		}
		same := true
		for i := range d.path {
			if d.path[i] != want.path[i] {
				same = false
				break
			}
		}
		if same {
			return d
		}
	}
	return want
}

// align4 rounds an offset up to the next four-byte boundary.
func align4(n int) int { return (n + 3) &^ 3 }
