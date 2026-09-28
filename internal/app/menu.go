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
// It is rebuilt from live state every time it opens, which is what lets it show
// current numbers rather than the ones that were true when the tray started.
//
// The shape is three blocks separated by rules, and the rule behind it is that no
// label may appear twice. An earlier version listed every scheduled task once as a
// switch and again as an action, under the same name — so "签到" appeared twice in
// one submenu meaning two different things, and the reader had no way to tell which
// was which. A name is now used for exactly one row in one place.
//
// Every row also carries a sentence, shown while the pointer is on it. A menu row
// can say what it is called; it cannot say what it does.

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

	return []traymenu.Item{
		a.stateRow(snap, paused, cfg, th, lang),
		traymenu.Separator(),

		// The four things the gateway is, in the order an operator thinks about
		// them: the console itself, then the process behind it, then what it is
		// serving and what it runs.
		traymenu.Command(IDOpenPanel, i18n.T(lang, "menu.panel"), i18n.T(lang, "hint.panel")).
			WithDefault(),
		a.gatewayRow(snap, cfg, lang, th),
		a.accountsRow(snap, lang),
		a.tasksRow(lang),
		a.settingsRow(cfg, th, lang),
		traymenu.Separator(),

		// The tray's own two switches. They are here rather than in the settings
		// block because they are the pair an operator reaches for when the numbers
		// look wrong, and a submenu between them and the reading would be in the
		// way.
		traymenu.Command(IDRefresh, i18n.T(lang, "menu.refresh"), i18n.T(lang, "hint.refresh")),
		a.pauseRow(paused, lang),
		traymenu.Separator(),

		traymenu.Command(IDQuit, i18n.T(lang, "menu.exit"), i18n.T(lang, "hint.exit")),
	}
}

// stateRow is the single readout at the top: a colour, the account the gateway is
// serving with, and its balance.
//
// One row, not two. The colour answers "is the gateway serving", the name answers
// "with whom", and the figure answers "for how much longer"; a second row of
// labels would repeat all three in words.
func (a *App) stateRow(snap status.Snapshot, paused bool, cfg config.Config, th theme.Palette, lang string) traymenu.Item {
	dot := th.State(int(healthOf(snap, paused)))

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
		// The account the gateway is answering with, rather than a word like
		// "credits": a pool has several accounts, and which one is serving is the
		// question worth answering at a glance.
		if acct, ok := snap.Current(); ok {
			name := acct.Nickname
			if name == "" {
				name = acct.UID
			}
			text = name
			value = i18n.Num(acct.Credits)
			hint = i18n.T(lang, "hint.state_account", name, snap.Ready(), snap.Total, snap.InFlight())
		} else {
			text = i18n.T(lang, "state.accounts")
			value = fmt.Sprintf("%d/%d", snap.Ready(), snap.Total)
			hint = i18n.T(lang, "hint.state", snap.Ready(), snap.Total, snap.InFlight())
		}
	}
	return traymenu.Status(text, value, hint, dot, true)
}

// pauseRow is the pause switch, named for what it will do.
func (a *App) pauseRow(paused bool, lang string) traymenu.Item {
	if paused {
		return traymenu.Command(IDPause, i18n.T(lang, "menu.resume"), i18n.T(lang, "hint.resume"))
	}
	return traymenu.Command(IDPause, i18n.T(lang, "menu.pause"), i18n.T(lang, "hint.pause"))
}

