package main

import (
	"bufio"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Configuration page, section "DMR Configuration" (admin/configure.php
// lines 4488-4867, POST handling 1481-1715 and 2621-2624), following
// docs/configure-spec/02-dmr.md. Reference defects fixed here are marked
// "Fix:" with the spec paragraph they come from.

func init() {
	registerCfgSection(cfgSectionImpl{ID: "dmr", Order: 50, Build: buildDMRSection, Apply: applyDMRSection})
}

// dmrHost is one DMR_Hosts.txt line: col[0] Name, col[2] Address,
// col[3] Password, col[4] Port (col[1] is unused by this page).
type dmrHost struct{ Name, Addr, Pass, Port string }

// value is the classic option value "address,password,port,name".
func (h dmrHost) value() string { return h.Addr + "," + h.Pass + "," + h.Port + "," + h.Name }

var dmrHostSplit = regexp.MustCompile(`\s+`)

// dmrHostList parses DMR_Hosts.txt like the classic page (spec §0): split the
// raw line on whitespace; skip it when the first token contains "#" anywhere
// or is empty (blank lines and lines starting with whitespace). Missing
// columns read as "". File order is kept. hostsFile() is not used because it
// trims leading whitespace and drops short lines, which the page does not.
func dmrHostList() []dmrHost {
	rows, _ := cachedHostFile("dmr-hosts", filepath.Join(gospotHostsDir, "DMR_Hosts.txt"), parseDMRHosts)
	return cloneRows(rows)
}

func parseDMRHosts(f io.Reader) []dmrHost {
	var out []dmrHost
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 64<<10), 1<<20)
	for s.Scan() {
		col := dmrHostSplit.Split(s.Text(), -1)
		if col[0] == "" || strings.Contains(col[0], "#") {
			continue
		}
		at := func(i int) string {
			if i < len(col) {
				return col[i]
			}
			return ""
		}
		out = append(out, dmrHost{Name: col[0], Addr: at(2), Pass: at(3), Port: at(4)})
	}
	return out
}

// dmrView is the saved state that decides which rows render (spec §1).
type dmrView struct {
	hosts []dmrHost
	// now is $dmrMasterNow: the name of the last non-XLX host whose address
	// and port match /etc/mmdvmhost [DMR Network]; "" when none matches.
	now     string
	nowHost string // option value of that host
	gateway bool   // now == "DMRGateway"
	bm      bool   // BM...
	freedmr bool   // FreeDMR_...
	plus    bool   // DMR+..., HB_..., FD_..., FreeSTAR_...
	tgif    bool   // TGIF...
	modem   iniData
}

// dmrIsOne is PHP's loose `$v == 1` for an INI string.
func dmrIsOne(v string) bool {
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	return err == nil && f == 1
}

func newDMRView(c *cfgContext) *dmrView {
	marker, ok := c.marker("mmdvmhost")
	if !ok || !c.has("mmdvmhost") || !dmrIsOne(c.get("mmdvmhost", "DMR", "Enable")) {
		return nil
	}
	v := &dmrView{hosts: dmrHostList(), modem: parseINI(strings.NewReader(marker))}
	addr, port := c.get("mmdvmhost", "DMR Network", "Address"), c.get("mmdvmhost", "DMR Network", "Port")
	for _, h := range v.hosts {
		if !strings.HasPrefix(h.Name, "XLX") && addr != "" && h.Addr == addr && h.Port == port {
			v.now, v.nowHost = h.Name, h.value()
		}
	}
	v.gateway = v.now == "DMRGateway"
	v.bm = strings.HasPrefix(v.now, "BM")
	v.freedmr = strings.HasPrefix(v.now, "FreeDMR_")
	v.plus = strings.HasPrefix(v.now, "DMR+") || strings.HasPrefix(v.now, "HB_") || strings.HasPrefix(v.now, "FD_") || strings.HasPrefix(v.now, "FreeSTAR_")
	v.tgif = strings.HasPrefix(v.now, "TGIF")
	return v
}

// hostOptions lists the hosts accepted by one select. Fix: identical lines are
// listed once (the classic page repeats them; repeated option values break
// the keyed option list and add nothing).
func (v *dmrView) hostOptions(accept func(name string) bool) ([]linkOption, map[string]dmrHost) {
	var opts []linkOption
	byValue := map[string]dmrHost{}
	for _, h := range v.hosts {
		if !accept(h.Name) {
			continue
		}
		if _, dup := byValue[h.value()]; dup {
			continue
		}
		byValue[h.value()] = h
		opts = append(opts, linkOption{Value: h.value(), Label: h.Name})
	}
	return opts, byValue
}

func dmrMainHost(name string) bool { return !strings.HasPrefix(name, "XLX") }
func dmrBMHost(name string) bool   { return strings.HasPrefix(name, "BM") }
func dmrPlusHost(name string) bool { return strings.HasPrefix(name, "DMR+") }
func dmrXLXHost(name string) bool  { return strings.HasPrefix(name, "XLX") }

// dmrSelectedHost is the last option matching address and port, or "" (spec
// §3.3, 3.8). Fix: with no match the classic browser shows the first host
// and Apply silently switches to it; here nothing is selected and an empty
// value leaves the stored master alone (spec §5.6).
func dmrSelectedHost(opts []linkOption, byValue map[string]dmrHost, addr, port string) string {
	sel := ""
	for _, o := range opts {
		if h := byValue[o.Value]; addr != "" && h.Addr == addr && h.Port == port {
			sel = o.Value
		}
	}
	return sel
}

