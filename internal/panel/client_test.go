package panel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A gateway that answers 503 has been reached.
//
// The health endpoint returns 503 while the pool cannot serve — every account
// cooling, a check-in in flight, or no account added yet — and the body it sends with
// that status describes a running gateway. Reporting it as unreachable is what put
// "gateway offline" on the screen of an operator whose gateway was up, and this is
// the check that keeps it from coming back.
func TestAServiceUnavailableAnswerIsReachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			// The gateway's own shape: a complete answer under a 503.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"healthy":1,"total":1,"realm_servable":{"cn":false,"global":false}}`))
		case "/panel/api/overview":
			_, _ = w.Write([]byte(`{"version":"1.11.9-panel","uptime_sec":10,"total":1,"healthy":1,"accounts":[]}`))
		case "/panel/api/usage":
			_, _ = w.Write([]byte(`{"totals":{},"series":[]}`))
		case "/panel/api/logs":
			_, _ = w.Write([]byte(`{"entries":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	snap := New(srv.URL, "", 3*time.Second).Fetch(ctx)

	if !snap.Reachable {
		t.Fatalf("a 503 from the health endpoint was reported as unreachable: %v", snap.Err)
	}
	if snap.Err != nil {
		t.Errorf("reachable snapshot carries an error: %v", snap.Err)
	}
	// The body was still decoded: reachability is not bought by throwing the answer
	// away, and the pool's own totals came from it.
	if snap.Total != 1 {
		t.Errorf("the 503 body was not decoded: total=%d, want 1", snap.Total)
	}
}

// A gateway that is not there is still unreachable, so the fix above cannot hide a
// real outage behind a status code.
func TestAConnectionFailureIsStillUnreachable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Port 1 on loopback: nothing listens there, and the refusal is immediate.
	snap := New("http://127.0.0.1:1", "", 2*time.Second).Fetch(ctx)
	if snap.Reachable {
		t.Fatal("a refused connection was reported as reachable")
	}
	if snap.Err == nil {
		t.Fatal("an unreachable snapshot carries no error")
	}
}

// The console page must never carry the API key.
//
// An earlier version appended "?key=" so the console would not ask for it. The
// panel does not read that parameter — its app.js has no query-string parsing, and
// opening the page with it still raises the "需要访问密钥" prompt — so the
// parameter only put the key in browser history, where profile sync copies it
// around and it survives clearing the site's cookies.
func TestPanelURLHasNoKey(t *testing.T) {
	c := New("http://127.0.0.1:7863", "secret-key-value", 5*time.Second)

	got := c.PanelURL()
	if want := "http://127.0.0.1:7863/panel/"; got != want {
		t.Errorf("PanelURL() = %q, want %q", got, want)
	}
	if strings.Contains(got, "secret-key-value") {
		t.Errorf("PanelURL() leaks the key: %q", got)
	}
	if strings.Contains(got, "?") {
		t.Errorf("PanelURL() carries a query string: %q", got)
	}

	// The key is still reachable, because the menu's copy action needs it.
	if c.Key() != "secret-key-value" {
		t.Errorf("Key() = %q, want the configured key", c.Key())
	}
}

// A gateway with no key configured is the same URL, not a differently shaped one.
func TestPanelURLWithoutKey(t *testing.T) {
	c := New("http://127.0.0.1:7863/", "", 5*time.Second)
	if got, want := c.PanelURL(), "http://127.0.0.1:7863/panel/"; got != want {
		t.Errorf("PanelURL() = %q, want %q", got, want)
	}
}
