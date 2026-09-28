package status

import (
	"encoding/json"
	"testing"
	"time"
)

// TestAccountUnmarshalTerminates is the check for the bug that took the tray down.
//
// Account implements UnmarshalJSON, and its first version read the response through a
// type that embedded Account. json sees the embedded type's method and calls it again,
// which is a stack overflow rather than an error, so the failure is a crash with no
// message rather than a wrong value. The only way to catch it is to decode something.
func TestAccountUnmarshalTerminates(t *testing.T) {
	const body = `{
		"uid": "u1", "nickname": "laincat", "credits": 8380, "credits_total": 14000,
		"cooling": false, "realm": "cn", "in_flight": 1, "success_count": 1637,
		"token_usage": {"last_used_at": "2026-09-28T09:25:13+08:00", "total_tokens": 653915018}
	}`

	var acct Account
	if err := json.Unmarshal([]byte(body), &acct); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if acct.Nickname != "laincat" || acct.Credits != 8380 || acct.InFlight != 1 {
		t.Errorf("the account read back as %+v", acct)
	}
	if acct.LastUsed.IsZero() {
		t.Error("the nested last-used time did not reach the account")
	}
	if got := acct.LastUsed.Year(); got != 2026 {
		t.Errorf("the last-used time is %v, want the value from the payload", acct.LastUsed)
	}
}

// TestCurrentPicksTheAccountInFlight is the rule the header depends on.
//
// A pool has several accounts and no primary one, so the answer to "which account is
// this" is whichever is serving: in flight first, then most recently used.
func TestCurrentPicksTheAccountInFlight(t *testing.T) {
	older := time.Now().Add(-time.Hour)
	newer := time.Now()

	snap := Snapshot{Accounts: []Account{
		{UID: "a", Nickname: "first", Credits: 100, LastUsed: older},
		{UID: "b", Nickname: "busy", Credits: 200, LastUsed: older, InFlight: 2},
		{UID: "c", Nickname: "recent", Credits: 300, LastUsed: newer},
	}}

	got, ok := snap.Current()
	if !ok {
		t.Fatal("no current account")
	}
	// "busy" is in flight, which beats being merely recent: a request is going
	// through it now.
	if got.Nickname != "busy" {
		t.Errorf("the current account is %q, want the one serving a request", got.Nickname)
	}
}

// TestCurrentFallsBackToMostRecent checks the case with nothing in flight.
func TestCurrentFallsBackToMostRecent(t *testing.T) {
	snap := Snapshot{Accounts: []Account{
		{UID: "a", Nickname: "first", LastUsed: time.Now().Add(-time.Hour)},
		{UID: "b", Nickname: "recent", LastUsed: time.Now()},
	}}
	got, ok := snap.Current()
	if !ok || got.Nickname != "recent" {
		t.Errorf("the current account is %q (ok=%v), want the most recently used", got.Nickname, ok)
	}
}

// TestCurrentWithNoHistoryStillAnswers checks the state right after a fresh install,
// where nothing has served anything yet and an empty header would be worse than a name.
func TestCurrentWithNoHistoryStillAnswers(t *testing.T) {
	snap := Snapshot{Accounts: []Account{{UID: "only", Nickname: "only"}}}
	if got, ok := snap.Current(); !ok || got.Nickname != "only" {
		t.Errorf("the current account is %q (ok=%v), want the only account", got.Nickname, ok)
	}
	if _, ok := (Snapshot{}).Current(); ok {
		t.Error("an empty pool reported a current account")
	}
}
