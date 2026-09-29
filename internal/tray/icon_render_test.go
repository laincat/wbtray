//go:build windows

package tray

import (
	"testing"
	"unsafe"

	"wbtray/internal/config"
	"wbtray/internal/iconstyle"
	"wbtray/internal/raster"
	"wbtray/internal/status"
	"wbtray/internal/theme"
	"wbtray/internal/winapi"
)

// TestTheIconTheShellIsGivenIsNotBlank renders the icon the tray hands the shell and
// then draws it the way the shell does, through the HICON rather than the canvas.
//
// The two are not the same thing: a canvas full of ink becomes an invisible icon if
// the conversion loses the alpha channel, and "Shell_NotifyIcon returned success" is
// no reassurance at all — a blank icon registers perfectly and shows nothing.
func TestTheIconTheShellIsGivenIsNotBlank(t *testing.T) {
	pal := theme.ForName(theme.DefaultName, false)
	snap := status.Snapshot{
		Reachable: true, Version: "1.11.9-panel", Uptime: 3600,
		Healthy: 1, Total: 1,
		Accounts: []status.Account{{UID: "u1", Nickname: "Laincat", Credits: 7954, Total: 14505}},
	}
	c := iconstyle.Draw(iconstyle.View{
		Size: 16, Style: config.StyleGauge, Metric: config.MetricAccounts,
		Lang: "en", Palette: pal, Snap: snap,
	})
	if c == nil {
		t.Fatal("the icon canvas was not drawn")
	}

	hicon := iconFromCanvas(c)
	if hicon == 0 {
		t.Fatal("iconFromCanvas produced no HICON")
	}
	defer winapi.ProcDestroyIcon.Call(hicon)

	// Draw it into a known background the way the shell draws it, then count the
	// pixels that differ from that background. An icon that renders blank leaves the
	// background untouched.
	const size = 16
	hdc, _, _ := winapi.ProcCreateCompatibleDC.Call(0)
	if hdc == 0 {
		t.Fatal("no DC")
	}
	defer winapi.ProcDeleteDC.Call(hdc)
	hbm, bits := winapi.NewDIBSection(size, size)
	if hbm == 0 {
		t.Fatal("no DIB")
	}
	defer winapi.ProcDeleteObject.Call(hbm)
	old, _, _ := winapi.ProcSelectObject.Call(hdc, hbm)
	defer winapi.ProcSelectObject.Call(hdc, old)

	// A mid-magenta background: a colour no state or ink in this palette uses, so a
	// pixel that still holds it is a pixel the icon did not paint.
	const (
		bgR, bgG, bgB = 0xff, 0x00, 0xff
	)
	pix := unsafe.Slice((*byte)(bits), size*size*4)
	for i := 0; i+3 < len(pix); i += 4 {
		pix[i+0], pix[i+1], pix[i+2], pix[i+3] = bgB, bgG, bgR, 0xff
	}

	// DI_NORMAL: image and mask, which is what the shell uses.
	const diNormal = 0x0003
	winapi.ProcDrawIconEx.Call(hdc, 0, 0, hicon,
		size, size, 0, 0, diNormal)

	painted := 0
	for i := 0; i+3 < len(pix); i += 4 {
		if pix[i+0] != bgB || pix[i+1] != bgG || pix[i+2] != bgR {
			painted++
		}
	}
	if painted == 0 {
		t.Fatal("the icon draws nothing when the shell draws it: " +
			"every pixel is still the background")
	}
	t.Logf("icon paints %d of %d pixels", painted, size*size)
	_ = raster.RGBA{}
}
