//go:build windows

package main

import (
	"wbtray/internal/raster"
	"wbtray/internal/theme"
	"wbtray/internal/winapi"
)

// The accent the tray draws with.
//
// It is read from Windows rather than chosen here, which is the point: an icon sitting on
// the taskbar beside the system's own should use the same accent, because two different
// blues on one taskbar read as one of them being wrong. The setting is read once at
// startup and again whenever the icon is drawn, so a change in Settings reaches the
// taskbar without a restart.
func systemAccent() theme.Accent {
	packed, ok := winapi.AccentColor()
	if !ok {
		return theme.Accent{}
	}
	// The word is 0x00BBGGRR, which is the order the shell writes it in.
	return theme.Accent{
		Known: true,
		Colour: raster.RGBA{
			R: uint8(packed & 0xff),
			G: uint8((packed >> 8) & 0xff),
			B: uint8((packed >> 16) & 0xff),
			A: 0xff,
		},
	}
}
