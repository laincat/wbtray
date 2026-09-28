//go:build windows

// Package winapi holds the Win32 bindings wbtray needs, and nothing else.
//
// Deliberately free of third-party code: every call here is a thin syscall
// against a documented user32/kernel32/gdi32/shell32 entry point. The
// declarations live in one file so it stays possible to see, at a glance, how
// much of the operating system this program depends on.
package winapi

import (
	"fmt"
	"syscall"
	"unsafe"
)

// DLLs are loaded lazily and resolved once per process.
var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	user32   = syscall.NewLazyDLL("user32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")

	ProcGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")

	ProcRegisterClassExW    = user32.NewProc("RegisterClassExW")
	ProcCreateWindowExW     = user32.NewProc("CreateWindowExW")
	ProcDefWindowProcW      = user32.NewProc("DefWindowProcW")
	ProcDestroyWindow       = user32.NewProc("DestroyWindow")
	ProcPostQuitMessage     = user32.NewProc("PostQuitMessage")
	ProcGetMessageW         = user32.NewProc("GetMessageW")
	ProcTranslateMessage    = user32.NewProc("TranslateMessage")
	ProcDispatchMessageW    = user32.NewProc("DispatchMessageW")
	ProcPostMessageW        = user32.NewProc("PostMessageW")
	ProcSetTimer            = user32.NewProc("SetTimer")
	ProcKillTimer           = user32.NewProc("KillTimer")
	ProcCreatePopupMenu     = user32.NewProc("CreatePopupMenu")
	ProcDestroyMenu         = user32.NewProc("DestroyMenu")
	ProcAppendMenuW         = user32.NewProc("AppendMenuW")
	ProcTrackPopupMenu      = user32.NewProc("TrackPopupMenu")
	ProcGetCursorPos        = user32.NewProc("GetCursorPos")
	ProcSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	ProcLoadIconW           = user32.NewProc("LoadIconW")
	ProcDestroyIcon         = user32.NewProc("DestroyIcon")
	ProcCreateIconIndirect  = user32.NewProc("CreateIconIndirect")
	ProcShowWindow          = user32.NewProc("ShowWindow")
	ProcSetWindowPos        = user32.NewProc("SetWindowPos")
	ProcGetWindowRect       = user32.NewProc("GetWindowRect")
	ProcBeginPaint          = user32.NewProc("BeginPaint")
	ProcEndPaint            = user32.NewProc("EndPaint")
	ProcInvalidateRect      = user32.NewProc("InvalidateRect")
	ProcUpdateWindow        = user32.NewProc("UpdateWindow")
	ProcFillRect            = user32.NewProc("FillRect")
	ProcReleaseCapture      = user32.NewProc("ReleaseCapture")
	ProcGetSystemMetrics    = user32.NewProc("GetSystemMetrics")
	ProcSetCapture          = user32.NewProc("SetCapture")
	ProcMessageBoxW         = user32.NewProc("MessageBoxW")
	ProcLoadCursorW         = user32.NewProc("LoadCursorW")
	ProcGetMonitorInfoW     = user32.NewProc("GetMonitorInfoW")
	ProcMonitorFromPoint    = user32.NewProc("MonitorFromPoint")
	ProcTrackMouseEvent     = user32.NewProc("TrackMouseEvent")
	ProcGetDoubleClickTime  = user32.NewProc("GetDoubleClickTime")
	ProcGetForegroundWindow = user32.NewProc("GetForegroundWindow")
	ProcGetTickCount        = kernel32.NewProc("GetTickCount")
	ProcGetAsyncKeyState    = user32.NewProc("GetAsyncKeyState")

	ProcShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")

	// The clipboard, bound directly.
	//
	// The obvious implementation is `cmd /c clip` with the text on its standard
	// input, and it is the wrong one for a program that has no console: it starts
	// a console host for every copy, and it resolves `clip` through the current
	// directory before the system one, so a file of that name beside the tray is
	// executed instead. These four calls have neither problem.
	ProcOpenClipboard    = user32.NewProc("OpenClipboard")
	ProcCloseClipboard   = user32.NewProc("CloseClipboard")
	ProcEmptyClipboard   = user32.NewProc("EmptyClipboard")
	ProcSetClipboardData = user32.NewProc("SetClipboardData")
	ProcGlobalAlloc      = kernel32.NewProc("GlobalAlloc")
	ProcGlobalLock       = kernel32.NewProc("GlobalLock")
	ProcGlobalUnlock     = kernel32.NewProc("GlobalUnlock")
	ProcRtlMoveMemory    = kernel32.NewProc("RtlMoveMemory")

	User32DLL = user32
	GDI32DLL  = gdi32

	procGetDC     = user32.NewProc("GetDC")
	procReleaseDC = user32.NewProc("ReleaseDC")

	ProcCreateDIBSection    = gdi32.NewProc("CreateDIBSection")
	ProcDeleteObject        = gdi32.NewProc("DeleteObject")
	ProcCreateCompatibleDC  = gdi32.NewProc("CreateCompatibleDC")
	ProcDeleteDC            = gdi32.NewProc("DeleteDC")
	ProcSelectObject        = gdi32.NewProc("SelectObject")
	ProcCreateFontIndirectW = gdi32.NewProc("CreateFontIndirectW")
	ProcSetTextColor        = gdi32.NewProc("SetTextColor")
	ProcSetBkMode           = gdi32.NewProc("SetBkMode")
	ProcCreateSolidBrush    = gdi32.NewProc("CreateSolidBrush")
	ProcGetDeviceCaps       = gdi32.NewProc("GetDeviceCaps")

	// The tray deliberately renders its own glyphs, but GDI still draws the
	// flyout's text: DirectWrite through COM would be a large amount of code for
	// one label per row, and the bitmap font is only legible at icon sizes.
	ProcTextOutW  = gdi32.NewProc("TextOutW")
	ProcBitBlt    = gdi32.NewProc("BitBlt")
	ProcGetDC     = user32.NewProc("GetDC")
	ProcReleaseDC = user32.NewProc("ReleaseDC")

	// The four calls an owner-drawn menu row needs beyond what is above: the
	// system palette, the font metrics of the DC the shell hands over, and the two
	// primitives that draw a pip.
	ProcGetSysColor           = user32.NewProc("GetSysColor")
	ProcGetTextMetricsW       = gdi32.NewProc("GetTextMetricsW")
	ProcGetTextExtentPoint32W = gdi32.NewProc("GetTextExtentPoint32W")
	ProcEllipse               = gdi32.NewProc("Ellipse")
	ProcCreatePen             = gdi32.NewProc("CreatePen")
	ProcGetStockObject        = gdi32.NewProc("GetStockObject")
	ProcDrawTextW             = user32.NewProc("DrawTextW")
	ProcGetClientRect         = user32.NewProc("GetClientRect")

	// The read side of the clipboard, used by the tests to check what a second
	// program would see.
	procIsClipboardFormatAvailable = user32.NewProc("IsClipboardFormatAvailable")
	procGetClipboardData           = user32.NewProc("GetClipboardData")
)

