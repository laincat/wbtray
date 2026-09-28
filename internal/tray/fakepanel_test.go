//go:build windows

package tray

import (
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"

	"wbtray/internal/iconstyle"
	"wbtray/internal/raster"
	"wbtray/internal/status"
	"wbtray/internal/theme"
)

// A fake panel, so the tray's client and menu can be exercised against realistic
// JSON rather than against a stub that agrees with whatever the code happens to
// expect.

type fakeServer struct {
	*httptest.Server
	requireKey string
}

// startFakePanel serves the four endpoints the tray reads, with the shapes the
// real gateway returns.
func startFakePanel(t *testing.T) *fakeServer {
	t.Helper()
	fs := &fakeServer{requireKey: "test-key"}
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSONTest(w, map[string]any{
			"healthy": 2, "total": 3, "service": "workbuddy2api",
			"realm_servable": map[string]bool{"cn": true, "global": false},
		})
	})

	mux.HandleFunc("/panel/api/overview", fs.auth(func(w http.ResponseWriter, r *http.Request) {
		writeJSONTest(w, map[string]any{
			"version": "1.11.6-panel", "uptime_sec": 7325, "auth_required": true,
			"redis_mode": "memory", "sticky_sessions": 2,
			"total": 3, "healthy": 2, "cooling": 1, "disabled": 1, "in_flight_full": 0,
			"accounts": []map[string]any{
				{"uid": "u1", "nickname": "laincat", "credits": 12000, "credits_total": 20000,
					"cooling": false, "disabled": false, "realm": "cn", "in_flight": 1,
					"success_count": 412, "err_total": 3},
				{"uid": "u2", "nickname": "backup", "credits": 9000, "credits_total": 20000,
					"cooling": true, "cool_kind": "soft", "cool_remaining_sec": 420,
					"disabled": false, "realm": "cn"},
				{"uid": "u3", "nickname": "spare", "credits": 0, "disabled": true,
					"disabled_reason": "token expired", "realm": "global"},
			},
		})
	}))

	mux.HandleFunc("/panel/api/usage", fs.auth(func(w http.ResponseWriter, r *http.Request) {
		hours := 24
		if v := r.URL.Query().Get("hours"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				hours = n
			}
		}
		series := make([]map[string]any, 0, hours)
		for i := 0; i < hours; i++ {
			series = append(series, map[string]any{
				"t": fmtHour(i), "requests": 20 + i*3, "errors": i % 5,
				"total_tokens": 1000 + i*137, "avg_latency_ms": 700 + float64(i),
			})
		}
		writeJSONTest(w, map[string]any{
			"totals": map[string]any{
				"requests": 1284, "errors": 12, "prompt_tokens": 40000,
				"completion_tokens": 9000, "total_tokens": 49000,
				"avg_latency_ms": 812.5, "avg_tokens_per_second": 41.7,
			},
			"by_realm": []any{}, "by_account": []any{}, "by_model": []any{},
			"series": series, "buckets": hours, "generated": "2026-09-26T12:00:00Z",
		})
	}))

	mux.HandleFunc("/panel/api/logs", fs.auth(func(w http.ResponseWriter, r *http.Request) {
		writeJSONTest(w, map[string]any{"entries": []map[string]any{
			{"ts": "2026-09-26T11:59:00Z", "ch": "task", "text": "checkin u1 成功"},
			{"ts": "2026-09-26T11:59:30Z", "ch": "sys", "text": "balance refresh failed once"},
			{"ts": "2026-09-26T12:00:00Z", "ch": "chat", "text": "| #12 u1 ok 812ms"},
		}})
	}))

	// The gateway's own configuration, which is where the tray learns which
	// scheduled tasks are switched on. It is served through the panel API rather
	// than read from disk so the answer is what the running gateway is using.
	mux.HandleFunc("/panel/api/config", fs.auth(func(w http.ResponseWriter, r *http.Request) {
		writeJSONTest(w, map[string]any{
			"ok": true,
			"config": map[string]any{
				"schedule": map[string]any{
					"checkin_enabled":         true,
					"travel_enabled":          true,
					"activity_enabled":        true,
					"keepalive_enabled":       false,
					"balance_refresh_enabled": true,
				},
			},
		})
	}))

	fs.Server = httptest.NewServer(mux)
	return fs
}

// auth refuses a request without the expected bearer token, which is what the
// tray has to handle rather than treat as an outage.
func (f *fakeServer) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+f.requireKey {
			w.WriteHeader(http.StatusUnauthorized)
			writeJSONTest(w, map[string]any{"ok": false, "error": "invalid_api_key"})
			return
		}
		next(w, r)
	}
}

func writeJSONTest(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func fmtHour(i int) string {
	return "2026-09-26T" + pad2(i) + ":00"
}

func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// renderForTest draws an icon through the same entry point the tray uses.
func renderForTest(t *testing.T, style, metric string, size int, pal theme.Palette, snap status.Snapshot) *raster.Canvas {
	t.Helper()
	return iconstyle.Draw(iconstyle.View{
		Size:    size,
		Style:   style,
		Metric:  metric,
		Lang:    "en",
		Palette: pal,
		Snap:    snap,
	})
}

// writeIconSheet saves one rendered icon, magnified, for inspection.
func writeIconSheet(t *testing.T, dir, themeName, style, metric string, c *raster.Canvas) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := dir + string(os.PathSeparator) + themeName + "-" + style + ".png"
	file, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := png.Encode(file, c.Scale(c.W*8, c.H*8).Image()); err != nil {
		t.Fatal(err)
	}
	_ = metric
}