// gatewayRow is the process: what it is, and the four things that can be done to it.
func (a *App) gatewayRow(snap status.Snapshot, cfg config.Config, lang string, th theme.Palette) traymenu.Item {
	pid := snap.Process
	children := []traymenu.Item{}

	if pid.Found {
		children = append(children,
			traymenu.ValueHint(i18n.T(lang, "menu.pid"), fmt.Sprint(pid.PID), i18n.T(lang, "hint.pid")),
			traymenu.Command(IDGatewayRestart, i18n.T(lang, "gw.restart"), i18n.T(lang, "hint.gw_restart")),
			traymenu.Command(IDConsoleToggle, consoleLabel(a, lang), i18n.T(lang, "hint.console")),
			traymenu.Command(IDGatewayStop, i18n.T(lang, "gw.stop"), i18n.T(lang, "hint.gw_stop")),
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
		)
		if !a.GatewayInstalled() {
			children = append(children, traymenu.Command(IDInstallGateway,
				i18n.T(lang, "menu.install"), i18n.T(lang, "hint.install")))
		}
	}

	// The clipboard pair, the autostart switch and the two files. They are the
	// gateway's, not the tray's, which is why they are here and not in Settings.
	children = append(children,
		traymenu.Separator(),
		a.copyAddrRow(lang),
		a.copyKeyRow(lang),
		traymenu.Separator(),
		traymenu.Check(IDGatewayAutoStart, i18n.T(lang, "menu.autostart"),
			i18n.T(lang, "hint.gw_autostart"), a.gateway.AutoStartEnabled()),
		traymenu.Command(IDOpenGatewayDir, i18n.T(lang, "menu.dir"), i18n.T(lang, "hint.gw_dir")),
		traymenu.Command(IDOpenGatewayConfig, i18n.T(lang, "menu.config"), i18n.T(lang, "hint.gw_file")),
	)

	// The row's own label carries the state when the process is not there, and its
	// value column carries the identifier when it is. Neither repeats the other.
	label := i18n.T(lang, "menu.gateway")
	value := ""
	health := status.HealthDown
	if pid.Found {
		value = fmt.Sprintf("PID %d", pid.PID)
		health = status.HealthOK
	} else {
		label += " · " + i18n.T(lang, "state.offline")
	}
	if a.NeedsLogin() {
		value = i18n.T(lang, "gw.no_accounts_short")
	}

	row := traymenu.Submenu(label, i18n.T(lang, "hint.gateway"), children)
	row.Value = value
	row.Dot = th.State(int(health))
	return row
}

// accountsRow is the pool, one row per account.
func (a *App) accountsRow(snap status.Snapshot, lang string) traymenu.Item {
	const maxRows = 16
	children := make([]traymenu.Item, 0, maxRows)
	for i, acct := range snap.Accounts {
		if i >= maxRows {
			children = append(children, traymenu.Value(
				i18n.T(lang, "menu.more_accounts", len(snap.Accounts)-i), ""))
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
			Dot:      a.accountState(acct),
		})
	}
	if len(children) == 0 {
		children = append(children, traymenu.Value(i18n.T(lang, "state.noaccounts"), ""))
	}
	row := traymenu.Submenu(i18n.T(lang, "menu.accounts"), i18n.T(lang, "hint.accounts"), children)
	row.Value = fmt.Sprintf("%d/%d", snap.Ready(), snap.Total)
	return row
}

// accountState is the pip beside an account row.
//
// It asks the palette, like everything else does. An earlier version carried four
// hex values of its own, so the pips were the one part of the menu that ignored
// the chosen palette — a green that appeared in the monochrome scheme, and a blue
// that appeared under an operator whose accent was orange, which is exactly the
// kind of second colour scheme this design is meant to prevent.
func (a *App) accountState(acct status.Account) raster.RGBA {
	th := a.Theme()
	switch {
	case acct.Disabled:
		return th.InkDim
	case acct.Cooling:
		return th.Warn
	case acct.InFlight > 0:
		return th.Accent
	default:
		return th.OK
	}
}

// tasksRow is what the gateway runs, one row per task.
//
// One row per task, and no second list. The tick says the gateway runs it on a
// schedule; the click runs it once, now. An earlier version had both as separate
// rows under the same name, which read as a stutter and left the reader guessing
// which "签到" was the switch.
func (a *App) tasksRow(lang string) traymenu.Item {
	sched := a.Schedule()
	on := map[string]bool{
		"task.checkin":   sched.Checkin,
		"task.travel":    sched.Travel,
		"task.activity":  sched.Activity,
		"task.keepalive": sched.Keepalive,
		"task.balance":   sched.BalanceRefresh,
	}

	children := make([]traymenu.Item, 0, len(Tasks))
	enabled := 0
	for i, t := range Tasks {
		row := traymenu.Command(uint32(IDTaskBase+i), i18n.T(lang, t.Key), i18n.T(lang, t.Hint))
		if scheduled, ok := on[t.Key]; ok {
			row.Checked = scheduled
			// The hint is the only place that can say both things at once, so a
			// row carrying a tick says what the tick means as well as what a
			// click does.
			if scheduled {
				enabled++
				row.Hint = i18n.T(lang, "hint.task_scheduled")
			}
		}
		children = append(children, row)
	}

	row := traymenu.Submenu(i18n.T(lang, "menu.tasks"), i18n.T(lang, "hint.tasks"), children)
	if !sched.Known {
		// A gateway that has not been asked yet says so, rather than showing five
		// unticked rows that mean "off" when they mean "unknown".
		row.Value = i18n.T(lang, "menu.unknown")
	} else {
		row.Value = fmt.Sprintf("%d/%d", enabled, len(on))
	}
	return row
}

