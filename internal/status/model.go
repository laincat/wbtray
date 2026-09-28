// Package status turns the panel's JSON into the numbers the tray shows.
package status

import "time"

// Snapshot is everything the tray knows at one moment. It is assembled from
// several panel endpoints so a slow one cannot hold up the others.
type Snapshot struct {
	Reachable bool
	Err       error

	// Health is /healthz.
	Healthy       int
	Total         int
	RealmServable map[string]bool

	// Overview is /panel/api/overview.
	Version  string
	Uptime   int64
	Sticky   int
	Redis    string
	Accounts []Account

	// Usage is /panel/api/usage.
	Usage      Usage
	UsageHours int

	// Logs is /panel/api/logs, reduced to counts and the newest line: the tray
	// shows a summary, not the console.
	LogTotal  int
	LogErrors int
	LogWarns  int
	LogTasks  int
	LastLine  string

	// Process is what the tray learned from its own process handle, which stays
	// true even when the HTTP API is unreachable.
	Process PIDInfo

	At time.Time
}

// Account mirrors one entry of the panel's pool list.
type Account struct {
	UID        string `json:"uid"`
	Nickname   string `json:"nickname"`
	Credits    int64  `json:"credits"`
	Total      int64  `json:"credits_total"`
	Cooling    bool   `json:"cooling"`
	CoolKind   string `json:"cool_kind"`
	CoolRemain int64  `json:"cool_remaining_sec"`
	// Until is when a cooldown ends, which is what the gateway actually sends.
	// The remaining time is worked out from it, because a duration would be stale
	// the moment the reading was taken.
	Until      time.Time `json:"until"`
	Disabled   bool      `json:"disabled"`
	Realm      string    `json:"realm"`
	InFlight   int       `json:"in_flight"`
	Success    int64     `json:"success_count"`
	ErrTotal   int64     `json:"err_total"`
	Reason     string    `json:"reason"`
	DisabledBy string    `json:"disabled_reason"`
	Breaker    int       `json:"breaker_fails"`

	// LastUsed is when the account last served a request, which decides which
	// account the tray calls the current one. It is filled by UnmarshalJSON from
	// the nested token usage the gateway sends.
	LastUsed time.Time
}

// Usage is the usage snapshot, reduced to what fits in a tray.
type Usage struct {
	Requests      int64
	Errors        int64
	PromptTokens  int64
	CompletionTok int64
	TotalTokens   int64
	AvgLatencyMs  float64
	AvgTPS        float64
	// Series is the request count per bucket, oldest first. It is what the
	// icon's chart styles draw.
	Series []float64
	// Tokens and Latency are the same buckets in other units, so switching the
	// chart metric does not need another round trip.
	Tokens  []float64
	Latency []float64
}

// PIDInfo is the gateway process as far as the tray can see it.
type PIDInfo struct {
	Found     bool
	PID       uint32
	Exe       string
	Started   bool // started by this tray process
	HasWindow bool
}

// CreditTotal sums what every enabled account still holds.
func (s Snapshot) CreditTotal() int64 {
	var sum int64
	for _, a := range s.Accounts {
		if !a.Disabled {
			sum += a.Credits
		}
	}
	return sum
}

// CoolRemaining is how long an account's cooldown has left.
//
// The gateway reports when a cooldown ends rather than how long is left, so the
// duration has to be worked out against the clock. It also means the figure goes
// stale on its own: a reading taken a minute ago describes a cooldown that has a
// minute less to run, which is exactly right for something drawn once and read
// once.
func (a Account) CoolRemaining() time.Duration {
	if !a.Cooling {
		return 0
	}
	// Some builds report a duration directly, and it wins when it is there.
	if a.CoolRemain > 0 {
		return time.Duration(a.CoolRemain) * time.Second
	}
	if a.Until.IsZero() {
		return 0
	}
	if left := time.Until(a.Until); left > 0 {
		return left
	}
	return 0
}

// Ready counts the accounts that can serve a request right now.
func (s Snapshot) Ready() int {
	n := 0
	for _, a := range s.Accounts {
		if !a.Disabled && !a.Cooling {
			n++
		}
	}
	return n
}

