package winres

import (
	"encoding/binary"
	"fmt"
)

// Resource type ordinals, from winuser.h.
const (
	rtIcon      = 3
	rtGroupIcon = 14
)

// langID is the language written into the resource tree: 0x0409, US English.
//
// 0x0409 is the value the resource compiler defaults to and the one every
// Windows build of this program has shipped with. The icon contains no words,
// so the language is arbitrary — but it is what the loader looks for when a
// program has no language folders, so it has to stay what it was.
const langID = 0x0409

// IconData is one image from the .ico file: its pixel dimensions and its PNG (or
// DIB) bytes.
type IconData struct {
	Width, Height uint8
	Bytes         []byte
}

// GroupIconEntry is one entry of a RT_GROUP_ICON resource, mirroring the
// GRPICONDIRENTRY structure. Note that it stores the image *number* rather than
// the byte offset the .ico directory uses: the loader looks the image up as
// RT_ICON/<n>, so the two formats differ in exactly this one field.
type GroupIconEntry struct {
	Width, Height uint8
	ColorCount    uint8
	Planes        uint16
	BitCount      uint16
	Bytes         uint32
	ID            uint16
}

// BuildIconResources packs a set of icon images into the three-level resource
// tree a PE image stores them in:
//
//	RT_ICON/<id>          one entry per image, holding the raw PNG or DIB bytes
//	RT_GROUP_ICON/<id>    one entry describing them all, which is what
//	                      Windows reads to enumerate the images
//
// Both levels are needed: RT_ICON alone gives the loader raw images it has no
// index for, and RT_GROUP_ICON alone gives it an index pointing at nothing.
//
// groupID is the id the group is stored under, and it is the value that has to
// match what an application asks for — Windows uses the first (lowest id) group
// when a program does not name one, and wbtray does not, so one group is enough.
func BuildIconResources(images []IconData, groupID uint16) ([]byte, error) {
	if len(images) == 0 {
		return nil, fmt.Errorf("winres: no icon images")
	}
	if len(images) > 0xffff {
		return nil, fmt.Errorf("winres: %d images is too many", len(images))
	}

	// The group entry's ID is the RT_ICON id, so the two levels agree on which
	// image is which. Numbering starts at 1: an id of 0 is the "no id" marker.
	entries := make([]GroupIconEntry, len(images))
	for i, img := range images {
		entries[i] = GroupIconEntry{
			Width:      img.Width,
			Height:     img.Height,
			ColorCount: 0, // 0 for true colour, which all of these are
			Planes:     1,
			BitCount:   32,
			Bytes:      uint32(len(img.Bytes)),
			ID:         uint16(i + 1),
		}
	}

	tree := newResourceTree()
	group := tree.add(rtGroupIcon, groupID)
	group.setData(groupIconBytes(entries))
	for i, img := range images {
		tree.add(rtIcon, uint16(i+1)).setData(img.Bytes)
	}
	return tree.bytes()
}

// groupIconBytes lays out a GRPICONDIR followed by its entries.
//
// The directory is the same shape as an .ico's, minus the images: a six-byte
// header and one sixteen-byte entry per image.
func groupIconBytes(entries []GroupIconEntry) []byte {
	buf := make([]byte, 6+14*len(entries))
	binary.LittleEndian.PutUint16(buf[0:], 0) // Reserved
	binary.LittleEndian.PutUint16(buf[2:], 1) // Type: 1 is an icon
	binary.LittleEndian.PutUint16(buf[4:], uint16(len(entries)))

	for i, e := range entries {
		off := 6 + 14*i
		buf[off+0] = e.Width
		buf[off+1] = e.Height
		buf[off+2] = e.ColorCount
		buf[off+3] = 0 // Reserved
		binary.LittleEndian.PutUint16(buf[off+4:], e.Planes)
		binary.LittleEndian.PutUint16(buf[off+6:], e.BitCount)
		binary.LittleEndian.PutUint32(buf[off+8:], e.Bytes)
		binary.LittleEndian.PutUint16(buf[off+12:], e.ID)
	}
	return buf
}
