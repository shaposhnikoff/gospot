# configure.php spec — P25, NXDN, M17, POCSAG, Mobile GPS sections

Reference: `Pi-Star_DV_Dash/admin/configure.php` at revision `10bf34c9`
(form lines 5307-5579, POST handling in lines 351-3944).
Helpers: `Pi-Star_DV_Dash/functions.js` (served as `/functions.js`),
`Pi-Star_DV_Dash/config/config_writer.php`. Labels are resolved from
`Pi-Star_DV_Dash/lang/english_uk.php`.

All line numbers below refer to `admin/configure.php` unless another file is named.

---

## 0. Shared mechanics for all five sections

### 0.1 Config loading (page render and POST)

| Variable | File | Loaded at | Condition |
|---|---|---|---|
| `$configmmdvm` | `/etc/mmdvmhost` | 119-120 | always, `parse_ini_file(..., true)` |
| `$configysfgateway` | `/etc/ysfgateway` | 123-124 | always |
| `$configysf2nxdn` | `/etc/ysf2nxdn` | 133-136 | if file exists |
| `$configysf2p25` | `/etc/ysf2p25` | 139-142 | if file exists |
| `$configdmr2nxdn` | `/etc/dmr2nxdn` | 157-160 | if file exists |
| `$configp25gateway` | `/etc/p25gateway` | 163-166 | if file exists |
| `$confignxdngateway` | `/etc/nxdngateway` | 169-172 | if file exists |
| `$configm17gateway` | `/etc/m17gateway` | 175-178 | if file exists |
| `$configdapnetgw` | `/etc/dapnetgateway` | 187-190 | if file exists |

`/etc/mobilegps` is **not** parsed. The form reads it with shell `grep`/`awk`, and the save
path writes it with `config_writer_stage_flat()` (see 6.5).

`parse_ini_file` (default `INI_SCANNER_NORMAL`) strips quotes and turns
`on/yes/true` into `"1"` and `off/no/false/none` into `""`. The whole array is
rewritten on save (see 0.4), so this normalisation is persisted. This is not
specific to these sections, but the native writer has to decide whether to copy it.

### 0.2 Form and buttons

- The page has one form: `<form id="config" method="post" action="/admin/configure.php">` (4006),
  followed by `csrf_field()` (4007), which adds a hidden CSRF token input. All sections post together.
- Each section ends with its own button (5358, 5415, 5494, 5535, 5578):
  ```html
  <div><input type="button" value="Apply Changes" onclick="submitform()" /><br /><br /></div>
  ```
  `submitform()` (274-278) calls `disablesubmitbuttons()`, which disables every
  `input[type=button]` on the page, and then `document.getElementById("config").submit()`.
  Any section's button submits the whole page.
- `<body onload="checkFrequency(); return false;">` (305): `checkFrequency()` runs once on load.
  This colours the POCSAG frequency box (see 5.3).

### 0.3 Table layout (identical for all five sections)

```html
<h2>{section title}</h2>
<table>
<tr>
  <th width="200"><a class="tooltip" href="#">Setting<span><b>Setting</b></span></a></th>
  <th colspan="2"><a class="tooltip" href="#">Value<span><b>Value</b>The current value from the<br />configuration files</span></a></th>
</tr>
<tr>
  <td align="left"><a class="tooltip2" href="#">{Label}:<span><b>{Tooltip title}</b>{Tooltip body}</span></a></td>
  <td align="left" | style="text-align: left;">{control}</td>
</tr>
...
</table>
<div><input type="button" value="Apply Changes" onclick="submitform()" /><br /><br /></div>
```

- The header has 3 logical columns (200px + colspan 2). Every data row in these sections has
  only **2 cells**: the label cell and the value cell. No value cell has a colspan.
  The third column is always empty.
- The label text always ends with `:`. The tooltip is a `<span>` inside the anchor, with a
  bold title followed by body text and no separator. The "Tooltip" entries below give the
  title and body as `Title — Body`.
- None of the value attributes use HTML escaping, except the Mobile GPS fields, which use
  `htmlspecialchars`. The PHP echoes raw INI values.

### 0.4 Save pipeline (applies to every field below)

1. `if (!empty($_POST))` (351). `sudo mount -o remount,rw /` (359).
2. `sudo systemctl stop cron.service` (551), then every DV service is stopped (554-575).
   This includes `p25gateway`, `p25parrot`, `nxdngateway`, `nxdnparrot`, `m17gateway`,
   `ysf2nxdn`, `ysf2p25`, `dmr2nxdn`, `dapnetgateway`, `mmdvmhost` and `ysfgateway`.
   Then `sleep(1)` (583).
3. Field handlers modify the in-memory arrays (lines given per field).
4. Version-dependent migrations (3084-3127), DAPNET defaults (3177-3202), the M17 daemon fix (3205)
   and Mobile GPS (3207-3255) run.
5. Every INI array is serialised as `[Section]\nkey=value\n...\n\n`, with `_` in section names
   replaced by a space. The result goes to a fixed `/tmp/*.tmp` file and is installed with
   `sudo install -m 644 -o root -g root <tmp> <dest>` only if the temp file has more than N lines:

   | Dest | Tmp file | Line threshold | Extra guard | Lines |
   |---|---|---|---|---|
   | `/etc/mmdvmhost` | `/tmp/bW1kdm1ob3N0DQo.tmp` | > 140 | — | 3274-3312 |
   | `/etc/ysfgateway` | `/tmp/eXNmZ2F0ZXdheQ.tmp` | > 35 | — | 3314-3350 |
   | `/etc/nxdngateway` | `/tmp/kXKwkDKy793HF5.tmp` | > 30 | `file_exists('/etc/nxdngateway')` | 3352-3388 |
   | `/etc/p25gateway` | `/tmp/sJSySkheSgrelJX.tmp` | > 30 | `file_exists('/etc/p25gateway')` | 3390-3426 |
   | `/etc/m17gateway` | `/tmp/spNcSdRUEmySTo9.tmp` | > 30 | `file_exists('/etc/m17gateway')` | 3428-3464 |
   | `/etc/ysf2nxdn` | `/tmp/dsWGR34tHRrSFFGb.tmp` | > 35 | — | 3504-3538 |
   | `/etc/ysf2p25` | `/tmp/dsWGR34tHRrSFFGc.tmp` | > 25 | — | 3540-3574 |
   | `/etc/dmr2nxdn` | `/tmp/nthfheS55HGc.tmp` | > 25 | — | 3650-3684 |
   | `/etc/dapnetgateway` | `/tmp/lsHWie734HS.tmp` | > 19 | **none** (see 5.7) | 3686-3720 |

   If a temp file is not writable, the page prints an "Unable to write configuration file(s)" error
   table, reloads after 5 s and `die()`s.
