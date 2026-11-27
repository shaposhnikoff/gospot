# Building and testing GoSpot

Everything here is run from a clone of the repository on a Linux x86-64 or
arm64 machine. Two Debian packages and one OS image come out of it:

| Artifact | Built by | Built in CI? |
| --- | --- | --- |
| `gospot_<version>_armhf.deb` (dashboard) | `Pi-Star_dash_new/scripts/build-deb` | yes |
| `gospot-radio_<version>_armhf.deb` (radio platform) | `radio/build-deb [bookworm\|trixie]` | no |
| `gospot-<suite>-<arch>.img.xz` (OS image) | `image/build.sh` | **never** |

Russian version: [BUILD.ru.md](BUILD.ru.md). How the pieces fit together:
[ARCHITECTURE.md](ARCHITECTURE.md).

---

## 1. Prerequisites

| Tool | Needed for | Notes |
| --- | --- | --- |
| **Go** | the panel binary and its tests | the version in `panel/go.mod`; the only Go module in the tree |
| **Node.js 22 + npm** | the Svelte UI, the frontend tests, the package audits | run `npm ci` in `Pi-Star_dash_new/` first |
| **Python 3.11+** | the root helpers and their tests, the radio tooling, the QA stand | no third-party packages |
| **`dpkg-deb`, `dpkg`** | both packages | `dpkg --validate-version` is called on the version |
| **`qemu-user-static`** | running the armhf binary on an x86 host | `scripts/test-native-runtime.mjs` runs the packaged ARM binary under `qemu-arm-static`; CI installs the package explicitly |
| **Docker (rootful)** | the radio libraries, the D-Star rebuild, the OS image, the QA stand | the image build needs privileged containers and loop devices |
| **`binfmt_misc` with an ARM entry carrying the `F` flag** | the armhf containers and the image build | `qemu-user-static` / `qemu-user-binfmt` |
| **`openssl`, `sudo`, `visudo`, `socat`, `zip`/`unzip`** | credentials, sudoers validation, the QA relay, config seeding tests | |
| **Chromium + a matching ChromeDriver** | `qa/real-write/run.sh` | `$CHROME_BIN` and `$CHROMEDRIVER` override discovery |
| **~25 GB free under `/var/tmp`** | the OS image | `IMAGE_BUILD_PARENT` |

The radio package additionally needs the two Pi-Star binary trees next to
`Pi-Star_dash_new/`, at the paths the settings name
(`REFERENCE_BINARIES_BIN_DIR`, `REFERENCE_BINARIES_SBIN_DIR` — by default
`../Pi-Star_v4_Binaries_Bin` and `../Pi-Star_Binaries_sbin`). In a git
worktree, link them in. They are not submodules and are not checked out by a
plain clone.

First-time setup:

```sh
git clone --recurse-submodules git@github.com:shaposhnikoff/gospot.git
cd gospot/Pi-Star_dash_new
npm ci
```

## 2. The dashboard package (`gospot`)

```sh
cd Pi-Star_dash_new
scripts/build-deb                       # no arguments, no options
DEB_VERSION=0.2.0+local.1 scripts/build-deb
```

The script takes **no arguments**; the only input is `DEB_VERSION`. Without
it, the version is the `Version:` line of `debian/control`
(currently `0.2.0-1`); CI uses `${base}+ci.${GITHUB_RUN_NUMBER}`. The version
must match `^[0-9][A-Za-z0-9.+~-]*$` and pass `dpkg --validate-version`.

What it does, in order:

1. `npm run check` — `svelte-check`, so a type error fails the build.
2. `npm run package:native` — `vite build` into `dist/`, then
   `scripts/package-native.mjs`, which audits `dist/`, creates
   `build/release-XXXXXX/`, copies the bundle to `release/ui`, writes
   `ui/runtime.json` (`{"backend":"gospot-go","wizardPort":<MANAGEMENT_PORT>}`),
   normalises modes to `0755`/`0644`, re-audits and prints
   `Native release: <path>`.
3. `source ./gospot.conf` — **every installation path and port comes from the
   settings file**: `DASHBOARD_LIB_DIR`, `DASHBOARD_UI_DIR`,
   `WIZARD_CONFIG_DIR`, `DASHBOARD_BM_STATE_DIR`, `PANEL_PORT`,
   `MANAGEMENT_PORT`, `OPERATOR_PORT`.
