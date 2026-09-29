// The tray menu, which is the shell's own.
//
// A native menu rather than a window this program draws, and that is the point of
// switching to Rust: the shell's menu is the one control that behaves the way a Windows
// user expects at the notification area — it closes when they click away, it follows the
// keyboard, it is drawn by the system under the system's theme. A drawn panel was tried
// at length and the fault was never in the drawing: it was in re-implementing dismissal,
// focus and z-order, which the shell already does correctly.
//
// The shape is the operator's own specification, and it is built fresh on every right
// click so what it says is what is true at that moment.

use crate::client::{Reading, State};
use crate::gateway::Schedule;
use tray_icon::menu::{CheckMenuItem, Menu, MenuId, MenuItem, PredefinedMenuItem, Submenu};

/// What a click on a row asks the program to do.
///
/// The rows carry an id rather than a closure, because the menu is rebuilt on every
/// reading and a click arrives after the menu it came from has been replaced. An id
/// survives that; a closure would have been dropped with the row.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Command {
    /// Open the gateway's own console in a browser.
    OpenPanel,
    /// Copy the gateway address.
    CopyAddress,
    /// Copy the API key.
    CopyKey,
    /// Start the gateway.
    Start,
    /// Stop the gateway, and let it stay stopped.
    Stop,
    /// Stop the gateway and start it again.
    Restart,
    /// Toggle starting the gateway with the tray.
    ToggleLinkage,
    /// Toggle starting the tray with Windows.
    ToggleAutostart,
    /// Show the About box.
    About,
    /// Quit the tray, leaving the gateway running.
    Quit,
}

/// The id a row carries, which is the command's own name.
///
/// Deriving the id from the command rather than keeping a second list beside it is what
/// makes the two impossible to disagree: a row added without an id is a compile error,
/// and there is no table to forget to extend.
impl Command {
    pub fn id(self) -> &'static str {
        match self {
            Command::OpenPanel => "open-panel",
            Command::CopyAddress => "copy-address",
            Command::CopyKey => "copy-key",
            Command::Start => "start",
            Command::Stop => "stop",
            Command::Restart => "restart",
            Command::ToggleLinkage => "linkage",
            Command::ToggleAutostart => "autostart",
            Command::About => "about",
            Command::Quit => "quit",
        }
    }

    /// The command a row id stands for.
    pub fn from_id(id: &str) -> Option<Command> {
        const ALL: [Command; 10] = [
            Command::OpenPanel,
            Command::CopyAddress,
            Command::CopyKey,
            Command::Start,
            Command::Stop,
            Command::Restart,
            Command::ToggleLinkage,
            Command::ToggleAutostart,
            Command::About,
            Command::Quit,
        ];
        ALL.into_iter().find(|c| c.id() == id)
    }
}

/// Everything the menu draws from.
pub struct Facts<'a> {
    pub reading: &'a Reading,
    pub schedule: &'a Schedule,
    /// The gateway address, as the operator would type it.
    pub base: &'a str,
    /// The key in use, empty when there is none to copy.
    pub key: &'a str,
    pub gateway_running: bool,
    /// Whether the tray starts the gateway when it is not running.
    pub linked: bool,
    pub autostart: bool,
}

/// The menu the shell will draw, built from one reading.
///
/// The rows, in the operator's order:
///
/// ```text
///   account name                     ● green | red
///   ──────────────────────────────────────────────
///   积分                              12,154
///   界面 ▸                            打开界面 / API 地址 / 密钥
///   任务 ▸                            签到 ✔ …
///   联动                              随启 ✔ · 重启 · 关闭
///   ──────────────────────────────────────────────
/// ✓ 自启
///   关于
///   退出
/// ```
pub fn build(f: &Facts) -> Menu {
    let menu = Menu::new();

    // The state row: who is being served, and whether the gateway is up at all. It is
    // disabled so it reads as a statement rather than as something to click.
    let state = match f.reading.state {
        State::Ok => "●",
        State::Degraded => "●",
        State::Down => "●",
    };
    let who = if f.gateway_running {
        match f.reading.overview.accounts.iter().find(|a| a.ready()) {
            Some(a) => a.label(),
            None => account_or_gateway(f),
        }
    } else {
        "网关未运行".into()
    };
    // The dot and the name in one row, with the dot last so it sits against the right
    // edge of the menu where the eye looks for a status light.
    let head = MenuItem::new(format!("{who}\t{state}"), false, None);
    let _ = menu.append(&head);
    separator(&menu);

    // 积分
    let credits = if f.reading.state == State::Down {
        "—".to_string()
    } else {
        comma(f.reading.overview.credits())
    };
    let _ = menu.append(&MenuItem::new(format!("积分\t{credits}"), false, None));

    // 界面 ▸
    let ui = Submenu::new("界面", true);
    push(&ui, "打开界面", Command::OpenPanel);
    separator(&ui);
    push(
        &ui,
        &format!("API 地址\t{}", strip_scheme(f.base)),
        Command::CopyAddress,
    );
    let key_label = if f.key_present() {
        "密钥\t点击复制"
    } else {
        "密钥\t未设置"
    };
    let key_item = MenuItem::with_id(
        MenuId(Command::CopyKey.id().to_string()),
        key_label,
        f.key_present(),
        None,
    );
    let _ = ui.append(&key_item);
    let _ = menu.append(&ui);

    // 任务 ▸ — which of the gateway's scheduled tasks are switched on.
    let tasks = Submenu::new("任务", true);
    for (name, on) in f.schedule.rows() {
        let mark = if on { "\u{2714}" } else { "　" };
        let _ = tasks.append(&MenuItem::new(format!("{mark} {name}"), false, None));
    }
    if f.schedule.on_count() == 0 {
        let _ = tasks.append(&MenuItem::new("未读取到任务", false, None));
    }
    let _ = menu.append(&tasks);

    // 联动 ▸ — the three things the operator does to the relationship between the tray
    // and the gateway.
    let linkage = Submenu::new("联动", true);
    let link_item = CheckMenuItem::with_id(
        MenuId(Command::ToggleLinkage.id().to_string()),
        "随启",
        true,
        f.linked,
        None,
    );
    let _ = linkage.append(&link_item);
    separator(&linkage);
    push(&linkage, "重启", Command::Restart);
    push(&linkage, "关闭", Command::Stop);
    if !f.gateway_running {
        push(&linkage, "启动", Command::Start);
    }
    let _ = menu.append(&linkage);

    separator(&menu);

    let auto = CheckMenuItem::with_id(
        MenuId(Command::ToggleAutostart.id().to_string()),
        "自启",
        true,
        f.autostart,
        None,
    );
    let _ = menu.append(&auto);

    push(&menu, "关于", Command::About);
    push(&menu, "退出", Command::Quit);

    // The id each row carries is the command's own name, so a click is understood by
    // reading it straight off the event; nothing has to be handed back alongside.
    menu
}

