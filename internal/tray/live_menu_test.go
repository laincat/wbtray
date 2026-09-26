//go:build windows

package tray

import (
	"context"
	"image/png"
	"os"
	"testing"

	"wbtray/internal/app"
	"wbtray/internal/config"
	"wbtray/internal/raster"
	"wbtray/internal/status"
	"wbtray/internal/theme"
)

// stubGateway is the process controller the application talks to, with no
// process behind it.
type stubGateway struct {
	pid       status.PIDInfo
	autoStart bool
	console   bool
}

func (s *stubGateway) PID() status.PIDInfo          { return s.pid }
func (s *stubGateway) Start() (uint32, error)       { s.pid = status.PIDInfo{Found: true, PID: 4242}; return 4242, nil }
func (s *stubGateway) Stop() error                  { s.pid = status.PIDInfo{}; return nil }
func (s *stubGateway) Restart() (uint32, error)     { return s.Start() }
func (s *stubGateway) HasConsole() bool             { return s.console }
func (s *stubGateway) SetConsole(show bool) error   { s.console = show; return nil }
func (s *stubGateway) AutoStartEnabled() bool       { return s.autoStart }
func (s *stubGateway) SetAutoStart(on bool) error   { s.autoStart = on; return nil }

// TestLiveMenuLaysOutAndPaints runs the real application's menu — every row the
// tray would offer, in the real order, with the real wording — through the real
// painter.
//
// The synthetic menu in the other test proves the painter can draw every kind of
// row; this proves the menu the application actually builds is one the painter
// can draw. Those are different failures, and only the second one ships.
func TestLiveMenuLaysOutAndPaints(t *testing.T) {
	for _, lang := range []string{"zh", "en"} {
		for _, menuStyle := range []string{"flyout", "native"} {
			name := lang + "-" + menuStyle
			t.Run(name, func(t *testing.T) {
				cfg := config.Default()
				cfg.Lang = lang
				cfg.MenuStyle = menuStyle
				cfg.BaseURL = "http://127.0.0.1:7863"
				cfg.APIKey = "test-key"

				gw := &stubGateway{pid: status.PIDInfo{Found: true, PID: 16780, Started: true}}
				a := app.New(cfg, "", gw, app.Options{})
				a.SetPaused(false)

				items := a.Menu()
				if len(items) < 10 {
					t.Fatalf("the menu has only %d rows", len(items))
				}

				f := &flyout{hover: -1, pressed: -1, subOwner: -1}
				f.mu.Lock()
				f.theme = theme.ByName(cfg.Theme)
				f.scale = 1
				f.mu.Unlock()
				f.layout(items)

				f.mu.Lock()
				rows, w, h := f.rows, f.width, f.height
				f.mu.Unlock()
				if w < 200 || h < 200 {
					t.Fatalf("laid out %dx%d", w, h)
				}

				c := raster.New(w, h)
				paintMenu(c, rows, 1, theme.ByName(cfg.Theme), -1, 0)
				painted := 0
				for y := 0; y < c.H; y++ {
					for x := 0; x < c.W; x++ {
						if c.At(x, y).A > 200 {
							painted++
						}
					}
				}
				if painted < c.W*c.H/2 {
					t.Fatalf("only %d of %d pixels are opaque", painted, c.W*c.H)
				}

				if dir := os.Getenv("WBTRAY_MENU_DIR"); dir != "" {
					writeSheet(t, dir, name, c)
				}
			})
		}
	}
}

// TestLiveMenuWithNoGateway draws the menu for a gateway that is not running,
// which is the state a new install starts in and therefore the one most likely
// to be broken by a change to the model.
func TestLiveMenuWithNoGateway(t *testing.T) {
	cfg := config.Default()
	a := app.New(cfg, "", &stubGateway{}, app.Options{})
	items := a.Menu()
	f := &flyout{hover: -1, pressed: -1, subOwner: -1}
	f.mu.Lock()
	f.theme = theme.Neon()
	f.scale = 1
	f.mu.Unlock()
	f.layout(items)
	f.mu.Lock()
	rows, w, h := f.rows, f.width, f.height
	f.mu.Unlock()
	c := raster.New(w, h)
	paintMenu(c, rows, 1, theme.Neon(), -1, 0)
	if c.At(w/2, h/2).A == 0 {
		t.Fatal("nothing was painted")
	}
}

// TestUnreachableGatewaySaysSo checks the wording of the state a fresh install
// shows before the gateway is up, which is the message most users see first.
func TestUnreachableGatewaySaysSo(t *testing.T) {
	for _, lang := range []string{"zh", "en"} {
		cfg := config.Default()
		cfg.Lang = lang
		cfg.BaseURL = "http://127.0.0.1:7863"
		a := app.New(cfg, "", &stubGateway{}, app.Options{})

		tip := a.Tooltip()
		if tip == "" {
			t.Fatalf("%s: the tooltip is empty", lang)
		}
		if !contains(tip, "7863") {
			t.Errorf("%s: the tooltip does not name the address it cannot reach: %q", lang, tip)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (len(needle) == 0 || indexOfSub(haystack, needle) >= 0)
}

func indexOfSub(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

// writeSheet saves a painted menu for inspection. It is a build artefact rather
// than a fixture: nothing reads it back.
func writeSheet(t *testing.T, dir, name string, c *raster.Canvas) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(dir + string(os.PathSeparator) + name + ".png")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := png.Encode(file, c.Image()); err != nil {
		t.Fatal(err)
	}
}

// Unused-import guards: the application is exercised through its own surface,
// and the context import documents that a client would be cancelled.
var _ = context.Background
