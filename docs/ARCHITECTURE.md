# GoSpot architecture

GoSpot is a Pi-Star–compatible hotspot dashboard and radio stack with **no PHP
anywhere**: the user interface is Svelte 5 + TypeScript, every HTTP and API
handler is Go, and the few operations that need root are small, auditable
Python or Bash helpers reached over `sudo`. On the board it reads and writes
the same files as classic Pi-Star (`/etc/mmdvmhost`, `/etc/dmrgateway`,
`/usr/local/bin`, `/usr/local/sbin`, `/usr/local/etc`, `/etc/dstar-radio.*`),
so an operator finds the same menus, pages, fields and buttons as before.

Russian version: [ARCHITECTURE.ru.md](ARCHITECTURE.ru.md). Build and test
instructions: [BUILD.md](BUILD.md).

---

## 1. Repository layout

| Directory | What lives there |
| --- | --- |
| `panel/` | The Go backend: one binary (`panel`) that provides all three listeners, plus `panel/helper/` with the root helpers and their unit tests |
| `Pi-Star_dash_new/` | The dashboard package: Svelte/TypeScript sources (`src/`), the settings file `gospot.conf`, `debian/` packaging, `scripts/` build tooling, `qa/` test stands, `docs/` parity specs |
| `radio/` | The `gospot-radio` package: radio binaries, service handlers, systemd units, the Python 3 `gospot-watchdog`/`gospot-remote` ports, rebrand and migration tooling |
| `image/` | The Raspberry Pi OS image build (pi-gen stages, first boot, verification, QEMU boot test) |
| `.github/workflows/` | CI that builds and publishes the dashboard `.deb` |
| `Pi-Star_DV_Dash/` | The immutable classic PHP dashboard, revision `10bf34c9`, kept only as a behavioural reference. It is not part of the product, its package or its runtime |

Two Debian packages ship to a board:

* **`gospot`** — the dashboard (Go binary, UI assets, helpers, settings
  conffile, sudoers, three units). Replaces `pistar-dashboard`.
* **`gospot-radio`** — the radio platform (daemons, tools, units, the libraries
  Debian lacks). Replaces `pistar-radio` via `Provides`/`Replaces`/`Conflicts`.

## 2. The settings file is the single source of paths

`Pi-Star_dash_new/gospot.conf`, installed as the conffile
`/etc/gospot/gospot.conf`, is the **only** place that holds paths to binaries
and scripts, radio configuration files, data sets, logs, controller markers,
systemd unit names, installation directories, ports, the deploy target and the
paths of neighbouring repositories. Hard-coding any of those in code, scripts,
units, `sudoers` or tests is forbidden; `sudoers` and systemd drop-ins are
*generated from the settings at package build time*. The only exceptions are
the installed path of the settings file itself and standard OS tools
(`/usr/bin/sudo`, `systemctl`, `journalctl`).

The format is deliberately the intersection of what Bash `source`, Go and
Python can all read: `KEY=value`, one per line, `#` starts a comment line, no
quotes, no spaces, no variable expansion. Runtime values must be clean
absolute paths.

Three independent readers enforce it:

* **Go** — `panel/config.go`. `readSettings` rejects a key that is not
  `[A-Z][A-Z0-9_]*`, a value containing ` `, tab, `"`, `'`, `$`, `` ` `` or
  `\`, and any duplicate key. `applySettings` then *validates every key the
  panel uses* — absolute paths, TCP ports in 1..65535, systemd service names
  matching `^[A-Za-z0-9@_.-]+\.service$`, second ranges, URL schemes and
  trailing `/`, `RADIO_LOG_SOURCE ∈ {files, journal}`,
  `WIFI_BACKEND ∈ {networkmanager, wpa_supplicant}`,
  `HOSELINE_MAX_LISTENERS ∈ 1..32` — collects *all* errors and refuses to start
  on any of them. The file is selected with the `-config` flag
  (default `/etc/gospot/gospot.conf`).
* **Python helpers** — `load_settings()` in `panel/helper/gospot-configure`,
  with the same grammar.
* **Shell** — `gospot-jitter` parses it with a regex rather than `source`, and
  the build scripts `source` it directly.

Crucially, the root helpers only **trust** the file if nobody but its owner can
change it: `gospot-configure` opens it `O_RDONLY|O_NOFOLLOW` and exits with
`unsafe settings file` unless it is a regular file owned by the effective uid
with no group- or other-write bits. Unprivileged runs (tests) may point at
another file through `GOSPOT_SETTINGS`; under `sudo` as root the installed path
is always used, so the environment variable can never redirect a privileged
run.

Because `gospot.conf` is a dpkg conffile, local edits on a board survive a
package upgrade. A newer package therefore cannot assume its keys exist in an
edited copy: `debian/postinst` runs `gospot-conf-merge`, which **appends keys
the installed copy lacks** and never touches lines that are already there.
After editing the file on a board, restart `gospot-panel`, `gospot-wizard` and
`gospot-stage`.

## 3. One binary, three listeners

`panel/main.go` builds one Go program that selects its role from a flag; the
package installs it once at `$DASHBOARD_LIB_DIR/panel` and generates three
units whose `ExecStart` lines differ:

| Unit | Flag | Port | Transport | Who may reach it |
| --- | --- | --- | --- | --- |
| `gospot-panel.service` | `-listen :PANEL_PORT` | `8787` | HTTP | the other two listeners, loopback |
| `gospot-stage.service` | `-operator-listen :OPERATOR_PORT` | `8789` | HTTP | anyone on the LAN (read-only operator view) |
| `gospot-wizard.service` | `-admin-listen :MANAGEMENT_PORT -tls-cert … -tls-key %d/tls-key` | `8788` | HTTPS | administrators, Basic auth |

### 3.1 The status listener (`:8787`)

This is the only part that talks to the radio stack's data. It starts one
long-running log follower and one 5-second collector goroutine, stores an
immutable `*Status` snapshot in an `atomic.Pointer`, and serves it:

* `GET /api/dashboard` — the full dashboard document (`panel/dashboard.go`):
  host and callsign, controller, modes and networks, RX/TX, firmware, per-mode
  link state, Last Heard and Local TX rows with resolved operator and talkgroup
  names, the CSS Tool theme, time zone and current time. This is the single
  endpoint every page polls.
* `GET /api/last_heard.php` — the classic public JSON API, kept at its original
  path for compatibility (`panel/legacy_heard.go`).
* `GET /api/status` and `GET /` — the raw snapshot as JSON and a minimal
  embedded status page (`panel/status.html` via `panel/ui.go`), used for
  first-stage rollout checks. `503` with `Retry-After: 5` before the first
  snapshot exists is normal.

Nothing here writes anything, and no page ever forks a `grep`, `tail` or
`journalctl` of its own: the follower and the snapshot exist precisely so that
page load cost is independent of log size.

### 3.2 The operator listener (`:8789`)

`panel/operator.go` serves the public operator experience. It is deliberately
tiny and read-only:

* **Method allowlist** — only `GET` and `HEAD`; anything else gets `405` with
  an `Allow` header.
* **Static assets** — `operatorStatic` maps `/` and `/kiosk` to `index.html`,
  accepts only paths matching
  `^/[A-Za-z0-9_-][A-Za-z0-9_.-]*(/[A-Za-z0-9_-][A-Za-z0-9_.-]*)*$` with a
  known extension (`.html .js .css .svg .png .ico .woff2 .webp`), resolves
  symlinks on both the asset root and the candidate and refuses anything that
  escapes the root, and refuses non-regular files or files over 16 MiB.
* **Proxies** — `/api/status`, `/api/dashboard` and
  `/api/last_heard.php[?num_transmissions=N]` are fetched from
  `http://127.0.0.1:PANEL_PORT` with a 3-second timeout, a 1 MiB body cap and
  a *shape check*: a dashboard/status reply must parse as a JSON object, a
  heard reply as a JSON array; anything else becomes `502`. A `503` from the
  status listener on the heard route degrades to an empty array instead of an
  error, so the table simply shows nothing.
