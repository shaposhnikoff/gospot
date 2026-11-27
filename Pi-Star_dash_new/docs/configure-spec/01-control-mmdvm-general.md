# configure.php spec, part 01: header, hardware info, Factory Reset, Control Software, MMDVMHost Configuration, General Configuration, and the global Apply flow

Reference: `Pi-Star_DV_Dash/admin/configure.php` at revision `10bf34c9` (5765 lines). All line numbers below refer to that file unless another file is named.
Helpers referenced: `Pi-Star_DV_Dash/config/config_writer.php`, `config/language.php`, `config/ircddblocal.php`, `config/version.php`, `config/csrf.php`, `functions.js` (served as `/functions.js`), `lang/english_uk.php`.

Labels shown as `$lang['x']` are resolved to the English UK strings from `lang/english_uk.php`.

---

## 0. Data loaded at page start (both GET and POST)

These are loaded once at the top of the script (lines 66-218). Every displayed value and every POST handler reads these in-memory copies. A POST handler that changes a file therefore sees the **pre-change** values.

| Variable | Source | Parser | Notes |
|---|---|---|---|
| `$cpuTempCRaw` | `cat /sys/class/thermal/thermal_zone0/temp` (l.67) | raw | if `> 1000` divide by 1000, round 1 dp; else round 1 dp. `°F = round(C*9/5+32, 1)` |
| `$version` | `config/version.php` | PHP | currently `'20261121'` |
| `$callsign`, `$gatewayConfigPath='/etc/ircddbgateway'` | `config/ircddblocal.php` | PHP | |
| `$pistarLanguage`, `$lang[]` | `config/language.php` → `lang/$pistarLanguage.php` | PHP | |
| `$cpuLoad` | `sys_getloadavg()` | | |
| `$configPistarRelease` | `/etc/pistar-release` | `parse_ini_file(..., true)` | `['Pi-Star']['Version']` used in header and version gates |
| `$configdstar` | `/etc/dstarrepeater` | custom: each line with `=` split by `preg_split('/=/')` → `list($k,$v)` (only first two parts kept), value `trim(str_replace('"','',$v))`, kept only if non-empty | |
| `$configs` | `/etc/ircddbgateway` | same custom parser; additionally skips key `ircddbPassword` | values containing `=` are truncated at the 2nd `=`; empty values are absent from the array |
| `$configmmdvm` | `/etc/mmdvmhost` | `parse_ini_file(..., true)` | PHP INI_SCANNER_NORMAL: `yes/on/true` become `"1"`, `no/off/false/null/none` become `""`; quotes stripped; comments dropped |
| `$configysfgateway` | `/etc/ysfgateway` | `parse_ini_file` | unconditional |
| `$configysf2dmr`, `$configysf2nxdn`, `$configysf2p25`, `$configdgidgateway`, `$configdmr2ysf`, `$configdmr2nxdn`, `$configp25gateway`, `$confignxdngateway`, `$configm17gateway`, `$confignxdn2dmr`, `$configdapnetgw`, `$configaprsgw` | `/etc/ysf2dmr`, `/etc/ysf2nxdn`, `/etc/ysf2p25`, `/etc/dgidgateway`, `/etc/dmr2ysf`, `/etc/dmr2nxdn`, `/etc/p25gateway`, `/etc/nxdngateway`, `/etc/m17gateway`, `/etc/nxdn2dmr`, `/etc/dapnetgateway`, `/etc/aprsgateway` | `parse_ini_file`, each only `if (file_exists(...))` | variable is undefined when the file is missing |
| `$configdmrgateway` | `/etc/dmrgateway` | `parse_ini_file` if `fopen` succeeds | |
| `$configModem` | `/etc/dstar-radio.dstarrepeater` then `/etc/dstar-radio.mmdvmhost` (l.209-218) | `parse_ini_file` | if both markers exist the mmdvmhost one wins (it is parsed second and overwrites). Undefined if neither exists. |
| `$MYCALL` | `strtoupper($configs['gatewayCallsign'])` (l.242) | | |

`aprspass($callsign)` (l.220-237): standard APRS-IS passcode hash. Strip from the first `-`, uppercase, first 10 chars, hash `0x73e2`, XOR pairs of bytes (`hi<<8`, `lo`), `& 0x7fff`.

---

## 1. Page header

### 1.1 `<head>` (l.244-302)

- `<title>`: `"$MYCALL - Digital Voice Dashboard - Configuration"` (`$MYCALL` - `$lang['digital_voice']` `$lang['dashboard']` - `$lang['configuration']`).
- CSS: `css/nice-select.min.css?ver=<Pi-Star Version>`, `css/pistar-css.php?version=0.95`.
- Inline JS (l.265-299):
  - `disablesubmitbuttons()`: sets `disabled=true` on every `<input type="button">` on the page.
  - `submitform()`: `disablesubmitbuttons(); document.getElementById("config").submit();`. All section "Apply Changes" buttons call this.
  - `submitPassform()` / `submitPskform()`: same, for forms `adminPassForm` / `autoApPassForm` (outside this scope).
  - `factoryReset()`: `confirm('WARNING: This will set all your settings back to factory defaults. WiFi setup will be retained to maintain network access to this Pi.\n\nAre you SURE you want to do this?\n\nPress OK to restore the factory configuration\nPress Cancel to go back.')`. OK submits form `#factoryReset`, Cancel returns false.
  - `resizeIframe(obj)`: unused in this scope.
- External script: `/functions.js?version=1.715`.
- `<body onload="checkFrequency(); return false;">`: colours the frequency inputs on load (see 5.5).

### 1.2 Banner (l.303-324)

- `pistar_warnings_render()` (site-wide banners, `config/banner_warnings.inc`) is output first.
- Top-right small text (8px): `Pi-Star:<pistar-release [Pi-Star] Version> / Dashboard: <$version>`.
- `<h1>`: `Pi-Star Digital Voice - Configuration`.
- Link bar (right aligned, white): `Dashboard` (`/`) | `Admin` (`/admin/`) | `Expert` (`/admin/expert/`) | `Calibrate` (`/admin/calibration.php`) | `Power` (`/admin/power.php`) | `Update` (`/admin/update.php`) | `Backup/Restore` (`/admin/config_backup.php`) | `Factory Reset` (`javascript:factoryReset();`).

### 1.3 Whole-page gate

Everything below (hardware table, POST handler and form) is inside `if ($_SERVER["PHP_SELF"] == "/admin/configure.php")` (l.328). The `else` branch (l.5748-5765) prints only an empty content area and the footer.

Footer (both branches): `Pi-Star web config, © Andy Taylor (MW0MWZ) 2014-<year>.` + support group link + downloads link. After it, `nice-select.min.js` is loaded and every `<select>` with **more than 30 options**, no `onchange` handler and a name not containing `ExtendedId` is converted into a searchable NiceSelect (l.5723-5733). In this scope this affects `confHardware`, `systemTimezone` and, if long enough, `selectedAPRSHost`, `mmdvmDisplayPort` and `dashboardLanguage`.

---

## 2. Hardware info table (l.333-350)

Rendered on GET **and** POST (it is printed before the POST handler starts).

`<h2>` `Gateway Hardware Information` (`$lang['hardware_info']`). `<table style="table-layout: fixed;">`, 2 rows, 6 visual columns.

Header row (`<th>`, each label is an `<a class="tooltip" href="#">` with a `<span>` tooltip):

| # | colspan | Label | Tooltip (HTML in `<span>`) |
|---|---|---|---|
| 1 | 1 | `Hostname` | `<b>Hostname</b>The name of host<br />running the Pi-Star Software.` |
| 2 | 1 | `Kernel` | `<b>Release</b>This is the version<br />number of the Linux Kernel running<br />on this Raspberry Pi.` |
| 3 | 2 | `Platform` | `<b>Uptime:<br />` + output of `uptime -p` with every `,` replaced by `,<br />` + `</b>` |
| 4 | 1 | `CPU Load` | `<b>CPU Load</b>This is the standard Linux<br />system load indicator.` |
| 5 | 1 | `CPU Temp` | `<b>CPU Temp</b>` |

Data row:

| # | Value |
|---|---|
| 1 | `php_uname('n')`; if `strlen >= 16` show `substr(..., 0, 14) . '..'` |
| 2 | `php_uname('r')` |
| 3 (colspan 2) | `htmlspecialchars(exec('/usr/local/bin/platformDetect.sh'))` |
| 4 | `number_format(load1,2) / number_format(load5,2) / number_format(load15,2)` (literal ` / ` separators) |
| 5 | `<td style="background: COLOR">C°C / F°F</td>` where COLOR is `#1d1` if `C < 50`, `#fa0` if `C >= 50`, `#f00` if `C >= 69` |

Followed by `<br />`.

---

## 3. Modem warning alert (GET only, l.3948)

On GET, before the forms: if (`/etc/dstar-radio.mmdvmhost` or `/etc/dstar-radio.dstarrepeater` exists) and `$configModem['Modem']['Hardware']` is unset or falsy, emit:

```js
alert("WARNING:\nThe Modem selection section has been updated,\nPlease re-select your modem from the list.")
```

## 3.1 Chrome-only toggle `onclick` (l.3949-3999)

If `HTTP_USER_AGENT` contains `Chrome`, each toggle `<input type="checkbox">` gets an extra attribute `onclick="toggleXxxCheckbox()"` (for example `$toggleDMRCheckboxCr = 'onclick="toggleDMRCheckbox()"'`). Otherwise the attribute is an empty string. In this scope that covers DMR, DSTAR, YSF, P25, NXDN, M17, YSF2DMR, YSF2NXDN, YSF2P25, DMR2YSF, DMR2NXDN, POCSAG and APRSGateway. This only syncs the ARIA state and has no effect on the saved value.

---

## 4. Factory Reset form (l.4001-4004)

```html
<form id="factoryReset" action="/admin/configure.php" method="post">
  <input type="hidden" name="csrf_token" value="..."/>
  <div><input type="hidden" name="factoryReset" value="1" /></div>
</form>
```

- No visible controls. It is submitted only by the banner link `Factory Reset` → `factoryReset()` → `confirm(...)` → `submit()`.
- POST name `factoryReset`, hidden, value `1`.

Save behaviour (l.586-618):

1. As with every POST, the request first passes `csrf_verify()` (l.63) and runs `sudo mount -o remount,rw /` (l.359).
2. The adminPassword and autoapPsk branches are skipped because those fields are absent.
3. `sudo systemctl stop cron.service` plus the stop of all 22 DV services runs (l.551-575, see 7.2), then the "Working..." table is printed and the script sleeps 1 s.
4. `if (empty($_POST['factoryReset']) != TRUE)`: prints the `Factory Reset Config` / `Loading fresh configuration file(s)...` table, then `exec('sudo /usr/local/sbin/pistar-factory-reset --really')`. Per the comment at l.594-603, this wrapper unzips `/usr/local/bin/config_clean.zip` into `/etc/`, removes `/etc/dstar-radio.*` and `/etc/pistar-css.ini`, runs `git reset --hard origin/master` on the dashboard, `/usr/local/sbin` and `/usr/local/bin` trees, and handles its own mount rw / sync / ro.
5. JS reload after 5000 ms, footer, `die()`.

Notes:
- The comment at l.605-609 says a typed `RESET` (`factoryResetConfirm`) is required, but **this handler does not check `factoryResetConfirm`**. That check exists only in `admin/expert/edit_dashboard.php` (l.134). From configure.php the only guard is the JS `confirm()`.
- The handler does **not** restart the stopped services or cron, and it does not remount `/` read-only itself. The wrapper's behaviour after reset is not visible in this repository.

---

## 5. Main form `#config`

`<form id="config" action="/admin/configure.php" method="post">` opens at l.4006 and closes at l.5711. It contains **every** section of the page. Any "Apply Changes" button submits the whole form, so all fields of all sections are POSTed together and the handler processes all of them. The first child is the CSRF hidden input `csrf_token` (l.4007).

General table layout used by every section in scope:

```html
<h2>SECTION TITLE</h2>
<table>
<tr>
  <th width="200"><a class="tooltip" href="#">Setting<span><b>Setting</b></span></a></th>
  <th colspan="2"><a class="tooltip" href="#">Value<span><b>Value</b>The current value from<br />the configuration files</span></a></th>
</tr>
<!-- rows: <td align="left"><a class="tooltip2" href="#">LABEL:<span><b>TT title</b>TT text</span></a></td> + value cell(s) -->
</table>
<div><input type="button" value="Apply Changes" onclick="submitform()" /><br /><br /></div>
```

In General Configuration the Value header tooltip is `<b>Value</b>The current value from the<br />configuration files` (the line break is in a different place).

Toggle markup (identical pattern for every mode switch; `X` = id suffix):

```html
<td align="left"[ colspan="2"]><div class="switch">
  <input id="toggle-X" class="toggle toggle-round-flat" type="checkbox" name="NAME" value="ON" [checked="checked"] aria-hidden="true" tabindex="-1" [onclick="toggleXCheckbox()" Chrome only] />
  <label id="aria-toggle-X" role="checkbox" tabindex="0" aria-label="ARIA" aria-checked="true|false" onKeyPress="toggleXCheckbox()" onclick="toggleXCheckbox()" for="toggle-X"><font style="font-size:0px">ARIA</font></label>
</div></td>
```

Checkbox semantics: a hidden input with the same name and value `OFF` is placed **before** the checkbox. PHP keeps the last value for a duplicated name, so the POST carries `ON` when the box is checked and `OFF` when it is not. `toggleXCheckbox()` in `functions.js` only flips `aria-checked` on the label, and on keyCode 32 it clicks the label.

### 5.1 Section "Control Software" (l.4008-4040)

Always shown, whatever the controller.

`<h2>Control Software</h2>` (`$lang['control_software']`), standard 2-column table.

#### Row 1: `controllerSoft`, radio (l.4014-4027)

- Label: `Controller Software:`. Tooltip: `<b>Radio Control Software</b>Choose the software used to control the DV Radio Module. Please note that DV Mega hardware will require a firmware upgrade.`
- Value cell: `<td align="left" colspan="2">` with two radios: `value="DSTAR"` text `DStarRepeater `, then `value="MMDVM"` text `MMDVMHost (DV-Mega Minimum Firmware 3.07 Required)`.
- Preselection:
  - `/etc/dstar-radio.mmdvmhost` exists → `MMDVM` checked. The `DSTAR` radio gets `onclick="alert('After applying your Configuration Settings, you will need to powercycle your Pi.');"`.
  - else `/etc/dstar-radio.dstarrepeater` exists → `DSTAR` checked. The `MMDVM` radio gets the same `onclick` alert.
  - else (no marker) → `MMDVM` checked. The `DSTAR` radio gets the alert.
- Because one radio is always checked, this field is posted on every Apply.
- Save (l.638-643):

  ```php
  if (empty($_POST['controllerSoft']) != TRUE ) {
    system('sudo rm -rf /etc/dstar-radio.*');
    if ($_POST['controllerSoft'] === 'DSTAR') { system('sudo touch /etc/dstar-radio.dstarrepeater'); }
    if ($_POST['controllerSoft'] === 'MMDVM') { system('sudo touch /etc/dstar-radio.mmdvmhost'); }
  }
  ```

  This runs on **every** Apply, even with no change: both markers are deleted and the selected one is recreated empty. Its content is restored later by the modem-file write-back (7.4, step "modem"). See 7.5 for the complete controller-switch logic.

#### Row 2: `trxMode`, radio (l.4028-4038)

- Label: `Controller Mode:`. Tooltip: `<b>TRX Mode</b>Choose the mode type Simplex node or Duplex repeater.`
- Value cell colspan 2: radio `value="SIMPLEX"` text `Simplex Node `, radio `value="DUPLEX"` text `Duplex Repeater (or Half-Duplex on Hotspots)`.
- Preselection: `SIMPLEX` if `$configmmdvm['Info']['RXFrequency'] === $configmmdvm['Info']['TXFrequency']` (strict string compare of the raw INI strings), else `DUPLEX`.
- Save (l.1448-1466). Runs **after** both frequency handlers (l.923 and l.1034) and **before** the hardware handler (l.1782):

  ```php
  if (empty($_POST['trxMode']) != TRUE ) {
    if (RX === TX && trxMode == "DUPLEX")  { $configmmdvm['Info']['RXFrequency'] = $configmmdvm['Info']['TXFrequency'] - 1; }
    if (RX !== TX && trxMode == "SIMPLEX") { $configmmdvm['Info']['RXFrequency'] = $configmmdvm['Info']['TXFrequency']; }
    if (trxMode == "DUPLEX")  { [General]Duplex = 1; [DMR Network]Slot1 = '1'; [DMR Network]Slot2 = '1'; }
    if (trxMode == "SIMPLEX") { [General]Duplex = 0; [DMR Network]Slot1 = '0'; [DMR Network]Slot2 = '1'; }
  }
  ```

  - All in `/etc/mmdvmhost`. RX and TX here are the values already overwritten by the frequency handlers of this POST.
  - Switching SIMPLEX→DUPLEX in one submit: the form still has only `confFREQ`, so the simplex handler sets RX=TX, then RX becomes `TX - 1` Hz (integer arithmetic, for example `438799999`). Only `/etc/mmdvmhost` gets that RX. ircddbgateway, dstarrepeater and the other gateways keep the simplex values from the simplex handler.
  - The `confHardware` handler runs later and may override `[General]Duplex` and `[DMR Network]Slot1` again (see 5.3, row `confHardware`).

#### Section button (l.4040)

`<input type="button" value="Apply Changes" onclick="submitform()" />`.

### 5.2 Section "MMDVMHost Configuration" (l.4041-4276)

**Visibility:** the whole block (hidden inputs, heading, table and its Apply button) is rendered only if `file_exists('/etc/dstar-radio.mmdvmhost')` (l.4041, closed at l.4276). In DStarRepeater mode or with no marker, none of these fields are posted, so their handlers do nothing.

#### Hidden defaults (l.4042-4053), always emitted inside the block

```
MMDVMModeDMR=OFF  MMDVMModeDSTAR=OFF  MMDVMModeFUSION=OFF  MMDVMModeP25=OFF
MMDVMModeNXDN=OFF MMDVMModeM17=OFF    MMDVMModeYSF2DMR=OFF MMDVMModeYSF2NXDN=OFF
MMDVMModeYSF2P25=OFF MMDVMModeDMR2YSF=OFF MMDVMModeDMR2NXDN=OFF MMDVMModePOCSAG=OFF
```

These 12 hidden fields are emitted even when the matching toggle row is hidden (for example YSF2NXDN when `/etc/ysf2nxdn` is missing). Those modes therefore always POST `OFF`. See the per-row notes for the effect.

`<h2>MMDVMHost Configuration</h2>` (`$lang['mmdvmhost_config']`), standard table.

The six "native mode" rows share one layout: `<td>` label, `<td align="left">` toggle (no colspan), then `<td>` with `RF Hangtime: <input .../>` and a newline, then `Net Hangtime: <input .../>`. All hang-time inputs are `type="text" size="7" maxlength="3"`, with no pattern and no placeholder. The default shown when the key is missing is `20`.

Hang-time save rule (shared, l.1717-1779): first, unconditionally, `unset [General]ModeHang`, `unset dmrgateway [General]Timeout`, and if present `[General]RFModeHang = 300` and `[General]NetModeHang = 300`. Then each hang field is saved only `if (empty($_POST[f]) != TRUE)` as `preg_replace('/[^0-9]/', '', value)`. **`"0"` counts as empty in PHP, so a value of 0 is never saved** and the old value stays.

| Row | Label (`$lang`) / tooltip | Toggle name, id, aria-label | Checked when | Hang-time fields (value source) |
|---|---|---|---|---|
| DMR (l.4060-4072) | `DMR Mode:` / `<b>DMR Mode</b>Turn on DMR Features` | `MMDVMModeDMR`, `toggle-dmr`, `DMR Mode` | `[DMR]Enable == 1` | `dmrRfHangTime` ← `[DMR]ModeHang`; `dmrNetHangTime` ← `[DMR Network]ModeHang` |
| D-Star (l.4073-4085) | `D-Star Mode:` / `<b>D-Star Mode</b>Turn on D-Star Features` | `MMDVMModeDSTAR`, `toggle-dstar`, `DStar Mode` | `[D-Star]Enable == 1` | `dstarRfHangTime` ← `[D-Star]ModeHang`; `dstarNetHangTime` ← `[D-Star Network]ModeHang` |
| M17 (l.4086-4098) | `M17 Mode:` / `<b>M17 Mode</b>Turn on M17 Features` | `MMDVMModeM17`, `toggle-m17`, `M 17 Mode` | `[M17]Enable == 1` | `m17RfHangTime` ← `[M17]ModeHang`; `m17NetHangTime` ← `[M17 Network]ModeHang` |
| YSF (l.4099-4111) | `YSF Mode:` / `<b>YSF Mode</b>Turn on YSF Features` | `MMDVMModeFUSION`, `toggle-ysf`, `Y S F Mode` | `[System Fusion]Enable == 1` | `ysfRfHangTime` ← `[System Fusion]ModeHang`; `ysfNetHangTime` ← `[System Fusion Network]ModeHang` |
| P25 (l.4112-4124) | `P25 Mode:` / `<b>P25 Mode</b>Turn on P25 Features` | `MMDVMModeP25`, `toggle-p25`, `P 25 Mode` | `[P25]Enable == 1` | `p25RfHangTime` ← `[P25]ModeHang`; `p25NetHangTime` ← `[P25 Network]ModeHang` |
| NXDN (l.4125-4137) | `NXDN Mode:` / `<b>NXDN Mode</b>Turn on NXDN Features` | `MMDVMModeNXDN`, `toggle-nxdn`, `NXDN Mode` | `[NXDN]Enable == 1` | `nxdnRfHangTime` ← `[NXDN]ModeHang`; `nxdnNetHangTime` ← `[NXDN Network]ModeHang` |

The order on the page is DMR, D-Star, **M17**, YSF, P25, NXDN. M17 comes before YSF.

Hang-time save targets (l.1723-1779):

| Field | Writes |
|---|---|
| `dmrRfHangTime` | mmdvmhost `[DMR]ModeHang`; dmrgateway `[General]RFTimeout` (same digits) |
| `dmrNetHangTime` | mmdvmhost `[DMR Network]ModeHang`; dmrgateway `[General]NetTimeout` |
| `dstarRfHangTime` | mmdvmhost `[D-Star]ModeHang` |
| `dstarNetHangTime` | mmdvmhost `[D-Star Network]ModeHang` |
| `ysfRfHangTime` | mmdvmhost `[System Fusion]ModeHang`; if `$configdgidgateway` is set: dgidgateway `[General]`, `[YSF Network]`, `[FCS Network]`, `[IMRS Network]` `RFHangTime` |
| `ysfNetHangTime` | mmdvmhost `[System Fusion Network]ModeHang`; dgidgateway the same four sections `NetHangTime` |
| `p25RfHangTime` | mmdvmhost `[P25]ModeHang`; p25gateway `[Network]RFHangTime = "0"` (hardcoded 0, not the posted value) |
| `p25NetHangTime` | mmdvmhost `[P25 Network]ModeHang`; p25gateway `[Network]NetHangTime = "0"` |
| `nxdnRfHangTime` | mmdvmhost `[NXDN]ModeHang`; nxdngateway `[Network]RFHangTime = "0"` |
| `nxdnNetHangTime` | mmdvmhost `[NXDN Network]ModeHang`; nxdngateway `[Network]NetHangTime = "0"` |
| `m17RfHangTime` | mmdvmhost `[M17]ModeHang` |
| `m17NetHangTime` | mmdvmhost `[M17 Network]ModeHang`; m17gateway `[Network]HangTime` only if that key already exists |

Mode toggle save (l.2460-2590). Each runs only if the POST value is non-empty, which is always the case when the block is rendered. `"1"` / `"0"` are strings:

| Field | `ON` | `OFF` |
|---|---|---|
| `MMDVMModeDMR` | mmdvmhost `[DMR]Enable=1`, `[DMR Network]Enable=1`; ysf2dmr `[Enabled]Enabled=0` | `[DMR]Enable=0`, `[DMR Network]Enable=0` |
| `MMDVMModeDSTAR` | `[D-Star]Enable=1`, `[D-Star Network]Enable=1` | both `0` |
| `MMDVMModeFUSION` | `[System Fusion]Enable=1`, `[System Fusion Network]Enable=1`; dmr2ysf `[Enabled]Enabled=0` | both `0` |
| `MMDVMModeP25` | `[P25]Enable=1`, `[P25 Network]Enable=1`; ysf2p25 `[Enabled]Enabled=0` | both `0` |
| `MMDVMModeNXDN` | `[NXDN]Enable=1`, `[NXDN Network]Enable=1`; ysf2nxdn `[Enabled]Enabled=0` | both `0` |
| `MMDVMModeM17` | `[M17]Enable=1`, `[M17 Network]Enable=1` | both `0` |
| `MMDVMModeYSF2DMR` | ysf2dmr `[Enabled]Enabled=1` | `0` |
| `MMDVMModeYSF2NXDN` | ysf2nxdn `[Enabled]Enabled=1`; mmdvmhost `[NXDN]Enable=0`, `[NXDN Network]Enable=0` | ysf2nxdn `[Enabled]Enabled=0` |
| `MMDVMModeYSF2P25` | ysf2p25 `[Enabled]Enabled=1`; mmdvmhost `[P25]Enable=0`, `[P25 Network]Enable=0` | ysf2p25 `[Enabled]Enabled=0`. Also, inside this block, `if MMDVMModeFUSION === 'OFF'` then ysf2p25 `[Enabled]Enabled=0` (l.2512) |
| `MMDVMModeDMR2YSF` | dmr2ysf `[Enabled]Enabled=1`; **unset** dmrgateway `[DMR Network 3]`, then rebuild it as: `Enabled=0`, `Name=DMR2YSF_Cross-over`, `Id=<[DMR Network 2]Id>`, `Address=127.0.0.1`, `Port=62033`, `Local=62034`, `TGRewrite0=2,7000001,2,1,999998`, `SrcRewrite0=2,1,2,7000001,999998`, `PCRewrite0=2,7000001,2,1,999998`, `Password="PASSWORD"`, `Location=0`, `Debug=0`; mmdvmhost `[System Fusion]Enable=0`, `[System Fusion Network]Enable=0` | dmr2ysf `[Enabled]Enabled=0`; dmrgateway `[DMR Network 3]Enabled=0` |
| `MMDVMModeDMR2NXDN` | if `MMDVMModeDMR2YSF === 'ON'` then dmr2ysf `[Enabled]Enabled=0`; if `MMDVMModeYSF2NXDN === 'ON'` then ysf2nxdn `[Enabled]Enabled=0`; dmr2nxdn `[Enabled]Enabled=1`; unset and rebuild dmrgateway `[DMR Network 3]` as above but `Name=DMR2NXDN_Cross-over`, `Port=62035`, `Local=62036`; mmdvmhost `[NXDN]Enable=0`, `[NXDN Network]Enable=0` | dmr2nxdn `[Enabled]Enabled=0`; dmrgateway `[DMR Network 3]Enabled=0` |
| (derived, l.2577-2584) | if either DMR2YSF or DMR2NXDN was posted: dmrgateway `[DMR Network 3]Enabled = "1"` if either is `ON`, else `"0"` | |
| `MMDVMModePOCSAG` | `[POCSAG]Enable=1`, `[POCSAG Network]Enable=1` | both `0` |

Processing order matters, because later handlers override earlier ones: DMR → DSTAR → FUSION → P25 → NXDN → M17 → YSF2DMR → YSF2NXDN → YSF2P25 → DMR2YSF → DMR2NXDN → DMR Network 3 → POCSAG. For example, YSF2DMR `ON` sets ysf2dmr `Enabled=1` after DMR `ON` has cleared it, so YSF2DMR wins. DMR2NXDN `ON` overrides YSF2NXDN `ON`.

Cross-mode rows:

| Row | Visible when | Label / tooltip | Toggle name, id, aria-label | Checked when | Layout |
|---|---|---|---|---|---|
| YSF2DMR (l.4138-4149) | always inside the block (**no file check**) | `YSF2DMR:` / `<b>YSF2DMR Mode</b>Turn on YSF2DMR Features` | `MMDVMModeYSF2DMR`, `toggle-ysf2dmr`, `Y S F 2 DMR Mode` | ysf2dmr `[Enabled]Enabled == 1` (undefined variable if `/etc/ysf2dmr` is missing, so it renders unchecked) | toggle cell `colspan="2"` |
| YSF2NXDN (l.4150-4163) | `file_exists('/etc/ysf2nxdn')` | `YSF2NXDN:` / `<b>YSF2NXDN Mode</b>Turn on YSF2NXDN Features` | `MMDVMModeYSF2NXDN`, `toggle-ysf2nxdn`, `Y S F 2 NXDN Mode` | ysf2nxdn `[Enabled]Enabled == 1` | toggle `colspan="2"` |
| YSF2P25 (l.4164-4177) | `file_exists('/etc/ysf2p25')` | `YSF2P25:` / `<b>YSF2P25 Mode</b>Turn on YSF2P25 Features` | `MMDVMModeYSF2P25`, `toggle-ysf2p25`, `Y S F 2 P 25 Mode` | ysf2p25 `[Enabled]Enabled == 1` | toggle `colspan="2"` |
| DMR2YSF (l.4178-4192) | `file_exists('/etc/dmr2ysf')` | `DMR2YSF:` / `<b>DMR2YSF Mode</b>Turn on DMR2YSF Features` | `MMDVMModeDMR2YSF`, `toggle-dmr2ysf`, `DMR 2 Y S F Mode` | dmr2ysf `[Enabled]Enabled == 1` | toggle `<td align="left">` + `<td>Uses 7 prefix on DMRGateway</td>` |
| DMR2NXDN (l.4193-4207) | `file_exists('/etc/dmr2nxdn')` | `DMR2NXDN:` / `<b>DMR2NXDN Mode</b>Turn on DMR2NXDN Features` | `MMDVMModeDMR2NXDN`, `toggle-dmr2nxdn`, `DMR 2 NXDN Mode` | dmr2nxdn `[Enabled]Enabled == 1` | toggle + `<td>Uses 7 prefix on DMRGateway</td>` |
| POCSAG (l.4208-4222) | `file_exists('/etc/dapnetgateway')` | `POCSAG:` / `<b>POCSAG Mode</b>Turn on POCSAG Features` | `MMDVMModePOCSAG`, `toggle-pocsag`, `POCSAG Mode` | mmdvmhost `[POCSAG]Enable == 1` | toggle + `<td>POCSAG Paging Features</td>` |

Hidden-row effect: if a cross-mode row is hidden, the hidden `OFF` is still posted, and the handler writes `[Enabled]Enabled=0` into a PHP array that may be undefined (file missing). The matching write-back (7.4) then installs nothing, because the serialised content falls below the line-count threshold. For a missing `/etc/dmr2ysf` or `/etc/dmr2nxdn`, `OFF` still sets dmrgateway `[DMR Network 3]Enabled=0`, and the derived rule then also forces `Enabled=0`.

#### Display row (l.4223-4270), single `<td align="left" colspan="2">` containing three selects

- Label: `MMDVM Display Type:` (`$lang['mmdvm_display']`). Tooltip: `<b>Display Type</b>Choose your display type, if you have one.`

1. `mmdvmDisplayType`, select. The cell text continues `Port: ` after it.

   | value | text | selected when |
   |---|---|---|
   | `None` | `None` | `[General]Display == "None"` or `== ""` |
   | `OLED3` | `OLED Type 3` | `[General]Display == "OLED"` && `[OLED]Type == "3"` |
   | `OLED6` | `OLED Type 6` | `Display == "OLED"` && `[OLED]Type == "6"` |
   | `Nextion` | `Nextion` | `Display == "Nextion"` |
   | `HD44780` | `HD44780` | `Display == "HD44780"` |
   | `TFT Serial` | `TFT Serial` | `Display == "TFT Serial"` |
   | `LCDproc` | `LCDproc` | `Display == "LCDproc"` |

   If nothing matches (for example OLED with a Type other than 3 or 6), the browser selects the first option (`None`), and the next Apply writes `Display=None`.
   Save (l.2592-2602): if non-empty, and the value starts with `OLED`: `[General]Display = "OLED"`, `[OLED]Type = substr(value,4,1)`, and if Type is `"6"` then `[OLED]Scroll = "0"`. Otherwise `[General]Display = escapeshellcmd(value)`.

2. `mmdvmDisplayPort`, select (l.4234-4262). Options are emitted in this order:
   - `None`: `selected` if `[General]Display` is `"None"` or `""`.
   - If `[Nextion]Port` is set: `modem` (selected if `[Nextion]Port == "modem"`). Then, if Port is not `"None"` and not `""`, one "current port" option: `<option selected="selected" value="$currentPort">[Nextion]Port</option>`, where `$currentPort = str_replace($configmmdvm['Nextion']['Port'], "/dev/", "")`. **The arguments are reversed** (subject is `""`), so `$currentPort` is always `""`. This option therefore has `value=""` with the full port as its label, and since an empty POST is skipped by the handler, keeping it selected leaves the port unchanged.
   - One option per line of `ls /dev/ | egrep -h "ttyA|ttyUSB"` (so ttyAMA*, ttyACM*, ttyUSB*, ...): `value="<name>"` text `/dev/<name>`, never preselected.
   - If `/dev/ttyS2` exists: `value="ttyS2"` text `/dev/ttyS2`, selected if `[Nextion]Port == "/dev/ttyS2"`.
   - If `/dev/ttyNextionDriver` exists: `value="ttyNextionDriver"` text `/dev/ttyNextionDriver`, selected if `[Nextion]Port == "/dev/ttyNextionDriver"`.
   - Several options can carry `selected`. In a single select the browser uses the last one. The cell text continues `Nextion Layout: `.
   Save (l.2604-2611): if non-empty, `None` or `modem` are written as-is to `[Nextion]Port`, anything else as `"/dev/" . value` (no sanitisation). Later post-processing (l.2823-2858): if `[TFT Serial]Port` is set it is forced to `/dev/ttyAMA0`; `[Nextion]Port == "/dev/modem"` becomes `modem`; if the `[NextionDriver]` section is missing it is created with defaults (see 7.3) **and `[Nextion]Port` is forced to `/dev/ttyNextionDriver`**; if `[NextionDriver]` exists and `[Nextion]Port` equals `[Modem]Port` or `[Modem]UARTPort`, it is forced to `/dev/ttyNextionDriver`.

3. `mmdvmNextionDisplayType`, select (l.4263-4268):

   | value | text | selected when `[Nextion]ScreenLayout ==` | saved as `[Nextion]ScreenLayout` |
   |---|---|---|---|
   | `G4KLX` | `G4KLX` | `"0"` | `0` |
   | `ON7LDSL2` | `ON7LDS L2` | `"2"` | `2` |
   | `ON7LDSL3` | `ON7LDS L3` | `"3"` | `3` |
   | `ON7LDSL3HS` | `ON7LDS L3 HS` | `"4"` | `4` |

   There is no option for layout `1`. Any other stored value preselects G4KLX and gets rewritten to `0` on Apply.

Commented-out row (l.4271-4274): `hangTime` (`Mode Hangtime`, `[General]RFModeHang`). It is HTML-commented, so it is **not** rendered or posted.

Section button (l.4275): `Apply Changes` → `submitform()`.

### 5.3 Section "General Configuration" (l.4277-4486)

Always shown. `<h2>General Configuration</h2>` (`$lang['general_config']`).

Right after the heading and before the table (l.4279): `<input type="hidden" name="APRSGatewayEnable" value="OFF" />`. It is always emitted, even when `/etc/aprsgateway` is missing.

Rows in order:

