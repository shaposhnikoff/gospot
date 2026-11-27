# configure.php spec, part 03: D-Star and Yaesu System Fusion sections

Source: `Pi-Star_DV_Dash/admin/configure.php` at revision `10bf34c9` (read-only reference).
Form: lines 4869-5306. POST handling: various places between lines 359 and 3944 (cited per field).
Helpers: `Pi-Star_DV_Dash/config/config_writer.php`, `Pi-Star_DV_Dash/config/ircddblocal.php`,
`Pi-Star_DV_Dash/functions.js`, `Pi-Star_DV_Dash/config/ircddbgateway_languages.inc`.
Labels: `Pi-Star_DV_Dash/lang/english_uk.php`.

All line numbers below refer to `admin/configure.php` unless another file is named.

---

## 0. Common mechanics this section relies on

### 0.1 How config files are loaded for display (top of file)

| Variable | Source | Parser | Notes |
|---|---|---|---|
| `$configdstar` | `/etc/dstarrepeater` | lines 91-102: for each line containing `=`, `list($k,$v)=preg_split('/=/',$line)`; `$v = trim(str_replace('"','',$v))`; stored only if `strlen($v) > 0` | Flat key=value file. Internal spaces are kept, trailing whitespace is trimmed. |
| `$configs` | `$gatewayConfigPath` = `/etc/ircddbgateway` (from `config/ircddblocal.php`) | lines 105-116: same as above, also skips key `ircddbPassword` | Values containing `=` are truncated at the second `=` (only two pieces are taken by `list()`). |
| `$configmmdvm` | `/etc/mmdvmhost` | `parse_ini_file(..., true)` (line 120) | INI_SCANNER_NORMAL: quotes stripped; `yes/on/true` become `"1"`, `no/off/false/none` become `""`. |
| `$configysfgateway` | `/etc/ysfgateway` | `parse_ini_file` (line 124) | |
| `$configysf2dmr` | `/etc/ysf2dmr` (only if it exists) | `parse_ini_file` (lines 127-130) | |
| `$configysf2nxdn` | `/etc/ysf2nxdn` (if it exists) | lines 133-136 | |
| `$configysf2p25` | `/etc/ysf2p25` (if it exists) | lines 139-142 | |
| `$configdgidgateway` | `/etc/dgidgateway` (if it exists) | lines 145-148 | No form field in this section, but defaults are rewritten on every save (see 3.4). |
| `$configdmr2ysf` | `/etc/dmr2ysf` (if it exists) | lines 151-154 | Used for YSF section visibility and written by `ysfStartupHost`. |
| `$configp25gateway` | `/etc/p25gateway` (if it exists) | lines 163-166 | Written by `ysf2p25StartupDstId`. |
| `$confignxdngateway` | `/etc/nxdngateway` (if it exists) | lines 169-172 | Written by `ysf2nxdnStartupDstId`. |
| `$configModem` | `/etc/dstar-radio.dstarrepeater` or, if it exists (wins), `/etc/dstar-radio.mmdvmhost` | `parse_ini_file` (lines 212-219) | Holds `[BrandMeister] Password` used by `bmHSSecurity`. |

### 0.2 Toggle-switch pattern

Every on/off "switch" in these sections uses the same pattern:

1. A `<input type="hidden" name="X" value="OFF" />` is printed earlier in the form.
2. Later in the form: `<div class="switch"><input id="toggle-…" class="toggle toggle-round-flat" type="checkbox" name="X" value="ON" [checked="checked"] aria-hidden="true" tabindex="-1" {chromeOnclick} /><label id="aria-toggle-…" role="checkbox" tabindex="0" aria-label="…" aria-checked="true|false" onKeyPress="fn()" onclick="fn()" for="toggle-…"><font style="font-size:0px">…</font></label></div>`.
3. PHP keeps the **last** value for a repeated POST name, so a checked box posts `ON` and an unchecked one posts the hidden `OFF`. The handler always gets `ON` or `OFF` while the section is rendered. When the section is not rendered, the key is missing and the handler is skipped.
4. `{chromeOnclick}` is `onclick="fn()"` only when the User-Agent contains `Chrome` (lines 3949-3999), and empty otherwise.
5. The `fn()` functions in `functions.js` (lines 316-359) only flip the label's `aria-checked`. They are called without an argument, so `event.keyCode` throws a harmless TypeError afterwards. The checkbox itself is toggled by the `<label for=…>`.

For a native re-implementation, a boolean toggle that always submits `ON`/`OFF` while the section is visible is enough.

### 0.3 Flat-file writer (`config_writer_stage_flat`)

Used for `/etc/ircddbgateway`, `/etc/dstarrepeater` and `/etc/timeserver` (allow-list in `config_writer.php` lines 98-108).

- `config_writer_stage_flat($path,$key,$value)` only queues the edit. Keys must match `/\A[A-Za-z_][A-Za-z0-9_]*\z/`, and values containing NUL/CR/LF are refused (`config_writer.php` 182-206). If the same key is staged twice, the last value wins.
- At commit (line 3832, `config_writer_commit(false)`), the writer replaces the first line that starts at column 0 with `key=` by `key=value`. **If the key is missing, nothing is appended**; it is only logged (`config_writer.php` 428-446). The file is then reinstalled with `sudo install -m 644 -o root -g root <tmp> <path>` (`config_writer.php` 341-346).
- `config_writer_stage_privileged_flat` (used for `/root/.Remote Control`) reads the file with `sudo -n cat` and reinstalls it with mode `600` (`config_writer.php` 282-305, 517-546).

### 0.4 INI writer (in-memory arrays)

`$configysfgateway`, `$configysf2dmr`, `$configysf2nxdn`, `$configysf2p25`, `$configdgidgateway`, `$configdmr2ysf`, `$confignxdngateway`, `$configp25gateway`, `$configmmdvm` and `$configModem` are written back in full after all handlers have run (lines 3274-3825). Each section becomes `[Section]\n`, followed by `key=value\n` lines in array order (new keys are appended at the end of their section) and a blank line. The result goes to a fixed `/tmp/*.tmp` file and is installed with `sudo install -m 644 -o root -g root`, but only if the temporary file has more lines than a threshold:

| File | Tmp file | Min lines | Extra guard | Lines |
|---|---|---|---|---|
| `/etc/mmdvmhost` | `/tmp/bW1kdm1ob3N0DQo.tmp` | >140 | | 3274-3312 |
| `/etc/ysfgateway` | `/tmp/eXNmZ2F0ZXdheQ.tmp` | >35 | | 3314-3350 |
| `/etc/nxdngateway` | `/tmp/kXKwkDKy793HF5.tmp` | >30 | file exists | 3352-3388 |
| `/etc/p25gateway` | `/tmp/sJSySkheSgrelJX.tmp` | >30 | file exists | 3390-3426 |
| `/etc/ysf2dmr` | `/tmp/dsWGR34tHRrSFFGA.tmp` | >35 | (none) | 3466-3502 |
| `/etc/ysf2nxdn` | `/tmp/dsWGR34tHRrSFFGb.tmp` | >35 | (none) | 3504-3538 |
| `/etc/ysf2p25` | `/tmp/dsWGR34tHRrSFFGc.tmp` | >25 | (none) | 3540-3574 |
| `/etc/dgidgateway` | `/tmp/cu0G4tG3CA45Z9B.tmp` | >25 | `isset($configdgidgateway)` | 3576-3612 |
| `/etc/dmr2ysf` | `/tmp/dhJSgdy7755HGc.tmp` | >25 | (none) | 3614-3648 |
| modem file | `/tmp/sja7hFRkw4euG7.tmp` | none | `isset($configModem)`. Installed to `/etc/dstar-radio.dstarrepeater` and/or `/etc/dstar-radio.mmdvmhost`, whichever exists | 3760-3825 |

