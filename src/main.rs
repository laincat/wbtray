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
    schedule: Option<Schedule>,
    owned_running: bool,
    local_control: bool,
    configured: settings::Settings,
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
        self.owned_running || self.reading.state != State::Down && self.reading.error.is_none()
    }
}

fn main() {
    let layout = Layout::discover();
    let settings_path = settings::path_beside_exe(&layout.root);
    let configured = settings::Settings::load(&settings_path);
    let remote = gateway::read_config(&layout);
    let settings = configured.effective(remote.as_ref());
    if std::env::args().any(|a| a == "--probe") {
        probe(&layout, &settings);
        return;
    }
    if !settings_path.exists() {
        if let Err(e) = configured.save(&settings_path) {
            dialog::warn("设置保存失败", &e.to_string());
        }
    }
    let shared = Arc::new(Mutex::new(Shared {
        reading: Reading::default(),
        schedule: if configured.local_control(remote.as_ref()) {
            remote.as_ref().map(|c| c.schedule.clone())
        } else {
            None
        },
        owned_running: false,
        local_control: configured.local_control(remote.as_ref()),
        configured,
        settings,
        stopped_by_operator: false,
        autostart: autostart::enabled(),
    }));
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

    // All network/process work is serialized on one worker, never the icon thread.
    {
        let shared = Arc::clone(&shared);
        let layout = layout.clone();
        std::thread::spawn(move || {
            let mut child: Option<std::process::Child> = None;
            let mut pending = None;
            loop {
                let config = gateway::read_config(&layout);
                let effective = {
                    let mut s = shared.lock().unwrap();
                    s.settings = s.configured.effective(config.as_ref());
                    s.local_control = s.configured.local_control(config.as_ref());
                    s.schedule = if s.local_control {
                        config.map(|c| c.schedule)
                    } else {
                        None
                    };
                    s.settings.clone()
                };
                let mut error = None;
                let mut force_start = false;
                if let Some(cmd) = pending.take() {
                    let allowed = shared.lock().unwrap().local_control;
                    if allowed {
                        match cmd {
                            menu::Command::Stop | menu::Command::Restart => {
                                shared.lock().unwrap().stopped_by_operator = true;
                                if let Some(c) = child.as_mut() {
                                    match gateway::stop(c) {
                                        Ok(()) => {
                                            child = None;
                                            if cmd == menu::Command::Restart {
                                                shared.lock().unwrap().stopped_by_operator = false;
                                                force_start = true;
                                            }
                                        }
                                        Err(e) => error = Some(format!("停止失败：{e}")),
                                    }
                                } else {
                                    error = Some(
                                        "外部启动的网关仅可查看，请使用原启动方式停止。".into(),
                                    );
                                }
                            }
                            menu::Command::Start => {
                                shared.lock().unwrap().stopped_by_operator = false;
                                force_start = true;
                            }
                            _ => {}
                        }
                    }
                }
                if let Some(c) = child.as_mut() {
                    match c.try_wait() {
                        Ok(Some(status)) => {
                            error = Some(format!("网关已退出：{status}"));
                            child = None;
                        }
                        Err(e) => error = Some(format!("无法检查网关：{e}")),
                        Ok(None) => {}
                    }
                }
                let client = Client::new(&effective.base_url, &effective.api_key, TIMEOUT);
                let reachable = client.probe().is_ok();
                let should_start = {
                    let s = shared.lock().unwrap();
                    s.local_control
                        && (s.configured.autostart_gateway || force_start)
                        && !s.stopped_by_operator
                };
                if should_start
                    && !reachable
                    && child.is_none()
                    && layout.installed()
                    && error.is_none()
                {
                    match start_gateway(&layout, &effective) {
                        Ok(c) => child = Some(c),
                        Err((still_owned, e)) => {
                            child = still_owned;
                            error = Some(e);
                        }
                    }
                }
                let mut reading = client.read();
                if let Some(e) = error {
                    reading.error = Some(e);
                }
                {
                    let mut s = shared.lock().unwrap();
                    s.reading = reading;
                    s.owned_running = child.is_some();
                }
                let _ = state_tx.send(());
                match cmd_rx.recv_timeout(POLL) {
                    Ok(cmd) => pending = Some(cmd),
                    Err(RecvTimeoutError::Timeout) => {}
                    Err(RecvTimeoutError::Disconnected) => break,
                }
            }
        });
    }

    let menu_rx = MenuEvent::receiver();
    loop {
        if !pump_messages() {
            break;
        }
        let mut changed = state_rx.try_iter().count() > 0;
        while let Ok(ev) = menu_rx.try_recv() {
            if let Some(cmd) = menu::Command::from_id(&ev.id.0) {
                if cmd == menu::Command::Quit {
                    return;
                }
                if matches!(
                    cmd,
                    menu::Command::Start | menu::Command::Stop | menu::Command::Restart
                ) {
                    let _ = cmd_tx.send(cmd);
                } else {
                    handle(cmd, &layout, &shared);
                    let _ = cmd_tx.send(cmd); // wake worker after settings changes
                }
                changed = true;
            }
        }
        if changed {
            let s = shared.lock().unwrap();
            let f = facts(&s);
            tray.set_menu(Some(Box::new(menu::build(&f))));
            let _ = tray.set_tooltip(Some(tooltip(&f)));
            let _ = tray.set_icon(Some(icon(&f, &s)));
        }
        std::thread::sleep(Duration::from_millis(15));
    }
}

