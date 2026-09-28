//go:build windows

package tray

import (
	"strings"
	"testing"

	"wbtray/internal/app"
	"wbtray/internal/config"
	"wbtray/internal/i18n"
	"wbtray/internal/status"
	"wbtray/internal/theme"
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

// pip is the colour the measurement tests give a row. Its value does not matter to a
// width; only whether a row has one.
var pip = theme.System(theme.Accent{}).OK

// TestTopLevelMenuIsEightRows is the shape of the whole menu.
//
// The top level is the state, five things to do, and the row that ends it — eight\r\n// rows in four blocks. The count is asserted rather than described because the way a
// menu grows back is one reasonable addition at a time.
func TestTopLevelMenuIsEightRows(t *testing.T) {
	server := newFakePanel(t)
	defer server.Close()

	cfg := config.Default()
	cfg.Lang = "zh"
	cfg.BaseURL = server.URL
	cfg.APIKey = "test-key"
	a := app.New(cfg, "", &stubGateway{pid: status.PIDInfo{Found: true, PID: 16780}}, app.Options{})
	a.Refresh()

	rows, separators := 0, 0
	for _, it := range a.Menu() {
		if it.Kind == traymenu.SeparatorRow {
			separators++
			continue
		}
		rows++
	}
	if rows != 8 {
		t.Errorf("the top level has %d rows, want 8:\n%s", rows, describe(a.Menu()))
	}
	if separators != 3 {
		t.Errorf("the top level has %d separators, want 3", separators)
	}
}

// TestEveryRowExplainsItself is the rule the menu was rebuilt around.
//
// A menu row can say what it is called and cannot say what it does, so every row that
// does something carries a sentence for the tip. It is checked rather than trusted:
// the natural way to add a row is to give it a label and stop.
func TestEveryRowExplainsItself(t *testing.T) {
	server := newFakePanel(t)
	defer server.Close()

	cfg := config.Default()
	cfg.Lang = "zh"
	cfg.BaseURL = server.URL
	cfg.APIKey = "test-key"
	a := app.New(cfg, "", &stubGateway{pid: status.PIDInfo{Found: true, PID: 16780}}, app.Options{})
	a.Refresh()

	for _, it := range rowsOf(a.Menu()) {
		switch it.Kind {
		case traymenu.SeparatorRow:
			continue
		case traymenu.ValueRow:
			// A readout is a figure, not an action; where it needs explaining the
			// explanation is on the row that opens its block.
			continue
		}
		if it.Hint == "" {
			t.Errorf("the row %q does nothing to explain itself", it.Text)
		}
	}
}

// TestEveryLabelIsShort is the naming rule.
func TestEveryLabelIsShort(t *testing.T) {
	server := newFakePanel(t)
	defer server.Close()

	cfg := config.Default()
	cfg.Lang = "zh"
	cfg.BaseURL = server.URL
	cfg.APIKey = "test-key"
	a := app.New(cfg, "", &stubGateway{pid: status.PIDInfo{Found: true, PID: 16780}}, app.Options{})
	a.Refresh()

	for _, it := range rowsOf(a.Menu()) {
		if it.ID == 0 {
			// A row with no command id is a readout, and a readout says whatever it
			// has to say.
			continue
		}
		if n := len([]rune(it.Text)); n > 4 {
			t.Errorf("the label %q is %d characters; menu labels are 2 to 4", it.Text, n)
		}
	}
}

// rowsOf walks every row at every depth.
func rowsOf(items []traymenu.Item) []traymenu.Item {
	var out []traymenu.Item
	for _, it := range items {
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
			out += "---" + "\n"
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

// TestStateRowNamesTheCurrentAccount checks the one readout the menu opens with.
//
// It used to be two rows of labels: "wbtray · healthy" and "accounts". What answers
// anything is the health colour, the account the gateway is serving with, and its
// balance: a pool has several accounts, and which one is answering is the question the
// header is asked.
func TestStateRowNamesTheCurrentAccount(t *testing.T) {
	server := newFakePanel(t)
	defer server.Close()

	cfg := config.Default()
	cfg.Lang = "en"
	cfg.BaseURL = server.URL
	cfg.APIKey = "test-key"

	a := app.New(cfg, "", &stubGateway{pid: status.PIDInfo{Found: true, PID: 1}}, app.Options{})
	a.Refresh()

	head := a.Menu()[0]
	if head.Dot.A == 0 {
		t.Error("the state row carries no colour")
	}
	if !head.OwnerDraw {
		t.Error("the state row is not drawn by hand, so its colour would be dimmed")
	}
	// The fixture's first account is the one with nothing wrong with it, and nothing
	// has served anything yet, so it is the current one.
	if head.Value != "12,000" {
		t.Errorf("the state row shows %q, want the current account's balance", head.Value)
	}
	if head.Text != "laincat" {
		t.Errorf("the state row says %q, want the current account's name", head.Text)
	}
	if head.Hint == "" {
		t.Error("the state row explains nothing on hover")
	}
	if strings.Contains(head.Text, "wbtray") {
		t.Errorf("the state row repeats the program name: %q", head.Text)
	}
}

// TestGatewaySubmenuCarriesEverythingAboutTheProcess checks the block the request
// grouped: what is running, the copy rows, and where it lives.
func TestGatewaySubmenuCarriesEverythingAboutTheProcess(t *testing.T) {
	server := newFakePanel(t)
	defer server.Close()

	cfg := config.Default()
	cfg.Lang = "en"
	cfg.BaseURL = server.URL
	cfg.APIKey = "test-key"

	a := app.New(cfg, "", &stubGateway{pid: status.PIDInfo{Found: true, PID: 16780}}, app.Options{})
	a.Refresh()

	var gw traymenu.Item
	for _, it := range a.Menu() {
		if it.Kind == traymenu.SubmenuRow && strings.HasPrefix(it.Text, i18n.T("en", "menu.gateway")) {
			gw = it
		}
	}
	if gw.ID != 0 && gw.Text == "" {
		t.Fatalf("no gateway row:\n%s", describe(a.Menu()))
	}
	if gw.Value != "PID 16780" {
		t.Errorf("the gateway row shows %q, want the pid", gw.Value)
	}

	ids := map[uint32]bool{}
	for _, child := range gw.Children {
		ids[child.ID] = true
	}
	for _, want := range []uint32{
		app.IDGatewayStop, app.IDGatewayRestart, app.IDConsoleToggle,
		app.IDCopyURL, app.IDCopyKey, app.IDGatewayAutoStart, app.IDOpenGatewayDir,
	} {
		if !ids[want] {
			t.Errorf("the gateway block is missing the row with id %d:\n%s", want, describe(gw.Children))
		}
	}
}

// TestTaskRowsShowTheGatewaySchedule checks that the ticks come from the gateway
// rather than from a list this program keeps.
func TestTaskRowsShowTheGatewaySchedule(t *testing.T) {
	server := newFakePanel(t)
	defer server.Close()

	cfg := config.Default()
	cfg.Lang = "en"
	cfg.BaseURL = server.URL
	cfg.APIKey = "test-key"

	a := app.New(cfg, "", &stubGateway{}, app.Options{})
	a.Refresh()

	var tasks traymenu.Item
	for _, it := range a.Menu() {
		if it.Kind == traymenu.SubmenuRow && it.Text == i18n.T("en", "menu.tasks") {
			tasks = it
		}
	}
	if tasks.Text == "" {
		t.Fatalf("no tasks row:\n%s", describe(a.Menu()))
	}

	var ticks, actions int
	for _, child := range tasks.Children {
		switch child.Kind {
		case traymenu.CheckRow:
			ticks++
		case traymenu.CommandRow:
			actions++
		}
	}
	// The fixture enables most of the schedule, and the maintenance actions sit
	// under the same block.
	if ticks < 3 {
		t.Errorf("the task block shows %d switches, want the gateway's schedule", ticks)
	}
	if actions != len(app.Tasks) {
		t.Errorf("the task block offers %d actions, want %d", actions, len(app.Tasks))
	}
}

// TestStatusRowsAreOwnerDrawn checks the row whose colour is information.
func TestStatusRowsAreOwnerDrawn(t *testing.T) {
	cfg := config.Default()
	cfg.Lang = "en"
	a := app.New(cfg, "", &stubGateway{}, app.Options{})

	count := 0
	for _, it := range a.Menu() {
		if it.OwnerDraw {
			count++
		}
	}
	if count != 1 {
		t.Errorf("%d rows are drawn by hand, want the one state row", count)
	}
}

// hinted returns every row that carries a sentence, for the tests that check them.
func hinted(items []traymenu.Item) []traymenu.Item {
	var out []traymenu.Item
	for _, it := range rowsOf(items) {
		if it.Hint != "" {
			out = append(out, it)
		}
	}
	return out
}