6. `config_writer_commit(false)` (3832) flushes the staged flat `key=value` edits.
   This covers `/etc/mobilegps`.
7. Services start again (3838-3860): `sudo systemctl daemon-reload`, then `start` for the same set.
8. `sudo systemctl start cron.service` (3938). The page reloads after 7.5 s (3941).
   `sudo sync && sudo sync && sudo sync && sudo mount -o remount,ro /` (3944).

### 0.5 `escapeshellcmd()` side effect on stored values

Several handlers pass values through `escapeshellcmd()` and store the result in an INI file.
That function puts a backslash before each of ``& # ; ` | * ? ~ < > ^ ( ) [ ] { } $ \``,
`\x0A` and `\xFF`, and before `'` / `"` when they are unpaired. The backslashes are
**persisted into the config file**. For example, a DAPNET AuthKey `ab$c` is saved as `ab\$c`.
A native implementation must either copy this bug or (more sensibly) apply a whitelist.
This spec marks each field that is affected.

---

## 1. P25 Configuration (5307-5359)

### 1.1 Section visibility (5307)

```php
if (file_exists('/etc/dstar-radio.mmdvmhost') && $configmmdvm['P25 Network']['Enable'] == 1) {
$p25Hosts = fopen("/usr/local/etc/P25Hosts.txt", "r");
```
The section is shown only if the controller is MMDVMHost (marker file `/etc/dstar-radio.mmdvmhost`)
**and** `/etc/mmdvmhost [P25 Network] Enable == 1`. That value comes from the
`MMDVMModeP25` toggle in the MMDVMHost section (2480-2481); RF `[P25] Enable` is not checked.
`P25Hosts.txt` is opened without checking that it exists.

Title: `$lang['p25_config']` = **"P25 Configuration"** (5310).

### 1.2 Field `p25StartupHost` (5316-5350)

- Type: `<select name="p25StartupHost">`. Value cell `style="text-align: left;"`. No size or onchange.
- Label: `$lang['p25_startup_host']` = **"P25 Startup Host:"**
- Tooltip: **P25 Host** — "Set your prefered P25 Host here"
- Current value `$testP25Host` (5320-5322):
  1. `/etc/p25gateway [Network] Startup`, if it is set; otherwise
  2. `/etc/p25gateway [Network] Static`, if it is set; otherwise
  3. the literal `"none"`.
- Options, in order:
  1. `<option value="none">None</option>`. It is `selected` **only if `$testP25Host == ""`** (5323).
     When neither key is set, `$testP25Host` is `"none"`, so nothing gets `selected` in PHP.
     The browser still shows "None" because it is the first option.
  2. `<option value="10">10 - Parrot</option>`. Selected if `$testP25Host == "10"` (5325).
  3. Every line of `/usr/local/etc/P25Hosts.txt` (5327-5335), split with `preg_split('/\s+/')`.
     The line is skipped if column 0 contains `#` anywhere or is empty.
     Output: `<option value="{col0}">{col0} - {col1}</option>`, `selected` when `$testP25Host == col0`
     (loose PHP comparison). The columns are TG number and host name, so the label looks like
     `10200 - hostname`. Column 2 (port) is ignored. The file order is kept and no sorting is done.
  4. If `/usr/local/etc/P25HostsLocal.txt` exists, its lines are added with the same rules (5336-5347).
  - Duplicates are not removed. For example, TG 10 appears twice if the hosts file also lists it.

#### Save (1268-1279)

```php
if (empty($_POST['p25StartupHost']) != TRUE ) {
      $newP25StartupHost = strtoupper(escapeshellcmd($_POST['p25StartupHost']));
      if ($newP25StartupHost === "NONE") {
      unset($configp25gateway['Network']['Startup']);
      unset($configysf2p25['P25 Network']['StartupDstId']);
      unset($configp25gateway['Network']['Static']);
  } else {
      $configp25gateway['Network']['Startup'] = $newP25StartupHost;
      $configysf2p25['P25 Network']['StartupDstId'] = $newP25StartupHost;
  }
}
```
- Sanitisation: `escapeshellcmd` then `strtoupper`. There is no whitelist against the hosts file.
- `none` → removes `/etc/p25gateway [Network] Startup` and `[Network] Static`, and
  `/etc/ysf2p25 [P25 Network] StartupDstId`.
- Otherwise → `/etc/p25gateway [Network] Startup={value}` and `/etc/ysf2p25 [P25 Network] StartupDstId={value}`.
- **Later override in the same request:** if `ysf2p25StartupDstId` (a YSF2P25 field in another
  section) is also posted, its handler at 1429-1441 runs afterwards and overwrites the same
  three keys.
- **Version migration (3084-3100):** `P25Gateway -v | awk {'print $3'} | cut -c 1-8` is run.
  If the result is `> 20210201`, any `[Network] Startup` is moved to `[Network] Static`
  (`Static = Startup; unset(Startup)`). On current P25Gateway the key **actually written is `Static`**.
  The same block also adds these defaults when missing:
  - `> 20200502`: `[Remote Commands] Enable=1`, `Port=6074`
  - `> 20210201`: remove `[General] Announcements`; add `[Log] DisplayLevel=1`, `FileLevel=1`,
    `[Network] P252DMRAddress=127.0.0.1`, `P252DMRPort=42012`