In the modem file, every `Password` key is rewritten as `Password="<value with all " removed>"` (line 3769). In the other INI files, values are written verbatim; values the handler does not touch lose the quotes that `parse_ini_file` stripped.

### 0.5 Save envelope (applies to all sections)

`submitform()` (lines 274-278) disables every `input[type=button]` and submits `form#config`. CSRF is checked at line 66. Handler order (only the steps relevant here):

1. `sudo mount -o remount,rw /` (line 359).
2. `sudo systemctl stop cron.service`, then stop all DV services (lines 551-575): dstarrepeater, mmdvmhost, ircddbgateway, timeserver, pistar-watchdog, pistar-remote, ysfgateway, ysf2dmr, ysf2nxdn, ysf2p25, nxdn2dmr, ysfparrot, p25gateway, p25parrot, nxdngateway, nxdnparrot, m17gateway, dmr2ysf, dmr2nxdn, dmrgateway, dapnetgateway, aprsgateway. Each runs as `sudo systemctl stop X.service > /dev/null 2>/dev/null &`.
3. Field handlers (cited below), then defaults and migrations (lines 2903-3175).
4. INI write-back (lines 3274-3825), then `config_writer_commit(false)` (line 3832).
5. `sudo systemctl daemon-reload`, then start the same services (lines 3838-3860) and cron (line 3938).
6. `sudo sync && sudo sync && sudo sync && sudo mount -o remount,ro /` (line 3944). The page reloads itself after 7.5 s (line 3941).

---

## 1. Section "D-Star Configuration" (form lines 4869-5057)

### 1.1 Visibility

```php
4869 <?php if (file_exists('/etc/dstar-radio.dstarrepeater') || $configmmdvm['D-Star']['Enable'] == 1) { ?>
```

The section is shown when the controller is DStarRepeater (marker `/etc/dstar-radio.dstarrepeater` exists), or when the controller is MMDVMHost and `/etc/mmdvmhost [D-Star] Enable` is `1`. Otherwise the whole block is skipped, including its hidden inputs.

### 1.2 Layout

```
<h2>D-Star Configuration</h2>                          ($lang['dstar_config'])
<input type="hidden" name="confTimeAnnounce" value="OFF" />       (4871)
<input type="hidden" name="confircddbEnabled" value="OFF" />      (4872)
<input type="hidden" name="confHostFilesNoDExtra" value="OFF" />  (4873)
<table>
 <tr><th width="200">Setting</th><th colspan="2">Value</th></tr>
 rows (3 logical columns: label | value | value/note)
</table>
<div><input type="button" value="Apply Changes" onclick="submitform()" /><br /><br /></div>
```

Header tooltips (4876-4877): `Setting` has `<span><b>Setting</b></span>`. `Value` has `<span><b>Value</b>The current value from the<br />configuration files</span>`. Both use `<a class="tooltip" href="#">`.

Every row label is `<td align="left"><a class="tooltip2" href="#">LABEL:<span><b>TIP TITLE</b>TIP BODY</span></a></td>`.

| # | Label (resolved) | Tooltip title / body | Value cell(s) |
|---|---|---|---|
| 1 | `RPT1 Callsign:` | **RPT1 Callsign** / This is the RPT1 field for your radio | colspan=2: text + select `confDStarModuleSuffix` |
| 2 | `RPT2 Callsign:` | **RPT2 Callsign** / This is the RPT2 field for your radio | colspan=2: read-only text |
| 3 | `Remote Password:` | **Remote Password** / Used for ircDDBGateway remote control access | colspan=2: password `confPassword` |
| 4 | `Default Reflector:` | **Default Reflector** / Used for setting the default reflector. | td colspan=1: select `confDefRef` + hidden text `confDefRef` + select `confDefRefLtr`. Then `<td width="300">`: radios `confDefRefAuto` |
| 5 | `ircDDBGateway Language:` | **ircDDBGateway Language** / Set your prefered language here | colspan=2 (`style="text-align: left;"`): select `ircDDBGatewayAnnounceLanguage` |
| 6 | `Time Announcements:` | **Time Announce** / Announce time hourly | colspan=2: toggle `confTimeAnnounce` |
| 7 | `Callsign Routing:` (hard-coded English) | **Callsign Routing** / Do you want callsign routing for D-Star | td: toggle `confircddbEnabled`. Then td: text `Connect ircDDB for call routing` |
| 8 | `Use DPlus for XRF:` (hard-coded English) | **No DExtra** / Should host files use DPlus Protocol for XRFs | td: toggle `confHostFilesNoDExtra`. Then td: text `Note: Update Required if changed` |

Section-level button: one `Apply Changes` (`$lang['apply']`) button, `type="button"`, `onclick="submitform()"` (line 5056). It submits the whole configuration form, not just this section.

### 1.3 Fields

#### 1.3.1 `confDStarModuleSuffix`: RPT1 module letter (lines 4879-4911)

- **Control**: plain text, then `<select name="confDStarModuleSuffix">`.
- **Display text**: `str_replace(' ', '&nbsp;', substr($configdstar['callsign'], 0, 7))`. These are the first 7 chars of `/etc/dstarrepeater` key `callsign` (for example `M1ABC  `), with spaces shown as non-breaking spaces.
- **Options**:
  1. First option, selected: `value = label = substr($configdstar['callsign'], 7)`, the current 8th char (module letter). It is an empty string if the callsign is ≤7 chars.
  2. Static `A` … `Z` (26 options, `<option>X</option>`, value = text).

  The current letter therefore appears twice in the list.
