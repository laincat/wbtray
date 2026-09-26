//go:build windows

// Package autostart registers wbtray to launch with the user's session.
//
// It writes only HKCU, never HKLM: starting with the user's session is a
// per-user preference and does not need administrator rights, so wbtray never
// asks for elevation.
package autostart

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

// ValueName is the registry value wbtray owns under Run.
const ValueName = "wbtray"

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// Registry access goes through advapi32 directly rather than a helper package:
// the four calls needed here are stable since Windows 2000, and staying on
// syscall keeps the module dependency-free.
const (
	hkeyCurrentUser   = 0x80000001
	keyQueryValue     = 0x0001
	keySetValue       = 0x0002
	regSZ             = 1
	errorFileNotFound = 2
)

var (
	advapi32 = syscall.NewLazyDLL("advapi32.dll")

	procRegOpenKeyExW    = advapi32.NewProc("RegOpenKeyExW")
	procRegCreateKeyExW  = advapi32.NewProc("RegCreateKeyExW")
	procRegSetValueExW   = advapi32.NewProc("RegSetValueExW")
	procRegQueryValueExW = advapi32.NewProc("RegQueryValueExW")
	procRegDeleteValueW  = advapi32.NewProc("RegDeleteValueW")
	procRegCloseKey      = advapi32.NewProc("RegCloseKey")
)

// GatewayValueName is the registry value the gateway gets when the tray is asked
// to start it with Windows. The two entries are separate on purpose: a gateway
// can be wanted without the tray, and the tray without the gateway.
const GatewayValueName = "wbtray-gateway"

// EnabledValue reports whether a named value exists under Run.
func EnabledValue(name string) bool {
	k, err := openRunKey(keyQueryValue)
	if err != nil {
		return false
	}
	defer procRegCloseKey.Call(k)

	value := utf16Bytes(name)
	var typ, size uint32
	ret, _, _ := procRegQueryValueExW.Call(k,
		uintptr(unsafe.Pointer(&value[0])), 0,
		uintptr(unsafe.Pointer(&typ)), 0,
		uintptr(unsafe.Pointer(&size)))
	return ret == 0
}

// EnableCommand writes a command line under Run, which is how the gateway is
// registered without the tray having to be its parent.
func EnableCommand(name, command string) error {
	k, err := createRunKey()
	if err != nil {
		return err
	}
	defer procRegCloseKey.Call(k)
	data := utf16Bytes(quote(command))
	value := utf16Bytes(name)
	ret, _, callErr := procRegSetValueExW.Call(k,
		uintptr(unsafe.Pointer(&value[0])), 0, regSZ,
		uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)))
	if ret != 0 {
		return fmt.Errorf("set %s autostart value: %v", name, callErr)
	}
	return nil
}

// DisableValue removes a named value under Run.
func DisableValue(name string) error {
	k, err := openRunKey(keySetValue)
	if err != nil {
		return nil
	}
	defer procRegCloseKey.Call(k)
	value := utf16Bytes(name)
	if ret, _, _ := procRegDeleteValueW.Call(k, uintptr(unsafe.Pointer(&value[0]))); ret != 0 && ret != errorFileNotFound {
		return fmt.Errorf("delete %s value: error %d", name, ret)
	}
	return nil
}

// Enabled reports whether the tray's own autostart entry exists.
func Enabled() bool {
	k, err := openRunKey(keyQueryValue)
	if err != nil {
		return false
	}
	defer procRegCloseKey.Call(k)

	name := utf16Bytes(ValueName)
	var typ, size uint32
	// A NULL data pointer with a non-nil size is the documented way to ask
	// for the value's size without reading it.
	ret, _, _ := procRegQueryValueExW.Call(k,
		uintptr(unsafe.Pointer(&name[0])), 0,
		uintptr(unsafe.Pointer(&typ)), 0,
		uintptr(unsafe.Pointer(&size)))
	return ret == 0
}

// Enable writes the autostart entry for the running executable.
func Enable() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate executable: %w", err)
	}
	k, err := createRunKey()
	if err != nil {
		return err
	}
	defer procRegCloseKey.Call(k)

	// Quoting the path is required: an unquoted path containing spaces would
	// be parsed as a command plus arguments.
	data := utf16Bytes(quote(exe))
	name := utf16Bytes(ValueName)
	ret, _, callErr := procRegSetValueExW.Call(k,
		uintptr(unsafe.Pointer(&name[0])), 0, regSZ,
		uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)))
	if ret != 0 {
		return fmt.Errorf("set autostart value: %v", callErr)
	}
	return nil
}

// Disable removes the autostart entry.
func Disable() error {
	k, err := openRunKey(keySetValue)
	if err != nil {
		// A missing key means nothing was registered, which is the goal.
		return nil
	}
	defer procRegCloseKey.Call(k)

	name := utf16Bytes(ValueName)
	if ret, _, _ := procRegDeleteValueW.Call(k, uintptr(unsafe.Pointer(&name[0]))); ret != 0 && ret != errorFileNotFound {
		return fmt.Errorf("delete autostart value: error %d", ret)
	}
	return nil
}

// openRunKey opens the Run key with the requested access.
func openRunKey(access uint32) (uintptr, error) {
	sub := utf16Bytes(runKey)
	var k uintptr
	ret, _, _ := procRegOpenKeyExW.Call(hkeyCurrentUser,
		uintptr(unsafe.Pointer(&sub[0])), 0, uintptr(access),
		uintptr(unsafe.Pointer(&k)))
	if ret != 0 {
		return 0, fmt.Errorf("open Run key: error %d", ret)
	}
	return k, nil
}

// createRunKey opens the Run key for writing, creating it if absent.
func createRunKey() (uintptr, error) {
	sub := utf16Bytes(runKey)
	var k, disposition uintptr
	ret, _, _ := procRegCreateKeyExW.Call(hkeyCurrentUser,
		uintptr(unsafe.Pointer(&sub[0])), 0, 0, 0,
		keySetValue, 0,
		uintptr(unsafe.Pointer(&k)), uintptr(unsafe.Pointer(&disposition)))
	if ret != 0 {
		return 0, fmt.Errorf("create Run key: error %d", ret)
	}
	return k, nil
}

// utf16Bytes encodes s as a NUL-terminated UTF-16LE byte slice, which is the
// shape the registry expects for REG_SZ.
func utf16Bytes(s string) []byte {
	u := syscall.StringToUTF16(s)
	return unsafe.Slice((*byte)(unsafe.Pointer(&u[0])), len(u)*2)
}

// Set enables or disables autostart.
func Set(on bool) error {
	if on {
		return Enable()
	}
	return Disable()
}

// quote wraps a path in quotes unless it already is.
func quote(path string) string {
	if strings.HasPrefix(path, `"`) {
		return path
	}
	return `"` + path + `"`
}

// ConfigDir returns the directory wbtray reads and writes its files in.
//
// A file next to the executable wins, because that is what a portable copy
// implies; otherwise the per-user configuration directory is used, which keeps
// a Program Files install writable.
func ConfigDir() string {
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		if dir != "" && writable(dir) {
			return dir
		}
	}
	if dir, err := os.UserConfigDir(); err == nil {
		sub := filepath.Join(dir, "wbtray")
		_ = os.MkdirAll(sub, 0o755)
		return sub
	}
	return "."
}

// writable reports whether a directory may be written to.
func writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".wbtray-probe-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	_ = os.Remove(name)
	return true
}