#### `confHostame` (sic), text (l.4285-4288)
- Label `Hostname:`. Tooltip `<b>System Hostname</b>This is the system hostname, used for access to the dashboard etc.`
- `<td align="left" colspan="2"><input type="text" name="confHostame" size="13" maxlength="15" value="..."/>Do not add suffixes such as .local</td>`
- Value: `htmlspecialchars(exec('cat /etc/hostname'))`.
- Save (l.2652-2680), if non-empty:
  - `$new = strtolower(preg_replace('/[^A-Za-z0-9\-]/', '', value))`; `$curr = preg_replace('/[^A-Za-z0-9\-]/','', exec('cat /etc/hostname'))`.
  - If both are non-empty and different: `sudo sed -i 's|CURR|NEW|'` (as escapeshellarg) on `/etc/hostname`, `/etc/hosts` and `/etc/motd`. This replaces the first occurrence on each line.
  - If `/etc/hostapd/hostapd.conf` exists, `ssid=<new>` is staged, even when unchanged (privileged flat, installed mode 600).
  - No `hostname`/`hostnamectl` command is run, so the running hostname does not change until reboot.

#### `confCallsign`, text (l.4289-4292)
- Label `Node Callsign:` (`$lang['node_call']`). Tooltip `<b>Gateway Callsign</b>This is your licenced callsign for use on this gateway, do not append the "G"`
- `size="13" maxlength="7"`, value `$configs['gatewayCallsign']` (from `/etc/ircddbgateway`, raw and not escaped). Colspan 2.
- Save (l.1143-1240), if non-empty. `$U = strtoupper(preg_replace('/[^A-Za-z0-9]/', '', value))`:
  - `/etc/ircddbgateway` (staged flat): `gatewayCallsign=U`, `dplusLogin=str_pad(U, 8, " ")`. If `$configs['ircddbHostname']` contains `openquad.net`: `ircddbUsername=U`.
  - `/var/www/dashboard/config/ircddblocal.php`: `$callsign='U';` (PHP-string stage).
  - `/etc/timeserver` `callsign=U`; `/etc/starnetserver` `callsign=U`, `ircddbUsername=U`.
  - ysfgateway: `[General]Callsign=U`, `[aprs.fi]Password=aprspass(U)`, `[aprs.fi]Description=U_Pi-Star`, `[Info]Name=U_Pi-Star`.
  - mmdvmhost: `[General]Callsign=U`.
  - ysf2dmr, ysf2nxdn, ysf2p25: `[aprs.fi]Password=aprspass(U)`, `[aprs.fi]Description=U_Pi-Star`, `[YSF Network]Callsign=U`, `[Info]Description=U_Pi-Star`.
  - dmr2ysf `[YSF Network]Callsign=U`; p25gateway `[General]Callsign=U`.
  - nxdngateway: `[aprs.fi]Description=U_Pi-Star`, `[aprs.fi]Password=aprspass(U)`, `[General]Callsign=U`.
  - m17gateway `[General]Callsign=U` (only if the key exists); dgidgateway (if loaded) `[General]Callsign=U`, `[Info]Description=U_Pi-Star`.
  - If `$configPistarRelease['Pi-Star']['Version'] >= "4.1.4"` (**string** comparison):
    - `/etc/aprsgateway` `Callsign=U`, `Password=aprspass(U)`; `/etc/ircddbgateway` `aprsPort=8673`.
    - The `aprsPassword=` line is deleted from `/etc/ircddbgateway` immediately: read the file, `preg_replace('/^aprsPassword=[^\r\n]*\r?\n/m','')`, write it to `tempnam('/tmp','pistar-cfg-')`, then `sudo install -m 644 -o root -g root <tmp> /etc/ircddbgateway` (l.1214-1229).
    - **`APRSGatewayEnable` is processed here** (l.1230-1233): `ON` → `/etc/aprsgateway Enabled=1`, `OFF` → `Enabled=0`.
  - If `$configs['aprsPassword']` is still set (the version gate did not unset it): `/etc/ircddbgateway aprsPassword=aprspass(U)`.
  - mmdvmhost `[FM]Callsign` is set from U only when the `[FM]` section is being created (l.2859-2866).
  - The frequency handlers (5.3 `confFREQ*`) also use the callsign, but through `escapeshellcmd($_POST['confCallsign'])` (not character-stripped) for RPT1/RPT2, `repeaterCall1`, and (if `urlAuto=auto`) the QRZ URL.

#### `dmrId`, text (l.4293-4297)
- Visible: `/etc/dstar-radio.mmdvmhost` exists **and** (`[DMR]Enable == 1` or `[P25]Enable == 1` or `[System Fusion]Enable == 1`), from the file at page load.
- Label `CCS7/DMR ID:` (`$lang['dmr_id']`). Tooltip `<b>CCS7/DMR ID</b>Enter your CCS7 / DMR ID here`
- `size="13" maxlength="9"`, value `[General]Id` if set, else `[DMR]Id`. Colspan 2.
- Save (l.1468-1479), if non-empty: digits only, then **`substr(..., 0, 7)`** (truncated to 7 digits even though maxlength is 9). Writes mmdvmhost `[General]Id` and `[DMR]Id`; ysfgateway `[General]Id`; dmrgateway `[XLX Network]Id`; dmr2ysf `[DMR Network]Id`; dmr2nxdn `[DMR Network]Id`; dgidgateway `[General]Id` (if loaded). Later handlers in other sections (`dmrExtendedId`, `bmExtendedId`, `dmrPlusExtendedId`) append suffixes to `[General]Id` (l.1481-1497, out of scope).

#### `nxdnId`, text (l.4298-4302)
- Visible: mmdvmhost marker && `[NXDN]Enable == 1`.
- Label `NXDN ID:`. Tooltip `<b>NXDN ID</b>Enter your NXDN ID here`
- `size="13" maxlength="5"`, value `[NXDN]Id` if set, else empty.
- Save (l.1505-1510): digits only → mmdvmhost `[NXDN]Id`. If `> 65535` the key is **unset** (removed from the file).

#### Frequency rows (l.4303-4320)
Mode is chosen by `$configmmdvm['Info']['TXFrequency'] === $configmmdvm['Info']['RXFrequency']`.

Display format for every frequency field: `number_format(round(Hz/1000000, 6), 6, '.', '')` followed by the text `MHz`. For example `438800000` → `438.800000`.

- **Simplex** (RX === TX): one row.
  - `confFREQ`, text, `id="confFREQ"`, `onkeyup="checkFrequency(); return false;"`, `size="13" maxlength="12"`, value from `[Info]RXFrequency`.
  - Label `Radio Frequency:`. Tooltip `<b>Radio Frequency</b>This is the Frequency your<br />Pi-Star is on`.
- **Duplex**: two rows.
  - `confFREQrx` (`id="confFREQrx"`), label `Radio Frequency RX:`, tooltip `<b>Radio Frequency</b>This is the Frequency your<br />repeater will listen on`, value `[Info]RXFrequency`.
  - `confFREQtx` (`id="confFREQtx"`), label `Radio Frequency TX:`, tooltip `<b>Radio Frequency</b>This is the Frequency your<br />repeater will transmit on`, value `[Info]TXFrequency`.
  - Both have the same size, maxlength and onkeyup as above.

`checkFrequency()` (functions.js l.41-104) is client-only colouring. It sets the background to `#ff6666` (bad), then `#66cc66` (good) if the value is in [144,148], [220,225], [420,450], [842,950] or [1240,1300] MHz, then back to bad if it is in [145.8,146], [435,438] or [1260,1270] (satellite sub-bands). It does not block submission.

Duplex save (l.922-1031), only if both `confFREQtx` and `confFREQrx` are non-empty:
- `tx/rx = preg_replace('/[^0-9\.]/','',v)`; `F = str_pad(str_replace(".", "", number_format(v, 6, ".", "")), 9, "0")`. This gives Hz as a string for MHz ≥ 100. Below 100 MHz it right-pads to 9 digits, for example 70 MHz → `700000000`, which is a defect.
- `ircFreq = number_format(tx, 5, ".", "")`; `offset = number_format((rxHz - txHz)/1000000, 4, '.', '')`.
- `/etc/ircddbgateway`: `frequency1=ircFreq`, `gatewayType=0`, `offset1=offset`. `/etc/dstarrepeater`: `dvapFrequency=RX`, `dvmegaRXFrequency=RX`, `dvmegaTXFrequency=TX`, `mode=0`.
- mmdvmhost, dmrgateway, ysfgateway `[Info]RXFrequency=RX`, `TXFrequency=TX`; ysfgateway `[General]Suffix=RPT`.
- ysf2dmr `[Info]RXFrequency=RX` and **`TXFrequency=RX`** (l.953, copy defect); `[YSF Network]Suffix=RPT`.
- ysf2nxdn and ysf2p25 `[Info]RX/TX`, `[YSF Network]Suffix=RPT`; dgidgateway (if loaded) `[Info]RX/TX`, `[General]Suffix=RPT`; dmr2ysf `[YSF Network]Suffix=RPT`; nxdngateway `[Info]RX/TX`, `[General]Suffix=RPT`.
- m17gateway: `[Info]RX/TX` and `[General]Suffix=R`, each only if the key exists.
- D-Star module / RPT1 (l.972-1030):
  - If `confDStarModuleSuffix` (a field from the D-Star section) is empty, the module is chosen by TX Hz: 1240-1300 MHz → `A` (timeserver `sendA=1`); 420-450 → `B` (`sendB=1`); 218-226 → `A` (`sendA=1`); 144-148 → `C` (`sendC=1`). Otherwise the suffix is uppercased, and if it matches `/\A[A-Z]\z/` then `send<L>=1`.
  - `RPT1 = strtoupper(str_pad(escapeshellcmd(callsign), 7, " ") . L)`; `RPT2 = strtoupper(str_pad(callsign, 7, " ") . "G")`.
  - `/etc/dstarrepeater` `callsign=RPT1`, `gateway=RPT2`, `beaconText=RPT1`; `/etc/ircddbgateway` `repeaterBand1=L`, `repeaterCall1=strtoupper(escapeshellcmd(callsign))`; mmdvmhost `[D-Star]Module=L`.
  - With an out-of-band TX frequency (for example 900 MHz) and no suffix, `$confRPT1` and `$confIRCrepeaterBand1` stay undefined, so empty values are staged. This is observed behaviour, not intended.
- Before either frequency handler, `/etc/timeserver sendA..sendE=0` is always staged (l.881-886).

Simplex save (l.1033-1141), if `confFREQ` is non-empty: the same pipeline with RX = TX = F, `offset1=0.0000`, `dstarrepeater mode=1`, `ircddbgateway gatewayType=1`, suffixes `ND` (ysfgateway `[General]`, ysf2* `[YSF Network]`, dgid `[General]`, dmr2ysf `[YSF Network]`, nxdngateway `[General]`) and m17gateway Suffix `H`. The band logic and RPT1/RPT2 are identical, and the 2 m band sets `sendC=1` (l.1106).

`confHardware` is read inside both frequency handlers (`$confHardware = escapeshellcmd(...)`) but not otherwise used there.

#### `confLatitude`, text (l.4321-4324)
- Label `Latitude:` (`$lang['lattitude']`). Tooltip `<b>Gateway Latitude</b>This is the latitude where the gateway is located (positive number for North, negative number for South) - Set to 0 to hide your hotspot location`
- `id="confLatitude" size="13" maxlength="9"`, value `$configs['latitude']` (ircddbgateway). After the input: `degrees (positive value for North, negative for South)`.
- Save (l.734-748), if non-empty: `v = preg_replace('/[^0-9\.\-]/','',...)`. Writes ircddbgateway `latitude=v` and `latitude1=v`; `[Info]Latitude=v` in mmdvmhost, ysfgateway, ysf2dmr, ysf2nxdn, ysf2p25, dgidgateway (if loaded), dmrgateway and nxdngateway; m17gateway only if the key exists.
- **`"0"` is `empty()` in PHP, so entering `0` (which the tooltip recommends to hide the location) is not saved.** `0.0` is saved.

#### `confLongitude`, text (l.4325-4328)
- Label `Longitude:`. Tooltip `<b>Gateway Longitude</b>This is the longitude where the gateway is located (positive number for East, negative number for West) - Set to 0 to hide your hotspot location`
- `id="confLongitude" size="13" maxlength="9"`, value `$configs['longitude']`. Suffix text `degrees (positive value for East, negative for West)`.
- Save (l.750-764): the same as latitude, with keys `longitude` / `longitude1` and `[Info]Longitude`. The same `"0"` quirk applies.