func dmrPresent(c *cfgContext, file, section, key string) bool {
	_, ok := c.St.Files[file][section][key]
	return ok
}

// dmrESSIDOptions is None, 01-99. Fix (spec §3.22): a current ESSID that is not
// one of them (the 1-digit ESSID of an 8-digit DMR ID) is offered as well,
// so an unedited Apply keeps it instead of truncating the ID.
func dmrESSIDOptions(cur string) []linkOption {
	opts := []linkOption{{Value: "None", Label: "None"}}
	if cur != "None" && !dmrESSIDStandard(cur) {
		opts = append(opts, linkOption{Value: cur, Label: cur})
	}
	for i := 1; i <= 99; i++ {
		s := strconv.Itoa(i)
		if i < 10 {
			s = "0" + s
		}
		opts = append(opts, linkOption{Value: s, Label: s})
	}
	return opts
}

func dmrESSIDStandard(s string) bool {
	n, err := strconv.Atoi(s)
	return err == nil && len(s) == 2 && n >= 1 && n <= 99
}

// dmrGatewayESSID is the BrandMeister / DMR+ ESSID preselection (spec §3.5, 3.10).
func dmrGatewayESSID(c *cfgContext, section string) string {
	general := c.get("mmdvmhost", "General", "Id")
	if dmrPresent(c, "dmrgateway", section, "Id") {
		id := c.get("dmrgateway", section, "Id")
		if len(id) > len(general) {
			return id[len(id)-2:]
		}
		return "None"
	}
	if dmrPresent(c, "mmdvmhost", "General", "Id") && len(general) == 9 {
		return general[7:]
	}
	return "None"
}

// dmrDirectESSID is the DMR ESSID preselection (spec §3.22).
func dmrDirectESSID(c *cfgContext) string {
	general, id := c.get("mmdvmhost", "General", "Id"), c.get("mmdvmhost", "DMR", "Id")
	if dmrPresent(c, "mmdvmhost", "DMR", "Id") && len(id) > len(general) {
		return id[len(general):]
	}
	return "None"
}

// dmrESSIDPrefix is the first 7 characters of [General] Id unless it is the
// 1234567 placeholder.
func dmrESSIDPrefix(c *cfgContext) string {
	id := c.get("mmdvmhost", "General", "Id")
	if !dmrPresent(c, "mmdvmhost", "General", "Id") || id == "1234567" {
		return ""
	}
	if len(id) > 7 {
		id = id[:7]
	}
	return id
}

// dmrColorCodeShown is the selected colour code: a missing key shows 0 (PHP
// null == 0); a value outside 0-15 selects nothing.
func dmrColorCodeShown(c *cfgContext) string {
	if !dmrPresent(c, "mmdvmhost", "DMR", "ColorCode") {
		return "0"
	}
	n, err := strconv.Atoi(strings.TrimSpace(c.get("mmdvmhost", "DMR", "ColorCode")))
	if err != nil || n < 0 || n > 15 {
		return ""
	}
	return strconv.Itoa(n)
}

// dmrXLXModuleOptions is the XLX Startup Module list (spec §3.14). Fix: "None"
// always posts "@" (the classic posts a single space when no Module is set)
// and the current module is not listed twice.
func dmrXLXModuleOptions(c *cfgContext) (string, []linkOption) {
	mod := c.get("dmrgateway", "XLX Network", "Module")
	set := dmrPresent(c, "dmrgateway", "XLX Network", "Module")
	var opts []linkOption
	value := "Default"
	switch {
	case set && mod != "@":
		value = mod
		opts = []linkOption{{Value: mod, Label: mod}, {Value: "Default", Label: "Default"}, {Value: "@", Label: "None"}}
	case set:
		value = "@"
		opts = []linkOption{{Value: "Default", Label: "Default"}, {Value: "@", Label: "None"}}
	default:
		opts = []linkOption{{Value: "Default", Label: "Default"}, {Value: "@", Label: "None"}}
	}
	for ch := 'A'; ch <= 'Z'; ch++ {
		if s := string(ch); !(set && s == mod) {
			opts = append(opts, linkOption{Value: s, Label: s})
		}
	}
	return value, opts
}

// dmrXLXStartupTG is the legacy XLX Startup TG list (spec §3.13).
func dmrXLXStartupTG(c *cfgContext) (string, []linkOption) {
	cur := c.get("dmrgateway", "XLX Network 1", "Startup")
	opts := []linkOption{{Value: "None", Label: "None"}}
	value := ""
	for i := 1; i <= 26; i++ {
		s := "40" + strconv.Itoa(i/10) + strconv.Itoa(i%10)
		opts = append(opts, linkOption{Value: s, Label: s})
		if n, err := strconv.Atoi(strings.TrimSpace(cur)); err == nil && strconv.Itoa(n) == s {
			value = s
		}
	}
	return value, opts
}

