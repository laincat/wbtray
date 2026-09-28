// Command iconbuild attaches a Windows icon to the executable.
//
// The tray draws its own icon at run time, but the file in Explorer, the entry
// in Task Manager and the alt-tab thumbnail are all PE resources, and Go can
// only carry one through a .syso beside the package's sources. This produces
// that file from the same renderer the tray uses, so the two cannot drift.
//
// It is a separate command rather than part of the build because the .syso is
// generated once and committed: a release build should not need a renderer, and
// a generated binary artefact in a build step is one more thing that can differ
// between two machines.
//
// Usage:
//
//	go run ./cmd/iconbuild [-arch amd64|arm64|386]
package main

import (
	"flag"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"

	"wbtray/internal/iconstyle"
	"wbtray/internal/status"
	"wbtray/internal/winres"
)

// sizes are the images packed into the icon, which is what Windows picks from by
// the size it needs.
var sizes = []int{16, 20, 24, 32, 48, 64, 128, 256}

func main() {
	arch := flag.String("arch", "amd64", "Windows architecture for the .syso (amd64, arm64, 386)")
	variant := flag.String("variant", "gauge",
		"which mark to draw: "+strings.Join(iconstyle.AppVariants, ", "))
	flag.Parse()

	if err := run(*arch, *variant); err != nil {
		fmt.Fprintf(os.Stderr, "iconbuild: %v\n", err)
		os.Exit(1)
	}
}

func run(arch, variant string) error {
	machine, ok := map[string]uint16{
		"amd64": winres.MachineAMD64,
		"arm64": winres.MachineARM64,
		"386":   winres.MachineI386,
	}[arch]
	if !ok {
		return fmt.Errorf("unknown arch %q: want amd64, arm64 or 386", arch)
	}
	root, err := repoRoot()
	if err != nil {
		return err
	}

	// The file icon is the product's mark rather than a status reading: it is drawn
	// once, here, and a reading baked into it would be a lie by the next day.
	//
	// The rendered icons are keyed by size because the encoder picks the format each
	// size requires.
	rendered := map[int]image.Image{}
	for _, size := range sizes {
		rendered[size] = iconstyle.DrawAppMark(size, variant).Image()
	}
	// The encoder decides PNG or DIB per size, which is a rule of the format
	// rather than a choice: an icon whose smaller images are PNG is not rejected,
	// it simply shows the system's generic icon everywhere.
	images, err := winres.EncodeIconData(rendered, sizes)
	if err != nil {
		return err
	}

	rsrc, err := winres.BuildIconResources(images, 1)
	if err != nil {
		return err
	}
	obj, err := winres.WriteObject(machine, rsrc)
	if err != nil {
		return err
	}

	syso := filepath.Join(root, "cmd", "wbtray", "rsrc_windows_"+arch+".syso")
	if err := os.WriteFile(syso, obj, 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s (%d images: %v)\n", syso, len(sizes), sizes)
	return nil
}

// byteOf encodes a dimension the way an icon directory does.
func byteOf(size int) byte {
	if size >= 256 {
		return 0
	}
	return byte(size)
}

// sampleState is a healthy pool, so the mark is drawn with a full gauge rather
// than an empty one.
func sampleState() status.Snapshot {
	return status.Snapshot{
		Reachable: true, Total: 3, Healthy: 3,
		Accounts: []status.Account{
			{UID: "a", Credits: 9000, Total: 12000},
			{UID: "b", Credits: 9000, Total: 12000},
			{UID: "c", Credits: 9000, Total: 12000},
		},
	}
}

// repoRoot walks up from the working directory to the module root, so the
// command works from anywhere in the tree.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found above %s", dir)
		}
		dir = parent
	}
}