4. Cross-compiles the panel:
   ```sh
   CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go -C ../panel build -trimpath \
     -ldflags="-s -w -X main.dashboardVersion=$version" -o "$p$lib/panel" .
   ```
5. Generates `/etc/sudoers.d/gospot` from `debian/sudoers` by substituting
   `@DASHBOARD_LIB_DIR@`, mode `0440`, and **fails the build if any `@`
   placeholder is left** (`unresolved sudoers placeholder`).
6. Generates the three units from their templates by rewriting `ExecStart`, and
   fails the build on a leftover placeholder. A `50-gospot.conf` drop-in is
   written per unit; the `gospot-wizard` drop-in also adds the two
   `LoadCredential=` lines and `StateDirectory=`.
7. Installs the seven root helpers from `../panel/helper/`, the settings file as
   the single conffile `/etc/gospot/gospot.conf`, `gospot.conf.default`,
   `gospot-conf-merge`, `gospot-migrate` and the UI tree.
8. `node scripts/audit-native.mjs` — rejects symlinks, non-regular files, any
   path containing `classic`, `compat` or `docroot`, `.php*`/`.phtml`/`.phar`
   files, `<?php`/`<?=` bytes anywhere, `php` in a dependency field, and
   `php-fpm`/`php-cgi`/`php` commands in the units or maintainer scripts.
9. Writes a deterministic `DEBIAN/md5sums` (the conffile deliberately
   excluded) and builds with `dpkg-deb --root-owner-group`.

Output and the printed SHA-256:

```
Pi-Star_dash_new/build/release-<id>/gospot_<version>_armhf.deb
```

Check the built artifact without installing it:

```sh
bash scripts/test-deb-entry build/release-<id>/gospot_<version>_armhf.deb
```

That unpacks the package with `dpkg-deb -R`, re-runs `audit-native.mjs`, and
then runs `scripts/test-native-runtime.mjs`, which asserts the packaged
`/etc/gospot/gospot.conf` is byte-identical to the repository copy and **starts
the real ARM binary under `qemu-arm-static`** to check the operator HTML and
assets byte for byte, `/runtime.json`, `404` for `/not-installed.php`, `405`
for `POST /`, the `/admin/configure.php` → `https://…:8788/manage/#configuration`
redirect, and the HTTPS management listener (anonymous `401`, Basic
`gospot:<password>` `200` with byte-identical HTML and assets). Set
`NATIVE_BROWSER=1` for an extra browser pass.

## 3. The radio package (`gospot-radio`)

Three scripts, each taking the Debian suite as its only argument:

```sh
cd radio
./build-libs.sh  [bookworm|trixie]      # default bookworm
./build-dstar.sh [bookworm|trixie]      # default bookworm
./build-deb      [bookworm|trixie]      # default bookworm; chains the two above
DEB_VERSION=0.2.0+local.1 ./build-deb trixie
```

`build-libs.sh` builds the three shared libraries Debian does not ship but the
Pi-Star PS4 binaries link against — `libwiringPi`, `libwiringPiDev`,
`libArduiPi_OLED` — in a `debian:<suite>-slim` container run with
`--platform linux/arm/v7`, from pinned revisions (WiringPi tag 3.14,
`hallard/ArduiPi_OLED`). ArduiPi_OLED gets an explicit
`CCFLAGS="-O2 -marm -march=armv6zk -mfpu=vfp -mfloat-abi=hard"` because
upstream omits `-marm` and Debian armhf defaults to Thumb. Output:
`radio/build/<suite>/lib/`.

`build-dstar.sh` rebuilds the nine D-Star daemons for **wxWidgets 3.2** (the
PS4 builds need 3.0, which Bookworm and Trixie no longer ship), in the same
kind of container with `libwxgtk3.2-dev`. It sources `gospot.conf` and bakes
the layout in through make variables only — no source patching:
`BUILD=release`, `CONFDIR=/etc`, `LOGDIR=$GOSPOT_LOG_DIR`, and `DATADIR` from
`DSTAR_IRCDDB_DATA_DIR` / `DSTAR_REPEATER_DATA_DIR`. Every binary is stripped
and checked with `ldd` for unresolved libraries; the build fails on any. It
prints the built binaries and the `libwx*` libraries `ircddbgatewayd` resolved
against. Output: `radio/build/<suite>/dstar/{bin,ircddbgateway,dstarrepeater}`
and `wx-libs`.