- Files written: `/etc/p25gateway` (if it exists and has more than 30 lines),
  `/etc/ysf2p25` (more than 25 lines).

### 1.3 Field `p25nac` (5351-5356)

- Row shown only if `$configmmdvm['P25']['NAC']` is truthy. PHP treats `""` and `"0"` as falsy,
  so NAC `000` is shown and a missing key or `0` hides the row.
- Type: `<input type="text" name="p25nac" size="13" maxlength="3" value="{NAC}" />`
- Label: `$lang['p25_nac']` = **"P25 NAC:"**
- Tooltip: **P25 NAC** — "Set your NAC code here"
- Value: `/etc/mmdvmhost [P25] NAC`, raw.

#### Save (1281-1287)

```php
if (empty($_POST['p25nac']) != TRUE ) {
  $p25nacNew = strtolower(escapeshellcmd($_POST['p25nac']));
  if (preg_match('/[a-f0-9]{3}/', $p25nacNew)) {
    $configmmdvm['P25']['NAC'] = $p25nacNew;
  }
}
```
- Writes `/etc/mmdvmhost [P25] NAC`, lower-cased.
- The regex is **not anchored**, so it only requires 3 consecutive hex characters somewhere in
  the string. The client `maxlength=3` is the only practical limit. A native version should use
  `^[0-9a-f]{3}$`, which accepts the same values the form can actually send.
- An empty post or `"0"` leaves the value unchanged. An invalid value is ignored silently.

### 1.4 Section button

`Apply Changes` (5358).

---

## 2. NXDN Configuration (5361-5416)

### 2.1 Section visibility (5361)

```php
if (file_exists('/etc/dstar-radio.mmdvmhost') && ($configmmdvm['NXDN Network']['Enable'] == 1 || $configdmr2nxdn['Enabled']['Enabled'] == 1) ) {
```
Requires MMDVMHost **and** one of these:
- `/etc/mmdvmhost [NXDN Network] Enable == 1` (set by the `MMDVMModeNXDN` toggle, 2486-2487), or
- `/etc/dmr2nxdn [Enabled] Enabled == 1` (set by the `MMDVMModeDMR2NXDN` toggle, 2541-2575).

Title: `$lang['nxdn_config']` = **"NXDN Configuration"** (5362).

### 2.2 Field `nxdnStartupHost` (5368-5407)

- Type: `<select name="nxdnStartupHost">`. Value cell `style="text-align: left;"`.
- Label: `$lang['nxdn_startup_host']` = **"NXDN Startup Host:"**
- Tooltip: **NXDN Host** — "Set your prefered NXDN Host here"
- **Branch A — `/etc/nxdngateway` exists** (5372-5401):
  - Current value: `/etc/nxdngateway [Network] Startup`, otherwise `[Network] Static`, otherwise `""` (5374-5376).
    Unlike P25, the fallback here is `""`, so "None" really is `selected` when nothing is set.
  - Options:
    1. `<option value="none">None</option>`, selected if the current value is `""`.
    2. `<option value="10">10 - Parrot</option>`, selected if it is `"10"`.
    3. `/usr/local/etc/NXDNHosts.txt`. Opened without an existence check. Same parsing as P25:
       `preg_split('/\s+/')`, skip lines whose col0 contains `#` or is empty,
       `<option value="{col0}">{col0} - {col1}</option>`, selected on loose equality.
    4. `/usr/local/etc/NXDNHostsLocal.txt`, if it exists, with the same rules.
- **Branch B — no `/etc/nxdngateway`** (5402-5404): a single fixed option.
  ```html
  <option value="176.9.1.168">D2FET Test Host - 176.9.1.168</option>
  ```
  It has no `selected` attribute. It is the only option, so it is effectively selected.

#### Save (1289-1304)

```php
if (empty($_POST['nxdnStartupHost']) != TRUE ) {
  $newNXDNStartupHost = strtoupper(escapeshellcmd($_POST['nxdnStartupHost']));
  if (file_exists('/etc/nxdngateway')) {
    if ($newNXDNStartupHost === "NONE") {
        if (isset($confignxdngateway['Network']['Startup'])) { unset($confignxdngateway['Network']['Startup']); }
        if (isset($confignxdngateway['Network']['Static']))  { unset($confignxdngateway['Network']['Static']); }
    } else {
        $confignxdngateway['Network']['Startup'] = $newNXDNStartupHost;
      }
  } else {
    $configmmdvm['NXDN Network']['GatewayAddress'] = $newNXDNStartupHost;
    $configmmdvm['NXDN Network']['GatewayPort'] = "41007";
  }
  $configysf2nxdn['NXDN Network']['StartupDstId'] = $newNXDNStartupHost;
}
```
- Sanitisation: `escapeshellcmd` then `strtoupper`.
- **When `/etc/nxdngateway` exists:** `none` removes `[Network] Startup` and `Static`.
  Any other value sets `/etc/nxdngateway [Network] Startup={value}`.
- **When it does not exist:** sets `/etc/mmdvmhost [NXDN Network] GatewayAddress={value}` and `GatewayPort=41007`.
- **In both cases**, and even for `NONE`, it sets `/etc/ysf2nxdn [NXDN Network] StartupDstId={value}`.
  For None, that means the literal string `NONE` is written.
- **Later override:** if `ysf2nxdnStartupDstId` is also posted (1411-1422), that handler overwrites
  `/etc/ysf2nxdn [NXDN Network] StartupDstId` and `/etc/nxdngateway [Network] Startup/Static`.
  It compares against lower-case `"none"` and does not upper-case the value.
