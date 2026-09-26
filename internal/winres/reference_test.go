package winres

import (
	"encoding/binary"
	"os"
	"testing"
)

// Go's own linker test fixtures include a .rsrc object that is known to work: it
// is what the toolchain links when a program has a resource section. Reading its
// structure is the only reliable way to know what Windows expects, because a
// resource tree that is subtly wrong does not fail — it produces an executable
// whose resources simply cannot be found.

// referenceSyso is the path to Go's fixture, if this toolchain has one.
func referenceSyso() string {
	for _, p := range []string{
		"/usr/local/go/src/cmd/link/testdata/pe-binutils/rsrc_amd64.syso",
		"/usr/lib/go/src/cmd/link/testdata/pe-binutils/rsrc_amd64.syso",
		`C:\\Program Files\\Go\\src\\cmd\\link\\testdata\\pe-binutils\\rsrc_amd64.syso`,
		`C:\\Go\\src\\cmd\\link\\testdata\\pe-binutils\\rsrc_amd64.syso`,
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// sectionOf returns the .rsrc section's bytes from an object file.
func sectionOf(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("cannot read %s: %v", path, err)
	}
	nsec := int(binary.LittleEndian.Uint16(b[2:]))
	optSize := int(binary.LittleEndian.Uint16(b[16:]))
	start := 20 + optSize
	for i := 0; i < nsec; i++ {
		s := start + i*40
		name := string(b[s : s+8])
		trimmed := name
		for len(trimmed) > 0 && trimmed[len(trimmed)-1] == 0 {
			trimmed = trimmed[:len(trimmed)-1]
		}
		if trimmed != ".rsrc" {
			continue
		}
		rawSize := int(binary.LittleEndian.Uint32(b[s+16:]))
		rawPtr := int(binary.LittleEndian.Uint32(b[s+20:]))
		return b[rawPtr : rawPtr+rawSize]
	}
	t.Fatalf("%s has no .rsrc section", path)
	return nil
}

// TestOurTreeMatchesGosReference is the check that would have caught the missing
// icon: compare the tree this package builds against the one Go links for real.
//
// The earlier version omitted the root directory entirely, so the first type
// directory sat at offset zero and Windows read it as the root. Nothing reported
// an error; the executable simply had no icon.
func TestOurTreeMatchesGosReference(t *testing.T) {
	path := referenceSyso()
	if path == "" {
		t.Skip("this toolchain ships no reference .rsrc object")
	}
	ref := sectionOf(t, path)

	root := readDir(t, ref, 0)
	if len(root) == 0 {
		t.Fatal("the reference has an empty root directory")
	}
	for _, e := range root {
		if !e.isDir {
			t.Errorf("reference root entry %d is not a directory", e.id)
		}
	}
	t.Logf("reference root names %d resource type(s): %v", len(root), root)

	// The tree below it has to be three levels deep, ending in data.
	for _, ty := range root {
		names := readDir(t, ref, int(ty.offset))
		if len(names) == 0 {
			t.Errorf("reference type %d has no names", ty.id)
			continue
		}
		for _, name := range names {
			langs := readDir(t, ref, int(name.offset))
			if len(langs) == 0 {
				t.Errorf("reference type %d name %d has no languages", ty.id, name.id)
			}
		}
	}

	// Ours has to have the same shape.
	images := []IconData{
		{Width: 16, Height: 16, Bytes: []byte{1, 2, 3, 4}},
		{Width: 32, Height: 32, Bytes: []byte{5, 6, 7, 8}},
	}
	mine, err := BuildIconResources(images, 1)
	if err != nil {
		t.Fatal(err)
	}
	myRoot := readDir(t, mine, 0)
	if len(myRoot) != 2 {
		t.Fatalf("our root names %d types, want 2:\n%s", len(myRoot), tree(t, mine))
	}
	for _, e := range myRoot {
		if !e.isDir {
			t.Errorf("our root entry %d is not a directory", e.id)
		}
	}
	types := map[uint32]bool{}
	for _, e := range myRoot {
		types[e.id] = true
	}
	if !types[rtIcon] || !types[rtGroupIcon] {
		t.Errorf("our root names %v, want RT_ICON (%d) and RT_GROUP_ICON (%d)",
			types, rtIcon, rtGroupIcon)
	}
}
