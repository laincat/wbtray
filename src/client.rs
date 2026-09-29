// The gateway's HTTP API, as the tray reads it.
//
// The tray holds no state of its own about accounts or usage. The gateway is the
// single source of truth and this is the only thing the tray reads, so the two cannot
// disagree about the numbers.

use serde::Deserialize;
use std::time::Duration;

/// How the last reading went.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum State {
    /// The gateway answered and has accounts it can serve with.
    Ok,
    /// The gateway answered and cannot serve: every account is cooling, disabled, or
    /// there are none.
    Degraded,
    /// The gateway did not answer, or answered with something the tray cannot use.
    Down,
}

/// One account in the pool.
#[derive(Clone, Debug, Default, Deserialize)]
pub struct Account {
    #[serde(default)]
    pub uid: String,
    #[serde(default)]
    pub nickname: String,
    #[serde(default)]
    pub credits: i64,
    #[serde(default)]
    pub cooling: bool,
    #[serde(default)]
    pub disabled: bool,
}

impl Account {
    /// The name to show: the operator's own if there is one, the uid otherwise.
    pub fn label(&self) -> String {
        if !self.nickname.is_empty() {
            self.nickname.clone()
        } else if !self.uid.is_empty() {
            self.uid.chars().take(8).collect()
        } else {
            "—".into()
        }
    }

    /// Whether this account can serve right now.
    pub fn ready(&self) -> bool {
        !self.disabled && !self.cooling
    }
}

/// The pool as one reading.
#[derive(Clone, Debug, Default, Deserialize)]
#[serde(default)]
pub struct Overview {
    pub total: i64,
    pub healthy: i64,
    pub cooling: i64,
    pub disabled: i64,
    pub uptime_sec: i64,
    pub version: String,
    pub accounts: Vec<Account>,
    /// True when the gateway rejected the key, which is worth saying out loud rather
    /// than reporting as "unreachable".
    pub auth_required: bool,
}

impl Overview {
    /// The accounts that can serve right now.
    pub fn ready(&self) -> i64 {
        self.accounts.iter().filter(|a| a.ready()).count() as i64
    }

    /// Everything the pool still holds, disabled accounts left out.
    pub fn credits(&self) -> i64 {
        self.accounts
            .iter()
            .filter(|a| !a.disabled)
            .map(|a| a.credits)
            .sum()
    }

    /// The state the tray draws and reports.
    pub fn state(&self) -> State {
        if self.total == 0 || self.ready() == 0 {
            State::Degraded
        } else {
            State::Ok
        }
    }
}

/// One reading: the pool, and how the reading went.
#[derive(Clone, Debug)]
pub struct Reading {
    pub state: State,
    pub overview: Overview,
    /// The failure, when there is one, for the tooltip and the About box.
    pub error: Option<String>,
    /// True when the gateway answered but refused the key.
    pub bad_key: bool,
}

impl Reading {
    fn down(err: impl Into<String>) -> Reading {
        Reading {
            state: State::Down,
            overview: Overview::default(),
            error: Some(err.into()),
            bad_key: false,
        }
    }

    /// A reading of a gateway that is up, for tests that are about something else.
    #[cfg(test)]
    pub fn default_for_test() -> Reading {
        Reading {
            state: State::Degraded,
            overview: Overview::default(),
            error: None,
            bad_key: false,
        }
    }
}

/// A client for one gateway.
#[derive(Clone)]
pub struct Client {
    base: String,
    key: String,
    agent: ureq::Agent,
}

impl Client {
    pub fn new(base: &str, key: &str, timeout: Duration) -> Client {
        Client {
            base: base.trim_end_matches('/').to_string(),
            key: key.to_string(),
            agent: ureq::Agent::config_builder()
                .timeout_global(Some(timeout))
                .build()
                .into(),
        }
    }

    /// The console page to open in a browser.
    ///
    /// It deliberately carries no key. A key in a URL is written to browser history,
    /// where it outlives the visit and is copied by profile sync, and the gateway's
    /// console does not read a key parameter anyway.
    pub fn panel_url(&self) -> String {
        format!("{}/panel/", self.base)
    }