// settingsRow is how the tray itself looks and behaves.
func (a *App) settingsRow(cfg config.Config, th theme.Palette, lang string) traymenu.Item {
	children := []traymenu.Item{
		traymenu.Submenu(i18n.T(lang, "menu.icon"), i18n.T(lang, "hint.icon"),
			a.styleItems(cfg, th, lang)),
		traymenu.Submenu(i18n.T(lang, "menu.palette"), i18n.T(lang, "hint.palette"),
			a.paletteItems(cfg, lang)),
		traymenu.Submenu(i18n.T(lang, "menu.language"), i18n.T(lang, "hint.language"),
			[]traymenu.Item{
				traymenu.Radio(IDLangBase, "中文", i18n.T(lang, "hint.lang_zh"), cfg.Lang == "zh"),
				traymenu.Radio(IDLangBase+1, "EN", i18n.T(lang, "hint.lang_en"), cfg.Lang == "en"),
			}),
		traymenu.Submenu(i18n.T(lang, "menu.metric"), i18n.T(lang, "hint.metric_block"),
			metricItems(cfg, lang)),
		traymenu.Submenu(i18n.T(lang, "menu.appicon"), i18n.T(lang, "hint.appicon"),
			appIconItems(cfg, lang)),
		traymenu.Separator(),
		traymenu.Check(IDTrayAutoStart, i18n.T(lang, "menu.autostart"),
			i18n.T(lang, "hint.tray_autostart"), a.TrayAutoStart()),
		traymenu.Command(IDOpenFolder, i18n.T(lang, "menu.dir"), i18n.T(lang, "hint.tray_dir")),
		traymenu.Command(IDOpenConfig, i18n.T(lang, "menu.config"), i18n.T(lang, "hint.tray_file")),
		traymenu.Command(IDReload, i18n.T(lang, "menu.reload"), i18n.T(lang, "hint.reload")),
		traymenu.Separator(),
		a.updateItems(lang),
		traymenu.Command(IDAbout, i18n.T(lang, "menu.about"), i18n.T(lang, "hint.about")),
	}
	return traymenu.Submenu(i18n.T(lang, "menu.settings"), i18n.T(lang, "hint.settings"), children)
}

// copyAddrRow copies the gateway address.
func (a *App) copyAddrRow(lang string) traymenu.Item {
	row := traymenu.Command(IDCopyURL, i18n.T(lang, "copy.addr"), i18n.T(lang, "hint.copy_addr"))
	row.Value = hostOf(a.BaseURL())
	return row
}

// copyKeyRow copies the gateway API key.
//
// The key is never drawn into the menu — a screenshot of an open menu should not be
// a credential — so the row is a verb and the balloon is the only confirmation.
func (a *App) copyKeyRow(lang string) traymenu.Item {
	row := traymenu.Command(IDCopyKey, i18n.T(lang, "copy.key"), i18n.T(lang, "hint.copy_key"))
	if a.PanelClient().Key() == "" {
		row.Disabled = true
		row.Value = i18n.T(lang, "copy.none")
		row.Hint = i18n.T(lang, "hint.copy_key_none")
	}
	return row
}

// styleItems is the gallery: one row per style, each carrying a rendering of the
// icon it would produce with the current metric, palette and numbers.
func (a *App) styleItems(cfg config.Config, th theme.Palette, lang string) []traymenu.Item {
	out := make([]traymenu.Item, 0, len(config.Styles))
	for i, name := range config.Styles {
		out = append(out, traymenu.Item{
			Kind:    traymenu.StyleRow,
			ID:      uint32(IDStyleBase + i),
			Text:    i18n.T(lang, "style."+name),
			Hint:    i18n.T(lang, "hint.style_pick", i18n.T(lang, "style."+name)),
			Checked: cfg.Style == name,
			Preview: a.previewFor(name, th),
		})
	}
	return out
}

// previewFor renders one style with the live numbers.
func (a *App) previewFor(style string, th theme.Palette) *raster.Canvas {
	a.mu.Lock()
	snap, metric, lang, paused := a.snap, a.cfg.Metric, a.cfg.Lang, a.paused
	a.mu.Unlock()
	return iconstyle.Draw(iconstyle.View{
		Size:    previewSize,
		Style:   style,
		Metric:  metric,
		Lang:    lang,
		Palette: th,
		Snap:    snap,
		Paused:  paused,
	})
}

