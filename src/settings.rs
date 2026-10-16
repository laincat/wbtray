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
    use std::fs::OpenOptions;
    let probe = dir.join(format!(".wbtray-write-probe-{}", std::process::id()));
    match OpenOptions::new().write(true).create_new(true).open(&probe) {
        Ok(file) => {
            drop(file);
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
            base_url: String::new(),
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
    /// Resolve a copy; never persist inherited credentials or ports.
    pub fn effective(&self, config: Option<&crate::gateway::Config>) -> Settings {
        let mut out = self.clone();
        if out.base_url.is_empty() {
            out.base_url = local_endpoint(config);
        }
        // Never send a local installation's key to an explicit remote endpoint.
        if out.api_key.is_empty() && self.local_control(config) {
            out.api_key = config.map(|c| c.api_key.clone()).unwrap_or_default();
        }
        out
    }

    /// Explicit remote URLs and mismatched ports are observation-only.
    pub fn local_control(&self, config: Option<&crate::gateway::Config>) -> bool {
        let endpoint = local_endpoint(config);
        let port = config
            .and_then(|c| crate::gateway::port_of(&c.listen))
            .unwrap_or(7863);
        self.base_url.is_empty()
            || self.base_url == endpoint
            || (endpoint == format!("http://127.0.0.1:{port}")
                && self.base_url == format!("http://localhost:{port}"))
    }

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
                "base_url" => s.base_url = v.to_string(),
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

/// Use the actual configured interface for readiness. A wildcard listener is
/// reached over the matching address family's loopback, never over the network.
fn local_endpoint(config: Option<&crate::gateway::Config>) -> String {
    let listen = config.map(|c| c.listen.as_str()).unwrap_or("");
    let port = crate::gateway::port_of(listen).unwrap_or(7863);
    let host = listen.rsplit_once(':').map(|(host, _)| host).unwrap_or("");
    let host = match host {
        "" | "0.0.0.0" | "localhost" => "127.0.0.1",
        "[::]" => "[::1]",
        other => other,
    };
    format!("http://{host}:{port}")
}

fn truthy(v: &str) -> bool {
    matches!(v.to_ascii_lowercase().as_str(), "1" | "true" | "yes" | "on")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn inherited_probe_endpoint_matches_preserved_listener() {
        for (listen, want) in [
            (":9000", "http://127.0.0.1:9000"),
            ("0.0.0.0:9000", "http://127.0.0.1:9000"),
            ("127.0.0.1:9000", "http://127.0.0.1:9000"),
            ("[::]:9001", "http://[::1]:9001"),
            ("[::1]:9001", "http://[::1]:9001"),
            ("192.168.1.2:9002", "http://192.168.1.2:9002"),
        ] {
            let config = crate::gateway::Config {
                listen: listen.into(),
                api_key: "local".into(),
                ..Default::default()
            };
            let configured = Settings::default();
            assert_eq!(configured.effective(Some(&config)).base_url, want);
            let explicit = Settings {
                base_url: want.into(),
                ..Default::default()
            };
            assert!(explicit.local_control(Some(&config)));
            assert_eq!(explicit.effective(Some(&config)).api_key, "local");
            let different = Settings {
                base_url: "http://192.168.1.3:9002".into(),
                ..Default::default()
            };
            assert!(!different.local_control(Some(&config)));
            assert!(different.effective(Some(&config)).api_key.is_empty());
        }
    }

    #[test]
    fn inherited_values_refresh_without_becoming_overrides() {
        let settings = Settings::default();
        let mut config = crate::gateway::Config {
            api_key: "first".into(),
            listen: "127.0.0.1:9000".into(),
            ..Default::default()
        };
        let first = settings.effective(Some(&config));
        assert_eq!(first.base_url, "http://127.0.0.1:9000");
        assert_eq!(first.api_key, "first");
        config.api_key = "rotated".into();
        config.listen = "127.0.0.1:9001".into();
        let next = settings.effective(Some(&config));
        assert_eq!(next.api_key, "rotated");
        assert_eq!(next.base_url, "http://127.0.0.1:9001");
        assert!(settings.api_key.is_empty());
        assert!(settings.base_url.is_empty());
    }

    #[test]
    fn remote_and_mismatched_endpoints_never_inherit_local_secrets_or_control() {
        let config = crate::gateway::Config {
            api_key: "local-secret".into(),
            listen: "127.0.0.1:9000".into(),
            ..Default::default()
        };
        for base in [
            "https://example.com",
            "http://127.0.0.1:9001",
            "http://localhost.evil:9000",
            "http://user@localhost:9000",
            "http://[::1]:9000/path",
        ] {
            let settings = Settings {
                base_url: base.into(),
                ..Default::default()
            };
            assert!(!settings.local_control(Some(&config)), "{base}");
            assert!(settings.effective(Some(&config)).api_key.is_empty());
        }
        for base in ["", "http://127.0.0.1:9000", "http://localhost:9000"] {
            let settings = Settings {
                base_url: base.into(),
                ..Default::default()
            };
            assert!(settings.local_control(Some(&config)), "{base}");
        }
    }

    #[test]
    fn defaults_are_usable_on_their_own() {
        let s = Settings::default();
        assert!(s.base_url.is_empty());
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