- **Gateway clean-up (3039-3075, `/etc/nxdngateway` exists):** this always forces
  `/etc/mmdvmhost [NXDN Network] LocalAddress=127.0.0.1`, `LocalPort=14021`, `GatewayAddress=127.0.0.1`,
  `GatewayPort=14020`. It renames `[Network] HostsFile` to `HostsFile1` and adds
  `HostsFile2=/usr/local/etc/NXDNHostsLocal.txt`. If `NXDNHostsLocal.txt` is missing, it runs
  `sudo touch /usr/local/etc/NXDNHostsLocal.txt`. It also re-orders `[NXDN] SelfOnly`/`ModeHang`
  and adds `[Info]`/`[aprs.fi]` defaults (3067-3074).
- **Version migration (3102-3127):** `NXDNGateway -v | awk {'print $3'} | cut -c 1-8` is run.
  - `> 20200502`: add `[Remote Commands] Enable=1`, `Port=6075` when missing.
  - `> 20210131`: remove the `[aprs.fi]` and `[Mobile GPS]` sections; add `[General] RptProtocol=Icom`,
    `[Log] DisplayLevel=1`, `FileLevel=1`, `[APRS] Enable=1`, `Address=127.0.0.1`, `Port=8673`, `Suffix=N`;
    set `[APRS] Description` to `{ysfgateway [Info] Name}_{ysfgateway [General] Suffix}`
    (this is always overwritten); add `[GPSD] Enable=0`, `Address=127.0.0.1`, `Port=2947`;
    **move `[Network] Startup` to `[Network] Static`**. On current NXDNGateway the key actually written is `Static`.
- Files written: `/etc/nxdngateway` (if it exists, more than 30 lines), `/etc/mmdvmhost`, and
  `/etc/ysf2nxdn` (more than 35 lines).

### 2.3 Field `nxdnran` (5408-5413)

- Row shown only if `$configmmdvm['NXDN']['RAN']` is truthy. RAN `0` or a missing key hides it.
- Type: `<input type="text" name="nxdnran" size="13" maxlength="2" value="{RAN}" />`
- Label: `$lang['nxdn_ran']` = **"NXDN RAN:"**
- Tooltip: **NXDN RAN** — "Set your RAN code here, sane values are 1-64"
- Value: `/etc/mmdvmhost [NXDN] RAN`.

#### Save (1306-1313)

```php
if (empty($_POST['nxdnran']) != TRUE ) {
  $nxdnranNew = strtolower(escapeshellcmd($_POST['nxdnran']));
  $nxdnranNew = preg_replace('/[^0-9]/', '', $nxdnranNew);
  if (($nxdnranNew >= 1) && ($nxdnranNew <= 64)) {
      $configmmdvm['NXDN']['RAN'] = $nxdnranNew;
  }
}
```
- Non-digit characters are stripped. The value is saved only if it is between 1 and 64
  (numeric comparison of the digit string). The digit string is stored as entered,
  so `07` stays `07`.
- `"0"` and empty posts are ignored because of `empty()`. Out-of-range values are ignored silently.
- Writes `/etc/mmdvmhost [NXDN] RAN`.

### 2.4 Section button

`Apply Changes` (5415).

---

## 3. M17 Configuration (5418-5495)

### 3.1 Section visibility (5418)

```php
if (file_exists('/etc/dstar-radio.mmdvmhost') && ($configmmdvm['M17 Network']['Enable'] == 1) ) {
```
Requires MMDVMHost **and** `/etc/mmdvmhost [M17 Network] Enable == 1`
(set by the `MMDVMModeM17` toggle, 2492-2493).

Title: `$lang['m17_config']` = **"M17 Configuration"** (5419).

### 3.2 Row "M17 Startup Host" (5425-5488): two selects in one cell

- Label: `$lang['m17_startup_host']` = **"M17 Startup Host:"**
- Tooltip: **M17 Host** — "Set your prefered M17 Host here"
- Value cell `style="text-align: left;"` holds two selects side by side, with no separator except
  the whitespace and newline in the source.

#### 3.2.1 `m17StartupHost` (5427-5458)

- Type: `<select name="m17StartupHost">`.
- It gets options only if `/etc/m17gateway` exists (5429). **Otherwise the select is empty**,
  and an empty select is not submitted.
- Current value: `explode("_", /etc/m17gateway [Network] Startup)[0]`, which is the reflector
  name before the first `_`. If Startup is not set, the value is `""` (5431-5432).
- Options:
  1. `<option value="none">None</option>`, selected if the current value is `""`. There is **no Parrot entry**.
  2. `/usr/local/etc/M17Hosts.txt` (opened without an existence check). Split with `preg_split('/\s+/')`.
     Lines whose col0 contains `#` or is empty are skipped. Output: `<option value="{col0}">{col0}</option>`,
     so the label is the reflector name only (for example `M17-M17`). Selected when it equals the current value.
  3. `/root/M17Hosts.txt`, if it exists, with the same rules (5444-5455). Note the unusual path:
     this is the "local" M17 hosts file.

#### 3.2.2 `m17StartupRoom` (5459-5487)

- Type: `<select name="m17StartupRoom">`. The options have **no `value` attribute**, so the submitted
  value is the option text.
- If `/etc/m17gateway [Network] Startup` is set, the first option is
  `<option value="{last char of Startup}" selected="selected">{last char}</option>` (5460).
  This is `substr(Startup, -1)`. It is not parsed after `_`; it is just the last character.
- Then the static options `A`, `B`, `C` … `Z` (26 options, 5461-5486). The current room appears twice.
- If Startup is not set, nothing is preselected and the browser selects `A`.

#### Save (1315-1325)

