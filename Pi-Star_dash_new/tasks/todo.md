# Native migration working plan

Authoritative scope and status: [migration issue #1](https://github.com/shaposhnikoff/pi-star-dashboard-new/issues/1). Project instructions: [AGENTS.md](../AGENTS.md).

Current implementation slice: [native package #2](https://github.com/shaposhnikoff/pi-star-dashboard-new/issues/2).

Immediate correction: verify the interactive preview through one forwarded origin. Start an isolated Go/helper fixture; replace guessed management ports with preview runtime navigation; test actual catalog/editor/back transitions and a persisted fixture write in Chromium. Keep the service already using host port 8788 untouched.

1. Replace classic packaging with independently assembled native UI assets.
2. Serve the operator UI through Go; preserve read-only status access and same-board management navigation.
3. Reject PHP and classic runtime files during packaging; verify the exact ARM executable and assets.
4. Run targeted unit, build, package and Chromium checks; record evidence and update the issue.
5. Continue with operation contracts and the remaining module issues linked from #1.

## OS image (Trixie, armhf first, arm64 later, local build only)

All three boards are Raspberry Pi 2 Model B (Bookworm armhf); Rev 1.1 boards cannot run arm64. Steps 2–5 target armhf first; repeat them for arm64 only after the armhf image passes verification.

Decisions recorded in [AGENTS.md](../AGENTS.md#образ-ос). Existing `image/` pipeline targets Bookworm armhf, installs a raw panel binary, and its last run (`image-builds/mmdvm-build.8MV6XxTKuZ`) stopped in `stage2/01-sys-tweaks` without an image.

1. Commit the pending native-package work so the image consumes a known revision.
2. Build `pistar-dashboard` for both `armhf` and `arm64` (`GOARCH` + `Architecture` in control); install-test each on Trixie under qemu.
3. Package radio services as pinned-revision `.deb`s per arch (MMDVMHost, DMRGateway, YSFClients, P25Clients, NXDNClients, DAPNETGateway, ircDDBGateway, DStarRepeater, …) with units, default INIs and conffiles.
4. Move `image/build.sh` to pi-gen `master` (trixie/armhf) and `arm64` branches, pinned by commit; custom stage installs the local `.deb` set via `apt install ./*.deb`.
5. Verify the image offline: mount rootfs, check packages, units enabled, no PHP, firstboot/TLS/password flow; then boot in QEMU and probe `:8789` and `:8788/manage/`.
6. Optionally flash a spare card for a hardware smoke test — only with explicit user approval; never touch the working Raspberry Pi boards.

Existing BrandMeister, helper, wizard and browser-test edits predate this slice and must be preserved. Do not infer full migration or pilot deployment from package checks.