    /// One reading, from the two endpoints that describe the pool.
    pub fn read(&self) -> Reading {
        // The health probe decides reachability. It answers 200 or 503 depending on
        // whether the pool can serve, and a 503 from it is a running gateway saying so
        // — the body is a complete answer. Treating that as a failure is what makes a
        // tray report "offline" about a gateway that is up.
        match self.get("/healthz") {
            Ok(_) => {}
            Err(e) => return Reading::down(e),
        }

        let body = match self.get("/panel/api/overview") {
            Ok(b) => b,
            Err(e) if e.contains("401") => {
                return Reading {
                    state: State::Down,
                    overview: Overview::default(),
                    error: None,
                    bad_key: true,
                }
            }
            Err(e) => return Reading::down(e),
        };

        match serde_json::from_str::<Overview>(&body) {
            Ok(o) => {
                let state = o.state();
                Reading {
                    state,
                    overview: o,
                    error: None,
                    bad_key: false,
                }
            }
            Err(e) => Reading::down(format!("the overview did not parse: {e}")),
        }
    }

    /// Ask whether the gateway is answering, without reading the pool.
    ///
    /// A response of any kind means it is up, including a 503: the health endpoint
    /// answers 503 while the pool has nothing it can serve with, and that is a running
    /// gateway saying so.
    pub fn probe(&self) -> Result<(), String> {
        self.get("/healthz").map(|_| ())
    }

    /// GET one path, returning the body or a short description of what went wrong.
    fn get(&self, path: &str) -> Result<String, String> {
        let url = format!("{}{path}", self.base);
        let mut req = self.agent.get(&url);
        if !self.key.is_empty() {
            req = req.header("Authorization", &format!("Bearer {}", self.key));
        }
        match req.call() {
            Ok(resp) => resp
                .into_body()
                .read_to_string()
                .map_err(|e| format!("reading {path}: {e}")),
            // A 503 is the gateway answering, so the body is read rather than discarded;
            // its caller has already decided that reaching it is enough.
            // A 503 is the gateway answering that it cannot serve yet. Its body is not
            // read because the caller has already decided that reaching it is enough, and
            // the health endpoint's answer is the whole of what it has to say.
            Err(ureq::Error::StatusCode(503)) => Ok(String::new()),
            Err(ureq::Error::StatusCode(code)) => Err(format!("{url} returned {code}")),
            Err(e) => Err(format!("cannot reach {url}: {e}")),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn pool(total: i64, ready: usize, cooling: bool) -> Overview {
        let mut accounts = Vec::new();
        for i in 0..total as usize {
            accounts.push(Account {
                uid: format!("u{i}"),
                credits: 100,
                cooling: cooling && i >= ready,
                disabled: false,
                ..Default::default()
            });
        }
        Overview {
            total,
            accounts,
            ..Default::default()
        }
    }

    #[test]
    fn a_pool_with_a_ready_account_is_ok() {
        assert_eq!(pool(3, 2, true).state(), State::Ok);
    }

    #[test]
    fn a_pool_that_can_serve_nothing_is_degraded_not_down() {
        // The gateway is running — it answered — and this is the whole reason State has
        // three values rather than two.
        assert_eq!(pool(2, 0, true).state(), State::Degraded);
        assert_eq!(pool(0, 0, false).state(), State::Degraded);
    }

    #[test]
    fn credits_leave_out_disabled_accounts() {
        let mut o = pool(2, 2, false);
        o.accounts[1].disabled = true;
        o.accounts[1].credits = 9999;
        assert_eq!(o.credits(), 100);
    }

    #[test]
    fn an_account_falls_back_to_its_uid() {
        let a = Account {
            uid: "17caa3c8-9718-4ec5".into(),
            ..Default::default()
        };
        assert_eq!(a.label(), "17caa3c8");
    }
}