`build-deb` assembles the package by hand and calls `dpkg-deb` — it is not a
`dpkg-buildpackage` source package, and `radio/debian/` holds only `control`
and `postinst`. It runs the two sub-builds automatically if their outputs are
missing, resolves `gospot-rename.table` against `gospot.conf`, rebrands the
Pi-Star tools, generates 27 systemd units and the `paths` and `config-seed`
data files, and aborts on any of:

* a D-Star binary that still contains a Pi-Star path, or a `dstarrepeaterd`
  that does not log to `$GOSPOT_LOG_DIR` (override with
  `GOSPOT_ALLOW_STALE_DSTAR=1`, for inspection only);
* a unit named in the settings that the package does not install;
* a settings key the `paths` file needs but `gospot.conf` lacks;
* a `config_clean.zip` member with no matching `CONF_*` key, or one the rename
  table cannot rewrite;
* any `*.php` file, any `*pistar*`/`*pi-star*` file name, or any text
  occurrence of `pistar`/`pi-star`/`pi_star` the table neither renames nor
  explicitly allows.

Output and the printed SHA-256:

```
radio/build/<suite>/gospot-radio_<version>_armhf.deb
```

Note: only `build-libs.sh` and `build-dstar.sh` validate the suite argument;
`build-deb` uses it only as the `build/<suite>` path component. `trixie` is
accepted everywhere but is not exercised in CI.

## 4. The OS image

```sh
./image/build.sh                                 # packages + image + offline verification
./image/build.sh --prepare-only                  # packages + pi-gen context, no Docker build
./image/build.sh --arch armhf                    # explicit architecture (default: IMAGE_ARCH)
RADIO_DEB=… DASHBOARD_DEB=… ./image/build.sh     # reuse already built packages
BUILD_PARENT=/srv/build ./image/build.sh         # override IMAGE_BUILD_PARENT
```

Options, exactly as the script's own usage prints them:

* `--arch ARCH` — image architecture, default `IMAGE_ARCH` from the settings.
  Only `armhf` is supported now; `arm64` exits with the reason (it needs a
  pinned pi-gen `arm64` branch commit and arm64 packages first).
* `--prepare-only` — fetch pi-gen, build the packages and prepare the Docker
  context, then stop and print the command to finish by hand.
* `--help` / `-h` — the usage text.

Environment: `RADIO_DEB` and `DASHBOARD_DEB` point at already built packages
instead of building them (the script verifies each one's `Package` and
`Architecture` fields against `RADIO_PACKAGE`/`DASHBOARD_PACKAGE` and the
requested architecture); `BUILD_PARENT` overrides `IMAGE_BUILD_PARENT`, and the
build refuses a parent inside the repository or a path with spaces.

Every path, name, port and pinned revision comes from the settings file named
by `image/settings.path` (currently `Pi-Star_dash_new/gospot.conf`):
`IMAGE_NAME`, `IMAGE_SUITE`, `IMAGE_ARCH`, `IMAGE_HOSTNAME`,
`IMAGE_DEFAULT_PASSWORD`, `IMAGE_ENABLE_UNITS`, `IMAGE_ENABLE_CLOUD_INIT`,
`IMAGE_COMPRESSION`, `IMAGE_MASK_UNITS`, `IMAGE_FIRSTBOOT_*`, `BOOT_CONFIG_TXT`,
`BOOT_CMDLINE_TXT`, `IMAGE_PIGEN_REPO`, `IMAGE_PIGEN_REV_ARMHF`,
`IMAGE_BUILD_PARENT`, `IMAGE_BUILD_RASPBIAN_MIRROR`, `RADIO_PACKAGE`,
`DASHBOARD_PACKAGE`. The *installed* path of the settings file is read out of
the dashboard package's own `conffiles`.

**Where the output lands.** Each run creates
`$IMAGE_BUILD_PARENT/<IMAGE_NAME>-build.XXXXXXXXXX/` (default under
`/var/tmp`, outside the repository). Inside it, `pi-gen/deploy/` holds:

* `<image>.img.xz` — the compressed image;
* `<image>.img.xz.sha256`;
* `<image>.manifest` — pi-gen repository and commit, suite and architecture,
  the source commit (marked if the tree was dirty), and each package's name,
  version, SHA-256 and file name;
* the pi-gen build logs.

