package winres

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sort"
)

// Building a resource directory.
//
// A PE resource section is a four-level tree, and the levels are counted from the
// root:
//
//	root                     one entry per resource type
//	  type                   one entry per name or id within that type
//	    name                 one entry per language
//	      language           points at the data
//
// Every node is a 16-byte header followed by one 8-byte entry per child. An entry
// either points at the next directory down or at a leaf, distinguished by the
// high bit of its offset word, and every offset is measured from the start of the
// section.
//
// The root is the part that is easy to leave out, and leaving it out does not
// fail: the section simply begins with a type directory, Windows reads that as
// the root, and the resources become unreachable. The executable then has no
// icon and nothing reports an error. That is exactly what happened here, and this
// file is written with the root explicit so it cannot happen again.

// leaf is one resource: its bytes and where it belongs in the tree.
type leaf struct {
	typ  uint16 // RT_ICON, RT_GROUP_ICON
	id   uint16 // the resource id, e.g. 1 for the first icon image
	data []byte
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

// node is one directory in the tree, identified by the path that reaches it.
//
// The path is what makes the arithmetic checkable: a node's children are the
// leaves whose first entries match its path, and its depth follows from the
// path's length. Writing the tree this way rather than as three nested loops
// means the root is a node like any other, with an empty path and every type as
// its children.
type node struct {
	path []uint16 // empty for the root, then type, then type+name, then type+name+lang
	// offset is filled in while the section is laid out.
	offset int
}

// depth is how many levels below the root this node sits.
func (n *node) depth() int { return len(n.path) }

// children returns the leaves directly below this node.
func (n *node) children(leaves []*leaf) []*leaf {
	var out []*leaf
	for _, l := range leaves {
		if leafMatches(l, n.path) {
			out = append(out, l)
		}
	}
	return out
}

// entries returns the distinct children of a node at its own level: the
// coordinates to emit, each with one leaf that reaches it.
//
// Distinctness is the point. A directory has one entry per child, not one per
// leaf below it, so the root with four icon images under two types has two
// entries and not four. Emitting one per leaf produced a root listing the same
// type twice, which is a tree Windows cannot resolve.
//
// At the last level there is nothing left to group: each leaf is its own entry,
// because a leaf is what the entry points at.
func (n *node) entries(leaves []*leaf) []*leaf {
	if n.depth() >= 3 {
		return n.children(leaves)
	}
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

// leafMatches reports whether a leaf hangs directly below a path.
//
// A node at depth d matches a leaf whose first d coordinates equal its path: the
// root matches everything, a type node matches that type, and so on. The leaf's
// own value at the node's depth is the entry the node has for it.
func leafMatches(l *leaf, path []uint16) bool {
	coords := [4]uint16{l.typ, l.id, langID, 0}
	for i, p := range path {
		if i >= len(coords) || coords[i] != p {
			return false
		}
	}
	return true
}

// leafCoord is the value a leaf contributes at a given depth.
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

// buildTree returns every node, parents before children, so laying them out in
// order puts each directory before the data it points at.
func buildTree(leaves []*leaf) []*node {
	root := &node{}
	nodes := []*node{root}
	seen := map[string]bool{}

	var walk func(n *node)
	walk = func(n *node) {
		// The children of a node are the distinct coordinates its leaves
		// contribute at its depth.
		values := map[uint16]bool{}
		for _, l := range n.children(leaves) {
			if n.depth() < 3 {
				values[leafCoord(l, n.depth())] = true
			}
		}
		sorted := make([]uint16, 0, len(values))
		for v := range values {
			sorted = append(sorted, v)
		}
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

		for _, v := range sorted {
			child := &node{path: append(append([]uint16{}, n.path...), v)}
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

	nodes := buildTree(t.leaves)

	// Directories first, then data, so no offset written into an entry is
	// disturbed by something added afterwards.
	offset := 0
	for _, n := range nodes {
		n.offset = offset
		offset += 16 + 8*len(n.entries(t.leaves))
	}
	leafOffset := map[*leaf]int{}
	for _, l := range t.leaves {
		offset = align4(offset)
		leafOffset[l] = offset
		offset += len(l.data)
	}

	var buf bytes.Buffer
	for _, n := range nodes {
		var hdr [16]byte
		binary.LittleEndian.PutUint16(hdr[14:], uint16(len(n.entries(t.leaves))))
		buf.Write(hdr[:])

		for _, l := range n.entries(t.leaves) {
			var e [8]byte
			if n.depth() == 3 {
				// A leaf: the language, pointing at the data.
				binary.LittleEndian.PutUint32(e[0:], langID)
				binary.LittleEndian.PutUint32(e[4:], uint32(leafOffset[l]))
			} else {
				// A subdirectory: the child's coordinate, pointing at its node.
				child := &node{path: append(append([]uint16{}, n.path...), leafCoord(l, n.depth()))}
				binary.LittleEndian.PutUint32(e[0:], uint32(leafCoord(l, n.depth())))
				binary.LittleEndian.PutUint32(e[4:], uint32(findNode(nodes, child).offset)|0x80000000)
			}
			buf.Write(e[:])
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

// findNode returns the node with a given path, which the builder has already
// created: entries are written from the parents, and every parent knows its
// children because the tree was built from them.
func findNode(nodes []*node, want *node) *node {
	for _, n := range nodes {
		if len(n.path) != len(want.path) {
			continue
		}
		same := true
		for i := range n.path {
			if n.path[i] != want.path[i] {
				same = false
				break
			}
		}
		if same {
			return n
		}
	}
	return want
}

// align4 rounds an offset up to the next four-byte boundary.
func align4(n int) int { return (n + 3) &^ 3 }