#### `confDesc1` (Town), text (l.4329-4332)
- Label `Town:`. Tooltip `<b>Gateway Town</b>The town where the gateway is located`
- `size="30" maxlength="30"`, value `$configs['description1']`. Colspan 2.
- Save (l.766-778): `v = preg_replace('/[^A-Za-z0-9\.\s\,\-]/','',...)`. ircddbgateway `description1=v`, `description1_1=v`. The quoted value `"v"` goes to mmdvmhost `[Info]Location`, dmrgateway `[Info]Location`, ysf2dmr, ysf2nxdn and ysf2p25 `[Info]Location`, nxdngateway `[Info]Name`, and m17gateway `[Info]Name` (if the key exists).

#### `confDesc2` (Country), text (l.4333-4336)
- Label `Country:`. Tooltip `<b>Gateway Country</b>The country where the gateway is located`
- `size="30" maxlength="30"`, value `$configs['description2']`.
- Save (l.780-791): the same sanitising. ircddbgateway `description2=v`, `description1_2=v`. The quoted value `"v"` goes to mmdvmhost, dmrgateway, ysfgateway and nxdngateway `[Info]Description`, m17gateway (if the key exists) and dgidgateway (if loaded) `[Info]Description`.

#### `confURL` + `urlAuto` (l.4337-4343). This row uses 3 cells (no colspan).
- Label `URL:`. Tooltip `<b>Gateway URL</b>The URL used to access this dashboard`
- Cell 2: `<input type="text" name="confURL" size="30" maxlength="30" value="$configs['url']"/>`.
- Cell 3 `<td width="300">`: radio `urlAuto` `value="auto"` text `Auto`, radio `urlAuto` `value="man"` text `Manual`.
  - `auto` is checked if `strpos($configs['url'], 'www.qrz.com/db/'.$configmmdvm['General']['Callsign']) !== FALSE`.
  - `man` is checked if that `strpos(...) == FALSE` (loose; effectively the complement).
- Save (l.793-814), **only if `confURL` is non-empty**. If the text box is empty, nothing is saved even when Auto is selected.
  - `newURL = strtolower(preg_replace('/[^A-Za-z0-9\.\s\,\-\/\:]/','',confURL))`.
  - If `urlAuto == 'auto'`: `url = 'https://www.qrz.com/db/' . strtoupper(escapeshellcmd($_POST['confCallsign']))`, else `url = newURL`.
  - Writes ircddbgateway `url=url`; `[Info]URL=url` in mmdvmhost, ysf2dmr, ysf2nxdn, ysf2p25 and dmrgateway.

#### `confHardware`, select (l.4344-4407)
- Visible: `/etc/dstar-radio.dstarrepeater` **or** `/etc/dstar-radio.mmdvmhost` exists.
- Label `Radio/Modem Type:` (`$lang['radio_type']`). Tooltip `<b>Radio/Modem</b>What kind of radio or modem hardware do you have?`
- Selected option: the one whose value `=== $configModem['Modem']['Hardware']` (from the marker file `[Modem] Hardware=`). The first option `value=""` text `--` is selected if Hardware is unset or falsy. The list has more than 30 options, so it becomes a NiceSelect (searchable).
- Options in exact order (value → text). The conditional ones are noted.

| # | value | text | condition |
|---|---|---|---|
| 0 | `` | `--` | |
| 1 | `icomTerminalAuto` | `Icom Radio in Terminal Mode (DStarRepeater Only)` | only if `/dev/icom_ta` exists |
| 2 | `idrp2c` | `Icom Repeater Controller ID-RP2C (DStarRepeater Only)` | |
| 3 | `dvmpis` | `DV-Mega Raspberry Pi Hat (GPIO) - Single Band (70cm)` | |
| 4 | `dvmpid` | `DV-Mega Raspberry Pi Hat (GPIO) - Dual Band` | |
| 5 | `dvmuadu` | `DV-Mega on Arduino (USB - /dev/ttyUSB0) - Dual Band` | |
| 6 | `dvmuada` | `DV-Mega on Arduino (USB - /dev/ttyACM0) - Dual Band` | |
| 7 | `dvmuagmsku` | `DV-Mega on Arduino (USB - /dev/ttyUSB0) - GMSK Modem` | |
| 8 | `dvmuagmska` | `DV-Mega on Arduino (USB - /dev/ttyACM0) - GMSK Modem` | |
| 9 | `dvmbss` | `DV-Mega on Bluestack (USB) - Single Band (70cm)` | |
| 10 | `dvmbsd` | `DV-Mega on Bluestack (USB) - Dual Band` | |
| 11 | `dvmpicast` | `DV-Mega Cast Base Radio (Main Unit)` | only if `/dev/ttyS2` exists |
| 12 | `dvmpicasths` | `DV-Mega Cast Hotspot - Single Band (70cm)` | only if `/dev/ttyS2` exists |
| 13 | `dvmpicasthd` | `DV-Mega Cast Hotspot - Dual Band (2m/70cm)` | only if `/dev/ttyS2` exists |
| 14 | `gmsk_modem` | `GMSK Modem (USB DStarRepeater Only)` | |
| 15 | `dvrptr1` | `DV-RPTR V1 (USB)` | |
| 16 | `dvrptr2` | `DV-RPTR V2 (USB)` | |
| 17 | `dvrptr3` | `DV-RPTR V3 (USB)` | |
| 18 | `dvap` | `DVAP (USB)` | |
| 19 | `zum` | `MMDVM / MMDVM_HS / Teensy / ZUM (USB)` | |
| 20 | `stm32dvm` | `STM32-DVM / MMDVM_HS - Raspberry Pi Hat (GPIO)` | |
| 21 | `stm32usb` | `STM32-DVM (USB)` | |
| 22 | `zumspotlibre` | `ZUMspot - Libre (USB)` | |
| 23 | `zumspotusb` | `ZUMspot - USB Stick` | |
| 24 | `zumspotgpio` | `ZUMspot - Single Band Raspberry Pi Hat (GPIO)` | |
| 25 | `zumspotdualgpio` | `ZUMspot - Dual Band Raspberry Pi Hat (GPIO)` | |
| 26 | `zumspotduplexgpio` | `ZUMspot - Duplex Raspberry Pi Hat (GPIO)` | |
| 27 | `zumradiopigpio` | `ZUM Radio-MMDVM for Pi (GPIO)` | |
| 28 | `zumradiopiusb` | `ZUM Radio-MMDVM-Nucleo (USB)` | |
| 29 | `mnnano-spot` | `MicroNode Nano-Spot (Built In)` | |
| 30 | `mnnano-teensy` | `MicroNode Teensy (USB)` | |
| 31 | `f4mgpio` | `MMDVM F4M-GPIO (GPIO)` | |
| 32 | `f4mf7m` | `MMDVM F4M/F7M Duplex (F0DEI) for USB` | |
| 33 | `mmdvmhsdualbandgpio` | `MMDVM_HS_Dual_Band for Pi (GPIO)` | |
| 34 | `mmdvmhshat` | `MMDVM_HS_Hat (DB9MAT & DF2ET) for Pi (GPIO)` | |
| 35 | `mmdvmhsdualhatgpio` | `MMDVM_HS_Duplex_Hat (DB9MAT, DF2ET & DO7EN) for Pi (GPIO)` | |
| 36 | `mmdvmhsdualhatusb` | `MMDVM_HS_Duplex_Hat (DB9MAT, DF2ET & DO7EN) for Pi (USB)` | |
| 37 | `mmdvmhshatambe` | `MMDVM_HS_AMBE (D2RG HS_AMBE) for Pi (GPIO)` | |
| 38 | `mmdvmrpthat` | `MMDVM_RPT_Duplex_Hat (DB9MAT, DF2ET & F0DEI) for Pi (GPIO)` | |
| 39 | `mmdvmmdohat` | `MMDVM_HS_MDO Hat (BG3MDO) for Pi (GPIO)` | |
| 40 | `mmdvmvyehat` | `MMDVM_HS_NPi Hat (VR2VYE) for Nano Pi (GPIO)` | |
| 41 | `mmdvmvyehatdual` | `MMDVM_HS_Hat_Duplex Hat (VR2VYE) for Pi (GPIO)` | |
| 42 | `genesyshat` | `Genesis - HHDVM_HS_Hat for Pi (GPIO)` | |
| 43 | `genesysdualhat` | `Genesis Duplex - HHDVM_HS_Hat_Dual for Pi (GPIO)` | |
| 44 | `lshshatgpio` | `LoneStar - MMDVM_HS_Hat for Pi (GPIO)` | |
| 45 | `lshsdualhatgpio` | `LoneStar - MMDVM_HS_Duplex_Hat for Pi (GPIO)` | |
| 46 | `lsusb` | `LoneStar - USB Stick` | |
| 47 | `sbhsdualbandgpio` | `SkyBridge - MMDVM_HS_Dual_Band for Pi (GPIO)` | |
| 48 | `nanodv` | `MMDVM_NANO_DV (BG4TGO) for NanoPi AIR (GPIO)` | |
| 49 | `nanodvusb` | `MMDVM_NANO_DV (BG4TGO) for NanoPi AIR (USB)` | |
| 50 | `opengd77` | `OpenGD77 DMR hotspot (USB)` | |
| 51 | `rptrbldrv1v2` | `Repeater Builder v1/v2 Simplex (GPIO)` | |
| 52 | `rptrbldrv1v2dup` | `Repeater Builder v1/v2 Duplex (GPIO)` | |
| 53 | `rptrbldrv3v4v5` | `Repeater Builder v3/v4/v5 Simplex 115200 baud (GPIO)` | |
| 54 | `rptrbldrv3v4v5dup` | `Repeater Builder v3/v4/v5 Duplex 115200 baud (GPIO)` | |
| 55 | `rptrbldrv3v4v5hs` | `Repeater Builder v3/v4/v5 Simplex 460800 baud (GPIO)` | |
| 56 | `rptrbldrv3v4v5hsdup` | `Repeater Builder v3/v4/v5 Duplex 460800 baud (GPIO)` | |

(`&` is written as `&amp;` in the HTML.)

Save (l.1781-2412), if non-empty. `$hw = escapeshellcmd(value)`.

Common steps for every non-empty value:
- `$configModem['Modem']['Hardware'] = $hw`. This is written to the marker file at the end (7.4).
- `/etc/dstarrepeater rpt1Validation=1`; `/etc/ircddbgateway repeaterAddress1=127.0.0.1`, `repeaterPort1=20011`.
- Default timer delay command: `sudo sed -i "/OnStartupSec=/c\OnStartupSec=30" /lib/systemd/system/dstarrepeater.timer` and the same for `mmdvmhost.timer`. Both are run at l.2408-2409 after the per-model block. `dvmbss` and `dvmbsd` use `60`.
- Unknown values (not in the table below) get only these common steps.

Per-model effects. "dsr" = `/etc/dstarrepeater` (staged flat), "irc" = `/etc/ircddbgateway`, "mm" = mmdvmhost array. Every model except `idrp2c` and `icomTerminalAuto` sets irc `repeaterType1=0`. Every model sets mm `[Modem]Protocol=uart` and `[Modem]UARTPort=<[Modem]Port after the change>`. "D0" means `mm [General]Duplex=0` and `[DMR Network]Slot1=0`. "D1" means `[General]Duplex=1` with Slot1 unchanged. "FM0" means `[FM]Enable=0` if the key exists.