/// Dispatch shell callbacks on the thread that owns the icon and menu.
#[cfg(windows)]
fn pump_messages() -> bool {
    use windows::Win32::UI::WindowsAndMessaging::{
        DispatchMessageW, PeekMessageW, TranslateMessage, MSG, PM_REMOVE, WM_QUIT,
    };
    unsafe {
        let mut msg = MSG::default();
        while PeekMessageW(&mut msg, None, 0, 0, PM_REMOVE).as_bool() {
            if msg.message == WM_QUIT {
                return false;
            }
            let _ = TranslateMessage(&msg);
            DispatchMessageW(&msg);
        }
    }
    true
}

#[cfg(not(windows))]
fn pump_messages() -> bool {
    false
}

/// Find a gateway or start one.
/// Print one report of what the tray makes of the machine. See the call in `main`.
fn probe(layout: &Layout, settings: &settings::Settings) {
    println!("wbtray {VERSION}");
    println!(
        "executable     {}",
        std::env::current_exe()
            .map(|p| p.display().to_string())
            .unwrap_or_default()
    );
    println!("install root   {}", layout.root.display());
    println!("gateway dir    {}", layout.dir.display());
    println!(
        "gateway exe    {}  ({})",
        layout.exe().display(),
        if layout.installed() {
            "present"
        } else {
            "missing"
        }
    );
    println!(
        "recorded ver   {}",
        layout.recorded_version().unwrap_or_else(|| "(none)".into())
    );
    println!("base_url       {}", settings.base_url);
    println!(
        "api_key        {} characters",
        settings.api_key.chars().count()
    );
    println!("linkage        {}", settings.autostart_gateway);
    println!("tray autostart {}", autostart::enabled());

    println!("process control: only children started by this tray");

    if let Some(c) = gateway::read_config(layout) {
        println!(
            "gateway config listen={:?} key={} chars",
            c.listen,
            c.api_key.chars().count()
        );
        let on: Vec<&str> = c
            .schedule
            .rows()
            .iter()
            .filter(|(_, on)| *on)
            .map(|(n, _)| *n)
            .collect();
        println!(
            "tasks enabled  {} of 7: {}",
            c.schedule.on_count(),
            on.join(", ")
        );
    } else {
        println!("gateway config (not readable)");
    }

    let client = Client::new(&settings.base_url, &settings.api_key, TIMEOUT);
    match client.probe() {
        Ok(()) => println!("healthz        answered"),
        Err(e) => println!("healthz        FAILED: {e}"),
    }
    let r = client.read();
    println!("reading        {:?}", r.state);
    if let Some(e) = &r.error {
        println!("reading error  {e}");
    }
    println!("bad key        {}", r.bad_key);
    println!(
        "accounts       {} total, {} ready",
        r.overview.total,
        r.overview.ready()
    );
    println!("credits        {}", menu::comma(r.overview.credits()));
    println!("gateway ver    {}", r.overview.version);
    for a in &r.overview.accounts {
        println!(
            "  {}  credits={} cooling={} disabled={}",
            a.label(),
            a.credits,
            a.cooling,
            a.disabled
        );
    }
    println!("panel url      {}", client.panel_url());
}

/// Readiness has one deadline and checks the retained child's liveness.
fn start_gateway(
    layout: &Layout,
    settings: &settings::Settings,
) -> Result<std::process::Child, (Option<std::process::Child>, String)> {
    let mut child = gateway::start(layout).map_err(|e| (None, format!("无法启动网关：{e}")))?;
    let ready = wait_ready(Duration::from_secs(10), |timeout| {
        if let Some(status) = child.try_wait().map_err(|e| e.to_string())? {
            return Err(format!("网关启动期间退出：{status}"));
        }
        let answered = Client::new(&settings.base_url, &settings.api_key, timeout)
            .probe()
            .is_ok();
        if child.try_wait().map_err(|e| e.to_string())?.is_some() {
            return Err("网关启动期间退出".into());
        }
        Ok(answered)
    });
    if let Err(error) = ready {
        if let Err(e) = gateway::stop(&mut child) {
            // Retain ownership after cleanup failure so no competing process is spawned.
            return Err((Some(child), format!("{error}；停止失败：{e}")));
        }
        return Err((None, error));
    }
    Ok(child)
}

