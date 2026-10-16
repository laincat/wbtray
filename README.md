# wbtray 3 (Rust)

A Windows notification-area companion for the
[workbuddy2api-panel gateway](https://github.com/linguo2625469/workbuddy2api-panel).
It reads gateway health and account information, opens the gateway's web panel,
copies the connection address/key, and can manage a local gateway it started.

**English** | [简体中文](ReadMe_zhCN.md)

## Version and scope

This source tree is the Rust 3.0.0 implementation. The published **v2.0.2 Go
release is a different implementation**; its screenshots, configuration options,
installer/updater behavior and build commands do not describe this tree. A
3.0.0 version in Cargo.toml does not by itself mean a Rust release is published.

The current UI is a native Windows tray menu with Chinese labels and a cat icon.
The gateway's browser panel provides account setup and detailed administration.
There is no custom wbtray dashboard, built-in gateway downloader, self-updater,
console-window switch, theme selector or language selector in this implementation.

## Manual installation

1. Build this Rust tree, or obtain a binary explicitly identified as this Rust
   version from a trusted release. Do not assume the v2.0.2 download is Rust 3.
2. Put `wbtray.exe` in a writable directory.
3. Download the gateway separately from its upstream project and extract it into
   `wb2api` beside the tray. Follow the gateway's own account/configuration setup.
4. Run `wbtray.exe`. The tray creates its own `wbtray.conf` when absent.

```text
wbtray/
  wbtray.exe
  wbtray.conf
  wb2api/
    wb2api.exe
    config.json
    auths/
    data/
```

The tray does not fetch missing gateway files. For an already-running remote
gateway, no local gateway installation is required: configure its URL and key
explicitly and disable gateway linkage.

## Configuration

`wbtray.conf` uses simple `key = value` lines, not general TOML. Put comments on
their own lines. Only these settings are supported:

```text
base_url =
api_key =
autostart_gateway = true
autostart_tray = false
```

- Empty `base_url` derives a loopback URL from the local gateway's `listen` port,
  with `http://127.0.0.1:7863` as the fallback.
- Empty `api_key` inherits the local gateway's key only when the endpoint is
  recognized as that local installation. An explicit remote URL never inherits
  the local key. Set the remote key explicitly if authentication is required.
- Local `wb2api/config.json` is reread each polling cycle. Inherited URL, key and
  task settings are refreshed; resolved credentials are not written back to the
  tray configuration. Explicit tray values remain overrides.
- `autostart_gateway` enables starting the installed local gateway when its
  endpoint is unavailable. The linkage menu changes this setting.
- Use the tray's **自启** menu item to register/unregister starting wbtray with
  Windows. The Windows per-user Run entry is the actual startup mechanism; the
  configuration flag alone is not an installer. Its command quotes the executable
  path, and a stale or unrelated Run value is not counted as enabled.

Settings are stored beside the executable where writable, otherwise under
`%APPDATA%\wbtray\wbtray.conf`. Restart the tray after manually changing this
file; gateway configuration is the part refreshed during polling.

## Menu and process ownership

Right-click the tray icon to use the native menu:

- Account/status and credits: current gateway readings
- **界面**: open the gateway web panel, copy the address, or copy the key
- **任务**: read-only local gateway scheduled-task flags
- **联动**: local gateway linkage and available start/stop/restart operations
- **自启**, **关于**, **退出**: Windows startup, About, and Quit

Only a child process started by this tray instance can be stopped or restarted
by it. A gateway launched by another application, another tray instance, a
scheduled task or manually is observation-only. Remote endpoints are also
observation-only; their task flags are not borrowed from local configuration.
Quitting the tray leaves a running gateway running. On relaunch that existing
gateway is external to the new tray instance.

The tray starts `wb2api/wb2api.exe` with that directory as its working directory.
It preserves the gateway's configured listen address, including the bind host;
it does not turn a loopback bind into `0.0.0.0` by reconstructing it from a URL.
A local gateway is started without a console window. Stop is process termination,
not a promise of graceful request draining; avoid stopping during important work.

Clipboard writes use the Win32 Unicode clipboard API. Copying a key exposes it
to the Windows clipboard and any enabled clipboard history/sync or clipboard
reader. Treat copied keys as credentials.

## Build and tests

Use Rust stable and a Windows toolchain. Dependency versions are recorded in
`Cargo.lock`; this application does use third-party crates.

For an MSVC Windows build, install Visual Studio Build Tools with the C++
workload and Windows SDK. In its Developer PowerShell (with `link.exe` and
`rc.exe` available):

```powershell
rustup target add x86_64-pc-windows-msvc
cargo build --release --locked --target x86_64-pc-windows-msvc
cargo test --release --locked --target x86_64-pc-windows-msvc
```

Alternatively, for a GNU Windows build, install MinGW-w64 including the linker
and `windres`, and put its tools on `PATH`:

```powershell
rustup target add x86_64-pc-windows-gnu
cargo build --release --locked --target x86_64-pc-windows-gnu
cargo test --release --locked --target x86_64-pc-windows-gnu
```

The resulting tray is
`target/<selected-target>/release/wbtray.exe`. Use your selected target in the
following checks as well.

```powershell
cargo fmt --check
cargo clippy --all-targets --locked --target x86_64-pc-windows-gnu -- -D warnings
```

### Required interactive Windows acceptance checks

These are **pending manual checks, not verified results**. Compilation and unit
tests do not establish notification-area behavior on an interactive desktop.
Use a disposable local gateway and non-secret clipboard probes.

- Right-click opens the menu; submenus, keyboard navigation, Escape and click-away
  dismissal work. Repeat while polling and while the gateway is unavailable.
- Copy address/key and paste into a local editor. Verify Unicode, repeated copies
  and expected clipboard-busy error handling. The opt-in test below replaces the
  clipboard and must fail, rather than silently pass, when copying fails.
- Start a local child; stop/restart only that child. Verify externally started
  gateways and remote endpoints cannot be stopped from this tray.
- Change the gateway's local port/key/task flags and verify the next polling
  cycle uses them without persisting inherited secrets into `wbtray.conf`.
- Toggle Windows startup from a directory containing spaces; inspect the quoted
  Run entry and verify a stale executable path is not marked enabled.
- Quit removes the tray icon and leaves the gateway alive; relaunch observes it.
- Restart Windows Explorer and verify icon restoration, working right-click
  menus and clipboard commands. Record the OS version and result.

```powershell
cargo test --locked --target x86_64-pc-windows-gnu clipboard::tests::interactive_round_trip -- --ignored
```

## License

MIT. See [LICENSE](LICENSE).

Migration: older Rust builds may already have saved discovered values as explicit
overrides. To resume following the gateway, clear `base_url` and `api_key` in
`wbtray.conf` yourself. Existing explicit values are preserved because the tray
cannot safely infer their origin. No credential migration is performed.
