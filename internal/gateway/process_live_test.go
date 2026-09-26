//go:build windows

package gateway

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestFindRunningSeesAProcessItDidNotStart starts a process named the gateway's
// name and checks the process walk finds it.
//
// The walk is by executable name over the whole process table, which is the only
// way to notice a gateway this tray did not launch — the arrangement an operator
// who starts the gateway by hand or from a scheduled task has. Getting the
// structure size wrong would silently return nothing, so a real process is
// started rather than a mock returned.
func TestFindRunningSeesAProcessItDidNotStart(t *testing.T) {
	if got := FindRunning(); got.Found {
		t.Logf("a gateway is already running as pid %d", got.PID)
		return
	}

	// A process with the gateway's name is enough: the walk matches on the file
	// name, and building the real gateway is not what is under test.
	dir := t.TempDir()
	src := dir + string(os.PathSeparator) + DefaultName
	copyOfCmd(t, src)
	cmd := exec.Command(src, "600")
	if err := cmd.Start(); err != nil {
		t.Skipf("could not start a stand-in process: %v", err)
	}
	defer func() { _ = cmd.Process.Kill() }()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got := FindRunning()
		if got.Found && got.PID == uint32(cmd.Process.Pid) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("the process walk did not find pid %d", cmd.Process.Pid)
}

// copyOfCmd puts a copy of a long-running system program under the gateway's
// name, so the walk has something real to find.
func copyOfCmd(t *testing.T, dst string) {
	t.Helper()
	data, err := os.ReadFile(execPath(t))
	if err != nil {
		t.Skipf("could not read a stand-in binary: %v", err)
	}
	if err := os.WriteFile(dst, data, 0o755); err != nil {
		t.Skipf("could not write the stand-in: %v", err)
	}
}

// execPath is a small console program that stays alive when given a number.
func execPath(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("timeout.exe")
	if err != nil {
		t.Skip("no stand-in console program on this system")
	}
	return p
}
