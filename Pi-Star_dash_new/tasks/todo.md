# Native migration working plan

Authoritative scope and status: [migration issue #1](https://github.com/shaposhnikoff/pi-star-dashboard-new/issues/1). Project instructions: [AGENTS.md](../AGENTS.md).

Current implementation slice: [native package #2](https://github.com/shaposhnikoff/pi-star-dashboard-new/issues/2).

Immediate correction: verify the interactive preview through one forwarded origin. Start an isolated Go/helper fixture; replace guessed management ports with preview runtime navigation; test actual catalog/editor/back transitions and a persisted fixture write in Chromium. Keep the service already using host port 8788 untouched.

1. Replace classic packaging with independently assembled native UI assets.
2. Serve the operator UI through Go; preserve read-only status access and same-board management navigation.
3. Reject PHP and classic runtime files during packaging; verify the exact ARM executable and assets.
4. Run targeted unit, build, package and Chromium checks; record evidence and update the issue.
5. Continue with operation contracts and the remaining module issues linked from #1.

## Full port in the classic look (decided 2026-11-27)

Main page done (`ClassicDashboard.svelte`, `/api/dashboard`, release `deb-10`). Remaining phases, each deployed to pistar3:

1. **Radio platform package (`pistar-radio`, armhf).** Pi-Star PS4 binaries → `/usr/local/bin`; wiringPi + ArduiPi_OLED libs; reusable sbin scripts and service handlers → `/usr/local/sbin`; systemd units calling the handlers; clean configs from `config_clean.zip` as defaults; `/etc/pistar-release`; `/var/log/pi-star`; daily HostFilesUpdate timer without git pulls or PHP. Migrate pistar3: translate `/etc/MMDVM-Host.ini` + `/etc/DMRGateway.ini` into `/etc/mmdvmhost` + `/etc/dmrgateway`, keep a rollback copy, verify modem answers and DMR network.
2. **Backend on Pi-Star layout.** Read `/etc/mmdvmhost` etc.; tail `/var/log/pi-star/MMDVM-*.log` and gateway logs (port `getHeardList`/`getActualLink` for all modes); hosts files for master/reflector names; helpers write Pi-Star paths.
3. **Admin shell in classic look.** `/admin/` index (system info, link managers BM/TGIF/YSF/P25/NXDN/M17/D-Star), header menu Dashboard | Admin | Live Logs | Power | Update | Configuration.
4. **Configuration** (`configure.php`) — all sections, both controllers.
5. **Expert**: Quick Edit (generic section editor per file), Full Edit (text editors), Tools (SSH, jitter, modem firmware, upgrade), API keys.
6. **Wi-Fi, Power, Update (Go: host files + apt, no dashboard git pull), Backup/Restore, Live Logs, Calibration.**

### Status 2026-11-27 (each closed with unit tests, a Chromium stand scenario and pistar3)

- [x] Main page, Admin page, BM links/manager (BM profile numbers as strings fixed, deb-30)
- [x] Live Logs, Power, Update, Expert Quick/Full Edit, API Keys (long BM JWT), FW, SSH
- [x] Backup/Restore (classic zip), Configuration (all sections, untouched form writes nothing)
- [x] CSS Tool + theme, Factory Reset (stand)
- [x] `pistar-radio` 0.1.0-2 seeds missing configs from `config_clean.zip`
- [ ] Wi-Fi (NetworkManager + wpa_supplicant backends) — in progress
- [ ] Calibration (MMDVMCal) and Jitter test — in progress
- [ ] TGIF manager (static TG API + legacy link manager) — in progress
- [ ] D-Star stack for wxWidgets 3.2, Trixie image armhf → arm64, kiosk calls screen

### Deferred (user decision 2026-11-27)

- **Displays (OLED, Nextion, HD44780, TFT): skipped, the user does not use screens.** The PS4 `MMDVMHost` still links `libArduiPi_OLED.so.1`, so `radio/build-libs.sh` ships the library only to let the binary start; no display configuration, `pistar-oledtext.py`/`pistar-nextiontext.py` handling, NextionDriver unit or display settings pages until requested.

## OS image (Trixie, armhf first, arm64 later, local build only)

All three boards are Raspberry Pi 2 Model B (Bookworm armhf); Rev 1.1 boards cannot run arm64. Steps 2–5 target armhf first; repeat them for arm64 only after the armhf image passes verification.

Decisions recorded in [AGENTS.md](../AGENTS.md#образ-ос). Existing `image/` pipeline targets Bookworm armhf, installs a raw panel binary, and its last run (`image-builds/mmdvm-build.8MV6XxTKuZ`) stopped in `stage2/01-sys-tweaks` without an image.

1. Commit the pending native-package work so the image consumes a known revision.
2. Build `pistar-dashboard` for both `armhf` and `arm64` (`GOARCH` + `Architecture` in control); install-test each on Trixie under qemu.
3. Package radio services as pinned-revision `.deb`s per arch (MMDVMHost, DMRGateway, YSFClients, P25Clients, NXDNClients, DAPNETGateway, ircDDBGateway, DStarRepeater, …) with units, default INIs and conffiles.
4. Move `image/build.sh` to pi-gen `master` (trixie/armhf) and `arm64` branches, pinned by commit; custom stage installs the local `.deb` set via `apt install ./*.deb`.
5. Verify the image offline: mount rootfs, check packages, units enabled, no PHP, firstboot/TLS/password flow; then boot in QEMU and probe `:8789` and `:8788/manage/`.
6. Optionally flash a spare card for a hardware smoke test — only with explicit user approval; never touch the working Raspberry Pi boards.

Status 2026-11-27 (branch `mod-image`): armhf steps 4–5 implemented in `image/` (`build.sh`, `stage-local/`, `verify-artifact.sh`, `qemu-boot.sh`, see [image/README.md](../../image/README.md)); pi-gen pinned to `291c62c1` (tag `2026-10-06-raspios-trixie-armhf`). Radio services ship as one `pistar-radio` package for now, not one `.deb` per service (step 3 remains open). Evidence: `qa/evidence/image-*.txt`.

Existing BrandMeister, helper, wizard and browser-test edits predate this slice and must be preserved. Do not infer full migration or pilot deployment from package checks.
