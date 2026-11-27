# GoSpot

A digital-voice hotspot dashboard and radio stack for the Raspberry Pi, built to
be Pi-Star compatible on disk and PHP-free in operation.

Russian version: [README.ru.md](README.ru.md) · Installation:
[docs/INSTALL.md](docs/INSTALL.md)

## What GoSpot is

- A **Go backend** ([`panel/`](panel)) that serves the dashboard, the HTTPS
  management UI and every configuration write, plus a small set of restricted
  root helpers in Python and bash ([`panel/helper/gospot-*`](panel/helper)).
- A **Svelte 5 / TypeScript UI** ([`Pi-Star_dash_new/src`](Pi-Star_dash_new/src))
  that reproduces the classic Pi-Star layout, colours and behaviour.
- A **radio package** ([`radio/`](radio)) with MMDVMHost, DMRGateway, the
  YSF/P25/NXDN/M17/DAPNET/DGId/APRS gateways, the cross-mode bridges, the
  D-Star daemons, MMDVMCal and the service handlers, all on the Pi-Star layout.
- A **Raspberry Pi OS Trixie (Debian 13) armhf image** ([`image/`](image)) that
  installs those packages the normal way, with `apt`, inside the image.

Two Debian packages come out of it: `gospot` (dashboard) and `gospot-radio`
(radio services), version 0.2.0-x, architecture armhf. Services:
`gospot-panel` (the collector, internal, port 8787), `gospot-wizard` (HTTPS
management, 8788) and `gospot-stage` (operator panel, 8789). Radio units are
installed **disabled** and stay that way until the operator enables them.

## What GoSpot is not

- **Not PHP.** There is no PHP in the sources, in the packages or at runtime —
  no PHP-FPM, no nginx, no `/var/www/dashboard`. Nothing is proxied to, or
  iframed from, an old dashboard.
- **Not Pi-Star.** The product name, the package names, the service accounts and
  the `gospot-*` paths are ours. GoSpot is not endorsed by, affiliated with or
  supported by the Pi-Star project.
- **Not a Pi-Star fork of the UI.** The dashboard was rewritten from scratch in
  Svelte and Go against the classic dashboard as a behavioural reference.

## Pi-Star compatibility

**The system layout is compatible**: configuration files without an `.ini`
suffix (`/etc/mmdvmhost`, `/etc/dmrgateway`, `/etc/ysfgateway`, …), binaries in
`/usr/local/bin`, scripts in `/usr/local/sbin`, host lists and ID databases in
`/usr/local/etc`, controller markers `/etc/dstar-radio.*`. GoSpot reads and
writes exactly the files the original did, so an existing radio configuration
keeps working. **Classic backups are compatible** too: Backup/Restore accepts a
classic `Pi-Star_Config_*.zip` and renames the members it has to
(`pistar-remote` → `gospot-remote`, `pistar-css.ini` → `gospot-css.ini`).

**The names are not compatible on purpose**: `gospot` and `gospot-radio` replace
`pistar-dashboard` and `pistar-radio` (via `Conflicts`/`Replaces`/`Provides`),
the settings file is `/etc/gospot/gospot.conf` and the logs are in
`/var/log/gospot`. A board upgraded from the Pi-Star packages is migrated
automatically — see [docs/INSTALL.md](docs/INSTALL.md).

## What works today

**Operator panel** — `http://<host>:8789/`, no login:

- Classic dashboard: node and modem state, RX/TX frequencies, enabled
  MMDVMHost modes, network status, Last Heard and Local RF Activity, POCSAG
  pages, and the D-Star panels (gateway config, active reflector links, CCS
  connections, StarNet groups) when DStarRepeater is the controller.
- A **BrandMeister manager** (TS1/TS2 subscriptions, static talkgroups,
  dropping dynamic ones, ending a QSO — with a BM API key) and a **TGIF
  manager** (connect/disconnect, plus static TG lists with a TGIF token).
- YSF, P25, NXDN and M17 link managers; a **D-Star Link Manager** with the
  GoSpot-Keeper logbook (link/unlink via `remotecontrold`).
- **BrandMeister Hoseline player** under Local RF Activity: listen-only, one
  talkgroup at a time, relayed by the board itself.
- `/kiosk` — a full-screen wall display for a TV next to the hotspot
  (1920×1080, 1280×720 and portrait 1080×1920): the current call with operator
  name and talkgroup name, or `Listening — last call N min ago`, plus the last
  ten calls.
- `/api/dashboard` and `/api/last_heard.php` for anything that wants the data.

**Administration** — `https://<host>:8788/manage/`, login `gospot`:

- **Configuration** with every section: Gateway Hardware Information, Control
  Software (controller, simplex/duplex, mode switches), MMDVMHost
  Configuration, General Configuration, DMR, D-Star, Yaesu System Fusion, P25,
  NXDN, M17, POCSAG, Mobile GPS, Firewall, Wireless and Remote Access Password
  — plus **Factory Reset**.