* **Hoseline** — `/api/hoseline` (WebSocket) and `/api/hoseline/groups`.
* **Operational endpoints** — `/health` (checks `index.html` is readable),
  `/metrics` (one Prometheus counter), `/runtime.json` (tells the page the
  management port, so no port number is baked into the bundle).
* **Classic URL redirects** — `operatorRedirect` keeps old bookmarks working.
  Public pages collapse onto the app (`/index.php`, `/mmdvmhost/lh.php`,
  `/mmdvmhost/localtx.php`, `/mmdvmhost/repeaterinfo.php` → `/`; `/kiosk/` →
  `/kiosk`). Admin pages redirect *across hosts* to
  `https://<host>:MANAGEMENT_PORT/manage/<fragment>`, e.g.
  `/admin/configure.php` → `…/manage/#configuration`,
  `/admin/expert/` → `…/manage/#expert`. The host is taken from the request,
  stripped of its port, and rejected if it contains `/ \ @ ? #`, CR, LF, tab or
  a space — the redirect can never be pointed somewhere else by a crafted
  `Host` header.
* **`/kiosk`** — the wall-screen view of the same single-page app: the
  operator port serves the same bundle, and the frontend router mounts
  `Kiosk.svelte` for that path.

Every response carries `Cache-Control: no-store`,
`X-Content-Type-Options: nosniff` and `Referrer-Policy: no-referrer`.

### 3.3 The HTTPS management listener (`:8788`)

`panel/wizard.go` is the administrative surface. Routes are registered at
`/{$}`, `/manage` and `/manage/`, and `wizardHandler` applies, in order:

1. **TLS required.** `r.TLS == nil` → `503`. The server is created with
   `ListenAndServeTLS` and `MinVersion: TLS 1.2`.
2. **Credential present.** The password hash is read once at start-up from
   `$CREDENTIALS_DIRECTORY/admin-password`, which systemd populates through
   `LoadCredential=` from `$WIZARD_CONFIG_DIR/admin-password`. The private TLS
   key is loaded the same way (`%d/tls-key`), so neither secret is ever a
   command-line argument or readable by the service's own user through a path.
   No credential → `503`.
3. **HTTP Basic auth.** User `gospot` (classic Pi-Star used `pi-star`), SHA-256
   of the password compared with `crypto/subtle.ConstantTimeCompare`. Failure
   returns `401` with
   `WWW-Authenticate: Basic realm="GoSpot", charset="UTF-8"`.
4. **Route dispatch.** Only after the two checks above. Every handler in
   `panel/` carries the comment *"Dispatch only after wizardHandler's TLS and
   Basic Auth checks"* — the authentication is done once, centrally, and the
   section handlers never repeat or weaken it.

Headers on every response: `Cache-Control: no-store`,
`X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer` and a
Content Security Policy. The API default is
`default-src 'none'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'`.
The served app page (`serveWizardAsset`) relaxes it to
`default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self'; connect-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'`
— `'unsafe-inline'` for styles only, because the classic markup colours table
cells with `style` attributes; scripts stay same-origin. When *Expert > SSH
Access* is available, `frame-src http://*:<port> https://*:<port>` is appended
for the shellinabox client, with the port read from `SHELLINABOX_DEFAULTS`.

Asset serving is hardened against a package upgrade happening mid-request:
`serveWizardAsset` opens the asset directory with
`O_RDONLY|O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY`, then `openat`s `assets/` and the
file relative to those descriptors, so no path component may be a symlink and
no component can be swapped underneath it. Bundle names must match
`^/manage/assets/([A-Za-z0-9_-][A-Za-z0-9._-]*\.(js|css|png))$`.

### 3.4 CSRF: cookie + header + Origin

`panel/csrf.go` holds the shared request checks for the management API. A
write is accepted only when **all** of these hold:

* the request arrived over TLS (`r.TLS != nil`);
* there is exactly one `Origin` header and it equals `https://` + the request's
  own `Host` (case-insensitively);
* exactly one `__Host-gospot-csrf` cookie is present and its value is 64 hex
  characters (`archiveHex`);
* the `X-GoSpot-CSRF` header carries the same value, compared with
  `subtle.ConstantTimeCompare`.

`archiveHeader` returns a value only when a header appears exactly once, so a
duplicated `Origin` or `X-GoSpot-CSRF` fails closed rather than letting the
first or last win.

The token itself is minted by `editorToken` (`panel/services.go`) on the first
`GET` of an editor: 32 bytes from `crypto/rand`, hex-encoded, set as
`__Host-gospot-csrf` with `Path=/`, `Secure`, `HttpOnly`,
`SameSite=Strict`. The `__Host-` prefix means a browser will only accept it
over HTTPS, for the exact host, with `Path=/` — it cannot be set by a
subdomain or over plain HTTP. `maintenancePost` wraps the check for every JSON
API: CSRF, then `Content-Type: application/json`, then a bounded decode.

The small built-in HTML wizard form (`GET /` on the management port) uses the
same token as a hidden `csrf` form field with
`Content-Type: application/x-www-form-urlencoded`, an 8 KiB body cap, and the
same single-`Origin` check.

### 3.5 Request paths