fn wait_ready(
    budget: Duration,
    mut probe: impl FnMut(Duration) -> Result<bool, String>,
) -> Result<(), String> {
    let deadline = std::time::Instant::now() + budget;
    loop {
        let remaining = deadline.saturating_duration_since(std::time::Instant::now());
        if remaining.is_zero() {
            return Err("网关启动超时".into());
        }
        if probe(remaining.min(Duration::from_millis(500)))? {
            return Ok(());
        }
        std::thread::sleep(
            deadline
                .saturating_duration_since(std::time::Instant::now())
                .min(Duration::from_millis(100)),
        );
    }
}

/// What the menu and the tooltip draw from, borrowed from the shared state.
fn facts(s: &Shared) -> menu::Facts<'_> {
    menu::Facts {
        reading: &s.reading,
        schedule: s.schedule.as_ref(),
        local_control: s.local_control,
        owned_running: s.owned_running,
        base: &s.settings.base_url,
        key: &s.settings.api_key,
        gateway_running: s.gateway_running(),
        linked: s.configured.autostart_gateway,
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
        "wbtray — {state}\n{} · 积分 {}\n{}",
        f.base,
        menu::comma(f.reading.overview.credits()),
        f.reading.error.as_deref().unwrap_or("")
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
        menu::Command::Start | menu::Command::Stop | menu::Command::Restart => {}
        menu::Command::ToggleLinkage => {
            if !shared.lock().unwrap().local_control {
                return;
            }
            let saved = {
                let mut s = shared.lock().unwrap();
                s.configured.autostart_gateway = !s.configured.autostart_gateway;
                if s.configured.autostart_gateway {
                    // Turning it back on is also a request for the gateway to be up.
                    s.stopped_by_operator = false;
                }
                s.configured.clone()
            };
            if let Err(e) = saved.save(&settings::path_beside_exe(&layout.root)) {
                dialog::warn("设置保存失败", &e.to_string());
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
                s.configured.autostart_tray = want;
                s.configured.clone()
            };
            if let Err(e) = saved.save(&settings::path_beside_exe(&layout.root)) {
                dialog::warn("设置保存失败", &e.to_string());
            }
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

#[cfg(test)]
mod lifecycle_tests {
    use super::*;

    #[cfg(windows)]
    #[test]
    fn native_pump_dispatches_window_messages() {
        use windows::core::w;
        use windows::Win32::Foundation::{LPARAM, WPARAM};
        use windows::Win32::UI::WindowsAndMessaging::{
            CreateWindowExW, IsWindow, PostMessageW, HWND_MESSAGE, WINDOW_EX_STYLE, WINDOW_STYLE,
            WM_CLOSE,
        };
        unsafe {
            let window = CreateWindowExW(
                WINDOW_EX_STYLE::default(),
                w!("STATIC"),
                w!("pump-test"),
                WINDOW_STYLE::default(),
                0,
                0,
                0,
                0,
                Some(HWND_MESSAGE),
                None,
                None,
                None,
            )
            .unwrap();
            assert!(IsWindow(Some(window)).as_bool());
            PostMessageW(Some(window), WM_CLOSE, WPARAM(0), LPARAM(0)).unwrap();
            assert!(pump_messages());
            assert!(
                !IsWindow(Some(window)).as_bool(),
                "WM_CLOSE must reach the window procedure"
            );
        }
    }

    #[test]
    fn readiness_requires_success_and_propagates_exit_errors() {
        assert!(wait_ready(Duration::from_millis(20), |_| Ok(true)).is_ok());
        assert_eq!(
            wait_ready(Duration::from_secs(10), |_| Err("child exited".into())),
            Err("child exited".into())
        );
    }

    #[test]
    fn readiness_has_one_total_deadline() {
        let start = std::time::Instant::now();
        let mut calls = 0;
        let result = wait_ready(Duration::from_millis(25), |remaining| {
            calls += 1;
            assert!(remaining <= Duration::from_millis(25));
            Ok(false)
        });
        assert!(result.is_err());
        assert!(calls > 0);
        assert!(start.elapsed() < Duration::from_secs(1));
        assert!(wait_ready(Duration::ZERO, |_| panic!("expired budget must not probe")).is_err());
    }
}
