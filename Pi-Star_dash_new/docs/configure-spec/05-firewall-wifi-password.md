# configure.php spec, part 05: Firewall Configuration, Wireless Configuration / Auto AP PSK, Remote Access Password

Reference: `Pi-Star_DV_Dash/admin/configure.php` at revision `10bf34c9` (5765 lines). All line numbers below refer to that file unless another file is named.
Other files used: `config/config_writer.php`, `config/csrf.php`, `config/banner_warnings.inc`, `functions.js` (served as `/functions.js`, the `checkPass`/`checkPsk`/`checkPskMatch` functions), `admin/wifi.php`, `lang/english_uk.php`, and from the sibling repo `Pi-Star_Binaries_sbin/`: `pistar-upnp.service`, `pistar-ap.service`, `pistar-upgrade` (sudoers it generates), `pistar-firewall`.

Labels written as `$lang['x']` are resolved to the English UK strings from `lang/english_uk.php`:

| key | English |
|---|---|
| `fw_config` | Firewall Configuration |
| `fw_dash` | Dashboard Access |
| `fw_irc` | ircDDBGateway Remote |
| `fw_ssh` | SSH Access |
| `wifi_config` | Wireless Configuration |
| `remote_access_pw` | Remote Access Password |
| `setting` | Setting |
| `value` | Value |
| `apply` | Apply Changes |
| `user` | User Name |
| `password` | Password |
| `set_password` | Set Password |

This part covers three separate HTML forms:

1. **Firewall Configuration** (l.5581-5663). Its rows sit at the end of the main form `<form id="config">` (opened l.4006, closed l.5665) and post with every other main-form field through `submitform()`.
2. **Wireless Configuration** (l.5667-5694). This is an iframe to `wifi.php` plus its own form, `<form id="autoApPassForm">`, which sets the Auto AP PSK.
3. **Remote Access Password** (l.5696-5711), in its own form `<form id="adminPassForm">`.

Rendering context. All three blocks render only in the GET/no-POST branch: the `else:` at l.3946 of `if (!empty($_POST)):` (l.355), closed by `<?php endif; ?>` at l.5712. They are also inside `if ($_SERVER["PHP_SELF"] == "/admin/configure.php")` (l.335). The firewall block is **not** inside any controller or mode conditional. The preceding `<?php } ?>` at l.5579 closes the Mobile GPS block. The rows therefore appear for DStarRepeater, for MMDVMHost and before a controller is chosen.

---

## 1. Firewall Configuration (l.5581-5663)

### 1.1 Layout

```
<h2>Firewall Configuration</h2>                       (l.5581)
<table>                                              (l.5582)
 <tr>
  <th width="200"> tooltip "Setting" </th>
  <th colspan="2"> tooltip "Value" </th>
 </tr>
 <tr> label td | td colspan=2 radios </tr>        Dashboard Access
 <tr> label td | td colspan=2 radios </tr>        ircDDBGateway Remote
 <tr> label td | td colspan=2 radios </tr>        SSH Access
 [if /etc/default/hostapd exists]
 <tr> label td | td radios (no colspan) | td "Note: Reboot Required if changed" </tr>   Auto AP
 <tr> label td | td colspan=2 radios </tr>        uPNP
</table>
<div><input type="button" value="Apply Changes" onclick="submitform()" /></div>   (l.5664)
</form>                                              (l.5665, closes the main config form)
```

Header cells (l.5584-5585):
- `<th width="200"><a class="tooltip" href="#">Setting<span><b>Setting</b></span></a></th>`
- `<th colspan="2"><a class="tooltip" href="#">Value<span><b>Value</b>The current value from the<br />configuration files</span></a></th>`

The Value tooltip text here is "The current value from the<br />configuration files". This is a different line break from the Control Software header at l.4012 ("from<br />the configuration").

Label cells use `<td align="left"><a class="tooltip2" href="#">LABEL:<span><b>TITLE</b>TEXT</span></a></td>`. The label is followed by a colon. The radio cells are `<td align="left" ...>`. Each radio pair is `<input type="radio" name=X value=A ... />TextA <input type="radio" name=X value=B ... />TextB`: the text sits directly after the input, with no `<label>` element.

Section button (l.5664): `<input type="button" value="Apply Changes" onclick="submitform()" />`. This is the last Apply button of the main form. It has no `<br /><br />` after it, unlike the other sections. `submitform()` (l.274) calls `disablesubmitbuttons()`, which disables **every** `input[type=button]` on the page, then submits `#config`.

### 1.2 Fields

#### 1.2.1 `dashAccess`: Dashboard Access (l.5587-5604)