The build directory also keeps `radio-build-deb.log` and
`dashboard-build-deb.log` when it built the packages itself, and the same
`manifest`. The pinned commits fix the *inputs*; Raspberry Pi OS packages, the
kernel and the firmware come from the live archive, so two builds of the same
sources are not byte-identical — keep the manifest and the SHA-256 with a
release.

### Verifying an image

`build.sh` runs the first of these itself:

```sh
image/verify-artifact.sh IMAGE RADIO_DEB DASHBOARD_DEB
image/qemu-boot.sh IMAGE [BOARDS]
```

`verify-artifact.sh` is an **offline** check: it attaches the root and boot
partitions read-only through a loop device (needs `sudo`) and inspects them
from the host, running `visudo`, `dpkg` and `ldd` *inside* the image through
qemu-user. It asserts, among other things: both packages installed at the
expected versions with files matching their `md5sums`; `IMAGE_ENABLE_UNITS`
enabled and every radio unit **not** enabled; `IMAGE_MASK_UNITS` masked; no
PHP, nginx or php-fpm packages or files and no `/var/www/dashboard`; no
Pi-Star file names and Pi-Star mentioned only where the rename table allows;
no management credentials, no first-boot marker, no SSH host keys and no
private keys under `/etc`, `/home`, `/root`, `/var/lib`; the build mirror
absent from `/etc/apt`; `machine-id` uninitialised; the packaged
`gospot.conf` byte-identical to the repository copy and root-owned; sudoers
parsing with `visudo -c`; the first-boot unit ordered before the management
service and conditioned on its marker; hostname and first user; every settings
directory present with `$GOSPOT_LOG_DIR` at mode `775`; the radio
configurations seeded; every program in `$GOSPOT_BIN_DIR` executable and
resolving its libraries; and `enable_uart=1` with no serial console.

`qemu-boot.sh` boots fresh copies of the image under QEMU `raspi2b` — the
Raspberry Pi 2 B of the target boards, using the image's own `kernel7.img`,
device tree and initramfs — inside a throw-away Docker container, and probes
each "board" from inside that container, so **nothing listens on host ports**.
It checks the operator panel (`200`), `/manage/` without credentials (`401`)
and with the defaults (`200`), SSH, the first-boot state and the unit states.
With two or more boards it compares TLS certificate and SSH host key
fingerprints to prove nothing is shared between boards flashed from one image.
It prints fingerprints, never passwords or keys. Waiting for a board to come
up can take a long time; the script gives up after 60 minutes per board.

The first-boot logic has its own unprivileged unit test:

```sh
image/stage-local/01-firstboot/tests/test_firstboot.sh
```

## 5. Test suites

The authoritative list is the CI test step. From `Pi-Star_dash_new/`:

```sh
npm ci
npm test
go -C ../panel test ./...
python3 -B -m unittest discover -s ../panel/helper
debian/test-gospot-conf-merge.sh
debian/test-gospot-migrate.sh
../radio/test_seed_configs.sh
python3 -B -m unittest discover -s ../radio -p 'test_gospot_*.py'
```

### Go

```sh
cd panel
gofmt -l .            # must print nothing
go vet ./...
go test ./...
```

`panel/` is the only Go module. `panel/config_test.go` loads the repository
copy of `gospot.conf`, so a key added to the code without being added to the
settings file fails here. Benchmarks live in `panel/configure_bench_test.go`.

### The Python root helpers

```sh
env -u GOSPOT_SETTINGS python3 -B -m unittest discover -s panel/helper
```

Clear `GOSPOT_SETTINGS` explicitly: the helpers honour it when not running as
root, so a value left over in the shell would point the tests at the wrong
settings file. The suite covers `gospot-configure` (read, plan, apply,
rollback, the per-file editors, backup/restore, factory reset, the CSS tool and
both Wi-Fi backends), `gospot-config`, `gospot-calibrate`, and the DMR services
and protocols paths. No root, no network and no systemd are needed — the tests
substitute `systemctl` and run against temporary fixtures.

### The radio package

```sh
radio/test_seed_configs.sh
python3 -B -m unittest discover -s radio -p 'test_gospot_*.py'
```

`test_seed_configs.sh` has no shebang (its first line is `set -euo pipefail`),
so run it as an executable file or through `bash`; it needs `zip` and `unzip`.
It builds its own fixture archive and proves that absent configurations are
created at mode `0644` with the Pi-Star strings rewritten, that an unknown
Pi-Star path is refused and not installed, that an existing file is left
byte- and mode-identical, that a listed-but-absent member is skipped, and that
a second run changes nothing.

