// Package panel is the client for the gateway's own HTTP API.
//
// The tray holds no state of its own about accounts or usage: the gateway is
// the single source of truth, and the panel API already exposes everything a
// tray needs. That is why this is a client rather than a second implementation
// of the same accounting.
package panel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"wbtray/internal/status"
)

// ErrUnauthorized is returned when the gateway rejects the API key. It is kept
// separate from other failures because the tray can say something useful about
// it, where "unreachable" would be misleading.
var ErrUnauthorized = errors.New("panel: invalid api key")

// Client reads the gateway's status endpoints.
type Client struct {
	base  string
	key   string
	http  *http.Client
	mu    sync.Mutex
	hours int
}

// New builds a client for a gateway root such as http://127.0.0.1:7863.
func New(base, key string, timeout time.Duration) *Client {
	return &Client{
		base:  strings.TrimRight(base, "/"),
		key:   key,
		http:  &http.Client{Timeout: timeout},
		hours: 24,
	}
}

// Window is the usage window currently being read.
//
// It was settable while the chart window existed, because that window let the range be
// changed. With the chart gone the range is fixed, so this reads the value the client
// was built with.
func (c *Client) Window() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hours
}

// Ping asks the gateway's health endpoint whether it is answering.
//
// The health endpoint needs no key and answers 200 or 503 depending on whether
// the pool can serve, so a response of either kind means the gateway is up. That
// distinction matters here: a gateway with no accounts is running and ready, and
// treating its 503 as "not started yet" would make the tray wait out its whole
// startup window for a gateway that was up the entire time.
func (c *Client) Ping(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/healthz", nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "wbtray")
	resp, err := c.http.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	return resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusServiceUnavailable
}

// Base is the gateway root this client talks to.
func (c *Client) Base() string { return c.base }

// Key is the API key in use.
func (c *Client) Key() string { return c.key }

// PanelURL is the console page with the key already filled in, so the browser
// does not have to ask for it again.
func (c *Client) PanelURL() string {
	if c.key == "" {
		return c.base + "/panel/"
	}
	return c.base + "/panel/?key=" + c.key
}

// Fetch reads every endpoint the tray shows and merges them into one snapshot.
//
// The calls run concurrently and none of them is fatal on its own: a tray that
// blanked out because the log endpoint was slow would be worse than one that
// showed slightly stale numbers.
func (c *Client) Fetch(ctx context.Context) status.Snapshot {
	snap := status.Snapshot{At: time.Now()}

	type result struct {
		name string
		err  error
	}
	results := make(chan result, 4)

	go func() { results <- result{"health", c.get(ctx, "/healthz", &healthBody{into: &snap})} }()
	go func() { results <- result{"overview", c.get(ctx, "/panel/api/overview", &overviewBody{into: &snap})} }()
	go func() {
		results <- result{"usage", c.get(ctx, fmt.Sprintf("/panel/api/usage?hours=%d", c.Window()), &usageBody{into: &snap, hours: c.Window()})}
	}()
	go func() { results <- result{"logs", c.get(ctx, "/panel/api/logs", &logsBody{into: &snap})} }()

	var firstErr error
	unauthorized := false
	for i := 0; i < 4; i++ {
		r := <-results
		if r.err == nil {
			continue
		}
		if errors.Is(r.err, ErrUnauthorized) {
			unauthorized = true
		}
		// The health probe decides reachability, so its error is the one worth
		// keeping; another endpoint's failure is reported only if nothing else
		// went wrong, which keeps the tooltip from blaming a side endpoint.
		if firstErr == nil || r.name == "health" {
			firstErr = r.err
		}
	}

	switch {
	case unauthorized:
		snap.Err = ErrUnauthorized
	case firstErr != nil:
		snap.Err = firstErr
	default:
		snap.Reachable = true
	}
	return snap
}

// get performs one request and hands the body to a decoder.
func (c *Client) get(ctx context.Context, path string, into decoder) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrUnauthorized
	}
	if resp.StatusCode >= 400 {
		// The body carries the gateway's own error text, which is more useful
		// than the status code alone.
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s: %s (%s)", path, strings.TrimSpace(string(msg)), resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	return into.decode(data)
}

