package app

import (
	"errors"
	"fmt"
	"strings"

	"wbtray/internal/config"
	"wbtray/internal/i18n"
	"wbtray/internal/iconstyle"
	"wbtray/internal/panel"
	"wbtray/internal/raster"
	"wbtray/internal/status"
	"wbtray/internal/theme"
	"wbtray/internal/traymenu"
)

// The menu model.
//
// It is rebuilt from live state on every open, which is what lets it show current
// numbers rather than the ones that were true when the tray started.
//
// The shape is four blocks separated by rules: the state, the gateway, the tray's own
// appearance, and the one row that ends it. Every row carries a sentence explaining
// what it does, shown while the pointer is on it — a menu row can say what it is
// called, and cannot say what it does.

// previewSize is the pixel size of a style row's preview: the same size the taskbar
// uses, so the gallery shows the real thing rather than an approximation.
const previewSize = 20

// Menu builds the model for the front end.
func (a *App) Menu() []traymenu.Item {
	a.mu.Lock()
	snap, paused, cfg := a.snap, a.paused, a.cfg
	a.mu.Unlock()

	lang := cfg.Lang
	th := a.Theme()

	items := []traymenu.Item{a.stateRow(snap, paused, cfg, th, lang)}
	items = append(items, traymenu.Separator())

	items = append(items,
		a.gatewayRow(snap, cfg, lang, th),
		a.accountsRow(snap, lang),
		a.tasksRow(lang),
		traymenu.Command(IDOpenPanel, i18n.T(lang, "menu.panel"), i18n.T(lang, "hint.panel")).
			WithDefault(),
		a.chartRow(lang),
		a.otherRow(snap, paused, cfg, lang),
		traymenu.Separator(),
	)

	items = append(items, a.trayRow(cfg, th, lang))
	items = append(items, traymenu.Separator())

	items = append(items, traymenu.Command(IDQuit, i18n.T(lang, "menu.exit"), i18n.T(lang, "hint.exit")))
	return items
}

// stateRow is the single readout at the top: a colour and the credits behind it.
//
// The colour answers "is the gateway serving", the figure answers "for how much
// longer", and the sentence answers everything else — which is why the row is one
// short pair rather than the two rows of labels this used to open with. The address
// that used to sit here is a row of the gateway submenu, where it can be clicked to
// copy.
func (a *App) stateRow(snap status.Snapshot, paused bool, cfg config.Config, th theme.Theme, lang string) traymenu.Item {
	dot := colorOf(th, healthOf(snap, paused))

	var text, value, hint string
	switch {
	case errors.Is(snap.Err, panel.ErrUnauthorized):
		text = i18n.T(lang, "state.badkey")
		hint = i18n.T(lang, "hint.badkey")
	case !snap.Reachable:
		if paused {
			text = i18n.T(lang, "state.paused")
			hint = i18n.T(lang, "hint.paused")
		} else {
			text = i18n.T(lang, "state.offline")
			hint = i18n.T(lang, "hint.offline", cfg.BaseURL)
		}
	case snap.Total == 0:
		text = i18n.T(lang, "state.noaccounts")
		hint = i18n.T(lang, "hint.noaccounts")
	default:
		text = i18n.T(lang, "state.credits")
		value = i18n.Num(snap.CreditTotal())
		hint = i18n.T(lang, "hint.state", snap.Ready(), snap.Total, snap.InFlight())
	}
	return traymenu.Status(text, value, hint, dot, true)
}

