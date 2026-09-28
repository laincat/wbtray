//go:build windows

package winapi

import "unsafe"

// Reading the system accent colour.
//
// Windows 11 tints its own surfaces with the accent the operator chose in Settings, and
// a program that wants to look like part of the system has to use the same value rather
// than pick a colour of its own. The value lives in the registry as a packed word, in
// the byte order the shell's own colour picker writes.

// AccentColor reads the operator's accent colour as a packed 0x00BBGGRR word. The
// second result is false when the value is absent, which is the case on a build that
// predates the setting: the caller then keeps whatever colour it had rather than
// adopting a guess.
//
// The word is returned as it was read rather than as a colour, because this package
// deals in Win32 values and does not import the drawing packages.
func AccentColor() (uint32, bool) {
	sub := UTF16Ptr(`Software\\Microsoft\\Windows\\DWM`)
	var key uintptr
	ret, _, _ := procRegOpenKeyExW.Call(hkeyCurrentUser,
		uintptr(unsafe.Pointer(sub)), 0, keyQueryValue, uintptr(unsafe.Pointer(&key)))
	if ret != 0 {
		return 0, false
	}
	defer procRegCloseKey.Call(key)

	name := UTF16Ptr("AccentColor")
	var packed, size uint32 = 0, 4
	ret, _, _ = procRegQueryValueExW.Call(key,
		uintptr(unsafe.Pointer(name)), 0, 0,
		uintptr(unsafe.Pointer(&packed)), uintptr(unsafe.Pointer(&size)))
	if ret != 0 {
		return 0, false
	}
	// The alpha byte carries nothing useful: the shell writes FF there whether or not
	// the colour is in use, so only the three colour bytes are kept.
	return packed & 0xffffff, true
}
