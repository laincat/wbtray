package ui

import (
	"strings"
	"testing"
	"unicode"

	"wbtray/internal/panel"
	"wbtray/internal/status"
	"wbtray/internal/theme"
)

// The window and the panel are drawn in both languages.
//
// This is a guard rather than a spot check, and it exists because the failure it
// catches already happened: the tray menu was translated while these two surfaces
// were written with Chinese literals, so choosing "EN" produced an English menu
// beside a Chinese window. Nothing about that looked broken in the tests, because
// every string involved was perfectly good Chinese.
//
// The check is over the laid-out text rather than over the source: a literal that
// never reaches a frame is not the bug, and a string assembled at run time from a
// Chinese prefix and an English value is.
func TestNoChineseInAnEnglishFrame(t *testing.T) {
	texts, chinese := englishFrameText(t)
	// A frame that drew nothing would pass the check above while proving nothing,
	// so the count is asserted too. The number is a floor rather than the exact
	// total: rows are added to these pages, and a test that has to be edited every
	// time one is is a test that gets edited without being read.
	if texts < 60 {
		t.Fatalf("the sampled frames drew only %d strings; the check below is vacuous", texts)
	}
	if len(chinese) > 0 {
		t.Errorf("the English window and panel drew %d strings with Chinese in them:\n  %s",
			len(chinese), strings.Join(chinese, "\n  "))
	}
}

// englishFrameText lays out every page and the tray panel in English, and returns
// how many strings were drawn along with the ones that contain Chinese.
func englishFrameText(t *testing.T) (int, []string) {
	t.Helper()
	pal := theme.ForName(theme.DefaultName, false)
	snap := status.Snapshot{
		Reachable: true,
		Version:   "1.11.9-panel",
		Uptime:    7265,
		Total:     2,
		Healthy:   1,
		Accounts: []status.Account{
			{UID: "u1", Nickname: "Laincat", Credits: 7954, Total: 14505,
				Requests: 3511, ErrTotal: 1, LastLatencyMs: 4691, LastTPS: 73.5},
			{UID: "u2", Nickname: "backup", Cooling: true, CoolRemain: 420,
				Disabled: true, Credits: 0, Total: 14505, Requests: 12},
		},
		Usage: status.Usage{
			Requests: 1214, Errors: 1, TotalTokens: 609_000_000,
			AvgLatencyMs: 8219, AvgTPS: 92.6,
			Series: []float64{23, 103, 427, 233, 147, 62},
		},
		Process: status.PIDInfo{Found: true, PID: 4242},
	}

	var out []string
	total := 0
	collect := func(texts []Text) {
		for _, tx := range texts {
			total++
			if hasChinese(tx.S) {
				out = append(out, tx.S)
			}
		}
	}

	// Every page, because a page that is not drawn in one test is a page whose
	// literals nobody checks.
	for _, tab := range Tabs {
		collect(Build(View{
			W: 1180, H: 760, Tab: tab, Snap: snap, Lang: "en", Palette: pal,
			ConfigPath:   "config.json",
			Models:       englishModels(),
			Logs:         []panel.LogEntry{{TS: "2026-09-29T09:17:06Z", Ch: "chat", Text: "#12 u1 ok 812ms"}},
			Schedule:     panel.Schedule{Known: true, Checkin: true, BalanceRefresh: true},
			Config:       englishConfig(),
			ScrollModels: 1, ScrollLogs: 1, ScrollAccounts: 1,
		}).Texts)
	}
	collect(BuildTray(TrayView{
		Snap: snap, Lang: "en", Palette: pal, Auto: true, Installed: true,
	}).Texts)
	return total, dedupe(out)
}

func hasChinese(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

// englishModels and englishConfig are the two pages whose text is drawn from data
// rather than from a label, so they need data that is not Chinese: a model named in
// Chinese would fail this test for a reason that is not the bug it looks for.
func englishModels() []panel.Model {
	return []panel.Model{{ID: "cn:test", Name: "Test Model", Credits: "x1.00",
		ContextLength: 128_000, MaxOutputTokens: 16_384, SupportsImages: true,
		SupportsReasoning: true, SupportsToolCall: true, DefaultEffort: "high"}}
}

func englishConfig() string {
	return `{"listen":":7863","api_key":"sk-x","auth_dir":"./auths",` +
		`"global":{"enabled":true},"pool":{"max_in_flight":3,"max_in_flight_global":2,` +
		`"breaker_threshold":3,"breaker_cooldown":"30m","expiring_soon":"168h"},` +
		`"session_sticky":{"enabled":true,"ttl":"30m"},` +
		`"upstream":{"timeout_seconds":120,"header_timeout_seconds":120}}`
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