// gatewayRow is everything about the process, gathered under one row.
func (a *App) gatewayRow(snap status.Snapshot, cfg config.Config, lang string, th theme.Theme) traymenu.Item {
	pid := snap.Process
	children := []traymenu.Item{}

	if pid.Found {
		children = append(children,
			traymenu.ValueHint("PID", fmt.Sprint(pid.PID), i18n.T(lang, "hint.pid")),
			traymenu.Command(IDGatewayStop, i18n.T(lang, "gw.stop"), i18n.T(lang, "hint.gw_stop")),
			traymenu.Command(IDGatewayRestart, i18n.T(lang, "gw.restart"), i18n.T(lang, "hint.gw_restart")),
			traymenu.Command(IDConsoleToggle, consoleLabel(a, lang), i18n.T(lang, "hint.console")),
		)
		if a.NeedsLogin() {
			children = append(children, traymenu.Command(IDLogin, i18n.T(lang, "menu.login"),
				i18n.T(lang, "hint.login")))
		}
		if !pid.Started {
			children = append(children, traymenu.ValueHint("", i18n.T(lang, "gw.external"),
				i18n.T(lang, "hint.external")))
		}
	} else {
		children = append(children,
			traymenu.Command(IDGatewayStart, i18n.T(lang, "gw.start"), i18n.T(lang, "hint.gw_start")),
			traymenu.Check(IDConsoleToggle, i18n.T(lang, "gw.console_show"),
				i18n.T(lang, "hint.console"), cfg.ShowConsole),
		)
		if !a.GatewayInstalled() {
			children = append(children, traymenu.Command(IDInstallGateway,
				i18n.T(lang, "menu.install_gateway"), i18n.T(lang, "hint.install")))
		}
	}

	children = append(children,
		traymenu.Separator(),
		a.copyAddrRow(lang),
		a.copyKeyRow(lang),
		traymenu.Separator(),
		traymenu.Check(IDGatewayAutoStart, i18n.T(lang, "menu.autostart"),
			i18n.T(lang, "hint.gw_autostart"), a.gateway.AutoStartEnabled()),
		traymenu.Command(IDOpenGatewayDir, i18n.T(lang, "menu.dir"), i18n.T(lang, "hint.gw_dir")),
		traymenu.Command(IDOpenGatewayConfig, i18n.T(lang, "menu.file"), i18n.T(lang, "hint.gw_file")),
	)

	// The block's own label carries the state, and the value column carries the
	// identifier, so neither repeats the other.
	state := i18n.T(lang, "status.offline")
	value := ""
	health := status.HealthDown
	if pid.Found {
		state = ""
		value = fmt.Sprintf("PID %d", pid.PID)
		health = status.HealthOK
	}
	if a.NeedsLogin() {
		value = i18n.T(lang, "gw.no_accounts_short")
	}
	label := i18n.T(lang, "menu.gateway")
	if state != "" {
		label += " · " + state
	}
	row := traymenu.Submenu(label, i18n.T(lang, "hint.gateway"), children)
	row.Value = value
	row.Dot = colorOf(th, int(health))
	return row
}

// accountsRow is the pool, one row per account.
func (a *App) accountsRow(snap status.Snapshot, lang string) traymenu.Item {
	const maxRows = 16
	children := make([]traymenu.Item, 0, maxRows)
	for i, acct := range snap.Accounts {
		if i >= maxRows {
			children = append(children, traymenu.Value(
				fmt.Sprintf("… %d more", len(snap.Accounts)-i), ""))
			break
		}
		name := acct.Nickname
		if name == "" {
			name = acct.UID
		}
		value := i18n.Num(acct.Credits)
		switch {
		case acct.Disabled:
			value = i18n.T(lang, "menu.disabled_one")
		case acct.Cooling:
			if left := acct.CoolRemaining(); left > 0 {
				value = fmt.Sprintf("%s · %s", value, i18n.Duration(int64(left.Seconds())))
			}
		case acct.InFlight > 0:
			value = fmt.Sprintf("%s · %s", value, i18n.T(lang, "menu.inflight_short", acct.InFlight))
		}
		children = append(children, traymenu.Item{
			Kind:     traymenu.ValueRow,
			Text:     name,
			Value:    value,
			Hint:     i18n.T(lang, "hint.account", acct.UID, acct.Realm),
			Disabled: true,
			Dot:      accountColor(acct),
		})
	}
	if len(children) == 0 {
		children = append(children, traymenu.Value(i18n.T(lang, "status.no_accounts"), ""))
	}
	row := traymenu.Submenu(i18n.T(lang, "menu.accounts"), i18n.T(lang, "hint.accounts"), children)
	row.Value = fmt.Sprintf("%d/%d", snap.Ready(), snap.Total)
	return row
}

