//go:build windows

package gateway

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"unsafe"
)

// Running the gateway.

// Process is the gateway as far as the tray can see it.
type Process struct {
	Exe   string
	PID   uint32
	Found bool
	// Started is true only when this tray process launched it, which is what
	// decides whether stopping it is the tray's business.
	Started bool

	cmd *exec.Cmd
}

const (
	createNewProcessGroup = 0x00000200
	createNewConsole      = 0x00000010
)

// FindRunning looks for a gateway process that is already running, wherever it
// came from. It walks the process list by executable name and takes the first
// match, which is what makes the tray useful next to a gateway it did not start.
func FindRunning() Process {
	procs := FindAll()
	if len(procs) > 0 {
		return procs[0]
	}
	return Process{}
}

// FindAll returns every process running under the gateway's name.
//
// The tray only ever acts on the first, but the list is what makes "is this
// specific process still alive" answerable — the question a test asks before it
// trusts a Stop, and the one an operator asks after seeing two gateways fight
// over a port.
func FindAll() []Process {
	snapshot := createToolhelp32Snapshot()
	if snapshot == 0 {
		return nil
	}
	defer procCloseHandle.Call(snapshot)

	var out []Process
	var entry processEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	ret, _, _ := procProcess32FirstW.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	for ret != 0 {
		if syscall.UTF16ToString(entry.ExeFile[:]) == DefaultName {
			out = append(out, Process{Found: true, PID: entry.ProcessID})
		}
		entry.Size = uint32(unsafe.Sizeof(entry))
		ret, _, _ = procProcess32NextW.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	}
	return out
}

// Alive reports whether a specific process is still running.
func Alive(pid uint32) bool {
	if pid == 0 {
		return false
	}
	for _, p := range FindAll() {
		if p.PID == pid {
			return true
		}
	}
	return false
}

// Start launches the gateway from the given executable.
//
// The console is always allocated, and merely hidden when one is not wanted. That
// is deliberate, and it is a change from starting the gateway with no console at
// all: a process created with CREATE_NO_WINDOW has no console to show, so "show the
// console" could never work on a gateway the tray had started — the process was
// already running and a console cannot be attached to it after the fact. Allocating
// one and hiding it costs nothing visible and makes the switch work in both
// directions.
//
// showConsole decides whether it appears. A gateway started by a double click shows
// one, because that is where its log goes; started by the tray it starts hidden,
// since the tray reads the same log over HTTP.
//
// extra carries any further arguments, which the tray does not use but a deployment
// that starts the gateway with its own flags does.
func Start(exe, port string, showConsole bool, extra ...string) (Process, error) {
	if exe == "" {
		return Process{}, fmt.Errorf("gateway executable not found")
	}
	// The gateway takes no listen flag; it reads WB2A_LISTEN from the environment
	// before its own configuration file, which is the documented way for a host to
	// say where it should bind. Passing the port this way keeps the tray's base URL
	// and the gateway's listener in step without editing the operator's config.json.
	cmd := exec.Command(exe, extra...)
	cmd.Dir = WorkDir(exe)
	if port != "" {
		cmd.Env = append(os.Environ(), "WB2A_LISTEN=:"+port)
	}
	// CREATE_NEW_CONSOLE in both cases: without it a console program started by a
	// program that has no console of its own gets a console by accident, and with
	// the flag it gets one on purpose.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNewProcessGroup | createNewConsole,
		// SW_HIDE. The console exists and does not appear, which is the state the
		// show/hide switch then takes over from.
		HideWindow: !showConsole,
	}
	if err := cmd.Start(); err != nil {
		return Process{}, err
	}
	p := Process{Exe: exe, Found: true, Started: true, cmd: cmd}
	if cmd.Process != nil {
		p.PID = uint32(cmd.Process.Pid)
	}
	return p, nil
}

// Stop asks the gateway to exit, then, if it does not, ends the process tree.
//
// The taskkill call is what handles a gateway that spawned children of its own;
// ending only the parent would leave a listener holding the port.
func Stop(pid uint32) error {
	if pid == 0 {
		return nil
	}
	kill := exec.Command("taskkill", "/PID", fmt.Sprint(pid), "/T", "/F")
	kill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := kill.CombinedOutput()
	if err != nil {
		return fmt.Errorf("taskkill: %v: %s", err, string(out))
	}
	return nil
}

// ExitCode reports the exit status of a gateway this tray started, or false if
// it is still running. It is how a crash is noticed without polling the port.
func (p *Process) ExitCode() (int, bool) {
	if p == nil || p.cmd == nil || p.cmd.ProcessState == nil {
		return 0, false
	}
	return p.cmd.ProcessState.ExitCode(), true
}

// Reap waits for a gateway this tray started, in the background, so the process
// table entry does not linger.
func (p *Process) Reap() {
	if p == nil || p.cmd == nil {
		return
	}
	_ = p.cmd.Wait()
}

// Console visibility.

// HasConsole reports whether a process currently owns a console window, which is
// what the tray's "show or hide the terminal" switch acts on.
func HasConsole(pid uint32) bool {
	hwnd := consoleWindowFor(pid)
	return hwnd != 0
}

// ShowConsole makes a process's console window visible and brings it forward.
func ShowConsole(pid uint32) error {
	hwnd := consoleWindowFor(pid)
	if hwnd == 0 {
		return fmt.Errorf("no console window for process %d", pid)
	}
	const swShow = 5
	procShowWindow.Call(hwnd, swShow)
	procSetForegroundWindow.Call(hwnd)
	return nil
}

// HideConsole hides a process's console window without stopping it.
func HideConsole(pid uint32) error {
	hwnd := consoleWindowFor(pid)
	if hwnd == 0 {
		return fmt.Errorf("no console window for process %d", pid)
	}
	const swHide = 0
	procShowWindow.Call(hwnd, swHide)
	return nil
}

// consoleWindowFor finds the console window belonging to a process.
//
// Windows does not expose the console window from the process id, so the window
// list is walked and each console-class window is matched to its owning process
// by re-running the title lookup that produced its caption.
func consoleWindowFor(pid uint32) uintptr {
	return enumerateWindows(func(hwnd uintptr) bool {
		if !isConsoleClass(hwnd) {
			return false
		}
		var owner uint32
		procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&owner)))
		return owner == pid
	})
}

// isConsoleClass reports whether a window has the console window class name.
func isConsoleClass(hwnd uintptr) bool {
	var buf [64]uint16
	procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	name := syscall.UTF16ToString(buf[:])
	return name == "ConsoleWindowClass" || name == "PseudoConsoleWindow"
}

// enumerateWindows walks the top-level window list and returns the first window
// the predicate accepts.
func enumerateWindows(match func(uintptr) bool) uintptr {
	var found uintptr
	cb := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		if match(hwnd) {
			found = hwnd
			return 0 // stop enumeration
		}
		return 1
	})
	procEnumWindows.Call(cb, 0)
	return found
}