| value | dsr `modemType` | other dsr keys | mm `[Modem]Port` | Duplex | other |
|---|---|---|---|---|---|
| `idrp2c` | (none) | | unchanged | unchanged | irc `repeaterType1=1`, `repeaterAddress1=172.16.0.1`, `repeaterPort1=20000`; if `grep "eth0:1" /etc/network/interfaces \| wc -l` is `0`, append `auto eth0:1 / allow-hotplug eth0:1 / iface eth0:1 inet static / address 172.16.0.20 / netmask 255.255.255.0` to `/etc/network/interfaces` via `sudo sed -i "$ a\ ..."` (l.1797-1800); no repeaterType1=0 |
| `icomTerminalAuto` | `Icom Access Point/Terminal Mode` | `icomPort=/dev/icom_ta`, `rpt1Validation=0` | unchanged | unchanged | no repeaterType1 |
| `dvmpis` | `DVMEGA` | `dvmegaPort=/dev/ttyAMA0`, `dvmegaVariant=2` | `/dev/ttyAMA0` | D0 | FM0 |
| `dvmpid` | `DVMEGA` | `dvmegaPort=/dev/ttyAMA0`, `dvmegaVariant=3` | `/dev/ttyAMA0` | D0 | FM0 |
| `dvmuadu` | `DVMEGA` | `dvmegaPort=/dev/ttyUSB0`, `dvmegaVariant=3` | `/dev/ttyUSB0` | D0 | FM0 |
| `dvmuada` | `DVMEGA` | `dvmegaPort=/dev/ttyACM0`, `dvmegaVariant=3` | `/dev/ttyACM0` | D0 | FM0 |
| `dvmbss` | `DVMEGA` | `dvmegaPort=/dev/ttyUSB0`, `dvmegaVariant=2` | `/dev/ttyUSB0` | D0 | FM0; timers `OnStartupSec=60` |
| `dvmbsd` | `DVMEGA` | `dvmegaPort=/dev/ttyUSB0`, `dvmegaVariant=3` | `/dev/ttyUSB0` | D0 | FM0; timers 60 |
| `dvmuagmsku` | `DVMEGA` | `dvmegaPort=/dev/ttyUSB0`, `dvmegaVariant=0` | `/dev/ttyUSB0` | unchanged | FM0 |
| `dvmuagmska` | `DVMEGA` | `dvmegaPort=/dev/ttyACM0`, `dvmegaVariant=0` | `/dev/ttyACM0` | unchanged | FM0 |
| `dvrptr1` | `DV-RPTR V1` | `dvrptr1Port=/dev/ttyACM0` | `/dev/ttyACM0` | unchanged | FM0 |
| `dvrptr2` | `DV-RPTR V2` | `dvrptr1Port=/dev/ttyACM0` | `/dev/ttyACM0` | unchanged | FM0 |
| `dvrptr3` | `DV-RPTR V3` | `dvrptr1Port=/dev/ttyACM0` | `/dev/ttyACM0` | unchanged | FM0 |
| `gmsk_modem` | `GMSK Modem` | | unchanged | unchanged | FM0 |
| `dvap` | `DVAP` | | `/dev/ttyUSB0` | unchanged | FM0 |
| `zumspotlibre` | `MMDVM` | | `/dev/ttyACM0` | D0 | FM0 |
| `zumspotusb` | `MMDVM` | | `/dev/ttyACM0` | D0 | FM0 |
| `lsusb` | `MMDVM` | | `/dev/ttyACM0` | D0 | FM0 |
| `zumspotgpio` | `MMDVM` | | `/dev/ttyAMA0` | D0 | FM0 |
| `zumspotdualgpio` | `MMDVM` | | `/dev/ttyAMA0` | D0 | FM0 |
| `zumspotduplexgpio` | `MMDVM` | | `/dev/ttyAMA0` | D1 | FM0 |
| `zumradiopiusb` | `MMDVM` | | `/dev/ttyACM0` | D0 | |
| `zumradiopigpio` | `MMDVM` | `mmdvmPort=/dev/ttyAMA0` | `/dev/ttyAMA0` | unchanged | |
| `zum` | `MMDVM` | `mmdvmPort=/dev/ttyACM0` | `/dev/ttyACM0` | unchanged | |
| `rptrbldrv1v2` | `MMDVM` | `mmdvmPort=/dev/ttyAMA0` | `/dev/ttyAMA0` | D0 | `[Modem]UARTSpeed=115200`; `[NXDN]Enable=0`, `[M17]Enable=0` (if set); FM0 |
| `rptrbldrv1v2dup` | `MMDVM` | `mmdvmPort=/dev/ttyAMA0` | `/dev/ttyAMA0` | D1 | UARTSpeed 115200; NXDN/M17/FM Enable=0 (if set) |
| `rptrbldrv3v4v5` | `MMDVM` | `mmdvmPort=/dev/ttyAMA0` | `/dev/ttyAMA0` | D0 | UARTSpeed 115200 |
| `rptrbldrv3v4v5dup` | `MMDVM` | `mmdvmPort=/dev/ttyAMA0` | `/dev/ttyAMA0` | D1 | UARTSpeed 115200 |
| `rptrbldrv3v4v5hs` | `MMDVM` | `mmdvmPort=/dev/ttyAMA0` | `/dev/ttyAMA0` | D0 | UARTSpeed 460800 |
| `rptrbldrv3v4v5hsdup` | `MMDVM` | `mmdvmPort=/dev/ttyAMA0` | `/dev/ttyAMA0` | D1 | UARTSpeed 460800 |
| `stm32dvm` | `MMDVM` | `mmdvmPort=/dev/ttyAMA0` | `/dev/ttyAMA0` | unchanged | |
| `stm32usb` | `MMDVM` | `mmdvmPort=/dev/ttyUSB0` | `/dev/ttyUSB0` | unchanged | |
| `f4mgpio` | `MMDVM` | `mmdvmPort=/dev/ttyAMA0` | `/dev/ttyAMA0` | unchanged | |
| `f4mf7m` | `MMDVM` | | `/dev/ttyUSB0` | D1 | |
| `mmdvmhshat` | `MMDVM` | | `/dev/ttyAMA0` | D0 | FM0 |
| `lshshatgpio` | `MMDVM` | | `/dev/ttyAMA0` | D0 | |
| `mmdvmhshatambe` | `MMDVM` | | `/dev/ttySC0` | D0 | FM0 |
| `mmdvmhsdualbandgpio` | `MMDVM` | | `/dev/ttyAMA0` | D0 | FM0 |
| `sbhsdualbandgpio` | `MMDVM` | | `/dev/ttyAMA0` | D0 | FM0 |
| `genesyshat` | `MMDVM` | | `/dev/ttyAMA0` | D0 | FM0 |
| `genesysdualhat` | `MMDVM` | | `/dev/ttyAMA0` | D1 | FM0 |
| `mmdvmhsdualhatgpio` | `MMDVM` | | `/dev/ttyAMA0` | D1 | FM0 |
| `lshsdualhatgpio` | `MMDVM` | | `/dev/ttyAMA0` | D1 | FM0 |
| `mmdvmhsdualhatusb` | `MMDVM` | | `/dev/ttyACM0` | D1 | FM0 |
| `mmdvmrpthat` | `MMDVM` | | `/dev/ttyAMA0` | D1 | |
| `mmdvmmdohat` | `MMDVM` | | `/dev/ttyAMA0` | D0 | FM0 |
| `mmdvmvyehat` | `MMDVM` | | `/dev/ttyAMA0` | D0 | FM0 |
| `mmdvmvyehatdual` | `MMDVM` | | `/dev/ttyAMA0` | D1 | FM0 |
| `mnnano-spot` | `MMDVM` | | `/dev/ttyAMA0` | D0 | FM0 |
| `mnnano-teensy` | `MMDVM` | `mmdvmPort=/dev/ttyUSB0` | `/dev/ttyUSB0` | D0 | |
| `nanodv` | `MMDVM` | | `/dev/ttyAMA0` | D0 | FM0 |
| `nanodvusb` | `MMDVM` | | `/dev/ttyACM0` | D0 | |
| `dvmpicast` | `DVMEGA` | `dvmegaPort=/dev/ttyAMA0`, `dvmegaVariant=2` | `/dev/ttyAMA0` | D0 | FM0 |
| `dvmpicasths` | `DVMEGA` | `dvmegaPort=/dev/ttyS2`, `dvmegaVariant=3` | `/dev/ttyS2` | D0 | FM0 |
| `dvmpicasthd` | `DVMEGA` | `dvmegaPort=/dev/ttyS2`, `dvmegaVariant=3` | `/dev/ttyS2` | D0 | FM0 |
| `opengd77` | `MMDVM` | | `/dev/ttyACM0` | D0 | FM0 |

For `idrp2c` and `icomTerminalAuto`, `UARTPort` is set to the unchanged `[Modem]Port`.

Because the hardware handler runs after the `trxMode` handler, a D0 model forces `Duplex=0` / `Slot1=0` even when DUPLEX was selected, and a D1 model forces `Duplex=1`.

#### `nodeMode`, radio (l.4408-4413)
- Label `Node Type:` (`$lang['node_type']`). Tooltip `<b>Node Lock</b>Set the public/private node type. &quot;Private&quot; limits access to your system to your ID/Callsign only, this may be a licence requirement for your country and helps prevent network loops.`
- Colspan 2: radio `value="prv"` text `Private`, radio `value="pub"` text `Public`.
- `prv` is checked if `[DMR]SelfOnly == 1`. `pub` is checked if `[DMR]SelfOnly == 0` (loose compare, so a missing key or `""` also checks Public).
- Save (l.2632-2650):
  - `prv`: mmdvmhost `SelfOnly=1` in `[DMR]`, `[D-Star]`, `[System Fusion]`, `[P25]` and `[NXDN]` (M17 is not touched); `/etc/dstarrepeater restriction=1`.
  - `pub`: the same keys set to `0`; `restriction=0`.

#### `confDMRWhiteList`, text (l.4414-4419)
- Visible: mmdvmhost marker && `[DMR]Enable == 1` && `[DMR]SelfOnly == 0`.
- Label `DMR Access List:`. Tooltip `<b>DMR IDs</b>Set the DMR IDs here that should have access to your hotspot, expected comma seperated list.`
- `size="30" maxlength="50"`, value `[DMR]WhiteList` if set. Colspan 2.
- Save (l.2626-2630): **`[DMR]WhiteList` is always unset first**, then set to `escapeshellcmd(preg_replace('/[^0-9\,]/','',v))` only if non-empty. When the row is hidden (Private mode, DMR off, or DStarRepeater mode), any Apply therefore **deletes** the existing WhiteList.

#### `APRSGatewayEnable`, toggle (l.4420-4428)
- Visible: `file_exists('/etc/aprsgateway')`.
- Label `APRS Host Enable:` (`$lang['aprs_host']` + ` Enable`). Tooltip `<b>APRS Host Enable</b>Enabling this feature will make your location public.`
- Toggle in `<td align="left" colspan="2">`: name `APRSGatewayEnable`, id `toggle-aprsgateway`, aria-label `Enable APRS Position Reporting`, function `toggleAPRSGatewayCheckbox()`. Checked if `$configaprsgw['Enabled']['Enabled'] == 1`. Note that `/etc/aprsgateway` is parsed as INI with section `[Enabled]` key `Enabled`.
- Markup defect: this row's `<tr>` is never closed (no `</tr>` is echoed, l.4421-4428).
- Hidden default `OFF` at l.4279.
- Save: only inside the `confCallsign` handler and only if Pi-Star version `>= "4.1.4"` (l.1230-1233). `/etc/aprsgateway` (staged flat, column-0 `Enabled=` line): `ON`→`Enabled=1`, `OFF`→`Enabled=0`. With an empty callsign the toggle is ignored.

#### `selectedAPRSHost`, select (l.4429-4445)
- Label `APRS Host:`. Tooltip `<b>APRS Host</b>Set your prefered APRS host here.`
- `<td colspan="2" style="text-align: left;">`.
- Options: read `/usr/local/etc/APRSHosts.txt` line by line with `fgets`, `preg_split('/:/', line)`. Take column 0 and keep the line if column 0 contains no `;` and is not `''`. Each option has value = text = column 0, untrimmed. A line without `:` keeps its newline, which the browser strips. Order is file order.
- Selected: `$configs['aprsHostname'] == col0` (from ircddbgateway).
- Save (l.816-872), if non-empty:
  - Build the whitelist from the same file with `trim(col0)` and require `in_array($post, $valid, true)`. Otherwise log `rejected selectedAPRSHost=...` and write nothing.
  - On success: ircddbgateway `aprsHostname=host`; `[aprs.fi]Server=host` in ysfgateway, ysf2dmr, ysf2nxdn and ysf2p25; `[aprs.fi]Enable="0"` in ysf2dmr, ysf2nxdn and ysf2p25; if version `>= "4.1.4"`, `/etc/aprsgateway Server=host`.

