// wbtray: a tray for the workbuddy2api gateway.
//
// It owns three things and no more. It starts the gateway when none is running, and
// leaves it alone when one is — including on the way out, where quitting the tray
// deliberately does not stop the gateway, because the gateway is what is serving
// requests and the tray is only a way to look at it. It shows the pool's state in the
// notification area. And it offers the handful of things an operator reaches for
// without opening a browser: the state, the credits, the address and the key, the
// scheduled tasks, and starting, stopping and restarting the gateway.
//
// The menu is the shell's own and the icon is drawn rather than loaded, so the program
// has no assets and installs by being copied.
//
// Threading: the notification icon belongs to the thread that created it, so that is the
// main thread and it is the one that rebuilds the menu and swaps the icon. The polling
// happens on a second thread and arrives as one message per reading; nothing else touches
// the icon.

#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod autostart;
mod cat;
mod client;
mod clipboard;
mod dialog;
mod gateway;
mod menu;
mod settings;
mod theme;

use client::{Client, Reading, State};
use gateway::{Layout, Schedule};
use std::sync::mpsc::{channel, RecvTimeoutError};
use std::sync::{Arc, Mutex};
use std::time::Duration;
use tray_icon::menu::MenuEvent;
use tray_icon::{Icon, TrayIconBuilder};

/// The version of this program, from Cargo.toml.
const VERSION: &str = env!("CARGO_PKG_VERSION");

/// How often the gateway is read.
const POLL: Duration = Duration::from_secs(5);

/// How long one reading is given before it is called a failure.
const TIMEOUT: Duration = Duration::from_secs(6);

/// The tray icon's size. Windows draws the notification icon from the small-icon metric,
/// which is 16 at 100% scaling and larger above it; 32 is the size the shell scales from
/// for every one of those, and drawing it directly would mean asking for the metric and
/// rebuilding the icon whenever it changed.
const ICON: u32 = 32;

/// Everything the tray knows, shared between the two threads.
struct Shared {
    reading: Reading,
    schedule: Schedule,
    /// The pid of the gateway this tray started, or zero.
    owned_pid: u32,
    /// The pid of a gateway it did not start, when one is running.
    found_pid: u32,
    settings: settings::Settings,
    /// Set when the operator asked for the gateway to stay down, so the linkage does not
    /// bring it straight back and make the menu row look like it did nothing.
    stopped_by_operator: bool,
    /// Whether the tray's own Run entry exists, cached because reading the registry on
    /// every menu rebuild is work for a value that changes only when this program
    /// changes it.
    autostart: bool,
}

impl Shared {
    fn gateway_running(&self) -> bool {
        gateway::alive(self.owned_pid) || gateway::alive(self.found_pid)
    }

    fn gateway_pid(&self) -> u32 {
        if gateway::alive(self.owned_pid) {
            self.owned_pid
        } else {
            self.found_pid
        }
    }
}

