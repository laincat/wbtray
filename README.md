# wbtray

A tray icon for the [workbuddy2api-panel](https://github.com/linguo2625469/workbuddy2api-panel)
gateway. It shows the pool's health, credits and traffic in the Windows
notification area, lets the switches that matter be flipped without opening a
browser, and can start, stop and hide the gateway itself.

**English** | [简体中文](ReadMe_zhCN.md)

```
   wb2api  :7863   --->   /healthz, /panel/api/*   --->   wbtray
   (the gateway)                                         (this program)
```

## What the icon shows

The icon is the readout, so the answer to "is anything wrong" does not need a
click. Its **colour** is the health of the pool, and its **shape** carries
whichever metric you choose.

Each of the eight styles, in both palettes, on both appearances, as the taskbar
actually draws them — 16 pixels, magnified here so the shapes are visible:

![Every style at 16 pixels](design/styles-all-16px.png)

| Colour | Means |
|---|---|
| Green | accounts are serving |
| Amber | the gateway is up but nothing can serve: every account is cooling, disabled, or there are none |
| Red | the gateway cannot be reached |
| Grey | refreshing is paused |

The icon has no plate. Its mark is drawn straight onto the taskbar over a soft
halo, which is what lets it sit next to the system's own icons without a block of
colour around it:

| Style | Draws |
|---|---|
| **Ring** | the metric as a filled gauge, with a tick at the exact value |
| **Bars** | the last four buckets of its history |
| **Sparkline** | the last twelve buckets as a smoothed curve, newest point marked |
| **Mascot** | the same cat the console uses, wearing the health colour |
| **Plain** | a quiet glyph, for when the tray should be present but not loud |
| **Bars + text** | the bar chart dimmed as a background, with a figure over it |
| **Text only** | the figure alone, at the largest size the icon allows |
| **Mascot + text** | the cat on the left, the figure on the right |

A shape and a number are different things: a curve shows a trend but cannot state
a value, and there are readings — credits left, accounts serving — where the value
is the point. Those three styles draw the figure into the icon with a 3×5 dot
matrix face, which at sixteen pixels is about as small as a digit can be drawn and
still read. A figure too long for the space is **shortened rather than
truncated** — `48250` becomes `48k`, `1602000` becomes `1.6M` — so what is left on
screen is still the right number. They also drop the status pip and colour the
figure by health instead, because four characters fill the icon and a pip would
land on top of them.

Two palettes. **System**, the default, draws the mark in the accent Windows is
using, which it reads from the system rather than choosing for itself: an icon
sitting on the taskbar beside the system's own should be the same colour as the
system's highlights, because two different blues on one taskbar read as one of
them being wrong. **Monochrome** drops the hue entirely and keeps colour only for
the two states that need to be noticed, for when the accent clashes with them.

Both can be pinned or left to follow the Windows light/dark mode, which turns the
mark inside out — dark ink on a light halo — for a light taskbar.

The metric behind the shape is one of accounts ready, credits left, requests,
tokens, average latency, tokens per second, or requests in flight, and the
numbers come from the gateway's own API rather than from a second implementation
of the same accounting.

## What the menu offers

Right-clicking the icon opens the shell's own menu, rebuilt from live state every
time, so what it says is what is true at that moment.

![The menu](design/menu-en.png)

Eight rows at the top level, each label two characters:

| Row | What it does |
|---|---|
| **healthy** | the state, with the gateway address beside it |
| **Accounts** | ready over total, and the credits behind them |
| **Panel** | opens the console, with the API key already filled in |
| **Gateway ▸** | the pid, start / stop / restart, the console window, the address and key to copy, autostart, and the folder and file it lives in |
| **Tray ▸** | style, metric, palette, language, and the config file, folder and reload rows |
| **Other ▸** | the live figures, the version block, the seven maintenance actions, refresh and pause |
| **Autostart** | whether the tray starts with Windows |
| **Exit** | quits the tray; the gateway keeps running |

The rows that carry a colour are the two status rows, and they are drawn by
wbtray rather than by the shell. That is not a preference: a menu row the shell
draws is dimmed when it cannot be clicked, and its icon is dimmed with it, so a
health colour in a plain row comes out grey exactly where it carries the most
information.

## What the copy rows offer

Both values an operator otherwise has to go and find are one click away, under
**Gateway ▸** — they are the gateway's own address and key, so that is where an
operator looks for them.

- **Gateway ▸ Address** copies the full gateway URL, scheme and all.
- **Gateway ▸ API key** copies the key the tray is actually using, which for a
  discovered gateway is the one from the gateway's own `config.json`. The key is
  never drawn into the menu — a screenshot of an open menu should not be a
  credential — so the balloon that confirms the copy is the only feedback.

The key row is dimmed when there is no key to copy, rather than reporting success
at copying an empty string.

## Install

Download `wbtray.exe` and run it. That is the whole install.

On first run it writes `wbtray.conf` beside itself. Nothing needs to be
copied into it: the tray reads the gateway's own `config.json` for the API
key and the port, so an unpacked copy pointed at a default installation works
immediately.

### What happens at startup

1. **A `wb2api.exe` process is already running**, whoever started it. Its
   information is read and it is left alone.
2. **No process, but a gateway installed in the folder.** It is started.
3. **Neither.** The newest release is downloaded from GitHub — or from the
   cnb.cool mirror, automatically, if GitHub cannot be reached — unpacked into
   `wb2api/` beside `wbtray.exe`, and started.

The folder ends up looking like this:

```
wbtray/
  wbtray.exe           the tray
  wbtray.conf          its settings
  wb2api/              the gateway, downloaded here
    wb2api.exe
    config.json        the gateway's own settings, written on its first run
    config.example.json
    auths/             account credentials
    data/              runtime state
```

### Adding an account

A gateway that has just started cannot serve anything, because it has no
accounts. The tray raises a balloon saying so and naming the panel; the menu has
an "Add an account" row that opens the same page. Log in through the panel the
way the upstream README describes, and the credentials appear in `auths/`,
where the tray picks them up and starts reporting.

```toml
base_url = http://127.0.0.1:7863   # left at the default, discovery follows the
api_key =                          # gateway's own configuration
discover = true

interval_seconds = 3               # how often the panel is read
timeout_seconds = 5

style = ring                       # ring | bar | spark | mascot | plain
                                   # bartext | text | mascottext
metric = accounts                  # accounts | credits | requests | tokens |
                                   # latency | tps | queue
theme = system                     # system | mono
appearance = auto                  # auto | dark | light
lang = zh                          # zh | en

show_console = false               # start the gateway with a visible window
manage_process = true              # let the tray start and stop it
autostart = false
```

Every one of these is also reachable from the menu, which rewrites the file.

## Updates

The tray checks both projects for new versions every six hours, and the menu
offers a check on demand:

- **The gateway** is downloaded from the upstream release. It is stopped first,
  and the directory replacement carries `config.json`, `auths/` and
  `data/` across untouched — **accounts and settings are never lost** — then
  it is started again.
- **The tray itself** downloads the new build, replaces itself and restarts.
  Windows will not let a running executable be overwritten but does allow it to
  be renamed, so the running image is renamed out of the way, the new one is
  moved into place, the new one is started, and this process exits.

Both downloads are **GitHub first, cnb.cool as the fallback**: if GitHub cannot be
reached at any step the mirror is tried automatically, with nothing for the
operator to switch by hand.

## The cnb.cool mirror

https://cnb.cool/laincat/wbtray is a mirror of GitHub, and builds nothing:

- It pulls every branch and tag from GitHub hourly, with a button on the
  repository page (and an API trigger) to do it now.
- When a version tag arrives, that tag's GitHub release assets are re-uploaded
  as they are — **not rebuilt** — so the files on the two sites are byte-for-byte
  identical.
- The same release also carries **the Windows build of workbuddy2api-panel**, so
  one mirror covers both programs. The upstream project has no cnb.cool
  repository of its own, and without this a reader who cannot reach GitHub would
  have no way to obtain the gateway at all.

## What it does with the gateway process

The tray treats the gateway as something it **observes first and controls
second**. A gateway started by another program, by a scheduled task, or by hand
is found by walking the process table, and is reported as running without the
tray pretending it started it.

Start and stop are offered in both cases, because an operator who asks for "stop"
means it. The distinction that matters is attribution: the menu says whether the
running gateway is the tray's own process or someone else's, so a stop is never a
surprise.

The console window is a separate switch. A gateway started by a double click
shows one, because that is where its log goes; started by the tray it can be
hidden, and shown or hidden again at any time, which is what makes a log readable
while the service keeps running.

## What it deliberately does not do

The tray holds no state of its own about accounts, credits or usage. The gateway
is the single source of truth and its HTTP API is the only thing the tray reads;
there is no second accounting to disagree with the first.

It also does not edit the gateway's configuration file. Every setting it changes
goes through the panel's own API, which validates and hot-applies it, so the tray
cannot leave the gateway in a state the console would refuse.

## Build

```
go build ./cmd/wbtray
```

No third-party dependencies: the module requires nothing, and the Win32 calls are
declared in `internal/winapi`. The tray icon is drawn at run time by a small
software renderer, so there is no icon asset to install, lose or version-skew.

Useful commands while developing:

```
go test ./...                                   # everything, including the layout checks
go run ./cmd/preview -out design/styles-all.png # the sheets above, regenerated
```

The executable's own icon, the one Explorer shows, is a PE resource, and Go can
only attach one through a `.syso` beside the package's sources. That file is
generated from the same renderer the tray uses and is committed, because a release
build should not need a renderer and a generated binary in a build step is one
more thing that can differ between two machines:

```
go run ./cmd/iconbuild -arch amd64
```

## Licence

MIT.
