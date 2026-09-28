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

The icon is the readout, so the answer to "is anything wrong" does not need a click.
Its **colour** is the health of the pool, and its **shape** carries whichever metric
you choose.

Ten shapes, in four palettes, on both appearances, at the size the taskbar actually
draws them — 16 pixels, magnified here:

![Every style at 16 pixels](design/styles-all-16px.png)

| Colour | Means |
|---|---|
| Green | accounts are serving |
| Amber | the gateway is up but nothing can serve: every account is cooling, disabled, or there are none |
| Red | the gateway cannot be reached |
| Grey | refreshing is paused |

The icon draws no plate. The mark goes straight onto the taskbar, which is what lets it
sit beside the system's own icons without a block of colour around it.

| Shape | Draws |
|---|---|
| **Gauge** | the metric as a filled ring |
| **Bars** | the last four buckets of its history |
| **Sparkline** | the last twelve buckets as a smoothed curve, newest point marked |
| **Number** | the reading itself, as a figure |
| **Cat** | the product's own mascot, wearing the health colour |
| **Shield / Prompt / Waves / Nodes / Ring** | the five marks the application icon can also carry, so the tray and the file can wear the same shape |

### The palettes

Four, each a neutral ramp in a light and a dark tone. Colour in this design is reserved
for the states that have to be noticed; everything else is ink.

![The four palettes](design/themes.png)

| Palette | The neutral |
|---|---|
| **Neutral** | the one the design was drawn from: a plain warm-neutral grey |
| **Cool** | a blue-leaning grey — the blacks read as slate rather than as charcoal |
| **Warm** | a brown-leaning grey, which reads as sepia |
| **Contrast** | the surfaces pulled apart: a darker background and a lighter card |

The accent is the ink rather than a colour of its own, and that is deliberate. An
earlier version wore the Windows accent, which sounds like the right idea — an icon
that matches the system's own — and is not, because an accent is chosen to be a
highlight rather than to be a status mark. A dark olive accent read at 1.5:1 against a
dark taskbar, so every mark in the tray was a muddy olive or, after the contrast
correction added to rescue it, a washed-out tan. Colour here is for problems.

The tests check the claim each palette makes: every ink legible against every surface
it is drawn on, by margins of 4.6:1 to 17.5:1.

### The application icon

The executable carries its own mark — an identity rather than a reading, because the
icon in Explorer is drawn once at build time and a reading baked into it is a lie by
the next day.

![The seven marks](design/appmarks.png)

Seven to choose from, at 16, 32 and 256 pixels on a light and a dark desktop. The cat
is the default, because it is the product's own character and the only one of the seven
that is recognisable among twenty other icons in a folder. Build with another:

```bash
go run ./cmd/iconbuild -variant shield   # cat, gauge, shield, prompt, waves, nodes, ring
```

At sixteen pixels the cat is drawn **without its face**. Below about forty pixels of
head an eye is two pixels across, and two specks plus a nose is not a face but noise;
the ears carry the silhouette, which is what makes the shape read as a cat at any size.

## What the tray opens

Right-clicking the icon opens a panel of wbtray's own rather than the shell's menu,
rebuilt from live state every time, so what it says is what is true at that moment.

![The tray panel](design/panel-en.png)

The reason is the switches. A native menu can list verbs; it cannot draw a switch, a
sparkline or a row of coloured figures, and those are what let a panel answer a
question instead of offering a command. A state shown as a position is read without
being read, where "Pause" and "Resume" are one row with two words that have to be told
apart.

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

Nothing in the panel is a submenu. A row that would need one opens the console on the
page that has it, because a menu nested inside a panel is still a menu, and not being
one is the point. The shell's own menu is kept as a fallback for a machine where the
panel cannot be created: a tray with no way to reach anything would be worse than a
tray with the wrong-looking menu.

## The console

A left click opens the window. It is the gateway's console: everything the browser
panel offers, in six pages behind a navigation rail.

![The console](design/console-en.png)

| Page | What it shows |
|---|---|
| **Overview** | the pool's figures, the trend, the pool as a row of state pips, and the seven pool-wide maintenance actions |
| **Accounts** | one row per account, with check-in, balance and revive against each one |
| **Models** | the catalogue: what each costs, how much context it takes, which reasoning efforts it accepts, and what it can do |
| **Logs** | the gateway's own log ring, newest first, coloured by channel |
| **Schedule** | which of the gateway's five scheduled tasks are switched on, and when they run |
| **Config** | the gateway's settings, read only |

The schedule and the config pages are read only, and they say so on the page. Both live
in the gateway's own `config.json`, which the browser console writes; a second writer
would silently overwrite the first, and the one that lost the race would be the
operator's change.


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

## The Windows 11 menu study

The menu the tray opens is the shell's own. `cmd/uidemo` draws what it would look like
rebuilt in the Windows 11 design language — rounded surface, hairline border, Fluent
icons, the system's own font and accent — so that question can be answered by looking
rather than by imagining. It is a development tool and not part of the program.

```
go run ./cmd/uidemo                         # both languages, dark
go run ./cmd/uidemo -scene submenu          # a cascade over its parent
go run ./cmd/uidemo -scene states           # every row state on one plate
go run ./cmd/uidemo -dark=false             # the light appearance
go run ./cmd/uidemo -accent '#c30052'       # under a chosen accent
```

It reads the accent Windows is using and the system's own UI font, so the picture is of
this machine rather than of a mock-up. Every scene is drawn once per language, because
the two have different lengths for nearly every row and a layout proven in one is not
proven at all.

What that study established, and what it costs:

- **The surface is cheap.** Rounded corners and a hairline come from the icon renderer
  the tray already has, drawn supersampled and averaged down. A drawn menu adds about
  nothing to the binary — the last one that existed made it 0.04 MB *smaller* than the
  system menu it replaced, because the renderer was there either way.
- **The text is not.** Chinese and the icon glyphs need a rasteriser, and the tray no
  longer has one: the window that needed it was removed with the chart. `cmd/uidemo`
  carries a minimal replacement, and its size is the honest estimate of that part.
- **Mica is not available with drawn content.** A translucent material is a compositor
  effect behind the window, so a window that paints its own background covers it. This
  was measured rather than assumed: every combination of the backdrop attributes is
  accepted and none of them shows through. Rounded corners and the border are what a
  drawn menu can have.

![The Windows 11 menu, dark](design/uidemo/menu-zh.png)

![A submenu over its parent](design/uidemo/submenu-zh.png)

![Every row state](design/uidemo/states-zh.png)

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