```
                         LAN
                          |
      +-------------------+--------------------+
      |                                        |
 http://<board>:8789                   https://<board>:8788
 gospot-stage.service                  gospot-wizard.service
 user gospot-stage                     user gospot-wizard
 (operator + /kiosk)                   (management, Basic auth + CSRF)
      |                                        |
      |  GET only                              |  TLS 1.2+ -> credential ->
      |                                        |  Basic auth -> route
      |                                        |
      +-- /, /kiosk, /assets/*  (static, symlink-safe)
      |                                        +-- /manage/, /manage/assets/*
      |                                        |      (openat, no symlinks)
      +-- /api/hoseline        ---------+      +-- /manage/api/hoseline ---+
      +-- /api/hoseline/groups          |      +-- /manage/api/...         |
      |                                 |      |     configure, wifi,      |
      +-- /api/dashboard  ----+         |      |     services, protocols,  |
      +-- /api/status         |         |      |     expert/*, links/*,    |
      +-- /api/last_heard.php |         |      |     bm/*, calibration,    |
      |      (proxy, JSON     |         |      |     backup/classic,       |
      |       shape-checked)  |         |      |     livelog, log, power,  |
      +-- /health /metrics    |         |      |     update, system        |
      +-- /runtime.json       |         |      |                           |
      +-- /admin/*.php -- 302 ----------|------+-- /manage/#<fragment>     |
                               |        |      |                           |
                               v        |      +-- /manage/api/dashboard --+
                   http://127.0.0.1:8787|            (proxy)         |     |
                   gospot-panel.service |                            |     |
                   user gospot-panel    |<---------------------------+     |
                   +-------------------------------------+                |
                   | GET /api/dashboard  /api/status     |                |
                   | GET /api/last_heard.php  GET /       |                |
                   +-------------------------------------+                |
                              |                   ^                       |
                     5 s collector          one log follower              |
                              |                   |                       |
          +-------------------+-------------------+----------------+       |
          |                   |                   |                |       |
   /etc/mmdvmhost     /var/log/gospot/     /usr/local/etc/    /proc, /sys  |
   /etc/dmrgateway    MMDVM-<date>.log     DMRIds.dat         systemctl    |
   (INI, re-read      DMRGateway-<date>    TGList_*.txt       show         |
    on change)        .log  (tail), or     *_Hosts.txt                     |
                      journalctl -f        (cached per version)            |
                                                                           |
   writes (management listener only)                                       |
          |                                                                |
          v                                                                v
   /usr/bin/sudo -n $DASHBOARD_LIB_DIR/gospot-{configure,config,         wss://
     power,native-update,modem,calibrate,jitter}  <verb>          hose.brandmeister
          |   stdin: JSON   stdout: JSON   exit 3 = stale                 .network
          v                                                        (relayed, one
   root: read /etc/gospot/gospot.conf (trusted only if root-owned           upstream
         and not group/world-writable)                                 per listener)
         revision check -> atomic replace -> system ops ->
         systemctl restart ... -> rollback on any failure
```

## 4. Privilege model

Every Go service runs **unprivileged**. The dashboard package's `postinst`
creates three system accounts with no login shell and no home directory —
`gospot-panel`, `gospot-wizard`, `gospot-stage` — and asserts none of them is
uid 0. The status and operator units add sandboxing: `NoNewPrivileges=yes`,
`CapabilityBoundingSet=` (empty), `PrivateDevices`, `PrivateTmp`,
`ProtectSystem=strict`, `ProtectHome`, `ProtectKernelTunables`,
`ProtectKernelModules`, `ProtectControlGroups` and a restricted
`RestrictAddressFamilies` (the operator unit also caps tasks and memory). The
management unit runs with `UMask=0077` and receives its two secrets only
through systemd credentials. Only `gospot-panel` gets
`SupplementaryGroups=systemd-journal`, and only so the log follower can read
the radio journal.

Nothing else is privileged. **Every write to the system goes through a root
helper invoked as `/usr/bin/sudo -n <helper> <fixed verb>`**, and the sudoers
file (generated at package build from the settings, installed mode `0440` as
`/etc/sudoers.d/gospot`) lists the *exact* command lines — not the helper
alone:

| Helper (in `$DASHBOARD_LIB_DIR`) | Verbs permitted by sudoers | Purpose |
| --- | --- | --- |
| `gospot-configure` | `read`, `apply`, `full-read`/`full-write` for exactly `dmrgateway`, `gospot-remote`, `wpaconfig`, `cron`, `rssidat`, `keys-read`, `keys-write`, `backup`, `restore`, `css-write`, `css-reset`, `factory-reset`, `wifi-read`, `wifi-write`, `wifi-scan`, `wifi-action up`/`down`/`reset` | the Configuration page, Expert Full Edit, API keys, CSS Tool, backup/restore, factory reset, Wi-Fi |
| `gospot-config` | `read`, `apply`, `rollback`, `services-read`, `services-apply`, `protocols-read`, `protocols-apply`, `bm-peer` | the first-stage station/network settings, DMR services and protocols |
| `gospot-power` | `reboot`, `shutdown` | the Power page |
| `gospot-native-update` | `start` | the Update page |
| `gospot-modem` | `list`, `flash *` | Expert > FW |
| `gospot-calibrate` | `run`, `restore` | the Calibration page |
| `gospot-jitter` | `run BM`, `run DMR+`, `run HB` | the Expert Jitter Test |

Notes that matter when reading the sudoers file:

* The verb is part of the permitted command, so `gospot-configure delete-everything`
  is not merely unimplemented, it is not permitted.
* Rules that carry secrets are marked `NOLOG_INPUT: NOLOG_OUTPUT:` so sudo's
  I/O logging never records passwords, PSKs or API tokens.
* `gospot-modem flash *` is the one wildcard, and the helper closes it: it
  accepts a type only if `gospot-modemupgrade list` prints that exact line
  (`grep -qxF`).
* The sudoers template contains `@DASHBOARD_LIB_DIR@`; the build substitutes it
  and **fails the build** if any `@` placeholder is left
  (`unresolved sudoers placeholder`). `postinst` then runs
  `visudo -cf /etc/sudoers.d/gospot` before enabling anything.

Each helper re-reads the trusted settings file itself rather than taking paths
from its caller, so a compromised dashboard service cannot make a root helper
act on a different file. Argument lists are passed to `subprocess.run`/`exec`
as vectors, never through a shell, with `cwd="/"` and a fixed
`PATH=/usr/sbin:/usr/bin:/sbin:/bin`.

Long-running helpers are detached rather than held open by an HTTP request:
`gospot-power`, `gospot-modem flash` and `gospot-native-update start` each use
`systemd-run --unit=<name>` so the work survives the request, and refuse to
start a second run while that unit is active (exit 3, "already running").

### 4.1 Revision checks and rollback

`gospot-configure` is the transactional one. Its `read` returns a
**revision**: the SHA-256 over every configured file's short name and its full
text (missing files hash as the literal `\0missing`), so the revision changes
if any file changes, appears or disappears.

`apply` takes `{revision, changes, restart, system}` on stdin and:

1. takes an exclusive `flock` on `$DASHBOARD_STATE_DIR/configure.lock`;
2. re-snapshots the files and **compares the revision**; a mismatch raises
   `Stale`, which prints `{"error": …, "stale": true}` and exits **3**. The Go
   side maps exit 3 to `errConfigStale` and the HTTP layer answers `409` with
   *"The configuration changed since this page was loaded; reload it"*;
3. validates the request (`plan`) and computes the new text of each target file
   — values are single-line strings, and comments, key ordering, ownership and
   modes are preserved;
4. refuses to run at all if `$DASHBOARD_STATE_DIR/configure-pending.json`
   already exists (`interrupted operation; inspect private pending backup`),
   then writes the pre-change text of every target into that file, mode `0600`;
5. replaces each file atomically — `mkstemp` in the same directory, write,
   restore uid/gid and mode from the original (`0644` for a new file), `fsync`,
   `os.replace`. A file that cannot be renamed over (a bind mount such as
   `/etc/hosts` in a container) is rewritten in place instead;
6. runs the system operations (hostname, time zone, controller marker, flag
   files, passwords, Auto AP, UPnP, …);
7. restarts the requested units, skipping any whose `systemctl show -p LoadState`
   is not `loaded` — the classic page restarts everything, and a board simply
   has not installed every mode;
8. on **any** exception: logs only the exception *type* (never a value),
   restores every file it had written, in reverse order, restarts the same
   units again, removes the pending file, and raises
   `operation failed; previous settings restored` (exit 2).

The pending file is the deliberate manual gate: if the helper is killed between
steps 5 and 8, mutations stop until an administrator inspects
`configure-pending.json`, reconciles the files and removes it.

The simpler `gospot-config` keeps a different flavour of the same idea: it
locks `$DASHBOARD_STATE_DIR`, replaces the two fixed INIs atomically, restarts
changed services gateway-first, and keeps the last successful
content-changing apply as a snapshot that `rollback` restores (a successful
rollback consumes it; a no-op apply does not replace it). Its snapshots and
pending files are mode `0600` because they may contain credentials.

The per-file editors use the same pattern at file granularity. `full-read`
returns `sha256(text or "\0missing")` and `full-write` refuses a mismatching
revision; the CSS Tool and both Wi-Fi backends compute their own revision the
same way (for NetworkManager, over the listed profiles — ids, names, SSIDs,
key management — and the country file, *never* over secrets).

## 5. How a configuration write works, end to end

The Configuration page is the largest surface, and it is built as a small
framework so that each classic section is one file. `panel/configure_page.go`
defines the shapes; the sections live in `configure_control.go`,
`configure_dmr.go`, `configure_dstar_ysf.go`, `configure_modes.go` and
`configure_system.go`, each registering a `cfgSectionImpl` with an `Order`, a
`Build` (what the page shows, returning `nil` when the classic page hides the
section) and an `Apply` (what the posted form changes). They follow
`Pi-Star_dash_new/docs/configure-spec/`, a line-by-line specification of the
reference `configure.php`.

### Read

```
GET /manage/api/configure
  -> readConfigState(run)
       -> sudo -n gospot-configure read
            -> {revision, files{name→section→key→value}, layout, system}
  -> buildCfgPage(ctx)      # every section's Build, in Order
  -> {csrf, revision, sections}
```

`layout` preserves file order so an editor can render a file the way it is
written. `system` carries the hostname, the time zone (read from the
`/etc/localtime` symlink) and the controller markers and flag files.

The `read` result is **cached in the Go process** while every watched file
keeps its stat signature. `configWatch` is assembled from the settings — every
`CONF_*`, `MARKER_*` and `FLAG_*` key plus `HOSTNAME_FILE` — with
`/etc/localtime` appended, and the signature is each path's modification time
(nanoseconds), size, mode and symlink target. Any helper verb other than
`read` drops the cache. A stale cache entry can never cause a wrong write,
because `apply` and every editor check the revision on the root side.

### Plan

A posted form is turned into a change set by `planCfgApply`, and this is where
the two rules that keep the page honest live:

1. **An untouched form writes nothing.** The appliers are run twice: once on
   the posted form, and once on a *base* form in which every control that the
   page actually rendered is reset to the value it displayed
   (`cfgDisplayed`). Anything the base run would also have staged — classic
   defaults and normalisations that the appliers stage on *any* save — is
   dropped from the real plan. `dropStored` then drops anything already equal
   to what is on disk. The result: opening the page and pressing *Apply
   Changes* without editing a field produces an empty plan, writes no file and
   restarts no service.
2. **An edited file still gets the classic migrations.** Once a file is being
   written anyway, the changes the classic page would also make to keys that
   file *already has* are put back in — for example dropping YSFGateway's old
   `[aprs.fi]` section — so a file is never left half-migrated. Defaults for
   keys that are *missing* are still not added, so a minimal configuration
   stays minimal.

`finishCfgPlan` then decides the restart. Any file change, or any system
operation other than the "quiet" ones (`password`, `autoap_psk` — the classic
password and Auto AP PSK forms restart nothing), schedules the full classic
restart list `cfgRadioUnits`: gateways and bridges first, controllers
(`mmdvmhost.service`, `dstarrepeater.service`) last.

A section's `Apply` may return a `cfgError`, which refuses the whole Apply and
is shown to the operator as HTTP `422` with that exact message.

### Apply

```
POST /manage/api/configure   {revision, form}
  -> archiveCSRF (TLS + Origin + __Host-gospot-csrf + X-GoSpot-CSRF)
  -> planCfgApply
  -> empty plan?  -> 200 {"ok":true,"changed":[]}            (nothing happens)
  -> sudo -n gospot-configure apply  <- {revision, changes, restart, system}
       revision mismatch -> exit 3 -> 409 "reload the page"
       validation/apply failure -> exit 2, files restored -> 422 with the reason
       success -> 200 {"ok":true,"changed":[…],"restarted":[…]}
```

The Go wrapper gives the write a 3-minute timeout and, importantly,
`context.WithoutCancel`: a browser tab closed in the middle of a restart must
not abort the transaction. The management handler additionally serialises
configuration work through a one-slot channel — a second concurrent apply gets
`503 "Настройки заняты. Повторите позже."` rather than racing — and a non-read
action gets a 110-second budget that survives the client going away.

Backup and restore ride the same helper: `GET /manage/api/backup/classic`
streams the helper's `backup` output as a classic-Pi-Star-compatible zip named
`GoSpot_Config_<host>_<YYYY-Mon-DD>.zip`, and `POST` accepts at most 256 KiB of
`application/zip` and hands it to `restore`. Factory reset requires the literal
server-side confirmation string `RESET`.

## 6. Data sources

### 6.1 Log tailing

