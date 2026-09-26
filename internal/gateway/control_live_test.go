//go:build windows

package gateway

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestStartAndStopControlARealProcess exercises the two directions of process
// control against a stand-in that stays alive the way a gateway does: it is
// started hidden, found by the process walk, and ended by Stop.
//
// It deliberately does not use the real gateway. This test ends what it starts,
// and pointing it at an operator's running service would make the test suite a
// way to take that service down.
func TestStartAndStopControlARealProcess(t *testing.T) {
	// A gateway already running on this machine is left alone: this test acts
	// only on the process it starts itself, and both the wait loops below name
	// that pid rather than asking whether any gateway is up.
	exe := standInGateway(t)

	// The stand-in decides whether to sleep from the environment, so the flag is
	// set here and inherited by the child. Without it the copy of the test binary
	// runs the helper, sees no flag, skips, and exits before the walk can see it.
	t.Setenv("WBTRAY_HELPER", "1")
	proc, err := Start(exe, "", false, "-test.run=TestHelperIsAGateway")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Whatever happens, the process this test started does not outlive it.
	defer func() {
		if proc.PID != 0 {
			_ = Stop(proc.PID)
		}
	}()

	if proc.PID == 0 || !proc.Found || !proc.Started {
		t.Fatalf("Start reported %+v", proc)
	}

	found := false
	for i := 0; i < 60 && !found; i++ {
		time.Sleep(50 * time.Millisecond)
		found = Alive(proc.PID)
	}
	if !found {
		t.Fatalf("the process walk did not find the process it started (pid %d)", proc.PID)
	}

	pid := proc.PID
	if err := Stop(pid); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	proc.PID = 0 // the deferred stop has nothing left to do

	// taskkill is asynchronous, so the exit is waited for rather than assumed.
	gone := false
	for i := 0; i < 80 && !gone; i++ {
		time.Sleep(50 * time.Millisecond)
		gone = !Alive(pid)
	}
	if !gone {
		t.Fatalf("process %d survived Stop", pid)
	}
}

// TestHelperIsAGateway is not a test: it is the body of the stand-in process,
// which sleeps until it is killed. Naming it Test* lets the test binary run it as
// a child, so no fixture binary has to be built or kept in the repository.
func TestHelperIsAGateway(t *testing.T) {
	if os.Getenv("WBTRAY_HELPER") == "" {
		t.Skip("helper process only")
	}
	time.Sleep(5 * time.Minute)
}

// TestStartRefusesAnEmptyExecutable is the guard that turns a missing gateway
// into a message rather than a process started in the wrong directory.
func TestStartRefusesAnEmptyExecutable(t *testing.T) {
	if _, err := Start("", "", false); err == nil {
		t.Fatal("Start accepted an empty executable")
	}
}

// TestStartPassesArgumentsToTheChild checks that the arguments reach the child,
// which is what a deployment starting the gateway with its own flags uses.
func TestStartPassesArgumentsToTheChild(t *testing.T) {
	exe, err := exec.LookPath("cmd.exe")
	if err != nil {
		t.Skip("no cmd.exe on this system")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran.txt")
	proc, err := Start(exe, "", false, "/c", "echo hi > "+marker)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		if proc.PID != 0 {
			_ = Stop(proc.PID)
		}
	}()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the argument was not passed to the child process")
}

// standInGateway copies the test binary under the gateway's name, so the process
// walk — which matches on the file name — sees something it recognises.
func standInGateway(t *testing.T) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Skipf("cannot locate the test binary: %v", err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Skipf("cannot read the test binary: %v", err)
	}
	dir := t.TempDir()
	dst := filepath.Join(dir, DefaultName)
	if err := os.WriteFile(dst, data, 0o755); err != nil {
		t.Skipf("cannot write the stand-in: %v", err)
	}
	return dst
}