- **Save** (only inside the frequency handlers, which belong to the General section):
  - Duplex path: runs if `confFREQtx` and `confFREQrx` are both non-empty (lines 923-1031). Simplex path: runs if `confFREQ` is non-empty (lines 1034-1141). The General section always renders either `confFREQ` or the pair `confFREQrx`/`confFREQtx` (about line 4303). **If neither frequency field is posted, this field is ignored.**
  - When `confDStarModuleSuffix` is **non-empty** (lines 999-1018 / 1109-1128):
    - `$confRPT1 = str_pad(escapeshellcmd($_POST['confCallsign']), 7, " ") . strtoupper(escapeshellcmd($_POST['confDStarModuleSuffix']))`
    - `$confIRCrepeaterBand1 = strtoupper(escapeshellcmd(suffix))`
    - `$configmmdvm['D-Star']['Module'] = strtoupper(escapeshellcmd(suffix))`, which goes to `/etc/mmdvmhost [D-Star] Module`.
    - `$bandSuffix = strtoupper(suffix)`. If it matches `/\A[A-Z]\z/`, `/etc/timeserver send<Letter>=1` is staged; otherwise an error is logged.
  - When it is **empty** (lines 973-998 / 1079-1108), the module is derived from the frequency in Hz (`$newFREQtx` for duplex, `$newFREQ` for simplex):

    | Range (Hz) | Module | timeserver |
    |---|---|---|
    | 1240000000-1300000000 | A | `sendA=1` |
    | 420000000-450000000 | B | `sendB=1` |
    | 218000000-226000000 | A | `sendA=1` |
    | 144000000-148000000 | C | `sendC=1` |

    Each band sets `$confRPT1` (callsign padded to 7 chars + letter), `$confIRCrepeaterBand1` and `$configmmdvm['D-Star']['Module']`. Outside these bands nothing is assigned. `$confRPT1` and `$confIRCrepeaterBand1` stay undefined, so `/etc/dstarrepeater callsign`, `beaconText` and `/etc/ircddbgateway repeaterBand1` are staged as empty strings (lines 1023-1029 run unconditionally; this is unclear or buggy legacy behaviour).
  - Then, in both cases (lines 1020-1030 / 1130-1140):
    - `$newCallsignUpper = strtoupper(escapeshellcmd($_POST['confCallsign']))`
    - `$confRPT2 = strtoupper(str_pad(escapeshellcmd($_POST['confCallsign']), 7, " ") . "G")`
    - `$confRPT1 = strtoupper($confRPT1)`
    - Staged flat writes: `/etc/dstarrepeater callsign=<RPT1>`, `gateway=<RPT2>`, `beaconText=<RPT1>`. `/etc/ircddbgateway repeaterBand1=<letter>`, `repeaterCall1=<CALLSIGN>`.
  - Also unconditional on every POST (lines 882-886): `/etc/timeserver sendA..sendE=0` are staged before the `send<Letter>=1` above. The later stage wins for the same key. A letter F-Z stages `sendF` and so on, which is silently skipped if the timeserver file has no such line (writer never appends).
- **Important consequence**: when the D-Star section is hidden (MMDVMHost with D-Star disabled), the field is not posted. The module is then recomputed from the frequency band on every save.
- **Validation**: only the single-letter regex for the timeserver key. RPT1, Module and repeaterBand1 use `escapeshellcmd` + `strtoupper` with no letter check.

#### 1.3.2 RPT2 display (lines 4912-4915)

- Display only, with no input: `str_replace(' ', '&nbsp;', $configdstar['gateway'])` (`/etc/dstarrepeater` key `gateway`, for example `M1ABC  G`).
- It is written as a side effect of 1.3.1: `gateway = str_pad(callsign,7," ")."G"`.

#### 1.3.3 `confPassword`: ircDDBGateway remote password (lines 4916-4919)

- `<input type="password" name="confPassword" size="30" maxlength="30" value="<?php echo $configs['remotePassword'] ?>" />`
- **Display**: `/etc/ircddbgateway` key `remotePassword` (quotes stripped, trimmed). Not HTML-escaped.
- **Save** (lines 687-703): only if `!empty($_POST['confPassword'])`, so an empty field cannot clear the password.
  - `/etc/ircddbgateway remotePassword=<raw POST value>` (flat).
  - `/root/.Remote Control password=<raw POST value>` (privileged flat: `sudo -n cat`, `sudo install -m 600`). The file name contains a literal space.
  - `/root/.Remote Control port=<$configs['remotePort']>` (current `/etc/ircddbgateway remotePort`).
  - No sanitisation beyond the writer's NUL/CR/LF refusal.

#### 1.3.4 `confDefRef` (select), `confDefRef` (text), `confDefRefLtr`, `confDefRefAuto`: default reflector (lines 4920-4998)

**Select `confDefRef`** (lines 4922-4962):

```html
<select name="confDefRef" onchange="if (this.options[this.selectedIndex].value == 'customOption') {
      toggleField(this,this.nextSibling);
      this.selectedIndex='0';
      } ">
```

Options in order:

1. `value = label = substr($configs['reflector1'], 0, 6)`, selected. This is the first 6 chars of `/etc/ircddbgateway reflector1` (for example `REF001` from `REF001 C`).
2. `<option value="customOption">Text Entry</option>`.
3. From `/usr/local/etc/DCS_Hosts.txt`: for each line that contains `DCS` anywhere and has no `#` anywhere, add `substr(line,0,6)`. Separately, for each line that contains `XLX` and no `#`, add `substr(line,0,6)`. A line matching both is emitted twice.
4. From `/usr/local/etc/DPlus_Hosts.txt`: lines containing `REF` (no `#`) give `substr(line,0,6)`. Lines containing `XRF` (no `#`) give `substr(line,0,6)`.
5. From `/usr/local/etc/DExtra_Hosts.txt`: lines containing `XRF` (no `#`) give `substr(line,0,6)`.

Option value equals the label. Duplicates are not removed. Files are opened with `fopen` and no existence check (lines 4928-4930).

**Hidden text `confDefRef`** (lines 4963-4964): `<input name="confDefRef" style="display:none;" disabled="disabled" type="text" size="7" maxlength="7" onblur="if(this.value==''){toggleField(this,this.previousSibling);}" />`. Choosing "Text Entry" runs `toggleField(select, text)` (`functions.js` 1-7). The select is disabled and hidden, and the text box is enabled, shown and focused. The select index is reset to 0. Leaving the text box empty on blur switches back. Only the enabled control is submitted, so both post under the same name.

**Select `confDefRefLtr`** (lines 4965-4993): the first option is `substr($configs['reflector1'], 7)` (the 8th char, the module letter), selected. Then static `A`…`Z`.

**Radios `confDefRefAuto`** (lines 4995-4997), in `<td width="300">`:

```html
<input type="radio" name="confDefRefAuto" value="ON" [checked if $configs['atStartup1']=='1'] />Startup
<input type="radio" name="confDefRefAuto" value="OFF" [checked if $configs['atStartup1']=='0'] />Manual
```

If `atStartup1` is neither value, no radio is checked and nothing is posted.

**Save, reflector** (lines 706-715):

```php
if (empty($_POST['confDefRef']) != TRUE ) {
  if (stristr(strtoupper(escapeshellcmd($_POST['confDefRef'])), strtoupper(escapeshellcmd($_POST['confCallsign']))) != TRUE ) {
    if (strlen($_POST['confDefRef']) != 7) { $targetRef = strtoupper(escapeshellcmd(str_pad($_POST['confDefRef'], 7, " "))); }
    else { $targetRef = strtoupper(escapeshellcmd($_POST['confDefRef'])); }
    config_writer_stage_flat('/etc/ircddbgateway', 'reflector1', $targetRef.escapeshellcmd($_POST['confDefRefLtr']));
  }
}
```

- The value is skipped when the reflector name contains the station's own callsign (`confCallsign` from the General section), case-insensitively.
- The name is right-padded with spaces to 7 chars (a 6-char `REF001` becomes `REF001 `) and uppercased. The letter is then appended without uppercasing; the options are already uppercase.
- Result: `/etc/ircddbgateway reflector1=REF001 C`.
- Text entry: anything up to 7 chars. With 7 chars there is no padding, so the result is 8 chars plus the letter. `escapeshellcmd` may insert backslashes before shell metacharacters.
- Unclear: with an empty `confCallsign`, `stristr` with an empty needle behaves differently in PHP 7 (returns false, so the value is written) and PHP 8 (returns the haystack, so the value is never written). In practice `confCallsign` is always posted.

