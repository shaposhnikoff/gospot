# `pistar-config`

Standalone Python 3 entrypoint for the existing Pi service configuration. Install
root-owned, mode `0755`, at `/usr/local/libexec/pistar-config`. Its only arguments
are `read`, `apply`, or `rollback`; exit status zero indicates success.

- `read` returns a JSON object with the seven public settings below.
- `apply` accepts one UTF-8 JSON object on stdin (8 KiB maximum). All eight
  fields are required; unknown and duplicate keys are rejected. An empty
  `password` keeps the existing secret.
- `rollback` restores the last successful content-changing apply's snapshot.
  A successful rollback consumes it. A no-op apply doesn't replace it.

```json
{"callsign":"UT3UDX","radio_id":2550430,"frequency":433700000,"uart":"/dev/ttyAMA0","claim":1234568,"master":"dmr.example.org","port":62031,"password":""}
```

`callsign` is 1–10 uppercase alphanumerics; `radio_id` and `claim` are positive
24-bit and 32-bit integers respectively. `frequency` is integer Hz
(1,000,000–1,300,000,000). `uart` accepts GPIO `/dev/ttyAMA0`, or an existing
USB serial character device (`/dev/ttyUSB<n>`, `/dev/ttyACM<n>`, or a
`/dev/serial/by-id/` alias resolving to one). `master` accepts an IPv4 address
or DNS hostname. `port` is 1–65535. Passwords may contain up to 128 printable
ASCII characters.

The helper locks `/var/lib/pistar-panel-config/` operations, atomically replaces
the two fixed INIs individually, and restarts changed services, gateway first.
The repository's service user owns `/etc/DMRGateway.ini`; apply and rollback
remove group/other access. Files in the private state directory, including the
rollback and pending snapshots, are mode `0600` and may contain credentials.
If the helper is interrupted mid-operation, mutations stop until an administrator
inspects `pending.json` (pre-operation host and gateway text, in that order),
reconciles the INIs and affected services, and removes that pending file.

Run isolated tests with `python3 -B -m unittest discover -s panel/helper`.
