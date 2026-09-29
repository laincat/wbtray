// The gateway: where it is, whether it is running, and how to start and stop it.
//
// The tray observes the gateway before it controls it. A gateway started by another
// program, by a scheduled task, or by hand is found by walking the process table and is
// reported as running, and the tray does not claim it started it. The distinction
// matters at exactly one moment — when the operator asks for a stop — because a stop
// is meant, whoever started the process.

use std::path::{Path, PathBuf};
use std::process::Command;

#[cfg(windows)]
use std::os::windows::process::CommandExt;

/// The executable the gateway ships as.
pub const EXE: &str = "wb2api.exe";

/// The subdirectory the gateway is installed into, beside the tray.
pub const SUBDIR: &str = "wb2api";

/// CREATE_NO_WINDOW. The gateway is a console program and the tray gives it no console
/// at all: its output goes to the log ring it serves over HTTP, so a console window
/// would be a black rectangle with nothing in it that the tray cannot already show.
#[cfg(windows)]
const CREATE_NO_WINDOW: u32 = 0x0800_0000;

/// Where the gateway is installed, and where it keeps its settings.
#[derive(Clone, Debug)]
pub struct Layout {
    /// The directory the tray runs from.
    pub root: PathBuf,
    /// The directory the gateway lives in.
    pub dir: PathBuf,
}

impl Layout {
    /// The layout for this run, derived from where the tray's own executable is.
    pub fn discover() -> Layout {
        let root = std::env::current_exe()
            .ok()
            .and_then(|p| p.parent().map(Path::to_path_buf))
            .unwrap_or_else(|| PathBuf::from("."));
        let dir = root.join(SUBDIR);
        Layout { root, dir }
    }

    /// The gateway's executable.
    pub fn exe(&self) -> PathBuf {
        self.dir.join(EXE)
    }

    /// The gateway's own configuration file.
    pub fn config(&self) -> PathBuf {
        self.dir.join("config.json")
    }

    /// Whether a gateway is installed here.
    pub fn installed(&self) -> bool {
        self.exe().is_file()
    }

    /// The version recorded when the gateway was installed, if it was written.
    pub fn recorded_version(&self) -> Option<String> {
        let s = std::fs::read_to_string(self.dir.join("installed-version.txt")).ok()?;
        let s = s.trim().to_string();
        if s.is_empty() {
            None
        } else {
            Some(s)
        }
    }
}

/// What the gateway's own configuration says, as far as the tray cares.
#[derive(Clone, Debug, Default, serde::Deserialize)]
pub struct Config {
    #[serde(default)]
    pub api_key: String,
    #[serde(default)]
    pub listen: String,
    #[serde(default)]
    pub schedule: Schedule,
}

/// Which of the gateway's scheduled tasks are switched on.
#[derive(Clone, Debug, Default, serde::Deserialize)]
pub struct Schedule {
    #[serde(default)]
    pub checkin_enabled: bool,
    #[serde(default)]
    pub travel_enabled: bool,
    #[serde(default)]
    pub activity_enabled: bool,
    #[serde(default)]
    pub keepalive_enabled: bool,
    #[serde(default)]
    pub blackcat_enabled: bool,
    #[serde(default)]
    pub growth_enabled: bool,
    #[serde(default)]
    pub balance_refresh_enabled: bool,
}