- Type: radio, values `PRV` (text "Private") and `PUB` (text "Public"), cell `colspan="2"`.
- Label: `Dashboard Access:`. Tooltip title **Dashboard Access**, text: "Do you want the dashboard access to be publicly available? This modifies the uPNP firewall configuration."
- Displayed value (l.5599-5600):
  ```php
  $testPrvPubDash = exec('grep "80 80" /usr/local/sbin/pistar-upnp.service | cut -c 1');
  if (substr($testPrvPubDash, 0, 1) === '#') { /* PRV checked */ } else { /* PUB checked */ }
  ```
  - Source: `/usr/local/sbin/pistar-upnp.service`, a bash script and not an INI file. The PHP reads the first character of the **last** line containing `80 80`, because `exec()` returns the last output line.
  - The control shows `#` as Private. Any other value shows Public: a tab, an empty result, a missing file or an unreadable file.
  - The read uses plain `grep` without sudo (comment at l.5590-5598: the file is mode 755, and the old `sudo grep` silently forced Public).
  - Stock file (`Pi-Star_Binaries_sbin/pistar-upnp.service` l.43): `#\t\t$DAEMON -u ${igdURL} -e ${hostVar}_Dash -a $ipVar 80 80 TCP > /dev/null 2>&1`, so Private is the default.
- Save (l.2414-2421), inside the main POST flow:
  ```php
  if (empty($_POST['dashAccess']) != TRUE ) {
    $publicDashboard  = 'sudo sed -i \'/$ipVar 80 80/c\\\t\t$DAEMON -u ${igdURL} -e ${hostVar}_Dash -a $ipVar 80 80 TCP > /dev/null 2>&1\' /usr/local/sbin/pistar-upnp.service';
    $privateDashboard = 'sudo sed -i \'/$ipVar 80 80/ s/^#*/#/\' /usr/local/sbin/pistar-upnp.service';
    if ($_POST['dashAccess'] === 'PUB' ) { system($publicDashboard); }
    if ($_POST['dashAccess'] === 'PRV' ) { system($privateDashboard); }
  }
  ```
  - `PUB`: replaces every line matching the literal text `$ipVar 80 80` (in this BRE a `$` that is not at the end is literal) with `<TAB><TAB>$DAEMON -u ${igdURL} -e ${hostVar}_Dash -a $ipVar 80 80 TCP > /dev/null 2>&1`. This was verified with GNU sed 4.9: the PHP single-quoted `c\\\t\t` reaches sed as `c\\t\t`, which writes two real tab characters.
  - `PRV`: `s/^#*/#/` on the matching lines collapses any run of leading `#` to exactly one `#`, so it is idempotent. The rest of the line is kept.
  - Any other value: no action. No other sanitisation is done; only the two exact strings are acted on.
  - The new state takes effect at the next `pistar-upnp.service start` (cron every 5 min, or the restart in `uPNP` below). The handler does **not** call `upnpc -d` to remove an existing port 80 mapping. Removal happens only on `pistar-upnp.service stop`, which deletes ports 22, 80, 10022 and the D-Star ports.
  - **Bug to keep in mind:** in the stock script these `$DAEMON` lines sit inside `start) ( ... )` but the dashboard line is **outside** the `if ! pgrep ircDDBGateway` block (l.42-43). The ircDDBGateway remote line (l.46) is **inside** that block. The `if` is negated, so port 10022 and the D-Star ports are opened only when ircDDBGateway is **not** running. This is upstream behaviour; it is noted here, not resolved.

#### 1.2.2 `ircRCAccess`: ircDDBGateway Remote (l.5606-5619)

- Type: radio `PRV` "Private" / `PUB` "Public", `colspan="2"`.
- Label: `ircDDBGateway Remote:`. Tooltip title **ircDDBGateway Remote**, text: "Do you want the ircDDBGateway remote control access to be publicly available? This modifies the uPNP firewall Configuration." ("Configuration" has a capital C here.)
- Displayed value (l.5614): `exec('grep "10022 10022" /usr/local/sbin/pistar-upnp.service | cut -c 1')`. A first character of `#` checks Private; anything else checks Public. Stock file l.46 is commented, so the default is Private.
- Save (l.2423-2430):
  - `PUB`: `sudo sed -i '/$ipVar 10022 10022/c\\t\t\t$DAEMON -u ${igdURL} -e ${hostVar}_Remote -a $ipVar 10022 10022 UDP > /dev/null 2>&1' /usr/local/sbin/pistar-upnp.service`. The replacement line starts with **three** tabs, verified.
  - `PRV`: `sudo sed -i '/$ipVar 10022 10022/ s/^#*/#/' /usr/local/sbin/pistar-upnp.service`.

#### 1.2.3 `sshAccess`: SSH Access (l.5621-5635)

