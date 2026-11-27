# GoSpot OS image (Raspberry Pi OS Trixie, armhf)

`image/build.sh` builds a Raspberry Pi OS Lite **Trixie (Debian 13) armhf**
image locally with Docker and a pinned [pi-gen](https://github.com/RPi-Distro/pi-gen).
The radio software comes from two local Debian packages installed with
`apt-get install ./…deb` inside the image, so their dependencies resolve from
the Raspberry Pi OS archive:

| Package | Built by |
| --- | --- |
| `pistar-radio_*_armhf.deb` | `radio/build-deb trixie` (PS4 radio binaries, D-Star daemons rebuilt for wxWidgets 3.2, Pi-Star tools and units) |
| `pistar-dashboard_*_armhf.deb` | `Pi-Star_dash_new/scripts/build-deb` (Go panel, UI, helpers; no PHP) |

The image is built **only locally**. CI builds the `.deb` packages, never the
image. Every path, name and port used by the build, the checks and the first
boot is in the settings file named by [`image/settings.path`](settings.path)
(now [`Pi-Star_dash_new/pistar.conf`](../Pi-Star_dash_new/pistar.conf): `IMAGE_*`,
`BOOT_*`, `RADIO_PACKAGE`, `DASHBOARD_PACKAGE`, `WIZARD_SECRETS_HELPER` and the
dashboard and radio keys). The installed path of that file is taken from the
dashboard package's conffiles; package, unit, image and host names are
settings, so a renamed product rebuilds by changing the settings only.

## Build

Requirements: Linux x86-64 or arm64 host, rootful Docker that allows
privileged containers and loop devices, `binfmt_misc` with an ARM entry that
has the `F` flag (`qemu-user-static`/`qemu-user-binfmt`), Git, Go, Node/npm
(dashboard UI), `dpkg-deb`, about 25 GB free under `/var/tmp` and network
access to GitHub, Docker Hub and the Raspberry Pi OS archive. The radio build
also needs `../Pi-Star_v4_Binaries_Bin` and `../Pi-Star_Binaries_sbin` next to
`Pi-Star_dash_new/` (`PISTAR_BINARIES_*_DIR`); in a git worktree link them in.

```sh
./image/build.sh                    # packages + image + offline verification
./image/build.sh --prepare-only     # packages + pi-gen context, no Docker build
RADIO_DEB=… DASHBOARD_DEB=… ./image/build.sh   # reuse built packages
./image/qemu-boot.sh /var/tmp/pistar-image.*/pi-gen/deploy/*.img.xz 2   # boot two "boards"
```

Each run creates `IMAGE_BUILD_PARENT/pistar-image.XXXXXXXXXX/` (default
`/var/tmp`, outside the repository). The image, its `.sha256`, a `.manifest`
(pi-gen commit, source commit, package versions and SHA-256) and the build logs
end up in `pi-gen/deploy/`. `--arch` selects the architecture (`IMAGE_ARCH`,
only `armhf` now); arm64 needs a pinned pi-gen `arm64` branch commit and arm64
packages first.

Stages: `stage0 → stage1 → stage2` (Lite, not exported) `→ stage-local`:

| Sub-stage | Does |
| --- | --- |
| `00-packages` | `apt-get install` of both local packages; enables `IMAGE_ENABLE_UNITS` (panel, management, operator panel, host-file timer); removes the management credentials the postinst created in the build container |
| `01-firstboot` | installs `pistar-firstboot` and its unit, ordered before `pistar-wizard.service` |
| `02-uart` | `enable_uart=1`, no serial console on the modem UART, serial gettys masked |

pi-gen settings: hostname `IMAGE_HOSTNAME` (`pi-star`), first user
`PISTAR_LINUX_USER` (`pi-star`) with the Pi-Star password `raspberry`,
`DISABLE_FIRST_BOOT_USER_RENAME=1`, SSH enabled, cloud-init kept for
Raspberry Pi Imager customisation (`IMAGE_ENABLE_CLOUD_INIT`). pi-gen deletes
the SSH host keys; every board generates its own on first boot.

The pinned commits fix the **inputs**; Raspberry Pi OS packages, kernel and
firmware come from the live archive at build time, so two builds of the same
sources are not byte-identical. Keep the manifest and SHA-256 with a release.

## First boot and logins

No credential is shared between boards flashed from one image:

- The first-boot unit (`IMAGE_FIRSTBOOT_UNIT`, `pistar-firstboot.service`) runs once (until `IMAGE_FIRSTBOOT_DONE` exists),
  after the network is up and before `pistar-wizard.service`. It deletes any
  management credential in `WIZARD_CONFIG_DIR` and runs the dashboard
  package's credentials helper (`WIZARD_SECRETS_HELPER`) — the code path of the package postinst —
  which creates this board's TLS key and self-signed certificate
  (`CN=pi-star`, SAN with the host name and current addresses). An
  interrupted first boot writes no marker and runs again on the next boot.
- SSH host keys are generated on first boot by Raspberry Pi OS.
- Radio units stay **disabled**, as after installing `pistar-radio`; enable
  the modes in use from the dashboard or with `systemctl enable --now`.

Default logins, as on a classic Pi-Star image:

| Where | User | Password |
| --- | --- | --- |
| SSH / console | `pi-star` | `raspberry` |
| Management (admin) web UI, `https://pi-star:8788/manage/` | `pi-star` | `raspberry` |
| Operator panel, `http://pi-star:8789/` | — | — |

Change the password in **Configuration > Remote Access Password**: it sets
the admin web password and the `pi-star` SSH password together (the page, like
Pi-Star, refuses `raspberry`). The management certificate is self-signed;
compare its fingerprint on the console before trusting it:

```sh
openssl x509 -in /etc/pistar-wizard/tls-cert.pem -noout -fingerprint -sha256
```

## Verification

- `image/stage-local/01-firstboot/tests/test_firstboot.sh` — unit tests of
  the first boot and the shared credentials helper (unprivileged).
- `image/verify-artifact.sh IMAGE RADIO_DEB DASHBOARD_DEB` (run by
  `build.sh`) attaches the image read-only through a loop device (`sudo`) and
  checks package versions and files, enabled/disabled units, absence of PHP,
  nginx and php-fpm, absence of credentials and SSH host keys, the packaged
  `pistar.conf` against the repository copy, sudoers (`visudo -c` in the
  image), the Pi-Star layout, library resolution and the UART settings.
- `image/qemu-boot.sh IMAGE [BOARDS]` boots fresh copies under QEMU
  `raspi2b` with the image's kernel inside a disposable container and probes
  `:8789`, `:8788/manage/` (401 without credentials, 200 with the defaults),
  SSH, first-boot state and unit states; with two boards it checks that TLS
  certificates and SSH host keys differ.

`image/stage-mmdvm/00-install/files/` is not part of the image any more: it
holds the unit templates and INI fixtures used by the dashboard package and
its test stands.