// Cooling counts the accounts sitting out a cooldown.
func (s Snapshot) Cooling() int {
	n := 0
	for _, a := range s.Accounts {
		if a.Cooling {
			n++
		}
	}
	return n
}

// Disabled counts the accounts an operator took out of the pool.
func (s Snapshot) Disabled() int {
	n := 0
	for _, a := range s.Accounts {
		if a.Disabled {
			n++
		}
	}
	return n
}

// InFlight sums the requests the pool is serving right now.
func (s Snapshot) InFlight() int {
	n := 0
	for _, a := range s.Accounts {
		n += a.InFlight
	}
	return n
}

// MetricValue returns the current value of one of the metrics the icon draws.
// Anything with a natural 0-100 range comes back as a percentage, and the
// shapes normalise whatever else they are given.
func (s Snapshot) MetricValue(metric string) float64 {
	switch metric {
	case "accounts":
		if s.Total == 0 {
			return 0
		}
		return float64(s.Ready()) * 100 / float64(s.Total)
	case "credits":
		return float64(s.CreditTotal())
	case "requests":
		return float64(s.Usage.Requests)
	case "tokens":
		return float64(s.Usage.TotalTokens)
	case "latency":
		return s.Usage.AvgLatencyMs
	case "tps":
		return s.Usage.AvgTPS
	case "queue":
		return float64(s.InFlight())
	}
	return 0
}

// SeriesFor returns the per-bucket series for a metric, and whether a chart
// style can draw it at all.
func (s Snapshot) SeriesFor(metric string) []float64 {
	switch metric {
	case "tokens":
		return s.Usage.Tokens
	case "latency":
		return s.Usage.Latency
	case "credits", "requests":
		return s.Usage.Series
	case "accounts", "queue", "tps":
		// These have no history: the tray only knows their present value, so a
		// chart style falls back to drawing the value itself.
		return nil
	}
	return s.Usage.Series
}

// MetricText renders a metric as text, for the tooltip and the menu where the
// figure matters more than the shape. compact is passed in so this package
// stays free of formatting choices that belong to the front end.
func (s Snapshot) MetricText(metric string, compact func(float64) string, round1 func(float64) string) string {
	switch metric {
	case "accounts":
		if s.Total == 0 {
			return "0/0"
		}
		return itoa(s.Ready()) + "/" + itoa(s.Total)
	case "credits":
		return compact(float64(s.CreditTotal()))
	case "requests":
		return compact(float64(s.Usage.Requests)) + " (" + compact(float64(s.Usage.Errors)) + " err)"
	case "tokens":
		return compact(float64(s.Usage.TotalTokens))
	case "latency":
		return round1(s.Usage.AvgLatencyMs) + " ms"
	case "tps":
		return round1(s.Usage.AvgTPS) + " tok/s"
	case "queue":
		return itoa(s.InFlight())
	}
	return ""
}

// Health is the traffic-light state the icon colour encodes.
type Health int

// itoa and round1 keep the formatting of small numbers out of the front end:
// they are used in exactly one place each and are not worth a dependency.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// round1 renders a float with one decimal, without the overhead of a format
// string allocation in a timer callback.
func round1(v float64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	scaled := int64(v*10 + 0.5)
	out := itoa(int(scaled/10)) + "." + itoa(int(scaled%10))
	if neg {
		return "-" + out
	}
	return out
}

// Health states, best first.
const (
	HealthOK Health = iota
	HealthWarn
	HealthDown
)

// Health classifies the snapshot for the icon's colour and the first menu line.
func (s Snapshot) Health() Health {
	switch {
	case !s.Reachable:
		return HealthDown
	case s.Total == 0:
		return HealthWarn
	case s.Ready() == 0:
		return HealthWarn
	default:
		return HealthOK
	}
}

// Label is a short state name for the tooltip and the menu.
func (h Health) Label(lang string) string {
	switch h {
	case HealthOK:
		if lang == "zh" {
			return "正常"
		}
		return "OK"
	case HealthWarn:
		if lang == "zh" {
			return "异常"
		}
		return "Degraded"
	default:
		if lang == "zh" {
			return "离线"
		}
		return "Offline"
	}
}