- Type: radio `PRV` "Private" / `PUB` "Public", `colspan="2"`.
- Label: `SSH Access:`. Tooltip title **SSH Access**, text: "Do you want access to be publicly available over SSH (used for support issues)? This modifies the uPNP firewall Configuration."
- Displayed value (l.5628): `exec('grep "22 22" /usr/local/sbin/pistar-upnp.service | cut -c 1')`. `#` checks Private; anything else checks Public. The grep pattern `22 22` does not match the `10022 10022` line, and no stop line contains `22 22`, so in the stock file it matches only l.42. The stock default is Private.
- Save (l.2432-2439):
  - `PUB`: `sudo sed -i '/$ipVar 22 22/c\\t\t$DAEMON -u ${igdURL} -e ${hostVar}_SSH -a $ipVar 22 22 TCP > /dev/null 2>&1' /usr/local/sbin/pistar-upnp.service` (two tabs).
  - `PRV`: `sudo sed -i '/$ipVar 22 22/ s/^#*/#/' /usr/local/sbin/pistar-upnp.service`.

#### 1.2.4 `autoAP`: Auto AP (l.5637-5649)

- Visibility: only when `file_exists('/etc/default/hostapd')` (l.5637), meaning the hostapd package is installed.
- Type: radio `ON` "On" / `OFF` "Off". The radio cell has **no colspan**. A third cell follows with the plain text `Note: Reboot Required if changed` (l.5647).
- Label: `Auto AP:` (a literal string, not from `$lang`). Tooltip title **Auto AP**, text: "Do you want your Pi to create its own AP if it cannot connect to WiFi within 120 secs of boot" (no trailing period).
- Displayed value (l.5641): `file_exists('/etc/hostap.off')` checks Off; otherwise On is checked.
- Save (l.645-649). This runs early in the main flow, after the `count($configmmdvm) <= 18` abort (l.621) and `controllerSoft` (l.639):
  ```php
  if (empty($_POST['autoAP']) != TRUE ) {
    if ($_POST['autoAP'] === 'OFF') { system('sudo touch /etc/hostap.off'); }
    if ($_POST['autoAP'] === 'ON')  { system('sudo rm -rf /etc/hostap.off'); }
  }
  ```
- Effect: `pistar-ap.service` (`Pi-Star_Binaries_sbin/pistar-ap.service` l.24-27) exits early when `/etc/hostapd/hostapd.conf` is missing or `/etc/hostap.off` exists. No service is restarted, hence the "Reboot Required" note.
- Note: visibility checks `/etc/default/hostapd`, but the AP service needs `/etc/hostapd/hostapd.conf`, and the PSK form (§2) is gated on the `wlan*` interfaces. These three conditions are independent.

#### 1.2.5 `uPNP`: uPNP (l.5650-5662)

- Type: radio `ON` "On" / `OFF` "Off", `colspan="2"`.
- Label: `uPNP:` (literal). Tooltip title **uPNP**, text: "Do you want your Pi to create its own Firewall rules for use with D-Star."
- Displayed value (l.5654): `exec('grep "pistar-upnp.service" /etc/crontab | cut -c 1')`. A first character of `#` checks Off. Anything else checks **On**, including when no such line exists in `/etc/crontab`.
- Save (l.2441-2452):
  ```php
  $uPNPon      = 'sudo sed -i \'/pistar-upnp.service/c\\*/5 *\t* * *\troot\t/usr/local/sbin/pistar-upnp.service start > /dev/null 2>&1 &\' /etc/crontab';
  $uPNPoff     = 'sudo sed -i \'/pistar-upnp.service/ s/^#*/#/\' /etc/crontab';
  $uPNPsvcOn   = 'sudo systemctl enable pistar-upnp.timer';
  $uPNPsvcOff  = 'sudo systemctl disable pistar-upnp.timer';
  $uPNPsvcStart= '(sudo systemctl stop pistar-upnp.service && sudo systemctl start pistar-upnp.service) > /dev/null 2>&1 &';
  $uPNPsvcStop = '(sudo systemctl stop pistar-upnp.service) > /dev/null 2>&1 &';
  if ($_POST['uPNP'] === 'ON' )  { system($uPNPon);  system($uPNPsvcOn);  system($uPNPsvcStart); }
  if ($_POST['uPNP'] === 'OFF' ) { system($uPNPoff); system($uPNPsvcStop); system($uPNPsvcOff); }
  ```
  - `ON`: every `/etc/crontab` line containing `pistar-upnp.service` becomes exactly `*/5 *<TAB>* * *<TAB>root<TAB>/usr/local/sbin/pistar-upnp.service start > /dev/null 2>&1 &`, verified. If no such line exists, nothing is added. The command then enables `pistar-upnp.timer` and restarts `pistar-upnp.service` (the systemd unit; `pistar-upgrade` l.528-541 writes `/lib/systemd/system/pistar-upnp.service` with `Type=oneshot`, `ExecStart=/usr/local/sbin/pistar-upnp.service start`, `ExecStopPost=... stop`) in the background.
  - `OFF`: comments out the cron line(s), stops `pistar-upnp.service` in the background (its `ExecStopPost` removes all mappings), then disables the timer.
  - Unclear: whether `pistar-upnp.timer` exists on a given image. `pistar-upgrade` l.545 tests for it with `systemctl | grep pistar-upnp.timer`. When the timer is missing, `systemctl enable/disable` fails and the failure is ignored (output is not captured).
  - Sudoers (`pistar-upgrade` l.66, 76, 172, 176; autoAP marker l.153, 158): `systemctl start|stop pistar-upnp.service`, `systemctl enable|disable pistar-upnp.timer`, `sed -i ?* /etc/crontab`, `sed -i ?* /usr/local/sbin/pistar-upnp.service`, `touch /etc/hostap.off`, `rm -rf /etc/hostap.off`.