`RADIO_LOG_SOURCE` selects one of two followers, and both feed the *same*
consumers (`panel/journal.go`'s `journalState` via `applyRecord`), so the rest
of the code does not know which is in use:

* **`files`** (`panel/logtail.go`, the Pi-Star layout). MMDVMHost and
  DMRGateway write daily files named after the UTC date in `$GOSPOT_LOG_DIR`
  (`FileRotate=1`): `MMDVM-2026-11-27.log`, `DMRGateway-2026-11-27.log`. One
  poller per file wakes every 500 ms, reads whatever was appended since the
  last offset, splits complete lines and keeps the partial tail for next time.
  On first attach it starts 512 KiB from the end (and drops the first,
  mid-line, fragment); if the file shrank or was replaced it restarts at zero.
  At a UTC midnight it drains yesterday's file before switching, and on the
  very first attach it reads yesterday's tail so history is present right after
  midnight. Only lines matching `^[A-Z]: YYYY-MM-DD HH:MM:SS.mmm ` are
  emitted, with the timestamp parsed in UTC.
  `primeFirmwareFiles` additionally scans yesterday's and today's whole file
  (bounded at 64 MiB) for the last `MMDVM protocol version` banner, because
  that line is written once at start-up and is normally far above the tail —
  this is the native equivalent of the classic `getDVModemFirmware()`.
* **`journal`** (`panel/journal.go`). One long-running `journalctl -f` for
  upstream builds that log to systemd-journald under `UNIT_MMDVMHOST` and
  `UNIT_DMRGATEWAY`.

A page refresh never starts a log scan; there is exactly one follower per
process.

### 6.2 Heard parsing

`panel/heard.go` and `panel/heard_modes.go` parse call records per mode from
the followed lines. MMDVMHost's `DMRSlot.cpp` logs a voice header or a late
entry at call start and a data header (with a block count) for data; the
parsers treat lookup values inside those messages as untrusted and publish
only bounded callsign/ID tokens. `panel/collector.go` only accepts an
application event at the *beginning* of a log record, never arbitrary text
embedded in an ID lookup, an MQTT payload or a configuration value.
`panel/legacy_heard.go` renders the same rows in the classic
`last_heard.php` JSON shape.

D-Star has its own sources (`panel/dstar.go`, `panel/dstar_links.go`):
ircDDBGateway writes `Links.log`, `STARnet.log` and `Headers.log` into
`$GOSPOT_LOG_DIR`, and each file is re-parsed only when its size or
modification time changes — `Headers.log` incrementally.

### 6.3 Host lists and the ID database

Both caches key on a *file version* — path, modification time and size — not on
a timer, because `HostFilesUpdate.sh` rewrites these files roughly once a day
and they are otherwise static.

* `panel/hostcache.go` is the generic one: `cachedHostFile[T]` stats the file,
  returns the cached parse if the version matches, and otherwise parses at most
  8 MiB through a caller-supplied function. It is used for the
  `TGList_BM.txt`, `TGList_P25.txt` and `TGList_NXDN.txt` talkgroup lists and
  the per-mode `*_Hosts.txt` lists.
* `panel/names.go` handles `DMRIds.dat`, which is a different problem: around
  250 000 lines. It is parsed exactly the way MMDVMHost's `CUserDB` does —
  fields split on `,`, TAB, CR or LF so empty fields keep their column number,
  an optional first-line header naming the columns
  (`RADIO_ID,CALLSIGN,FIRST_NAME,…`), otherwise columns 0/1/2, `#` comments,
  and a repeated ID taking the later line. The result is stored **without
  pointers** so the garbage collector never scans it: two integer maps (DMR ID
  → name offset, packed callsign → name offset) over one length-prefixed
  `[]byte` of interned names. That is about 13 MiB for 250 k IDs instead of
  ~36 MiB of string maps. Callsigns are packed into a `uint64` in base 37
  after upper-casing and stripping a portable/SSID suffix or a D-Star module
  (`M1ABC/P`, `MW0MWZ-1`, `M1ABC  B`). Names are bounded to 40 runes and
  stripped of non-printable characters, because the file comes from the
  internet.
  Because a re-parse takes real time, `backgroundUserDB` serves the previous
  table while a changed version is parsed in a goroutine, so the daily host-file
  update never stalls an `/api/dashboard` build.

`hostNames.target` resolves what the heard row's destination means: `TG nnn`
looks up the mode's TG list; for DMR a bare number is tried as a talkgroup
(so 9990 shows as *Parrot*) and then as a user ID.

### 6.4 BrandMeister and TGIF

`panel/brandmeister.go` and `panel/links.go` implement the admin link
managers. Gateway-side link control (YSF, P25, NXDN, M17, DMR+) is driven over
each gateway's **UDP remote-command port** — what the classic `RemoteCommand`
binary does — so those operations need no root helper at all. BrandMeister
self-care (static talkgroups, dynamic group reset, QSO drop, peer and profile
views) goes to the BrandMeister HTTP API with the key from `BM_API_KEY_FILE`;
`panel/brandmeister.go`'s `bmNumber` tolerates the API sending numbers either
as JSON numbers or as strings.

`panel/tgif.go` is the native port of the classic TGIF panels. Two different
TGIF services are involved, and the settings keep them apart:

* `TGIF_API_URL` (must be `https` and end with `/`) — the Static/Dynamic
  Talkgroup API. The token from `TGIF_API_KEY_FILE` is sent **only** to this
  URL, as a `Bearer` header, and is never logged and never part of a reply.
* `TGIF_SESSION_URL` (must end with `/`) — the legacy token-less link manager,
  `GET <url><id>/<slot>/<talkgroup>`.

API key files live under `/etc` at mode `0600` owned by
`DASHBOARD_ADMIN_USER` (`gospot-wizard`), and are written through
`gospot-configure keys-write`.

### 6.5 The Hoseline relay

The BrandMeister Hoseline player sits under *Local RF Activity* and is listen
only. A browser cannot open the spotter WebSocket itself, because the spotter
checks `Origin` — so the board relays (`panel/hoseline.go`):

1. **Token.** `spotterToken` fetches `HOSELINE_PAGE_URL` (bounded at 4 MiB) and
   extracts the public token the page embeds. It looks for the page's own
   construction `new X("<spotter URL>", token)` — the spotter URL from the
   settings is quoted into the regexp — takes the identifier from that call,
   then finds `<identifier>="…"` and validates the value against
   `^[A-Za-z0-9._~+/=:@-]{1,1000}$`. The token is cached for 15 minutes, and
   forgotten on a refused handshake, since a stale token is the usual cause.
2. **Talkgroup list.** `offered` is the hotspot's own BrandMeister profile
   talkgroups (static plus dynamic, read from the public profile — no API key
   is read or sent, and only when DMR is enabled and the master name starts
   with `BM`; cached 5 minutes, 1 minute after an error) plus
   `HOSELINE_TALKGROUPS`, deduplicated and sorted. A requested talkgroup must
   be in that list.
3. **Admission.** `GET /api/hoseline` requires exactly one `Origin` equal to
   this listener's own origin, exactly one `tg` query parameter matching
   `^[1-9][0-9]{1,7}$` within 91..99999999 and present in `offered`, and a free
   listener slot. `HOSELINE_MAX_LISTENERS` (1..32) is counted **per dashboard
   service**, so the operator and management listeners each have their own
   budget, with one upstream connection per listener. A full relay answers
   `503` with `Retry-After: 30`.
4. **Relay.** The board dials `HOSELINE_SPOTTER_URL?token=…` with the page's
   origin and the `spotter` subprotocol, sends the MessagePack subscription
   `[1,[TG]]`, and forwards: spotter message type `11` → a JSON
   `{"type":"start","source":…}` text frame, `13` → `{"type":"end"}`, `20` →
   the μ-law audio payload as a binary frame (bounded at 8000 bytes, one second
   at 8 kHz). Both directions get a 20-second keepalive ping and a 75-second
   idle timeout; either side going away ends the session, and the browser is
   told why in a `{"type":"error","message":…}` frame before a close.

Two small purpose-built codecs support this, rather than a dependency:

* `panel/websocket.go` — RFC 6455 with no extensions and no compression, only
  what the relay needs: `wsUpgrade` for inbound browsers, `wsDial` for the
  outbound spotter connection (a client masks its frames), per-message size
  caps, a read deadline renewed before every frame, and `wsHandshakeError`
  carrying the refused status so a stale token can be distinguished.
* `panel/msgpack.go` — a decoder for exactly the spotter protocol: integers to
  `int64`, `str` to `string`, `bin` to `[]byte`, arrays to `[]any`, maps to
  `mpMap`, ext to `mpExt`, a depth limit of 16, and a strict requirement that
  one value occupies the whole buffer. Encoding is only
  `hoselineSubscribe(tg)`.

### 6.6 System information and services

`panel/sysinfo.go` is the native scope of `admin/sysinfo.php`: host health,
RAM and the root filesystem, deliberately *not* enumerating arbitrary mounts,
not running gateway binaries to scrape banners, and not exposing addresses or
gateway configuration. `panel/services.go` reports service state through
`systemctl show`. `panel/logview.go` and `panel/maintenance.go` serve Live
Logs (an offset-based tail, 256 KiB per chunk, negative offset meaning "start
at the current end" as the classic pages do) and the Power/Update pages.

## 7. Frontend

Svelte 5 + TypeScript, built by Vite (`base: './'`, `target: 'es2022'`,
`assetsInlineLimit: 0` so nothing is inlined and the CSP stays strict). The
output is plain static files served by the Go listeners; there is no Node
runtime on the board.

**Classic markup and the theme.** The design is the standard Pi-Star one: the
same table structure, block order, labels, colours and colouring rules as
`index.php`, `mmdvmhost/*.php` and `css/pistar-css*.php`. Deviations are
allowed only for security or usability (horizontal scrolling of wide tables on
a phone). The CSS Tool palette is carried as **CSS custom properties** rather
than generated stylesheets: `panel/theme.go` reads `$GOSPOT_CSS_FILE`
(`/etc/gospot-css.ini`), falls back to the classic palette for any value that
is not a 3- or 6-digit hex colour, and publishes the nine colours in
`/api/dashboard` as `Theme`; `src/classic.ts`'s `themeStyle`/`applyTheme`
validate each one again in the browser and set `--ps-page`, `--ps-content`,
`--ps-banners`, `--ps-banner-text`, `--ps-banner-drop`, `--ps-head-drop`,
`--ps-text`, `--ps-even`, `--ps-odd` on the root element. `ClassicDashboard`,
`GoSpotFrame`, `Configure`, `Wifi` and `Kiosk` consume only those variables, so
a theme change is one style recalculation and never a rebuild. The same module
also holds the classic per-cell colour rules (loss, BER, RSSI, hang-time
colours per mode) and the callsign lookup link builder, which switches between
RadioID and QRZ from the `[Lookup] Service` setting.

**Routing.** `src/main.ts` picks an entry by location, and the admin side is a
**hash router** so the whole management UI is one same-origin document:

```
/manage…  -> hash router: ''|admin → ClassicDashboard(admin)
                          #logs #power #update #configuration #backup
                          #wifi (and #wifi-conf)
                          #calibration (and #calibration-debug)
                          #expert, #expert-<page>      → Expert(page)
                          anything else                → AdminWorkspace
/kiosk    -> Kiosk.svelte
/#console -> App.svelte
/         -> ClassicDashboard.svelte
```

Every page is a dynamic `import()`, so one route's code is not downloaded for
another. The router guards against a slow import racing a fast navigation with
a generation counter, and unmounts the previous component before mounting the
next. `src/routes.ts` is the operator-facing catalogue of destinations taken
from the reference dashboard — each entry opens a *page*, never a command.

**The kiosk.** `/kiosk` is a full-screen wall display for a TV next to the
hotspot (1920×1080, 1280×720 and portrait 1080×1920). It polls
`/api/dashboard` once a second, stops polling on a hidden tab, and shows an
explicit *Connection lost* state with recovery. On air it shows the callsign,
the operator name, the target with its talkgroup name, mode, slot, RF or
network and a live duration counter, with BER and signal added for RF after
the transmission ends; otherwise *Listening — last call N min ago*, with the
last ~10 calls below. It uses the CSS Tool palette as accents, no external
fonts or CDNs, no scrolling and no hover-only information. The `name` and
`targetName` row fields exist for it; no pre-existing field was changed.

**`classic-entry.ts`** mounts the classic shell into a **shadow root** so the
reference stylesheet cannot leak into the app, and only in a top-level window.

## 8. The radio package (`radio/`)

`gospot-radio` provides the Pi-Star layout: programs in `$GOSPOT_BIN_DIR`
(`/usr/local/bin`), scripts and service handlers in `$GOSPOT_SBIN_DIR`
(`/usr/local/sbin`), data sets in `$GOSPOT_HOSTS_DIR` (`/usr/local/etc`), file
logs in `$GOSPOT_LOG_DIR` (`/var/log/gospot`), configuration files without an
`.ini` suffix in `/etc`, and controller markers `/etc/dstar-radio.*`.

**Contents.** 21 PS4 radio programs that do not need wxWidgets (MMDVMHost,
DMRGateway, the YSF/P25/NXDN/M17/DAPNET/DGId/APRS gateways, the cross-mode
bridges, the parrots, MMDVMCal, RemoteCommand, MobileGPS,
`teensy_loader_cli`, `platformDetect.sh`), modem firmware images, the reusable
Pi-Star `sbin` tools renamed to `gospot-*`, the three shared libraries Debian
does not ship (`libwiringPi`, `libwiringPiDev`, `libArduiPi_OLED`), and the
default configuration archive. Displays (`NextionDriver`,
`MMDVMHost_Adafruit`) and the apt/dpkg remount wrappers are deliberately not
shipped, and no script that touches PHP, nginx, `.htpasswd` or
`/var/www/dashboard` is installed — those functions were rewritten in Go or as
PHP-free scripts.

**D-Star rebuilt for wxWidgets 3.2.** The PS4 D-Star daemons were built
against wxWidgets 3.0, which Bookworm and Trixie no longer ship, so
`radio/build-dstar.sh` rebuilds nine daemons from pinned upstream revisions in
an armhf Debian container: `ircddbgatewayd`, `timeserverd`, `starnetserverd`,
`remotecontrold`, `timercontrold`, `aprstransmitd`, `texttransmitd`,
`voicetransmitd` from `g4klx/ircDDBGateway` (the last revision before MQTT,
which still has StarNetServer) and `dstarrepeaterd` from
`g4klx/DStarRepeater`. There is **no source patching**: Pi-Star compatibility
comes entirely from make variables — `BUILD=release`, `CONFDIR=/etc`,
`LOGDIR=$GOSPOT_LOG_DIR`, `DATADIR=$DSTAR_IRCDDB_DATA_DIR` or
`$DSTAR_REPEATER_DATA_DIR` — so `/etc/ircddbgateway` and `/etc/dstarrepeater`
keep the Pi-Star format. The AMBE voice data (`*.ambe`, `*.indx`) is installed
alongside the daemons, while the host lists stay in `$GOSPOT_HOSTS_DIR` with
symlinks (`DPlus_Hosts.txt`, `DExtra_Hosts.txt`, `DCS_Hosts.txt`,
`CCS_Hosts.txt`, `XLXHosts.txt`) pointing there so `HostFilesUpdate.sh` owns
them. Every built binary is `strip`ped and checked with `ldd` for unresolved
libraries, and `build-deb` refuses to package a D-Star binary that still
contains a Pi-Star path or whose `dstarrepeaterd` does not log to
`$GOSPOT_LOG_DIR`.

**Service handlers and units.** Pi-Star drives daemons through handler scripts
named `<name>.service` in `$GOSPOT_SBIN_DIR` that take `start|stop|restart`;
GoSpot keeps that and wraps each in a generated systemd unit of the same base
name in `$SYSTEMD_UNIT_DIR` (`/lib/systemd/system`) whose `ExecStart`,
`ExecStop` and `ExecReload` call the handler. 23 handler units are generated
(`mmdvmhost`, `dmrgateway`, `ysfgateway`, the parrots, the gateways, the
bridges, `mobilegps`, `ircddbgateway`, `timeserver`, `timercontrol`,
`dstarrepeater`), plus units for the two Python daemons, plus
`gospot-hostfiles.service`/`.timer` (daily `HostFilesUpdate.sh`, 10 minutes
after boot, up to 2 hours of randomised delay, `Persistent=true`).
**Only the host-file timer is enabled on install**; every radio unit stays
disabled, for the operator or `radio/migrate-board` to enable. `RADIO_USER`
(`mmdvm`) is created as a system account in `dialout` and `tty` and owns
`$GOSPOT_LOG_DIR` as its group (mode `0775`).

**`gospot-watchdog` and `gospot-remote`.** Pi-Star's `pistar-watchdog` and
`pistar-remote` are Python 2 and do not run on Bookworm or Trixie, so
`radio/sbin/` carries Python 3 ports with no nginx, no `iwconfig` and no shell
use.

* `gospot-watchdog` runs a check every 300 s (`--once` for a single pass). It
  keeps the log volume under 90 % full — removing rotated `*.1`/`*.1.gz` at the
  top level of `$SYSTEM_LOG_DIR`, `*.gz` older than a day below it and
  `$GOSPOT_LOG_DIR/*.log` older than a day, then trimming today's controller log
  to 500 lines and restarting it — and restarts the unit of any missing
  process from `WATCHDOG_DSTARREPEATER_PROCESSES` (when
  `MARKER_DSTARREPEATER` exists), `WATCHDOG_MMDVMHOST_PROCESSES` (when
  `MARKER_MMDVMHOST` exists) or `WATCHDOG_COMMON_PROCESSES` (always). It also
  scans the DStarRepeater log incrementally for `Bad DVAP header` and
  `Cannot open the D-Star modem` (restart with a long pause to let a DVAP
  recover) and `No space to write`.
* `gospot-remote` lets the keeper callsign in `CONF_GOSPOT_REMOTE` control the
  hotspot over RF, by watching the radio logs for a DMR voice header to a
  configured talkgroup, a P25 transmission, a D-Star header whose UR field is a
  command, or a keeper header followed by a YSF room connect. Commands are
  `svckill`, `svcrestart`, `reboot`, `shutdown`, `hostfiles` for every mode plus
  `getip`, `wifissid`, `wifirssi` and `8ball` for D-Star, which answer as
  D-Star slow-data text through `texttransmitd`. Which units each command
  touches is entirely in the settings
  (`REMOTE_MMDVMHOST_STOP_UNITS`, `REMOTE_{DMR,P25,DSTAR,YSF}_RESTART_UNITS`,
  …). A 30-second cooldown per `(mode, action)` suppresses repeats.

Both daemons share a hardened `load_paths()`: the generated
`$RADIO_SHARE_DIR/paths` file must be a regular file, owned by the effective
uid, with no group/other write bits, opened `O_NOFOLLOW`; keys and values are
re-validated; and the `GOSPOT_RADIO_PATHS` override is honoured only when not
running as root.

**Configuration seeding.** `build-deb` turns the Pi-Star `config_clean.zip`
into a `config-seed` list of `member target` pairs, resolving each target
through the rename table and the `CONF_*` settings keys, and dry-runs the
rewrite of every member so an unknown Pi-Star string fails the *build* rather
than the install. On the board, `postinst` runs
`seed-configs config-seed config_clean.zip rename.table`, which — **only for a
target that does not exist** — extracts the member, rewrites it through
`gospot-rebrand apply`, and moves it into place atomically at mode `0644`.
Existing files are never read or touched. Seeding runs *after*
`gospot-radio-migrate`, so an upgraded board keeps its migrated configuration
and only genuinely absent files are created. `/etc/gospot-release` is likewise
created only if absent.

**Renaming and migration.** `gospot-rebrand` with the table
`gospot-rename.table` (kinds `tool`, `literal`, `word`, `delete`, `allow`,
`allow-comment`) rewrites every Pi-Star script and configuration default, and
**fails on any `pistar`/`pi-star`/`pi_star` occurrence the table neither
renames nor explicitly allows**. Layout paths in the table are written as
`${KEY}` and resolved from `gospot.conf` at build time, so the table itself
holds no hard-coded path. Deliberately kept: the `pistar.uk` URLs and data
sources, the `mw0mwz.co.uk/pi-star/` link, the phrase "Pi-Star compatible",
and the attribution banners in comments. `gospot-radio-migrate` moves a board
from the `pistar-radio` layout (logs entry by entry with `os.rename` so inodes
survive, `/etc/pistar-release` → `/etc/gospot-release`, the CSS and remote
files, `/var/log/pi-star` references inside every `CONF_*` file, the keeper
log, the MOTD generator, and the old units) and **never deletes anything**: an
old file whose new location already exists is kept and reported.
`radio/migrate-board` is the one-shot, rollback-safe conversion of a board from
the upstream `MMDVM-Host.ini`/`DMRGateway.ini` layout, using
`convert-config.py` to produce Pi-Star-format `/etc/mmdvmhost` and
`/etc/dmrgateway`, with a timestamped backup directory, a `trap … ERR`
rollback that reinstalls the old units and binaries, and a verification loop
that waits for both the modem banner and the DMR master login in the new logs.

## 9. The OS image (`image/`)

`image/build.sh` builds a Raspberry Pi OS Lite image locally with Docker and a
pinned [pi-gen](https://github.com/RPi-Distro/pi-gen), and installs the two
local `.deb` packages *as packages* (`apt-get install ./…deb`) so their
dependencies resolve from the Raspberry Pi OS archive. Target: Trixie
(Debian 13) **armhf** first, since all three reference boards are Raspberry
Pi 2 Model B and cannot run arm64; arm64 is a later stage and needs a pinned
pi-gen `arm64` branch commit and arm64 packages. The image is built **only
locally — never in CI**.

Everything the build, the checks and the first boot use comes from the settings
file named by `image/settings.path` (currently
`Pi-Star_dash_new/gospot.conf`): the `IMAGE_*` and `BOOT_*` keys,
`RADIO_PACKAGE`, `DASHBOARD_PACKAGE` and the dashboard and radio keys. The
*installed* path of that settings file is taken from the dashboard package's
own `conffiles`, so package, unit, image and host names are settings and a
rename rebuilds by editing settings only.

**Stages.** `stage0 → stage1 → stage2` (Raspberry Pi OS Lite, not exported;
`SKIP_IMAGES` is touched) `→ stage-local`:

| Sub-stage | What it does |
| --- | --- |
| `00-packages` | installs both local packages, enables `IMAGE_ENABLE_UNITS` (the two panels, the management service, the host-file timer), and **removes the management credentials the postinst created inside the build container** |
| `01-firstboot` | installs the first-boot script and its unit, ordered before `gospot-wizard.service` |
| `02-uart` | `enable_uart=1` in `BOOT_CONFIG_TXT`, no serial console in `BOOT_CMDLINE_TXT`, `IMAGE_MASK_UNITS` serial gettys masked |

pi-gen is configured with `TARGET_HOSTNAME=$IMAGE_HOSTNAME`,
`FIRST_USER_NAME=$GOSPOT_LINUX_USER`,
`FIRST_USER_PASS=$IMAGE_DEFAULT_PASSWORD`,
`DISABLE_FIRST_BOOT_USER_RENAME=1`, `ENABLE_SSH=1` and cloud-init kept for
Raspberry Pi Imager customisation. The build verifies that the pinned pi-gen
actually defaults to the requested suite and architecture before using it, and
rewrites the Docker image tag and container name per build so parallel builds
cannot collide. `IMAGE_BUILD_RASPBIAN_MIRROR` may point the *build* at a
specific Raspbian mirror (some geographic redirects stall the build); an
`export-image` hook restores pi-gen's own source in the finished image and
clears the apt lists, and the verification checks that the mirror does not
appear in `/etc/apt`.

**Per-board secrets.** No credential is shared between boards flashed from one
image. The image carries none: `00-packages` deletes what the package
`postinst` created in the container, pi-gen deletes the SSH host keys, and
`/etc/machine-id` is left uninitialised. On first boot,
`IMAGE_FIRSTBOOT_UNIT` runs once — guarded by
`ConditionPathExists=!$IMAGE_FIRSTBOOT_DONE`, after the network is up and
**before** the management service, which `Requires=` it — and deletes any
management credential in `$WIZARD_CONFIG_DIR`, then runs the dashboard
package's own credentials code path to create *this board's* TLS key and
self-signed certificate (`CN=<hostname>`, SAN with the host name and the
current addresses) and reset the admin web password to the factory default.
The marker is written **last**, so an interrupted first boot simply runs again
on the next boot. SSH host keys are regenerated by Raspberry Pi OS on first
boot. Radio units stay disabled, exactly as after installing `gospot-radio`.

Default logins are the classic Pi-Star ones: SSH and console
`$GOSPOT_LINUX_USER` with `$IMAGE_DEFAULT_PASSWORD`, management UI user
`gospot` with the same password. *Configuration > Remote Access Password*
changes the admin web password and the Linux account password together, and —
like Pi-Star — refuses the factory value.

The pinned commits fix the **inputs**, not the output: Raspberry Pi OS
packages, the kernel and the firmware come from the live archive at build time,
so two builds of the same sources are not byte-identical. The build therefore
writes a manifest (pi-gen commit, suite and architecture, source commit and
whether the tree was dirty, and each package's name, version, SHA-256 and file
name) and a `.sha256` next to the image; keep both with a release.

## 10. Invariants worth knowing before changing anything

1. **No PHP.** Not in sources, not in the packages, not in the dependencies,
   not at runtime. No iframe, proxy, link or shell around the classic PHP
   dashboard counts as a port. `Pi-Star_DV_Dash` is an immutable reference
   only. The package build asserts it (`audit-native.mjs`, and
   `find … -name '*.php'` in the radio build), and the image verification
   checks the finished filesystem for `*.php`, `php-fpm*`, nginx and
   `/var/www/dashboard`.
2. **Paths only in `gospot.conf`.** Add the key with a comment first, then read
   it. `panel/config_test.go` loads the repository copy and
   `scripts/test-native-runtime.mjs` compares it with the packaged one.
3. **One JSON endpoint per page, one follower per process.** No `grep` or
   `tail` per request; INI files are re-read only when they change.
4. **Writes are transactional.** Read → plan → revision-checked helper apply →
   service restarts, with rollback on failure. An untouched form writes
   nothing; a written file is never left half-migrated.
5. **A module is finished only when its tests are green** — Go, Python helper,
   frontend, the shell tests, and the isolated Chromium stand that opens,
   edits, saves, reopens and then verifies the files, modes and restarts
   independently. See [BUILD.md](BUILD.md).