// dmrXLXSelected is the XLX Master preselection (spec §3.12): $cur is
// [XLX Network 1] Address, overridden by [XLX Network] Startup; the option
// is the last one whose address is $cur or whose name is "XLX_"+$cur.
// Fix: each host is listed once (the classic can emit it twice).
func dmrXLXSelected(c *cfgContext, opts []linkOption, byValue map[string]dmrHost) string {
	cur, ok := "", false
	if dmrPresent(c, "dmrgateway", "XLX Network 1", "Address") {
		cur, ok = c.get("dmrgateway", "XLX Network 1", "Address"), true
	}
	if dmrPresent(c, "dmrgateway", "XLX Network", "Startup") {
		cur, ok = c.get("dmrgateway", "XLX Network", "Startup"), true
	}
	sel := ""
	if !ok {
		return sel
	}
	for _, o := range opts {
		if h := byValue[o.Value]; h.Addr == cur || h.Name == "XLX_"+cur {
			sel = o.Value
		}
	}
	return sel
}

func dmrXLXEnabledShown(c *cfgContext) bool {
	return dmrIsOne(c.get("dmrgateway", "XLX Network 1", "Enabled")) || dmrIsOne(c.get("dmrgateway", "XLX Network", "Enabled"))
}

// Row helpers.

func dmrLabel(label, title, body string) cfgCell {
	return cfgCell{Align: "left", Label: label, Tooltip: tip(title, body)}
}

func dmrRow(id string, label cfgCell, value cfgCell) cfgRow {
	return cfgRow{ID: id, Cells: []cfgCell{label, value}}
}

func dmrSelectCell(c cfgControl) cfgCell {
	return cfgCell{Style: "text-align: left;", Controls: []cfgControl{c}}
}

func dmrToggle(name string, on bool, aria string) cfgControl {
	t := toggle(name, on)
	t.Attrs = map[string]string{"label": aria}
	return t
}

func dmrPassword(name, value string) cfgControl {
	return cfgControl{Kind: "password", Name: name, Value: value, Attrs: map[string]string{"size": "30", "maxlength": "30"}}
}

func dmrLink(text, href string) cfgControl {
	return cfgControl{Kind: "link", Text: text, Attrs: map[string]string{"href": href, "target": "_new", "style": "color: #000;"}}
}

func dmrOptionsCell(value string) cfgCell {
	return td(static("Options="), text("dmrNetworkOptions", value, map[string]string{"size": "65", "maxlength": "250"}))
}

func dmrESSIDCell(c *cfgContext, name, cur string) cfgCell {
	var ctl []cfgControl
	if p := dmrESSIDPrefix(c); p != "" {
		ctl = append(ctl, static(p))
	}
	return td(append(ctl, selectBox(name, cur, dmrESSIDOptions(cur)))...)
}

func dmrBMLinks(id string) cfgCell {
	href := "https://brandmeister.network/#/device/" + id
	return cfgCell{Controls: []cfgControl{dmrLink("Device Information", href), static(" | "), dmrLink("Edit Device (BrandMeister Selfcare)", href)}}
}

const dmrCustomPasswordTip = "Override the Password for your DMR Host with your own custom password, make sure you already configured this with your chosen DMR Host too. Empty the field to use the default."

