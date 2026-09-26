// Package winres writes the COFF object a Go linker turns into a PE resource.
//
// A Go executable can only carry a file icon if a .syso sits beside the
// package's .go sources: the linker notices it, copies its .rsrc section into
// the image, and applies the relocations the resource tree needs. That file is
// normally produced by an external resource compiler (rsrc, windres, rc.exe),
// which is a toolchain dependency the build otherwise does not have — this
// package produces it directly.
//
// The object is deliberately minimal: one section, one symbol, one relocation
// list. Go's linker reads the section bytes and the relocations and nothing
// else, so there is no need for a .text, an optional header, or a symbol table
// entry per resource. Keeping it small also keeps it readable, which matters
// because a malformed object fails at link time with an unhelpful message.
package winres

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// The machine types a Windows target can use. The values are IMAGE_FILE_MACHINE_*
// constants from winnt.h; the Go linker checks that the machine matches the
// target it is linking for, so writing the wrong one produces a confusing error.
const (
	MachineAMD64 = 0x8664
	MachineARM64 = 0xaa64
	MachineI386  = 0x014c
)

// Section characteristics for .rsrc: initialised data, readable and writable,
// four-byte aligned.
//
// MEM_WRITE (0x80000000) is the one that matters, and its absence is why the
// icon was missing from every build before this was fixed. The flags were
// MEM_DISCARDABLE, which is what an object compiler emits for a section the
// loader may throw away after use — and Go's linker, reading a resource section
// it may discard, treats the data as something no image needs and drops it. The
// executable then has no resource directory at all, so Windows falls back to its
// generic blank icon and the .syso looks like it did nothing.
//
// Go's own linker test fixtures carry exactly 0xC0300040 for this section
// (MEM_READ | MEM_WRITE | CNT_INITIALIZED_DATA | ALIGN_4BYTES), which is what an
// icon resource has to look like to survive the link.
const sectionCharacteristics = 0x00000040 | // CNT_INITIALIZED_DATA
	0x40000000 | // MEM_READ
	0x80000000 | // MEM_WRITE
	0x00300000 // ALIGN_4BYTES

// relocationType is IMAGE_REL_AMD64_ADDR32NB (and its ARM64 twin): the
// "32-bit image-base-relative address" relocation a resource directory needs.
//
// Resource directories store offsets that Windows rewrites into virtual
// addresses when the image loads, so each one has to be relocated. ADDR32NB is
// the right one on every 64-bit Windows target at this width.
const relocationType = 0x0003