- **Expert**: Quick Edit for MMDVMHost, DMRGateway, the YSF/P25/NXDN/DAPNET
  gateways, ircDDBGateway, TimeServer and DStarRepeater; Full Edit for the
  DMRGateway file, GoSpot-Remote, the Wi-Fi file, System Cron and RSSI.dat;
  **API Keys** (BrandMeister, DAPNET, TGIF); **CSS Tool**; **FW** (modem
  firmware flashing with a live log); **SSH Access** (the shellinabox web
  terminal, if installed); and the **jitter test** for BrandMeister, DMR+ and
  HBLink.
- **Wi-Fi**: status and adapter reset, network scan, profile add/delete,
  country code — over NetworkManager or classic `wpa_supplicant`, selected by
  one setting.
- **Calibration**: MMDVMCal on the board's modem with the live graph, BER
  statistics and *Save Offset*.
- **Live Logs**, **Power** (reboot/shutdown), **Update** (host files, ID
  databases and OS packages), **Backup/Restore**.

Every write goes through a restricted helper, is validated, is checked against
a stale-form revision, and is rolled back if the service fails to restart.

## Hardware and OS support

| | Supported |
| --- | --- |
| Boards | Raspberry Pi 2 Model B and newer |
| Architecture | armhf (ARMv7+) today; arm64 is planned |
| OS image | Raspberry Pi OS Lite Trixie (Debian 13), armhf |
| Existing boards | Raspberry Pi OS Bookworm and Trixie |

## Quick start

Flash the image, or install the two packages on a Raspberry Pi OS you already
run. Both routes, including the migration of an existing Pi-Star board, are in
**[docs/INSTALL.md](docs/INSTALL.md)** ([Russian](docs/INSTALL.ru.md)).

## Repository layout

| Path | Contents |
| --- | --- |
| [`panel/`](panel) | Go backend: dashboard, management service, configuration writers, root helpers in `panel/helper/` |
| [`Pi-Star_dash_new/`](Pi-Star_dash_new) | Svelte UI (`src/`), the settings file `gospot.conf`, the `gospot` package (`debian/`, `scripts/`) and the Chromium test stands (`qa/`) |
| [`radio/`](radio) | The `gospot-radio` package: radio binaries, D-Star rebuilds, Python 3 service handlers, rename table, board migration |
| [`image/`](image) | Local Docker + pi-gen build of the Trixie armhf image, first-boot unit, offline verifier, QEMU boot test |
| [`Pi-Star_DV_Dash/`](Pi-Star_DV_Dash) | The classic dashboard, pinned as a read-only behavioural reference |
| [`docs/`](docs) | Top-level documentation, English and Russian |

Every path, port, unit name and service account lives in **one** file —
[`Pi-Star_dash_new/gospot.conf`](Pi-Star_dash_new/gospot.conf), installed as
`/etc/gospot/gospot.conf`. Code, helpers, packaging and tests read it; nothing
hard-codes a path.

## Verification

- Unit tests: Go (`go test ./...` in `panel/`), the Python helpers
  (`panel/helper/test_*.py`, `radio/test_*.py`) and the UI (`npm test`).
- 13 Chromium scenarios on an isolated stand with the real Go backend, real
  helpers and test systemd units — `Pi-Star_dash_new/qa/real-write/run.sh`
  (`--configure`, `--expert`, `--services`, `--backup`, `--css`, `--factory`,
  `--wifi`, `--calibration`, `--jitter`, `--dstar`, `--tgif`, `--hoseline`)
  and `qa/kiosk_screens.py`. Each opens the page, writes, re-reads, compares
  the files on disk byte for byte and checks the rollback.
- 81 offline checks on the finished image (`image/verify-artifact.sh`):
  packages and files, enabled and disabled units, absence of PHP/nginx/php-fpm
  and of per-board secrets, the packaged settings file against the repository
  copy, `visudo -c`, the Pi-Star layout, library resolution, UART settings.
  `image/qemu-boot.sh` then boots two "boards" and checks that their TLS
  certificates and SSH host keys differ.

## Reporting issues

Bugs, questions and feature requests go to
<https://github.com/shaposhnikoff/gospot/issues>; releases (both `.deb`
packages, the image, the checksums) are at
<https://github.com/shaposhnikoff/gospot/releases>.

## Licence and attribution

The radio software GoSpot ships is **Jonathan Naylor G4KLX's** work — MMDVMHost,
DMRGateway, the YSF/P25/NXDN/M17/DAPNET gateways, ircDDBGateway, DStarRepeater,
MMDVMCal and the cross-mode bridges — and remains under its own licences.

**Pi-Star**, by **Andy Taylor MW0MWZ**, is the reference whose system layout,
configuration format and dashboard behaviour GoSpot follows. GoSpot is an
independent project; it is not Pi-Star and carries no Pi-Star endorsement.

GoSpot's own code has no licence file yet.

## Planned

On the backlog, not shipped: arm64 packages and image, a `gospot` command-line
interface, and GoSpot's own host-file mirror (today the daily
`gospot-hostfiles.timer` fetches the lists and ID databases from `pistar.uk`).
