# configure.php spec, section 02: DMR Configuration

Reference: `Pi-Star_DV_Dash/admin/configure.php` at revision `10bf34c9` (5765 lines).
Form block: lines 4488-4867. POST handling: lines 1384-1716, 2462-2463, 2621-2624, 2682-2749, 2768-2771, 3078-3082, plus the file writers at 3274-3312 (`/etc/mmdvmhost`), 3722-3758 (`/etc/dmrgateway`) and 3760-3824 (`/etc/dstar-radio.mmdvmhost`).
JS toggle helpers: `Pi-Star_DV_Dash/functions.js` lines 261-315. Labels: `Pi-Star_DV_Dash/lang/english_uk.php`.

All line numbers below refer to `admin/configure.php` unless a file is named.

---

## 0. Data sources used by this section

| Variable | Source | Loaded at | Notes |
|---|---|---|---|
| `$configmmdvm` | `/etc/mmdvmhost` | 119-120 | `parse_ini_file($f, true)` |
| `$configdmrgateway` | `/etc/dmrgateway` | 198-200 | only if `fopen()` succeeds |
| `$configModem` | `/etc/dstar-radio.mmdvmhost` (or `/etc/dstar-radio.dstarrepeater` if only that exists) | 211-219 | the "modem" marker file; it has `[BrandMeister] Password=` and `[TGIF] Password=` |
| `$configysf2dmr`, `$configdmr2ysf`, `$configdmr2nxdn` | `/etc/ysf2dmr`, `/etc/dmr2ysf`, `/etc/dmr2nxdn` | 129, 153, 159 | the DMR master POST handler changes these as side effects |
| host list | `/usr/local/etc/DMR_Hosts.txt` | 4489, 4525, 4599, 4666 | opened again for every select |

`parse_ini_file()` uses the default `INI_SCANNER_NORMAL` mode. It strips surrounding double quotes, and it turns unquoted `true/on/yes` into `"1"` and `false/off/no/none` into `""`. The writers re-emit values exactly as stored (`key=value`), with no re-quoting. The only re-quoting is the explicit `'"'.x.'"'` code shown below, the dmrgateway fix-ups at 2738-2749, and the modem-file writer for `Password` keys (3769). So a value that was quoted on disk and is not touched by the handler is written back **unquoted**. For example, `[DMR Network] Password` in `/etc/mmdvmhost` is written back unquoted when no branch re-sets it.

### DMR_Hosts.txt parsing rule (identical in all four selects)

```php
$line = fgets($fh);                       // raw line incl. trailing newline
$col  = preg_split('/\s+/', $line);       // whitespace split
// col[0]=Name  col[1]=(unused here)  col[2]=Address  col[3]=Password  col[4]=Port
```

- A line is skipped if `strpos($col[0], '#') !== FALSE` (a `#` **anywhere** in the first token, not only at the start) or `$col[0] == ''` (blank lines, and lines that start with whitespace).
- Option `value` = `"{col[2]},{col[3]},{col[4]},{col[0]}"`, i.e. `address,password,port,name`. Option text = `col[0]`.
- Options keep file order. There is no sorting and no de-duplication.
- Values are echoed unescaped. Nothing is HTML-escaped.
- Column 1 is never used by this page.

Well-known special entries (from the upstream host file, matched by the POST handler on address+port):
- `DMRGateway` -> `127.0.0.1` port `62031` (the "use DMRGateway" master)
- `DMR2YSF` -> `127.0.0.2` port `62033`
- `DMR2NXDN` -> `127.0.0.3` port `62035`

The repo fixture `Pi-Star_dash_new/qa/classic-runtime/fixtures/DMR_Hosts.txt` holds only one demo line (`QA_Demo 0000 fixture.invalid unused 62031`). It confirms the 5-column layout.

---

## 1. Section visibility

```php
4488 <?php if (file_exists('/etc/dstar-radio.mmdvmhost') && $configmmdvm['DMR']['Enable'] == 1) {
4489     $dmrMasterFile = fopen("/usr/local/etc/DMR_Hosts.txt", "r"); ?>
...
4867 <?php } ?>
```

- The whole section (heading, hidden inputs, table, Apply button) renders only when the controller is MMDVMHost (marker file `/etc/dstar-radio.mmdvmhost` exists) **and** `/etc/mmdvmhost [DMR] Enable == 1`.
- There is no modem-type condition.
- The section is part of `<form id="config" method="post">` (line 4006), which every section of the page shares.
- Everything in it is rendered on the server from the **saved** state. No JS shows or hides rows when the user changes a select. Changing the DMR master and pressing Apply saves the master. The rows that belong to the new master type (passwords, Options, ESSID, DMRGateway sub-networks) appear only after the page reloads.

### Derived variable `$dmrMasterNow` (drives all sub-row visibility)

This variable is set while the main `dmrMasterHost` select is built (4512). It holds `col[0]` (the name) of the first host line whose `col[2] == [DMR Network] Address` **and** `col[4] == [DMR Network] Port` from `/etc/mmdvmhost`.
- If no line matches, `$dmrMasterNow` stays undefined (it behaves as `""`/null).
- XLX lines are excluded from this scan.
- If several host lines share address and port, the **last** match wins for `$dmrMasterNow`. All matching options get `selected`, so the browser shows the last one too.