// WriteObject renders a .rsrc section into a COFF object for one machine.
func WriteObject(machine uint16, rsrc []byte) ([]byte, error) {
	if len(rsrc) == 0 {
		return nil, fmt.Errorf("winres: no resource data")
	}
	switch machine {
	case MachineAMD64, MachineARM64, MachineI386:
	default:
		return nil, fmt.Errorf("winres: unsupported machine %#x", machine)
	}

	relocs := findRelocations(rsrc)

	// Layout: 20-byte file header, 40-byte section header, section bytes, then
	// the relocation list, and finally a symbol table holding one entry for the
	// section plus its auxiliary record. Every offset below is that layout
	// written out; the parts are built separately only so each can be emitted in
	// file order.
	const (
		fileHeaderLen    = 20
		sectionHeaderLen = 40
		symbolLen        = 18
		relocLen         = 10
	)
	rawData := fileHeaderLen + sectionHeaderLen
	relocStart := rawData + len(rsrc)
	symStart := relocStart + relocLen*len(relocs)

	// A section symbol and its auxiliary record are two symbol-table entries, so
	// NumberOfSymbols is 2 even though only one index is ever referenced.
	const symCount = 2

	fileHeader := make([]byte, fileHeaderLen)
	binary.LittleEndian.PutUint16(fileHeader[0:], machine)
	binary.LittleEndian.PutUint16(fileHeader[2:], 1) // NumberOfSections
	// TimeDateStamp stays zero so a regenerated object is byte-identical; the
	// linker ignores it.
	binary.LittleEndian.PutUint32(fileHeader[4:], 0)
	binary.LittleEndian.PutUint32(fileHeader[8:], uint32(symStart))
	binary.LittleEndian.PutUint32(fileHeader[12:], symCount)
	binary.LittleEndian.PutUint16(fileHeader[16:], 0) // SizeOfOptionalHeader
	binary.LittleEndian.PutUint16(fileHeader[18:], 0) // Characteristics

	sectionHeader := make([]byte, sectionHeaderLen)
	copy(sectionHeader[0:8], ".rsrc")
	binary.LittleEndian.PutUint32(sectionHeader[8:], 0)                       // VirtualSize
	binary.LittleEndian.PutUint32(sectionHeader[12:], 0)                      // VirtualAddress
	binary.LittleEndian.PutUint32(sectionHeader[16:], uint32(len(rsrc)))      // SizeOfRawData
	binary.LittleEndian.PutUint32(sectionHeader[20:], uint32(rawData))        // PointerToRawData
	binary.LittleEndian.PutUint32(sectionHeader[24:], uint32(relocStart))     // PointerToRelocations
	binary.LittleEndian.PutUint32(sectionHeader[28:], 0)                      // PointerToLineNumbers
	binary.LittleEndian.PutUint16(sectionHeader[32:], uint16(len(relocs)))    // NumberOfRelocations
	binary.LittleEndian.PutUint16(sectionHeader[34:], 0)                      // NumberOfLineNumbers
	binary.LittleEndian.PutUint32(sectionHeader[36:], sectionCharacteristics) // Characteristics

	// One relocation per directory: the target is symbol index 0, the section
	// symbol itself, which is how a section-relative fixup is expressed.
	relocations := make([]byte, relocLen*len(relocs))
	for i, off := range relocs {
		entry := relocations[i*relocLen:]
		binary.LittleEndian.PutUint32(entry[0:], uint32(off)) // VirtualAddress
		binary.LittleEndian.PutUint32(entry[4:], 0)           // SymbolTableIndex
		binary.LittleEndian.PutUint16(entry[8:], relocationType)
	}

	symbolTable := make([]byte, symbolLen*symCount)
	copy(symbolTable[0:8], ".rsrc")
	binary.LittleEndian.PutUint32(symbolTable[8:], 0)  // Value
	binary.LittleEndian.PutUint16(symbolTable[12:], 1) // SectionNumber (1-based)
	binary.LittleEndian.PutUint16(symbolTable[14:], 0) // Type
	symbolTable[16] = 3                                // StorageClass: STATIC
	symbolTable[17] = 1                                // NumberOfAuxSymbols
	// The auxiliary record only has to say how long the section is; the rest is
	// zeroed by the allocation above.
	binary.LittleEndian.PutUint32(symbolTable[symbolLen:], uint32(len(rsrc)))

	var out bytes.Buffer
	out.Write(fileHeader)
	out.Write(sectionHeader)
	out.Write(rsrc)
	out.Write(relocations)
	out.Write(symbolTable)
	// The string table follows the symbol table and must be present even when it
	// is empty: its first four bytes are the table's own length, and a linker
	// that finds no bytes there reports "fail to read string table length: EOF"
	// and refuses the object. All four names in this object are eight bytes or
	// shorter and so are stored inline in their symbol records, which is why the
	// table itself holds nothing but this header.
	out.Write([]byte{4, 0, 0, 0})
	return out.Bytes(), nil
}

// findRelocations returns the offsets that need an ADDR32NB relocation.
//
// A resource directory stores its own size and offsets in the first four bytes
// of every directory, and those offsets are the fields Windows rewrites at load
// time. The table lives at the top of the section, so the tree is walked the
// same way the loader walks it, recording each directory's offset field. Leaves
// hold raw bytes and are not relocated.
func findRelocations(rsrc []byte) []int {
	var offsets []int
	var walk func(off int)

	walk = func(off int) {
		// A directory header is 16 bytes: characteristics, timestamp, version
		// words, then the counts of named and id entries.
		if off+16 > len(rsrc) {
			return
		}
		offsets = append(offsets, off)
		named := int(binary.LittleEndian.Uint16(rsrc[off+12:]))
		ids := int(binary.LittleEndian.Uint16(rsrc[off+14:]))
		for i := 0; i < named+ids; i++ {
			entry := off + 16 + i*8
			if entry+8 > len(rsrc) {
				return
			}
			target := binary.LittleEndian.Uint32(rsrc[entry+4:])
			// The high bit marks a subdirectory; without it the entry points at
			// a leaf, whose data the loader does not relocate.
			if target&0x80000000 != 0 {
				walk(int(target & 0x7fffffff))
			}
		}
	}
	walk(0)
	return offsets
}
