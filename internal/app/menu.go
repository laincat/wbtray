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
// The top level is deliberately nine rows and no more. A tray menu is opened to do
// one of a few things — look at the state, open the console, start or stop the
// gateway, change how the tray looks — and everything else is a click away inside a
// submenu. The earlier menu put every figure, every style and every maintenance
// action within reach of one right-click, and the rows worth reaching for were lost
// among them.
//
// Every label is two characters in Chinese and one short word in English, so the
// column reads as a list rather than as a paragraph. That is a constraint worth
// keeping rather than a coincidence: a long label beside a short one makes the
// short one look like a different kind of thing.

// previewSize is the pixel size of a style row's preview: the same size the
// taskbar uses, so the gallery shows the real thing rather than an approximation.
const previewSize = 20

// Menu builds the model for the front end.
func (a *App) Menu() []traymenu.Item {
	a.mu.Lock()
	snap, paused, cfg := a.snap, a.paused, a.cfg
	a.mu.Unlock()

	lang := cfg.Lang
	th := a.Theme()
	health := healthOf(snap, paused)

	// The two status rows. They are the only rows the front end paints, because
	// they are the only rows whose colour is information: the system dims a row it
	// cannot click, and it dims the row's icon with it.
	//
	// The header is the state and the address, and deliberately not the program's
	// own name: this is wbtray's menu, so writing "wbtray" in it is a word that
	// tells the reader nothing and makes the header the widest row — and a menu is
	// as wide as its widest row.
	items := []traymenu.Item{
		traymenu.Status(
			healthLabel(lang, health),
			hostOf(cfg.BaseURL), colorOf(th, health), true),
	}
	items = append(items, a.statusRow(snap, paused, cfg, th, lang))
	items = append(items, traymenu.Separator())

	// The console is the one row worth a keyboard shortcut, so it is the menu's
	// default item: Enter reaches it without moving to it.
	panel := traymenu.Command(IDOpenPanel, i18n.T(lang, "menu.panel"))
	panel.Bold = true
	items = append(items, panel)

	items = append(items,
		a.gatewayItems(snap, cfg, lang, th),
		a.trayItems(cfg, th, lang),
		a.moreItems(snap, paused, cfg, lang),
		traymenu.Separator(),
		traymenu.Check(IDTrayAutoStart, i18n.T(lang, "menu.autostart"), a.TrayAutoStart()),
		traymenu.Command(IDQuit, i18n.T(lang, "menu.exit")),
	)
	return items
}

// statusRow is the second line of the header: what the pool is holding, or the one
// sentence that says why there is nothing to hold.
func (a *App) statusRow(snap status.Snapshot, paused bool, cfg config.Config, th theme.Theme, lang string) traymenu.Item {
	dot := colorOf(th, healthOf(snap, paused))
	switch {
	case errors.Is(snap.Err, panel.ErrUnauthorized):
		// A rejected key is not an unreachable gateway, and saying it was would
		// send an operator to look at the wrong thing: the gateway is answering,
		// it simply will not answer this caller.
		return traymenu.Status(i18n.T(lang, "status.unauthorized"), "", dot, false)
	case !snap.Reachable:
		text := i18n.T(lang, "status.unreachable", hostOf(cfg.BaseURL))
		if paused {
			text = i18n.T(lang, "health.paused")
		}
		return traymenu.Status(text, "", dot, false)
	case snap.Total == 0:
		return traymenu.Status(i18n.T(lang, "menu.accounts"), i18n.T(lang, "status.no_accounts"), dot, false)
	default:
		// Ready over total, then the credits behind them: the pair that answers
		// "can this thing serve a request", and the figure that answers "for how
		// much longer".
		return traymenu.Status(
			i18n.T(lang, "menu.accounts"),
			fmt.Sprintf("%d/%d · %s", snap.Ready(), snap.Total, i18n.Num(snap.CreditTotal())),
			dot, false)
	}
}

// copyAddrRow copies the gateway address.
//
// The value shown is the host, which is the part that fits a menu column; what is
// copied is the full address with its scheme, because that is what pastes into a
// browser or a client.
func (a *App) copyAddrRow(lang string) traymenu.Item {
	row := traymenu.Command(IDCopyURL, i18n.T(lang, "copy.addr"))
	row.Value = hostOf(a.BaseURL())
	return row
}

// copyKeyRow copies the gateway API key.
//
// The key itself is never drawn into the menu — a screenshot of an open menu should
// not be a credential — so the row is a verb and the balloon is the only
// confirmation. With no key to copy the row is dimmed, which is more useful than a
// row that reports success at copying an empty string.
func (a *App) copyKeyRow(lang string) traymenu.Item {
	row := traymenu.Command(IDCopyKey, i18n.T(lang, "copy.key"))
	if a.PanelClient().Key() == "" {
		row.Disabled = true
		row.Value = i18n.T(lang, "copy.none")
	}
	return row
}

// trayItems is everything about how the tray itself looks and behaves.
func (a *App) trayItems(cfg config.Config, th theme.Theme, lang string) traymenu.Item {
	children := []traymenu.Item{
		traymenu.Submenu(i18n.T(lang, "menu.style"), a.styleItems(cfg, th, lang)),
		traymenu.Submenu(i18n.T(lang, "menu.metric"), metricItems(cfg, lang)),
		traymenu.Submenu(i18n.T(lang, "menu.theme"), themeItems(a, cfg, lang)),
		traymenu.Submenu(i18n.T(lang, "menu.language"), []traymenu.Item{
			// The two names are the language's own, which is the one place a
			// translated label would be useless: a reader who cannot read the
			// current language is looking for their own.
			traymenu.Radio(IDLangBase, "中文", cfg.Lang == "zh"),
			traymenu.Radio(IDLangBase+1, "EN", cfg.Lang == "en"),
		}),
		traymenu.Separator(),
		traymenu.Command(IDOpenConfig, i18n.T(lang, "menu.file")),
		traymenu.Command(IDOpenFolder, i18n.T(lang, "menu.dir")),
		traymenu.Command(IDReload, i18n.T(lang, "menu.reload")),
	}
	return traymenu.Submenu(i18n.T(lang, "menu.tray"), children)
}