#### `systemTimezone`, select (l.4446-4459)
- Label `System Time Zone:` (`$lang['timezone']`). Tooltip `<b>System TimeZone</b>Set the system timezone`
- Options: `exec('timedatectl list-timezones')`, with `UTC` appended if missing. Value = text = zone. Selected: equals the first line of `cat /etc/timezone`. NiceSelect applies.
- Save (l.3862-3935). This runs **after the services have been started**:
  - If non-empty and `in_array($tz, DateTimeZone::listIdentifiers(), true)`:
    1. `sudo timedatectl set-timezone '<tz>'`.
    2. `/etc/timezone` ← `"<tz>\n"` via `tempnam('/tmp','pistar_tz_')` + `sudo install -m 644 -o root -g root`.
    3. `/var/www/dashboard/config/config.php`: `preg_replace('/date_default_timezone_set\s*\([^)]*\)\s*;/', "date_default_timezone_set('<tz>');", ..., 1)`. Installed via a tmp file + `sudo install -m 644 -o root -g root` only if the content changed. A regex miss or error is logged.
  - Non-whitelisted values are ignored silently.

#### `dashboardLanguage`, select (l.4460-4482)
- Visible: `is_dir('./lang')`, relative to `admin/`. `admin/lang` is a symlink to `../lang/`.
- Label `Dashboard Language:` (`$lang['dash_lang']`). Tooltip `<b>Dashboard Language</b>Set the language for the dashboard.`
- Options: `readdir` of `./lang`, sorted (`sort()` on every iteration). Skip `index.php`, `.`, `..` and `''`. `substr($file, 0, -4)` strips `.php`, so options are value = text = basename (for example `english_uk`). No other filter is applied, so any non-PHP file would appear truncated.
- Selected: equals `$pistarLanguage`.
- The row is closed with `</select></td></tr>`.
- Save (l.651-674): the value must match `/\A[A-Za-z][A-Za-z0-9_]+\z/` **and** `DOCUMENT_ROOT/lang/<v>.php` must exist. Then it is staged as a PHP string: `/var/www/dashboard/config/language.php` line `$pistarLanguage='<v>';`. Otherwise it is logged and ignored.

#### Section button (l.4486)
`Apply Changes` → `submitform()`.

---

## 6. Field inventory for this scope (form order)

1. `factoryReset` (hidden, separate form) + `csrf_token`
2. `controllerSoft` (radio)
3. `trxMode` (radio)
4. 12 hidden defaults `MMDVMMode*=OFF` (mmdvmhost marker only)
5. `MMDVMModeDMR`, `dmrRfHangTime`, `dmrNetHangTime`
6. `MMDVMModeDSTAR`, `dstarRfHangTime`, `dstarNetHangTime`
7. `MMDVMModeM17`, `m17RfHangTime`, `m17NetHangTime`
8. `MMDVMModeFUSION`, `ysfRfHangTime`, `ysfNetHangTime`
9. `MMDVMModeP25`, `p25RfHangTime`, `p25NetHangTime`
10. `MMDVMModeNXDN`, `nxdnRfHangTime`, `nxdnNetHangTime`
11. `MMDVMModeYSF2DMR`; `MMDVMModeYSF2NXDN` (if /etc/ysf2nxdn); `MMDVMModeYSF2P25` (if /etc/ysf2p25); `MMDVMModeDMR2YSF` (if /etc/dmr2ysf); `MMDVMModeDMR2NXDN` (if /etc/dmr2nxdn); `MMDVMModePOCSAG` (if /etc/dapnetgateway)
12. `mmdvmDisplayType`, `mmdvmDisplayPort`, `mmdvmNextionDisplayType`
13. hidden `APRSGatewayEnable=OFF`
14. `confHostame`, `confCallsign`, `dmrId`*, `nxdnId`*, `confFREQ` | (`confFREQrx`, `confFREQtx`), `confLatitude`, `confLongitude`, `confDesc1`, `confDesc2`, `confURL`, `urlAuto`, `confHardware`*, `nodeMode`, `confDMRWhiteList`*, `APRSGatewayEnable` toggle*, `selectedAPRSHost`, `systemTimezone`, `dashboardLanguage`*

(* = conditional.) That is 51 distinct POST names in total (52 including `csrf_token`).

---

## 7. Overall Apply flow (POST handler, l.351-3945)

### 7.1 Envelope and early exits

1. Before any output: `csrf_verify()` (l.63; a token mismatch gives 403 + exit) and `pistar_warnings_enforce_redirect()` (l.67; may redirect for the default password).
2. The header and hardware table are printed (sections 1-2).
3. `if (!empty($_POST))` (l.351): `system('sudo mount -o remount,rw /')` (l.359).
4. `adminPassword` branch (l.395-492): `chpasswd` + `htpasswd`, "Working..." page, reload after 5 s, `die()`. Out of scope, but note that it dies **before** any service stop and never remounts read-only.
5. `autoapPsk` branch (l.502-548): stages hostapd.conf, `config_writer_commit(false)`, `die()`. Same caveat.
6. `sudo systemctl stop cron.service` (background).
7. Stop the DV services, in this order, each as `sudo systemctl stop X > /dev/null 2>/dev/null &` (background, not awaited): `dstarrepeater`, `mmdvmhost`, `ircddbgateway`, `timeserver`, `pistar-watchdog`, `pistar-remote`, `ysfgateway`, `ysf2dmr`, `ysf2nxdn`, `ysf2p25`, `nxdn2dmr`, `ysfparrot`, `p25gateway`, `p25parrot`, `nxdngateway`, `nxdnparrot`, `m17gateway`, `dmr2ysf`, `dmr2nxdn`, `dmrgateway`, `dapnetgateway`, `aprsgateway` (all `.service`).
8. Print the `Working...` / `Stopping services and applying your configuration changes...` table; `sleep(1)`.
9. `factoryReset` branch → section 4 (`die()`).
10. Sanity guard (l.620-636): if `count($configmmdvm) <= 18` (fewer than 19 sections parsed from `/etc/mmdvmhost`), print an ERROR table ("Unable to read source configuration file(s)... Please wait a few seconds and retry..."), reload after 5 s, `die()`. Services stay **stopped**, cron stays stopped, and `/` stays rw.

### 7.2 Field handlers (in source order; only the order matters for a re-implementation)

`controllerSoft` (639) → `autoAP` (646, marker `/etc/hostap.off`) → `dashboardLanguage` (662) → `confPassword` (687) → `confDefRef` (706) → `confDefRefAuto` (718) → random `ccsHost` if `ccsEnabled=1` (728) → `confLatitude` (735) → `confLongitude` (751) → `confDesc1` (767) → `confDesc2` (781) → `confURL` (801) → `selectedAPRSHost` (834) → `ircDDBGatewayAnnounceLanguage` (875) → timeserver `sendA..E=0` (882) → POCSAG fields (888-920) → duplex freq (923) → simplex freq (1034) → `confCallsign` (+ `APRSGatewayEnable`) (1144) → ircDDB / P25 / NXDN / M17 / YSF / YSF2x startup fields (1255-1446) → `trxMode` (1449) → `dmrId` (1469) → extended IDs (1482-1497) → `ysf2dmrId` (1500) → `nxdnId` (1506) → DMR master / DMRGateway fields (1513-1715) → hang-time cleanup + hang times (1717-1779) → `confHardware` (1782, runs the two timer `sed`s) → UPnP / firewall (2415-2452) → `confTimeAnnounce` (2455) → `MMDVMMode*` (2461-2590) → display (2593-2619) → `dmrColorCode` (2622) → `confDMRWhiteList` (2627) → `nodeMode` (2633) → `confHostame` (2653, immediate `sed` on /etc/hostname, /etc/hosts, /etc/motd).

Values are written in one of three ways:
- **INI files** (`/etc/mmdvmhost`, `/etc/ysfgateway`, `/etc/dmrgateway`, ...): the in-memory PHP array is mutated and written back in bulk in 7.4.
- **Flat key=value files** (`/etc/ircddbgateway`, `/etc/dstarrepeater`, `/etc/timeserver`, `/etc/aprsgateway`, `/etc/mobilegps`, `/etc/starnetserver`): `config_writer_stage_flat()` queues the edit in `$GLOBALS`, and it is flushed in 7.5.
- **Immediate shell side effects**: marker `touch`/`rm`, timer `sed`, `/etc/network/interfaces` append, hostname `sed`, UPnP `sed`, and the two direct `/etc/ircddbgateway` rewrites (`aprsPassword` delete at l.1214, and the `ircddbEnabled2=0` insert at l.2913 when `ircddbHostname2` and `ircddbEnabled2` are both absent). These rewrites use `tempnam` + `sudo install -m 644 -o root -g root`.

### 7.3 Unconditional normalisation (l.2682-3256)

These run on every Apply, whatever was submitted. They are listed because a native implementation must reproduce them or explicitly drop them:
- **DMRGateway** (2682-2749): missing defaults for `[Info]`, `[XLX Network]` and `[DMR Network 3]`. If `DMRGateway -v` (date, first 8 chars) is `> 20210130`: `Location` keys, `[GPSD]`, `[APRS]` and `[Dynamic TG Control]` sections are added. Values that start without `"` get re-quoted: `[Info]Location`, `[Info]Description`, and `Password` / `Options` of Networks 1-5.
- **MMDVMHost** (2751-2901): about 60 add-if-missing defaults (`[Modem]` levels, `UARTSpeed=115200`, D-Star Ack*, DMR Beacon*/OVCM, `[DMR Network]Type=Direct`, P25/NXDN/M17/AX.25 sections, `[NXDN Id Lookup]`, `[Lock File]`, `[Mobile GPS]`, OLED Rotate/Cast/LogoScreensaver, `[Remote Control]`). `[TFT Serial]Port` is forced to `/dev/ttyAMA0` if present. The Nextion port rules are in 5.2. The `[NextionDriver]` section is created if missing (Port=modem, LogLevel=2, DataFilesPath=/usr/local/etc/, GroupsFile, DMRid* columns, SleepWhenInactive=600, WaitForLan=1, ...) together with `[Transparent Data]Enable=1`, `SendFrameType=1`, and `[Nextion]Port=/dev/ttyNextionDriver`. A full `[FM]` section is created if missing (l.2859-2901).
- ircddbgateway `ircddbEnabled2=0` insert (2903-2931).
- DMR2YSF / YSFGateway / YSF2DMR / YSF2NXDN / YSF2P25 defaults (2933-3026). Note `dmr2ysf [Log]DisplayLevel=0`, `FileLevel=2` are always set, and ysfgateway `[Network]DataPort` / `StatusPort` are always unset.
- DGIdGateway (3027-3037): `[General]LocalPort=<mmdvm [System Fusion Network]GatewayPort>`, `RptPort=<... LocalPort>`, and log settings.
- NXDNGateway clean-up (3039-3075). Only if `/etc/nxdngateway` exists: HostsFile → HostsFile1/HostsFile2, `sudo touch /usr/local/etc/NXDNHostsLocal.txt`, mmdvmhost `[NXDN Network]` Local/Gateway address 127.0.0.1 and ports 14021/14020, and a re-ordering of NXDN `SelfOnly` / `ModeHang`.
- Version-gated migrations that run `DMRGateway -v`, `P25Gateway -v`, `NXDNGateway -v` and `YSFGateway -v` (3077-3175).
- DAPNET defaults + mmdvmhost POCSAG defaults (3177-3202); `[POCSAG Network]ModeHang` is always forced to `5`.
- M17Gateway `Daemon=1` if the key exists (3205). MobileGPS (3207-3255) and `/etc/hostfiles.nodextra` (3257-3265) are out of scope.

### 7.4 Bulk INI write-back (l.3267-3825)

Prints the `Done...` / `Changes applied, starting services...` table.

Serialisation for every INI file: for each section, `"[" . str_replace("_"," ",$section) . "]\n"`, then `"key=value\n"` per entry, then a blank line. Order follows the array (original file order, with new keys appended at the end of their section and new sections at the end of the file). Comments and the original quoting are lost. `parse_ini_file` stripped the quotes, so only keys the code re-quotes explicitly keep them.