```php
if (empty($_POST['m17StartupHost']) != TRUE || empty($_POST['m17StartupRoom']) != TRUE) {
  $newM17StartupHost = strtoupper(escapeshellcmd($_POST['m17StartupHost']))."_".strtoupper(escapeshellcmd($_POST['m17StartupRoom']));
  if (file_exists('/etc/m17gateway')) {
    if (strtoupper(escapeshellcmd($_POST['m17StartupHost'])) === "NONE") {
        if (isset($configm17gateway['Network']['Startup'])) { unset($configm17gateway['Network']['Startup']); }
    } else {
        $configm17gateway['Network']['Startup'] = $newM17StartupHost;
      }
  }
}
```
- This block always runs when the section is rendered, because the room select always posts.
- Value format: `{HOST upper}_{ROOM upper}`, for example `M17-M17_C`. Both parts go through `escapeshellcmd`.
- `none` → removes `/etc/m17gateway [Network] Startup`. The room is ignored.
- It writes only when `/etc/m17gateway` exists. No ysf2* or mmdvmhost side effects.
- If `/etc/m17gateway` is missing, `$_POST['m17StartupHost']` is undefined (empty select), which raises a
  PHP notice. Nothing is written.
- Unconditional side effect at 3204-3205: if `[General] Daemon` is set in `/etc/m17gateway`,
  it is forced to `1`.
- File: `/etc/m17gateway` (if it exists, more than 30 lines).

### 3.3 Field `m17can` (5489-5492)

- Always shown in the section. There is **no** truthiness guard, unlike NAC and RAN.
- Type: `<input type="text" name="m17can" size="13" maxlength="2" value="{CAN}" />`
- Label: `$lang['m17_can']` = **"M17 CAN:"**
- Tooltip: **M17 CAN** — "Set your CAN code here, sane values are 0-64"
- Value: `/etc/mmdvmhost [M17] CAN`. If the key is missing, the value is empty (PHP notice).

#### Save (1327-1332)

```php
if (isset($_POST['m17can']) && $_POST['m17can'] !== '') {
  $m17canNew = strtolower(escapeshellcmd($_POST['m17can']));
  $m17canNew = preg_replace('/[^0-9]/', '', $m17canNew);
  $configmmdvm['M17']['CAN'] = $m17canNew;
}
```
- `0` is accepted, because the check is `isset` plus not-empty-string rather than `empty()`.
- Non-digit characters are stripped. **There is no range check**, even though the tooltip says 0-64.
  If the input has no digits (for example `ab`), the stripped result is empty and `CAN=` is written.
- Writes `/etc/mmdvmhost [M17] CAN`.

### 3.4 Section button

`Apply Changes` (5494).

---

## 4. Cross-section effects on these gateway files (other sections' fields)

These keys are written by fields that live in other sections. They are listed so the native
writer does not clobber them:
- Callsign (1187-1191): `/etc/p25gateway [General] Callsign`, `/etc/nxdngateway [General] Callsign`,
  `[aprs.fi] Description`, `[aprs.fi] Password`, and `/etc/m17gateway [General] Callsign` if set.
- Frequencies (965-970 duplex, 1071-1076 simplex): `/etc/nxdngateway [Info] RXFrequency/TXFrequency`,
  `[General] Suffix` (`RPT` / `ND`); `/etc/m17gateway [Info] RX/TXFrequency`, `[General] Suffix` (`R` / `H`) if set.
- Latitude, longitude and description (746-789): `/etc/nxdngateway [Info] ...`; `/etc/m17gateway [Info] ...` if set.
- Hang timers (1754-1779): `/etc/mmdvmhost [P25]/[P25 Network]/[NXDN]/[NXDN Network]/[M17]/[M17 Network] ModeHang`;
  `/etc/p25gateway` and `/etc/nxdngateway [Network] RFHangTime=0`/`NetHangTime=0`;
  `/etc/m17gateway [Network] HangTime`.

---

## 5. POCSAG Configuration (5497-5536)

### 5.1 Section visibility (5497)

```php
if ( $configmmdvm['POCSAG']['Enable'] == 1 ) {
```
The only condition is `/etc/mmdvmhost [POCSAG] Enable == 1`. There is **no**
`/etc/dstar-radio.mmdvmhost` check and **no** `/etc/dapnetgateway` existence check.
The `MMDVMModePOCSAG` toggle that sets it (2588-2589) is shown only if `/etc/dapnetgateway` exists (4209).

Title: `$lang['pocsag_config']` = **"POCSAG Configuration"** (5498).

Labels in this section are hard-coded English, partly combined with `$lang`.

### 5.2 Field `pocsagServer` (5504-5513)

- Type: `<select name="pocsagServer">`. Value cell `style="text-align: left;"`.
- Label: **"POCSAG Server:"** (hard-coded)
- Tooltip: **POCSAG Server** — "Set the POCSAG Network here"
- Options, verbatim:
  1. `<option value="{dapnetgateway [DAPNET] Address}" selected="selected">{same}</option>`. This is the current value and is always selected.
  2. `<option value="dapnet.afu.rwth-aachen.de">dapnet.afu.rwth-aachen.de</option>`
  3. `<option value="db0dbn.ig-funk-siebengebirge.de">db0dbn.ig-funk-siebengebirge.de</option>`
  4. `<option value="dapnet.db0sda.ampr.org">dapnet.db0sda.ampr.org (HAMNET)</option>`
  5. `<option value="node1.dapnet-italia.it">node1.dapnet-italia.it</option>`
  - The current value usually also appears again as one of 2-5. Duplicates are not removed.
- Save (917-920): if not empty, `/etc/dapnetgateway [DAPNET] Address = escapeshellcmd(value)`.
  There is no whitelist. If missing, the default at 3188 is `dapnet.afu.rwth-aachen.de`.

### 5.3 Field `pocsagCallsign` (5514-5517)

- Type: `<input type="text" name="pocsagCallsign" size="13" maxlength="12" value="{...}" />`
- Label: `"POCSAG " . $lang['node_call']` = **"POCSAG Node Callsign:"**
- Tooltip: **POCSAG Callsign** — "Set your paging callsign here"
- Value: `/etc/dapnetgateway [General] Callsign`.
- Save (900-903): if not empty, `[General] Callsign = strtoupper(escapeshellcmd(value))`.
  If missing, the default at 3178 is `M1ABC`.

