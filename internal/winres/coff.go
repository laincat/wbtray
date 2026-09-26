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
// These flags are not decoration: Go's linker selects the sections it keeps by
// reading them. Two of its checks matter here, both in cmd/link/internal/loadpe.
//
// A section marked MEM_DISCARDABLE is skipped outright when the loader creates
// its symbols — the loader assumes data the OS may throw away after use is not
// something the image needs. The section then has no symbol, addpersrc finds no
// resource to add, and the executable ships with no resource directory at all:
// Windows falls back to its generic blank icon and the .syso looks like it did
// nothing. That is what every build before this fix did.
//
// The remaining flags have to name a section type the loader recognises, because
// anything it cannot classify is an error rather than a guess. The combination
// below is the one it reads as plain initialised data, and it is the combination
// Go's own linker test fixtures carry for this section.
const sectionCharacteristics = 0x00000040 | // CNT_INITIALIZED_DATA
	0x40000000 | // MEM_READ
	0x80000000 | // MEM_WRITE
	0x00300000 // ALIGN_4BYTES

// relocationType is IMAGE_REL_AMD64_ADDR32NB (and its ARM64 twin), which is what
// Go's linker expects for the address field of a resource data entry: a 32-bit
// value relative to the image base, filled in once the section has been placed.
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
// Exactly one kind of field in a resource section is relocated, and picking the
// wrong kind is why an icon can be embedded and still not appear:
//
//   - A resource *directory* entry holds an offset from the start of the section.
//     The loader adds the section's base address itself, so the value in the file
//     is already correct and must not be touched.
//   - An IMAGE_RESOURCE_DATA_ENTRY holds a virtual address. It cannot be known
//     when the object is written, so it is emitted as an offset and a relocation
//     has the linker add the section's address to it.
//
// Relocating the directory entries instead — which is what this did — leaves the
// tree looking perfect and every leaf pointing at a plausible address shifted by
// the section base. Windows follows one, finds image bytes where a data entry
// should be, discards the resource, and shows its generic icon.
func findRelocations(rsrc []byte) []int {
	var offsets []int
	var walk func(off int)

	walk = func(off int) {
		// A directory header is 16 bytes: characteristics, timestamp, version
		// words, then the counts of named and id entries.
		if off+16 > len(rsrc) {
			return
		}
		named := int(binary.LittleEndian.Uint16(rsrc[off+12:]))
		ids := int(binary.LittleEndian.Uint16(rsrc[off+14:]))
		for i := 0; i < named+ids; i++ {
			entry := off + 16 + i*8
			if entry+8 > len(rsrc) {
				return
			}
			target := binary.LittleEndian.Uint32(rsrc[entry+4:])
			if target&0x80000000 != 0 {
				// A subdirectory. Its entries hold offsets from the start of the
				// section, which the loader translates itself, so nothing in it is
				// relocated.
				walk(int(target & 0x7fffffff))
				continue
			}
			// A data entry. The field to relocate is the address at the start of
			// it, not the entry itself.
			offsets = append(offsets, int(target&0x7fffffff))
		}
	}
	walk(0)
	return offsets
}
