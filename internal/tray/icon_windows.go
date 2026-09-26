//go:build windows

package tray

import (
	"unsafe"

	"wbtray/internal/raster"
	"wbtray/internal/winapi"
)

// Converting a rendered canvas into the handles Windows wants.

// iconInfo mirrors ICONINFO.
type iconInfo struct {
	FIcon    int32
	XHotspot uint32
	YHotspot uint32
	HbmMask  uintptr
	HbmColor uintptr
}

// iconFromCanvas turns a rendered canvas into an HICON. The caller owns the
// handle and must release it with DestroyIcon.
func iconFromCanvas(c *raster.Canvas) uintptr {
	if c == nil || c.W <= 0 || c.H <= 0 {
		return 0
	}
	hbm, bits := winapi.NewDIBSection(c.W, c.H)
	if hbm == 0 {
		return 0
	}
	// The DIB wants premultiplied BGRA, which is what BGRA returns.
	dst := unsafe.Slice((*byte)(bits), c.W*c.H*4)
	copy(dst, c.BGRA())

	// ICONINFO requires a mask bitmap even when alpha is present; an all-zero
	// mask defers entirely to the alpha channel.
	hMask, maskBits := winapi.NewDIBSection(c.W, c.H)
	if hMask != 0 && maskBits != nil {
		zero := unsafe.Slice((*byte)(maskBits), c.W*c.H*4)
		for i := range zero {
			zero[i] = 0
		}
	}

	ii := iconInfo{FIcon: 1, HbmMask: hMask, HbmColor: hbm}
	hicon, _, _ := winapi.ProcCreateIconIndirect.Call(uintptr(unsafe.Pointer(&ii)))

	// The DIBs are copied into the icon, so they can be released now.
	winapi.ProcDeleteObject.Call(hbm)
	if hMask != 0 {
		winapi.ProcDeleteObject.Call(hMask)
	}
	return hicon
}