| Condition on `$dmrMasterNow` | Rows rendered (form order) |
|---|---|
| `== "DMRGateway"` (4520) | BrandMeister Master, BM Hotspot Security, BrandMeister Network ESSID, BrandMeister Network Enable, BrandMeister Network (links), DMR+ Master, DMR+ Network (Options), DMR+ Network ESSID, DMR+ Network Enable, XLX Master, [XLX Startup TG], [XLX Startup Module], XLX Master Enable |
| `substr(...,0,2) == "BM"` (4759) | Hotspot Security (`bmHSSecurity`), BrandMeister Network (links) |
| `substr(...,0,8) == "FreeDMR_"` (4772) | DMR Options (`dmrNetworkOptions`), FreeDMR Help (links) |
| `substr(...,0,4)=="DMR+"` or `substr(...,0,3)=="HB_"` or `substr(...,0,3)=="FD_"` or `substr(...,0,9)=="FreeSTAR_"` (4788) | DMR Options (`dmrNetworkOptions`) |
| `substr(...,0,4) == "TGIF"` (4797) | Hotspot Security (`tgifHSSecurity`) |
| `!== "DMRGateway"` (4807) | DMR ESSID (`dmrExtendedId`). This includes the undefined/no-match case. |
| always | DMR Colour Code, DMR EmbeddedLCOnly, DMR DumpTAData |

The conditions are independent `if`s, but the name prefixes do not overlap, so at most one of the BM / FreeDMR_ / DMR+-family / TGIF blocks renders.

---

## 2. Layout

- `<h2>` = `$lang['dmr_config']` = **"DMR Configuration"** (4490).
- Six hidden inputs come right after the heading (4491-4496), outside the table.
- One `<table>` (4497-4865) with two columns. There are **no colspans** in this section.
  - Header row: `<th width="200">` "Setting" with tooltip `<b>Setting</b>`; `<th>` "Value" with tooltip `<b>Value</b>The current value from the<br />configuration files` (4499-4500).
  - Every data row: `<td align="left">` with `<a class="tooltip2" href="#">LABEL:<span><b>TITLE</b>TEXT</span></a>`, then a value `<td>` (`style="text-align: left;"` for selects, `align="left"` for the rest).
- After the table: `<div><input type="button" value="Apply Changes" onclick="submitform()" /><br /><br /></div>` (4866). `submitform()` (274-278) disables every `input[type=button]` on the page and submits `#config`.

Markup defects to be aware of (do not reproduce, but they explain the rendered DOM):
- The TGIF block (4797-4804) ends with an extra `<tr>` that it never closes. If ESSID is also shown, that `<tr>` gets a second `<tr>` opened right after it (4808), which produces an empty row.
- The ESSID block ends with `<tr>` (4836), which opens the Colour Code row. When `$dmrMasterNow == "DMRGateway"` the ESSID block is skipped, so the Colour Code row's `<td>`s (4838) are emitted with **no** opening `<tr>`. The browser repairs this with an implied row.
- Commented-out JitterBuffer row (4858-4864). The HTML comment ends with `</td></tr>. -->`. Nothing is rendered except inside the comment.

### Toggle markup (used by 5 toggles)

```html
<div class="switch">
  <input id="toggle-NAME" class="toggle toggle-round-flat" type="checkbox" name="NAME" value="ON"
         [checked="checked"] aria-hidden="true" tabindex="-1" [onclick="JSFN()" only for Chrome UA] />
  <label id="aria-toggle-NAME" role="checkbox" tabindex="0" aria-label="ARIA" aria-checked="true|false"
         onKeyPress="JSFN()" onclick="JSFN()" for="toggle-NAME"><font style="font-size:0px">HIDDEN TEXT</font></label>
</div>
```

- The input gets the `onclick` attribute only when `HTTP_USER_AGENT` contains `Chrome` (3951-3992, the `$toggle...Cr` variables).
- `JSFN()` (functions.js 261-315) only flips the label's `aria-checked` between "true" and "false". It then runs `if(event.keyCode == '32') label.click()`. Because the handlers are called with no argument, `event` is undefined and that line throws after the aria flip. The throw is harmless.
- The checkbox itself toggles natively through `<label for>`.
- Unchecked state: the hidden input with the same name posts `OFF`. Checked: the checkbox comes later in the form, and PHP keeps the last value, so `ON` is posted.

---

## 3. Controls in form order

### 3.1 Hidden inputs (4491-4496), always posted when the section renders

| # | POST name | value | Purpose |
|---|---|---|---|
| H1 | `dmrEmbeddedLCOnly` | `OFF` | default for toggle 3.19 |
| H2 | `dmrDumpTAData` | `OFF` | default for toggle 3.20 |
| H3 | `dmrGatewayXlxEn` | `OFF` | default for toggle 3.13 |
| H4 | `dmrGatewayNet1En` | `OFF` | default for toggle 3.5 |
| H5 | `dmrGatewayNet2En` | `OFF` | default for toggle 3.9 |
| H6 | `dmrDMRnetJitterBufer` | `OFF` | its toggle is commented out and its handler is commented out (1680-1684), so it has no effect |

**Important side effect:** H3/H4/H5 are posted even when `$dmrMasterNow != "DMRGateway"`, when their toggles are not rendered. Pressing Apply in any direct-master mode therefore writes `/etc/dmrgateway [DMR Network 1] Enabled=0`, `[DMR Network 2] Enabled=0`, `[XLX Network] Enabled=0` (and `[XLX Network 1] Enabled=0`) (1699-1715).

---

### 3.2 DMR Master (MMDVMHost): `dmrMasterHost` (4502-4519)

- Type: `<select name="dmrMasterHost">`. No size, no onchange.
- Label: `$lang['dmr_master']`. `english_uk.php` defines this key twice: line 72 `"DMR Master (MMDVMHost)"` and line 121 `"DMR Master"`. PHP keeps the **last** one, so the label is **"DMR Master:"**.
- Tooltip: `<b>DMR Master (MMDVMHost)</b>Set your prefered DMR master here`.
- Options: every DMR_Hosts.txt line (rule in §0) **except** names starting with `XLX` (`substr(col0,0,3) != "XLX"`).
- Preselected: options where `col[2] == /etc/mmdvmhost [DMR Network] Address` **and** `col[4] == [DMR Network] Port`. There is no "none" option. If nothing matches, the browser selects the first option, and Apply would then save that first host.
- Always visible within the section.

**Save (1512-1624)**, runs when `$_POST['dmrMasterHost']` is non-empty:

```php
$arr = explode(',', escapeshellcmd($_POST['dmrMasterHost']));   // [0]addr [1]pass [2]port [3]name
```

`escapeshellcmd` is applied to the whole string before the split. It backslash-escapes shell metacharacters (``#&;`|*?~<>^()[]{}$\``, `\x0A`, `\xFF`, and unpaired quotes) in the stored values.

`/etc/mmdvmhost [DMR Network]`:
1. `Address = arr[0]`, `RemoteAddress = arr[0]`.
2. `Password = '"'.arr[1].'"'` **only if `Password` is not already set** (1517). Switching masters keeps the old password.
3. `Port = arr[2]`, `RemotePort = arr[2]`.
4. `Type = "Gateway"` if `arr[0]=='127.0.0.1' && arr[2]=='62031'`, otherwise `Type = "Direct"` (1522-1528).
5. BM password, if `isset($_POST['bmHSSecurity'])` and `substr(arr[3],0,2)=="BM"` (1529-1541):
   - non-empty: `/etc/dstar-radio.mmdvmhost [BrandMeister] Password = '"'.$_POST['bmHSSecurity'].'"'`. Also, when `arr[0] != '127.0.0.1'`, mmdvmhost `Password = '"'.$_POST['bmHSSecurity'].'"'`. The raw POST is used: no trimming, no sanitising.
   - empty: `unset([BrandMeister] Password)` in the modem file. The mmdvmhost `Password` is kept if set, otherwise set to `'"'.arr[1].'"'`.
6. TGIF password, if `isset($_POST['tgifHSSecurity'])` and `substr(arr[3],0,4)=="TGIF"` (1542-1549):
   - non-empty: modem `[TGIF] Password = '"'.$_POST['tgifHSSecurity'].'"'`. When `arr[0] != '127.0.0.1'`, mmdvmhost `Password = '"'.$_POST['tgifHSSecurity'].'"'`.
   - empty: `unset` modem `[TGIF] Password`. The mmdvmhost password is unchanged.
7. Name starts with `BM` (1551-1561):
   - `unset` mmdvmhost `[DMR Network] Options`, `Local`, `LocalPort`.
   - `unset` dmrgateway `[DMR Network 2] Options`.
   - `unset` ysf2dmr `[DMR Network] Options`, `Local`.
   - Then, if modem `[BrandMeister] Password` is set: mmdvmhost `Password = '"'.str_replace('"','',modemBMPassword).'"'`.
8. DMRGateway (`127.0.0.1`/`62031`) (1564-1577):
   - `unset` mmdvmhost `Options` and dmrgateway `[DMR Network 2] Options`.
   - mmdvmhost `Local="62032"`, `LocalPort="62032"`.
   - `unset` ysf2dmr `[DMR Network] Options`; ysf2dmr `[DMR Network] Local="62032"`.
   - If set: dmr2ysf `[DMR Network] LocalAddress="127.0.0.1"`, and dmr2nxdn `[DMR Network] LocalAddress="127.0.0.1"`.
9. DMR2YSF (`127.0.0.2`/`62033`) (1580-1587):
   - `unset` mmdvmhost `Options`.
   - `Local="62034"`, `LocalPort="62034"`.
   - If set: dmr2ysf `LocalAddress="127.0.0.2"`.
10. DMR2NXDN (`127.0.0.3`/`62035`) (1590-1597):
    - `unset` mmdvmhost `Options`.
    - `Local="62036"`, `LocalPort="62036"`.
    - If set: dmr2nxdn `LocalAddress="127.0.0.3"`.
11. Name starts with `TGIF` (1600-1602): `unset` mmdvmhost `Options`.
12. Name starts with `DMR+`, `HB_`, `FD_`, `FreeDMR_` or `FreeSTAR_` (1605-1619):
    - `unset` mmdvmhost `Local`, `LocalPort`, and ysf2dmr `Local`.
    - If `$_POST['dmrNetworkOptions']` is non-empty: `$s = str_replace('"','',POST)`. Then mmdvmhost `[DMR Network] Options = '"'.$s.'"'` **and** dmrgateway `[DMR Network 2] Options = '"'.$s.'"'`.
    - Otherwise: `unset` mmdvmhost `Options`, dmrgateway `[DMR Network 2] Options`, ysf2dmr `[DMR Network] Options`.

If `$_POST['dmrMasterHost']` is empty or not posted (1621-1624): `unset` mmdvmhost `[DMR Network] Options` and dmrgateway `[DMR Network 2] Options`. This happens on **every** Apply where the DMR section is hidden, for example when DMR mode is off.

Other branches do not touch `Local`/`LocalPort` for hosts that are not BM, DMRGateway, DMR2YSF, DMR2NXDN, or the DMR+ family. A plain TGIF or other direct host keeps whatever `Local` was there.

---

### 3.3 BrandMeister Master: `dmrMasterHost1` (4521-4538). Visible only if `$dmrMasterNow == "DMRGateway"`

- Type: `<select name="dmrMasterHost1">`.
- Label: `$lang['bm_master']` = **"BrandMeister Master:"**. Tooltip: `<b>BrandMeister Master</b>Set your prefered DMR master here`.
- Options: DMR_Hosts.txt lines with `substr(col0,0,2) == "BM"`.
- Preselected: `col[2] == /etc/dmrgateway [DMR Network 1] Address` and `col[4] == [DMR Network 1] Port`. If nothing matches, the first option is shown.

**Save (1625-1636)**, if non-empty: `arr1 = explode(',', escapeshellcmd(POST))`. In `/etc/dmrgateway [DMR Network 1]`:
- `Address = arr1[0]`
- `Password = '"'.arr1[1].'"'` **only if not already set**
- if `$_POST['bmHSSecurity']` is non-empty: `Password = '"'.$_POST['bmHSSecurity'].'"'` (raw)
- `Port = arr1[2]`
- `Name = arr1[3]`

---

### 3.4 BM Hotspot Security: `bmHSSecurity` (variant A, 4539-4544). Visible only if `$dmrMasterNow == "DMRGateway"`

- Type: `<input type="password" name="bmHSSecurity" size="30" maxlength="30">`.
- Label: literal **"BM Hotspot Security:"** (not from lang). Tooltip: `<b>BrandMeister Password</b>Override the Password for BrandMeister with your own custom password, make sure you already configured this using BM Self Care. Empty the field to use the default.`
- Value: `/etc/dstar-radio.mmdvmhost [BrandMeister] Password` if set (quotes already stripped by parse_ini), otherwise empty. The value is not HTML-escaped.
- Save:
  - In this mode the master name is `DMRGateway`, so the BM branch of 3.2 step 5 does **not** run. The modem file's `[BrandMeister] Password` is **neither updated nor cleared**.
  - The only write is 3.3: when non-empty, it sets dmrgateway `[DMR Network 1] Password`.
  - An empty value does not restore the default if a Network 1 password already exists.
  - The field shows the modem-file value, but it saves to dmrgateway. That mismatch is real classic behaviour.
- Also read by the YSF2DMR handler (1392-1402). See §5.

---

### 3.5 BrandMeister Network ESSID: `bmExtendedId` (4545-4580). DMRGateway mode only

- Label: `$lang['bm_network']` + " ESSID:" = **"BrandMeister Network ESSID:"**. Tooltip: `<b>BrandMeister Extended ID</b>This is the extended ID, to make your DMR ID 9 digits long`.
- Cell content: a text prefix, then `<select name="bmExtendedId">`.
  - Prefix: `substr([General] Id, 0, 7)` from `/etc/mmdvmhost`. It is printed only if `[General] Id` is set and `!== "1234567"`.
- Options: `None` (value `None`), then `01` … `99` (zero-padded, value = text).
- Preselection (4549-4565):
  - If `/etc/dmrgateway [DMR Network 1] Id` is set: when `strlen(Net1 Id) > strlen([General] Id)`, the ESSID is the last 2 characters of Net1 Id; otherwise `None`.
  - Else, if `[General] Id` is set and `strlen == 9`: the last 2 characters of `[General] Id`.
  - Else `None`.
  - The comparison with each option is strict (`===`) against the 2-char string.
- Save (1487-1491), if non-empty: `$e = preg_replace('/[^0-9]/','',POST)`. `"None"` becomes `""`. Then `/etc/dmrgateway [DMR Network 1] Id = [General] Id . $e`.
  - `[General] Id` is the value after the General-section `dmrId` handler (1468-1479) has run. That handler runs first and truncates the ID to 7 digits.

---

### 3.6 BrandMeister Network Enable: `dmrGatewayNet1En` (4581-4587). DMRGateway mode only

- Checkbox toggle (markup §2). id `toggle-dmrGatewayNet1En`, aria-label `Enable BrandMeister DMR`, hidden text `Enable Brandmeister DMR`, JS `toggleDmrGatewayNet1EnCheckbox()`.
- Label: **"BrandMeister Network Enable:"**. Tooltip: `<b>BrandMeister Network Enable</b>Enable or disable BrandMeister Network`.
- Checked if `/etc/dmrgateway [DMR Network 1] Enabled == 1`.
- Save (1711-1715): `ON` gives `[DMR Network 1] Enabled = "1"`. `OFF` (hidden H4) gives `"0"`.

### 3.7 BrandMeister Network links row (4588-4594). DMRGateway mode only. Not a control

- Label: **"BrandMeister Network:"**. Tooltip: `<b>BrandMeister Dashboards</b>Direct links to your BrandMeister Dashboards`.
- Value `<td>` holds two links with `target="_new" style="color: #000;"`, separated by ` | `:
  - `Device Information`
  - `Edit Device (BrandMeister Selfcare)`
- Both have the same href, `https://brandmeister.network/#/device/{ID}`. ID = `/etc/dmrgateway [DMR Network 1] Id` if set, otherwise `/etc/mmdvmhost [General] Id`.

---

### 3.8 DMR+ Master: `dmrMasterHost2` (4595-4612). DMRGateway mode only

- `<select name="dmrMasterHost2">`. Label `$lang['dmr_plus_master']` = **"DMR+ Master:"**. Tooltip: `<b>DMR+ Master</b>Set your prefered DMR master here`.
- Options: DMR_Hosts.txt lines with `substr(col0,0,4) == "DMR+"`.
- Preselected: `col[2] == /etc/dmrgateway [DMR Network 2] Address` and `col[4] == [DMR Network 2] Port`.
- Save (1637-1651), if non-empty: `arr2 = explode(',', escapeshellcmd(POST))`. In `/etc/dmrgateway [DMR Network 2]`:
  - `Address = arr2[0]`
  - `Password = '"'.arr2[1].'"'` (always overwritten)
  - `Port = arr2[2]`
  - `Name = arr2[3]`
  - Then, if `$_POST['dmrNetworkOptions']` is non-empty: `unset` mmdvmhost `[DMR Network] Options`, and `[DMR Network 2] Options = '"'.str_replace('"','',POST).'"'`.
  - Otherwise: `unset([DMR Network 2] Options)`.
  - In DMRGateway mode, 3.2 step 8 has already unset both Options. This block runs after it and re-creates the dmrgateway one.

### 3.9 DMR+ Network Options: `dmrNetworkOptions` (variant A, 4613-4618). DMRGateway mode only

- Cell: literal text `Options=` followed by `<input type="text" name="dmrNetworkOptions" size="65" maxlength="250">`.
- Label: `$lang['dmr_plus_network']` = **"DMR+ Network:"**. Tooltip: `<b>DMR+ Network</b>Set your options= for DMR+ here`.
- Value: `/etc/dmrgateway [DMR Network 2] Options` if set (quotes stripped). Not HTML-escaped.
- Save: see 3.8. `"` characters are stripped and the result is wrapped in quotes.

### 3.10 DMR+ Network ESSID: `dmrPlusExtendedId` (4619-4654). DMRGateway mode only

- Label: **"DMR+ Network ESSID:"**. Tooltip: `<b>DMR Plus Extended ID</b>This is the extended ID, to make your DMR ID 8 digits long`. The tooltip says 8 digits, but the options are 2-digit, so the result is 9 digits.
- Same prefix text and options as 3.5 (`None`, `01`-`99`).
- Preselection: the same algorithm as 3.5, using `/etc/dmrgateway [DMR Network 2] Id`.
- Save (1493-1497): `[DMR Network 2] Id = [General] Id . preg_replace('/[^0-9]/','',POST)`.

### 3.11 DMR+ Network Enable: `dmrGatewayNet2En` (4655-4661). DMRGateway mode only

- Toggle. id `toggle-dmrGatewayNet2En`, aria-label `Enable DMR+`, hidden text `Enable DMR+`, JS `toggleDmrGatewayNet2EnCheckbox()`.
- Label **"DMR+ Network Enable:"**. Tooltip: `<b>DMR+ Network Enable</b>` (title only, no text).
- Checked if `/etc/dmrgateway [DMR Network 2] Enabled == 1`.
- Save (1705-1709): `[DMR Network 2] Enabled = "1"` or `"0"`.

---

### 3.12 XLX Master: `dmrMasterHost3` (4662-4680). DMRGateway mode only

- `<select name="dmrMasterHost3">`. Label `$lang['xlx_master']` = **"XLX Master:"**. Tooltip: `<b>XLX Master</b>Set your prefered XLX master here`.
- Options: DMR_Hosts.txt lines with `substr(col0,0,3) == "XLX"`. The value format is the same as the other selects.
- Current value `$cur` (4667-4668): `/etc/dmrgateway [XLX Network 1] Address` if set. It is then **overridden** by `[XLX Network] Startup` if that is set. Normally only `Startup` exists, because `[XLX Network 1]` is deleted on save (§4).
- Per-line emission (4673-4675). This has a bug: an option can be emitted twice.
  ```php
  if ($cur == col[2])            echo <option selected>;   // A
  if ('XLX_'.$cur == col[0])     echo <option selected>;   // B
  else                           echo <option>;            // C
  ```
  So the selected option is the one whose name is `"XLX_".Startup` (e.g. `XLX_950` when `Startup=950`). If A matches but B does not, the option appears twice (selected, then unselected). If `$cur` is undefined, no option is selected and the first one is shown.
- Save (1652-1659), if non-empty: `arr3 = explode(',', escapeshellcmd(POST))`.
  - `/etc/dmrgateway [XLX Network 1]`: `Address=arr3[0]`, `Password='"'.arr3[1].'"'`, `Port=arr3[2]`, `Name=arr3[3]`.
  - `[XLX Network] Startup = substr(arr3[3], 4)`, i.e. the name without the `XLX_` prefix (`XLX_950` becomes `950`).
  - `[XLX Network 1]` is then removed at 3078-3082 when `DMRGateway -v` reports a version greater than `20170924` (all current builds). So in practice only `[XLX Network] Startup` persists.

### 3.13 XLX Startup TG: `dmrMasterHost3Startup` (4681-4703). Only if DMRGateway mode **and** `isset([XLX Network 1] Startup)`

This is a legacy row. It is effectively never shown on current builds, because `[XLX Network 1]` is deleted on every save.
- Label literal **"XLX Startup TG:"**. Tooltip: `<b>XLX Startup TG</b>`.
- Options:
  - `None`. It is selected only if `[XLX Network 1] Startup` is **not** set. That branch is dead code, because the row only renders when it is set.
  - Then `4001` … `4026` (`'40'.sprintf('%02d',1..26)`). The one equal to `[XLX Network 1] Startup` is selected.
- Save (1661-1668), if non-empty: `$v = escapeshellcmd(POST)`. If `$v != "None"`, `[XLX Network 1] Startup = $v`; otherwise `unset([XLX Network 1] Startup)`. Either way it is later removed together with `[XLX Network 1]`.

### 3.14 XLX Startup Module: `dmrMasterHost3StartupModule` (4704-4749). Only if DMRGateway mode **and** `isset(/etc/dmrgateway [XLX Network] TG)`

`TG` is defaulted to `6` on every save (2692), so this row is effectively always present in DMRGateway mode after the first save.
- Label literal **"XLX Startup Module:"**. Tooltip: `<b>XLX Startup Module override</b>Default will use the host file option, or override it here.`
- Leading options depend on `[XLX Network] Module` (4709-4719):
  - Module set and `!= "@"`: `<option value="{Module}" selected>{Module}</option>`, `<option value="Default">Default</option>`, `<option value="@">None</option>`.
  - Module `== "@"`: `Default`, then `<option value="@" selected>None</option>`.
  - Module not set: `<option value="Default" selected>Default</option>`, `<option value=" ">None</option>`. Note that the value here is a **single space**, not `@`.
- Then static `A` … `Z` (value = text). The current module therefore appears twice in case 1.
- Save (1670-1678), if non-empty: `$m = escapeshellcmd(POST)`.
  - `"Default"`: `unset([XLX Network] Module)`.
  - Anything else: `[XLX Network] Module = $m`.
  - PHP's `empty(" ")` is false, so choosing "None" from the not-set state writes `Module= ` (a space). Choosing "None" in the other states writes `Module=@`.

### 3.15 XLX Master Enable: `dmrGatewayXlxEn` (4750-4757). DMRGateway mode only

- Toggle. id `toggle-dmrGatewayXlxEn`, aria-label `Enable XLX Network`, hidden text `Enable XLX via DMR`, JS `toggleDmrGatewayXlxEnCheckbox()`.
- Label `$lang['xlx_enable']` = **"XLX Master Enable:"**. Tooltip: `<b>XLX Master Enable</b>Turn your XLX connection on or off.`
- Checked if `[XLX Network 1] Enabled == 1`, else if `[XLX Network] Enabled == 1` (both are isset-guarded).
- Save (1699-1703): `ON` sets `[XLX Network 1] Enabled="1"` and `[XLX Network] Enabled="1"`. `OFF` sets both to `"0"`. `[XLX Network 1]` is later deleted (3080).

---

### 3.16 Hotspot Security (BM direct): `bmHSSecurity` (variant B, 4759-4764). Only if `substr($dmrMasterNow,0,2) == "BM"`

- `<input type="password" name="bmHSSecurity" size="30" maxlength="30">`.
- Label literal **"Hotspot Security:"**. Tooltip: `<b>Custom Password</b>Override the Password for your DMR Host with your own custom password, make sure you already configured this with your chosen DMR Host too. Empty the field to use the default.`
- Value: `/etc/dstar-radio.mmdvmhost [BrandMeister] Password` if set.
- Save: 3.2 steps 5 and 7.
  - Non-empty: modem `[BrandMeister] Password` and mmdvmhost `[DMR Network] Password` are both set to `"value"`.
  - Empty: the modem key is removed. mmdvmhost `Password` keeps its existing value, or gets the host-file password if it had none. It is **not** reset to the host-file default when it already existed.

### 3.17 BrandMeister Network links (BM direct, 4765-4771). Not a control

- Label **"BrandMeister Network:"** (from `$lang['bm_network']`). Tooltip: `<b>BrandMeister Dashboards</b>Direct links to your BrandMeister Dashboards`.
- Same two links as 3.7. ID = `/etc/mmdvmhost [DMR] Id` if set, otherwise `[General] Id`.

### 3.18 DMR Options (FreeDMR): `dmrNetworkOptions` (variant B, 4772-4780). Only if `substr($dmrMasterNow,0,8) == "FreeDMR_"`

- Cell: `Options=` + `<input type="text" name="dmrNetworkOptions" size="65" maxlength="250">`.
- Label literal **"DMR Options:"**. Tooltip: `<b>DMR Network</b>Set your options= for DMR here`.
- Value: `/etc/mmdvmhost [DMR Network] Options` if set (unescaped).
- Save: 3.2 step 12 writes it to mmdvmhost `[DMR Network] Options` **and** dmrgateway `[DMR Network 2] Options`, both quoted with `"` stripped. Empty unsets both, plus ysf2dmr `Options`.

### 3.19 FreeDMR Help links (4781-4787). FreeDMR_ only. Not a control

- Label literal **"FreeDMR Help:"**. Tooltip: `<b>FreeDMR Help</b>FreeDMR Options Help`.
- Links (`target="_new" style="color: #000;"`, separated by ` | `):
  - `FreeDMR Options Guide`, href `http://www.freedmr.uk/index.php/static-talk-groups-pi-star/`
  - `FreeDMR Options Calculator`, href `http://www.freedmr.uk/index.php/dashboard/options-calculator/`

### 3.20 DMR Options (DMR+/HB_/FD_/FreeSTAR_ direct): `dmrNetworkOptions` (variant C, 4788-4796)

- Identical to 3.18 (label "DMR Options:", same tooltip, same source and save), but without the help row.
- Condition: `$dmrMasterNow` starts with `DMR+`, `HB_`, `FD_` or `FreeSTAR_`.

### 3.21 Hotspot Security (TGIF): `tgifHSSecurity` (4797-4804). Only if `substr($dmrMasterNow,0,4) == "TGIF"`

- `<input type="password" name="tgifHSSecurity" size="30" maxlength="30">`.
- Label literal **"Hotspot Security:"**. The tooltip is identical to 3.16.
- Value: `/etc/dstar-radio.mmdvmhost [TGIF] Password` if set.
- Save: 3.2 step 6.
  - Non-empty: modem `[TGIF] Password="v"` and mmdvmhost `[DMR Network] Password="v"`.
  - Empty: only the modem key is removed. The mmdvmhost password is unchanged.

### 3.22 DMR ESSID: `dmrExtendedId` (4807-4836). Only if `$dmrMasterNow !== "DMRGateway"`

- Label literal **"DMR ESSID:"**. Tooltip: `<b>DMR Extended ID</b>This is the extended ID, to make your DMR ID 8 or 9 digits long`.
- Same prefix text (first 7 characters of `[General] Id`, unless it is `1234567`). Options: `None`, `01`-`99`.
- Preselection (4811-4820):
  - If `/etc/mmdvmhost [DMR] Id` is set and longer than `[General] Id`: `len = strlen(DMR Id) - strlen(General Id)`, and the ESSID is the last `len` characters. Otherwise `None`.
  - For an 8-digit `[DMR] Id` the ESSID is 1 character (e.g. `5`). No option matches it (`===` against `01`-`99`), so the browser shows `None`, and saving produces a 7-digit ID. The 1-digit ESSID is lost. The tooltip promises 8 digits, but this form cannot represent them.
- Save (1481-1485), if non-empty: `[DMR] Id = [General] Id . preg_replace('/[^0-9]/','',POST)`. `None` gives the 7-digit ID.

### 3.23 DMR Colour Code: `dmrColorCode` (4838-4845). Always

- `<select name="dmrColorCode">`. Label `$lang['dmr_cc']` = **"DMR Colour Code:"**. Tooltip: `<b>DMR Color Code</b>Set your DMR Color Code here`.
- Options `0` … `15` (value = text). The one where `/etc/mmdvmhost [DMR] ColorCode == n` (loose `==`) is selected. If the key is missing, PHP's `null == 0` makes `0` selected.
- Save (2621-2624): `if (empty($_POST['dmrColorCode']) != TRUE) [DMR] ColorCode = escapeshellcmd(POST)`.
  - **Bug:** `empty("0")` is true in PHP, so selecting colour code **0 is never saved** and the previous value stays.

### 3.24 DMR EmbeddedLCOnly: `dmrEmbeddedLCOnly` (4846-4851). Always

- Toggle. id `toggle-dmrEmbeddedLCOnly`, aria-label `Enable DMR Embedded LC Only`, hidden text `Enable DMR Embedded LC only` when checked and `Enable DMR Embedded LC Only` when unchecked, JS `toggleDmrEmbeddedLCOnly()`.
- Label `$lang['dmr_embeddedlconly']` = **"DMR EmbeddedLCOnly:"**. Tooltip: `<b>DMR EmbeddedLCOnly</b>Turn ON to disable extended message support, including GPS and Talker Alias data. This can help reduce problems with some DMR Radios that do not support such features.`
- Checked if `/etc/mmdvmhost [DMR] EmbeddedLCOnly == 1`.
- Save (1687-1691): `[DMR] EmbeddedLCOnly = "1"` for `ON`, `"0"` for `OFF`.

### 3.25 DMR DumpTAData: `dmrDumpTAData` (4852-4857). Always

- Toggle. id `toggle-dmrDumpTAData`, aria-label `DMR Dump TA Data`, hidden text `DMR Dump T-A Data`, JS `toggleDmrDumpTAData()`.
- Label `$lang['dmr_dumptadata']` = **"DMR DumpTAData:"**. Tooltip: `<b>DMR DumpTAData</b>Turn ON to dump GPS and Talker Alias data to MMDVMHost log file.`
- Checked if `[DMR] DumpTAData == 1`.
- Save (1693-1697): `[DMR] DumpTAData = "1"` or `"0"`.

### 3.26 (commented out) DMR JitterBuffer: `dmrDMRnetJitterBufer` (4858-4864)

- Not rendered. Its handler is commented out (1680-1684).
- Line 1685 runs on every Apply: `unset($configmmdvm['DMR Network']['JitterEnabled'])`. A re-implementation must also drop `[DMR Network] JitterEnabled` from `/etc/mmdvmhost`.

### 3.27 Apply Changes button (4866)

- `<input type="button" value="Apply Changes" onclick="submitform()">`. It submits the whole `#config` form, not only this section.

---

## 4. Always-run normalisation on Apply that affects this section's files

These run on every POST, whether or not the DMR section was shown:

- 1685: `unset /etc/mmdvmhost [DMR Network] JitterEnabled`.
- 1621-1624: if `dmrMasterHost` was not posted, unset mmdvmhost `[DMR Network] Options` and dmrgateway `[DMR Network 2] Options`.
- 1717-1721: unset `[General] ModeHang` (mmdvmhost) and `[General] Timeout` (dmrgateway). If present, force `[General] RFModeHang=300` and `NetModeHang=300` in mmdvmhost.
- 2462-2463 (Mode section `MMDVMModeDMR`): `ON` sets `[DMR] Enable=1` and `[DMR Network] Enable=1`, and sets ysf2dmr `Enabled=0`. `OFF` sets both to 0. This controls the visibility of this section on the next load.
- 2768-2771: if missing, set mmdvmhost `[DMR] BeaconInterval=60`, `BeaconDuration=3`, `OVCM=0`, and `[DMR Network] Type=Direct`.
- 2682-2705, defaults added to `/etc/dmrgateway` if missing:
  - `[Info] Enabled=0`, `Power` = mmdvmhost `[Info] Power`, `Height` = mmdvmhost `[Info] Height`.
  - `[XLX Network]`: `Enabled=0`, `File=/usr/local/etc/XLXHosts.txt`, `Port=62030`, `Password=passw0rd`, `ReloadTime=60`, `Slot=2`, `TG=6`, `Base=64000`, `Startup=950`, `Relink=60`, `Debug=0`, `UserControl=1`.
  - `[DMR Network 3]`: `Enabled=0`, `Name=HBLink`, `Address=1.2.3.4`, `Port=5555`, `TGRewrite0=2,11,2,11,1`, `Password=PASSWORD`, `Location=0`, `Debug=0`.
- 2706-2736: `exec("DMRGateway -v | awk {'print $3'} | cut -c 1-8")`. If the result is `> 20210130`, add if missing:
  - `[DMR Network 1] Location=1`, `[DMR Network 2] Location=0`, `[DMR Network 3] Location=0`, and `[DMR Network 4/5/6] Location=0` when those sections exist.
  - `[GPSD] Enable=0, Address=127.0.0.1, Port=2947`.
  - `[APRS] Enable=1, Address=127.0.0.1, Port=8673, Description=APRS for DMRGateway, Suffix=DMR`.
  - `[Dynamic TG Control] Enabled=1, Port=3769`.
- 2737-2749: re-wrap in `"…"` (only when the first character is not already `"`) the dmrgateway values `[Info] Location`, `[Info] Description`, and `Password`/`Options` of `[DMR Network 1]` … `[DMR Network 5]`. The Options/Password values written by this section therefore end up quoted in `/etc/dmrgateway`. mmdvmhost gets no equivalent fix-up.
- 3078-3082: a second `DMRGateway -v` probe. If the version is `> 20170924`, `unset [XLX Network 1]` and `[XLX Network 2]` in dmrgateway.
- Mode section DMR2YSF/DMR2NXDN (2514-2584) can rebuild `[DMR Network 3]` with `Id = [DMR Network 2] Id`. That belongs to the Mode section and is listed only because it reads 3.10's result.

### Handler order (matters for ID composition)

1. `dmrId` (General section, 1468-1479): sets `[General] Id`, `[DMR] Id`, ysfgateway `[General] Id`, dmrgateway `[XLX Network] Id`, … to the first 7 digits.
2. `dmrExtendedId` (1482) overrides `[DMR] Id`.
3. `bmExtendedId` (1488) sets Net1 `Id`.
4. `dmrPlusExtendedId` (1494) sets Net2 `Id`.
5. `dmrMasterHost` (1513), then `dmrMasterHost1/2/3`, `…Startup`, `…StartupModule`, then the toggles (1687-1715).
6. Colour code (2622).
7. The defaults and normalisation listed above.
8. The writers.

### File writers

- `/etc/mmdvmhost` (3274-3312): serialised as `[Section]\nkey=value\n…\n\n`. `_` in section names is replaced by a space. Written to `/tmp/bW1kdm1ob3N0DQo.tmp`, then `sudo install -m 644 -o root -g root … /etc/mmdvmhost`, **only if the temp file has more than 140 lines**.
- `/etc/dmrgateway` (3722-3758): same format. Temp file `/tmp/k4jhdd34jeFr8f.tmp`. Installed only if `/etc/dmrgateway` is readable and the temp file has more than 55 lines.
- `/etc/dstar-radio.mmdvmhost` (3760-3824): same format, but every key named `Password` is written as `Password="<value with all " removed>"`. Temp file `/tmp/sja7hFRkw4euG7.tmp`, then `sudo install -m 644 -o root -g root` to whichever marker files exist.
- `/etc/ysf2dmr` (installed only if more than 35 lines), `/etc/dmr2ysf`, `/etc/dmr2nxdn`: rewritten by their own writers (≈3466-3685). They carry the side effects from 3.2.
- If any temp file is not writable, the page shows "Unable to write configuration file(s)..." and calls `die()`.

---

## 5. Cross-section notes and quirks

1. **Duplicate POST name `bmHSSecurity`.** The YSF section also renders `bmHSSecurity` (line 5207) when `/etc/ysf2dmr [Enabled] Enabled == 1`. The YSF section comes later in the form, so if both sections render, PHP keeps the YSF2DMR field's value for both handlers. The YSF2DMR handler (1392-1402) also writes modem `[BrandMeister] Password` (non-empty) or unsets it (empty), whenever `ysf2dmrMasterHost` is posted. A native implementation should give the two fields separate names, or reproduce the "last one wins" rule on purpose.
2. **Duplicate POST name `dmrNetworkOptions`.** Variants A/B/C never render together. A appears only in DMRGateway mode, B and C only in direct mode with mutually exclusive prefixes.
3. **Hidden-OFF side effect** (§3.1). Any Apply in direct mode turns off DMRGateway Net1, Net2 and XLX.
4. **Stale passwords.** mmdvmhost `[DMR Network] Password` is set to the host-file password only when it is missing. Switching masters (BM to TGIF, TGIF to DMR+, …) keeps the previous password unless the relevant Hotspot Security field is filled in.
5. **No live re-render.** Rows depend on the saved master (`$dmrMasterNow`), not on the selected one.
6. **No match for the current master.** If `[DMR Network] Address/Port` matches no host line, no option is preselected. Apply then silently switches to the first host in the file. With no master detected, only DMR ESSID, Colour Code, EmbeddedLCOnly and DumpTAData rows render.
7. **Colour code 0 cannot be saved** (§3.23).
8. **8-digit DMR IDs are lost** on save (§3.22).
9. **XLX select can show a duplicate option** (§3.12). **XLX module "None"** can write a single space (§3.14).
10. **No HTML escaping** of any echoed value (passwords, options, host names). A native UI should escape, while keeping the stored bytes identical.
11. **Sanitisation summary.**
    - Host selects: `escapeshellcmd`, then `explode(',')`.
    - ESSIDs: `preg_replace('/[^0-9]/','')`.
    - Options: `str_replace('"','')`.
    - Passwords: **no** sanitisation. They are wrapped in `"`. Only the modem-file writer strips embedded `"`, while mmdvmhost and dmrgateway get them raw inside quotes (dmrgateway only re-quotes if the first character is not `"`).
    - Colour code and XLX startup/module: `escapeshellcmd`.
12. **Unclear points.**
    - Line 4673-4675 (XLX selection): the double-emission looks unintended. It is documented as the actual behaviour.
    - Line 4686-4691 (XLX Startup TG "None" selected branch): unreachable, because the row requires `isset(Startup)`.
    - `$dmrMasterNow` on duplicate address+port host lines: the last match wins (4512). Whether the upstream host file contains such duplicates was not verified, because only the QA fixture is available locally.

---

## Services/commands touched by this section on Apply

- `sudo mount -o remount,rw /` at the start (359). `sudo sync && sudo sync && sudo sync && sudo mount -o remount,ro /` at the end (3944).
- `sudo systemctl stop cron.service` / `start cron.service` (around the whole apply).
- `sudo systemctl stop` (≈572-574), then `start` (3838-3860), for all DV services. The ones relevant here: `mmdvmhost.service`, `dmrgateway.service`, `ysf2dmr.service`, `dmr2ysf.service`, `dmr2nxdn.service`. The handler restarts every service, not only these. Also `sudo systemctl daemon-reload` (3838).
- `DMRGateway -v | awk {'print $3'} | cut -c 1-8`, run twice (2706, 3078), to decide whether to add defaults and drop `[XLX Network 1/2]`.
- `sudo install -m 644 -o root -g root /tmp/bW1kdm1ob3N0DQo.tmp /etc/mmdvmhost` (if more than 140 lines).
- `sudo install -m 644 -o root -g root /tmp/k4jhdd34jeFr8f.tmp /etc/dmrgateway` (if more than 55 lines).
- `sudo install -m 644 -o root -g root /tmp/sja7hFRkw4euG7.tmp /etc/dstar-radio.mmdvmhost` (modem file: BrandMeister/TGIF passwords).
- `sudo install … /etc/ysf2dmr`, `/etc/dmr2ysf`, `/etc/dmr2nxdn`, for the side effects of `dmrMasterHost` (Local/Options/LocalAddress).
