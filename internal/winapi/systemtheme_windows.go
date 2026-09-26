//go:build windows

package winapi

import (
	"syscall"
	"unsafe"
)

// Reading the system's app theme.
//
// Windows exposes it as a registry value rather than through an API, and the
// value's absence is meaningful: it did not exist before Windows 10 1809, and a
// build without it is light by default. That is why a missing value is treated
// as light rather than as an error.

const (
	hkeyCurrentUser = 0x80000001
	keyQueryValue   = 0x0001
	regDWORD        = 4

	personalizeKey = `SoftwareMicrosoftWindowsCurrentVersionThemesPersonalize`
	appsUseLight   = "AppsUseLightTheme"
)

var (
	advapi32 = syscall.NewLazyDLL("advapi32.dll")

	procRegOpenKeyExW    = advapi32.NewProc("RegOpenKeyExW")
	procRegQueryValueExW = advapi32.NewProc("RegQueryValueExW")
	procRegCloseKey      = advapi32.NewProc("RegCloseKey")
)

// SystemDark reports whether Windows is currently using its dark app theme.
//
// It is read on every use rather than cached: the operator can switch modes while
// the tray is running, and a cached answer would leave the icon on the wrong side
// until the next restart.
func SystemDark() bool {
	sub := UTF16Ptr(personalizeKey)
	var key uintptr
	ret, _, _ := procRegOpenKeyExW.Call(hkeyCurrentUser,
		uintptr(unsafe.Pointer(sub)), 0, keyQueryValue, uintptr(unsafe.Pointer(&key)))
	if ret != 0 {
		return false
	}
	defer procRegCloseKey.Call(key)

	name := UTF16Ptr(appsUseLight)
	var value, size uint32 = 1, 4
	ret, _, _ = procRegQueryValueExW.Call(key,
		uintptr(unsafe.Pointer(name)), 0, 0,
		uintptr(unsafe.Pointer(&value)), uintptr(unsafe.Pointer(&size)))
	if ret != 0 {
		// No value means a build that predates the setting, which is light.
		return false
	}
	// AppsUseLightTheme is inverted, as its name says.
	return value == 0
}
