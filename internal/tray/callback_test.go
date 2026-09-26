//go:build windows

package tray

import (
	"testing"

	"wbtray/internal/winapi"
)

// TestVersion4NotificationIdsAreExtractedFromTheLowWord is the bug that made the
// tray ignore every click.
//
// A version-4 icon does not receive plain mouse messages. The shell packs the
// icon's id into the high word of lParam and the notification into the low word,
// so the value that arrives is 0x00010205 rather than 0x0205. Comparing the whole
// word against WM_RBUTTONUP — which is what the code did — matches nothing, and
// the symptom is not an error but silence: no menu, no panel, no response.
func TestVersion4NotificationIdsAreExtractedFromTheLowWord(t *testing.T) {
	const iconID = 1
	packed := func(notification uintptr) uintptr { return iconID<<16 | notification }

	for _, tc := range []struct {
		name    string
		lparam  uintptr
		want    uint32
		meaning string
	}{
		{"right click", packed(wmRButtonUp), wmRButtonUp, "opens the menu"},
		{"left click", packed(ninSelect), ninSelect, "opens the panel"},
		{"keyboard menu", packed(ninKeySelect), ninKeySelect, "opens the menu"},
		{"context menu", packed(wmContextMenu), wmContextMenu, "opens the menu"},
		{"double click", packed(wmLButtonDBL), wmLButtonDBL, "opens the chart"},
	} {
		if got := winapi.LowWord(tc.lparam); got != tc.want {
			t.Errorf("%s: extracted %#x from %#x, want %#x",
				tc.name, got, tc.lparam, tc.want)
		}
		// And the whole word is what the broken version compared: it must not
		// equal the notification, or this test would pass on the old code too.
		if uint32(tc.lparam) == tc.want {
			t.Errorf("%s: the packed value equals the notification, so this case proves nothing", tc.name)
		}
	}
}

// TestDispatchReachesEveryAction checks the switch that turns a notification into
// an action, using the same packed values the shell sends.
func TestDispatchReachesEveryAction(t *testing.T) {
	// The dispatch is a switch inside a window procedure that cannot be called
	// without a window, so what is checked here is the classification it relies
	// on: every notification the menu and the panel are reachable from has to be
	// one the low word yields.
	reachable := map[uint32]string{
		wmRButtonUp:   "menu by right click",
		ninKeySelect:  "menu by keyboard",
		wmContextMenu: "menu by context message",
		ninSelect:     "panel by left click",
	}
	for notification, what := range reachable {
		if got := winapi.LowWord(uintptr(1)<<16 | uintptr(notification)); got != notification {
			t.Errorf("%s is not reachable: low word gave %#x", what, got)
		}
	}
}