// moreItems is everything that is a readout or a one-shot action.
//
// These are the rows that used to fill the top level. None of them is gone; they are
// one submenu down, where they can be as detailed as they are useful without pushing
// the rows an operator reaches for off the screen.
func (a *App) moreItems(snap status.Snapshot, paused bool, cfg config.Config, lang string) traymenu.Item {
	children := []traymenu.Item{
		a.accountItems(snap, lang),
		traymenu.Submenu(i18n.T(lang, "menu.data"), a.liveItems(snap, paused, cfg)),
		a.updateItems(lang),
		a.taskItems(lang),
		traymenu.Separator(),
		traymenu.Command(IDChartWindow, i18n.T(lang, "menu.chart")),
		traymenu.Command(IDRefresh, i18n.T(lang, "menu.refresh")),
	}
	if paused {
		children = append(children, traymenu.Command(IDPause, i18n.T(lang, "menu.resume")))
	} else {
		children = append(children, traymenu.Command(IDPause, i18n.T(lang, "menu.pause")))
	}
	children = append(children,
		traymenu.Separator(),
		traymenu.Command(IDAbout, i18n.T(lang, "menu.about")),
	)
	return traymenu.Submenu(i18n.T(lang, "menu.more"), children)
}

// liveItems is the block of current numbers. Each row is one metric, which is what
// makes the menu a complete readout rather than a summary: whichever one the
// operator put in the icon, the rest are one right-click away.
func (a *App) liveItems(snap status.Snapshot, paused bool, cfg config.Config) []traymenu.Item {
	lang := cfg.Lang
	if errors.Is(snap.Err, panel.ErrUnauthorized) {
		return []traymenu.Item{traymenu.Value("", i18n.T(lang, "status.unauthorized"))}
	}
	if !snap.Reachable {
		// One full-width sentence rather than a label and a value: the message is
		// the whole row's content, and squeezing it into a value column would clip
		// exactly the part that says which address failed.
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
	// The window is the same one the chart uses, so the figures here and the curve
	// there describe the same period.
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
	if snap.LogTotal > 0 {
		items = append(items, traymenu.Value(i18n.T(lang, "menu.logs"),
			i18n.T(lang, "menu.logs_value", snap.LogTotal, snap.LogErrors)))
	}
	if snap.Version != "" {
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
			// The remaining time is worked out from the wall clock the gateway
			// reports, because it does not report a duration.
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
			Disabled: true,
			Dot:      accountColor(acct),
		})
	}
	if len(children) == 0 {
		children = append(children, traymenu.Value(i18n.T(lang, "status.no_accounts"), ""))
	}
	row := traymenu.Submenu(i18n.T(lang, "menu.accounts"), children)
	row.Value = fmt.Sprintf("%d/%d", snap.Ready(), snap.Total)
	return row
}

// accountColor is the pip beside an account row: green when it can serve, amber
// when it is cooling, blue when it is busy, grey when it is out of play.
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
	// Every style is offered on every palette. The two palettes differ in ink, not
	// in what they can draw, and a gallery with holes in it is a puzzle rather than
	// a choice.
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

// gatewayItems is the process block: what is running, and the things that can be
// done about it.
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
	// The clipboard rows go with the gateway because both of the values they copy
	// are the gateway's: its address is where it listens, and its key is what it
	// expects a client to present. An operator looking for either looks here.
	children = append(children,
		traymenu.Separator(),
		a.copyAddrRow(lang),
		a.copyKeyRow(lang),
		traymenu.Separator(),
		traymenu.Check(IDGatewayAutoStart, i18n.T(lang, "menu.autostart"), a.gateway.AutoStartEnabled()),
		traymenu.Command(IDOpenGatewayDir, i18n.T(lang, "menu.dir")),
		traymenu.Command(IDOpenGatewayConfig, i18n.T(lang, "menu.file")),
	)

	// The block's own label carries the state, and the row's value column carries
	// the identifier, so neither has to repeat the other. Spelling the state out
	// inside the label as well produced a row too long to read and left the pid,
	// which is what an operator actually wants, cut off at the edge.
	state := i18n.T(lang, "status.offline")
	value := ""
	if pid.Found {
		state = ""
		value = fmt.Sprintf("PID %d", pid.PID)
	}
	if a.NeedsLogin() {
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

// updateItems is the version block: what is installed, what is available, and the
// one action that resolves the difference.
//
// The rows are built from the last check rather than from a fresh one, so opening
// the menu never waits on the network. A check that has not run yet says so, and the
// row that re-runs it sits right there.
func (a *App) updateItems(lang string) traymenu.Item {
	u := a.Updates()
	installedGW := a.GatewayVersion()
	installedTray := a.TrayVersion()

	children := []traymenu.Item{}

	// The gateway's row: an action when there is something to do, a statement when
	// there is not.
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

	// An unreachable check is stated in the row rather than hidden: an operator who
	// wonders why no update is offered deserves to know the check failed.
	// A failed check is stated in the value column rather than in the label: the
	// label stays two characters, and the failure is still visible without opening
	// the block.
	row := traymenu.Submenu(i18n.T(lang, "menu.version_block"), children)
	if u.Err != nil {
		row.Value = i18n.T(lang, "menu.check_failed")
	}
	return row
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
