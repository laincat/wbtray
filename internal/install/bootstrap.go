package install

import (
	"context"
	"fmt"
	"time"
)

// Getting a gateway in place at startup.
//
// The order is fixed and each step only happens if the one before it did not:
//
//  1. A gateway already running anywhere on the machine is used as it is. It may
//     have been started by hand, by a scheduled task, or by an earlier wbtray,
//     and in every one of those cases the operator's intent is clear — there is
//     a gateway, and starting a second one would fight it for the port.
//  2. Otherwise, a gateway installed in wbtray's own directory is started.
//  3. Otherwise the newest release is downloaded, installed into that directory,
//     and started.
//
// The distinction that matters is between (1) and (2): a gateway found running
// is one wbtray did not install, and using it means never overwriting it.

// Running is what a probe for an existing gateway reports.
type Running struct {
	Found bool
	PID   uint32
	Exe   string
}

// Bootstrap resolves the gateway, installing it if it is nowhere to be found.
//
// probe reports whether a gateway is already running; startOrInstall is handed
// the reason the other two steps did not apply. The two are separate functions
// rather than one so the decision — which is the part worth testing — does not
// depend on a process table or the network.
func Bootstrap(ctx context.Context, probe func() Running, local func() (Layout, bool), fetch func(context.Context, Layout) error) (Step, error) {
	if r := probe(); r.Found {
		return Step{Kind: UseRunning, PID: r.PID, Exe: r.Exe}, nil
	}
	layout, installed := local()
	if installed {
		return Step{Kind: UseInstalled, Layout: layout}, nil
	}
	if fetch == nil {
		return Step{}, fmt.Errorf("no gateway is running and none is installed")
	}
	if err := fetch(ctx, layout); err != nil {
		return Step{}, err
	}
	return Step{Kind: Installed, Layout: layout}, nil
}

// Kind is which step of the bootstrap applied.
type Kind int

// The steps, in the order they are tried.
const (
	// UseRunning means a gateway was already running and is used as it is.
	UseRunning Kind = iota
	// UseInstalled means one was found in wbtray's own directory.
	UseInstalled
	// Installed means one was downloaded and put in place.
	Installed
)

// Step is the outcome of the bootstrap.
type Step struct {
	Kind   Kind
	PID    uint32
	Exe    string
	Layout Layout
}

// String describes the step, for a log line.
func (s Step) String() string {
	switch s.Kind {
	case UseRunning:
		return fmt.Sprintf("using the gateway already running as pid %d", s.PID)
	case UseInstalled:
		return "using the gateway installed in " + s.Layout.Gateway
	default:
		return "installed and started the gateway in " + s.Layout.Gateway
	}
}

// WaitForGateway waits for the gateway's API to answer after it has been started.
//
// A gateway that has been launched is not a gateway that is ready: it reads its
// configuration, loads its account files and binds its port over the next few
// seconds. Reporting on it before that produces a tray that says "offline" for a
// moment every time it starts, which looks like a fault rather than like
// startup.
func WaitForGateway(ctx context.Context, ready func(context.Context) bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if ready(ctx) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(400 * time.Millisecond):
		}
	}
}
