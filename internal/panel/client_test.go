package panel

import (
	"strings"
	"testing"
	"time"
)

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
