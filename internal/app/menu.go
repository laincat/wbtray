package app

import (
	"fmt"
	"strings"

	"wbtray/internal/config"
	"wbtray/internal/i18n"
	"wbtray/internal/iconstyle"
	"wbtray/internal/raster"
	"wbtray/internal/status"
	"wbtray/internal/theme"
	"wbtray/internal/traymenu"
)

// The menu model.
//
// It is rebuilt from live state on every open, which is what lets it show
// current numbers rather than the ones that were true when the tray started.
// Submenus are used for anything with more than a handful of rows — the style
// gallery, the metric list, the account list — so the top level stays short
// enough to read at a glance.

// previewSize is the pixel size of a style row's preview: the same size the
// taskbar uses, so the gallery shows the real thing rather than an
// approximation.
const previewSize = 20

// Menu builds the model for the front end.
func (a *App) Menu() []traymenu.Item {
	a.mu.Lock()
	snap, paused, cfg := a.snap, a.paused, a.cfg
	a.mu.Unlock()

	lang := cfg.Lang
	th := a.Theme()
	health := healthOf(snap, paused)

	// The header carries the state and the gateway's version, not its address: a
	// full URL beside a label leaves room for neither, and the address is already
	// a row of its own further down, where copying it is the point.
	header := traymenu.Item{
		Kind: traymenu.ValueRow,
		Text: fmt.Sprintf("%s · %s", i18n.T(lang, "app.name"), healthLabel(lang, health)),
		Dot:  colorOf(th, health),
		Bold: true,
	}
	header.Value = hostOf(cfg.BaseURL)
	items := []traymenu.Item{header}

	items = append(items, a.liveItems(snap, paused, cfg)...)
	items = append(items, traymenu.Separator())
	items = append(items, a.accountItems(snap, lang))
	items = append(items, traymenu.Command(IDChartWindow, i18n.T(lang, "menu.chart")))
	// A gateway that is neither running nor installed is the one situation where
	// the next thing to do is not discoverable from anywhere in this menu: there
	// is nothing to inspect and nothing to start. The row goes at the top level
	// rather than inside the process block, because an operator in that state has
	// no reason to go looking inside a block about a process that does not exist.
	if !snap.Process.Found && !a.GatewayInstalled() {
		items = append(items, traymenu.Command(IDInstallGateway, i18n.T(lang, "menu.install_gateway")))
	}
	items = append(items, traymenu.Separator())

	items = append(items,
		traymenu.Submenu(i18n.T(lang, "menu.style"), a.styleItems(cfg, th, lang)).
			Expanded(cfg.MenuStyle != "native"),
		traymenu.Submenu(i18n.T(lang, "menu.theme"), themeItems(a, cfg, lang)),
		traymenu.Submenu(i18n.T(lang, "menu.metric"), metricItems(cfg, lang)),
		traymenu.Submenu(i18n.T(lang, "menu.language"), []traymenu.Item{
			traymenu.Radio(IDLangBase, "简体中文", cfg.Lang == "zh"),
			traymenu.Radio(IDLangBase+1, "English", cfg.Lang == "en"),
		}),
		traymenu.Radio(IDClassicMenu, i18n.T(lang, "menu.classic"), cfg.MenuStyle == "native"),
		traymenu.Separator(),
		a.gatewayItems(snap, cfg, lang, th),
		a.updateItems(lang),
		a.taskItems(lang),
		traymenu.Separator(),
		traymenu.Command(IDOpenPanel, i18n.T(lang, "menu.open_panel")),
		traymenu.Command(IDCopyURL, i18n.T(lang, "menu.copy_url")),
		traymenu.Command(IDOpenConfig, i18n.T(lang, "menu.open_config")),
		traymenu.Command(IDReload, i18n.T(lang, "menu.reload")),
		traymenu.Command(IDRefresh, i18n.T(lang, "menu.refresh")),
	)
	if paused {
		items = append(items, traymenu.Command(IDPause, i18n.T(lang, "menu.resume")))
	} else {
		items = append(items, traymenu.Command(IDPause, i18n.T(lang, "menu.pause")))
	}
	items = append(items, traymenu.Separator(),
		traymenu.Check(IDTrayAutoStart, i18n.T(lang, "menu.autostart"), a.TrayAutoStart()),
		traymenu.Command(IDAbout, i18n.T(lang, "menu.about")),
		traymenu.Command(IDQuit, i18n.T(lang, "menu.exit")),
	)
	return items
}