// decoder fills part of a snapshot from one endpoint's JSON.
type decoder interface{ decode([]byte) error }

type healthBody struct{ into *status.Snapshot }

func (b *healthBody) decode(data []byte) error {
	var v struct {
		Healthy       int             `json:"healthy"`
		Total         int             `json:"total"`
		RealmServable map[string]bool `json:"realm_servable"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	b.into.Healthy = v.Healthy
	b.into.Total = v.Total
	b.into.RealmServable = v.RealmServable
	return nil
}

type overviewBody struct{ into *status.Snapshot }

func (b *overviewBody) decode(data []byte) error {
	var v struct {
		Version  string           `json:"version"`
		Uptime   int64            `json:"uptime_sec"`
		Sticky   int              `json:"sticky_sessions"`
		Redis    string           `json:"redis_mode"`
		Total    int              `json:"total"`
		Healthy  int              `json:"healthy"`
		Accounts []status.Account `json:"accounts"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	b.into.Version = v.Version
	b.into.Uptime = v.Uptime
	b.into.Sticky = v.Sticky
	b.into.Redis = v.Redis
	b.into.Accounts = v.Accounts
	// The overview repeats the totals, and it is the more authoritative of the
	// two when they disagree: /healthz is a liveness probe with its own idea of
	// "healthy", while this is the pool's own count.
	if v.Total > 0 {
		b.into.Total = v.Total
	}
	if v.Healthy > 0 {
		b.into.Healthy = v.Healthy
	}
	return nil
}

type usageBody struct {
	into  *status.Snapshot
	hours int
}

func (b *usageBody) decode(data []byte) error {
	var v struct {
		Totals struct {
			Requests      int64   `json:"requests"`
			Errors        int64   `json:"errors"`
			PromptTokens  int64   `json:"prompt_tokens"`
			CompletionTok int64   `json:"completion_tokens"`
			TotalTokens   int64   `json:"total_tokens"`
			AvgLatencyMs  float64 `json:"avg_latency_ms"`
			AvgTPS        float64 `json:"avg_tokens_per_second"`
		} `json:"totals"`
		Series []struct {
			T   string  `json:"t"`
			Req int64   `json:"requests"`
			Tok int64   `json:"total_tokens"`
			Lat float64 `json:"avg_latency_ms"`
		} `json:"series"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	u := status.Usage{
		Requests:      v.Totals.Requests,
		Errors:        v.Totals.Errors,
		PromptTokens:  v.Totals.PromptTokens,
		CompletionTok: v.Totals.CompletionTok,
		TotalTokens:   v.Totals.TotalTokens,
		AvgLatencyMs:  v.Totals.AvgLatencyMs,
		AvgTPS:        v.Totals.AvgTPS,
		Latency:       make([]float64, 0, len(v.Series)),
		Tokens:        make([]float64, 0, len(v.Series)),
	}
	for _, p := range v.Series {
		u.Series = append(u.Series, float64(p.Req))
		u.Latency = append(u.Latency, p.Lat)
		u.Tokens = append(u.Tokens, float64(p.Tok))
	}
	b.into.Usage = u
	b.into.UsageHours = b.hours
	return nil
}

type logsBody struct{ into *status.Snapshot }

func (b *logsBody) decode(data []byte) error {
	var v struct {
		Entries []struct {
			TS   string `json:"ts"`
			Ch   string `json:"ch"`
			Text string `json:"text"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	b.into.LogTotal = len(v.Entries)
	for _, e := range v.Entries {
		if e.Ch == "task" {
			b.into.LogTasks++
		}
		switch classify(e.Text) {
		case levelError:
			b.into.LogErrors++
		case levelWarn:
			b.into.LogWarns++
		}
	}
	// Entries arrive oldest first, so the newest line is the last non-empty one.
	for i := len(v.Entries) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(v.Entries[i].Text); t != "" {
			b.into.LastLine = t
			break
		}
	}
	return nil
}

type level int

const (
	levelInfo level = iota
	levelWarn
	levelError
)

// classify mirrors the panel's own colouring of log lines, so the tray's counts
// and the panel's highlighting agree.
func classify(text string) level {
	lower := strings.ToLower(text)
	for _, w := range []string{"error", "fail", "失败", "错误", "panic", "fatal"} {
		if strings.Contains(lower, w) {
			return levelError
		}
	}
	for _, w := range []string{"warn", "冷却", "熔断", "超时", "timeout", "retry"} {
		if strings.Contains(lower, w) {
			return levelWarn
		}
	}
	return levelInfo
}

// Schedule is which of the gateway's scheduled tasks are switched on.
//
// The zero value means "not read", which the menu has to tell from "all off": an
// unticked row says a task is disabled, and a task that is on but unread is a
// different thing.
type Schedule struct {
	Known          bool
	Checkin        bool
	Travel         bool
	Activity       bool
	Keepalive      bool
	BalanceRefresh bool
}

// scheduleBody is the part of the gateway's configuration this reads. The field tags
// mirror the gateway's own names, which is why they are spelled out rather than
// derived.
type scheduleBody struct {
	Config struct {
		Schedule struct {
			Checkin        bool `json:"checkin_enabled"`
			Travel         bool `json:"travel_enabled"`
			Activity       bool `json:"activity_enabled"`
			Keepalive      bool `json:"keepalive_enabled"`
			BalanceRefresh bool `json:"balance_refresh_enabled"`
		} `json:"schedule"`
	} `json:"config"`
}

// FetchSchedule asks the gateway which scheduled tasks it is running.
//
// It reads the gateway's own configuration through its panel API rather than the file
// on disk, so the answer is what the running gateway is using rather than what was
// written there before it started. A failure leaves Known false.
func (c *Client) FetchSchedule(ctx context.Context) Schedule {
	var v scheduleBody
	if err := c.get(ctx, "/panel/api/config", &v); err != nil {
		return Schedule{}
	}
	s := v.Config.Schedule
	return Schedule{
		Known:          true,
		Checkin:        s.Checkin,
		Travel:         s.Travel,
		Activity:       s.Activity,
		Keepalive:      s.Keepalive,
		BalanceRefresh: s.BalanceRefresh,
	}
}

// decode lets a scheduleBody be filled by the client's own request path, which hands
// each decoder the response body rather than a reader.
func (b *scheduleBody) decode(data []byte) error {
	return json.Unmarshal(data, b)
}

// Trigger posts to one of the panel's one-shot maintenance endpoints, which is how
// the tray runs a task without making the operator open the console.
//
// The response body is returned because the panel answers with what it did, and
// that text is what the tray shows in its confirmation balloon.
func (c *Client) Trigger(ctx context.Context, path string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, nil)
	if err != nil {
		return "", err
	}
	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode == http.StatusUnauthorized {
		return "", ErrUnauthorized
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return summariseResponse(body), nil
}

// summariseResponse pulls the human-readable part out of a panel response.
//
// The panel answers with JSON, and the interesting field differs between
// endpoints, so the generic keys are tried in turn. The raw body is the last
// resort rather than the first: a balloon holding a brace-heavy JSON blob is
// worse than one holding a sentence.
func summariseResponse(body []byte) string {
	var v map[string]any
	if err := json.Unmarshal(body, &v); err != nil {
		return truncateText(strings.TrimSpace(string(body)), 200)
	}
	for _, key := range []string{"message", "msg", "summary", "note", "error", "detail"} {
		if s, ok := v[key].(string); ok && s != "" {
			return truncateText(s, 200)
		}
	}
	if n, ok := v["count"].(float64); ok {
		return strconv.Itoa(int(n))
	}
	return "ok"
}

func truncateText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// SortAccounts orders accounts the way an operator scans them: broken ones
// first, then by name.
func SortAccounts(accounts []status.Account) {
	rank := func(a status.Account) int {
		switch {
		case a.Disabled:
			return 3
		case a.Cooling:
			return 2
		default:
			return 0
		}
	}
	sort.SliceStable(accounts, func(i, j int) bool {
		ri, rj := rank(accounts[i]), rank(accounts[j])
		if ri != rj {
			return ri < rj
		}
		return accounts[i].Nickname < accounts[j].Nickname
	})
}
