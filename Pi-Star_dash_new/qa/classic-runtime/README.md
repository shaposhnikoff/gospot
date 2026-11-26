# Run the packaged PHP dashboard on a desktop

`run.sh` serves an existing stock release with real PHP 8.2 over HTTP. It runs once with an MMDVMHost station fixture and once with a D-Star repeater fixture. A failed assertion, unexpected shell probe, PHP warning/fatal/parse error or timeout exits nonzero.

From `Pi-Star_dash_new/`, on Linux with Bash, GNU coreutils, and access to local Docker (`/var/run/docker.sock`):

```bash
# One-time runtime download, or preload this digest in the CI image cache.
docker --host unix:///var/run/docker.sock pull php:8.2-cli@sha256:080193293786a2d9442806eca60635516120a30309b694c4292a56a4f8e2d353

# Pass the exact docroot printed by npm run package:stock:
bash qa/classic-runtime/run.sh build/release-XXXXXX/docroot
```

For a fresh CI checkout, place the upstream `Pi-Star_DV_Dash` checkout beside `Pi-Star_dash_new`, use the project's Node/npm dependencies and the cached Docker image, then:

```bash
set -euo pipefail
npm ci
root=$(npm run package:stock | sed -n 's|^Stock Pi-Star docroot.*: ||p')
bash qa/classic-runtime/run.sh "$root"
```

The verifier only reads the local release. Both HTTP server and client run inside each `--rm --network none` container on its private loopback. The root filesystem, package, fixtures and harness mounts are read-only. The non-root process has no added devices or capabilities; sessions, logs and generated fixture caches live in a 12 MiB tmpfs. Each run has a 40 s in-container deadline and a 60 s host deadline. It uses no station credentials, published ports, Docker socket mount or running Pi services.

## Evidence and limits

`--bm-drop-dynamic-simplex` seeds static TG91 and dynamic TG701 and TG702 on slot 0. Chromium selects TS2 and submits **Drop All Dynamic**. The local API must receive one authenticated `dropDynamicGroups/0` action; after reload the table must show TG91 and no dynamic subscriptions.

`--bm-static-simplex` selects TS2 on the simplex fixture and submits ADD and DEL for TG777. Chromium checks the original CSRF, feedback and refreshed subscription table for each action. The local API must receive slot `"0"` in the POST JSON and `/0/777` in the DELETE path, with no repeat on reload.

`--bm-drop-qso-simplex` checks the installed fixture configuration (`Duplex=0`, `Slot1=0`, `Slot2=1`) and submits Drop QSO with TS2 in Chromium. The API must receive `dropCallRoute/0` exactly once. The test reads the fixture's slot-0 call before submission and verifies its removal both before and after the automatic refresh.

`--bm-drop-qso-503` returns HTTP 503 for the authenticated Drop QSO action while profile reads remain available. Chromium must show `BrandMeister APIv2: No Response`; both fixture calls must survive the failed action and the timed refresh without a retry.

`--bm-drop-qso` seeds one active call per slot on the local BM API. Chromium submits **Drop QSO** on TS1. The test reads the stub's call routes before submission, after the command and after automatic reload. It also checks that subscriptions stay unchanged and that only one authenticated `dropCallRoute/1` request was received.

`--bm-drop-dynamic` runs a separate BM fixture with two dynamic groups on TS1, one on TS2, and a static group on each slot. Chromium selects TS1 and clicks **Drop All Dynamic**. The audit distinguishes the authenticated action GET from profile reads and checks that the timed reload leaves the action count at one.

`bash qa/classic-runtime/run.sh build/release-Ojl5GU/docroot --bm-manager` exercises BrandMeister static TG add/delete on a two-slot fixture. The container resolves `api.brandmeister.network` to its own HTTPS stub. Chromium selects TS1, submits TG777, and verifies feedback and the subscriptions table after each timed GET. The stub audits the authenticated JSON POST and path-based DELETE without recording the token; TG91 on TS2 must survive both actions.