fn main() {
    let layout = Layout::discover();
    let settings_path = settings::path_beside_exe(&layout.root);
    let mut settings = settings::Settings::load(&settings_path);

    // The gateway's own configuration supplies the key and the port, which is what makes
    // a first run work without copying a key out of a file by hand. An explicit setting in
    // the tray's own file wins, so pointing it at a remote gateway stays one line.
    let remote = gateway::read_config(&layout).unwrap_or_default();
    if settings.api_key.is_empty() {
        settings.api_key = remote.api_key.clone();
    }
    if settings.base_url == settings::Settings::default().base_url {
        if let Some(port) = gateway::port_of(&remote.listen) {
            settings.base_url = format!("http://127.0.0.1:{port}");
        }
    }
    // Written back only when there was no file, so an operator's own edits are never
    // overwritten by a value the tray discovered.
    if !settings_path.exists() {
        let _ = settings.save(&settings_path);
    }

    let autostart_on = autostart::enabled();
    let shared = Arc::new(Mutex::new(Shared {
        reading: Reading {
            state: State::Down,
            overview: Default::default(),
            error: Some("尚未读取".into()),
            bad_key: false,
        },
        schedule: remote.schedule.clone(),
        owned_pid: 0,
        found_pid: 0,
        settings: settings.clone(),
        stopped_by_operator: false,
        autostart: autostart_on,
    }));

    // Bring the gateway up before the first reading. This is the whole reason the tray
    // exists, and doing it first is what keeps the first state the real one: a poll that
    // raced the gateway's startup reports "down" about a gateway that is about to be up.
    if settings.autostart_gateway {
        bootstrap(&layout, &shared);
    }

    let (state_tx, state_rx) = channel::<()>();
    let (cmd_tx, cmd_rx) = channel::<menu::Command>();

    let tray = {
        let s = shared.lock().unwrap();
        let facts = facts(&s);
        let m = menu::build(&facts);
        TrayIconBuilder::new()
            .with_menu(Box::new(m))
            .with_tooltip(tooltip(&facts))
            .with_icon(icon(&facts, &s))
            .build()
            .expect("the notification area refused the icon")
    };

    // The poll, on its own thread. It reports a reading, a schedule, and whether the
    // gateway is up; it does not touch the tray, because the tray belongs to the thread
    // that made it.
    {
        let shared = Arc::clone(&shared);
        let layout = layout.clone();
        let state_tx = state_tx.clone();
        std::thread::spawn(move || {
            loop {
                let (client, linkage, stopped) = {
                    let s = shared.lock().unwrap();
                    (
                        Client::new(&s.settings.base_url, &s.settings.api_key, TIMEOUT),
                        s.settings.autostart_gateway,
                        s.stopped_by_operator,
                    )
                };
                let reading = client.read();

                // The gateway is looked for on every poll, so one started or stopped by
                // anything else is noticed and not only the ones this tray did.
                let found = gateway::find_all();
                let still_ours = {
                    let s = shared.lock().unwrap();
                    gateway::alive(s.owned_pid)
                };

                {
                    let mut s = shared.lock().unwrap();
                    s.reading = reading;
                    if !still_ours {
                        s.owned_pid = 0;
                    }
                    s.found_pid = found.first().copied().unwrap_or(0);
                    if let Some(c) = gateway::read_config(&layout) {
                        if c.schedule.on_count() > 0 {
                            s.schedule = c.schedule;
                        }
                    }
                    // Linkage: a gateway that is not running is started again, unless the
                    // operator asked for it to stay down.
                }

                // Read after the block above so the pids it just refreshed are the ones
                // being asked about.
                let need_start = {
                    let s = shared.lock().unwrap();
                    linkage && !stopped && !s.gateway_running()
                };
                if need_start && layout.installed() {
                    let _ = start_gateway(&layout, &shared);
                }

                // One message per reading: the main thread rebuilds the menu from it.
                let _ = state_tx.send(());
                std::thread::sleep(POLL);
            }
        });
    }

    let menu_rx = MenuEvent::receiver();
    // A short timeout rather than a blocking wait, because there are two sources of work —
    // a reading and a click — and a blocking wait on one would stall the other. The wait
    // ends on a reading, on the timeout, and never on a disconnect: the sender lives as
    // long as the thread that owns it, so a disconnect means the poll thread has ended and
    // there is nothing left to wait for.
    while let Ok(()) | Err(RecvTimeoutError::Timeout) =
        state_rx.recv_timeout(Duration::from_millis(120))
    {
        {
            let s = shared.lock().unwrap();
            let f = facts(&s);
            let m = menu::build(&f);
            tray.set_menu(Some(Box::new(m)));
            let _ = tray.set_tooltip(Some(tooltip(&f)));
            let _ = tray.set_icon(Some(icon(&f, &s)));
        }

        while let Ok(ev) = menu_rx.try_recv() {
            let cmd = menu::Command::from_id(&ev.id.0);
            if let Some(cmd) = cmd {
                let _ = cmd_tx.send(cmd);
            }
        }

        while let Ok(cmd) = cmd_rx.try_recv() {
            if cmd == menu::Command::Quit {
                // The gateway is left running, deliberately: it is what is serving, and
                // quitting the thing that looks at it is not a request to stop it.
                return;
            }
            handle(cmd, &layout, &shared);
        }
    }
}