### 5.4 Field `pocsagFrequency` (5518-5521)

- Type: `<input type="text" id="pocsagFrequency" onkeyup="checkFrequency(); return false;" name="pocsagFrequency" size="13" maxlength="12" value="{MHz}" />`
- Label: `"POCSAG " . $lang['radio_freq']` = **"POCSAG Radio Frequency:"**. Unlike the main frequency row, it has no "MHz" suffix.
- Tooltip: **POCSAG Frequency** — "Set your paging frequency here"
- Value: `/etc/mmdvmhost [POCSAG] Frequency` (Hz), shown as MHz:
  `number_format(round(Frequency/1000000, 6), 6, '.', '')`. Example: `439987500` → `439.987500`.
- Client-side colouring (`/functions.js` 41-104, POCSAG part 92-103). On load and on each keyup,
  the background is first set to `#ff6666` (bad). Then `parseFloat(value)` is checked:
  - Good (`#66cc66`) if it is in [144,148], [220,225], [420,450] or [842,950].
  - These overrides set it back to bad: [145.8,146] and [435,438].
  - Unlike TRX/RX/TX, the POCSAG check has **no** 1240-1300 good range and no 1260-1270 bad range.
  - This is advisory only. It does not block submit.
- Save (887-893):
  ```php
  if (empty($_POST['pocsagFrequency']) != TRUE ) {
    $newPocsagFREQ = preg_replace('/[^0-9\.]/', '', $_POST['pocsagFrequency']);
    $newPocsagFREQ = number_format($newPocsagFREQ, 6, ".", "");
    $newPocsagFREQ = str_pad(str_replace(".", "", $newPocsagFREQ), 9, "0");
    $configmmdvm['POCSAG']['Frequency'] = $newPocsagFREQ;
  }
  ```
  - MHz → Hz by text manipulation: keep only digits and dots, format to 6 decimals, remove the dot,
    then right-pad with `0` to at least 9 characters.
  - The result is only correct for frequencies of 100 MHz or more. For example, `50.5` becomes
    `50.500000`, then `50500000`, then is padded to `505000000` (a wrong value).
    Values of 1000 MHz or more give 10 digits, which is correct.
  - For a malformed string such as `.` or `1.2.3`, the behaviour of `number_format()` depends on the
    PHP version (warning or TypeError). This is unclear (889-890).
  - Writes `/etc/mmdvmhost [POCSAG] Frequency`. If missing, the default at 3194 is `439987500`.
  - The handler sits at top level of the POST block, so it runs whenever the field is posted.

### 5.5 Field `pocsagAuthKey` (5522-5525)

- Type: `<input type="password" name="pocsagAuthKey" size="30" maxlength="50" value="{AuthKey}" />`.
  The current secret is put into the page source in clear text as the value.
- Label: **"DAPNET AuthKey:"** (hard-coded)
- Tooltip: **DAPNET AuthKey** — "Set your DAPNET AuthKey here"
- Value: `/etc/dapnetgateway [DAPNET] AuthKey`.
- Save (895-898): if not empty, `[DAPNET] AuthKey = escapeshellcmd(value)`. See 0.5: shell
  metacharacters are stored with backslashes. If missing, the default at 3190 is `TOPSECRET`.

### 5.6 Fields `pocsagWhitelist` / `pocsagBlacklist` (5526-5533)

| | Whitelist | Blacklist |
|---|---|---|
| Markup | `<input type="text" name="pocsagWhitelist" size="60" maxlength="350" value="..." />` | `<input type="text" name="pocsagBlacklist" size="60" maxlength="350" value="..." />` |
| Label | **"POCSAG Whitelist:"** | **"POCSAG Blacklist:"** |
| Tooltip | **POCSAG Whitelist** — "Set your POCSAG RIC Whitelist here, if these are set ONLY these RICs will be transmitted. List is comma seperated." | **POCSAG Blacklist** — "Set your POCSAG RIC Blacklist here, if these are set any other RIC will be transmitted, but not these. List is comma seperated." |
| Value | `/etc/dapnetgateway [General] WhiteList` if set, else empty | `/etc/dapnetgateway [General] BlackList` if set, else empty |

Save (905-915):
```php
if (isset($configdapnetgw['General']['WhiteList'])) { unset($configdapnetgw['General']['WhiteList']); }
if (empty($_POST['pocsagWhitelist']) != TRUE ) {
  $configdapnetgw['General']['WhiteList'] = preg_replace('/[^0-9\,]/', '', escapeshellcmd($_POST['pocsagWhitelist']));
}
// same for BlackList with pocsagBlacklist
```
- The key is **always removed first, unconditionally**, at top level of the POST block. It is written
  back only if the field is non-empty. The value keeps only digits and commas; spaces are removed.
- Because the unset is unconditional, **every Apply from any section while the POCSAG section is hidden
  (POCSAG disabled) deletes WhiteList and BlackList** from `/etc/dapnetgateway`. A native
  implementation should keep this or fix it on purpose; whichever it chooses, document it.
- The removed or re-added key moves to the end of the `[General]` section, which changes key order.

### 5.7 DAPNET and POCSAG defaults applied on every save (3177-3202)

These run whether or not the section was shown:
- `/etc/dapnetgateway`, when the key is missing:
  - `[General]`: `Callsign=M1ABC`, `RptAddress=127.0.0.1`, `RptPort=3800`, `LocalAddress=127.0.0.1`, `LocalPort=4800`, `Daemon=0`
  - `[Log]`: `DisplayLevel=0`, `FileLevel=2`, `FilePath=/var/log/pi-star`, `FileRoot=DAPNETGateway`
  - `[DAPNET]`: `Address=dapnet.afu.rwth-aachen.de`, `Port=43434`, `AuthKey=TOPSECRET`, `SuppressTimeWhenBusy=1`, `Debug=0`