**Save, autostart** (lines 718-725): `ON` writes `/etc/ircddbgateway atStartup1=1`; `OFF` writes `atStartup1=0`.

**Related unconditional side effect** (lines 728-732): if `$configs['ccsEnabled'] == "1"`, `/etc/ircddbgateway ccsHost` is set to a random pick from `CCS701`, `CCS702`, `CCS704` on every save.

#### 1.3.5 `ircDDBGatewayAnnounceLanguage`: ircDDBGateway and timeserver language (lines 4999-5019)

- `<select name="ircDDBGatewayAnnounceLanguage">`.
- **Options**: built only if `/var/www/dashboard/config/ircddbgateway_languages.inc` is readable. Each line is split on `;` into `[0]=display name; [1]=ircddbgateway index; [2]=timeserver index` (`[2]` is `rtrim`med). Lines whose `[0]` contains `#`, or whose `[0]` is empty, are skipped. Option: `value="[1],[2]"`, label `htmlspecialchars([0])`.
- **Preselected** when `$configs['language'] == [1]` (`/etc/ircddbgateway language`).
- Current file contents (display, irc, ts), in this order:
  `English_(UK);0;0`, `Deutsch;1;4`, `Dansk;2;0`, `Francais;3;6`, `Italiano;4;0`, `Polski;5;0`, `English_(US);6;2`, `Espanol;7;9`, `Svenska;8;8`, `Nederlands_(NL);9;7`, `Nederlands_(BE);10;7`, `Norsk;11;10`, `Portugues;12;11`. Labels keep the underscores, for example `English_(UK)`.
- **Save** (lines 875-879): `explode(',', escapeshellcmd(value))`. Then `/etc/ircddbgateway language=<[0]>` and `/etc/timeserver language=<[1]>` (both via `escapeshellcmd`). Skipped if empty.

#### 1.3.6 `confTimeAnnounce`: time announcements (lines 5020-5030)

- Toggle (pattern 0.2). Hidden default `OFF` at line 4871. `id="toggle-timeAnnounce"`, label id `aria-toggle-timeAnnounce`, `aria-label="D-Star Time Announcements"`, JS `toggleDstarTimeAnnounce()`. Cell `colspan="2"`.
- **Display**: checked when `/etc/timeserver.dissable` does **not** exist (note the spelling "dissable").
- **Save** (lines 2455-2458): `ON` runs `system('sudo rm -rf /etc/timeserver.dissable')`; `OFF` runs `system('sudo touch /etc/timeserver.dissable')`. No config key is written; the timeserver service reads the marker file.

#### 1.3.7 `confircddbEnabled`: callsign routing (lines 5031-5042)

- Toggle. Hidden default `OFF` at line 4872. `id="toggle-ircddbEnabled"`, label id `aria-toggle-ircddbEnabled`, `aria-label="Use ircDDB callsign routing"`, hidden label text `Enable ircDDB callsign routing`, JS `toggleircddbEnabled()`. **This function is not defined in `functions.js`**, so clicking raises a ReferenceError and `aria-checked` never updates. The checkbox still toggles through the `<label for>`. The toggle cell has no colspan, and the next cell holds the text `Connect ircDDB for call routing`.
- **Display**: checked when `isset($configs['ircddbEnabled']) && $configs['ircddbEnabled'] == "1"` (`/etc/ircddbgateway ircddbEnabled`).
- **Save** (lines 1255-1266):
  - `ON` writes `/etc/ircddbgateway ircddbEnabled=1`; `OFF` writes `ircddbEnabled=0`.
  - Then, if `$configs['ircddbHostname'] == "rr.openquad.net"`, it stages `ircddbEnabled=0` (overriding ON, since the last stage wins) and `ircddbHostname=ircv4.openquad.net`.
- **Related unconditional side effect** (lines 2913-2931): if `/etc/ircddbgateway` has neither `ircddbHostname2` nor `ircddbEnabled2`, the file is read and the line `ircddbEnabled2=0` is inserted after the first `ircddbEnabled=` line (`preg_replace` with limit 1). It is installed at once with `sudo install -m 644 -o root -g root <tempnam> /etc/ircddbgateway`, before the commit.

#### 1.3.8 `confHostFilesNoDExtra`: use DPlus for XRF (lines 5043-5054)

- Toggle. Hidden default `OFF` at line 4873. `id="toggle-dplusHostFiles"`, label id `aria-toggle-dplusHostFiles`, `aria-label="Use D-Plus for XRF Hosts"`, JS `toggleDstarDplusHostfiles()`. The toggle cell has no colspan, and the next cell holds the text `Note: Update Required if changed`.
- **Display**: checked when `/etc/hostfiles.nodextra` exists.
- **Save** (lines 3258-3265): `ON` runs `sudo touch /etc/hostfiles.nodextra` if the file is missing; `OFF` runs `sudo rm -rf /etc/hostfiles.nodextra` if it exists. No config key is written. The marker is used by the host-files update script, which is outside this repo; the UI only warns that an update is needed.

### 1.4 D-Star-related writes that come from other sections (cross-reference only)

These are handled elsewhere but touch the same files, so a native implementation must not clobber them:

- `confCallsign` (General): `/etc/ircddbgateway gatewayCallsign`, `dplusLogin` (padded to 8), `/etc/timeserver callsign`, `/etc/starnetserver callsign/ircddbUsername`, `ircddblocal.php $callsign`, optionally `ircddbUsername`, `aprsPassword`, and the `/etc/aprsgateway` Callsign/Password, plus the `aprsPassword=` line removal (lines 1144-1240).
- Latitude, longitude, town, country and URL go to `/etc/ircddbgateway latitude, latitude1, longitude, longitude1, description1, description1_1, description2, description1_2, url` (lines 734-814).
- `selectedAPRSHost` (General, form line 4434) goes to `/etc/ircddbgateway aprsHostname`, `/etc/aprsgateway Server` and the `aprs.fi Server` of ysfgateway, ysf2dmr, ysf2nxdn and ysf2p25 (lines 834-872).
- Frequencies go to `/etc/ircddbgateway frequency1, offset1, gatewayType` and `/etc/dstarrepeater dvapFrequency, dvmegaRX/TXFrequency, mode` (lines 938-944, 1044-1050).
- `confHardware` (modem) goes to `/etc/dstarrepeater modemType/...Port/...Variant` and `/etc/ircddbgateway repeaterType1/repeaterAddress1/repeaterPort1` (lines 1786-2398).
- `nodeMode` goes to `/etc/dstarrepeater restriction` (lines 2633-2650).
- `dstarRfHangTime` and `dstarNetHangTime` (MMDVMHost section) go to `/etc/mmdvmhost [D-Star] ModeHang` and `[D-Star Network] ModeHang`.

---

## 2. Section "Yaesu System Fusion Configuration" (form lines 5058-5306)

### 2.1 Visibility

```php
5058 <?php if (file_exists('/etc/dstar-radio.mmdvmhost') && ($configmmdvm['System Fusion Network']['Enable'] == 1 || $configdmr2ysf['Enabled']['Enabled'] == 1 )) {
5059 $ysfHosts = fopen("/usr/local/etc/YSFHosts.txt", "r"); ?>
```