func buildDMRSection(c *cfgContext) *cfgSection {
	v := newDMRView(c)
	if v == nil {
		return nil
	}
	head := th("Setting", tip("Setting", ""))
	head.Style = "width: 200px;"
	rows := []cfgRow{{Cells: []cfgCell{head, th("Value", tip("Value", "The current value from the\nconfiguration files"))}}}

	mainOpts, _ := v.hostOptions(dmrMainHost)
	rows = append(rows, dmrRow("dmrMasterHost", dmrLabel("DMR Master:", "DMR Master (MMDVMHost)", "Set your prefered DMR master here"),
		dmrSelectCell(selectBox("dmrMasterHost", v.nowHost, mainOpts))))

	bmPassword := v.modem.get("BrandMeister", "Password")
	if v.gateway {
		bmOpts, bmBy := v.hostOptions(dmrBMHost)
		rows = append(rows, dmrRow("dmrMasterHost1", dmrLabel("BrandMeister Master:", "BrandMeister Master", "Set your prefered DMR master here"),
			dmrSelectCell(selectBox("dmrMasterHost1", dmrSelectedHost(bmOpts, bmBy, c.get("dmrgateway", "DMR Network 1", "Address"), c.get("dmrgateway", "DMR Network 1", "Port")), bmOpts))))
		rows = append(rows, dmrRow("bmHSSecurity", dmrLabel("BM Hotspot Security:", "BrandMeister Password",
			"Override the Password for BrandMeister with your own custom password, make sure you already configured this using BM Self Care. Empty the field to use the default."),
			td(dmrPassword("bmHSSecurity", bmPassword))))
		rows = append(rows, dmrRow("bmExtendedId", dmrLabel("BrandMeister Network ESSID:", "BrandMeister Extended ID", "This is the extended ID, to make your DMR ID 9 digits long"),
			dmrESSIDCell(c, "bmExtendedId", dmrGatewayESSID(c, "DMR Network 1"))))
		rows = append(rows, dmrRow("dmrGatewayNet1En", dmrLabel("BrandMeister Network Enable:", "BrandMeister Network Enable", "Enable or disable BrandMeister Network"),
			td(dmrToggle("dmrGatewayNet1En", dmrIsOne(c.get("dmrgateway", "DMR Network 1", "Enabled")), "Enable BrandMeister DMR"))))
		id := c.get("mmdvmhost", "General", "Id")
		if dmrPresent(c, "dmrgateway", "DMR Network 1", "Id") {
			id = c.get("dmrgateway", "DMR Network 1", "Id")
		}
		rows = append(rows, dmrRow("bmLinks", dmrLabel("BrandMeister Network:", "BrandMeister Dashboards", "Direct links to your BrandMeister Dashboards"), dmrBMLinks(id)))

		plusOpts, plusBy := v.hostOptions(dmrPlusHost)
		rows = append(rows, dmrRow("dmrMasterHost2", dmrLabel("DMR+ Master:", "DMR+ Master", "Set your prefered DMR master here"),
			dmrSelectCell(selectBox("dmrMasterHost2", dmrSelectedHost(plusOpts, plusBy, c.get("dmrgateway", "DMR Network 2", "Address"), c.get("dmrgateway", "DMR Network 2", "Port")), plusOpts))))
		rows = append(rows, dmrRow("dmrNetworkOptions", dmrLabel("DMR+ Network:", "DMR+ Network", "Set your options= for DMR+ here"),
			dmrOptionsCell(c.get("dmrgateway", "DMR Network 2", "Options"))))
		rows = append(rows, dmrRow("dmrPlusExtendedId", dmrLabel("DMR+ Network ESSID:", "DMR Plus Extended ID", "This is the extended ID, to make your DMR ID 8 digits long"),
			dmrESSIDCell(c, "dmrPlusExtendedId", dmrGatewayESSID(c, "DMR Network 2"))))
		rows = append(rows, dmrRow("dmrGatewayNet2En", dmrLabel("DMR+ Network Enable:", "DMR+ Network Enable", ""),
			td(dmrToggle("dmrGatewayNet2En", dmrIsOne(c.get("dmrgateway", "DMR Network 2", "Enabled")), "Enable DMR+"))))

		xlxOpts, xlxBy := v.hostOptions(dmrXLXHost)
		rows = append(rows, dmrRow("dmrMasterHost3", dmrLabel("XLX Master:", "XLX Master", "Set your prefered XLX master here"),
			dmrSelectCell(selectBox("dmrMasterHost3", dmrXLXSelected(c, xlxOpts, xlxBy), xlxOpts))))
		if dmrPresent(c, "dmrgateway", "XLX Network 1", "Startup") {
			val, opts := dmrXLXStartupTG(c)
			rows = append(rows, dmrRow("dmrMasterHost3Startup", dmrLabel("XLX Startup TG:", "XLX Startup TG", ""),
				td(selectBox("dmrMasterHost3Startup", val, opts))))
		}
		if dmrPresent(c, "dmrgateway", "XLX Network", "TG") {
			val, opts := dmrXLXModuleOptions(c)
			rows = append(rows, dmrRow("dmrMasterHost3StartupModule", dmrLabel("XLX Startup Module:", "XLX Startup Module override", "Default will use the host file option, or override it here."),
				td(selectBox("dmrMasterHost3StartupModule", val, opts))))
		}
		rows = append(rows, dmrRow("dmrGatewayXlxEn", dmrLabel("XLX Master Enable:", "XLX Master Enable", "Turn your XLX connection on or off."),
			td(dmrToggle("dmrGatewayXlxEn", dmrXLXEnabledShown(c), "Enable XLX Network"))))
	}
	if v.bm {
		rows = append(rows, dmrRow("bmHSSecurity", dmrLabel("Hotspot Security:", "Custom Password", dmrCustomPasswordTip), td(dmrPassword("bmHSSecurity", bmPassword))))
		id := c.get("mmdvmhost", "General", "Id")
		if dmrPresent(c, "mmdvmhost", "DMR", "Id") {
			id = c.get("mmdvmhost", "DMR", "Id")
		}
		rows = append(rows, dmrRow("bmLinks", dmrLabel("BrandMeister Network:", "BrandMeister Dashboards", "Direct links to your BrandMeister Dashboards"), dmrBMLinks(id)))
	}
	if v.freedmr || v.plus {
		rows = append(rows, dmrRow("dmrNetworkOptions", dmrLabel("DMR Options:", "DMR Network", "Set your options= for DMR here"),
			dmrOptionsCell(c.get("mmdvmhost", "DMR Network", "Options"))))
	}
	if v.freedmr {
		rows = append(rows, dmrRow("freeDMRHelp", dmrLabel("FreeDMR Help:", "FreeDMR Help", "FreeDMR Options Help"), cfgCell{Controls: []cfgControl{
			dmrLink("FreeDMR Options Guide", "http://www.freedmr.uk/index.php/static-talk-groups-pi-star/"), static(" | "),
			dmrLink("FreeDMR Options Calculator", "http://www.freedmr.uk/index.php/dashboard/options-calculator/")}}))
	}
	if v.tgif {
		rows = append(rows, dmrRow("tgifHSSecurity", dmrLabel("Hotspot Security:", "Custom Password", dmrCustomPasswordTip),
			td(dmrPassword("tgifHSSecurity", v.modem.get("TGIF", "Password")))))
	}
	if !v.gateway {
		rows = append(rows, dmrRow("dmrExtendedId", dmrLabel("DMR ESSID:", "DMR Extended ID", "This is the extended ID, to make your DMR ID 8 or 9 digits long"),
			dmrESSIDCell(c, "dmrExtendedId", dmrDirectESSID(c))))
	}
	var cc []linkOption
	for i := 0; i <= 15; i++ {
		cc = append(cc, linkOption{Value: strconv.Itoa(i), Label: strconv.Itoa(i)})
	}
	rows = append(rows,
		dmrRow("dmrColorCode", dmrLabel("DMR Colour Code:", "DMR Color Code", "Set your DMR Color Code here"), dmrSelectCell(selectBox("dmrColorCode", dmrColorCodeShown(c), cc))),
		dmrRow("dmrEmbeddedLCOnly", dmrLabel("DMR EmbeddedLCOnly:", "DMR EmbeddedLCOnly",
			"Turn ON to disable extended message support, including GPS and Talker Alias data. This can help reduce problems with some DMR Radios that do not support such features."),
			td(dmrToggle("dmrEmbeddedLCOnly", dmrIsOne(c.get("mmdvmhost", "DMR", "EmbeddedLCOnly")), "Enable DMR Embedded LC Only"))),
		dmrRow("dmrDumpTAData", dmrLabel("DMR DumpTAData:", "DMR DumpTAData", "Turn ON to dump GPS and Talker Alias data to MMDVMHost log file."),
			td(dmrToggle("dmrDumpTAData", dmrIsOne(c.get("mmdvmhost", "DMR", "DumpTAData")), "DMR Dump TA Data"))),
	)
	// The hidden OFF inputs (spec §3.1) are not needed: toggles post OFF
	// themselves. Fix: the DMRGateway toggles are rendered, and therefore
	// applied, only in DMRGateway mode, so an Apply with a direct master no
	// longer disables DMRGateway Network 1, Network 2 and XLX (spec §5.3).
	// The commented-out JitterBuffer row is not rendered (spec §3.26).
	return &cfgSection{ID: "dmr", Title: "DMR Configuration", Rows: rows, Apply: true}
}