### 1.3 Save semantics shared by all firewall fields

- The radios always have one checked value, so **every** Apply of the main form re-submits all five values. Every main-form save therefore re-runs the `sed` edits on `pistar-upnp.service` and `/etc/crontab` and touches or removes `/etc/hostap.off`. With uPNP=ON it also re-enables the timer and restarts `pistar-upnp.service`. A native version should either reproduce this or apply only on change. Applying only on change is behaviourally equivalent, except for the uPNP restart.
- These handlers run inside the global Apply envelope documented in part 01: `mount -o remount,rw /` (l.362) → stop cron and all DV services (l.540-563) → `sleep(1)` → field handlers → INI write-back → restart services → start cron (l.3938) → `sync ×3 && mount -o remount,ro /` (l.3944). The firewall handlers use only `sudo sed` and `systemctl`; they do not use the config_writer.
- If `count($configmmdvm) <= 18` (l.621), the whole POST aborts before any of these handlers run.
- Order inside the POST: `autoAP` (l.646) runs early. `dashAccess` → `ircRCAccess` → `sshAccess` → `uPNP` (l.2415-2452) run after the `confHardware` block and before `confTimeAnnounce`.
- `pistar-firewall` (iptables) is **not** called, read or edited by this section or by any code in `configure.php`. The name "Firewall Configuration" refers only to the UPnP port mappings in `pistar-upnp.service`. `pistar-firewall` is run by `pistar-update` (l.209) and opens the hostapd DHCP/DNS/NAT rules when `/etc/hostapd/hostapd.conf` exists (`pistar-firewall` l.143-150). It reads `/root/ipv4.fw` for custom rules.

---

## 2. Wireless Configuration and Auto AP PSK (l.5667-5694)

### 2.1 Visibility

```php
if ( file_exists('/sys/class/net/wlan0') || file_exists('/sys/class/net/wlan1') || file_exists('/sys/class/net/wlan0_ap') ) { ... }   // l.5668
```

The whole block (heading, iframe and PSK form) is printed by a single-quoted `echo`. If no wireless interface exists, nothing is printed. The PSK form is **not** additionally gated on hostapd being installed.

### 2.2 Layout

```
<br />
<h2>Wireless Configuration</h2>
<table><tr><td>
  <iframe frameborder="0" scrolling="auto" name="wifi" src="wifi.php?page=wlan0_info" width="100%"
          onload="javascript:resizeIframe(this);">If you can see this message, your browser does not support iFrames,
          however if you would like to see the content please click <a href="wifi.php?page=wlan0_info">here</a>.</iframe>
</td></tr></table>
<br />
<form id="autoApPassForm" action="/admin/configure.php" method="post">
  <input type="hidden" name="csrf_token" value="..." />          (csrf_field(), l.5682)
  <table>
    <tr><th width="200">Auto AP SSID</th><th colspan="3">PSK</th></tr>
    <tr>
      <td align="left"><b>{php_uname('n')}</b></td>
      <td align="left">
        <label for="psk1">PSK:</label><input type="password" name="autoapPsk" id="psk1" onkeyup="checkPsk(); return false;" size="20" />
        <label for="psk2">Confirm PSK:</label><input type="password" name="autoapPsk" id="psk2" onkeyup="checkPskMatch(); return false;" />
        <br /><span id="confirmMessage" class="confirmMessage"></span>
      </td>
      <td align="right"><input type="button" id="submitpsk" value="Set PSK" onclick="submitPskform()" disabled="disabled" /></td>
    </tr>
  </table>
</form>
```

