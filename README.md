# wbtray

A tray program for the
[workbuddy2api-panel](https://github.com/linguo2625469/workbuddy2api-panel)
gateway. It lives in the notification area, shows the pool's health and traffic
there, and can fetch, install, start, stop and update the gateway itself — so the
gateway does not have to be downloaded by hand, kept running by hand, or watched
in a browser.

**English** | [简体中文](ReadMe_zhCN.md)

```
   wb2api  :7863   --->   /healthz, /panel/api/*   --->   wbtray
   (the gateway)                                         (this program)
```

The gateway is the source of truth and the tray holds no copy of it: every figure
comes from the gateway's own HTTP API, so the two can never disagree about the
numbers. There is no second accounting to fall out of step with the first.

## Install

Download `wbtray.exe` and run it. That is the whole install: no runtime, no
installer, no assets. The program is a single file with no third-party
dependencies, and everything it draws — the tray icon, the panel, the console and
the executable's own icon — is drawn at run time.

On first run it writes `wbtray.conf` beside itself and points at the gateway on
`127.0.0.1:7863`. Nothing has to be copied into it by hand: the tray reads the
gateway's own `config.json` for the API key and port, so a copy unpacked next to
an existing gateway works immediately.

### What happens at startup

1. **A `wb2api.exe` process is already running**, whoever started it. Its
   information is read and it is left alone.
2. **No process, but a gateway installed in the folder.** It is started.
3. **Neither.** The newest release is downloaded — from GitHub, or from the
   cnb.cool mirror automatically when GitHub cannot be reached — unpacked into
   `wb2api/` beside `wbtray.exe`, and started.

After it starts, the tray waits for the gateway's API to answer before reporting
on it, because a process that has just launched is not a gateway that is ready,
and reporting "offline" for three seconds every launch looks like a fault rather
than like startup.

The installation ends up looking like this:

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

A gateway that has just started cannot serve anything, because it has no accounts
yet. The tray raises a balloon saying so, and the console's accounts page has the
row that opens the browser panel. Log in there the way the upstream README
describes; the credentials appear in `auths/`, where the tray picks them up and
starts reporting.

## The tray icon

The icon is a readout rather than a logo, so "is anything wrong" is answered
without a click. Its **colour** is the pool's health; its **shape** carries the
metric you choose.

Ten shapes, in four palettes, on both appearances, at the size the taskbar really
draws them — 16 pixels, magnified here:

![Every style at 16 pixels](design/styles-all-16px.png)

| Colour | Means |
|---|---|
| Green | accounts are serving |
| Amber | the gateway is up but nothing can serve: every account is cooling, disabled, or there are none |
| Red | the gateway cannot be reached |
| Grey | refreshing is paused |

The icon draws no plate, and that is a measured decision rather than a
preference: a tray icon sits beside the system's own, and those are bare glyphs.
An earlier version drew a translucent rounded square behind the mark, which on a
dark taskbar came out as exactly the block of colour it was meant to avoid.
Legibility comes from the palette instead: every mark is drawn in an ink the
palette has already checked against the taskbar it will be read on.

| Shape | Draws |
|---|---|
| **Gauge** | the metric as a filled ring |
| **Bars** | the last four buckets of its history |
| **Sparkline** | the last twelve buckets as a smoothed curve, newest point marked |
| **Number** | the reading itself, as a figure |
| **Cat** | the product's own mascot, wearing the health colour |
| **Shield / Prompt / Waves / Nodes / Ring** | the five marks the application icon can also carry, so the tray and the file can wear the same shape |

### The palettes

Four, each a neutral ramp in a light and a dark tone. Colour in this design is
reserved for the states that have to be noticed; everything else is ink.

![The four palettes](design/themes.png)

| Palette | The neutral |
|---|---|
| **Neutral** | the one the design was drawn from: a plain warm-neutral grey |
| **Cool** | a blue-leaning grey — the blacks read as slate rather than as charcoal |
| **Warm** | a brown-leaning grey, which reads as sepia |
| **Contrast** | the surfaces pulled apart: a darker background and a lighter card |

The accent is ink rather than a colour of its own, and that is deliberate. An
earlier version wore the Windows accent, which sounds right — an icon that matches
the system's own — and is not: an accent is chosen to be a highlight, not a status
mark. A dark olive accent measured 1.5:1 against a dark taskbar, so every mark in
the tray was a muddy olive, or a washed-out tan once a contrast correction was
added to rescue it. Colour here is for problems.

The tests check the claim each palette makes: every ink legible against every
surface it is drawn on, by margins of 4.6:1 to 17.5:1.

### The application icon

The executable carries its own mark — an identity rather than a reading, because
the icon in Explorer is written once at build time and a reading baked into it is
a lie by the next day.

![The seven marks](design/appmarks.png)

Seven to choose from, at 16, 32 and 256 pixels on a light and a dark desktop. The
cat is the default, because it is the product's own character and the only one of
the seven that is recognisable among twenty other icons in a folder. Build with
another:

```
go run ./cmd/iconbuild -variant shield   # cat, gauge, shield, prompt, waves, nodes, ring
```

At sixteen pixels the cat is drawn **without its face**. Below about forty pixels
of head an eye is two pixels across, and two specks plus a nose is not a face but
noise; the ears carry the silhouette, which is what makes the shape read as a cat
at any size.

## The panel and the console

**Left click** opens the console — the window, on the page that was last open.
**Right click** opens the panel, a card of wbtray's own rather than the shell's
menu.

![The tray panel](design/panel-en.png)

The reason for a drawn panel is the switches. A native menu can list verbs; it
cannot draw a switch, a sparkline or a row of coloured figures, and those are what
let a panel answer a question instead of offering a command. A state shown as a
position is read without being read, where "Pause" and "Resume" are one row with
two words that have to be told apart.

| Row | What it does |
|---|---|
| **the hero** | the state as a colour, the account being served with, and the credits behind it |
| **the trend** | the last twelve buckets of requests, as a shape rather than an axis |
| **the figures** | requests, latency and throughput, each in its own colour |
| **Pause refreshing** | a switch, not a command |
| **Start with Windows** | a switch, for the tray's own autostart entry |
| **Open console** | the window, on the page the row names |
| **Gateway / Accounts / Models and logs** | the console, opened on that page |
| **Exit** | quits the tray; the gateway keeps running |

Nothing in the panel is a submenu. A row that would need one opens the console on
the page that has it, because a menu nested inside a panel is still a menu, and
not being one is the point.

The shell's own menu is still there as a fallback for a machine where the panel
cannot be created — a tray with no way to reach anything would be worse than a
tray with the wrong-looking menu. It is the classic system menu, nine rows at the
top level with two-character labels, holding the gateway's process control, the
accounts, the maintenance tasks, the appearance settings and the updates.

The console is the window: everything the browser panel offers, in six pages
behind a navigation rail.

![The console](design/console-en.png)

| Page | What it shows |
|---|---|
| **Overview** | the pool's figures, the trend, the pool as a row of state pips, and the seven pool-wide maintenance actions |
| **Accounts** | one row per account, with check-in, balance and revive against each one |
| **Models** | the catalogue: what each costs, how much context it takes, which reasoning efforts it accepts, and what it can do |
| **Logs** | the gateway's own log ring, newest first, coloured by channel |
| **Schedule** | which of the gateway's five scheduled tasks are switched on, and when they run |
| **Config** | the gateway's settings, read only |

The schedule and the config pages are read only, and they say so on the page. Both
live in the gateway's own `config.json`, which the browser console writes; a
second writer would silently overwrite the first, and the one that lost the race
would be the operator's change.

### Copying the address and the key

Both values an operator otherwise has to go and find are one click away, on the
console's config page.

- **Copy address** puts the full gateway URL on the clipboard, scheme and all.
- **Copy key** puts the key the tray is actually using there, which for a
  discovered gateway is the one from the gateway's own `config.json`.

The key is copied rather than drawn: a window with a credential in it is a window
that ends up in a screenshot, and the whole reason this is a button is that it is a
value nobody should have to read off a screen. A gateway with no key yet says so
instead of reporting success at copying an empty string. The write goes straight
to the Win32 clipboard API rather than through `cmd /c clip`, so it neither
spawns a console window nor depends on the current directory or `PATH`.

## Language

The menu, the panel and the console are all bilingual — **Chinese and English** —
and the setting is under the tray's settings block or `lang` in the file. A frame
is laid out in one language from a single table; a test asserts that an English
frame contains no Chinese, because the failure it guards against is exactly the
one that happened: a translated menu beside an untranslated window, which looks
like nothing being wrong.

## Configuration

```toml
base_url = http://127.0.0.1:7863   # left at the default, discovery follows the
api_key =                          # gateway's own configuration
discover = true

interval_seconds = 3               # how often the panel is read
timeout_seconds = 5

style = gauge                      # gauge | bars | spark | number | cat
                                   # shield | prompt | waves | nodes | ring
metric = accounts                  # accounts | credits | requests | tokens |
                                   # latency | tps | queue
theme = neutral                    # neutral | cool | warm | contrast
appearance = auto                  # auto | dark | light
app_icon = cat                     # cat | gauge | shield | prompt | waves |
                                   # nodes | ring — takes effect on the next build
lang = zh                          # zh | en

show_console = false               # start the gateway with a visible window
manage_process = true              # let the tray start and stop it
autostart = false
```

The file is written beside `wbtray.exe` when that directory can be written to,
and in `%APPDATA%\wbtray\` when it cannot — an installed copy under
`Program Files` is the case that decision exists for.

Every setting is also reachable from the menu, the panel and the console, which
rewrite the file. `app_icon` is the one exception: the icon is a PE resource
written at build time, so a choice there is recorded now and picked up by the next
build.

## Updates

The tray checks both projects for new versions every six hours, and the menu
offers a check on demand:

- **The gateway** is downloaded from the upstream release. It is stopped first,
  and the directory replacement carries `config.json`, `auths/` and `data/`
  across untouched — **accounts and settings are never lost** — then it is started
  again.
- **The tray itself** downloads the new build, replaces itself and restarts.
  Windows will not let a running executable be overwritten but does allow it to be
  renamed, so the running image is renamed out of the way, the new one is moved
  into place, the new one is started, and this process exits. The renamed copy is
  deleted on the next launch, which is the first moment nothing is holding it.

Both downloads are **GitHub first, cnb.cool as the fallback**: if GitHub cannot be
reached at any step the mirror is tried automatically, with nothing for the
operator to switch by hand.

## The cnb.cool mirror

https://cnb.cool/laincat/wbtray is a mirror of GitHub, and builds nothing:

- It pulls every branch and tag from GitHub hourly, with a button on the repository
  page (and an API trigger) to do it now.
- When a version tag arrives, that tag's GitHub release assets are re-uploaded as
  they are — **not rebuilt** — so the files on the two sites are byte-for-byte
  identical. The job retries for twenty minutes, because a tag and its assets do
  not arrive together on the other side and a run that 404s once would never run
  again for that tag.
- The same release also carries **the Windows build of workbuddy2api-panel**, so
  one mirror covers both programs. The upstream project has no cnb.cool repository
  of its own, and without this a reader who cannot reach GitHub would have no way
  to obtain the gateway at all. Its version is recorded in
  `workbuddy2api-panel-version.txt`, because the mirrored copy keeps a stable
  name and cannot carry the version in it.

## What it does with the gateway process

The tray treats the gateway as something it **observes first and controls
second**. A gateway started by another program, by a scheduled task, or by hand is
found by walking the process table, and is reported as running without the tray
pretending it started it.

Start, stop and restart are offered in both cases, because an operator who asks
for "stop" means it. The distinction that matters is attribution: the menu says
whether the running gateway is the tray's own process or someone else's, so a stop
is never a surprise.

The console window is a separate switch. A gateway started by a double click shows
one, because that is where its log goes; started by the tray it can be hidden, and
shown or hidden again at any time, which is what makes a log readable while the
service keeps running. Starting it uses its own console rather than a detached
process with no window, because a process with no console cannot be given one
later — the switch would be structurally unable to work.

## What it deliberately does not do

The tray does not edit the gateway's configuration file. Every setting it changes
goes through the panel's own API, which validates and hot-applies it, so the tray
cannot leave the gateway in a state the console would refuse.

## Build

```
go build ./cmd/wbtray
```

No third-party dependencies: `go.mod` requires nothing, and the Win32 calls are
declared in `internal/winapi`. Windows' own font does the text, through GDI,
because that is the one thing a program should take from the system rather than
draw; every shape is drawn by a small software renderer, so there is no asset to
install, lose or version-skew.

Useful commands while developing:

```
go test ./...                                      # everything, including the layout checks
go run ./cmd/iconsheet  -out design/appmarks.png   # the application marks
go run ./cmd/themesheet -out design/themes.png     # the palettes, both tones
go run ./cmd/preview    -out design/styles-all.png # every style, palette, tone and state
go run ./cmd/uipreview  -out design/console-en.png -tab all -lang en
go run ./cmd/uipreview  -out design/panel-zh.png   -tab tray -lang zh
go run ./cmd/iconbuild  -variant cat               # regenerate the .syso
```

`cmd/palettecheck` prints the palette resolved on this machine and checks every
ink against every surface it is drawn on. None of these are part of the program.

### The executable's icon

It is a PE resource, and Go can only attach one through a `.syso` beside the
package's sources. That file is generated from the same renderer the tray uses and
is committed, because a release build should not need a renderer, and a generated
binary in a build step is one more thing that can differ between two machines.

The build checks the result rather than trusting it: a resource section the linker
drops, or an image in a format Windows rejects below 256×256, produces an
executable that builds, runs, and quietly shows the system's generic icon. The
release job reads the built file the way Windows reads it, so that fails the build
instead of shipping.

## Licence

MIT.