- `/etc/mmdvmhost`, when the key is missing:
  - `[POCSAG]`: `Enable=0`, `Frequency=439987500`
  - `[POCSAG Network]`: `Enable=0`, `LocalAddress=127.0.0.1`, `LocalPort=3800`, `GatewayAddress=127.0.0.1`, `GatewayPort=4800`, `Debug=0`
- `[POCSAG Network] ModeHang` is **always forced to `5`** (3200 and 3202).
- The `/etc/dapnetgateway` install (3715-3717) has **no `file_exists` guard**. The defaults produce
  21 lines, which is more than the threshold of 19, so the file is **created even on systems where
  it did not exist**. It is unclear whether this is intended (3686-3720).

### 5.8 Section button

`Apply Changes` (5535).

---

## 6. Mobile GPS Configuration (5538-5579)

### 6.1 Section visibility (5538)

```php
if ( file_exists('/etc/mobilegps') && file_exists('/etc/dstar-radio.mmdvmhost') ) {
```
Title: `$lang['mobilegps_config']` = **"Mobile GPS Configuration"** (5540).

### 6.2 Field `mobilegps_enable`: hidden fallback plus checkbox toggle (5539, 5546-5560)

- Line 5539 is placed **before** the `<h2>`, outside the table:
  `<input type="hidden" name="mobilegps_enable" value="OFF" />`.
  The checkbox below has the same name and comes later in the form. PHP keeps the last value,
  so the post is `ON` if the box is checked and `OFF` if not. Both are submitted only when the section is rendered.
- Label: `$lang['mobilegps_enable']` = **"MobileGPS Enable:"**
- Tooltip: **MobileGPS** — "Enable the MobileGPS service, this uses a GPS dongle to update the position information in MMDVMHost."
- Checked state: `/etc/mmdvmhost [Mobile GPS] Enable` is set and truthy → checked. Unset or falsy → unchecked (5549-5558).
- Markup (value cell `align="left"`), with `checked="checked"` and `aria-checked="true"` only when checked:
  ```html
  <td align="left"><div class="switch">
    <input id="toggle-confMobilegps_enable" class="toggle toggle-round-flat" type="checkbox"
           name="mobilegps_enable" value="ON" [checked="checked"] aria-hidden="true" tabindex="-1"
           {$toggleMobilegps_enableCr} />
    <label id="aria-toggle-confMobilegps_enable" role="checkbox" tabindex="0" aria-label="Mobile GPS"
           aria-checked="true|false" onKeyPress="toggleMobilegps_enable()" onclick="toggleMobilegps_enable()"
           for="toggle-confMobilegps_enable"><font style="font-size:0px">Mobile GPS</font></label>
  </div></td>
  ```
  `$toggleMobilegps_enableCr` is `onclick="toggleMobilegps_enable()"` when the user agent contains
  `Chrome` and empty otherwise (3949, 3972, 3997).
- **`toggleMobilegps_enable()` is not defined anywhere.** It is not in `/functions.js` or in the
  page's inline scripts (checked with grep). Clicking or pressing a key on the label throws a
  `ReferenceError`. The label's `for=` still toggles the checkbox on click, but `aria-checked`
  never updates and the Space key does not toggle it. Other toggles, such as `togglePOCSAGCheckbox`,
  flip `aria-checked` and click on Space (keyCode 32).

### 6.3 Field `mobilegps_port` (5561-5572)

- Type: `<select name="mobilegps_port">`. Value cell `align="left"`.
- Label: `$lang['mobilegps_port']` = **"GPS Port:"**
- Tooltip: **GPS Port** — "The port used by the Mobile GPS service."
- Options:
  1. The current port, selected:
     - value = `grep "Port=/dev/" /etc/mobilegps | awk -F "=" '{print $2}' | awk -F "/" '{print $3}'` (for example `ttyACM0`)
     - label = `grep "Port=/dev/" /etc/mobilegps | awk -F "=" '{print $2}'` (for example `/dev/ttyACM0`)
     - Both go through `htmlspecialchars`. `exec()` returns the **last** matching line. If there is no
       match, the option has an empty value and label.
  2. One option per entry from `ls /dev/ | egrep -h "ttyA|ttyUSB"`: `<option value="{name}">/dev/{name}</option>`.
     This matches `ttyAMA*`, `ttyACM*` and `ttyUSB*`, in the order of `ls`. The current port is listed again.
- Save (3247-3250, only if `/etc/mobilegps` exists): if not empty, strip non-alphanumerics
  (`preg_replace('/[^a-z0-9]/i', '', ...)`) and stage a flat edit `Port=/dev/{value}` on `/etc/mobilegps`.

### 6.4 Field `mobilegps_speed` (5573-5576)

- Type: `<input type="text" name="mobilegps_speed" size="13" maxlength="6" value="..." />`
- Label: `$lang['mobilegps_speed']` = **"GPS Port Speed:"**
- Tooltip: **GPS Port Speed** — "Port speed for the Mobile GPS port."
- Value: `grep "Speed" /etc/mobilegps | awk -F "=" '{print $2}'`, passed through `htmlspecialchars`.
  This takes the last line containing `Speed`.
- Save (3251-3254): if not empty, keep digits only and stage the flat edit `Speed={digits}` on `/etc/mobilegps`.

### 6.5 Mobile GPS save block (3207-3255)

Runs only if `/etc/mobilegps` exists. The `mobilegps_enable` part runs only if that post is non-empty,
which it always is when the section is rendered (`ON`/`OFF`):