// DwmAPI is loaded lazily like the rest: it is only for the flyout window's
// rounded corners, and a system without it simply gets square ones.
var (
	DwmAPI                    = syscall.NewLazyDLL("DwmAPI.dll")
	ProcDwmSetWindowAttribute = DwmAPI.NewProc("DwmSetWindowAttribute")
)

// Point mirrors the POINT structure.
type Point struct{ X, Y int32 }

// Rect mirrors the RECT structure.
type Rect struct{ Left, Top, Right, Bottom int32 }

// RectF is RECT in floating point, which is what the layout code works in.
type RectF struct{ X, Y, W, H float64 }

// PaintStruct mirrors PAINTSTRUCT. Only the device context is read by the
// caller; the rest exists so the structure is the right size.
type PaintStruct struct {
	HDC         uintptr
	FErase      int32
	RcPaint     Rect
	FRestore    int32
	FIncUpdate  int32
	RgbReserved [32]byte
}

// LogFont mirrors LOGFONTW.
type LogFont struct {
	Height         int32
	Width          int32
	Escapement     int32
	Orientation    int32
	Weight         int32
	Italic         byte
	Underline      byte
	StrikeOut      byte
	CharSet        byte
	OutPrecision   byte
	ClipPrecision  byte
	Quality        byte
	PitchAndFamily byte
	FaceName       [32]uint16
}

