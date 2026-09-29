// The tray's own settings: the few things that are its business rather than the
// gateway's.
//
// The gateway's settings live in its own `config.json` and are edited through its own
// API. Nothing here duplicates them.

use std::path::{Path, PathBuf};

/// Where the tray's settings are kept.
///
/// Beside the executable first, because that is what an unpacked copy implies and what
/// makes a portable install portable. When that directory cannot be written to — an
/// install under `Program Files` — it falls back to the per-user directory.
pub fn path_beside_exe(exe_dir: &Path) -> PathBuf {
    let beside = exe_dir.join("wbtray.conf");
    if can_write(exe_dir) {
        return beside;
    }
    if let Some(dir) = std::env::var_os("APPDATA") {
        return PathBuf::from(dir).join("wbtray").join("wbtray.conf");
    }
    beside
}

fn can_write(dir: &Path) -> bool {
    let probe = dir.join(".wbtray-write-probe");
    match std::fs::write(&probe, b"1") {
        Ok(()) => {
            let _ = std::fs::remove_file(&probe);
            true
        }
        Err(_) => false,
    }
}

/// What the tray remembers between runs.
#[derive(Clone, Debug)]
pub struct Settings {
    /// The gateway root, e.g. `http://127.0.0.1:7863`.
    pub base_url: String,
    /// An explicit key, empty when the gateway's own configuration supplies it.
    pub api_key: String,
    /// Whether the tray starts the gateway when it is not running.
    pub autostart_gateway: bool,
    /// Whether the tray itself starts with Windows.
    pub autostart_tray: bool,
}

impl Default for Settings {
    fn default() -> Self {
        Settings {
            base_url: "http://127.0.0.1:7863".into(),
            api_key: String::new(),
            // On by default, because it is the whole point of the program: a tray that
            // had to be told to start its gateway would be a tray that does nothing the
            // first time it runs.
            autostart_gateway: true,
            autostart_tray: false,
        }
    }
}

impl Settings {
    /// Read the file, falling back to the defaults for anything it does not set.
    pub fn load(path: &Path) -> Settings {
        let mut s = Settings::default();
        let Ok(text) = std::fs::read_to_string(path) else {
            return s;
        };
        for line in text.lines() {
            let line = line.trim();
            if line.is_empty() || line.starts_with('#') {
                continue;
            }
            let Some((k, v)) = line.split_once('=') else {
                continue;
            };
            let v = v.trim().trim_matches('"');
            match k.trim() {
                "base_url" if !v.is_empty() => s.base_url = v.to_string(),
                "api_key" => s.api_key = v.to_string(),
                "autostart_gateway" => s.autostart_gateway = truthy(v),
                "autostart_tray" => s.autostart_tray = truthy(v),
                _ => {}
            }
        }
        s
    }

    /// Write the file, so what the tray is doing is visible and editable.
    pub fn save(&self, path: &Path) -> std::io::Result<()> {
        if let Some(dir) = path.parent() {
            std::fs::create_dir_all(dir)?;
        }
        let text = format!(
            "# wbtray — a tray for the workbuddy2api gateway.\n\
             #\n\
             # base_url is the gateway root. api_key may be left empty, in which case\n\
             # the tray reads the gateway's own config.json for the key and the port,\n\
             # which is what makes a first run work without copying anything by hand.\n\
             \n\
             base_url = {}\n\
             api_key = {}\n\
             \n\
             # autostart_gateway: start the gateway when it is not already running.\n\
             autostart_gateway = {}\n\
             # autostart_tray: start this tray when Windows starts.\n\
             autostart_tray = {}\n",
            self.base_url, self.api_key, self.autostart_gateway, self.autostart_tray
        );
        let tmp = path.with_extension("conf.tmp");
        std::fs::write(&tmp, text)?;
        std::fs::rename(&tmp, path)
    }
}

fn truthy(v: &str) -> bool {
    matches!(v.to_ascii_lowercase().as_str(), "1" | "true" | "yes" | "on")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn defaults_are_usable_on_their_own() {
        let s = Settings::default();
        assert_eq!(s.base_url, "http://127.0.0.1:7863");
        assert!(s.autostart_gateway);
        assert!(!s.autostart_tray);
    }

    #[test]
    fn a_missing_file_yields_the_defaults() {
        let s = Settings::load(Path::new("no-such-file.conf"));
        assert_eq!(s.base_url, Settings::default().base_url);
    }

    #[test]
    fn the_file_round_trips() {
        let dir = std::env::temp_dir().join("wbtray-settings-test");
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        let path = dir.join("wbtray.conf");

        let want = Settings {
            base_url: "http://127.0.0.1:9999".into(),
            api_key: "sk-test".into(),
            autostart_gateway: false,
            autostart_tray: true,
        };
        want.save(&path).unwrap();
        let got = Settings::load(&path);
        assert_eq!(got.base_url, want.base_url);
        assert_eq!(got.api_key, want.api_key);
        assert_eq!(got.autostart_gateway, want.autostart_gateway);
        assert_eq!(got.autostart_tray, want.autostart_tray);

        let _ = std::fs::remove_dir_all(&dir);
    }
}
