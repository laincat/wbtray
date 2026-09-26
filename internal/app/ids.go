package app

// Command identifiers for the menu rows the application owns.
//
// They start well above the front end's own ids, which occupy the low
// thousands, so a click can be routed by range without a lookup table.
const (
	// IDStyleBase plus the index of a style in config.Styles.
	IDStyleBase = 2000
	// IDMetricBase plus the index of a metric in config.Metrics.
	IDMetricBase = 2100
	// IDThemeBase plus the index of a theme in theme.All.
	IDThemeBase = 2200
	// IDLangBase plus 0 for Chinese and 1 for English.
	IDLangBase = 2300
	// IDAppearanceBase plus the index of a mode in theme.Appearances.
	IDAppearanceBase = 2350
	// IDAccountBase plus the index of an account row.
	IDAccountBase = 2400
	// IDTaskBase plus the index of a task in Tasks.
	IDTaskBase = 2500

	// IDChartWindow opens the usage chart in its own window.
	IDChartWindow = 2600
	// IDOpenPanel opens the console in the browser.
	IDOpenPanel = 2601
	// IDCopyURL copies the gateway address to the clipboard.
	IDCopyURL = 2602
	// IDOpenConfig opens wbtray.conf.
	IDOpenConfig = 2603
	// IDReload re-reads the configuration file.
	IDReload = 2604
	// IDAbout shows the about box.
	IDAbout = 2605
	// IDClassicMenu switches to the system menu.
	IDClassicMenu = 2606
	// IDRefresh prompts an immediate refresh.
	IDRefresh = 2607
	// IDPause toggles refreshing.
	IDPause = 2608
	// IDTrayAutoStart toggles the tray's own autostart entry.
	IDTrayAutoStart = 2609
	// IDQuit exits the tray.
	IDQuit = 2610
	// IDOpenFolder opens the folder the tray runs from.
	IDOpenFolder = 2611
	// IDOpenLogs opens the gateway's log folder.
	IDOpenLogs = 2612

	// Gateway process commands.
	IDGatewayStart      = 2700
	IDGatewayStop       = 2701
	IDGatewayRestart    = 2702
	IDConsoleToggle     = 2703
	IDGatewayAutoStart  = 2704
)

// Task is one of the gateway's one-shot maintenance actions.
type Task struct {
	// Key is the message key for the row's label.
	Key string
	// Path is the panel endpoint, relative to the gateway root.
	Path string
}

// Tasks are the maintenance actions the tray can trigger, in menu order. They
// mirror the panel's own "run for every account" buttons: the tray offers the
// ones an operator reaches for while something is already wrong, not the whole
// console.
var Tasks = []Task{
	{"task.checkin", "/panel/api/checkin_all"},
	{"task.travel", "/panel/api/travel_all"},
	{"task.activity", "/panel/api/activity_all"},
	{"task.keepalive", "/panel/api/keepalive_all"},
	{"task.balance", "/panel/api/balance_all"},
	{"task.scan", "/panel/api/tasks/scan_all"},
	{"task.run_queue", "/panel/api/tasks/run_queue"},
}