The section requires the MMDVMHost controller, and either `/etc/mmdvmhost [System Fusion Network] Enable=1` or `/etc/dmr2ysf [Enabled] Enabled=1`. Inside it, three sub-blocks have their own conditions:

- YSF2DMR rows: `file_exists('/etc/dstar-radio.mmdvmhost') && $configysf2dmr['Enabled']['Enabled'] == 1` (line 5155).
- YSF2NXDN rows: `... && $configysf2nxdn['Enabled']['Enabled'] == 1` (line 5215).
- YSF2P25 rows: `... && $configysf2p25['Enabled']['Enabled'] == 1` (line 5255).

### 2.2 Layout

```
<input type="hidden" name="confHostFilesYSFUpper" value="OFF" />      (5060)
<input type="hidden" name="wiresXCommandPassthrough" value="OFF" />   (5061)
<h2>Yaesu System Fusion Configuration</h2>                            ($lang['ysf_config'])
<table>
 <tr><th width="200">Setting</th><th colspan="2">Value</th></tr>    (same header tooltips as 1.2)
 rows
</table>
<div><input type="button" value="Apply Changes" onclick="submitform()" /><br /><br /></div>  (5305)
```

| # | Label (resolved) | Tooltip title / body | Value cell(s) | Condition |
|---|---|---|---|---|
| 1 | `YSF Startup Host:` | **YSF Host** / Set your prefered YSF Host here | colspan=2 (`text-align:left`): select `ysfStartupHost` | always |
| 2 | `UPPERCASE Hostfiles:` | **UPPERCASE Hostfiles** / Should host files use UPPERCASE only - fixes issues with FT-70D radios. | td: toggle `confHostFilesYSFUpper`. td: `Note: Update Required if changed` | always |
| 3 | `WiresX Passthrough:` | **WiresX Auto Passthrough** / Use this to automatically send WiresX commands through to YSF2xxx cross-over modes. | colspan=2: toggle `wiresXCommandPassthrough` | always |
| 4 | `(YSF2DMR)CCS7/DMR ID:` (no space after `)`) | **CCS7/DMR ID** / Enter your CCS7 / DMR ID here | colspan=2: text + select `ysf2dmrId` | YSF2DMR |
| 5 | `DMR Master:` (`$lang['dmr_master']`; the later duplicate key at lang line 121 wins over "DMR Master (MMDVMHost)" at line 72) | **DMR Master (YSF2DMR)** / Set your prefered DMR master here | colspan=2: select `ysf2dmrMasterHost` | YSF2DMR |
| 6 | `Hotspot Security:` | **DMR Master Password** / Override the Password for DMR with your own custom password, make sure you already configured this on your chosed DMR Master. Empty the field to use the default. | colspan=2: password `bmHSSecurity` | YSF2DMR |
| 7 | `DMR TG:` | **YSF2DMR TG** / Enter your DMR TG here | colspan=2: text `ysf2dmrTg` | YSF2DMR |
| 8 | `(YSF2NXDN) NXDN ID:` | **NXDN ID** / Enter your NXDN ID here | colspan=2: text `ysf2nxdnId` | YSF2NXDN |
| 9 | `NXDN Startup Host:` | **NXDN Host** / Set your prefered NXDN Host here | colspan=2: select `ysf2nxdnStartupDstId` | YSF2NXDN |
| 10 | `(YSF2P25) CCS7/DMR ID:` | **DMR ID** / Enter your CCS7 / DMR ID here | colspan=2: text `ysf2p25Id` | YSF2P25 |
| 11 | `P25 Startup Host:` | **P25 Host** / Set your prefered P25 Host here | colspan=2: select `ysf2p25StartupDstId` | YSF2P25 |

Section-level button: one `Apply Changes` button that submits the whole form (line 5305).

### 2.3 Fields

#### 2.3.1 `ysfStartupHost`: YSF startup host (lines 5068-5123)

- `<select name="ysfStartupHost">`.
- **Current value**: `$testYSFHost = $configysfgateway['Network']['Startup']` (`/etc/ysfgateway [Network] Startup`) if set; otherwise `"none"`.
- **Options in order**:
  1. `<option value="none">None</option>`. Selected only when `[Network] Startup` is not set.
  2. Static pseudo-hosts. Each is selected when `$testYSFHost` equals the name after the comma:

     | value | label | selected when Startup == |
     |---|---|---|
     | `00001,ZZ Parrot` | `YSF00001 - Parrot` | `ZZ Parrot` |
     | `00002,YSF2DMR` | `YSF00002 - Link YSF2DMR` | `YSF2DMR` |
     | `00003,YSF2NXDN` | `YSF00003 - Link YSF2NXDN` | `YSF2NXDN` |
     | `00004,YSF2P25` | `YSF00004 - Link YSF2P25` | `YSF2P25` |

  3. `/usr/local/etc/YSFHosts.txt`: each line is split on `;` (`[0]=5-digit id; [1]=name; [2]=description; …`). Lines whose `[0]` contains `#`, or whose `[0]` is empty, are skipped. `value="[0],[1]"` (not HTML-escaped). Label: `YSF[0] - htmlspecialchars([1]) - htmlspecialchars([2])`. Selected when `$testYSFHost == [1]` (the name).
  4. `/usr/local/etc/FCSHosts.txt` (if it exists): split on `;`. Only lines where `substr([0],0,3) == "FCS"` are used. `value="[0],[0]"`, label `[0] - htmlspecialchars([1])`. Selected when `$testYSFHost == [0]`.
- **Save** (lines 1335-1369). If not empty:

  ```php
  $newYSFStartupHostArr = explode(',', escapeshellcmd($_POST['ysfStartupHost']));
  ```

  - If `/etc/ysfgateway` has a `[FCS Network]` section (`isset($configysfgateway['FCS Network'])`):
    - `none`: `unset([Network] Startup)` and `/etc/dmr2ysf [DMR Network] DefaultDstTG=9`.
    - otherwise: `[Network] Startup = Arr[1]` (the **name**, or the FCS id for FCS rooms). `DefaultDstTG = Arr[0]` (the 5-digit YSF id, with leading zeros kept, for example `00001`), or `9` if `Arr[0]` starts with `FCS`.
  - Without `[FCS Network]`: the same, but `[Network] Startup = Arr[0]` (the **id**).
  - Note: `escapeshellcmd` backslash-escapes characters such as ``#&;`|*?~<>^()[]{}$\``, `'` and `"` in host names before writing. A name containing `,` is truncated at the comma. Without `[FCS Network]`, the stored id never matches the name-based preselection (unclear legacy behaviour).
- **Related defaults and migrations**: these run on every POST, whether or not this section was rendered. See 3.1.

#### 2.3.2 `confHostFilesYSFUpper`: uppercase host files (lines 5124-5139)

- Toggle. Hidden default `OFF` at line 5060. `id="toggle-confHostFilesYSFUpper"`, label id `aria-toggle-confHostFilesYSFUpper`, `aria-label="Uppercase Host Files"`, JS `toggleHostFilesYSFUpper()`. The toggle cell has no colspan, and the next cell holds `Note: Update Required if changed`.
- **Display**: checked when `isset($configysfgateway['General']['WiresXMakeUpper'])` and it is truthy (`/etc/ysfgateway [General] WiresXMakeUpper`). Unchecked if the key is unset or falsy.
- **Save** (lines 1378-1382): `ON` sets `[General] WiresXMakeUpper="1"`; `OFF` sets `"0"`. Then, if `/etc/hostfiles.ysfupper` exists, `system('sudo rm -rf /etc/hostfiles.ysfupper')` (legacy marker removal).
- Default (line 2945): if the key is missing it is set to `"1"`, but only after the handler has run. In practice the handler always sets it while the section is visible.