impl Schedule {
    /// The tasks that are on, as (label, on) in the order they are shown.
    pub fn rows(&self) -> [(&'static str, bool); 7] {
        [
            ("签到", self.checkin_enabled),
            ("旅行", self.travel_enabled),
            ("活跃", self.activity_enabled),
            ("保活", self.keepalive_enabled),
            ("黑猫", self.blackcat_enabled),
            ("成长", self.growth_enabled),
            ("余额", self.balance_refresh_enabled),
        ]
    }

    /// How many are on.
    pub fn on_count(&self) -> usize {
        self.rows().iter().filter(|(_, on)| *on).count()
    }
}

/// Read the gateway's configuration from beside its executable.
pub fn read_config(layout: &Layout) -> Option<Config> {
    let text = std::fs::read_to_string(layout.config()).ok()?;
    serde_json::from_str(&text).ok()
}

/// The port out of a listen address such as ":7863" or "0.0.0.0:7863".
pub fn port_of(listen: &str) -> Option<u16> {
    listen.rsplit(':').next()?.trim().parse().ok()
}

/// Every process running under the gateway's name.
#[cfg(windows)]
pub fn find_all() -> Vec<u32> {
    use windows::Win32::System::Diagnostics::ToolHelp::{
        CreateToolhelp32Snapshot, Process32FirstW, Process32NextW, PROCESSENTRY32W,
        TH32CS_SNAPPROCESS,
    };

    let mut out = Vec::new();
    unsafe {
        let snap = match CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0) {
            Ok(h) => h,
            Err(_) => return out,
        };
        let mut entry = PROCESSENTRY32W {
            dwSize: std::mem::size_of::<PROCESSENTRY32W>() as u32,
            ..Default::default()
        };
        if Process32FirstW(snap, &mut entry).is_ok() {
            loop {
                let name = String::from_utf16_lossy(
                    &entry.szExeFile[..entry
                        .szExeFile
                        .iter()
                        .position(|c| *c == 0)
                        .unwrap_or(entry.szExeFile.len())],
                );
                if name.eq_ignore_ascii_case(EXE) {
                    out.push(entry.th32ProcessID);
                }
                entry.dwSize = std::mem::size_of::<PROCESSENTRY32W>() as u32;
                if Process32NextW(snap, &mut entry).is_err() {
                    break;
                }
            }
        }
        let _ = windows::Win32::Foundation::CloseHandle(snap);
    }
    out
}

#[cfg(not(windows))]
pub fn find_all() -> Vec<u32> {
    Vec::new()
}

/// Whether a specific process is still alive.
pub fn alive(pid: u32) -> bool {
    pid != 0 && find_all().contains(&pid)
}

/// Start the gateway, with no console of its own.
///
/// The port is passed through the environment rather than as an argument, because the
/// gateway takes no listen flag: `WB2A_LISTEN` is the documented way for a host to say
/// where it should bind, and using it keeps the tray's address and the gateway's
/// listener in step without editing the operator's `config.json`.
pub fn start(layout: &Layout, port: Option<u16>) -> std::io::Result<u32> {
    let mut cmd = Command::new(layout.exe());
    cmd.current_dir(&layout.dir);
    if let Some(p) = port {
        cmd.env("WB2A_LISTEN", format!(":{p}"));
    }
    #[cfg(windows)]
    cmd.creation_flags(CREATE_NO_WINDOW);
    let child = cmd.spawn()?;
    Ok(child.id())
}

/// Stop a gateway and everything it started.
///
/// The child processes go too: a gateway that spawned a helper would otherwise leave
/// the helper holding the port, and the next start would fail for a reason the operator
/// cannot see.
pub fn stop(pid: u32) -> std::io::Result<()> {
    if pid == 0 {
        return Ok(());
    }
    #[cfg(windows)]
    {
        let status = Command::new("taskkill")
            .args(["/PID", &pid.to_string(), "/T", "/F"])
            .creation_flags(CREATE_NO_WINDOW)
            .status()?;
        if !status.success() {
            return Err(std::io::Error::other(format!("taskkill exited {status}")));
        }
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_port_comes_out_of_every_listen_shape() {
        for (listen, want) in [
            (":7863", 7863),
            ("0.0.0.0:7863", 7863),
            ("127.0.0.1:9000", 9000),
        ] {
            assert_eq!(port_of(listen), Some(want), "listen = {listen}");
        }
        assert_eq!(port_of(""), None);
        assert_eq!(port_of("localhost"), None);
        assert_eq!(port_of(":notaport"), None);
    }

    #[test]
    fn the_schedule_reports_which_tasks_are_on() {
        let s = Schedule {
            checkin_enabled: true,
            keepalive_enabled: true,
            ..Default::default()
        };
        assert_eq!(s.on_count(), 2);
        let on: Vec<&str> = s
            .rows()
            .iter()
            .filter(|(_, on)| *on)
            .map(|(n, _)| *n)
            .collect();
        assert_eq!(on, vec!["签到", "保活"]);
    }

    #[test]
    fn a_missing_config_file_is_not_an_error() {
        let layout = Layout {
            root: PathBuf::from("does-not-exist"),
            dir: PathBuf::from("does-not-exist"),
        };
        assert!(read_config(&layout).is_none());
    }
}
