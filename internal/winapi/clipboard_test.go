//go:build windows

package winapi

import (
	"syscall"
	"testing"
	"unsafe"
)

// getClipboardText reads the clipboard back, which is how a test tells "the value
// was written" from "the call returned an error". It deliberately reads the way
// another program would — through the same API, in a fresh open — rather than
// checking a variable this package owns.
func getClipboardText(t *testing.T) string {
	t.Helper()
	avail, _, _ := procIsClipboardFormatAvailable.Call(CFUnicodeText)
	if avail == 0 {
		t.Fatal("the clipboard holds no unicode text")
	}
	if ret, _, _ := ProcOpenClipboard.Call(0); ret == 0 {
		t.Fatal("OpenClipboard failed")
	}
	defer ProcCloseClipboard.Call()

	h, _, _ := procGetClipboardData.Call(CFUnicodeText)
	if h == 0 {
		t.Fatal("GetClipboardData returned nothing")
	}
	ptr, _, _ := ProcGlobalLock.Call(h)
	if ptr == 0 {
		t.Fatal("GlobalLock failed")
	}
	defer ProcGlobalUnlock.Call(h)

	// Read into a local buffer through the same copy primitive the writer uses,
	// which keeps this free of a pointer built from an integer.
	buf := make([]uint16, 4096)
	ProcRtlMoveMemory.Call(uintptr(unsafe.Pointer(&buf[0])), ptr, uintptr(len(buf)*2))

	// The string ends at its first NUL, which StringToUTF16 guarantees is there.
	for i, c := range buf {
		if c == 0 {
			return syscall.UTF16ToString(buf[:i])
		}
	}
	return syscall.UTF16ToString(buf)
}

// TestSetClipboardTextRoundTrips is the check that the copy path works at all.
//
// It is a real test rather than a smoke test because the implementation is raw
// syscalls against GlobalAlloc and the clipboard: a wrong length, a missing lock or a
// forgotten terminator produces a clipboard that a second program cannot read, which
// is indistinguishable from a copy that never happened.
func TestSetClipboardTextRoundTrips(t *testing.T) {
	for _, want := range []string{
		"http://127.0.0.1:7863",
		"sk-2cGOUn3vQ5agutpg-TPrgks_",
		"中文密钥",
	} {
		if err := SetClipboardText(want); err != nil {
			t.Fatalf("SetClipboardText(%q): %v", want, err)
		}
		if got := getClipboardText(t); got != want {
			t.Errorf("the clipboard holds %q, want %q", got, want)
		}
	}
}

// TestSetClipboardTextReplacesThePreviousValue checks the empty-then-set path, which
// is the one that exercises EmptyClipboard: a copy that leaves the previous value in
// place is worse than one that fails, because it silently pastes the wrong thing.
func TestSetClipboardTextReplacesThePreviousValue(t *testing.T) {
	if err := SetClipboardText("first"); err != nil {
		t.Fatal(err)
	}
	if err := SetClipboardText("second"); err != nil {
		t.Fatal(err)
	}
	if got := getClipboardText(t); got != "second" {
		t.Errorf("the clipboard holds %q, want the second value", got)
	}
}

// TestSetClipboardTextRejectsAnEmptyString documents the one refusal: there is no
// useful clipboard content for an empty string, and a row that offered to copy one
// would report success at doing nothing.
func TestSetClipboardTextRejectsAnEmptyString(t *testing.T) {
	if err := SetClipboardText(""); err == nil {
		t.Error("copying an empty string reported success")
	}
}