// accountColor is the pip beside an account row: green when it can serve, amber when
// it is cooling, blue when it is busy, grey when it is out of play.
func accountColor(acct status.Account) raster.RGBA {
	switch {
	case acct.Disabled:
		return raster.Hex("#6b7488")
	case acct.Cooling:
		return raster.Hex("#f5b544")
	case acct.InFlight > 0:
		return raster.Hex("#5b7cfa")
	default:
		return raster.Hex("#3ddc97")
	}
}

// tasksRow is what the gateway runs on a schedule, shown as the switches it has.
//
// The values come from the gateway's own configuration rather than from a list this
// program keeps: a task the gateway has turned off is drawn without its tick, and a
// task this build has never heard of is not offered as a switch at all.
func (a *App) tasksRow(lang string) traymenu.Item {
	sched := a.Schedule()
	children := []traymenu.Item{
		ticked(i18n.T(lang, "task.checkin"), sched.Checkin, i18n.T(lang, "hint.task_checkin"), lang),
		ticked(i18n.T(lang, "task.travel"), sched.Travel, i18n.T(lang, "hint.task_travel"), lang),
		ticked(i18n.T(lang, "task.activity"), sched.Activity, i18n.T(lang, "hint.task_activity"), lang),
		ticked(i18n.T(lang, "task.keepalive"), sched.Keepalive, i18n.T(lang, "hint.task_keepalive"), lang),
		ticked(i18n.T(lang, "task.balance"), sched.BalanceRefresh, i18n.T(lang, "hint.task_balance"), lang),
		traymenu.Separator(),
	}
	for i, t := range Tasks {
		children = append(children, traymenu.Command(uint32(IDTaskBase+i), i18n.T(lang, t.Key), i18n.T(lang, t.Hint)))
	}
	row := traymenu.Submenu(i18n.T(lang, "menu.tasks"), i18n.T(lang, "hint.tasks"), children)
	if !sched.Known {
		// A gateway that has not been asked yet says so, rather than showing five
		// unticked switches that mean "off" when they mean "unknown".
		row.Value = i18n.T(lang, "menu.unknown")
	}
	return row
}

// ticked builds a row for one scheduled task: a tick when the gateway runs it, and a
// dash when it does not, with the schedule in the sentence either way.
func ticked(name string, on bool, hint, lang string) traymenu.Item {
	row := traymenu.Item{Kind: traymenu.CheckRow, Text: name, Hint: hint, Checked: on}
	if !on {
		row.Value = i18n.T(lang, "menu.off")
	}
	return row
}

// chartRow opens the chart window.
func (a *App) chartRow(lang string) traymenu.Item {
	return traymenu.Command(IDChartWindow, i18n.T(lang, "menu.chart"), i18n.T(lang, "hint.chart"))
}