// dmrApply carries one Apply: writes are skipped for files that are not
// installed, and the modem marker changes are collected separately.
type dmrApply struct {
	c     *cfgContext
	p     *cfgPlan
	v     *dmrView
	form  cfgForm
	modem map[string]map[string]*string // planned [BrandMeister]/[TGIF] changes
}

func (a *dmrApply) set(file, section, key, value string) {
	if a.c.has(file) {
		a.p.set(file, section, key, value)
	}
}

func (a *dmrApply) del(file, section, key string) {
	if a.c.has(file) {
		a.p.del(file, section, key)
	}
}

// present reports whether the key exists after the changes planned so far.
func (a *dmrApply) present(file, section, key string) bool {
	if v, ok := a.p.change.Changes[file][section][key]; ok {
		return v != nil
	}
	return dmrPresent(a.c, file, section, key)
}

func (a *dmrApply) modemSet(section string, value *string) {
	if a.modem[section] == nil {
		a.modem[section] = map[string]*string{}
	}
	a.modem[section]["Password"] = value
}

func (a *dmrApply) modemPassword(section string) (string, bool) {
	if v, ok := a.modem[section]["Password"]; ok {
		if v == nil {
			return "", false
		}
		return unquote(*v), true
	}
	return a.v.modem.get(section, "Password"), a.v.modem.has(section, "Password")
}

// posted returns a field only when Build rendered it.
func (a *dmrApply) posted(name string, rendered bool) (string, bool) {
	if !rendered || !a.form.has(name) {
		return "", false
	}
	return a.form[name], true
}

func dmrQuoted(s string) string { return `"` + s + `"` }

// pick resolves a posted host value against the select's own options, so
// only hosts from DMR_Hosts.txt are accepted. Fix: the values are taken from
// the host file as they are; the classic escapeshellcmd() put backslashes
// into passwords with shell metacharacters, and explode(',') misread
// passwords that contain commas (spec §3.2, §5.11).
func (a *dmrApply) pick(name string, rendered bool, accept func(string) bool, label string) (dmrHost, bool, error) {
	val, ok := a.posted(name, rendered)
	if !ok || val == "" {
		return dmrHost{}, false, nil
	}
	_, byValue := a.v.hostOptions(accept)
	h, found := byValue[val]
	if !found {
		return dmrHost{}, false, cfgError(label + ": unknown host; reload the page")
	}
	return h, true, nil
}

func dmrOneOf(name, val string, opts []linkOption, label string) error {
	for _, o := range opts {
		if o.Value == val {
			return nil
		}
	}
	return cfgError(label + ": invalid value")
}

func dmrToggleValue(form cfgForm, name, label string) (string, bool, error) {
	val, ok := form[name]
	if !ok {
		return "", false, nil
	}
	switch val {
	case "ON":
		return "1", true, nil
	case "OFF":
		return "0", true, nil
	}
	return "", false, cfgError(label + ": invalid value")
}

// dmrText validates a free-text field: no control characters and at most max
// characters, unless it is the value the page showed.
func dmrText(val, shown string, max int, label string, noQuotes bool) error {
	if val == shown {
		return nil
	}
	if len([]rune(val)) > max {
		return cfgError(label + ": at most " + strconv.Itoa(max) + " characters")
	}
	for _, r := range val {
		if r < 0x20 || r == 0x7f {
			return cfgError(label + ": control characters are not allowed")
		}
	}
	if noQuotes && strings.Contains(val, `"`) {
		return cfgError(label + ": double quotes are not allowed")
	}
	return nil
}

