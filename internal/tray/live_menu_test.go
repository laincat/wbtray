//go:build windows

package tray

import (
	"strings"
	"testing"

	"wbtray/internal/app"
	"wbtray/internal/config"
	"wbtray/internal/i18n"
	"wbtray/internal/status"
	"wbtray/internal/traymenu"
)

// stubGateway is the process controller the application talks to, with no process
// behind it.
type stubGateway struct {
	pid       status.PIDInfo
	autoStart bool
	console   bool
}

func (s *stubGateway) PID() status.PIDInfo { return s.pid }
func (s *stubGateway) AdoptProcess(pid uint32, exe string) {
	s.pid = status.PIDInfo{Found: true, PID: pid, Exe: exe}
}
func (s *stubGateway) Start() (uint32, error) {
	s.pid = status.PIDInfo{Found: true, PID: 4242}
	return 4242, nil
}
func (s *stubGateway) Stop() error                { s.pid = status.PIDInfo{}; return nil }
func (s *stubGateway) Restart() (uint32, error)   { return s.Start() }
func (s *stubGateway) HasConsole() bool           { return s.console }
func (s *stubGateway) SetConsole(show bool) error { s.console = show; return nil }
func (s *stubGateway) AutoStartEnabled() bool     { return s.autoStart }
func (s *stubGateway) SetAutoStart(on bool) error { s.autoStart = on; return nil }

// TestTopLevelMenuIsNineRows is the shape of the whole rewrite.
//
// The menu used to put every figure, every style and every maintenance action at the
// top level: twenty-six rows, taller than a laptop screen at a large text size, with
// the rows an operator reaches for somewhere in the middle of it. The count is
// asserted rather than described because the way a menu grows back is one reasonable
// addition at a time.
func TestTopLevelMenuIsNineRows(t *testing.T) {
	cfg := config.Default()
	cfg.Lang = "zh"
	a := app.New(cfg, "", &stubGateway{pid: status.PIDInfo{Found: true, PID: 16780}}, app.Options{})

	items := a.Menu()
	rows, separators := 0, 0
	for _, it := range items {
		if it.Kind == traymenu.SeparatorRow {
			separators++
			continue
		}
		rows++
	}
	if rows != 9 {
		t.Errorf("the top level has %d rows, want 9:\n%s", rows, describe(items))
	}
	if separators != 2 {
		t.Errorf("the top level has %d separators, want 2", separators)
	}
}

// TestEveryLabelIsShort is the naming rule the rewrite was asked for.
//
// Two characters for a Chinese label and one word for an English one, because a long
// label beside a short one makes the short one look like a different kind of thing.
// It is checked rather than trusted: the natural way to write a new row is to
// describe what it does, and the description is always too long.
func TestEveryLabelIsShort(t *testing.T) {
	cfg := config.Default()
	cfg.Lang = "zh"
	a := app.New(cfg, "", &stubGateway{pid: status.PIDInfo{Found: true, PID: 16780}}, app.Options{})

	for _, it := range rowsOf(a.Menu()) {
		if it.ID == 0 {
			// A row with no command id is a readout, and a readout says whatever
			// it has to say: "无法连接 127.0.0.1:7863" is the whole message, and
			// shortening it to four characters would leave it saying nothing.
			continue
		}
		if n := len([]rune(it.Text)); n > 4 {
			t.Errorf("the label %q is %d characters; menu labels are 2 to 4", it.Text, n)
		}
	}
}

// rowsOf walks every row at every depth, which is the only way a rule like the one
// above applies to the whole menu rather than to what is visible at once.
func rowsOf(items []traymenu.Item) []traymenu.Item {
	var out []traymenu.Item
	for _, it := range items {
		if it.Kind == traymenu.SeparatorRow {
			continue
		}
		out = append(out, it)
		out = append(out, rowsOf(it.Children)...)
	}
	return out
}

// describe renders a menu as text, so a failure prints what was actually built.
func describe(items []traymenu.Item) string {
	out := ""
	for _, it := range items {
		if it.Kind == traymenu.SeparatorRow {
			out += "---\n"
			continue
		}
		out += it.Text
		if it.Kind == traymenu.SubmenuRow {
			out += " >"
		}
		if it.Value != "" {
			out += "  [" + it.Value + "]"
		}
		out += "\n"
	}
	return out
}