// liveItems is the block of current numbers.
//
// Each row is one metric, which is what makes the menu a complete readout rather than
// a summary: whichever one the operator put in the icon, the rest are one submenu
// away.
func (a *App) liveItems(snap status.Snapshot, paused bool, cfg config.Config) []traymenu.Item {
	lang := cfg.Lang
	if errors.Is(snap.Err, panel.ErrUnauthorized) {
		return []traymenu.Item{traymenu.Value("", i18n.T(lang, "status.badkey"))}
	}
	if !snap.Reachable {
		text := i18n.T(lang, "state.offline")
		if paused {
			text = i18n.T(lang, "state.paused")
		}
		return []traymenu.Item{traymenu.Value("", text)}
	}
	if snap.Total == 0 {
		return []traymenu.Item{traymenu.Value(i18n.T(lang, "state.noaccounts"), "")}
	}

	items := []traymenu.Item{
		traymenu.Value(i18n.T(lang, "metric.accounts"), fmt.Sprintf("%d/%d", snap.Ready(), snap.Total)),
		traymenu.Value(i18n.T(lang, "metric.credits"), i18n.Num(snap.CreditTotal())),
	}
	if snap.UsageHours > 0 {
		items = append(items, traymenu.Value(
			fmt.Sprintf("%s · %dh", i18n.T(lang, "metric.requests"), snap.UsageHours),
			fmt.Sprintf("%s / %s err", i18n.Compact(float64(snap.Usage.Requests)),
				i18n.Compact(float64(snap.Usage.Errors)))))
	}
	items = append(items,
		traymenu.Value(i18n.T(lang, "metric.tokens"), i18n.Compact(float64(snap.Usage.TotalTokens))),
		traymenu.Value(i18n.T(lang, "metric.latency"), i18n.Round1(snap.Usage.AvgLatencyMs)+" ms"),
		traymenu.Value(i18n.T(lang, "metric.tps"), i18n.Round1(snap.Usage.AvgTPS)),
		traymenu.Value(i18n.T(lang, "metric.queue"), fmt.Sprint(snap.InFlight())),
	)
	if snap.Cooling() > 0 {
		items = append(items, traymenu.Value(i18n.T(lang, "menu.cooling"),
			i18n.T(lang, "menu.cooling_count", snap.Cooling())))
	}
	if snap.Disabled() > 0 {
		items = append(items, traymenu.Value(i18n.T(lang, "menu.disabled"),
			i18n.T(lang, "menu.disabled_count", snap.Disabled())))
	}
	if snap.LogTotal > 0 {
		items = append(items, traymenu.Value(i18n.T(lang, "menu.logs"),
			i18n.T(lang, "menu.logs_value", snap.LogTotal, snap.LogErrors)))
	}
	if snap.Version != "" {
		items = append(items, traymenu.Value(i18n.T(lang, "menu.version"), snap.Version))
	}
	return items
}

// otherRow is everything else the gateway reports and the tray can do to it.
//
// The live figures live here rather than in the header: they are what an operator
// reads once they have decided something is wrong, and the header is for deciding.
func (a *App) otherRow(snap status.Snapshot, paused bool, cfg config.Config, lang string) traymenu.Item {
	children := append([]traymenu.Item{}, a.liveItems(snap, paused, cfg)...)
	children = append(children,
		traymenu.Separator(),
		traymenu.Command(IDRefresh, i18n.T(lang, "menu.refresh"), i18n.T(lang, "hint.refresh")),
	)
	if paused {
		children = append(children, traymenu.Command(IDPause, i18n.T(lang, "menu.resume"),
			i18n.T(lang, "hint.resume")))
	} else {
		children = append(children, traymenu.Command(IDPause, i18n.T(lang, "menu.pause"),
			i18n.T(lang, "hint.pause")))
	}
	return traymenu.Submenu(i18n.T(lang, "menu.other"), i18n.T(lang, "hint.other"), children)
}