// MonitorInfo mirrors MONITORINFO.
type MonitorInfo struct {
	CbSize    uint32
	RcMonitor Rect
	RcWork    Rect
	DwFlags   uint32
}

// TrackMouseEventStruct mirrors TRACKMOUSEEVENT.
type TrackMouseEventStruct struct {
	CbSize      uint32
	DwFlags     uint32
	HwndTrack   uintptr
	DwHoverTime uint32
}

// System metrics and shell constants used across the tray.
const (
	SmCXSmallIcon           = 49
	SmCYScreen              = 1
	SmCXScreen              = 0
	SmCXSmIcon              = 49
	SmCYSmIcon              = 50
	MonitorDefaultToNearest = 2
)

// Msg mirrors the MSG structure.
type Msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      Point
}

// WndClassExW mirrors the WNDCLASSEXW structure.
type WndClassExW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

// NotifyIconData mirrors the NOTIFYICONDATAW structure.
type NotifyIconData struct {
	CbSize           uint32
	Hwnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         syscall.GUID
	HBalloonIcon     uintptr
}

// UTF16Ptr is a convenience wrapper for the most common conversion.
func UTF16Ptr(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

// CopyUTF16 copies s into a fixed-size UTF-16 buffer, always NUL-terminating.
// Out-of-range input is truncated rather than rejected: tooltips and balloon
// text are advisory, so losing the tail beats losing the whole label.
func CopyUTF16(dst []uint16, s string) {
	src := syscall.StringToUTF16(s)
	if len(src) > len(dst) {
		src = src[:len(dst)]
		src[len(src)-1] = 0
	}
	copy(dst, src)
}

// LowWord extracts the low 16 bits of a message parameter (menu / item ids).
func LowWord(v uintptr) uint32 { return uint32(v & 0xffff) }

// The DrawTextW flags this program uses, named where the binding is.
const (
	// DtLeft aligns text to the left edge of the rectangle.
	DtLeft = 0x00000000
	// DtVCenter centres a single line vertically.
	DtVCenter = 0x00000004
	// DtSingleLine draws one line, so a long sentence does not wrap into a block.
	DtSingleLine = 0x0020
	// DtNoPrefix stops an ampersand in the text being read as a keyboard prefix.
	DtNoPrefix = 0x0800
	// DtEndEllipsis shortens a sentence that does not fit, rather than clipping it
	// mid-character.
	DtEndEllipsis = 0x00008000
)

// CFUnicodeText is the clipboard format for a UTF-16 string.
const CFUnicodeText = 13

// gmemMoveable is GMEM_MOVEABLE, which is mandatory for clipboard memory: the
// clipboard manager has to be able to move it.
const gmemMoveable = 0x0002

// SetClipboardText puts a string on the clipboard as UTF-16 text.
//
// The memory handed to SetClipboardData belongs to the system from that moment
// on, so it is deliberately not freed: releasing it would leave every paste in
// every program pointing at freed memory. The failure paths do free it, because
// nothing has taken ownership yet.
func SetClipboardText(s string) error {
	if s == "" {
		return fmt.Errorf("nothing to copy")
	}
	// The source is a UTF-16 slice, so the copy carries the terminating NUL that
	// every clipboard reader expects to find.
	utf16 := syscall.StringToUTF16(s)
	n := len(utf16) * 2

	h, _, err := ProcGlobalAlloc.Call(gmemMoveable, uintptr(n))
	if h == 0 {
		return fmt.Errorf("GlobalAlloc: %v", err)
	}
	ptr, _, _ := ProcGlobalLock.Call(h)
	if ptr == 0 {
		return fmt.Errorf("GlobalLock failed")
	}
	// RtlMoveMemory copies between two raw addresses, which is the one form of
	// this that does not turn a uintptr back into a pointer: the source is passed
	// as an address and the destination is the handle's own address, so neither
	// conversion outlives the call.
	ProcRtlMoveMemory.Call(ptr, uintptr(unsafe.Pointer(&utf16[0])), uintptr(n))
	ProcGlobalUnlock.Call(h)

	if ret, _, err := ProcOpenClipboard.Call(0); ret == 0 {
		return fmt.Errorf("OpenClipboard: %v", err)
	}
	defer ProcCloseClipboard.Call()
	ProcEmptyClipboard.Call()
	if ret, _, err := ProcSetClipboardData.Call(CFUnicodeText, h); ret == 0 {
		return fmt.Errorf("SetClipboardData: %v", err)
	}
	return nil
}

// DIBHeader mirrors BITMAPV5HEADER far enough to ask for a 32-bit top-down DIB
// with an alpha channel. Declaring the masks is what makes the shell honour
// per-pixel alpha instead of treating the bitmap as opaque.
type DIBHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
	RedMask       uint32
	GreenMask     uint32
	BlueMask      uint32
	AlphaMask     uint32
	CSType        uint32
}

