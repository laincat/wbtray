// A check against the gateway actually running on this machine.
//
// It is skipped unless `WBTRAY_LIVE_CONF` names a configuration file, because a test that
// needs a gateway is a test that fails for anyone who does not have one — and the failure
// would say nothing about the code. What it catches is the class of fault the tray had
// repeatedly: a reading that is wrong about a gateway that is up, which no amount of
// unit testing against a stub can see.
//
//
//     WBTRAY_LIVE_CONF="D:\Program Files\wbtray\wbtray.conf" cargo test --test live_gateway -- --nocapture

// The tray's own crate uses every one of these; this test pulls the module in on its own
// and leaves the ones it does not exercise looking unused.
#![allow(dead_code)]

use std::time::Duration;

#[path = "../src/client.rs"]
mod client;

#[test]
fn the_real_gateway_reads_as_reachable() {
    let Ok(conf) = std::env::var("WBTRAY_LIVE_CONF") else {
        eprintln!("set WBTRAY_LIVE_CONF to the installed wbtray.conf; skipping");
        return;
    };
    // The file is the tray's own format: `key = value` lines.
    let text = std::fs::read_to_string(&conf).expect("the configuration file could not be read");
    let mut base = String::new();
    let mut key = String::new();
    for line in text.lines() {
        let line = line.trim();
        if line.starts_with('#') || line.is_empty() {
            continue;
        }
        if let Some((k, v)) = line.split_once('=') {
            let v = v.trim().to_string();
            match k.trim() {
                "base_url" if !v.is_empty() => base = v,
                "api_key" if !v.is_empty() => key = v,
                _ => {}
            }
        }
    }
    assert!(!base.is_empty(), "no base_url in {conf}");
    eprintln!("reading {base} with a {}-character key", key.len());

    let c = client::Client::new(&base, &key, Duration::from_secs(8));
    let r = c.read();
    eprintln!(
        "state={:?} reachable_error={:?} bad_key={} accounts={} credits={}",
        r.state,
        r.error,
        r.bad_key,
        r.overview.total,
        r.overview.credits()
    );

    // A gateway that is running must not be reported as down. That is the fault this
    // test exists for, and it is the one the tray showed on a machine where the gateway
    // answered every request in milliseconds.
    assert!(
        r.state != client::State::Down,
        "the gateway reported as down: {:?}",
        r.error
    );
    assert!(!r.bad_key, "the key was rejected: {}", key.len());
}