1. Add missing keys to `/etc/mmdvmhost [Mobile GPS]`: `Enable=0`, `Address=127.0.0.1`, `Port=7834`.
2. If `/etc/ysfgateway` has no `[GPSD]` section, add missing `[Mobile GPS] Enable=0`,
   `Address=127.0.0.1`, `Port=7834`. On YSFGateway > 20200907, `[GPSD]` is created at 3161 and
   `[Mobile GPS]` is removed at 3170, so this step is normally a no-op.
3. Stage literal flat edits on `/etc/mobilegps`: `Daemon=0`, `Debug=0`, `DisplayLevel=0`, `FileLevel=1`,
   `FilePath=/var/log/pi-star`.
4. `ON`:
   - `/etc/mmdvmhost [Mobile GPS] Enable=1`
   - `/etc/ysfgateway [Mobile GPS] Enable=1` if the key exists
   - `/etc/nxdngateway [Mobile GPS] Enable=1` if the key exists (normally removed earlier at 3110 on NXDNGateway > 20210131)
   - flat `Enabled=1` on `/etc/mobilegps`

   `OFF`: the same keys set to `0`.
5. Port and speed, as described in 6.3 and 6.4.

**Flat-writer semantics** (`config/config_writer.php`, `config_writer_stage_flat` at 182-206 and
`config_writer_commit` at 387+):
- `/etc/mobilegps` is on the allow-list (line 105).
- Keys must match `^[A-Za-z_][A-Za-z0-9_]*$`. Values must not contain NUL, CR or LF.
- On commit, the writer replaces the **first line in the file that starts with `Key=`**, whatever the
  INI section. If the file has the same key in several sections (for example `Port=` or `Debug=`),
  only the first one changes.
- If the key does not exist, the edit is skipped and an `error_log` entry is written.
  New keys are never added.
- The file is then installed atomically. The real layout of `/etc/mobilegps` is not in this repo,
  so it is unconfirmed which section's `Port=`/`Debug=` comes first and whether an `Enabled=` key exists.

**Service note:** `mobilegps.service` is **not** stopped or started by configure.php (554-575, 3838-3860).
Changes to `/etc/mobilegps` take effect only after the service is restarted some other way or the system reboots.

### 6.6 Section button

`Apply Changes` (5578).

---

## 7. Field inventory (form order)

| # | POST name | Type | Section | Written to |
|---|---|---|---|---|
| 1 | `p25StartupHost` | select | P25 | p25gateway [Network] Startup→Static; ysf2p25 [P25 Network] StartupDstId |
| 2 | `p25nac` | text (cond.) | P25 | mmdvmhost [P25] NAC |
| 3 | `nxdnStartupHost` | select | NXDN | nxdngateway [Network] Startup→Static, or mmdvmhost [NXDN Network] GatewayAddress/Port; ysf2nxdn [NXDN Network] StartupDstId |
| 4 | `nxdnran` | text (cond.) | NXDN | mmdvmhost [NXDN] RAN |
| 5 | `m17StartupHost` | select | M17 | m17gateway [Network] Startup (with room) |
| 6 | `m17StartupRoom` | select | M17 | m17gateway [Network] Startup (suffix) |
| 7 | `m17can` | text | M17 | mmdvmhost [M17] CAN |
| 8 | `pocsagServer` | select | POCSAG | dapnetgateway [DAPNET] Address |
| 9 | `pocsagCallsign` | text | POCSAG | dapnetgateway [General] Callsign |
| 10 | `pocsagFrequency` | text | POCSAG | mmdvmhost [POCSAG] Frequency (MHz→Hz) |
| 11 | `pocsagAuthKey` | password | POCSAG | dapnetgateway [DAPNET] AuthKey |
| 12 | `pocsagWhitelist` | text | POCSAG | dapnetgateway [General] WhiteList |
| 13 | `pocsagBlacklist` | text | POCSAG | dapnetgateway [General] BlackList |
| 14 | `mobilegps_enable` | hidden `OFF` | GPS | (fallback for #15) |
| 15 | `mobilegps_enable` | checkbox toggle `ON` | GPS | mmdvmhost [Mobile GPS] Enable; mobilegps Enabled; ysf/nxdn gw [Mobile GPS] Enable if present |
| 16 | `mobilegps_port` | select | GPS | mobilegps Port=/dev/… (flat) |
| 17 | `mobilegps_speed` | text | GPS | mobilegps Speed (flat) |

There are also 5 section `Apply Changes` buttons (`type="button"`, unnamed).

---

## Services/commands touched by this section on Apply

- `sudo mount -o remount,rw /` … `sudo sync ×3 && sudo mount -o remount,ro /` (359, 3944)
- `sudo systemctl stop` / `start`: `p25gateway`, `p25parrot`, `nxdngateway`, `nxdnparrot`, `m17gateway`,
  `ysf2p25`, `ysf2nxdn`, `dmr2nxdn`, `dapnetgateway`, `mmdvmhost`, `ysfgateway`. In practice every DV
  service in 554-575 and 3838-3860 is stopped and started, plus `cron.service` and `sudo systemctl daemon-reload`.
- `mobilegps.service` is **not** restarted.
- `sudo install -m 644 -o root -g root /tmp/<tmp> <dest>` for `/etc/mmdvmhost`, `/etc/p25gateway`,
  `/etc/nxdngateway`, `/etc/m17gateway`, `/etc/ysf2p25`, `/etc/ysf2nxdn`, `/etc/dapnetgateway`,
  `/etc/ysfgateway`, `/etc/dmr2nxdn` (line-count guarded, see 0.4).
- `config_writer_commit(false)`: an atomic install of `/etc/mobilegps` with the staged flat edits.
- `sudo touch /usr/local/etc/NXDNHostsLocal.txt` (3045, only during the NXDN `HostsFile` migration).
- Version probes: `P25Gateway -v`, `NXDNGateway -v`, `YSFGateway -v` (piped through `awk`/`cut`; 3085, 3103, 3130).
- Render-time only: `grep`/`awk` on `/etc/mobilegps`, `ls /dev/ | egrep -h "ttyA|ttyUSB"`.