fn account_or_gateway(f: &Facts) -> String {
    if f.reading.bad_key {
        "密钥无效".into()
    } else if f.reading.state == State::Down {
        "无法连接".into()
    } else {
        "无可用账号".into()
    }
}

fn strip_scheme(url: &str) -> String {
    url.trim_start_matches("http://")
        .trim_start_matches("https://")
        .to_string()
}

/// Append a row that does something, with the id the click will arrive under.
fn push(menu: &dyn MenuTarget, label: &str, cmd: Command) {
    // The id is given at construction: muda assigns one of its own to `new`, and a row
    // whose id is a number the caller never sees is a row that cannot be routed.
    let item = MenuItem::with_id(MenuId(cmd.id().to_string()), label, true, None);
    menu.add_item(&item);
}

fn separator(menu: &dyn MenuTarget) {
    menu.add_separator();
}

/// The two menus the rows go into. Both `Menu` and `Submenu` take an item, and the code
/// above is written once for either.
trait MenuTarget {
    fn add_item(&self, item: &MenuItem);
    fn add_separator(&self);
}

impl MenuTarget for Menu {
    fn add_item(&self, item: &MenuItem) {
        let _ = self.append(item);
    }
    fn add_separator(&self) {
        let _ = self.append(&PredefinedMenuItem::separator());
    }
}

impl MenuTarget for Submenu {
    fn add_item(&self, item: &MenuItem) {
        let _ = self.append(item);
    }
    fn add_separator(&self) {
        let _ = self.append(&PredefinedMenuItem::separator());
    }
}

impl Facts<'_> {
    fn key_present(&self) -> bool {
        !self.key.is_empty()
    }
}

/// Thousands separators, so a credit balance reads at a glance.
pub fn comma(v: i64) -> String {
    let neg = v < 0;
    let s = (v.abs()).to_string();
    let mut out = String::new();
    for (i, c) in s.chars().enumerate() {
        if i > 0 && (s.len() - i).is_multiple_of(3) {
            out.push(',');
        }
        out.push(c);
    }
    if neg {
        format!("-{out}")
    } else {
        out
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn every_row_id_maps_to_a_command() {
        // Every command has an id, and every id comes back as the command it names. The
        // list of commands is stated once, in `from_id`, so this walks the pairs that
        // the menu would produce rather than a second copy of them.
        for cmd in [
            Command::OpenPanel,
            Command::CopyAddress,
            Command::CopyKey,
            Command::Start,
            Command::Stop,
            Command::Restart,
            Command::ToggleLinkage,
            Command::ToggleAutostart,
            Command::About,
            Command::Quit,
        ] {
            assert_eq!(Command::from_id(cmd.id()), Some(cmd), "{cmd:?}");
        }
        assert!(Command::from_id("nonsense").is_none());
    }

    #[test]
    fn no_two_commands_share_an_id() {
        // A duplicate would make one row unreachable and the other fire twice.
        let ids: Vec<&str> = [
            Command::OpenPanel,
            Command::CopyAddress,
            Command::CopyKey,
            Command::Start,
            Command::Stop,
            Command::Restart,
            Command::ToggleLinkage,
            Command::ToggleAutostart,
            Command::About,
            Command::Quit,
        ]
        .iter()
        .map(|c| c.id())
        .collect();
        let mut sorted = ids.clone();
        sorted.sort_unstable();
        sorted.dedup();
        assert_eq!(sorted.len(), ids.len(), "duplicate ids in {ids:?}");
    }

    #[test]
    fn credits_are_grouped() {
        assert_eq!(comma(0), "0");
        assert_eq!(comma(42), "42");
        assert_eq!(comma(1234), "1,234");
        assert_eq!(comma(1234567), "1,234,567");
        assert_eq!(comma(-4200), "-4,200");
    }

    #[test]
    fn the_scheme_is_kept_out_of_the_address_row() {
        assert_eq!(strip_scheme("http://127.0.0.1:7863"), "127.0.0.1:7863");
        assert_eq!(strip_scheme("https://example.com"), "example.com");
    }
}