/// Find a gateway or start one.
fn bootstrap(layout: &Layout, shared: &Arc<Mutex<Shared>>) {
    if let Some(&pid) = gateway::find_all().first() {
        shared.lock().unwrap().found_pid = pid;
        return;
    }
    if layout.installed() {
        let _ = start_gateway(layout, shared);
    }
}

/// Start the gateway, wait for it to answer, and record that this tray owns it.
fn start_gateway(layout: &Layout, shared: &Arc<Mutex<Shared>>) -> Result<u32, String> {
    let (base, key) = {
        let s = shared.lock().unwrap();
        (s.settings.base_url.clone(), s.settings.api_key.clone())
    };
    let port = gateway::port_of(
        base.trim_start_matches("http://")
            .trim_start_matches("https://"),
    );
    let pid = gateway::start(layout, port).map_err(|e| format!("无法启动网关：{e}"))?;

    // A process that has launched is not a gateway that is ready: it reads its
    // configuration and binds its port over the next seconds, and reading it before that
    // produces a tray that reports "down" every time it starts.
    let client = Client::new(&base, &key, TIMEOUT);
    for _ in 0..40 {
        if client.probe().is_ok() {
            break;
        }
        std::thread::sleep(Duration::from_millis(250));
    }

    let mut s = shared.lock().unwrap();
    s.owned_pid = pid;
    s.found_pid = 0;
    s.stopped_by_operator = false;
    Ok(pid)
}

/// What the menu and the tooltip draw from, borrowed from the shared state.
fn facts(s: &Shared) -> menu::Facts<'_> {
    menu::Facts {
        reading: &s.reading,
        schedule: &s.schedule,
        base: &s.settings.base_url,
        key: &s.settings.api_key,
        gateway_running: s.gateway_running(),
        linked: s.settings.autostart_gateway,
        autostart: s.autostart,
    }
}

fn tooltip(f: &menu::Facts) -> String {
    let state = match f.reading.state {
        State::Ok => "正常",
        State::Degraded => "异常",
        State::Down => "离线",
    };
    format!(
        "wbtray — {state}\n{} · 积分 {}",
        f.base,
        menu::comma(f.reading.overview.credits())
    )
}

/// The tray icon, drawn in the tone that reads against the taskbar Windows is using.
///
/// A fixed colour is invisible on one of the two tones or needs a plate behind it, and a
/// plate is exactly the block of colour this design avoids: the notification area draws
/// bare glyphs beside this one.
fn icon(f: &menu::Facts, _s: &Shared) -> Icon {
    // The mark carries the state, so the tray answers "is anything wrong" without being
    // clicked: a light mark when the pool is serving, a dimmed one when it is not.
    let ink: [u8; 3] = match (theme::taskbar_is_light(), f.reading.state) {
        (true, State::Ok) => [0x14, 0x14, 0x14],
        (true, _) => [0x8a, 0x8a, 0x8a],
        (false, State::Ok) => [0xf2, 0xf2, 0xf2],
        (false, _) => [0x8a, 0x8a, 0x8a],
    };
    Icon::from_rgba(cat::icon_rgba_toned(ICON, ink), ICON, ICON).expect("the cat did not render")
}