// NewDIBSection creates a 32-bit top-down DIB of the given size and returns its
// handle plus a pointer to its pixels, which the caller writes through. A
// negative height is what makes the rows go top-down, matching every raster in
// this program.
func NewDIBSection(w, h int) (uintptr, unsafe.Pointer) {
	if w <= 0 || h <= 0 {
		return 0, nil
	}
	hdr := DIBHeader{
		Size:        uint32(unsafe.Sizeof(DIBHeader{})),
		Width:       int32(w),
		Height:      int32(-h),
		Planes:      1,
		BitCount:    32,
		Compression: 3, // BI_BITFIELDS
		RedMask:     0x00ff0000,
		GreenMask:   0x0000ff00,
		BlueMask:    0x000000ff,
		AlphaMask:   0xff000000,
	}
	var bits unsafe.Pointer
	hbm, _, _ := ProcCreateDIBSection.Call(0,
		uintptr(unsafe.Pointer(&hdr)), 0,
		uintptr(unsafe.Pointer(&bits)), 0, 0)
	if hbm == 0 || bits == nil {
		return 0, nil
	}
	return hbm, bits
}

// ScreenPoint packs a screen coordinate the way MonitorFromPoint expects it.
func ScreenPoint(x, y int) uintptr {
	return uintptr(uint32(x)) | uintptr(uint32(y))<<32
}

// IconMetric is SM_CXSMICON: 16 at 100% scaling, 20 at 125%, 24 at 150%, 32 at
// 200%. Rendering at this size is what keeps the icon crisp on a scaled display.
func IconMetric() int {
	v, _, _ := ProcGetSystemMetrics.Call(SmCXSmallIcon)
	if v < 16 {
		return 16
	}
	if v > 64 {
		return 64
	}
	return int(v)
}

// DPIScaleFor reports a window's scaling factor, falling back to the system DPI
// where GetDpiForWindow is missing. A slightly wrong scale is cosmetic, so this
// never fails.
func DPIScaleFor(hwnd uintptr) float64 {
	if ProcGetDpiForWindow.Find() == nil {
		dpi, _, _ := ProcGetDpiForWindow.Call(hwnd)
		if dpi > 0 {
			return float64(dpi) / 96
		}
	}
	dc, _, _ := ProcGetDC.Call(0)
	if dc == 0 {
		return 1
	}
	defer ProcReleaseDC.Call(0, dc)
	const logPixelsX = 88
	dpi, _, _ := ProcGetDeviceCaps.Call(dc, logPixelsX)
	if dpi == 0 {
		return 1
	}
	return float64(dpi) / 96
}

// ProcGetDpiForWindow is optional, so it is resolved separately rather than in
// the table of calls that must exist.
var ProcGetDpiForWindow = User32DLL.NewProc("GetDpiForWindow")

// WorkArea returns the work area of the monitor a point is on, which is what
// keeps the tray's own windows from landing under the taskbar.
func WorkArea(x, y int) Rect {
	mi := MonitorInfo{CbSize: uint32(unsafe.Sizeof(MonitorInfo{}))}
	hmon, _, _ := ProcMonitorFromPoint.Call(ScreenPoint(x, y), MonitorDefaultToNearest)
	ProcGetMonitorInfoW.Call(hmon, uintptr(unsafe.Pointer(&mi)))
	if mi.RcWork.Right == 0 && mi.RcWork.Bottom == 0 {
		// A monitor query that returned nothing: use the primary screen metrics,
		// which is the same answer for the ordinary single-monitor case.
		w, _, _ := ProcGetSystemMetrics.Call(SmCXScreen)
		h, _, _ := ProcGetSystemMetrics.Call(SmCYScreen)
		return Rect{0, 0, int32(w), int32(h)}
	}
	return mi.RcWork
}