`bash qa/classic-runtime/run.sh build/release-Ojl5GU/docroot --m17-manager` enables M17 Network and selects both `M17-QAT` from `M17Hosts.txt` and room `Q` in Chromium. Both forms submit the reflector and room. Link must request `16076 Reflector M17-QAT Q`; UnLink must request `16076 Reflector unlink` without a room argument. The logging-only stub verifies the arguments without sending any packets to a gateway.

`bash qa/classic-runtime/run.sh build/release-Ojl5GU/docroot --nxdn-manager` enables NXDN Network with TG 23456 from `NXDNHosts.txt` and port 16075 from `/etc/nxdngateway`. The expected `RemoteCommand` arguments are `16075 TalkGroup 23456` for Link and `16075 TalkGroup unlink` for UnLink. The logging-only stub must receive exactly one call per submitted form and none on automatic reload.

`bash qa/classic-runtime/run.sh build/release-Ojl5GU/docroot --ysf-manager` enables System Fusion Network with a loopback test reflector. Use `--p25-manager` for P25 Network with TG 31415 from `P25Hosts.txt`: Link must request `TalkGroup 31415`, UnLink must request `TalkGroup 9999`, both on the configured remote port 16074. Chromium submits each action from the real admin form. Each POST is checked against the form's CSRF token, HTTP status, visible Command Output and the stub's argument log after submission and reload. Only these fixtures enable `proc_open` to launch the mounted logging-only `RemoteCommand` with an argument array, without sudo or a shell; they never contact a gateway or reflector.

`bash qa/classic-runtime/run.sh build/release-XXXXXX/docroot --catalog` inventories links from the installed catalog in Chromium, then issues one GET per distinct reviewed path/query without following redirects, executing page scripts, or submitting forms. Per-mode rows include item IDs, fragments, HTTP status, content checks, PHP diagnostics and reasons for skips/failures. Expert staging copies go only to private temp files; system probes use synthetic inputs.

For the end-to-end Svelte/Last Heard check, run `bash qa/classic-runtime/run.sh build/release-XXXXXX/docroot --operator-browser`. This variant connects host Chromium (using `chromedriver` and `socat`) over a loopback-only relay to PHP on a Docker internal network. In each station mode it captures the browser's own fetch response and compares the ordered visible cells or empty-state message to that response.

| Check | What a pass means |
| --- | --- |
| `/api/last_heard.php` | The endpoint and its MMDVM parser/helpers are byte-identical to the sibling upstream source. Two synthetic DMR transmissions give the expected newest-first JSON, all ten string fields (including RF/network, duration, loss and BER), and query limits `1` and `0`. |
| D-Star-only station | The same API returns `[]` when only a D-Star header log exists: upstream does not parse `Headers.log` into this JSON endpoint. The public/admin HTML and both D-Star partials display the D-Star fixture callsign. The two single-row grep/sort **outputs** are preseeded; the upstream shell pipelines are not executed. |
| PHP routes and dependencies | Both index variants, mode-specific last heard and local TX fragments, expert menu, log viewer, configuration, backup, update, power, Wi-Fi information, and referenced core assets render. The tests fetch the packaged classic shell JS/CSS and `/operator/` entry module; they do not execute JS or verify browser layout. |
| Live Logs AJAX | In both modes the PHP handler starts at EOF, returns exactly a line appended to the dated log in a separate 1 MiB `/var/log/pi-star` tmpfs, then returns an empty body on the next poll in the same session. |
| Upstream forms | On five form pages, GET supplies an actual hidden token and session cookie. Same-session POSTs with missing and wrong tokens yield the styled `403`; a fresh session receives upstream's `303` back to the same form. No action fields, valid-token POSTs, uploads or redirect following. |

PHP's native command-execution functions are disabled on the server except as described for the RemoteCommand stub. `probes.php` implements fixture responses for read-only probes such as process listing, temperature, hostname and Wi-Fi status. Unknown `exec` calls fail. The verifier does not exercise station authentication/TLS, privileged editors (some stage files on GET), adapter scans, configuration writes, radio hardware, gateways or peer connectivity. It tests PHP's built-in HTTP server rather than the production web-server configuration.

Observed with the pinned image: PHP 8.2.36, both fixture modes passed with clean server warning/fatal/parse logs.