#### 2.3.3 `wiresXCommandPassthrough`: WiresX passthrough (lines 5140-5154)

- Toggle. Hidden default `OFF` at line 5061. `id="toggle-confWiresXCommandPassthrough"`, label id `aria-toggle-confWiresXCommandPassthrough`, `aria-label="Wires-X Command Passthrough"`, JS `toggleWiresXCommandPassthrough()`. Cell `colspan="2"`.
- **Display**: checked when `isset([General] WiresXCommandPassthrough)` and it is truthy (`/etc/ysfgateway`).
- **Save** (lines 1372-1375): `ON` sets `[General] WiresXCommandPassthrough="1"`; `OFF` sets `"0"`. A missing key is appended to the end of `[General]`.

#### 2.3.4 `ysf2dmrId`: YSF2DMR DMR ID with ESSID (lines 5157-5186), YSF2DMR only

- **Display text**: `substr($configmmdvm['General']['Id'], 0, 7)` (`/etc/mmdvmhost [General] Id`), printed only if it is set and `!== "1234567"`.
- Base: `$ysf2dmrIdBase = substr([General] Id, 0, 7)`, or `"1234567"` if unset.
- ESSID detection: if `/etc/ysf2dmr [DMR Network] Id` is set and longer than `[General] Id`, then `$ysf2dmrESSID = substr(ysf2dmrId, -(len diff))` (the trailing digits). Otherwise `"None"`.
- `<select name="ysf2dmrId">`:
  - First option, label `None`. **Value depends on state**: when the ESSID is `"None"`, it is `value="<base>"` and selected. Otherwise it is the literal `value="None"`.
  - Then 01…99 (2-digit zero-padded): `value="<base><NN>"`, label `NN`. Selected when `$ysf2dmrESSID === "NN"` (strict). An ESSID that is not exactly 2 digits matches nothing, and the browser defaults to the first option.
- **Save** (lines 1500-1503): if not empty, `/etc/ysf2dmr [DMR Network] Id = preg_replace('/[^0-9]/','',value)`.
  - **Bug**: choosing `None` while an ESSID exists posts `None`, which becomes `""`, so `Id=` is written empty.
- Note: there is no length validation. The base comes from the **current** `/etc/mmdvmhost` value, not from the `dmrId` posted in the same request.

#### 2.3.5 `ysf2dmrMasterHost`: YSF2DMR DMR master (lines 5187-5203), YSF2DMR only

- `<select name="ysf2dmrMasterHost">`. Source `/usr/local/etc/DMR_Hosts.txt`, opened at line 5156. Each line is split with `preg_split('/\s+/')`, which gives `[0]=name, [1]=(unused here), [2]=address, [3]=password, [4]=port`.
- Included when `[0]` has no `#`, is not empty, and does not start with `XLX`, `DMRG` or `DMR2`.
- Option: `value="[2],[3],[4],[0]"`, label `[0]`. Selected when `/etc/ysf2dmr [DMR Network] Address == [2]`. **Only the address is compared, not the port**, so several options may carry `selected` and the browser keeps the last one. Selecting also sets the global `$dmrMasterNow` (line 5196); the DMR section has already rendered by then, so this has no visible effect.
- There is no "None" option. If nothing matches, the first host is preselected.
- **Save** (lines 1385-1403). If not empty: `Arr = explode(',', escapeshellcmd(value))`.
  - `/etc/ysf2dmr [DMR Network] Address = Arr[0]`.
  - `Password = '"'.Arr[1].'"'` **only if `[DMR Network] Password` is not already set**. Changing the master therefore keeps the old password unless `bmHSSecurity` is non-empty.
  - `Port = Arr[2]`.
  - `bmHSSecurity` handling is described in 2.3.6.
- Side effects from the MMDVMHost DMR master handler (`dmrMasterHost`, lines 1551-1570, other section):
  - When the MMDVMHost master is `BM*`: unset ysf2dmr `[DMR Network] Options` and `Local`.
  - When it is DMRGateway (`127.0.0.1:62031`): unset `Options` and set `Local=62032`.
  - For DMR+/HB/FD/FreeDMR/FreeSTAR: unset `Local`, and unset `Options` if `dmrNetworkOptions` is empty.

#### 2.3.6 `bmHSSecurity`: hotspot security password (lines 5204-5209), YSF2DMR only

- `<input type="password" name="bmHSSecurity" size="30" maxlength="30" value="<?php if (isset($configModem['BrandMeister']['Password'])) echo …; ?>"></input>`.
- **Display**: modem file (`/etc/dstar-radio.mmdvmhost`) `[BrandMeister] Password`, quotes stripped by `parse_ini_file`. Not HTML-escaped.
- **Duplicate POST name**: the DMR section can also render `bmHSSecurity` (line 4542 when the MMDVMHost master is DMRGateway, line 4762 when it is `BM*`). Since PHP keeps the last occurrence, **the YSF2DMR field (later in the form) wins** whenever it is present. All copies are prefilled with the same value, but an edit made only in the DMR-section copy is discarded.
- **Save** within the YSF2DMR master handler (lines 1392-1402), only when `ysf2dmrMasterHost` is posted and `isset($_POST['bmHSSecurity'])`:
  - Non-empty: `/etc/ysf2dmr [DMR Network] Password = '"'.raw.'"'` and modem `[BrandMeister] Password = '"'.raw.'"'`. The modem writer re-quotes it after stripping every `"` (line 3769).
  - Empty: `unset(modem [BrandMeister] Password)`, and `/etc/ysf2dmr Password='"'.Arr[1].'"'` only if unset (always false here, so effectively the existing password is kept).
- The same POST value is also used by other handlers:
  - MMDVMHost DMR master (lines 1529-1541): for `BM*` masters it sets the modem `[BrandMeister] Password` and `/etc/mmdvmhost [DMR Network] Password` (unless 127.0.0.1); empty unsets the modem key. Line 1558 copies it into mmdvmhost.
  - DMRGateway Net 1 (lines 1631-1633): `/etc/dmrgateway [DMR Network 1] Password`.
- There is no sanitisation. A `"` in the value is written raw into ysf2dmr.

#### 2.3.7 `ysf2dmrTg`: YSF2DMR startup TG (lines 5210-5213), YSF2DMR only

- `<input type="text" name="ysf2dmrTg" size="13" maxlength="7" value="<ysf2dmr [DMR Network] StartupDstId>" />` (empty if unset).
- **Save** (lines 1406-1409): if not empty, `/etc/ysf2dmr [DMR Network] StartupDstId = preg_replace('/[^0-9]/','',value)`.
- Default (line 2965): `StartupDstId=31672` if missing.

#### 2.3.8 `ysf2nxdnId`: YSF2NXDN NXDN ID (lines 5215-5219), YSF2NXDN only