- `resizeIframe(obj)` (l.297-301) sets the iframe height to `contentWindow.document.body.scrollHeight` px on load. It is same-origin only.
- Iframe content (`admin/wifi.php`, 543 lines, outside this part's scope): `?page=wlan0_info` shows the interface's IP/netmask, counters, SSID/BSSID, bitrate, signal, channel and regulatory domain from `ifconfig`/`iwconfig`/`iw`. It has "Refresh" and "Configure WiFi" buttons; `?page=wpa_conf` is an editor for `/etc/wpa_supplicant/wpa_supplicant.conf`. The native version needs its own Wi-Fi status and wpa_supplicant editor; wifi.php is PHP.
- The header row declares `colspan="3"` for "PSK", but the body row has only two cells after the SSID cell. This is upstream markup; keep the visual layout of SSID | inputs | right-aligned button.
- The SSID shown is `php_uname('n')`, the kernel hostname. It is printed unescaped and is **not** read from `/etc/hostapd/hostapd.conf`. The actual `ssid=` key is set to the lower-cased sanitised hostname whenever the hostname changes (main form `confHostame` handler, l.2674-2679, via `config_writer_stage_privileged_flat('/etc/hostapd/hostapd.conf','ssid',$newHostnameLower)`).
- `id="confirmMessage"` is duplicated: the password form has the same id (§3). No JS ever writes to it.

### 2.3 Fields

#### `autoapPsk` (two inputs, `#psk1` and `#psk2`)

- Type: `password`, both with the **same name** `autoapPsk`. PHP keeps the last one in document order, so **the server uses the value of `#psk2` (Confirm PSK)**. The server never compares the two values.
- `#psk1`: `size="20"`. No maxlength, pattern or placeholder. `onkeyup="checkPsk(); return false;"`.
- `#psk2`: no size, maxlength, pattern or placeholder. `onkeyup="checkPskMatch(); return false;"`.
- Labels: `PSK:` and `Confirm PSK:` (literal `<label for>`).
- Displayed value: always empty. The current passphrase is never shown.
- Client JS (`functions.js` l.21-40):
  ```js
  function checkPsk() {                       // uses implicit global psk1 (element id)
    if (psk1.value.length > 0 && psk1.value.length < 8) psk1.style.background='#ff6666';
    else psk1.style.background='#66cc66';
  }
  function checkPskMatch(){
    var psk1 = document.getElementById('psk1'), psk2 = document.getElementById('psk2');
    if ((psk1.value != '') && (psk1.value == psk2.value)) { psk2.style.backgroundColor = "#66cc66"; submitpsk.removeAttribute("disabled"); }
    else { psk2.style.backgroundColor = "#ff6666"; submitpsk.setAttribute("disabled","disabled"); }
  }
  ```
  - Length under 8 only colours `#psk1` red. It does **not** disable the button.
  - The button is enabled or disabled only from `#psk2` keyup. Editing `#psk1` after `#psk2` matches leaves the button enabled with mismatching values; the server then uses `#psk2`.
  - The client has no upper-length check and no check for 64-hex input.

#### `submitpsk` button

- `<input type="button" id="submitpsk" value="Set PSK" onclick="submitPskform()" disabled="disabled" />`. It has no `name`, so it is not posted.
- `submitPskform()` (l.284-288) calls `disablesubmitbuttons()` and submits `#autoApPassForm`.
- Posted fields: `csrf_token`, `autoapPsk` (twice).

### 2.4 Save behaviour (l.494-538)

Preconditions shared with every POST: `csrf_verify()` at l.65 (a bad or missing token gets 403 and exit), then `pistar_warnings_enforce_redirect()` (l.69), then `system('sudo mount -o remount,rw /')` (l.362). The password branch (§3.3) runs first and is skipped because `adminPassword` is absent.

```php
if (empty($_POST['autoapPsk']) != TRUE) {
  $newPsk = $_POST['autoapPsk'];
  if (preg_match('/\A([\x20-\x7e]{8,63}|[0-9A-Fa-f]{64})\z/', $newPsk)) {
    config_writer_stage_privileged_flat('/etc/hostapd/hostapd.conf', 'wpa_passphrase', $newPsk);
    config_writer_stage_privileged_flat('/etc/hostapd/hostapd.conf', 'wpa', '2');
    $cfgWriterErrors = config_writer_commit(false);
    foreach ($cfgWriterErrors as $msg) { error_log('Pi-Star configure.php: ' . $msg); }
  } else {
    error_log("Pi-Star configure.php: AutoAP PSK rejected (must be 8-63 printable ASCII or 64-hex)");
  }
  unset($_POST);
  /* "Working..." page, JS reload after 5000 ms, footer */  die();
}
```

- Validation: the value is not trimmed. It must be 8-63 printable ASCII (0x20-0x7E, spaces and shell metacharacters allowed) or exactly 64 hex digits. If it fails, the server only writes `error_log` and the user still sees the same "Working..." page. **There is no user-visible error.**
- Write path (`config_writer.php` l.282-305 and l.514-556, "pass 3"):
  1. Staging allowlist: `/etc/hostapd/hostapd.conf` (l.123-129). The key must match `/\A[A-Za-z_][A-Za-z0-9_]*\z/`. A value containing NUL, CR or LF is refused.
  2. Read: `sudo -n cat '/etc/hostapd/hostapd.conf' 2>/dev/null` (the file is mode 600 root:root). A non-zero exit skips the edits.
  3. For each key, replace the **first** line that starts at column 0 with `key=` by `key=value`: `wpa_passphrase=<psk>` and `wpa=2`. `wpa_passphrase=` lines do not match the `wpa=` prefix. A key that is absent is **not added**; it is only logged ("has no 'wpa=' line"). Commented lines (`#wpa=`) do not match.
  4. Write: `tempnam('/tmp','pistar_cw_')`, then `chmod 0600`, then `file_put_contents(implode("\n",$lines)."\n")`, then `sudo install -m 600 -o root -g root <tmp> /etc/hostapd/hostapd.conf`, then unlink the temp file. The file is fully rewritten and a single trailing newline is normalised.
  5. `config_writer_commit(false)` does **not** do its own remount, because the caller already remounted rw.
- Response: the "Working..." table ("Working..." / "Applying your configuration changes..."), `setTimeout(window.location=window.location, 5000)`, the footer, then `die()`.
- Quirk: this branch `die()`s **without** `mount -o remount,ro /`. The rootfs stays read-write after a PSK change, because the remount-ro at l.3944 is never reached. Services and cron are not stopped (that happens at l.540+, after this branch).
- No service is restarted: hostapd is not reloaded, so the new PSK applies at the next AP start or reboot.
- Edge case: `empty("0")` is true in PHP. A PSK of `"0"` is client-side "valid" (non-empty and matching), but it skips this branch and falls into the **full main-form apply** with almost no fields: every service is stopped, configs are rewritten from the in-memory values, and services are restarted. Other inputs that PHP treats as empty behave the same way. The native version must not copy this.
- Sudoers needed (`pistar-upgrade` l.180, l.84, l.79): `/bin/cat /etc/hostapd/hostapd.conf`, `/usr/bin/install -m 600 -o root -g root /tmp/* /etc/hostapd/hostapd.conf`, `/bin/mount -o remount,rw /`.

---

## 3. Remote Access Password (l.5696-5711)

### 3.1 Layout

```
<br />
<h2>Remote Access Password</h2>
<form id="adminPassForm" action="/admin/configure.php" method="post">
  <input type="hidden" name="csrf_token" value="..." />          (csrf_field(), l.5699)
  <table>
    <tr><th width="200">User Name</th><th colspan="3">Password</th></tr>
    <tr>
      <td align="left"><b>pi-star</b></td>
      <td align="left">
        <label for="pass1">Password:</label><input type="password" name="adminPassword" id="pass1" onkeyup="checkPass(); return false;" size="20" />
        <label for="pass2">Confirm Password:</label><input type="password" name="adminPassword" id="pass2" onkeyup="checkPass(); return false;" />
        <br /><span id="confirmMessage" class="confirmMessage"></span>
      </td>
      <td align="right"><input type="button" id="submitpwd" value="Set Password" onclick="submitPassform()" disabled="disabled" /></td>
    </tr>
    <tr><td colspan="3"><b>WARNING:</b> This changes the password for this admin page<br />AND the "pi-star" SSH account</td></tr>
  </table>
</form>
```

- Always shown when the page renders the form, with no conditions.
- The user name is the hard-coded literal `pi-star`. It is not a field and is not posted.
- The header's `colspan="3"` does not match the 2 remaining body cells (same upstream quirk as §2.2).

### 3.2 Fields

#### `adminPassword` (two inputs, `#pass1` and `#pass2`)

- Type: `password`, both with the **same name** `adminPassword`. The server uses the last one, `#pass2` (Confirm Password), and never compares the two.
- `#pass1`: `size="20"`. `#pass2`: no size. Neither has maxlength, pattern or placeholder. Both have `onkeyup="checkPass(); return false;"`.
- Labels: `Password:` and `Confirm Password:`.
- Displayed value: always empty.
- Client JS (`functions.js` l.8-20): if `pass1 != ''` and `pass1 == pass2`, `#pass2` turns green (`#66cc66`) and `#submitpwd` is enabled. Otherwise `#pass2` turns red (`#ff6666`) and the button is disabled. Both inputs re-evaluate, so a later edit to `#pass1` disables the button again. Unlike the PSK pair, this works correctly. The client has no length or complexity check.

#### `submitpwd` button

- `<input type="button" id="submitpwd" value="Set Password" onclick="submitPassform()" disabled="disabled" />`. It has no name.
- `submitPassform()` (l.279-283) calls `disablesubmitbuttons()` and submits `#adminPassForm`.

### 3.3 Save behaviour (l.395-492)

This is the **first** handler after `mount -o remount,rw /` (l.362), so it runs before services are stopped.

```php
if (empty($_POST['adminPassword']) != TRUE) {
  $rawPassword = stripslashes(trim($_POST['adminPassword']));
  $hasIllegalChar = preg_match('/[\x00\r\n]/', $rawPassword);
  $tooLong        = strlen($rawPassword) > 256;
  $isStillDefault = hash_equals('raspberry', $rawPassword);
  if ($rawPassword === '' || $hasIllegalChar || $tooLong || $isStillDefault) {
    error_log("... adminPassword rejected (empty, contains NUL/CR/LF, > 256 bytes, or matches the factory default)");
  } else {
    // 1. proc_open('sudo -n /usr/sbin/chpasswd'); stdin: "pi-star:" . $rawPassword . "\n"; close stdin; read stdout/stderr; exit code
    // 2. only if chpasswd exit == 0:
    //    proc_open('htpasswd -i /var/www/.htpasswd pi-star'); stdin: $rawPassword (no newline)
    //    on non-zero exit or spawn failure: error_log only
    //    then @chmod('/var/www/.htpasswd', 0600), logging on failure
  }
  unset($_POST);  /* "Working..." page, reload after 5000 ms, footer */  die();
}
```

Exact processing:

1. **Normalisation:** `trim()` removes leading and trailing space, `\t`, `\n`, `\r`, `\0` and `\x0B`. `stripslashes()` then removes backslashes: `\x` becomes `x`, `\\` becomes `\`, and a lone trailing `\` is dropped. A password typed with backslashes is therefore stored **without** them, and the user must later log in with the stripped form. Decide whether the native version mirrors this (for compatibility) or drops it. Mirroring is recommended only if migrated users rely on it; otherwise document the difference.
2. **Rejections (silent, error_log only, same "Working..." page):** empty after normalisation; contains NUL, CR or LF; longer than 256 bytes; or exactly `raspberry` (the factory default; compare `banner_warnings.inc` l.131-139 and `admin/change_password_required.php`).
3. **Linux account:** `sudo -n /usr/sbin/chpasswd` with stdin `pi-star:<password>\n`. It uses the system PAM stack and ENCRYPT_METHOD (SHA512 on Bookworm). A PAM rejection (pwquality, history and so on) gives a non-zero exit. That is logged and **`.htpasswd` is left unchanged**, which keeps the two passwords in sync. Sudoers: `/usr/sbin/chpasswd` (`pistar-upgrade` l.184).
4. **Web basic-auth:** runs as www-data, no sudo: `htpasswd -i /var/www/.htpasswd pi-star`, password on stdin without a newline. There is no `-c`, so the file must already exist. There is no `-B`, so the hash algorithm is htpasswd's default. Unclear: the comment at l.469 says "bcrypt hash", but the command has no `-B`. Apache 2.4's `htpasswd` defaults to APR1-MD5, so the actual algorithm depends on the installed apache2-utils version. Nginx reads the file for `auth_basic`. If htpasswd fails after chpasswd succeeded, the two passwords drift apart; this is logged only (l.459-463).
5. **Permissions:** `chmod 0600 /var/www/.htpasswd`, owner unchanged (www-data). Failure is logged.
6. **Response:** "Working..." page, reload after 5 s, `die()`. As with the PSK branch, **no remount ro** happens: the rootfs is left rw. No service restart happens. Existing browser sessions keep sending the old basic-auth credentials and get a 401 prompt on the next request after the reload.
7. **Edge case:** `empty("0")`: a password `"0"` passes the client check but falls through to the full main-form apply (see §2.4). The `trim()` happens after the `empty()` test, so `" "` (one space) enters the branch, becomes `''` and is rejected. That case is harmless.

Related: the default-password banner and redirect (`banner_warnings.inc`, `pistar_warnings_enforce_redirect()` l.277). Every `/admin/*` page except `change_password_required.php` (and `update.php` / `expert/upgrade.php` / `?ajax` requests) redirects to `/admin/change_password_required.php` while the basic-auth credentials are `pi-star` / `raspberry`. This is determined from `PHP_AUTH_USER`/`PHP_AUTH_PW`, or `REMOTE_USER` (l.110-115).

---

## 4. Parts that depend on the PHP web server and need a native replacement

| Classic mechanism | Dependency | Native replacement note |
|---|---|---|
| CSRF `csrf_token` hidden field + `csrf_verify()` (session cookie) | PHP sessions (`config/csrf.php`) | Native `panel` already uses its own CSRF cookie (`__Host-pistar-csrf`, `panel/wizard.go`) on the TLS management listener. |
| `/var/www/.htpasswd` + `htpasswd -i` | nginx `auth_basic`, apache2-utils, www-data owning the file | The native admin listener (`pistar-wizard`, `panel/wizard.go` l.74-119) authenticates user `pi-star` against a SHA-256 of the systemd credential `admin-password` (`LoadCredential=admin-password:/etc/pistar-wizard/admin-password`, `scripts/build-deb` l.55; generated as `openssl rand -hex 32` by `debian/postinst` l.15). The credential is read **once** when the handler is constructed, so a password change must rewrite `/etc/pistar-wizard/admin-password` (via a root helper, mode 600, no newline issues: the reader strips one trailing `\n`/`\r`, rejects embedded CR/LF/NUL and caps the size at 4096 bytes) **and** restart `pistar-wizard`. Whether `.htpasswd` should still be written depends on whether nginx and the old dashboard remain installed; per project AGENTS.md, `.htpasswd`-touching scripts are not installed. |
| `chpasswd` for the `pi-star` Linux account | sudo from www-data | Keep it: a root helper (e.g. `pistar-configure`) feeds `pi-star:<pw>\n` to `/usr/sbin/chpasswd` on stdin. Order: chpasswd first, then the dashboard credential, only on success. Note that `panel/helper/pistar-configure` l.89-91 currently **blocks** `passwd`/`chpasswd`/`htpasswd` and `/etc/shadow`/`.htpasswd` in user-edited cron content; that is unrelated to this action but must not be confused with it. |
| `wifi.php` iframe (`wlan0_info`, `wpa_conf`) | PHP page, `ifconfig`/`iwconfig`/`iw`, writes `/etc/wpa_supplicant/wpa_supplicant.conf` | Needs native Wi-Fi status and network-editor views. The native expert editor already offers `full-read/full-write wpaconfig` through `pistar-configure` (`debian/sudoers`). |
| hostapd PSK via `config_writer` (`sudo cat` + `sudo install -m 600`) | PHP helper + sudoers for www-data | A root helper reads `/etc/hostapd/hostapd.conf`, replaces the first column-0 `wpa_passphrase=` and `wpa=` lines (no insert when absent), writes it atomically with mode 600 root:root, and validates `^([\x20-\x7e]{8,63}|[0-9A-Fa-f]{64})$`. Native should show validation errors instead of failing silently. |
| `sudo sed` on `/usr/local/sbin/pistar-upnp.service` and `/etc/crontab`; `sudo touch/rm /etc/hostap.off`; `systemctl` for upnp | sudo grants to www-data | Move into the root helper with fixed actions (`dashAccess`/`ircRCAccess`/`sshAccess` = PUB/PRV; `uPNP` = ON/OFF; `autoAP` = ON/OFF), using the exact replacement lines given in §1.2. On Trixie, check whether `/etc/crontab` carries the `pistar-upnp.service` line and whether `pistar-upnp.timer` exists. Without the cron line, the classic page reports uPNP as "On". |
| Reads of `pistar-upnp.service`, `/etc/crontab`, `/etc/hostap.off`, `/etc/default/hostapd`, `/sys/class/net/wlan*` | none (world-readable) | Go can read them directly. Mirror the "first char of the last matching line == '#'" rule exactly, including the "missing means Public / On" default. |
| `php_uname('n')` for the SSID cell | PHP | `os.Hostname()`. Consider showing `ssid=` from hostapd.conf instead, which requires root to read. |
| Rootfs `remount,rw` / `remount,ro` | Pi-Star read-only root | The PSK and password branches leave the rootfs rw (upstream quirk). The native version should remount ro afterwards if the target image uses a read-only root. |

---

## 5. Field count

- Firewall: 5 controls: `dashAccess`, `ircRCAccess`, `sshAccess`, `autoAP` (conditional), `uPNP`. Plus the section Apply button.
- Wi-Fi: 1 iframe, 2 `autoapPsk` password inputs (one effective POST value), the `submitpsk` button and the `csrf_token` hidden field.
- Remote Access Password: 2 `adminPassword` password inputs (one effective POST value), the `submitpwd` button and the `csrf_token` hidden field.

---

## Services/commands touched by this section on Apply

- Main form (firewall rows), on every main Apply inside the global stop/restart envelope:
  - `sudo sed -i '/$ipVar 80 80/c\…'` or `'… s/^#*/#/'` on `/usr/local/sbin/pistar-upnp.service` (dashAccess)
  - `sudo sed -i '/$ipVar 10022 10022/…'` on the same file (ircRCAccess)
  - `sudo sed -i '/$ipVar 22 22/…'` on the same file (sshAccess)
  - `sudo touch /etc/hostap.off` / `sudo rm -rf /etc/hostap.off` (autoAP; takes effect on reboot through `pistar-ap.service`)
  - uPNP ON: `sudo sed -i '/pistar-upnp.service/c\*/5 *\t* * *\troot\t/usr/local/sbin/pistar-upnp.service start > /dev/null 2>&1 &' /etc/crontab`, `sudo systemctl enable pistar-upnp.timer`, `(sudo systemctl stop pistar-upnp.service && sudo systemctl start pistar-upnp.service) &`
  - uPNP OFF: `sudo sed -i '/pistar-upnp.service/ s/^#*/#/' /etc/crontab`, `(sudo systemctl stop pistar-upnp.service) &`, `sudo systemctl disable pistar-upnp.timer`
  - The global envelope also runs: `mount -o remount,rw /`, stop/start of cron and all DV services, `sync; mount -o remount,ro /` (see part 01).
- Set PSK: `sudo mount -o remount,rw /`, `sudo -n cat /etc/hostapd/hostapd.conf`, `sudo install -m 600 -o root -g root /tmp/pistar_cw_* /etc/hostapd/hostapd.conf`. hostapd is not restarted and there is no remount ro.
- Set Password: `sudo mount -o remount,rw /`, `sudo -n /usr/sbin/chpasswd` (stdin `pi-star:<pw>`), `htpasswd -i /var/www/.htpasswd pi-star` (stdin, as www-data), `chmod 0600 /var/www/.htpasswd`. No service restart and no remount ro.
- Not touched by this section: `pistar-firewall` (iptables), `hostapd`/`pistar-ap.service` (no restart), nginx.