// liveItems is the block of current numbers. Each row is one metric, which is
// what makes the menu a complete readout rather than a summary: whichever one
// the operator put in the icon, the rest are one right-click away.
func (a *App) liveItems(snap status.Snapshot, paused bool, cfg config.Config) []traymenu.Item {
	lang := cfg.Lang
	if !snap.Reachable {
		// One full-width sentence rather than a label and a value: the message
		// is the whole row's content, and squeezing it into a value column would
		// clip exactly the part that says which address failed.
		text := i18n.T(lang, "status.unreachable", hostOf(cfg.BaseURL))
		if paused {
			text = i18n.T(lang, "health.paused")
		}
		return []traymenu.Item{{Kind: traymenu.ValueRow, Text: text, Disabled: true}}
	}
	if snap.Total == 0 {
		return []traymenu.Item{traymenu.Value(i18n.T(lang, "menu.live"), i18n.T(lang, "status.no_accounts"))}
	}

	items := []traymenu.Item{
		traymenu.Value(i18n.T(lang, "metric.accounts"), fmt.Sprintf("%d/%d", snap.Ready(), snap.Total)),
		traymenu.Value(i18n.T(lang, "metric.credits"), i18n.Num(snap.CreditTotal())),
	}
	// The window is the same one the chart uses, so the figures here and the
	// curve there describe the same period.
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
		items = append(items, traymenu.Value(i18n.T(lang, "menu.cooling"), i18n.T(lang, "menu.cooling_count", snap.Cooling())))
	}
	if snap.Disabled() > 0 {
		items = append(items, traymenu.Value(i18n.T(lang, "menu.disabled"), i18n.T(lang, "menu.disabled_count", snap.Disabled())))
	}
	// The log row counts what matters rather than reciting all three totals: the
	// trimmed row carries the two figures an operator reacts to, and the full
	// sentence would not fit beside its own label.
	if snap.LogTotal > 0 {
		items = append(items, traymenu.Value(i18n.T(lang, "menu.logs"),
			i18n.T(lang, "menu.logs_value", snap.LogTotal, snap.LogErrors)))
	}
	if snap.Version != "" {
		// The version row shows the version. Its uptime went with it: the two
		// together overflow the value column and the uptime is already in the
		// tooltip, where there is room for it.
		items = append(items, traymenu.Value(i18n.T(lang, "menu.version"), snap.Version))
	}
	return items
}

// accountItems lists the pool, capped: a menu with a row per account is fine at
// three accounts and unusable at thirty.
func (a *App) accountItems(snap status.Snapshot, lang string) traymenu.Item {
	const maxRows = 12
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
			value = fmt.Sprintf("%s · %s", value, i18n.Duration(acct.CoolRemain))
		case acct.InFlight > 0:
			value = fmt.Sprintf("%s · %s", value, i18n.T(lang, "menu.inflight_short", acct.InFlight))
		}
		children = append(children, traymenu.Item{
			Kind:     traymenu.ValueRow,
			Text:     name,
			Value:    value,
			Disabled: true,
			Dot:      accountColor(acct),
		})
	}
	if len(children) == 0 {
		children = append(children, traymenu.Value(i18n.T(lang, "status.no_accounts"), ""))
	}
	return traymenu.Submenu(
		fmt.Sprintf("%s · %d/%d", i18n.T(lang, "menu.accounts"), snap.Ready(), snap.Total),
		children)
}

