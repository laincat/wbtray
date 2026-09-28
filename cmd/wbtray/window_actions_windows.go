//go:build windows

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"wbtray/internal/app"
	"wbtray/internal/panel"
	"wbtray/internal/tray"
	"wbtray/internal/ui"
)

// The window's actions.
//
// Every one of them is a call to the gateway's own API or to a local setting, and
// the window is told what happened so the footer can say so. They run off the
// message thread — the caller in window_windows.go puts each on a goroutine — because
// all of them are network round trips and a window that stops repainting while one
// is in flight reads as a window that has hung.

// windowActions is the handler the window installs. It closes over the application
// and the lifecycle so the actions that touch the installation have what they need.
func windowActions(a *app.App, lc *lifecycle, win *tray.Window) func(ui.Action, string) {
	return func(action ui.Action, arg string) {
		msg, err := runWindowAction(a, lc, win, action, arg)
		win.Finish(msg, err)
	}
}

func runWindowAction(a *app.App, lc *lifecycle, win *tray.Window, action ui.Action, arg string) (string, error) {
	// Every action is bounded: the gateway answers in milliseconds when it is well,
	// and one that does not answer is the thing the operator needs to be told about
	// rather than wait for.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	switch action {
	case ui.ActionRefresh:
		a.Refresh()
		loadWindowData(ctx, a, win)
		return "已刷新", nil

	case ui.ActionOpenPanel:
		if err := openURL(a.PanelClient().PanelURL()); err != nil {
			return "", err
		}
		return "已在浏览器打开控制台", nil

	case ui.ActionOpenGatewayDir:
		if err := openPath(lc.layout.Gateway); err != nil {
			return "", err
		}
		return "已打开网关目录", nil

	case ui.ActionCopyAddress:
		if err := copyToClipboard(a.PanelClient().Base()); err != nil {
			return "", err
		}
		return "已复制网关地址", nil

	case ui.ActionCopyKey:
		// The key is copied rather than shown. A window with a credential drawn in it
		// is a window that ends up in a screenshot, and the whole reason this is a
		// button is that it is a value nobody should have to read off a screen.
		key := a.PanelClient().Key()
		if key == "" {
			return "没有可复制的密钥：网关尚未读取到配置", nil
		}
		if err := copyToClipboard(key); err != nil {
			return "", err
		}
		return "已复制 API 密钥", nil

	case ui.ActionTogglePause:
		a.SetPaused(!a.Paused())
		if a.Paused() {
			return "已暂停刷新", nil
		}
		return "已恢复刷新", nil

	// The pool-wide maintenance, which is the console's own set of buttons.
	case ui.ActionCheckin, ui.ActionTravel, ui.ActionActivity,
		ui.ActionKeepalive, ui.ActionBalance:
		path := map[ui.Action]string{
			ui.ActionCheckin:   "/panel/api/checkin_all",
			ui.ActionTravel:    "/panel/api/travel_all",
			ui.ActionActivity:  "/panel/api/activity_all",
			ui.ActionKeepalive: "/panel/api/keepalive_all",
			ui.ActionBalance:   "/panel/api/balance_all",
		}[action]
		label := map[ui.Action]string{
			ui.ActionCheckin:   "全部签到",
			ui.ActionTravel:    "旅行巡检",
			ui.ActionActivity:  "活跃上报",
			ui.ActionKeepalive: "全部保活",
			ui.ActionBalance:   "刷新余额",
		}[action]
		return runWindowTask(a, label, path, ctx)

	case ui.ActionScan:
		return runWindowTask(a, "扫描待办", "/panel/api/tasks/scan_all", ctx)
	case ui.ActionRunQueue:
		return runWindowTask(a, "执行待办", "/panel/api/tasks/run_queue", ctx)

	// The per-account actions, which are the console's per-row buttons.
	case ui.ActionAccountCheckin, ui.ActionAccountBalance, ui.ActionAccountRevive:
		if arg == "" {
			return "", fmt.Errorf("没有指定账号")
		}
		what := map[ui.Action]string{
			ui.ActionAccountCheckin: "acct_checkin",
			ui.ActionAccountBalance: "acct_balance",
			ui.ActionAccountRevive:  "revive",
		}[action]
		label := map[ui.Action]string{
			ui.ActionAccountCheckin: "签到",
			ui.ActionAccountBalance: "余额",
			ui.ActionAccountRevive:  "恢复",
		}[action]
		summary, err := a.PanelClient().AccountAction(ctx, arg, what)
		if err != nil {
			return "", err
		}
		// The account's own figures are stale the moment one of these lands, so the
		// snapshot is re-read rather than left showing the state before the click.
		a.Refresh()
		loadWindowData(ctx, a, win)
		if summary == "" {
			summary = "完成"
		}
		return fmt.Sprintf("%s：%s", label, summary), nil
	}
	return "", fmt.Errorf("未知操作 %q", action)
}

// runWindowTask posts one of the gateway's one-shot maintenance endpoints and reports what
// came back.
func runWindowTask(a *app.App, label, path string, ctx context.Context) (string, error) {
	summary, err := a.PanelClient().Trigger(ctx, path)
	if err != nil {
		return "", err
	}
	// The numbers move when one of these runs, so the reading is taken again rather
	// than left showing the state before the click.
	a.Refresh()
	if summary == "" {
		summary = "完成"
	}
	return fmt.Sprintf("%s：%s", label, summary), nil
}

// loadWindowData reads the pages beyond the overview and hands them to the window.
//
// It reads all of them rather than only the page showing, because the tabs are one
// click apart and a page that had to fetch on the way in would show an empty state
// for as long as the round trip took. The exception is the model catalogue, which is
// the one call that has to go upstream and can be slow; it is read in the background
// so the window can be looked at while it arrives.
func loadWindowData(ctx context.Context, a *app.App, win *tray.Window) {
	client := a.PanelClient()

	logs, _ := client.Logs(ctx, 0)
	schedule := client.FetchSchedule(ctx)
	config := fetchConfigText(ctx, client)
	win.SetExtra(nil, logs, schedule, config)

	go func() {
		// A second context: this one may outlive the action that started it, and it
		// must not be cancelled by that action's defer.
		mctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		models, err := client.Models(mctx)
		if err != nil {
			return
		}
		win.SetExtra(models, nil, panel.Schedule{}, "")
	}()
}

// fetchConfigText reads the gateway's configuration and returns it as JSON.
//
// It is re-encoded from the gateway's own reply rather than passed through, because
// the reply wraps the configuration in an object with a path and an ok flag, and the
// page wants the settings themselves.
func fetchConfigText(ctx context.Context, client *panel.Client) string {
	raw, err := client.ConfigJSON(ctx)
	if err != nil {
		return ""
	}
	var v struct {
		Config json.RawMessage `json:"config"`
	}
	if err := json.Unmarshal(raw, &v); err != nil || len(v.Config) == 0 {
		return string(raw)
	}
	return string(v.Config)
}