- `<input type="text" name="ysf2nxdnId" size="13" maxlength="5" value="<ysf2nxdn [NXDN Network] Id>" />`.
- **Save** (lines 1425-1427): if not empty, `/etc/ysf2nxdn [NXDN Network] Id = digits only`. There is no range check; contrast this with `nxdnId`, which is capped at 65535 at line 1509.

#### 2.3.9 `ysf2nxdnStartupDstId`: YSF2NXDN startup host (lines 5220-5253), YSF2NXDN only

- `<select name="ysf2nxdnStartupDstId">`. The current value is `$testNXDNHost = $configysf2nxdn['NXDN Network']['StartupDstId']`; if unset it is null, which counts as `== ""`.
- Options:
  1. `none` / `None`, selected if the current value `== ""`.
  2. `10` / `10 - Parrot`, selected if `== "10"`.
  3. `/usr/local/etc/NXDNHosts.txt`: split on `/\s+/` (`[0]=TG id`, `[1]=second column`, the host address in the standard file). Lines whose `[0]` contains `#`, or whose `[0]` is empty, are skipped. `value="[0]"`, label `[0] - [1]`, selected when `== [0]`.
  4. `/usr/local/etc/NXDNHostsLocal.txt` (if it exists): same parsing, appended after.
- **Save** (lines 1412-1422). If not empty:
  - `/etc/ysf2nxdn [NXDN Network] StartupDstId = escapeshellcmd(value)`. **`none` is written literally as `StartupDstId=none`; it is not unset.** After a reload, `parse_ini_file` turns `none` into `""`, so None shows as selected again.
  - If `/etc/nxdngateway` exists: `none` unsets `[Network] Startup` and `[Network] Static`; otherwise it sets `[Network] Startup = escapeshellcmd(value)`.
  - Ordering: the NXDN-section handler `nxdnStartupHost` (lines 1290-1304) runs first and also writes `ysf2nxdn StartupDstId` and `nxdngateway Startup`. **This later handler wins** when both are posted.
  - Migration (lines 3108-3127): if `NXDNGateway -v` is newer than `20210131`, `[Network] Startup` is moved to `[Network] Static` (Startup unset).

#### 2.3.10 `ysf2p25Id`: YSF2P25 ID (lines 5255-5259), YSF2P25 only

- `<input type="text" name="ysf2p25Id" size="13" maxlength="7" value="<ysf2p25 [P25 Network] Id>" />`.
- **Save** (lines 1444-1446): if not empty, `/etc/ysf2p25 [P25 Network] Id = digits only`.

#### 2.3.11 `ysf2p25StartupDstId`: YSF2P25 startup host (lines 5260-5297), YSF2P25 only

- `<select name="ysf2p25StartupDstId">`. Current value: `ysf2p25 [P25 Network] StartupDstId`, or `""` if unset.
- Options: `none`/`None` (selected if `""`), `10`/`10 - Parrot` (selected if `"10"`). Then `/usr/local/etc/P25Hosts.txt` and, if it exists, `/usr/local/etc/P25HostsLocal.txt`, both split on `/\s+/`: `value="[0]"`, label `[0] - [1]`, skipping lines with `#` in `[0]` or an empty `[0]`. Selected when `== [0]`.
- **Save** (lines 1430-1441). If not empty, `$v = strtoupper(escapeshellcmd(value))`:
  - `NONE`: unset `/etc/p25gateway [Network] Startup`, `[Network] Static`, and `/etc/ysf2p25 [P25 Network] StartupDstId`.
  - otherwise: `/etc/p25gateway [Network] Startup = $v` and `/etc/ysf2p25 [P25 Network] StartupDstId = $v`.
  - Ordering: the P25-section handler `p25StartupHost` (lines 1269-1279) runs first and writes the same keys. **This later handler wins.**
  - Migration (lines 3090-3100): if `P25Gateway -v` is newer than `20210201`, `[Network] Startup` is moved to `[Network] Static`.

---

## 3. Every-save defaults and migrations touching YSF, DGId and D-Star files

These run on every POST, whether or not the section was visible. A native backend that wants identical results has to reproduce them, or explicitly decide to drop them.

### 3.1 YSFGateway (`/etc/ysfgateway`)

- Lines 2945-2954 (only if missing): `[General] WiresXMakeUpper=1`, `[Network] Revert=0`, `[Network] Port=42000`, `[Mobile GPS] Enable=0`, `Address=127.0.0.1`, `Port=7834`. Always: unset `[Network] YSF2DMRAddress`, `YSF2DMRPort`, `DataPort`, `StatusPort`.
- Line 3130: `$ysfGatewayVer = exec("YSFGateway -v | awk {'print $3'} | cut -c 1-8")`.
  - `> 20180303` (lines 3131-3154): keep `[Network] Startup`. Set `[aprs.fi] Enable=1` if missing, and `InactivityTimeout=0` and `Revert=0` if missing. Force `[Network] Debug=0`. Force `[YSF Network]`: `Enable=1`, `Port=42000`, `Hosts=/usr/local/etc/YSFHosts.txt`, `ReloadTime=60`, `ParrotAddress=127.0.0.1`, `ParrotPort=42012`, `YSF2DMRAddress=127.0.0.1`, `YSF2DMRPort=42013`, `YSF2NXDNAddress=127.0.0.1`, `YSF2NXDNPort=42014`, `YSF2P25Address=127.0.0.1`, `YSF2P25Port=42015`. Force `[FCS Network]`: `Enable=1`, `Port=42001`, `Rooms=/usr/local/etc/FCSHosts.txt`.
  - `> 20200502`: `[Remote Commands] Enable=1`, `Port=6073` if missing.
  - `> 20200907`: if missing, `[General] Debug=0`, `[GPSD] Enable=0/Address=127.0.0.1/Port=2947`, `[APRS] Enable=0/Address=127.0.0.1/Port=8673/Suffix=Y`. Always `[APRS] Description = [Info] Name . "_" . [General] Suffix`. Unset `[Mobile GPS]` and `[aprs.fi]`.
  - `> 20210130`: `[APRS] Enable=1` if the key is set.
- The `[FCS Network]` check in `ysfStartupHost` (2.3.1) uses the file as loaded, before these migrations.

### 3.2 YSF2DMR (`/etc/ysf2dmr`), lines 2957-2991

If missing:

- `[Info]`: `Power=1`, `Height=0`.
- `[YSF Network]`: `DstAddress=127.0.0.1`, `DstPort=42000`, `LocalAddress=127.0.0.1`, `LocalPort=42013`, `Daemon=1`, `EnableWiresX=1`, `WiresXMakeUpper=1`, `DT1=1,34,97,95,43,3,17,0,0,0`, `DT2=0,0,0,0,108,32,28,32,3,8`.
- `[DMR Network]`: `StartupDstId=31672`, `StartupPC=0`, `Jitter=500`, `EnableUnlink=1`, `TGUnlink=4000`, `PCUnlink=0`, `Debug=0`, and `TGListFile=/usr/local/etc/TGList_BM.txt` (only if that file exists).
- `[DMR Id Lookup]`: `File=/usr/local/etc/DMRIds.dat`, `Time=24`, `DropUnknown=0`.
- `[Log]`: `FilePath=/var/log/pi-star`, `FileRoot=YSF2DMR`.
- `[aprs.fi]`: `Enable=0`, `Port=14580`, `Refresh=240`.
- `[Enabled]`: `Enabled=0`.