func applyDMRSection(c *cfgContext, form cfgForm, p *cfgPlan) error {
	v := newDMRView(c)
	if v == nil {
		return nil
	}
	a := &dmrApply{c: c, p: p, v: v, form: form, modem: map[string]map[string]*string{}}

	// Validate everything first; any error refuses the whole Apply.
	master, haveMaster, err := a.pick("dmrMasterHost", true, dmrMainHost, "DMR Master")
	if err != nil {
		return err
	}
	bm, haveBM, err := a.pick("dmrMasterHost1", v.gateway, dmrBMHost, "BrandMeister Master")
	if err != nil {
		return err
	}
	plus, havePlus, err := a.pick("dmrMasterHost2", v.gateway, dmrPlusHost, "DMR+ Master")
	if err != nil {
		return err
	}
	xlx, haveXLX, err := a.pick("dmrMasterHost3", v.gateway, dmrXLXHost, "XLX Master")
	if err != nil {
		return err
	}
	bmPw, bmPwPosted := a.posted("bmHSSecurity", v.gateway || v.bm)
	if bmPwPosted {
		label := "Hotspot Security"
		if v.gateway {
			label = "BM Hotspot Security"
		}
		if err := dmrText(bmPw, v.modem.get("BrandMeister", "Password"), 30, label, true); err != nil {
			return err
		}
	}
	tgifPw, tgifPwPosted := a.posted("tgifHSSecurity", v.tgif)
	if tgifPwPosted {
		if err := dmrText(tgifPw, v.modem.get("TGIF", "Password"), 30, "Hotspot Security", true); err != nil {
			return err
		}
	}
	opts, optsPosted := a.posted("dmrNetworkOptions", v.gateway || v.freedmr || v.plus)
	if optsPosted {
		shown := c.get("mmdvmhost", "DMR Network", "Options")
		if v.gateway {
			shown = c.get("dmrgateway", "DMR Network 2", "Options")
		}
		if err := dmrText(opts, shown, 250, "DMR Options", false); err != nil {
			return err
		}
		opts = strings.ReplaceAll(opts, `"`, "")
	}
	essids := []struct {
		name, cur, label string
		rendered         bool
	}{
		{"dmrExtendedId", dmrDirectESSID(c), "DMR ESSID", !v.gateway},
		{"bmExtendedId", dmrGatewayESSID(c, "DMR Network 1"), "BrandMeister Network ESSID", v.gateway},
		{"dmrPlusExtendedId", dmrGatewayESSID(c, "DMR Network 2"), "DMR+ Network ESSID", v.gateway},
	}
	for _, e := range essids {
		if val, ok := a.posted(e.name, e.rendered); ok && val != "" {
			if err := dmrOneOf(e.name, val, dmrESSIDOptions(e.cur), e.label); err != nil {
				return err
			}
		}
	}
	startup, startupPosted := a.posted("dmrMasterHost3Startup", v.gateway && dmrPresent(c, "dmrgateway", "XLX Network 1", "Startup"))
	if startupPosted && startup != "" {
		_, o := dmrXLXStartupTG(c)
		if err := dmrOneOf("dmrMasterHost3Startup", startup, o, "XLX Startup TG"); err != nil {
			return err
		}
	}
	module, modulePosted := a.posted("dmrMasterHost3StartupModule", v.gateway && dmrPresent(c, "dmrgateway", "XLX Network", "TG"))
	if modulePosted && module != "" {
		_, o := dmrXLXModuleOptions(c)
		if err := dmrOneOf("dmrMasterHost3StartupModule", module, o, "XLX Startup Module"); err != nil {
			return err
		}
	}
	cc, ccPosted := form["dmrColorCode"]
	if ccPosted && cc != "" {
		n, err := strconv.Atoi(cc)
		if err != nil || n < 0 || n > 15 || strconv.Itoa(n) != cc {
			return cfgError("DMR Colour Code: must be a number from 0 to 15")
		}
	}
	type tog struct {
		name, label string
		rendered    bool
		val         string
		ok          bool
	}
	toggles := []*tog{
		{name: "dmrEmbeddedLCOnly", label: "DMR EmbeddedLCOnly", rendered: true},
		{name: "dmrDumpTAData", label: "DMR DumpTAData", rendered: true},
		{name: "dmrGatewayXlxEn", label: "XLX Master Enable", rendered: v.gateway},
		{name: "dmrGatewayNet2En", label: "DMR+ Network Enable", rendered: v.gateway},
		{name: "dmrGatewayNet1En", label: "BrandMeister Network Enable", rendered: v.gateway},
	}
	for _, t := range toggles {
		if t.rendered {
			if t.val, t.ok, err = dmrToggleValue(form, t.name, t.label); err != nil {
				return err
			}
		}
	}

	// ESSIDs (configure.php 1481-1497) build on [General] Id as the General
	// section left it.
	base := p.value("mmdvmhost", "General", "Id")
	digits := func(s string) string {
		return strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, s)
	}
	targets := map[string][3]string{
		"dmrExtendedId":     {"mmdvmhost", "DMR", "Id"},
		"bmExtendedId":      {"dmrgateway", "DMR Network 1", "Id"},
		"dmrPlusExtendedId": {"dmrgateway", "DMR Network 2", "Id"},
	}
	for _, e := range essids {
		if val, ok := a.posted(e.name, e.rendered); ok && val != "" && base != "" {
			t := targets[e.name]
			a.set(t[0], t[1], t[2], base+digits(val))
		}
	}

	// DMR Master (configure.php 1512-1620).
	mmOptionsSet := false
	if haveMaster {
		const mm, net = "mmdvmhost", "DMR Network"
		gw := master.Addr == "127.0.0.1" && master.Port == "62031"
		a.set(mm, net, "Address", master.Addr)
		a.set(mm, net, "RemoteAddress", master.Addr)
		if !a.present(mm, net, "Password") {
			a.set(mm, net, "Password", dmrQuoted(master.Pass))
		}
		a.set(mm, net, "Port", master.Port)
		a.set(mm, net, "RemotePort", master.Port)
		if gw {
			a.set(mm, net, "Type", "Gateway")
		} else {
			a.set(mm, net, "Type", "Direct")
		}
		if bmPwPosted && strings.HasPrefix(master.Name, "BM") {
			if bmPw != "" {
				a.modemSet("BrandMeister", dmrPtr(dmrQuoted(bmPw)))
				if master.Addr != "127.0.0.1" {
					a.set(mm, net, "Password", dmrQuoted(bmPw))
				}
			} else {
				a.modemSet("BrandMeister", nil)
			}
		}
		if tgifPwPosted && strings.HasPrefix(master.Name, "TGIF") {
			if tgifPw != "" {
				a.modemSet("TGIF", dmrPtr(dmrQuoted(tgifPw)))
				if master.Addr != "127.0.0.1" {
					a.set(mm, net, "Password", dmrQuoted(tgifPw))
				}
			} else {
				a.modemSet("TGIF", nil)
			}
		}
		if strings.HasPrefix(master.Name, "BM") {
			a.del(mm, net, "Options")
			a.del("dmrgateway", "DMR Network 2", "Options")
			a.del(mm, net, "Local")
			a.del(mm, net, "LocalPort")
			a.del("ysf2dmr", "DMR Network", "Options")
			a.del("ysf2dmr", "DMR Network", "Local")
			if pw, ok := a.modemPassword("BrandMeister"); ok {
				a.set(mm, net, "Password", dmrQuoted(strings.ReplaceAll(pw, `"`, "")))
			}
		}
		if gw {
			a.del(mm, net, "Options")
			a.del("dmrgateway", "DMR Network 2", "Options")
			a.set(mm, net, "Local", "62032")
			a.set(mm, net, "LocalPort", "62032")
			a.del("ysf2dmr", "DMR Network", "Options")
			a.set("ysf2dmr", "DMR Network", "Local", "62032")
			if dmrPresent(c, "dmr2ysf", "DMR Network", "LocalAddress") {
				a.set("dmr2ysf", "DMR Network", "LocalAddress", "127.0.0.1")
			}
			if dmrPresent(c, "dmr2nxdn", "DMR Network", "LocalAddress") {
				a.set("dmr2nxdn", "DMR Network", "LocalAddress", "127.0.0.1")
			}
		}
		if master.Addr == "127.0.0.2" && master.Port == "62033" {
			a.del(mm, net, "Options")
			a.set(mm, net, "Local", "62034")
			a.set(mm, net, "LocalPort", "62034")
			if dmrPresent(c, "dmr2ysf", "DMR Network", "LocalAddress") {
				a.set("dmr2ysf", "DMR Network", "LocalAddress", "127.0.0.2")
			}
		}
		if master.Addr == "127.0.0.3" && master.Port == "62035" {
			a.del(mm, net, "Options")
			a.set(mm, net, "Local", "62036")
			a.set(mm, net, "LocalPort", "62036")
			if dmrPresent(c, "dmr2nxdn", "DMR Network", "LocalAddress") {
				a.set("dmr2nxdn", "DMR Network", "LocalAddress", "127.0.0.3")
			}
		}
		if strings.HasPrefix(master.Name, "TGIF") {
			a.del(mm, net, "Options")
		}
		n := master.Name
		if strings.HasPrefix(n, "DMR+") || strings.HasPrefix(n, "HB_") || strings.HasPrefix(n, "FD_") || strings.HasPrefix(n, "FreeDMR_") || strings.HasPrefix(n, "FreeSTAR_") {
			a.del(mm, net, "Local")
			a.del(mm, net, "LocalPort")
			a.del("ysf2dmr", "DMR Network", "Local")
			if opts != "" {
				a.set(mm, net, "Options", dmrQuoted(opts))
				a.set("dmrgateway", "DMR Network 2", "Options", dmrQuoted(opts))
				mmOptionsSet = true
			} else {
				a.del(mm, net, "Options")
				a.del("dmrgateway", "DMR Network 2", "Options")
				a.del("ysf2dmr", "DMR Network", "Options")
			}
		}
	}
	// Not reproduced: with no master posted the classic unsets the Options
	// keys (1621-1624), which only happens when this section is hidden.

	// DMRGateway networks (1625-1678).
	if haveBM {
		const n1 = "DMR Network 1"
		a.set("dmrgateway", n1, "Address", bm.Addr)
		if !a.present("dmrgateway", n1, "Password") {
			a.set("dmrgateway", n1, "Password", dmrQuoted(bm.Pass))
		}
		if bmPw != "" {
			a.set("dmrgateway", n1, "Password", dmrQuoted(bmPw))
		}
		a.set("dmrgateway", n1, "Port", bm.Port)
		a.set("dmrgateway", n1, "Name", bm.Name)
	}
	if havePlus {
		const n2 = "DMR Network 2"
		a.set("dmrgateway", n2, "Address", plus.Addr)
		a.set("dmrgateway", n2, "Password", dmrQuoted(plus.Pass))
		a.set("dmrgateway", n2, "Port", plus.Port)
		a.set("dmrgateway", n2, "Name", plus.Name)
	}
	// Fix: the DMR+ Options row is applied whenever it is shown. The classic
	// handles it only inside the DMR+ Master branch, so with no DMR+ host
	// selected the Options just cleared by the DMRGateway master were lost;
	// and it no longer removes the mmdvmhost Options that a switch to a
	// DMR+ direct master has just written.
	if v.gateway && optsPosted {
		if opts != "" {
			if !mmOptionsSet {
				a.del("mmdvmhost", "DMR Network", "Options")
			}
			a.set("dmrgateway", "DMR Network 2", "Options", dmrQuoted(opts))
		} else {
			a.del("dmrgateway", "DMR Network 2", "Options")
		}
	}
	// The classic also writes Address/Password/Port/Name to [XLX Network 1]
	// and then deletes that section for every DMRGateway newer than
	// 20170924 (3078-3082); only [XLX Network] Startup persists, so only it
	// is written.
	if haveXLX {
		startupName := ""
		if len(xlx.Name) > 4 {
			startupName = xlx.Name[4:]
		}
		a.set("dmrgateway", "XLX Network", "Startup", startupName)
	}
	if startupPosted && startup != "" {
		if startup == "None" {
			a.del("dmrgateway", "XLX Network 1", "Startup")
		} else {
			a.set("dmrgateway", "XLX Network 1", "Startup", startup)
		}
	}
	if modulePosted && module != "" {
		if module == "Default" {
			a.del("dmrgateway", "XLX Network", "Module")
		} else {
			a.set("dmrgateway", "XLX Network", "Module", module)
		}
	}

	// configure.php 1685: the JitterBuffer option is always dropped.
	a.del("mmdvmhost", "DMR Network", "JitterEnabled")

	// Toggles (1687-1715). An unedited toggle does not add a missing key.
	shownToggle := map[string]bool{
		"dmrEmbeddedLCOnly": dmrIsOne(c.get("mmdvmhost", "DMR", "EmbeddedLCOnly")),
		"dmrDumpTAData":     dmrIsOne(c.get("mmdvmhost", "DMR", "DumpTAData")),
		"dmrGatewayXlxEn":   dmrXLXEnabledShown(c),
		"dmrGatewayNet2En":  dmrIsOne(c.get("dmrgateway", "DMR Network 2", "Enabled")),
		"dmrGatewayNet1En":  dmrIsOne(c.get("dmrgateway", "DMR Network 1", "Enabled")),
	}
	keys := map[string][3]string{
		"dmrEmbeddedLCOnly": {"mmdvmhost", "DMR", "EmbeddedLCOnly"},
		"dmrDumpTAData":     {"mmdvmhost", "DMR", "DumpTAData"},
		"dmrGatewayXlxEn":   {"dmrgateway", "XLX Network", "Enabled"},
		"dmrGatewayNet2En":  {"dmrgateway", "DMR Network 2", "Enabled"},
		"dmrGatewayNet1En":  {"dmrgateway", "DMR Network 1", "Enabled"},
	}
	for _, t := range toggles {
		if !t.ok {
			continue
		}
		k := keys[t.name]
		if (t.val == "1") == shownToggle[t.name] && !dmrPresent(c, k[0], k[1], k[2]) {
			continue
		}
		a.set(k[0], k[1], k[2], t.val)
		// The classic sets the legacy [XLX Network 1] Enabled too; it is
		// kept in step only where that key still exists, since it is read
		// for the toggle state.
		if t.name == "dmrGatewayXlxEn" && dmrPresent(c, "dmrgateway", "XLX Network 1", "Enabled") {
			a.set("dmrgateway", "XLX Network 1", "Enabled", t.val)
		}
	}

	// Colour code (2621-2624). Fix: 0 is saved (PHP empty("0") skipped it,
	// spec §3.23); an unedited 0 shown for a missing key adds nothing.
	if ccPosted && cc != "" && !(cc == dmrColorCodeShown(c) && !dmrPresent(c, "mmdvmhost", "DMR", "ColorCode")) {
		a.set("mmdvmhost", "DMR", "ColorCode", cc)
	}

	a.flushModem()
	return nil
}

// flushModem queues the BrandMeister/TGIF password changes for the active
// controller marker (/etc/dstar-radio.mmdvmhost) as system op "modem_ini":
// {section: {key: value|null}}, values written as given, null removes the
// key. Only real changes are queued, and they merge with other sections'.
func (a *dmrApply) flushModem() {
	for section, keys := range a.modem {
		for key, val := range keys {
			cur, has := a.v.modem.get(section, key), a.v.modem.has(section, key)
			if (val == nil && !has) || (val != nil && has && unquote(*val) == cur) {
				continue
			}
			a.p.modemSet(section, key, val)
		}
	}
}

func dmrPtr(s string) *string { return &s }