// trayRow is everything about how the tray itself looks and behaves.
func (a *App) trayRow(cfg config.Config, th theme.Theme, lang string) traymenu.Item {
	children := []traymenu.Item{
		traymenu.Submenu(i18n.T(lang, "menu.style"), i18n.T(lang, "hint.style"),
			a.styleItems(cfg, th, lang)),
		traymenu.Submenu(i18n.T(lang, "menu.theme"), i18n.T(lang, "hint.theme"),
			themeItems(a, cfg, lang)),
		traymenu.Submenu(i18n.T(lang, "menu.metric"), i18n.T(lang, "hint.metric_block"),
			metricItems(cfg, lang)),
		traymenu.Submenu(i18n.T(lang, "menu.language"), i18n.T(lang, "hint.language"),
			[]traymenu.Item{
				traymenu.Radio(IDLangBase, "中文", i18n.T(lang, "hint.lang_zh"), cfg.Lang == "zh"),
				traymenu.Radio(IDLangBase+1, "EN", i18n.T(lang, "hint.lang_en"), cfg.Lang == "en"),
			}),
		traymenu.Separator(),
		traymenu.Check(IDTrayAutoStart, i18n.T(lang, "menu.autostart"),
			i18n.T(lang, "hint.tray_autostart"), a.TrayAutoStart()),
		traymenu.Command(IDOpenFolder, i18n.T(lang, "menu.dir"), i18n.T(lang, "hint.tray_dir")),
		traymenu.Command(IDOpenConfig, i18n.T(lang, "menu.file"), i18n.T(lang, "hint.tray_file")),
		traymenu.Command(IDReload, i18n.T(lang, "menu.reload"), i18n.T(lang, "hint.reload")),
		a.updateItems(lang),
		traymenu.Command(IDAbout, i18n.T(lang, "menu.about"), i18n.T(lang, "hint.about")),
	}
	return traymenu.Submenu(i18n.T(lang, "menu.tray"), i18n.T(lang, "hint.tray"), children)
}

// copyAddrRow copies the gateway address.
func (a *App) copyAddrRow(lang string) traymenu.Item {
	row := traymenu.Command(IDCopyURL, i18n.T(lang, "copy.addr"), i18n.T(lang, "hint.copy_addr"))
	row.Value = hostOf(a.BaseURL())
	return row
}

// copyKeyRow copies the gateway API key.
//
// The key is never drawn into the menu — a screenshot of an open menu should not be a
// credential — so the row is a verb and the balloon is the only confirmation.
func (a *App) copyKeyRow(lang string) traymenu.Item {
	row := traymenu.Command(IDCopyKey, i18n.T(lang, "copy.key"), i18n.T(lang, "hint.copy_key"))
	if a.PanelClient().Key() == "" {
		row.Disabled = true
		row.Value = i18n.T(lang, "copy.none")
		row.Hint = i18n.T(lang, "hint.copy_key_none")
	}
	return row
}

// styleItems is the gallery: one row per style, each carrying a rendering of the icon
// it would produce at the current metric, theme and numbers.
func (a *App) styleItems(cfg config.Config, th theme.Theme, lang string) []traymenu.Item {
	out := make([]traymenu.Item, 0, len(config.Styles))
	for _, name := range config.Styles {
		idx := indexOf(config.Styles, name)
		out = append(out, traymenu.Item{
			Kind:    traymenu.StyleRow,
			ID:      uint32(IDStyleBase + idx),
			Text:    i18n.T(lang, "style."+name),
			Hint:    i18n.T(lang, "hint.style_pick", i18n.T(lang, "style."+name)),
			Checked: cfg.Style == name,
			Preview: a.previewFor(name, th),
		})
	}
	return out
}

// previewFor renders one style with the live numbers.
func (a *App) previewFor(style string, th theme.Theme) *raster.Canvas {
	a.mu.Lock()
	snap, metric, lang, paused := a.snap, a.cfg.Metric, a.cfg.Lang, a.paused
	a.mu.Unlock()
	return iconstyle.Draw(iconstyle.View{
		Size:   previewSize,
		Style:  style,
		Metric: metric,
		Lang:   lang,
		Theme:  th,
		Snap:   snap,
		Paused: paused,
	})
}

// themeItems is the palette list.
func themeItems(a *App, cfg config.Config, lang string) []traymenu.Item {
	all := theme.All()
	out := make([]traymenu.Item, 0, len(all)+len(theme.Appearances)+1)
	for i, th := range all {
		out = append(out, traymenu.Radio(uint32(IDThemeBase+i), th.Label(lang),
			i18n.T(lang, "hint.theme_pick", th.Label(lang)), cfg.Theme == th.Name))
	}
	out = append(out, traymenu.Separator())
	for i, ap := range theme.Appearances {
		out = append(out, traymenu.Radio(uint32(IDAppearanceBase+i),
			theme.AppearanceLabel(ap, lang), i18n.T(lang, "hint.appearance"), theme.ParseAppearance(cfg.Appearance) == ap))
	}
	return out
}