Always: unset `[Info] Enabled` and `[DMR Network] JitterEnabled`; force `[Log] DisplayLevel=0` and `FileLevel=0`.

### 3.3 YSF2NXDN and YSF2P25, lines 2994-3025

- ysf2nxdn always:
  - `[YSF Network]`: `LocalPort = ysfgateway [YSF Network] YSF2NXDNPort`, `DstPort = ysfgateway [YSF Network] Port`, `Daemon=1`, `EnableWiresX=1`.
  - `[NXDN Id Lookup] File=/usr/local/etc/NXDN.csv`.
  - `[NXDN Network] TGListFile=/usr/local/etc/TGList_NXDN.txt`.
  - `[Log]`: `DisplayLevel=0`, `FileLevel=0`, `FilePath=/var/log/pi-star`, `FileRoot=YSF2NXDN`.

  If missing: `[Enabled] Enabled=0`, `[aprs.fi] Enable=0`, `[YSF Network] WiresXMakeUpper=1`, `DT1`, `DT2` (same values as YSF2DMR).

  These values are read from the ysfgateway array **before** the 3.1 migrations force them, so they are the values from the file as loaded, apart from the earlier handler edits.
- ysf2p25 always:
  - `[YSF Network]`: `LocalPort = ysfgateway YSF2P25Port`, `DstPort = ysfgateway [YSF Network] Port`, `Daemon=1`, `EnableWiresX=1`.
  - `[DMR Id Lookup] File=/usr/local/etc/DMRIds.dat`.
  - `[P25 Network] TGListFile=/usr/local/etc/TGList_P25.txt`.
  - `[Log]`: `DisplayLevel=0`, `FileLevel=0`, `FilePath=/var/log/pi-star`, `FileRoot=YSF2P25`.
  - Unset `[aprs.fi]`.

  If missing: `[Enabled] Enabled=0`, `WiresXMakeUpper=1`, `DT1`, `DT2`.
- Caveat: `/etc/ysf2dmr`, `/etc/ysf2nxdn` and `/etc/ysf2p25` are written whenever the generated text passes the line threshold, **even if the file did not exist before** (no `file_exists` guard; lines 3497, 3533, 3569).

### 3.4 DGIdGateway (`/etc/dgidgateway`, only if it exists), lines 3027-3037

- `[General]`: `LocalPort = mmdvmhost [System Fusion Network] GatewayPort`, `RptPort = mmdvmhost [System Fusion Network] LocalPort`.
- `[Log]`: `DisplayLevel=1`, `FileLevel=1`, `FilePath=/var/log/pi-star`, `FileRoot=DGIdGateway`, `FileRotate=0`.
- `[YSF Network] Hosts=/usr/local/etc/YSFHosts.txt`.

The D-Star and YSF sections have **no DGIdGateway form field**. DGId values are otherwise written only from General and MMDVMHost fields (callsign, ID, lat/lon, description, frequency/suffix, YSF hang times).

### 3.5 DMR2YSF (`/etc/dmr2ysf`), lines 2934-2942

If missing:

- `[YSF Network]`: `FCSRooms=/usr/local/etc/FCSHosts.txt`, `DT1`, `DT2`, `Debug=0`.
- `[DMR Network]`: `DefaultDstTG=9`, `TGUnlink=4000`, `TGListFile=/usr/local/etc/TGList_YSF.txt`.

Always: `[Log] DisplayLevel=0`, `FileLevel=2`. `DefaultDstTG` itself is set by `ysfStartupHost` (2.3.1).

### 3.6 ircDDBGateway and timeserver

- Every save: `/etc/timeserver sendA..sendE=0` (lines 882-886), then `send<Module>=1` (1.3.1).
- Every save: the `ircddbEnabled2=0` insertion when it is missing (1.3.7), and the random `ccsHost` when `ccsEnabled=1` (1.3.4).

---

## 4. Section-specific notes and quirks (summary)

1. Both sections end with an `Apply Changes` button that submits the **entire** configuration form. There is no per-section save.
2. The hidden `OFF` defaults exist only while the section is rendered. A hidden section posts nothing, so its handlers are skipped. The exception is `confDStarModuleSuffix`, whose absence changes behaviour: the module is re-derived from the frequency band.
3. `confDefRef` is posted by either the select or the text box. They share a name, and only the enabled one is submitted.
4. `toggleircddbEnabled()` is referenced but not defined in `functions.js`.
5. `bmHSSecurity` has duplicate inputs across the DMR and YSF2DMR rows. The last one in DOM order wins.
6. `ysf2dmrId`: picking "None" while an ESSID exists writes an empty `[DMR Network] Id` (bug).
7. `ysf2dmrMasterHost` keeps the previous `Password` when the master changes, unless a custom password is supplied.
8. `ysf2nxdnStartupDstId=none` is written literally to `/etc/ysf2nxdn`, while `ysf2p25StartupDstId=none` unsets the key.
9. `ysfStartupHost` values pass through `escapeshellcmd`, which can inject backslashes into names with shell metacharacters.
10. `/etc/timeserver.dissable` (misspelt) is the marker that turns announcements **off**.
11. HTML attribute values (`remotePassword`, `bmHSSecurity`, YSF option values) are echoed without escaping. A native implementation should escape them; this does not change behaviour for normal values.

---

## Services/commands touched by this section on Apply

- `sudo mount -o remount,rw /` at the start; `sudo sync` x3 and `sudo mount -o remount,ro /` at the end.
- `sudo systemctl stop`, then `start`: `dstarrepeater`, `mmdvmhost`, `ircddbgateway`, `timeserver`, `ysfgateway`, `ysfparrot`, `ysf2dmr`, `ysf2nxdn`, `ysf2p25`, `dmr2ysf`, `nxdngateway`, `p25gateway`, `aprsgateway` (with every other DV service in the global envelope), plus `cron`. `sudo systemctl daemon-reload` runs before the starts.
- `sudo rm -rf /etc/timeserver.dissable` / `sudo touch /etc/timeserver.dissable` (`confTimeAnnounce`).
- `sudo touch /etc/hostfiles.nodextra` / `sudo rm -rf /etc/hostfiles.nodextra` (`confHostFilesNoDExtra`).
- `sudo rm -rf /etc/hostfiles.ysfupper` (`confHostFilesYSFUpper`, if the legacy marker exists).
- `sudo install -m 644 -o root -g root` into `/etc/ircddbgateway`, `/etc/timeserver`, `/etc/dstarrepeater` (config_writer commit, plus the immediate `ircddbEnabled2` insertion), `/etc/ysfgateway`, `/etc/ysf2dmr`, `/etc/ysf2nxdn`, `/etc/ysf2p25`, `/etc/dgidgateway`, `/etc/dmr2ysf`, `/etc/nxdngateway`, `/etc/p25gateway`, `/etc/mmdvmhost` (`[D-Star] Module`) and the modem file `/etc/dstar-radio.mmdvmhost` (`[BrandMeister] Password`).
- `sudo -n cat '/root/.Remote Control'` and `sudo install -m 600 -o root -g root … '/root/.Remote Control'` (`confPassword`).
- Version probes used by the migrations: `YSFGateway -v`, `P25Gateway -v`, `NXDNGateway -v` (each piped through `awk {'print $3'} | cut -c 1-8`).