// accountColor is the pip beside an account row: green when it can serve,
// amber when it is cooling, blue when it is busy, grey when it is out of play.
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

// styleItems is the gallery: one row per style, each carrying a rendering of the
// icon it would produce at the current metric, theme and numbers.
func (a *App) styleItems(cfg config.Config, th theme.Theme, lang string) []traymenu.Item {
	// Every style is offered on every palette. The two palettes differ in ink,
	// not in what they can draw, and a gallery with holes in it is a puzzle
	// rather than a choice.
	out := make([]traymenu.Item, 0, len(config.Styles))
	for _, name := range config.Styles {
		idx := indexOf(config.Styles, name)
		out = append(out, traymenu.Item{
			Kind:    traymenu.StyleRow,
			ID:      uint32(IDStyleBase + idx),
			Text:    i18n.T(lang, "style."+name),
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
		out = append(out, traymenu.Radio(uint32(IDThemeBase+i), th.Label(lang), cfg.Theme == th.Name))
	}
	out = append(out, traymenu.Separator())
	// The appearance rows sit with the palettes because they answer the same
	// question — what the tray looks like — and an operator looking for "follow
	// Windows" looks here.
	for i, ap := range theme.Appearances {
		out = append(out, traymenu.Radio(uint32(IDAppearanceBase+i),
			theme.AppearanceLabel(ap, lang), theme.ParseAppearance(cfg.Appearance) == ap))
	}
	return out
}

// metricItems is the metric list.
func metricItems(cfg config.Config, lang string) []traymenu.Item {
	out := make([]traymenu.Item, 0, len(config.Metrics))
	for i, m := range config.Metrics {
		out = append(out, traymenu.Radio(uint32(IDMetricBase+i), i18n.T(lang, "metric."+m), cfg.Metric == m))
	}
	return out
}

// gatewayItems is the process block: what is running, and the three things that
// can be done about it.
func (a *App) gatewayItems(snap status.Snapshot, cfg config.Config, lang string, th theme.Theme) traymenu.Item {
	pid := snap.Process
	children := []traymenu.Item{}

	if pid.Found {
		children = append(children,
			traymenu.Value("PID", fmt.Sprint(pid.PID)),
			traymenu.Command(IDGatewayStop, i18n.T(lang, "gw.stop")),
			traymenu.Command(IDGatewayRestart, i18n.T(lang, "gw.restart")),
			traymenu.Command(IDConsoleToggle, consoleLabel(a, lang)),
		)
		if a.NeedsLogin() {
			// The state a fresh install is in: the gateway is up and serving
			// nothing, and the one thing to do about it is not obvious from
			// anywhere else in the menu.
			children = append(children, traymenu.Command(IDLogin, i18n.T(lang, "menu.login")))
		}
		if !pid.Started {
			children = append(children, traymenu.Value("", i18n.T(lang, "gw.external")))
		}
	} else {
		children = append(children,
			traymenu.Command(IDGatewayStart, i18n.T(lang, "gw.start")),
			traymenu.Check(IDConsoleToggle, i18n.T(lang, "gw.console_show"), cfg.ShowConsole),
		)
		if !a.GatewayInstalled() {
			// Nothing to start, so the useful action is to fetch one.
			children = append(children, traymenu.Command(IDInstallGateway,
				i18n.T(lang, "menu.install_gateway")))
		}
	}
	children = append(children,
		traymenu.Separator(),
		traymenu.Check(IDGatewayAutoStart, i18n.T(lang, "menu.gw_autostart"), a.gateway.AutoStartEnabled()),
		traymenu.Command(IDOpenGatewayDir, i18n.T(lang, "menu.open_gateway_dir")),
	)

	// The block's own label carries the state, and the row's value column carries
	// the identifier, so neither has to repeat the other. Spelling the state out
	// inside the label as well produced a row too long to read and left the PID,
	// which is what an operator actually wants, cut off at the edge.
	state := i18n.T(lang, "status.offline")
	value := ""
	if pid.Found {
		state = ""
		value = fmt.Sprintf("PID %d", pid.PID)
	}
	if a.NeedsLogin() {
		// The block's label says what the operator has to act on, which for a
		// gateway with no accounts is not the pid but the missing login.
		value = i18n.T(lang, "gw.no_accounts_short")
	}
	health := status.HealthDown
	if pid.Found {
		health = status.HealthOK
	}
	label := i18n.T(lang, "menu.gateway")
	if state != "" {
		label += " · " + state
	}
	row := traymenu.Submenu(label, children)
	row.Value = value
	row.Dot = colorOf(th, int(health))
	return row
}

// consoleLabel says what the console switch will do, which depends on whether a
// console window exists right now.
func consoleLabel(a *App, lang string) string {
	if a.gateway.HasConsole() {
		return i18n.T(lang, "gw.console_hide")
	}
	return i18n.T(lang, "gw.console_show")
}

// taskItems is the gateway's maintenance block.
// updateItems is the version block: what is installed, what is available, and the
// one action that resolves the difference.
//
// The rows are built from the last check rather than from a fresh one, so opening
// the menu never waits on the network. A check that has not run yet says so, and
// the row that re-runs it sits right there.
func (a *App) updateItems(lang string) traymenu.Item {
	u := a.Updates()
	installedGW := a.GatewayVersion()
	installedTray := a.TrayVersion()

	children := []traymenu.Item{}

	// The gateway's row: an action when there is something to do, a statement
	// when there is not.
	switch {
	case u.Checked.IsZero():
		children = append(children, traymenu.Value(i18n.T(lang, "menu.gateway_version"),
			i18n.T(lang, "menu.update_checking")))
	case u.GatewayUpdate:
		children = append(children, traymenu.Command(IDUpdateGateway,
			i18n.T(lang, "menu.update_gateway", u.Gateway)))
	default:
		children = append(children, traymenu.Value(i18n.T(lang, "menu.gateway_version"),
			i18n.T(lang, "menu.no_update")))
	}
	if installedGW != "" {
		children = append(children, traymenu.Value("", i18n.T(lang, "menu.installed_ver", installedGW)))
	}

	// The tray's own row. Updating itself restarts the tray, which is why it is
	// offered as its own command rather than folded in with the gateway's.
	if u.TrayUpdate {
		children = append(children, traymenu.Command(IDUpdateTray,
			i18n.T(lang, "menu.update_tray", u.Tray)))
	}
	children = append(children,
		traymenu.Separator(),
		traymenu.Value(i18n.T(lang, "menu.tray_version"), installedTray),
		traymenu.Command(IDCheckUpdates, i18n.T(lang, "menu.updates")),
	)

	// An unreachable check is stated in the row rather than hidden: an operator
	// who wonders why no update is offered deserves to know the check failed.
	label := i18n.T(lang, "menu.version_block")
	if u.Err != nil {
		label = i18n.T(lang, "menu.version_block_failed")
	}
	return traymenu.Submenu(label, children)
}

// taskItems is the gateway's maintenance block.
func (a *App) taskItems(lang string) traymenu.Item {
	children := make([]traymenu.Item, 0, len(Tasks))
	for i, t := range Tasks {
		children = append(children, traymenu.Command(uint32(IDTaskBase+i), i18n.T(lang, t.Key)))
	}
	return traymenu.Submenu(i18n.T(lang, "menu.tasks"), children)
}

// healthOf classifies the snapshot, counting a paused tray as neither well nor
// broken: its numbers are simply not being refreshed.
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

// hostOf strips the scheme from a base URL, which is the part an operator
// recognises at a glance and the part that fits in a menu's value column.
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