// metricItems is which figure the icon draws.
//
// It is a block of its own rather than part of the style gallery: the style is the
// shape and the metric is the reading, and every shape can carry every reading.
func metricItems(cfg config.Config, lang string) []traymenu.Item {
	hint := i18n.T(lang, "hint.metric")
	out := make([]traymenu.Item, 0, len(config.Metrics))
	for i, m := range config.Metrics {
		out = append(out, traymenu.Radio(uint32(IDMetricBase+i), i18n.T(lang, "metric."+m),
			hint, cfg.Metric == m))
	}
	return out
}

// updateItems is the version block: what is installed, what is available, and the one
// action that resolves the difference.
func (a *App) updateItems(lang string) traymenu.Item {
	u := a.Updates()
	installedGW := a.GatewayVersion()
	installedTray := a.TrayVersion()

	children := []traymenu.Item{}

	switch {
	case u.Checked.IsZero():
		children = append(children, traymenu.Value(i18n.T(lang, "menu.gateway_version"),
			i18n.T(lang, "menu.update_checking")))
	case u.GatewayUpdate:
		children = append(children, traymenu.Command(IDUpdateGateway,
			i18n.T(lang, "menu.update_gateway", u.Gateway), i18n.T(lang, "hint.update_gateway")))
	default:
		children = append(children, traymenu.Value(i18n.T(lang, "menu.gateway_version"),
			i18n.T(lang, "menu.no_update")))
	}
	if installedGW != "" {
		children = append(children, traymenu.Value("", i18n.T(lang, "menu.installed_ver", installedGW)))
	}
	if u.TrayUpdate {
		children = append(children, traymenu.Command(IDUpdateTray,
			i18n.T(lang, "menu.update_tray", u.Tray), i18n.T(lang, "hint.update_tray")))
	}
	children = append(children,
		traymenu.Value(i18n.T(lang, "menu.tray_version"), installedTray),
		traymenu.Command(IDCheckUpdates, i18n.T(lang, "menu.updates"), i18n.T(lang, "hint.check_updates")),
	)

	row := traymenu.Submenu(i18n.T(lang, "menu.version_block"), i18n.T(lang, "hint.version"), children)
	if u.Err != nil {
		row.Value = i18n.T(lang, "menu.check_failed")
	}
	return row
}

// healthOf classifies the snapshot, counting a paused tray as neither well nor broken:
// its numbers are simply not being refreshed.
func healthOf(snap status.Snapshot, paused bool) int {
	if paused && !snap.Reachable {
		return int(status.HealthWarn)
	}
	return int(snap.Health())
}

func healthLabel(lang string, health int) string {
	if lang == "zh" {
		switch health {
		case int(status.HealthOK):
			return "正常"
		case int(status.HealthWarn):
			return "异常"
		default:
			return "离线"
		}
	}
	return status.Health(health).Label(lang)
}

func colorOf(th theme.Theme, health int) raster.RGBA {
	return th.Health(health)
}

// hostOf strips the scheme from a base URL, which is the part an operator recognises
// at a glance and the part that fits a menu's value column.
func hostOf(base string) string {
	return strings.TrimPrefix(strings.TrimPrefix(base, "http://"), "https://")
}

func indexOf(list []string, value string) int {
	for i, v := range list {
		if v == value {
			return i
		}
	}
	return -1
}

// consoleLabel says what the console switch will do, which depends on whether a console
// window exists right now.
func consoleLabel(a *App, lang string) string {
	if a.gateway.HasConsole() {
		return i18n.T(lang, "gw.console_hide")
	}
	return i18n.T(lang, "gw.console_show")
}
