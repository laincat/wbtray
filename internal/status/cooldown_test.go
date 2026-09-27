package status

import (
	"testing"
	"time"
)

// TestCoolRemainingUsesTheUntilWallClock is the reading that was always zero.
//
// The gateway reports when a cooldown ends rather than how long is left in it, and
// the tray read a duration field the gateway does not send. The result was a cooling
// account shown as "cooling, 0s" — a row that looked like information and was not.
func TestCoolRemainingUsesTheUntilWallClock(t *testing.T) {
	acct := Account{
		UID:     "u1",
		Cooling: true,
		Until:   time.Now().Add(7 * time.Minute),
	}
	got := acct.CoolRemaining()
	if got < 6*time.Minute || got > 7*time.Minute {
		t.Errorf("a cooldown ending in seven minutes reports %v", got)
	}
}

// TestCoolRemainingPrefersAnExplicitDuration checks the precedence, because some
// builds do send a duration and it is the more precise of the two.
func TestCoolRemainingPrefersAnExplicitDuration(t *testing.T) {
	acct := Account{
		Cooling:    true,
		CoolRemain: 300,
		// A wall clock that would say something quite different.
		Until: time.Now().Add(4 * time.Hour),
	}
	if got := acct.CoolRemaining(); got != 300*time.Second {
		t.Errorf("an explicit duration of 300s reports %v", got)
	}
}

// TestCoolRemainingIsZeroWhenThereIsNothingToSay covers the three states that must
// not produce a countdown: an account that is not cooling, one with no end time, and
// one whose end time has passed.
func TestCoolRemainingIsZeroWhenThereIsNothingToSay(t *testing.T) {
	cases := []struct {
		name string
		acct Account
	}{
		{"not cooling", Account{UID: "u", Until: time.Now().Add(time.Hour)}},
		{"no end time", Account{UID: "u", Cooling: true}},
		{"already over", Account{UID: "u", Cooling: true, Until: time.Now().Add(-time.Minute)}},
	}
	for _, tc := range cases {
		if got := tc.acct.CoolRemaining(); got != 0 {
			t.Errorf("%s reports %v, want zero", tc.name, got)
		}
	}
}
