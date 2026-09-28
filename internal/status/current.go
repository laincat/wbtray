package status

import (
	"encoding/json"
	"time"
)

// The current account.
//
// A pool has no notion of a primary account: the gateway picks one per request, so the
// honest answer to "which account am I on" is whichever one last served something. The
// gateway nests that time inside the account token usage rather than sending it beside
// the other fields, which is why reading it takes a type of its own.

// accountWire is the account as the gateway sends it.
//
// It repeats every field rather than embedding the Account it produces, and that is
// not redundancy: Account implements UnmarshalJSON, so an embedded Account here would
// have this function call itself until the stack ran out. The fields are listed so the
// recursion has somewhere to stop.
type accountWire struct {
	UID        string    `json:"uid"`
	Nickname   string    `json:"nickname"`
	Credits    int64     `json:"credits"`
	Total      int64     `json:"credits_total"`
	Cooling    bool      `json:"cooling"`
	CoolKind   string    `json:"cool_kind"`
	CoolRemain int64     `json:"cool_remaining_sec"`
	Until      time.Time `json:"until"`
	Disabled   bool      `json:"disabled"`
	Realm      string    `json:"realm"`
	InFlight   int       `json:"in_flight"`
	Success    int64     `json:"success_count"`
	ErrTotal   int64     `json:"err_total"`
	Reason     string    `json:"reason"`
	DisabledBy string    `json:"disabled_reason"`
	Breaker    int       `json:"breaker_fails"`

	TokenUsage struct {
		LastUsedAt time.Time `json:"last_used_at"`
	} `json:"token_usage"`
}

// UnmarshalJSON reads an account and lifts the nested last-used time up to where the
// rest of the code can see it.
func (a *Account) UnmarshalJSON(data []byte) error {
	var wire accountWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*a = Account{
		UID:        wire.UID,
		Nickname:   wire.Nickname,
		Credits:    wire.Credits,
		Total:      wire.Total,
		Cooling:    wire.Cooling,
		CoolKind:   wire.CoolKind,
		CoolRemain: wire.CoolRemain,
		Until:      wire.Until,
		Disabled:   wire.Disabled,
		Realm:      wire.Realm,
		InFlight:   wire.InFlight,
		Success:    wire.Success,
		ErrTotal:   wire.ErrTotal,
		Reason:     wire.Reason,
		DisabledBy: wire.DisabledBy,
		Breaker:    wire.Breaker,
		LastUsed:   wire.TokenUsage.LastUsedAt,
	}
	return nil
}

// Current is the account the gateway is serving with right now.
//
// An account with a request in flight wins over one that merely finished, because that
// is the one a request is going through at this moment.
func (s Snapshot) Current() (Account, bool) {
	var best *Account
	var bestAt time.Time
	for i, a := range s.Accounts {
		if a.InFlight > 0 {
			return a, true
		}
		if best == nil || a.LastUsed.After(bestAt) {
			best, bestAt = &s.Accounts[i], a.LastUsed
		}
	}
	if best != nil && !bestAt.IsZero() {
		return *best, true
	}
	// Nothing has served anything yet, so the first account is as good an answer as
	// any, and better than an empty row.
	if len(s.Accounts) > 0 {
		return s.Accounts[0], true
	}
	return Account{}, false
}