The Python suites cover `gospot-watchdog`, `gospot-remote`, `gospot-rebrand`
and `gospot-radio-migrate`. The two daemon suites run the real daemon and its
real handler as subprocesses with a fake `systemctl`, so they need no root.
Two `gospot-rebrand` tests are skipped unless the Pi-Star binary trees are
checked out next to the repository.

### The dashboard shell tests

```sh
cd Pi-Star_dash_new
debian/test-gospot-conf-merge.sh
debian/test-gospot-migrate.sh
```

`test-gospot-conf-merge.sh` proves the conffile merge rule: keys the installed
copy lacks are appended exactly once, a locally edited value such as
`PANEL_PORT=9999` survives and the packaged default is not re-added, a
commented-out key is ignored, the second run prints nothing and changes no
bytes, and merging the file into itself is a no-op.

`test-gospot-migrate.sh` drives the one-time takeover of a `pistar-dashboard`
installation against four fixture roots with a fake `systemctl`: local values
kept, build-time and obsolete keys handled, credentials and state copied with
modes, owners and mtimes preserved, the Wi-Fi country modprobe file renamed,
the old tree never modified, the old unit disabled exactly once, the migration
marker written, full idempotence, existing credentials never overwritten, a
fresh install untouched, and no secret in the output. The owner-remapping
check is skipped when `fakeroot` is not installed.

### The frontend

```sh
cd Pi-Star_dash_new
npm test                 # tsx --test src/*.test.ts && node --test scripts/package-native.test.mjs
npm run check            # svelte-check --tsconfig ./tsconfig.json
npm run dev              # vite --host 127.0.0.1 --port 5173 --strictPort
npm run build            # vite build
npm run preview          # vite preview --host 127.0.0.1 --port 4173 --strictPort
```

`npm test` runs the unit tests of the logic modules — `calibration`, `classic`,
`data`, `dstar`, `hoseline`, `kiosk`, `runtime`, `tgif`, `wifi` — plus the
packaging test. `svelte-check` is also the first step of `scripts/build-deb`,
so a type error cannot reach a package. There is also a kiosk screenshot
harness, after a build:

```sh
npm run build && python3 -B qa/kiosk_screens.py --out <dir>
```

### The isolated Chromium stand (`qa/real-write`)

This is the end-to-end gate: a real Svelte bundle, the real Go binary, the
shipped root helpers, the generated sudoers, and real file writes — with no
Raspberry Pi, no PHP, no SSH to a board, and no UART or MMDVMHost emulation.
Run it from `Pi-Star_dash_new/`:

```sh
qa/real-write/run.sh                       # default: station settings
qa/real-write/run.sh --configure
qa/real-write/run.sh --services
qa/real-write/run.sh --expert
qa/real-write/run.sh --backup
qa/real-write/run.sh --css
qa/real-write/run.sh --factory
qa/real-write/run.sh --wifi
qa/real-write/run.sh --calibration
qa/real-write/run.sh --jitter
qa/real-write/run.sh --dstar
qa/real-write/run.sh --tgif
qa/real-write/run.sh --hoseline
qa/real-write/run.sh --preview             # interactive stand, no assertions
qa/real-write/run.sh --dist build/release-<id>/ui   # ship a built release instead of rebuilding
```

Flag parsing is strictly positional: `--preview` must be first, `--dist DIR`
must be the last two arguments, and exactly one scenario runs. The complete
set of scenario flags and what each one proves:

| Flag | What it proves |
| --- | --- |
| *(none)* | Station settings: callsign `DEMO1 → DEMO2` and master address `192.0.2.11 → 192.0.2.12`, every other byte unchanged, both files' applied SHA-256 printed; a confirmed operator rollback is byte-identical and consumes `previous.json`; then an injected service failure restores both files in the right recovery order and leaves no pending state |
| `--configure` | The Configuration page: an untouched Apply writes and restarts nothing; `Colour Code 1 → 7` changes exactly that line and restarts DMRGateway then MMDVMHost; an invalid value (`422`) and a stale page (`409`) write nothing; the Remote Access Password form refuses a mismatch, then writes mode `0600` with no radio restart and only a scheduled management-service restart |
| `--services` | DMR services: `ColorCode 1 → 7` and `TGRewrite0` destination `91 → 310`, a fresh GET, exact comparison of both INIs, a confirmed rollback, plus CSRF `403`, invalid rule `422`, stale form `409` and no extra restarts |
| `--expert` | Expert Quick Edit (`ColorCode 1 → 5`, one MMDVMHost restart, reopen shows the new value, unchanged form writes nothing, stale `409`, missing CSRF `403`), Full Edit of DMRGateway (whole text written keeping `mmdvm:mmdvm 600`, MMDVMHost then DMRGateway restarted), System Cron refusing a blocked pattern, and API Keys storing a 1 KB BrandMeister JWT at `0600` with no restarts |
| `--backup` | The classic zip download (`GoSpot_Config_<host>_<Y-M-d>.zip`, classic member names, exact bytes, no PHP), restore with modes preserved and the right restart order, a classic `Pi-Star_Config` archive restoring `pistar-css.ini` as `/etc/gospot-css.ini`, and an unsafe archive refused with no writes or restarts |
| `--css` | CSS Tool: defaults with no file; a save writes the classic file `root:root 644` (an empty value as none) with no restarts; the banner colour and `BannerH1` show on the admin pages and the dashboard; the QRZ lookup round-trips; a stale page `409`; Factory Reset refuses a wrong confirmation and removes the file on `RESET` |
| `--factory` | Factory Reset: Cancel writes nothing; OK restores MMDVMHost, DMRGateway and a missing YSFGateway byte for byte from `config_clean.zip` with owners and modes kept, removes the controller marker and the CSS override, and restarts DMRGateway then MMDVMHost |
| `--wifi` | The Wi-Fi page against a `wpa_supplicant` fixture (the stand rewrites `WIFI_BACKEND` for this scenario): networks listed without keys; a save without CSRF `403`; Delete + Save sends one POST and removes only the selected block, keeping `root:root 600`; a PSK change matches an **independently computed** WPA hash (PBKDF2-HMAC-SHA1, 4096 rounds, 32 bytes) with every other byte kept; a two-tab stale form `409` with no second install; an invalid PSK blocked in the page and `422` from the API; a missing `wlan0` reported; no restarts |
| `--calibration` | Start holds MMDVMHost and runs MMDVMCal on the `[Modem]` port; output and the saved frequency are shown; key presses reach MMDVMCal's stdin; a second start `409`; Save Offset writes exactly `[Modem] RXOffset` (`mmdvm:mmdvm 640`, no restart); a stale page `409`; Stop and an abandoned page each restart MMDVMHost exactly once |
| `--jitter` | The Expert Jitter Test runs `gospot-jittertest BM` into a cleared log (`root:root 644`) and shows it; a second run `409`; an unknown group `400`; no group means DMR+; no restarts |
| `--dstar` | The D-Star panels for both controllers (link information, link manager, active CCS connections with StarNet groups and members; with DStarRepeater also the gateway software configuration and the activity tables); link, unlink and Text Entry each invoke the fake `remotecontrold` exactly once with the exact argument list, through a private `0700` home with a `0600` control file that is removed afterwards; CSRF `403`, bad input `400`, unknown module or reflector `422`, a self-link invoking nothing; gateway passwords never reach the page, the network or the logs |
| `--tgif` | Against a local HTTPS TGIF double: the classic table with placeholders and then static and dynamic groups; Set Static add/remove and Set Dynamic remove each send exactly one request with the classic JSON and a `Bearer` token; `TG 0` refused without a request; with no token the legacy manager sends one token-less `GET sessions/update/<id>/1/91`; the token is absent from the page, the request bodies and the service log |
| `--hoseline` | Against a local spotter double: the table under Local RF Activity lists the configured talkgroups; Play opens exactly one relay (the spotter saw the page token, the `Origin` and the `spotter` subprotocol, then the subscription); μ-law frames and start/end events arrive and the status links the caller; Stop closes the upstream connection; at `HOSELINE_MAX_LISTENERS` Play shows the limit error without a new relay; audio also works without AudioWorklet; no CSP errors |
| `--preview` | Builds and runs `preview.go` as a loopback HTTP gateway and prints its URL for manual inspection. It is not part of any release and not meant for a real board |

How the isolation works, and therefore what the result means:

* A **dedicated `--internal` Docker network** and a per-run container built
  from `qa/real-write/Dockerfile` (`debian:bookworm-slim` plus `python3` and
  `sudo`, with the `gospot-wizard` and `mmdvm` system users), `--pids-limit`
  set. An internal network publishes no ports, so a `socat` relay forwards raw
  TLS bytes on loopback — TLS and authentication still terminate at the real Go
  server.
* **No host system bus.** A stub `systemctl` inside the container talks over
  one bind-mounted Unix socket to `qa/real-write/bridge.py` on the host, which
  accepts only `(dmrgateway|mmdvmhost).service (ok|fail)` and only restarts two
  disposable per-run `systemd-run --user` units. Failure injection is the test
  service exiting non-zero, and recovery requires systemd's own confirmation.
* The configuration files are copies of the INI fixtures under
  `image/stage-mmdvm/00-install/files/` with test-only identities, owned
  `mmdvm:mmdvm`. Credentials are generated per run.
* Only real artifacts are used: the UI is either freshly built or the release
  passed with `--dist`, the Go binary is cross-built from `panel/`, the four
  helpers are copied from `panel/helper/` and **SHA-256-verified inside the
  container**, and the sudoers file is generated from `debian/sudoers` and
  validated with `visudo -c`.
* Assertions are independent of the UI: both radio INIs are compared byte for
  byte against an oracle built separately, the restart history is read from the
  container's own event log, and leftover `previous.json`/`pending.json` are
  checked for.

Evidence is the `PASS …` lines on stdout, together with the applied SHA-256
digests, the UI hash verification line, the HTTPS status-code line and the
per-restart systemd results. On failure the script dumps the service results
and the service log, and the Python harness prints the page and the browser
console. Everything transient is removed by an `EXIT` trap (containers,
network, user units and the work directory); the runtime Docker image is kept
so a re-run is fast.

Requirements: Docker, Go, npm with local dependencies, Chromium with a matching
ChromeDriver, Python 3, OpenSSL, `socat`, and a working `systemctl --user` on
Linux. The first run needs Debian package access to build the container.

## 6. CI

`.github/workflows/deb-release.yml`, *Build and publish GoSpot Bookworm deb*,
runs **on every push** (`on: push`, any branch or tag; the build job is skipped
for a branch deletion). Top-level permissions are `contents: read`.

The `build` job on `ubuntu-24.04` checks out without submodules and without
persisted credentials, sets up Node 22 (npm cache keyed on
`Pi-Star_dash_new/package-lock.json`) and Go from `panel/go.mod`, runs the test
list from §5, installs `qemu-user-static`, builds the dashboard package with
`DEB_VERSION="${base}+ci.${GITHUB_RUN_NUMBER}"`, asserts exactly one
`gospot_*_armhf.deb` with the expected `Version` and `Architecture`, runs
`scripts/test-deb-entry` on it, and uploads the `.deb` plus a `SHA256SUMS` as
the `bookworm-armhf` artifact.

The `release` job downloads the artifact, re-verifies `sha256sum -c`, and
publishes it to GitHub Releases under the per-run tag
`deb-<run number>-<first 12 of the commit>` — creating the release or
`--clobber`-ing the files if a re-run uses the same tag.

So CI builds and publishes **only the dashboard package**, one `.deb` and one
`SHA256SUMS` per commit. The `gospot-radio` package is not built in CI (it
needs the Pi-Star binary trees and armhf Docker containers), and **the OS image
is never built in CI** — it is a separate product, built locally, and dashboard
checks are done on fixtures instead. Releases themselves carry both packages,
the OS image, the checksums and bilingual release notes.

## 7. Deploying a release to a board

The supported path installs a **published, verified package** — no source sync,
no rebuild on the board:

```sh
cd Pi-Star_dash_new
scripts/install-release deb-<run number>-<commit12>
```

The tag must match `^deb-[0-9]+-[0-9a-f]{12}$`. The script downloads
`SHA256SUMS` and the single `.deb` from the GitHub release, requires the file
list to be exactly one line, verifies the digest, checks the package's
`Package` and `Architecture` fields, prints the release tag and the SHA-256,
and streams the package over SSH to `DEPLOY_HOST` from the settings
(`pistar3`) with `BatchMode=yes` and `StrictHostKeyChecking=yes`.

