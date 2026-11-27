# GoSpot panel: first-stage rollout

На странице статуса слева показаны позывной, ID, RX/TX и включённые
режимы из `/etc/MMDVM-Host.ini`. Справа — подтверждения от модема и
DMRGateway с временем события (UTC), затем Last Heard по DMR и его
RF-подборка. При отсутствии данных панель оставляет поле пустым или
показывает причину. Обновление страницы — каждые 15 секунд.

The coordinator runs this after merging the panel on the build host. Target:
`pistar3` (`192.168.210.194`, ARMv7). Use Go locally, an already verified SSH
host key for alias `pistar3`, and noninteractive sudo on the Pi. Port 8787 must
be available on the trusted management LAN: `:8787` listens on all interfaces.

## Baseline and install

From `panel/`, capture the boot ID and radio process identities before installation:

```sh
ssh pistar3 'cat /proc/sys/kernel/random/boot_id; systemctl show mmdvmhost dmrgateway -p Id -p ActiveState -p MainPID -p InvocationID'
go test ./...
./deploy.sh
```

The script takes no target argument. It builds the current package locally with
`CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7`, checks the remote hostname,
installs `/usr/local/bin/gospot-panel` and `gospot-panel.service`, and starts the
panel under a dedicated system account with journal access and no login shell. Repeat the baseline
command: both radio services should still be active, with identical invocation
IDs, PIDs and boot ID.

## Verify the first node

```sh
ssh pistar3 'systemctl show gospot-panel -p ActiveState -p SubState -p DynamicUser -p SupplementaryGroups -p NRestarts; journalctl -u gospot-panel -b -n 30 --no-pager'
curl -fsS --retry 3 --retry-delay 5 --max-time 5 http://192.168.210.194:8787/api/status
ssh pistar3 'sudo -n journalctl --utc -u mmdvmhost -u dmrgateway --since "15 minutes ago" --no-pager'
```

Open `http://192.168.210.194:8787/` in a browser. Compare the displayed node,
modem and master status with the JSON snapshot and the Pi's journal. A 503
before the first snapshot is expected. On the host running the
master (`192.168.10.11`), inspect independent evidence for this peer:

```sh
docker logs --timestamps --since 15m dvserver 2>&1 | grep -Ei '1234570|gospot-3|homebrew'
```

Match **claim 1234570 / principal 1005** specifically; other peers share the
callsign UT3UDX. Compare a naturally occurring session or TG91/TG92 call in
the same UTC window, checking time, talkgroup and slot when available. An
active local gateway alone does not establish master connectivity. If the peer
has been idle, widen the window or record the traffic check as pending; do not
force a reconnect. After several refreshes verify `NRestarts` has not grown.

## Failure fixtures (local only)

Exercise the panel's collector/parser with saved or synthetic observations in
local tests; do not stop a live service or alter node configuration to create
these cases. Use one known-good observation as the control, vary one input at a
time, and check the rendered status as well as the process staying responsive:

| Fixture input | Acceptance check |
| --- | --- |
| Gateway inactive; previous journal contains a successful login | Inactive gateway is visible; old success is not proof of a current link. |
| Modem open/handshake failure while the host process remains active | Modem is not shown as healthy merely because the process is running. |
| Active gateway; journal ends with disconnect after login | No current successful link inferred from the earlier login. |
| Journal unavailable (permission error), or empty after rotation | Missing evidence is distinguishable from confirmed traffic; other fields still render. |
| Truncated/unrecognised record mixed with valid activity | No crash or invented call; valid records remain usable. |

## Rollback

For a failed first install, remove just the panel on `pistar3`:

```sh
ssh pistar3 'sudo -n systemctl disable --now gospot-panel.service && sudo -n rm -f /etc/systemd/system/gospot-panel.service /usr/local/bin/gospot-panel /usr/local/bin/gospot-panel.new && sudo -n systemctl daemon-reload'
```

Repeat the baseline check. For a later update, redeploy the last known-good
panel revision instead.