/// Do what a menu row asked for.
fn handle(cmd: menu::Command, layout: &Layout, shared: &Arc<Mutex<Shared>>) {
    let (base, key) = {
        let s = shared.lock().unwrap();
        (s.settings.base_url.clone(), s.settings.api_key.clone())
    };

    match cmd {
        menu::Command::OpenPanel => {
            let url = Client::new(&base, &key, TIMEOUT).panel_url();
            if let Err(e) = dialog::open_url(&url) {
                dialog::warn("wbtray", &e);
            }
        }
        menu::Command::CopyAddress => {
            if let Err(e) = clipboard::copy(&base) {
                dialog::warn("复制失败", &e);
            }
        }
        menu::Command::CopyKey => {
            if let Err(e) = clipboard::copy(&key) {
                dialog::warn("复制失败", &e);
            }
        }
        menu::Command::Start => {
            if let Err(e) = start_gateway(layout, shared) {
                dialog::warn("wbtray", &e);
            }
        }
        menu::Command::Stop => {
            let pid = shared.lock().unwrap().gateway_pid();
            if pid != 0 {
                if let Err(e) = gateway::stop(pid) {
                    dialog::warn("停止失败", &e.to_string());
                }
            }
            let mut s = shared.lock().unwrap();
            s.owned_pid = 0;
            s.found_pid = 0;
            // Asked for, so it stays down: without this the linkage would start it again
            // on the next poll and the row would look like it had done nothing.
            s.stopped_by_operator = true;
        }
        menu::Command::Restart => {
            let pid = shared.lock().unwrap().gateway_pid();
            if pid != 0 {
                let _ = gateway::stop(pid);
            }
            {
                let mut s = shared.lock().unwrap();
                s.owned_pid = 0;
                s.found_pid = 0;
                s.stopped_by_operator = false;
            }
            // The port is released a moment after the process ends, so starting straight
            // away gets a bind failure that looks like a fault and is only timing.
            std::thread::sleep(Duration::from_millis(700));
            if let Err(e) = start_gateway(layout, shared) {
                dialog::warn("重启失败", &e);
            }
        }
        menu::Command::ToggleLinkage => {
            let (on, saved) = {
                let mut s = shared.lock().unwrap();
                s.settings.autostart_gateway = !s.settings.autostart_gateway;
                if s.settings.autostart_gateway {
                    // Turning it back on is also a request for the gateway to be up.
                    s.stopped_by_operator = false;
                }
                (s.settings.autostart_gateway, s.settings.clone())
            };
            let _ = saved.save(&settings::path_beside_exe(&layout.root));
            if on && !shared.lock().unwrap().gateway_running() {
                let _ = start_gateway(layout, shared);
            }
        }
        menu::Command::ToggleAutostart => {
            let want = !shared.lock().unwrap().autostart;
            if let Err(e) = autostart::set(want) {
                dialog::warn("自启设置失败", &e);
                return;
            }
            let saved = {
                let mut s = shared.lock().unwrap();
                s.autostart = want;
                s.settings.autostart_tray = want;
                s.settings.clone()
            };
            let _ = saved.save(&settings::path_beside_exe(&layout.root));
        }
        menu::Command::About => show_about(layout, shared),
        menu::Command::Quit => {}
    }
}

fn show_about(layout: &Layout, shared: &Arc<Mutex<Shared>>) {
    let s = shared.lock().unwrap();
    let installed = layout
        .recorded_version()
        .or_else(|| {
            (!s.reading.overview.version.is_empty()).then(|| s.reading.overview.version.clone())
        })
        .unwrap_or_else(|| "未知".into());
    let state = match s.reading.state {
        State::Ok => "运行中".to_string(),
        State::Degraded => "运行中，无可用账号".to_string(),
        State::Down => match &s.reading.error {
            Some(e) => format!("未运行（{e}）"),
            None => "未运行".to_string(),
        },
    };
    let body = format!(
        "wbtray {VERSION}\n\
         workbuddy2api {installed}\n\
         \n\
         网关：{}\n\
         状态：{state}\n\
         账号：{}/{} 可用\n\
         积分：{}\n\
         \n\
         作者：laincat\n\
         https://github.com/laincat/wbtray\n\
         以 MIT 许可发布。",
        s.settings.base_url,
        s.reading.overview.ready(),
        s.reading.overview.total,
        menu::comma(s.reading.overview.credits()),
    );
    drop(s);
    dialog::about("关于 wbtray", &body);
}