Each file is written with `fopen('/tmp/<fixed name>.tmp','w')` + `fwrite`. If the temp file is not writable the script prints ERROR and `die()`s, and later files are not written. Then a **line-count guard** applies, with the line count taken from `cat tmp | wc -l`. If it passes: `exec('sudo install -m 644 -o root -g root /tmp/X.tmp /etc/<file>')` and unlink the temp file. If it fails, the file is silently not installed and the temp file is left in `/tmp`.

| Order | Target | Temp file | Install condition |
|---|---|---|---|
| 1 | `/etc/mmdvmhost` | `/tmp/bW1kdm1ob3N0DQo.tmp` | lines > 140 |
| 2 | `/etc/ysfgateway` | `/tmp/eXNmZ2F0ZXdheQ.tmp` | > 35 |
| 3 | `/etc/nxdngateway` | `/tmp/kXKwkDKy793HF5.tmp` | > 30 and file exists |
| 4 | `/etc/p25gateway` | `/tmp/sJSySkheSgrelJX.tmp` | > 30 and file exists |
| 5 | `/etc/m17gateway` | `/tmp/spNcSdRUEmySTo9.tmp` | > 30 and file exists |
| 6 | `/etc/ysf2dmr` | `/tmp/dsWGR34tHRrSFFGA.tmp` | > 35 |
| 7 | `/etc/ysf2nxdn` | `/tmp/dsWGR34tHRrSFFGb.tmp` | > 35 |
| 8 | `/etc/ysf2p25` | `/tmp/dsWGR34tHRrSFFGc.tmp` | > 25 |
| 9 | `/etc/dgidgateway` | `/tmp/cu0G4tG3CA45Z9B.tmp` | `$configdgidgateway` set and > 25 |
| 10 | `/etc/dmr2ysf` | `/tmp/dhJSgdy7755HGc.tmp` | > 25 |
| 11 | `/etc/dmr2nxdn` | `/tmp/nthfheS55HGc.tmp` | > 25 |
| 12 | `/etc/dapnetgateway` | `/tmp/lsHWie734HS.tmp` | > 19 |
| 13 | `/etc/dmrgateway` | `/tmp/k4jhdd34jeFr8f.tmp` | `fopen('/etc/dmrgateway','r')` succeeds and > 55 |
| 14 | modem marker (see 7.5) | `/tmp/sja7hFRkw4euG7.tmp` | `isset($configModem)`; no line guard. Any key named `Password` is written as `Password="<value with " removed>"` |

### 7.5 Controller switch and the modem marker (`/etc/dstar-radio.mmdvmhost` vs `/etc/dstar-radio.dstarrepeater`)

- Marker existence is the only controller switch. Read side: section visibility (5.2) and the radio preselection (5.1). Write side (l.639-643): on every Apply, `sudo rm -rf /etc/dstar-radio.*`, then `sudo touch` the chosen marker.
- At page start `$configModem` was parsed from the **old** marker (the mmdvmhost one wins if both exist). Its `[Modem]Hardware` may be replaced by `confHardware`, and `[BrandMeister]Password` may be set or unset by the DMR section (l.1531/1534).
- At the end (l.3760-3825), if `$configModem` is set, it is serialised to `/tmp/sja7hFRkw4euG7.tmp` and installed with `sudo install -m 644 -o root -g root` into `/etc/dstar-radio.dstarrepeater` if that file now exists, and into `/etc/dstar-radio.mmdvmhost` if that file now exists. The content therefore moves to the newly chosen marker. The paths are defined unconditionally at l.207-208, so the install also works right after a fresh `touch`.
- If no marker existed at page load and no `confHardware` was posted (the hardware row is hidden then), `$configModem` stays unset and the new marker remains a **0-byte** file. The next GET then shows the "re-select your modem" alert (section 3), the hardware row appears, and choosing a model creates `$configModem['Modem']['Hardware']`, which is then written.
- Which daemon runs is decided by the service wrapper scripts (repo `Pi-Star_Binaries_sbin/`, not configure.php). `mmdvmhost.service` exits if `/etc/dstar-radio.dstarrepeater` is readable or `/etc/dstar-radio.mmdvmhost` is not, and with neither marker it writes "Unconfigured" to a Nextion/OLED. `dstarrepeater.service` does the mirror check. `ircddbgateway.service` and `timeserver.service` branch on `/etc/dstar-radio.mmdvmhost`. configure.php itself starts **both** `dstarrepeater.service` and `mmdvmhost.service` (7.6) and relies on these checks.
- UI hint only: an alert asks the user to power-cycle after switching (5.1). No reboot is triggered.

### 7.6 Flat-file commit, service start, timezone, close (l.3827-3944)

1. `config_writer_commit(false)` (l.3832). `false` means no extra mount cycle. Errors are logged with `error_log`. For each file:
   - **Flat** (`/etc/ircddbgateway`, `/etc/dstarrepeater`, `/etc/timeserver`, `/etc/aprsgateway`, `/etc/mobilegps`, `/etc/starnetserver`): read with `file()`; for each staged key replace the **first** line starting at column 0 with `key=`; a missing key is skipped and logged (never appended). Join with `\n` plus a trailing `\n`, write to `tempnam('/tmp','pistar_cw_')` (chmod 600), then `sudo install -m 644 -o root -g root`. Values may not contain NUL, CR or LF, and keys must match `[A-Za-z_][A-Za-z0-9_]*`.
   - **PHP-string** (`/var/www/dashboard/config/language.php`, `/var/www/dashboard/config/ircddblocal.php`): replace the first line matching `^\$var\s*=` with `$var='<addcslashes(v,"\\'")>';`, then install with mode 644.
   - **Privileged** (`/root/.Remote Control`, `/etc/hostapd/hostapd.conf`): read with `sudo -n cat`, use the same key rule, install with `sudo install -m 600 -o root -g root`.
   - Because the commit reads files at commit time, the earlier direct rewrites of `/etc/ircddbgateway` are preserved.
2. `sudo systemctl daemon-reload` (background), then `sudo systemctl start X` in background for: `dstarrepeater`, `mmdvmhost`, `ircddbgateway`, `timeserver`, `pistar-watchdog`, `pistar-remote`, `ysf2dmr`, `ysf2nxdn`, `ysf2p25`, `nxdn2dmr`, `ysfgateway`, `ysfparrot`, `p25gateway`, `p25parrot`, `nxdngateway`, `nxdnparrot`, `m17gateway`, `dmr2ysf`, `dmr2nxdn`, `dmrgateway`, `dapnetgateway`, `aprsgateway`. This is the same set as the stop list. The start order differs slightly (ysf2* before ysfgateway).
3. Timezone handler (5.3 `systemTimezone`). It runs after the services have been started.
4. `sudo systemctl start cron.service` (background).
5. `unset($_POST)`; emit `<script>setTimeout(function(){ window.location=window.location; }, 7500);</script>`. This is a GET reload of the page after 7.5 s.
6. `sudo sync && sudo sync && sudo sync && sudo mount -o remount,ro /`.
7. The footer is rendered (l.5711 onward). The form is **not** rendered on a POST response.

Behaviour notes for a native re-implementation:
- Services are stopped and started with `&`, so the PHP request never waits for them. The 1 s sleep is the only wait between stop and writes.
- All the `empty()` checks mean the string `"0"` is treated as "not submitted" for every text field in scope (hang times, latitude/longitude, IDs).
- Displayed values are echoed without `htmlspecialchars`, except the hostname, the platform string and the `PHP_SELF` form action. The native UI must escape them.
- The INI write-back rewrites whole files: comments are dropped, `parse_ini_file` boolean words are converted (`yes/on/true`→`1`, `no/off/false/null/none`→empty), and quotes are dropped except for re-quoted keys.

---

## 8. Unclear or defective points (with line numbers)

- l.4250: `str_replace` arguments reversed, so the "current Nextion port" option always has `value=""`. Keeping it selected leaves the port unchanged, because an empty POST is skipped.
- l.1471: the DMR ID is silently truncated to 7 digits although `maxlength="9"`.
- l.929-931 and l.1038: frequencies below 100 MHz are right-padded to 9 digits (×10 error).
- l.953: in duplex mode ysf2dmr `TXFrequency` is set to the RX value.
- l.972-997 and l.1079-1107: a TX frequency outside 144-148 / 218-226 / 420-450 / 1240-1300 MHz with no `confDStarModuleSuffix` stages empty `callsign`, `beaconText` and `repeaterBand1` (undefined variables).
- l.1230: `APRSGatewayEnable` is saved only when `confCallsign` is non-empty and Pi-Star Version `>= "4.1.4"` (string compare).
- l.2627: the DMR WhiteList is erased on every Apply where the field is hidden.
- l.586-618: the factory reset has no server-side `RESET` confirmation in this file, and services and cron are not restarted afterwards. Whether the wrapper restarts or reboots cannot be checked from this repo.
- l.620-636: on the config-read error path, services stay stopped and `/` stays rw.
- l.4421-4428: the APRS Host Enable `<tr>` is never closed.
- `/usr/local/etc/APRSHosts.txt`: no sample in the repo. The `host:port`-style format is inferred only from the `preg_split('/:/')` parse.

---

## Services/commands touched by this section on Apply

- `sudo mount -o remount,rw /` … `sudo sync && sudo sync && sudo sync && sudo mount -o remount,ro /`
- `sudo systemctl stop|start cron.service`
- `sudo systemctl stop` / `start` (background) of: dstarrepeater, mmdvmhost, ircddbgateway, timeserver, pistar-watchdog, pistar-remote, ysfgateway, ysf2dmr, ysf2nxdn, ysf2p25, nxdn2dmr, ysfparrot, p25gateway, p25parrot, nxdngateway, nxdnparrot, m17gateway, dmr2ysf, dmr2nxdn, dmrgateway, dapnetgateway, aprsgateway; plus `sudo systemctl daemon-reload`
- `sudo /usr/local/sbin/pistar-factory-reset --really` (Factory Reset only)
- `sudo rm -rf /etc/dstar-radio.*`, `sudo touch /etc/dstar-radio.{mmdvmhost|dstarrepeater}`
- `sudo sed -i "/OnStartupSec=/c\OnStartupSec=30|60" /lib/systemd/system/{dstarrepeater,mmdvmhost}.timer` (when `confHardware` is posted)
- `sudo sed -i "$ a\ ...eth0:1..." /etc/network/interfaces` (`idrp2c` only, when there is no existing eth0:1)
- `sudo sed -i 's|old|new|' /etc/hostname /etc/hosts /etc/motd` (hostname change)
- `sudo timedatectl set-timezone <tz>`; `sudo install` → `/etc/timezone`, `/var/www/dashboard/config/config.php`
- `sudo install -m 644 -o root -g root` → `/etc/mmdvmhost`, `/etc/ysfgateway`, `/etc/nxdngateway`, `/etc/p25gateway`, `/etc/m17gateway`, `/etc/ysf2dmr`, `/etc/ysf2nxdn`, `/etc/ysf2p25`, `/etc/dgidgateway`, `/etc/dmr2ysf`, `/etc/dmr2nxdn`, `/etc/dapnetgateway`, `/etc/dmrgateway`, `/etc/dstar-radio.*`, `/etc/ircddbgateway`, `/etc/dstarrepeater`, `/etc/timeserver`, `/etc/aprsgateway`, `/etc/starnetserver`, `/var/www/dashboard/config/language.php`, `/var/www/dashboard/config/ircddblocal.php`
- `sudo install -m 600 -o root -g root` → `/etc/hostapd/hostapd.conf` (ssid on hostname change); `sudo -n cat` to read it
- Read-only probes: `cat /etc/hostname`, `cat /etc/timezone`, `timedatectl list-timezones`, `ls /dev/ | egrep -h "ttyA|ttyUSB"`, `grep "eth0:1" /etc/network/interfaces | wc -l`, `DMRGateway -v`, `P25Gateway -v`, `NXDNGateway -v`, `YSFGateway -v`, `wc -l` on the temp files, `uptime -p`, `/usr/local/bin/platformDetect.sh`, `cat /sys/class/thermal/thermal_zone0/temp`