On the board it asserts the hostname and architecture, takes a **before**
snapshot (the SHA-256, owner, group and mode of both radio INIs and the
`InvocationID` of `mmdvmhost.service` and `dmrgateway.service`), re-verifies the
transferred file's hash, installs with `sudo -n dpkg -i`, asserts that
`pistar-dashboard` is gone (the `Conflicts`/`Replaces` takeover), runs
`dpkg -V gospot` tolerating only the conffile line, checks that
`gospot-stage`, `gospot-wizard`, `gospot-panel`, `mmdvmhost.service` and
`dmrgateway.service` are active, checks that the management listener answers
`401` without credentials using the board's own certificate, checks that the
operator page is byte-identical to the installed `index.html`, and re-takes the
snapshot requiring it to be unchanged:

```
PASS package installed: operator bytes match; both radio INIs, ownership,
     modes and InvocationIDs unchanged
```

The other scripts in the tree are development aids, not release paths, and each
says so: `panel/deploy.sh` and `panel/deploy-wizard.sh` install a
freshly cross-built binary and a generated unit onto the fixed pilot board (see
`panel/README.md` and `panel/WIZARD.md`); `scripts/release-pistar3` chains the
checks, the build and both of those; `scripts/deploy-pistar3` manages the
Python staging server under `/opt/gospot-stage/` and installs no package at
all. Converting a board from the upstream radio layout to the Pi-Star layout is
`radio/migrate-board <gospot-radio_*.deb>`, which backs everything up, has a
`trap … ERR` rollback and verifies the modem banner and the DMR master login
before declaring success.

Do not present a deployment of the native part as an installation of the full
dashboard, and do not change a working Raspberry Pi for a test — write,
failure and rollback runs belong on the isolated fixtures.

## 8. Conventions a contributor must follow

1. **No PHP.** Not in the sources, not in the packages, not in the
   dependencies, not at runtime; no build or install step may need PHP, PHP-FPM
   or the old PHP docroot. A port must not be replaced by a shell around PHP,
   an iframe, a proxy to a PHP page, or a link to the old admin UI or another
   board — every function must work through GoSpot's own UI and API on the
   target node. `Pi-Star_DV_Dash` at revision `10bf34c9` stays an immutable
   external reference; it is not part of the product, its package or its
   required runtime. `scripts/audit-native.mjs`, the radio build's own checks
   and `image/verify-artifact.sh` all enforce this.
2. **Paths only in `gospot.conf`.** Add a new path, port or parameter to
   `Pi-Star_dash_new/gospot.conf` with a comment **first**, then read it from
   the code. Hard-coding such a value in code, scripts, units, `sudoers` or
   tests is forbidden; `sudoers` and systemd drop-ins are generated at build
   time. The only exceptions are the installed path of the settings file itself
   and standard OS tools. `panel/config_test.go` loads the repository copy, and
   `scripts/test-native-runtime.mjs` compares it with the packaged one.
   Keep the format strict: `KEY=value` per line, `#` comments, no quotes, no
   spaces, no expansion — Bash, Go and Python must all read it identically.
   After editing the file on a board, restart `gospot-panel`, `gospot-wizard`
   and `gospot-stage`; a new key is appended to a locally edited conffile by
   `gospot-conf-merge` on upgrade.
3. **A module is finished only when its tests are green.** When a page,
   section, helper or package is done, write the tests and check **all** of its
   behaviour before moving on: Go, Python-helper and frontend unit tests for
   every write rule, refusal and rollback; then the end-to-end Chromium run on
   the isolated stand (open → change → save → open again, with an independent
   check of the files, their modes and the service restarts); then the package
   build, `scripts/test-deb-entry`, and a check on the pilot board that does not
   harm the radio side. A red test is fixed now, not later.
4. **Keep the classic look and the speed.** The dashboard, the admin UI and
   every section reproduce the reference markup, block order, labels, colours
   and colouring rules. Deviate only for security or usability. A page polls
   one JSON endpoint; logs are followed by a single long-running reader; INI
   files are re-read only when they change; nothing forks `grep` or `tail` per
   request.
5. **Both languages, always.** Every document exists as `<name>.md` and
   `<name>.ru.md`, and editing one without the other counts as unfinished.
6. **Record evidence, not claims.** Save the command, the actual output and the
   exit status; distinguish clearly between a field being displayed, a
   successful file write on the stand, a simulated command and an action
   confirmed on hardware. Never publish passwords, PSKs or tokens.
