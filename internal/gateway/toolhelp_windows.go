//go:build windows

package gateway

import "syscall"

// The process-list bindings.
//
// These live here rather than in internal/winapi because only this package
// needs them, and a process walk is the kind of thing that belongs with the code
// that interprets its result.

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	user32   = syscall.NewLazyDLL("user32.dll")

	procCreateToolhelp32Snapshot = kernel32.NewProc("CreateToolhelp32Snapshot")
	procProcess32FirstW          = kernel32.NewProc("Process32FirstW")
	procProcess32NextW           = kernel32.NewProc("Process32NextW")
	procCloseHandle              = kernel32.NewProc("CloseHandle")

	procShowWindow               = user32.NewProc("ShowWindow")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procGetWindowThreadProcessID = user32.NewProc("GetWindowThreadProcessId")
	procGetClassNameW            = user32.NewProc("GetClassNameW")
	procEnumWindows              = user32.NewProc("EnumWindows")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
)

const th32csSnapProcess = 0x00000002

// processEntry32 mirrors PROCESSENTRY32W.
type processEntry32 struct {
	Size            uint32
	CntUsage        uint32
	ProcessID       uint32
	DefaultHeapID   uintptr
	ModuleID        uint32
	CntThreads      uint32
	ParentProcessID uint32
	PriClassBase    int32
	Flags           uint32
	ExeFile         [260]uint16
}

func createToolhelp32Snapshot() uintptr {
	h, _, _ := procCreateToolhelp32Snapshot.Call(th32csSnapProcess, 0)
	return h
}