// paletteItems is the tone list.
//
// There used to be a palette choice above this — one that wore the Windows accent
// and one that did not — and it is gone, because wearing the accent made every mark
// in the tray the operator's chosen colour whatever that colour was. What is left
// is the question that was always underneath it: which way round the design is
// drawn, since an ink that reads on a dark taskbar is invisible on a light one.
func (a *App) paletteItems(cfg config.Config, lang string) []traymenu.Item {
	// The palettes, then the tone they are worn in. Two questions, because they are
	// two: the palette is which neutral, and the tone is which way round it is.
	out := make([]traymenu.Item, 0, len(theme.Names)+len(theme.Appearances)+1)
	for i, name := range theme.Names {
		out = append(out, traymenu.Radio(uint32(IDThemeBase+i),
			theme.VariantLabel(name, lang), i18n.T(lang, "hint.theme_pick"),
			cfg.Theme == name))
	}
	out = append(out, traymenu.Separator())
	for i, ap := range theme.Appearances {
		out = append(out, traymenu.Radio(uint32(IDAppearanceBase+i),
			theme.AppearanceLabel(ap, lang), i18n.T(lang, "hint.appearance"),
			theme.ParseAppearance(cfg.Appearance) == ap))
	}
	return out
}

// metricItems is which figure the icon draws.
func metricItems(cfg config.Config, lang string) []traymenu.Item {
	hint := i18n.T(lang, "hint.metric")
	out := make([]traymenu.Item, 0, len(config.Metrics))
	for i, m := range config.Metrics {
		out = append(out, traymenu.Radio(uint32(IDMetricBase+i), i18n.T(lang, "metric."+m),
			hint, cfg.Metric == m))
	}
	return out
}

// appIconItems is which mark the executable carries.
//
// The choice is a build-time one — the icon is a PE resource — so this records the
// preference and the next build picks it up. The hint says so, because a menu that
// appears to change something and does not is worse than one that explains why it
// cannot yet.
func appIconItems(cfg config.Config, lang string) []traymenu.Item {
	out := make([]traymenu.Item, 0, len(config.AppIcons))
	for i, name := range config.AppIcons {
		out = append(out, traymenu.Radio(uint32(IDAppIconBase+i),
			i18n.T(lang, "appicon."+name), i18n.T(lang, "hint.appicon_pick"),
			cfg.AppIcon == name))
	}
	return out
}

// updateItems is the version block: what is installed, what is available, and the
// one action that resolves the difference.
func (a *App) updateItems(lang string) traymenu.Item {
	u := a.Updates()
	installedGW := a.GatewayVersion()
	installedTray := a.TrayVersion()

	children := []traymenu.Item{
		traymenu.Value(i18n.T(lang, "menu.gw_version"), orDash(installedGW)),
		traymenu.Value(i18n.T(lang, "menu.tray_version"), installedTray),
	}
	if u.GatewayUpdate {
		children = append(children, traymenu.Command(IDUpdateGateway,
			i18n.T(lang, "menu.update_gateway", u.Gateway), i18n.T(lang, "hint.update_gateway")))
	}
	if u.TrayUpdate {
		children = append(children, traymenu.Command(IDUpdateTray,
			i18n.T(lang, "menu.update_tray", u.Tray), i18n.T(lang, "hint.update_tray")))
	}
	children = append(children, traymenu.Command(IDCheckUpdates,
		i18n.T(lang, "menu.check_updates"), i18n.T(lang, "hint.check_updates")))

	row := traymenu.Submenu(i18n.T(lang, "menu.updates"), i18n.T(lang, "hint.version"), children)
	switch {
	case u.Err != nil:
		row.Value = i18n.T(lang, "menu.check_failed")
	case u.Checked.IsZero():
		row.Value = i18n.T(lang, "menu.not_checked")
	case u.GatewayUpdate || u.TrayUpdate:
		row.Value = i18n.T(lang, "menu.update_ready")
	default:
		row.Value = i18n.T(lang, "menu.up_to_date")
	}
	return row
}

// healthOf classifies the snapshot, counting a paused tray as neither well nor
// broken: its numbers are simply not being refreshed.
func healthOf(snap status.Snapshot, paused bool) int {
	if paused && !snap.Reachable {
		return int(status.HealthWarn)
	}
	return int(snap.Health())
}

// hostOf strips the scheme from a base URL, which is the part an operator
// recognises at a glance and the part that fits a menu's value column.
func hostOf(base string) string {
	return strings.TrimPrefix(strings.TrimPrefix(base, "http://"), "https://")
}

// orDash renders an unknown version, rather than leaving the row blank: a blank
// value column reads as a rendering fault, where a dash reads as "not known".
func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// consoleLabel says what the console switch will do, which depends on whether a
// console window exists right now.
func consoleLabel(a *App, lang string) string {
	if a.gateway.HasConsole() {
		return i18n.T(lang, "gw.console_hide")
	}
	return i18n.T(lang, "gw.console_show")
}