// TestCopySubmenuOffersBothValues checks the rows the submenu exists for.
//
// The address is what an operator pastes into a browser and the key is what a client
// needs. Both were previously unreachable: the first because its row was inert, and
// the second because no row existed at all.
func TestCopySubmenuOffersBothValues(t *testing.T) {
	cfg := config.Default()
	cfg.Lang = "zh"
	cfg.BaseURL = "http://127.0.0.1:7863"
	cfg.APIKey = "sk-test"

	a := app.New(cfg, "", &stubGateway{}, app.Options{})
	item, ok := submenuWithChild(a.Menu(), app.IDCopyURL)
	if !ok {
		t.Fatalf("no copy submenu:\n%s", describe(a.Menu()))
	}
	if len(item.Children) != 2 {
		t.Fatalf("the copy submenu has %d rows, want 2", len(item.Children))
	}

	addr := item.Children[0]
	if addr.ID != app.IDCopyURL {
		t.Errorf("the first row is id %d, want the address row", addr.ID)
	}
	// The value shown is the host, which is the part that fits a menu column; what
	// is copied is the full address with its scheme.
	if addr.Value != "127.0.0.1:7863" {
		t.Errorf("the address row shows %q, want the host", addr.Value)
	}
	if addr.Disabled {
		t.Error("the address row is inert")
	}

	key := item.Children[1]
	if key.ID != app.IDCopyKey {
		t.Errorf("the second row is id %d, want the key row", key.ID)
	}
	// The key itself must not be in the menu: an open menu is a screenshot.
	if key.Value != "" {
		t.Errorf("the key row carries the key itself: %q", key.Value)
	}
}

// TestCopyKeyRowIsInertWithoutAKey checks the empty case, which is the state a first
// run is in before discovery has read the gateway's configuration.
func TestCopyKeyRowIsInertWithoutAKey(t *testing.T) {
	cfg := config.Default()
	cfg.Lang = "en"
	cfg.APIKey = ""
	cfg.DiscoveryEnabled = false

	a := app.New(cfg, "", &stubGateway{}, app.Options{})
	item, ok := submenuWithChild(a.Menu(), app.IDCopyKey)
	if !ok {
		t.Fatalf("no copy submenu:\n%s", describe(a.Menu()))
	}
	key := item.Children[1]
	if !key.Disabled {
		t.Error("the key row is clickable with no key to copy")
	}
	if key.Value != i18n.T("en", "copy.none") {
		t.Errorf("the key row says %q, want the empty-state wording", key.Value)
	}
}

// TestGatewayRowCarriesThePID checks the value column on a submenu row, which is
// where the pid is shown now that it is not a row of its own.
//
// A reading has to happen first: the row is built from the snapshot, and the snapshot
// is what carries the process the tray is watching.
func TestGatewayRowCarriesThePID(t *testing.T) {
	server := newFakePanel(t)
	defer server.Close()

	cfg := config.Default()
	cfg.Lang = "en"
	cfg.BaseURL = server.URL
	cfg.APIKey = "test-key"

	a := app.New(cfg, "", &stubGateway{pid: status.PIDInfo{Found: true, PID: 16780}}, app.Options{})
	a.Refresh()

	var found bool
	for _, it := range a.Menu() {
		// The label carries a state suffix while the gateway is down, so the test
		// matches on the prefix rather than on the whole string.
		if it.Kind == traymenu.SubmenuRow && strings.HasPrefix(it.Text, i18n.T("en", "menu.gateway")) {
			found = true
			if it.Value != "PID 16780" {
				t.Errorf("the gateway row shows %q, want the pid", it.Value)
			}
		}
	}
	if !found {
		t.Errorf("no gateway row:\n%s", describe(a.Menu()))
	}
}

// TestStatusRowsAreOwnerDrawn checks the rows whose colour is information.
//
// It is the reason those two rows are painted by hand at all: the shell dims a row it
// cannot click, and it dims that row's icon with it.
func TestStatusRowsAreOwnerDrawn(t *testing.T) {
	cfg := config.Default()
	cfg.Lang = "en"
	a := app.New(cfg, "", &stubGateway{}, app.Options{})

	count := 0
	for _, it := range a.Menu() {
		if it.OwnerDraw {
			count++
			if it.Dot.A == 0 {
				t.Errorf("the owner-drawn row %q carries no colour", it.Text)
			}
		}
	}
	if count != 2 {
		t.Errorf("%d rows are drawn by hand, want the two status rows", count)
	}
}

// submenuWithChild finds a submenu whose children include a given command id.
func submenuWithChild(items []traymenu.Item, id uint32) (traymenu.Item, bool) {
	for _, it := range items {
		if it.Kind != traymenu.SubmenuRow {
			continue
		}
		for _, child := range it.Children {
			if child.ID == id {
				return it, true
			}
		}
	}
	return traymenu.Item{}, false
}
