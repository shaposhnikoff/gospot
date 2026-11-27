package main

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Configuration page: "D-Star Configuration" (Order 60) and "Yaesu System
// Fusion Configuration" (Order 70), docs/configure-spec/03-dstar-ysf.md,
// classic admin/configure.php form lines 4869-5306.
//
// Interface with the General section (Order 40, configure.php 923-1141):
//
//   - The D-Star module / RPT1 / RPT2 logic of the classic frequency handlers
//     lives here, in planDStarModule. It runs only when the General section
//     posted a frequency (confFREQ, or both confFREQrx and confFREQtx), as in
//     the classic page, and reads the values General planned through
//     p.value(): mmdvmhost [General] Callsign (fallback: ircddbgateway
//     gatewayCallsign) and mmdvmhost [Info] TXFrequency in Hz (fallback:
//     dstarrepeater dvmegaTXFrequency). General therefore must run first
//     (lower Order) and must not write dstarrepeater callsign/gateway/
//     beaconText, ircddbgateway repeaterBand1/repeaterCall1, mmdvmhost
//     [D-Star] Module or timeserver sendA..sendZ itself.
//   - While the D-Star section is visible its Apply calls planDStarModule with
//     the posted confDStarModuleSuffix. When the section is hidden (MMDVMHost
//     with D-Star disabled) the classic page re-derives the module from the
//     band on every save; General reproduces that by calling
//     planDStarModule(c, form, p, "") when buildDStarSection(c) == nil.
//   - The every-save defaults of the YSF/DGId/D-Star files (spec part 3) are
//     in planDStarYSFDefaults; both appliers call it (it is idempotent). The
//     classic page runs them even when both sections are hidden, which needs
//     a framework hook (see the report).
//
// Markers and files outside configState (spec 1.3.6, 1.3.8, 2.3.2): the
// timeserver.dissable, hostfiles.nodextra and hostfiles.ysfupper markers are
// read from c.St.System.Markers under the names below and changed through the
// system operations below. They are used only when the helper reports the
// marker (the key is present in Markers, nil meaning "absent"); otherwise the
// toggle shows the classic default and Apply leaves the marker alone.
const (
	dsyMarkerTimeDisable = "timeserver-disable"  // /etc/timeserver.dissable
	dsyMarkerNoDExtra    = "hostfiles-nodextra"  // /etc/hostfiles.nodextra
	dsyMarkerYSFUpper    = "hostfiles-ysfupper"  // /etc/hostfiles.ysfupper (legacy)
	dsyOpTimeAnnounce    = "timeserver_announce" // "on" removes, "off" creates the marker
	dsyOpNoDExtra        = "hostfiles_nodextra"  // "on" creates, "off" removes the marker
	dsyOpYSFUpper        = "hostfiles_ysfupper"  // "off" removes the legacy marker
)

func init() {
	registerCfgSection(cfgSectionImpl{ID: "dstar", Order: 60, Build: buildDStarSection, Apply: applyDStarSection})
	registerCfgSection(cfgSectionImpl{ID: "ysf", Order: 70, Build: buildYSFSection, Apply: applyYSFSection})
}

// ---------------------------------------------------------------------------
// Shared helpers (prefixed dsy to stay clear of the other section files).

// dsyTrue is the classic `parse_ini_file(...)[s][k] == 1`.
func dsyTrue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "on", "yes", "true":
		return true
	}
	return false
}

// dsyTruthy is PHP truthiness of a parse_ini_file value.
func dsyTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "0", "no", "off", "false", "none":
		return false
	}
	return true
}

// dsyFlat reads a flat file (/etc/ircddbgateway, /etc/dstarrepeater,
// /etc/timeserver) the classic way: all '"' removed, trimmed.
func dsyFlat(c *cfgContext, file, key string) string {
	return strings.TrimSpace(strings.ReplaceAll(c.St.get(file, "", key), `"`, ""))
}

func dsyStored(c *cfgContext, file, section, key string) bool {
	_, ok := c.St.Files[file][section][key]
	return ok
}

// dsySetFlat stages a flat-file edit: config_writer_stage_flat never appends
// a missing key, so neither do we.
func dsySetFlat(c *cfgContext, p *cfgPlan, file, key, value string) {
	if c.has(file) && dsyStored(c, file, "", key) {
		p.set(file, "", key, value)
	}
}

// dsySet/dsyDel edit INI files that exist; a missing file is never created.
func dsySet(c *cfgContext, p *cfgPlan, file, section, key, value string) {
	if c.has(file) {
		p.set(file, section, key, value)
	}
}

func dsyDel(c *cfgContext, p *cfgPlan, file, section, key string) {
	if c.has(file) {
		p.del(file, section, key)
	}
}

// dsyMissing is PHP !isset() on the in-memory array: planned edits first.
func dsyMissing(c *cfgContext, p *cfgPlan, file, section, key string) bool {
	if v, ok := p.change.Changes[file][section][key]; ok {
		return v == nil
	}
	return !dsyStored(c, file, section, key)
}

func dsyDefault(c *cfgContext, p *cfgPlan, file, section, key, value string) {
	if c.has(file) && dsyMissing(c, p, file, section, key) {
		p.set(file, section, key, value)
	}
}

// dsyDelSection is PHP unset($ini[section]): every key of the section goes
// (the helper keeps the empty [section] header line).
func dsyDelSection(c *cfgContext, p *cfgPlan, file, section string) {
	if !c.has(file) {
		return
	}
	for key := range c.St.Files[file][section] {
		p.del(file, section, key)
	}
	for key, v := range p.change.Changes[file][section] {
		if v != nil {
			p.del(file, section, key)
		}
	}
}

// dsyMarker reports whether a marker file exists and whether the helper
// reports that marker at all.
func dsyMarker(c *cfgContext, name string) (exists, supported bool) {
	m, ok := c.St.System.Markers[name]
	return ok && m != nil, ok
}

// dsyOptions removes duplicate values (the classic lists repeat entries,
// which the keyed option list of the UI cannot render).
func dsyOptions(opts []linkOption) []linkOption {
	seen := map[string]bool{}
	out := opts[:0:0]
	for _, o := range opts {
		if !seen[o.Value] {
			seen[o.Value] = true
			out = append(out, o)
		}
	}
	return out
}

func dsyHasOption(opts []linkOption, v string) bool {
	for _, o := range opts {
		if o.Value == v {
			return true
		}
	}
	return false
}

func dsyLetters() []linkOption {
	out := make([]linkOption, 0, 26)
	for l := 'A'; l <= 'Z'; l++ {
		out = append(out, linkOption{string(l), string(l)})
	}
	return out
}

// dsyLines reads a host list line by line (the classic fgets loops).
func dsyLines(name string) []string {
	lines, _ := cachedHostFile("lines", filepath.Join(gospotHostsDir, name), func(f io.Reader) []string {
		var out []string
		s := bufio.NewScanner(f)
		for s.Scan() {
			out = append(out, s.Text())
		}
		return out
	})
	return cloneRows(lines)
}

func dsyHeader() cfgRow {
	return cfgRow{Cells: []cfgCell{
		{Header: true, Style: "width:200px", Label: "Setting", Tooltip: tip("Setting", "")},
		{Header: true, Colspan: 2, Label: "Value", Tooltip: tip("Value", "The current value from the\nconfiguration files")},
	}}
}

func dsyLabel(label, title, body string) cfgCell {
	return cfgCell{Align: "left", Label: label, Tooltip: tip(title, body)}
}

func dsyRow(id string, cells ...cfgCell) cfgRow { return cfgRow{ID: id, Cells: cells} }

func dsyWide(controls ...cfgControl) cfgCell {
	return cfgCell{Align: "left", Colspan: 2, Controls: controls}
}

func dsyToggle(name string, on bool, label string) cfgControl {
	t := toggle(name, on)
	t.Attrs = map[string]string{"label": label}
	return t
}

func dsyAttrs(kv ...string) map[string]string {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return m
}

var (
	dsyDigits     = regexp.MustCompile(`^[0-9]+$`)
	dsyLetter     = regexp.MustCompile(`^[A-Z]$`)
	dsyReflector  = regexp.MustCompile(`^[A-Z0-9]{1,7}$`)
	dsyPrintable  = regexp.MustCompile(`^[\x20-\x7e]*$`)
	dsyCallsignRe = regexp.MustCompile(`^[A-Z0-9]{1,8}$`)
)

// dsyToggleValue reads a posted toggle; an absent name is left alone.
func dsyToggleValue(form cfgForm, name string) (on, posted bool, err error) {
	if !form.has(name) {
		return false, false, nil
	}
	switch form[name] {
	case "ON":
		return true, true, nil
	case "OFF":
		return false, true, nil
	}
	return false, false, cfgError("Invalid value for " + name)
}

// dsyPosted reports whether the request carries any of a section's
// always-rendered controls (another form, such as the password form, posts
// none of them and must not trigger this section's writes or defaults).
func dsyPosted(form cfgForm, names ...string) bool {
	for _, n := range names {
		if form.has(n) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// D-Star module / RPT1 (General's frequency handlers, spec 1.3.1).

// dsyBandModule is the classic TX-frequency band -> module table (Hz).
func dsyBandModule(hz string) string {
	f, err := strconv.ParseInt(strings.TrimSpace(hz), 10, 64)
	if err != nil {
		return ""
	}
	switch {
	case f >= 1240000000 && f <= 1300000000:
		return "A"
	case f >= 420000000 && f <= 450000000:
		return "B"
	case f >= 218000000 && f <= 226000000:
		return "A"
	case f >= 144000000 && f <= 148000000:
		return "C"
	}
	return ""
}

// dsyCallsign is the station callsign General planned (or the stored one).
func dsyCallsign(p *cfgPlan) string {
	call := p.value("mmdvmhost", "General", "Callsign")
	if call == "" {
		call = strings.ReplaceAll(p.value("ircddbgateway", "", "gatewayCallsign"), `"`, "")
	}
	return strings.ToUpper(strings.TrimSpace(call))
}

// planDStarModule stages RPT1/RPT2, the module letter and the timeserver
// send flags. suffix is "" (derive from the TX band) or one letter.
func planDStarModule(c *cfgContext, form cfgForm, p *cfgPlan, suffix string) error {
	if form["confFREQ"] == "" && (form["confFREQtx"] == "" || form["confFREQrx"] == "") {
		return nil // classic: no frequency posted, the suffix is ignored
	}
	suffix = strings.ToUpper(strings.TrimSpace(suffix))
	if suffix != "" && !dsyLetter.MatchString(suffix) {
		return cfgError("The D-Star module must be a single letter A-Z")
	}
	call := dsyCallsign(p)
	if call == "" {
		return nil
	}
	if !dsyCallsignRe.MatchString(call) {
		return cfgError("Invalid callsign for the D-Star RPT1/RPT2 fields")
	}
	letter := suffix
	if letter == "" {
		tx := p.value("mmdvmhost", "Info", "TXFrequency")
		if tx == "" {
			tx = dsyFlat(c, "dstarrepeater", "dvmegaTXFrequency")
		}
		letter = dsyBandModule(tx)
	}
	// configure.php 882-886: sendA..sendE=0 on every save, then send<L>=1.
	for _, l := range "ABCDE" {
		dsySetFlat(c, p, "timeserver", "send"+string(l), "0")
	}
	if letter == "" {
		// Reference defect (spec 1.3.1): out of band and no suffix, the
		// classic page stages empty RPT1/beaconText/repeaterBand1. Keep them.
		return nil
	}
	padded := call
	for len(padded) < 7 {
		padded += " "
	}
	rpt1, rpt2 := padded+letter, padded+"G"
	dsySetFlat(c, p, "dstarrepeater", "callsign", rpt1)
	dsySetFlat(c, p, "dstarrepeater", "gateway", rpt2)
	dsySetFlat(c, p, "dstarrepeater", "beaconText", rpt1)
	dsySetFlat(c, p, "ircddbgateway", "repeaterBand1", letter)
	dsySetFlat(c, p, "ircddbgateway", "repeaterCall1", call)
	dsySet(c, p, "mmdvmhost", "D-Star", "Module", letter)
	dsySetFlat(c, p, "timeserver", "send"+letter, "1")
	return nil
}

// ---------------------------------------------------------------------------
// D-Star Configuration.

func dsyDStarVisible(c *cfgContext) bool {
	_, dstarrepeater := c.marker("dstarrepeater")
	return dstarrepeater || dsyTrue(c.get("mmdvmhost", "D-Star", "Enable"))
}

// ircddbgateway_languages.inc: display name; ircddbgateway index; timeserver index.
var dsyLanguages = [][3]string{
	{"English_(UK)", "0", "0"}, {"Deutsch", "1", "4"}, {"Dansk", "2", "0"}, {"Francais", "3", "6"},
	{"Italiano", "4", "0"}, {"Polski", "5", "0"}, {"English_(US)", "6", "2"}, {"Espanol", "7", "9"},
	{"Svenska", "8", "8"}, {"Nederlands_(NL)", "9", "7"}, {"Nederlands_(BE)", "10", "7"},
	{"Norsk", "11", "10"}, {"Portugues", "12", "11"},
}

func dsyLanguageOptions() []linkOption {
	out := make([]linkOption, 0, len(dsyLanguages))
	for _, l := range dsyLanguages {
		out = append(out, linkOption{l[1] + "," + l[2], l[0]})
	}
	return out
}

// dsyReflectorOptions lists the default reflector choices (configure.php
// 4928-4962): current, Text Entry, then DCS/XLX, REF/XRF, XRF host lines.
func dsyReflectorOptions(c *cfgContext) []linkOption {
	cur := dsyFlat(c, "ircddbgateway", "reflector1")
	if len(cur) > 6 {
		cur = cur[:6]
	}
	opts := []linkOption{{cur, cur}, {"customOption", "Text Entry"}}
	add := func(file string, needles ...string) {
		for _, line := range dsyLines(file) {
			if strings.Contains(line, "#") {
				continue
			}
			for _, n := range needles {
				if strings.Contains(line, n) {
					v := line
					if len(v) > 6 {
						v = v[:6]
					}
					v = strings.TrimRight(v, " \t")
					opts = append(opts, linkOption{v, v})
				}
			}
		}
	}
	add("DCS_Hosts.txt", "DCS", "XLX")
	add("DPlus_Hosts.txt", "REF", "XRF")
	add("DExtra_Hosts.txt", "XRF")
	return dsyOptions(opts)
}

func dsyModuleOptions(cur string) []linkOption {
	return dsyOptions(append([]linkOption{{cur, cur}}, dsyLetters()...))
}

func buildDStarSection(c *cfgContext) *cfgSection {
	if !dsyDStarVisible(c) {
		return nil
	}
	call := dsyFlat(c, "dstarrepeater", "callsign")
	rpt1, letter := call, ""
	if len(call) > 7 {
		rpt1, letter = call[:7], call[7:]
	}
	refl := dsyFlat(c, "ircddbgateway", "reflector1")
	reflName, reflLetter := refl, ""
	if len(refl) > 6 {
		reflName = refl[:6]
	}
	if len(refl) > 7 {
		reflLetter = refl[7:]
	}
	auto := ""
	switch dsyFlat(c, "ircddbgateway", "atStartup1") {
	case "1":
		auto = "ON"
	case "0":
		auto = "OFF"
	}
	lang := ""
	for _, l := range dsyLanguages {
		if dsyFlat(c, "ircddbgateway", "language") == l[1] {
			lang = l[1] + "," + l[2]
		}
	}
	timeOff, _ := dsyMarker(c, dsyMarkerTimeDisable)
	noDExtra, _ := dsyMarker(c, dsyMarkerNoDExtra)
	nbsp := func(s string) string { return strings.ReplaceAll(s, " ", " ") }

	return &cfgSection{ID: "dstar", Title: "D-Star Configuration", Apply: true, Rows: []cfgRow{
		dsyHeader(),
		dsyRow("confDStarModuleSuffix", dsyLabel("RPT1 Callsign:", "RPT1 Callsign", "This is the RPT1 field for your radio"),
			dsyWide(static(nbsp(rpt1)), selectBox("confDStarModuleSuffix", letter, dsyModuleOptions(letter)))),
		dsyRow("rpt2", dsyLabel("RPT2 Callsign:", "RPT2 Callsign", "This is the RPT2 field for your radio"),
			dsyWide(static(nbsp(dsyFlat(c, "dstarrepeater", "gateway"))))),
		dsyRow("confPassword", dsyLabel("Remote Password:", "Remote Password", "Used for ircDDBGateway remote control access"),
			dsyWide(cfgControl{Kind: "password", Name: "confPassword", Value: dsyFlat(c, "ircddbgateway", "remotePassword"),
				Attrs: dsyAttrs("size", "30", "maxlength", "30")})),
		// Deviation: the classic hidden "Text Entry" box shares the name
		// confDefRef and is swapped in by toggleField(); here it is a
		// separate always-visible confDefRefText used when "Text Entry" is
		// selected.
		dsyRow("confDefRef", dsyLabel("Default Reflector:", "Default Reflector", "Used for setting the default reflector."),
			cfgCell{Align: "left", Colspan: 1, Controls: []cfgControl{
				selectBox("confDefRef", reflName, dsyReflectorOptions(c)),
				text("confDefRefText", "", dsyAttrs("size", "7", "maxlength", "7", "label", "Default Reflector (Text Entry)")),
				selectBox("confDefRefLtr", reflLetter, dsyModuleOptions(reflLetter)),
			}},
			cfgCell{Style: "width:300px", Controls: []cfgControl{{Kind: "radio", Name: "confDefRefAuto", Value: auto,
				Options: []linkOption{{"ON", "Startup"}, {"OFF", "Manual"}}}}}),
		dsyRow("ircDDBGatewayAnnounceLanguage", dsyLabel("ircDDBGateway Language:", "ircDDBGateway Language", "Set your prefered language here"),
			cfgCell{Colspan: 2, Style: "text-align: left;", Controls: []cfgControl{
				selectBox("ircDDBGatewayAnnounceLanguage", lang, dsyLanguageOptions())}}),
		dsyRow("confTimeAnnounce", dsyLabel("Time Announcements:", "Time Announce", "Announce time hourly"),
			dsyWide(dsyToggle("confTimeAnnounce", !timeOff, "D-Star Time Announcements"))),
		dsyRow("confircddbEnabled", dsyLabel("Callsign Routing:", "Callsign Routing", "Do you want callsign routing for D-Star"),
			cfgCell{Align: "left", Controls: []cfgControl{dsyToggle("confircddbEnabled", dsyFlat(c, "ircddbgateway", "ircddbEnabled") == "1", "Use ircDDB callsign routing")}},
			cfgCell{Controls: []cfgControl{static("Connect ircDDB for call routing")}}),
		dsyRow("confHostFilesNoDExtra", dsyLabel("Use DPlus for XRF:", "No DExtra", "Should host files use DPlus Protocol for XRFs"),
			cfgCell{Align: "left", Controls: []cfgControl{dsyToggle("confHostFilesNoDExtra", noDExtra, "Use D-Plus for XRF Hosts")}},
			cfgCell{Controls: []cfgControl{static("Note: Update Required if changed")}}),
	}}
}

var dsyDStarNames = []string{"confDStarModuleSuffix", "confPassword", "confDefRef", "confDefRefText", "confDefRefLtr",
	"confDefRefAuto", "ircDDBGatewayAnnounceLanguage", "confTimeAnnounce", "confircddbEnabled", "confHostFilesNoDExtra"}

func applyDStarSection(c *cfgContext, form cfgForm, p *cfgPlan) error {
	if !dsyPosted(form, dsyDStarNames...) {
		return nil
	}
	// RPT1 module suffix (only together with General's frequency).
	suffix := form["confDStarModuleSuffix"]
	if suffix != "" && !dsyHasOption(dsyModuleOptions(""), strings.ToUpper(suffix)) {
		return cfgError("The D-Star module must be a single letter A-Z")
	}
	if err := planDStarModule(c, form, p, suffix); err != nil {
		return err
	}

	// Remote password (687-703): an empty field never clears it.
	if pw := form["confPassword"]; pw != "" {
		if len(pw) > 30 || !dsyPrintable.MatchString(pw) || strings.Contains(pw, `"`) {
			return cfgError("The Remote Password must be up to 30 printable characters without quotes")
		}
		dsySetFlat(c, p, "ircddbgateway", "remotePassword", pw)
		// Not ported: /root/.Remote Control password/port (no helper operation).
	}

	// Default reflector (706-715).
	ref := form["confDefRef"]
	if ref == "customOption" {
		ref = form["confDefRefText"]
	}
	ref = strings.ToUpper(strings.TrimSpace(ref))
	ltr := strings.ToUpper(form["confDefRefLtr"])
	if ref != "" {
		if !dsyReflector.MatchString(ref) {
			return cfgError("The Default Reflector must be 1-7 letters or digits")
		}
		if ltr != "" && !dsyLetter.MatchString(ltr) {
			return cfgError("The Default Reflector module must be a single letter A-Z")
		}
		call := dsyCallsign(p)
		current := dsyFlat(c, "ircddbgateway", "reflector1")
		target := ref
		for len(target) < 7 {
			target += " "
		}
		target += ltr
		switch {
		case call != "" && strings.Contains(ref, call):
			// classic: a reflector containing the own callsign is ignored
		case strings.TrimSpace(target) == current:
			// unchanged (also covers a stored reflector without a letter)
		case ltr == "":
			return cfgError("Select a module letter for the Default Reflector")
		default:
			dsySetFlat(c, p, "ircddbgateway", "reflector1", target)
		}
	}

	// Startup / Manual (718-725).
	switch form["confDefRefAuto"] {
	case "":
	case "ON":
		dsySetFlat(c, p, "ircddbgateway", "atStartup1", "1")
	case "OFF":
		dsySetFlat(c, p, "ircddbgateway", "atStartup1", "0")
	default:
		return cfgError("Invalid value for confDefRefAuto")
	}

	// Language (875-879).
	if v := form["ircDDBGatewayAnnounceLanguage"]; v != "" {
		if !dsyHasOption(dsyLanguageOptions(), v) {
			return cfgError("Unknown ircDDBGateway language")
		}
		irc, ts, _ := strings.Cut(v, ",")
		dsySetFlat(c, p, "ircddbgateway", "language", irc)
		dsySetFlat(c, p, "timeserver", "language", ts)
	}

	// Time announcements (2455-2458): marker /etc/timeserver.dissable.
	announce, posted, err := dsyToggleValue(form, "confTimeAnnounce")
	if err != nil {
		return err
	}
	if off, ok := dsyMarker(c, dsyMarkerTimeDisable); ok && posted && announce == off {
		p.system(dsyOpTimeAnnounce, map[bool]string{true: "on", false: "off"}[announce])
	}

	// Callsign routing (1255-1266).
	routing, posted, err := dsyToggleValue(form, "confircddbEnabled")
	if err != nil {
		return err
	}
	if posted {
		dsySetFlat(c, p, "ircddbgateway", "ircddbEnabled", boolString(routing))
		if dsyFlat(c, "ircddbgateway", "ircddbHostname") == "rr.openquad.net" {
			dsySetFlat(c, p, "ircddbgateway", "ircddbEnabled", "0")
			dsySetFlat(c, p, "ircddbgateway", "ircddbHostname", "ircv4.openquad.net")
		}
	}

	// Use DPlus for XRF (3258-3265): marker /etc/hostfiles.nodextra.
	nodextra, posted, err := dsyToggleValue(form, "confHostFilesNoDExtra")
	if err != nil {
		return err
	}
	if exists, ok := dsyMarker(c, dsyMarkerNoDExtra); ok && posted && nodextra != exists {
		p.system(dsyOpNoDExtra, map[bool]string{true: "on", false: "off"}[nodextra])
	}

	planDStarYSFDefaults(c, p)
	return nil
}

// ---------------------------------------------------------------------------
// Yaesu System Fusion Configuration.

func dsyYSFVisible(c *cfgContext) bool {
	_, mmdvm := c.marker("mmdvmhost")
	return mmdvm && (dsyTrue(c.get("mmdvmhost", "System Fusion Network", "Enable")) || dsyTrue(c.get("dmr2ysf", "Enabled", "Enabled")))
}

func dsyYSF2DMR(c *cfgContext) bool  { return dsyTrue(c.get("ysf2dmr", "Enabled", "Enabled")) }
func dsyYSF2NXDN(c *cfgContext) bool { return dsyTrue(c.get("ysf2nxdn", "Enabled", "Enabled")) }
func dsyYSF2P25(c *cfgContext) bool  { return dsyTrue(c.get("ysf2p25", "Enabled", "Enabled")) }

// dsyYSFHosts lists the startup hosts (5068-5123) and the selected value.
// Reference defect fixed: without [FCS Network] the classic save stores the
// host id, which the name-based preselection never matched (so the next save
// reset it to None); the id is matched too. An unmatched stored host selects
// nothing ("") instead of silently falling back to None.
func dsyYSFHosts(c *cfgContext) ([]linkOption, string) {
	startup, set := c.St.Files["ysfgateway"]["Network"]["Startup"]
	startup = unquote(startup)
	selected := ""
	if !set {
		selected = "none"
	}
	opts := []linkOption{{"none", "None"}}
	add := func(value, label string, keys ...string) {
		opts = append(opts, linkOption{value, label})
		for _, k := range keys {
			if set && k != "" && startup == k {
				selected = value
			}
		}
	}
	add("00001,ZZ Parrot", "YSF00001 - Parrot", "ZZ Parrot", "00001")
	add("00002,YSF2DMR", "YSF00002 - Link YSF2DMR", "YSF2DMR", "00002")
	add("00003,YSF2NXDN", "YSF00003 - Link YSF2NXDN", "YSF2NXDN", "00003")
	add("00004,YSF2P25", "YSF00004 - Link YSF2P25", "YSF2P25", "00004")
	for _, h := range hostsFile("YSFHosts.txt", ";") {
		desc := ""
		if len(h) > 2 {
			desc = h[2]
		}
		add(h[0]+","+h[1], "YSF"+h[0]+" - "+h[1]+" - "+desc, h[1], h[0])
	}
	for _, h := range hostsFile("FCSHosts.txt", ";") {
		if strings.HasPrefix(h[0], "FCS") {
			add(h[0]+","+h[0], h[0]+" - "+h[1], h[0])
		}
	}
	return dsyOptions(opts), selected
}

// dsyYSF2DMRIds builds the ESSID list (5157-5186). Reference defect fixed:
// "None" always carries the base ID (classic posted the literal "None" when
// an ESSID existed, which wrote an empty Id).
func dsyYSF2DMRIds(c *cfgContext) (shown string, opts []linkOption, selected string) {
	gen, set := c.St.Files["mmdvmhost"]["General"]["Id"]
	gen = unquote(gen)
	base := "1234567"
	if set {
		base = gen
		if len(base) > 7 {
			base = base[:7]
		}
		if gen != "1234567" {
			shown = base
		}
	}
	essid := "None"
	if cur, ok := c.St.Files["ysf2dmr"]["DMR Network"]["Id"]; ok {
		cur = unquote(cur)
		if len(cur) > len(gen) {
			essid = cur[len(gen):]
		}
	}
	opts = []linkOption{{base, "None"}}
	if essid == "None" {
		selected = base
	} else {
		// An ESSID that is not two digits matches no option; keep the
		// stored Id so an unchanged form does not rewrite it.
		selected = c.get("ysf2dmr", "DMR Network", "Id")
	}
	for i := 1; i <= 99; i++ {
		nn := strconv.Itoa(100 + i)[1:]
		opts = append(opts, linkOption{base + nn, nn})
		if essid == nn {
			selected = base + nn
		}
	}
	return shown, dsyOptions(opts), selected
}

// dsyDMRMasters lists DMR_Hosts.txt (5187-5203) keyed by option value.
// An unmatched address selects nothing (classic: the first host, which an
// unchanged save then wrote).
func dsyDMRMasters(c *cfgContext) ([]linkOption, map[string][]string, string) {
	address := c.get("ysf2dmr", "DMR Network", "Address")
	var opts []linkOption
	hosts := map[string][]string{}
	selected := ""
	for _, h := range hostsFile("DMR_Hosts.txt", "") {
		if len(h) < 5 || strings.HasPrefix(h[0], "XLX") || strings.HasPrefix(h[0], "DMRG") || strings.HasPrefix(h[0], "DMR2") {
			continue
		}
		v := h[2] + "," + h[3] + "," + h[4] + "," + h[0]
		if _, dup := hosts[v]; !dup {
			hosts[v] = h
			opts = append(opts, linkOption{v, h[0]})
		}
		if address == h[2] {
			selected = v // the last match wins, as in the browser
		}
	}
	return opts, hosts, selected
}

// dsyTGHosts is the NXDN/P25 startup list: None, 10 - Parrot, host files.
// parse_ini_file reads a literal "none" as "".
func dsyTGHosts(current string, files ...string) ([]linkOption, string) {
	if strings.EqualFold(current, "none") {
		current = ""
	}
	opts := []linkOption{{"none", "None"}, {"10", "10 - Parrot"}}
	selected := ""
	if current == "" {
		selected = "none"
	} else if current == "10" {
		selected = "10"
	}
	for _, file := range files {
		for _, h := range hostsFile(file, "") {
			opts = append(opts, linkOption{h[0], h[0] + " - " + h[1]})
			if current != "" && current == h[0] {
				selected = h[0]
			}
		}
	}
	return dsyOptions(opts), selected
}

// dsyModemPassword is [BrandMeister] Password of the modem file
// (/etc/dstar-radio.mmdvmhost wins over .dstarrepeater).
func dsyModemPassword(c *cfgContext) string {
	text, ok := c.marker("mmdvmhost")
	if !ok {
		text, _ = c.marker("dstarrepeater")
	}
	return parseINI(strings.NewReader(text)).get("BrandMeister", "Password")
}

func buildYSFSection(c *cfgContext) *cfgSection {
	if !dsyYSFVisible(c) {
		return nil
	}
	hosts, startup := dsyYSFHosts(c)
	upper := dsyTruthy(c.get("ysfgateway", "General", "WiresXMakeUpper"))
	pass := dsyTruthy(c.get("ysfgateway", "General", "WiresXCommandPassthrough"))
	rows := []cfgRow{
		dsyHeader(),
		dsyRow("ysfStartupHost", dsyLabel("YSF Startup Host:", "YSF Host", "Set your prefered YSF Host here"),
			cfgCell{Colspan: 2, Style: "text-align: left;", Controls: []cfgControl{selectBox("ysfStartupHost", startup, hosts)}}),
		dsyRow("confHostFilesYSFUpper", dsyLabel("UPPERCASE Hostfiles:", "UPPERCASE Hostfiles", "Should host files use UPPERCASE only - fixes issues with FT-70D radios."),
			cfgCell{Align: "left", Controls: []cfgControl{dsyToggle("confHostFilesYSFUpper", upper, "Uppercase Host Files")}},
			cfgCell{Controls: []cfgControl{static("Note: Update Required if changed")}}),
		dsyRow("wiresXCommandPassthrough", dsyLabel("WiresX Passthrough:", "WiresX Auto Passthrough", "Use this to automatically send WiresX commands through to YSF2xxx cross-over modes."),
			dsyWide(dsyToggle("wiresXCommandPassthrough", pass, "Wires-X Command Passthrough"))),
	}
	if dsyYSF2DMR(c) {
		shown, ids, id := dsyYSF2DMRIds(c)
		masters, _, master := dsyDMRMasters(c)
		var idControls []cfgControl
		if shown != "" {
			idControls = append(idControls, static(shown))
		}
		idControls = append(idControls, selectBox("ysf2dmrId", id, ids))
		rows = append(rows,
			dsyRow("ysf2dmrId", dsyLabel("(YSF2DMR)CCS7/DMR ID:", "CCS7/DMR ID", "Enter your CCS7 / DMR ID here"), dsyWide(idControls...)),
			dsyRow("ysf2dmrMasterHost", dsyLabel("DMR Master:", "DMR Master (YSF2DMR)", "Set your prefered DMR master here"),
				cfgCell{Colspan: 2, Style: "text-align: left;", Controls: []cfgControl{selectBox("ysf2dmrMasterHost", master, masters)}}),
			dsyRow("ysf2dmrHSSecurity", dsyLabel("Hotspot Security:", "DMR Master Password", "Override the Password for DMR with your own custom password, make sure you already configured this on your chosed DMR Master. Empty the field to use the default."),
				dsyWide(cfgControl{Kind: "password", Name: "ysf2dmrHSSecurity", Value: dsyModemPassword(c), Attrs: dsyAttrs("size", "30", "maxlength", "30")})),
			dsyRow("ysf2dmrTg", dsyLabel("DMR TG:", "YSF2DMR TG", "Enter your DMR TG here"),
				dsyWide(text("ysf2dmrTg", c.get("ysf2dmr", "DMR Network", "StartupDstId"), dsyAttrs("size", "13", "maxlength", "7")))),
		)
	}
	if dsyYSF2NXDN(c) {
		opts, sel := dsyTGHosts(c.get("ysf2nxdn", "NXDN Network", "StartupDstId"), "NXDNHosts.txt", "NXDNHostsLocal.txt")
		rows = append(rows,
			dsyRow("ysf2nxdnId", dsyLabel("(YSF2NXDN) NXDN ID:", "NXDN ID", "Enter your NXDN ID here"),
				dsyWide(text("ysf2nxdnId", c.get("ysf2nxdn", "NXDN Network", "Id"), dsyAttrs("size", "13", "maxlength", "5")))),
			dsyRow("ysf2nxdnStartupDstId", dsyLabel("NXDN Startup Host:", "NXDN Host", "Set your prefered NXDN Host here"),
				cfgCell{Colspan: 2, Style: "text-align: left;", Controls: []cfgControl{selectBox("ysf2nxdnStartupDstId", sel, opts)}}),
		)
	}
	if dsyYSF2P25(c) {
		opts, sel := dsyTGHosts(c.get("ysf2p25", "P25 Network", "StartupDstId"), "P25Hosts.txt", "P25HostsLocal.txt")
		rows = append(rows,
			dsyRow("ysf2p25Id", dsyLabel("(YSF2P25) CCS7/DMR ID:", "DMR ID", "Enter your CCS7 / DMR ID here"),
				dsyWide(text("ysf2p25Id", c.get("ysf2p25", "P25 Network", "Id"), dsyAttrs("size", "13", "maxlength", "7")))),
			dsyRow("ysf2p25StartupDstId", dsyLabel("P25 Startup Host:", "P25 Host", "Set your prefered P25 Host here"),
				cfgCell{Colspan: 2, Style: "text-align: left;", Controls: []cfgControl{selectBox("ysf2p25StartupDstId", sel, opts)}}),
		)
	}
	return &cfgSection{ID: "ysf", Title: "Yaesu System Fusion Configuration", Apply: true, Rows: rows}
}

func dsyDigitsField(form cfgForm, name, what string, maxLen int, maxValue int64) (string, error) {
	v := strings.TrimSpace(form[name])
	if v == "" {
		return "", nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if !dsyDigits.MatchString(v) || len(v) > maxLen || err != nil || (maxValue > 0 && (n < 1 || n > maxValue)) {
		return "", cfgError(what)
	}
	return v, nil
}

var dsyYSFNames = []string{"ysfStartupHost", "confHostFilesYSFUpper", "wiresXCommandPassthrough"}

func applyYSFSection(c *cfgContext, form cfgForm, p *cfgPlan) error {
	if !dsyPosted(form, dsyYSFNames...) {
		return nil
	}
	// YSF startup host (1335-1369).
	if v := form["ysfStartupHost"]; v != "" {
		hosts, _ := dsyYSFHosts(c)
		if !dsyHasOption(hosts, v) {
			return cfgError("Unknown YSF Startup Host")
		}
		id, name, _ := strings.Cut(v, ",")
		_, fcs := c.St.Files["ysfgateway"]["FCS Network"] // as loaded, before the migrations
		if id == "none" {
			dsyDel(c, p, "ysfgateway", "Network", "Startup")
			dsySet(c, p, "dmr2ysf", "DMR Network", "DefaultDstTG", "9")
		} else {
			startup := id
			if fcs {
				startup = name
			}
			dsySet(c, p, "ysfgateway", "Network", "Startup", startup)
			tg := id
			if strings.HasPrefix(id, "FCS") {
				tg = "9"
			}
			dsySet(c, p, "dmr2ysf", "DMR Network", "DefaultDstTG", tg)
		}
	}

	// WiresX passthrough (1372-1375) and UPPERCASE host files (1378-1382).
	pass, posted, err := dsyToggleValue(form, "wiresXCommandPassthrough")
	if err != nil {
		return err
	}
	if posted {
		dsySet(c, p, "ysfgateway", "General", "WiresXCommandPassthrough", boolString(pass))
	}
	upper, posted, err := dsyToggleValue(form, "confHostFilesYSFUpper")
	if err != nil {
		return err
	}
	if posted {
		dsySet(c, p, "ysfgateway", "General", "WiresXMakeUpper", boolString(upper))
		if exists, ok := dsyMarker(c, dsyMarkerYSFUpper); ok && exists {
			p.system(dsyOpYSFUpper, "off")
		}
	}

	if dsyYSF2DMR(c) {
		// Master (1385-1403) and Hotspot Security.
		if v := form["ysf2dmrMasterHost"]; v != "" {
			_, hosts, _ := dsyDMRMasters(c)
			h, ok := hosts[v]
			if !ok {
				return cfgError("Unknown YSF2DMR DMR Master")
			}
			sec := form["ysf2dmrHSSecurity"]
			if len(sec) > 30 || !dsyPrintable.MatchString(sec) || strings.Contains(sec, `"`) {
				return cfgError("The Hotspot Security password must be up to 30 printable characters without quotes")
			}
			dsySet(c, p, "ysf2dmr", "DMR Network", "Address", h[2])
			if dsyMissing(c, p, "ysf2dmr", "DMR Network", "Password") {
				dsySet(c, p, "ysf2dmr", "DMR Network", "Password", `"`+strings.ReplaceAll(h[3], `"`, "")+`"`)
			}
			dsySet(c, p, "ysf2dmr", "DMR Network", "Port", h[4])
			if sec != "" {
				dsySet(c, p, "ysf2dmr", "DMR Network", "Password", `"`+sec+`"`)
				if sec != unquote(dsyModemPassword(c)) {
					v := `"` + sec + `"`
					p.modemSet("BrandMeister", "Password", &v)
				}
			}
		}
		if v, err := dsyDigitsField(form, "ysf2dmrTg", "The YSF2DMR TG must be 1-7 digits", 7, 0); err != nil {
			return err
		} else if v != "" {
			dsySet(c, p, "ysf2dmr", "DMR Network", "StartupDstId", v)
		}
		// DMR ID with ESSID (1500-1503); the base is the stored mmdvmhost Id, as in the classic page.
		if v, err := dsyDigitsField(form, "ysf2dmrId", "The YSF2DMR CCS7/DMR ID must be numeric", 10, 0); err != nil {
			return err
		} else if v != "" {
			dsySet(c, p, "ysf2dmr", "DMR Network", "Id", v)
		}
	}

	if dsyYSF2NXDN(c) {
		// Startup host (1412-1422); NXDNGateway newer than 20210131 keeps
		// the startup TG in [Network] Static (migration 3108-3127).
		if v := form["ysf2nxdnStartupDstId"]; v != "" {
			opts, _ := dsyTGHosts("", "NXDNHosts.txt", "NXDNHostsLocal.txt")
			if !dsyHasOption(opts, v) {
				return cfgError("Unknown NXDN Startup Host")
			}
			dsySet(c, p, "ysf2nxdn", "NXDN Network", "StartupDstId", v) // classic writes "none" literally
			if c.has("nxdngateway") {
				p.del("nxdngateway", "Network", "Startup")
				if v == "none" {
					p.del("nxdngateway", "Network", "Static")
				} else {
					p.set("nxdngateway", "Network", "Static", v)
				}
			}
		}
		// Reference defect fixed: NXDN IDs are 16-bit (classic: no range check).
		if v, err := dsyDigitsField(form, "ysf2nxdnId", "The YSF2NXDN NXDN ID must be a number from 1 to 65535", 5, 65535); err != nil {
			return err
		} else if v != "" {
			dsySet(c, p, "ysf2nxdn", "NXDN Network", "Id", v)
		}
	}

	if dsyYSF2P25(c) {
		// Startup host (1430-1441); P25Gateway newer than 20210201 keeps
		// the startup TG in [Network] Static (migration 3090-3100).
		if v := form["ysf2p25StartupDstId"]; v != "" {
			opts, _ := dsyTGHosts("", "P25Hosts.txt", "P25HostsLocal.txt")
			if !dsyHasOption(opts, v) {
				return cfgError("Unknown P25 Startup Host")
			}
			v = strings.ToUpper(v)
			if v == "NONE" {
				dsyDel(c, p, "p25gateway", "Network", "Startup")
				dsyDel(c, p, "p25gateway", "Network", "Static")
				dsyDel(c, p, "ysf2p25", "P25 Network", "StartupDstId")
			} else {
				dsyDel(c, p, "p25gateway", "Network", "Startup")
				dsySet(c, p, "p25gateway", "Network", "Static", v)
				dsySet(c, p, "ysf2p25", "P25 Network", "StartupDstId", v)
			}
		}
		if v, err := dsyDigitsField(form, "ysf2p25Id", "The YSF2P25 CCS7/DMR ID must be 1-7 digits", 7, 0); err != nil {
			return err
		} else if v != "" {
			dsySet(c, p, "ysf2p25", "P25 Network", "Id", v)
		}
	}

	planDStarYSFDefaults(c, p)
	return nil
}

// ---------------------------------------------------------------------------
// Every-save defaults and migrations (spec part 3, configure.php 2903-3154).
// Files that do not exist are never created (reference defect: ysf2dmr,
// ysf2nxdn, ysf2p25 and dmr2ysf were written unconditionally). There are no
// version probes; the gateways are taken to be newer than every threshold in
// the classic page (Pi-Star 4 binaries), so the newest branch is applied.
// Written paths derive from GOSPOT_HOSTS_DIR and GOSPOT_LOG_DIR.

const (
	dsyDT1 = "1,34,97,95,43,3,17,0,0,0"
	dsyDT2 = "0,0,0,0,108,32,28,32,3,8"
)

func planDStarYSFDefaults(c *cfgContext, p *cfgPlan) {
	hostsPath := func(name string) string { return filepath.Join(gospotHostsDir, name) }

	// ircDDBGateway (2913-2931). Deviation: the helper appends the line at
	// the end of the flat file instead of right after ircddbEnabled=.
	if c.has("ircddbgateway") && dsyStored(c, "ircddbgateway", "", "ircddbEnabled") &&
		dsyFlat(c, "ircddbgateway", "ircddbHostname2") == "" && dsyFlat(c, "ircddbgateway", "ircddbEnabled2") == "" {
		p.set("ircddbgateway", "", "ircddbEnabled2", "0")
	}
	// ccsHost (728-732). Deviation: the classic page re-rolls the random pick
	// on every save; a valid pick is kept so unchanged forms write nothing.
	if dsyFlat(c, "ircddbgateway", "ccsEnabled") == "1" {
		ccs := []string{"CCS701", "CCS702", "CCS704"}
		cur := dsyFlat(c, "ircddbgateway", "ccsHost")
		if cur != ccs[0] && cur != ccs[1] && cur != ccs[2] {
			dsySetFlat(c, p, "ircddbgateway", "ccsHost", ccs[c.Now.UnixNano()%3])
		}
	}

	// DMR2YSF (2934-2942).
	dsyDefault(c, p, "dmr2ysf", "YSF Network", "FCSRooms", hostsPath("FCSHosts.txt"))
	dsyDefault(c, p, "dmr2ysf", "DMR Network", "DefaultDstTG", "9")
	dsyDefault(c, p, "dmr2ysf", "DMR Network", "TGUnlink", "4000")
	dsyDefault(c, p, "dmr2ysf", "DMR Network", "TGListFile", hostsPath("TGList_YSF.txt"))
	dsySet(c, p, "dmr2ysf", "Log", "DisplayLevel", "0")
	dsySet(c, p, "dmr2ysf", "Log", "FileLevel", "2")
	dsyDefault(c, p, "dmr2ysf", "YSF Network", "DT1", dsyDT1)
	dsyDefault(c, p, "dmr2ysf", "YSF Network", "DT2", dsyDT2)
	dsyDefault(c, p, "dmr2ysf", "YSF Network", "Debug", "0")

	// YSFGateway (2945-2954, 3130-3154). [Mobile GPS] and [aprs.fi] are
	// first defaulted and then dropped by the newest migration: net removal.
	g := "ysfgateway"
	dsyDefault(c, p, g, "General", "WiresXMakeUpper", "1")
	dsyDefault(c, p, g, "Network", "Revert", "0")
	dsyDefault(c, p, g, "Network", "Port", "42000")
	for _, k := range []string{"YSF2DMRAddress", "YSF2DMRPort", "DataPort", "StatusPort"} {
		dsyDel(c, p, g, "Network", k)
	}
	dsyDefault(c, p, g, "Network", "InactivityTimeout", "0")
	dsySet(c, p, g, "Network", "Debug", "0")
	for _, kv := range [][2]string{{"Enable", "1"}, {"Port", "42000"}, {"Hosts", hostsPath("YSFHosts.txt")}, {"ReloadTime", "60"},
		{"ParrotAddress", "127.0.0.1"}, {"ParrotPort", "42012"}, {"YSF2DMRAddress", "127.0.0.1"}, {"YSF2DMRPort", "42013"},
		{"YSF2NXDNAddress", "127.0.0.1"}, {"YSF2NXDNPort", "42014"}, {"YSF2P25Address", "127.0.0.1"}, {"YSF2P25Port", "42015"}} {
		dsySet(c, p, g, "YSF Network", kv[0], kv[1])
	}
	dsySet(c, p, g, "FCS Network", "Enable", "1")
	dsySet(c, p, g, "FCS Network", "Port", "42001")
	dsySet(c, p, g, "FCS Network", "Rooms", hostsPath("FCSHosts.txt"))
	dsyDefault(c, p, g, "Remote Commands", "Enable", "1")
	dsyDefault(c, p, g, "Remote Commands", "Port", "6073")
	dsyDefault(c, p, g, "General", "Debug", "0")
	dsyDefault(c, p, g, "GPSD", "Enable", "0")
	dsyDefault(c, p, g, "GPSD", "Address", "127.0.0.1")
	dsyDefault(c, p, g, "GPSD", "Port", "2947")
	dsySet(c, p, g, "APRS", "Enable", "1") // defaulted to 0, then forced to 1 (> 20210130)
	dsyDefault(c, p, g, "APRS", "Address", "127.0.0.1")
	dsyDefault(c, p, g, "APRS", "Port", "8673")
	dsySet(c, p, g, "APRS", "Description", p.value(g, "Info", "Name")+"_"+p.value(g, "General", "Suffix"))
	dsyDefault(c, p, g, "APRS", "Suffix", "Y")
	dsyDelSection(c, p, g, "Mobile GPS")
	dsyDelSection(c, p, g, "aprs.fi")

	// YSF2DMR (2957-2991).
	d := "ysf2dmr"
	for _, kv := range [][3]string{
		{"Info", "Power", "1"}, {"Info", "Height", "0"},
		{"YSF Network", "DstAddress", "127.0.0.1"}, {"YSF Network", "DstPort", "42000"},
		{"YSF Network", "LocalAddress", "127.0.0.1"}, {"YSF Network", "LocalPort", "42013"},
		{"YSF Network", "Daemon", "1"}, {"YSF Network", "EnableWiresX", "1"},
		{"DMR Network", "StartupDstId", "31672"}, {"DMR Network", "StartupPC", "0"}, {"DMR Network", "Jitter", "500"},
		{"DMR Network", "EnableUnlink", "1"}, {"DMR Network", "TGUnlink", "4000"}, {"DMR Network", "PCUnlink", "0"},
		{"DMR Network", "Debug", "0"},
		{"DMR Id Lookup", "File", hostsPath("DMRIds.dat")}, {"DMR Id Lookup", "Time", "24"}, {"DMR Id Lookup", "DropUnknown", "0"},
		{"Log", "FilePath", gospotLogDir}, {"Log", "FileRoot", "YSF2DMR"},
		{"aprs.fi", "Enable", "0"}, {"aprs.fi", "Port", "14580"}, {"aprs.fi", "Refresh", "240"},
		{"Enabled", "Enabled", "0"},
		{"YSF Network", "WiresXMakeUpper", "1"}, {"YSF Network", "DT1", dsyDT1}, {"YSF Network", "DT2", dsyDT2},
	} {
		dsyDefault(c, p, d, kv[0], kv[1], kv[2])
	}
	if _, err := os.Stat(hostsPath("TGList_BM.txt")); err == nil {
		dsyDefault(c, p, d, "DMR Network", "TGListFile", hostsPath("TGList_BM.txt"))
	}
	dsyDel(c, p, d, "Info", "Enabled")
	dsyDel(c, p, d, "DMR Network", "JitterEnabled")
	dsySet(c, p, d, "Log", "DisplayLevel", "0")
	dsySet(c, p, d, "Log", "FileLevel", "0")

	// YSF2NXDN and YSF2P25 (2994-3025). Reference defect fixed: the ports
	// are taken after the YSFGateway migration (classic used the values as
	// loaded, and wrote empty ports when ysfgateway lacked them).
	for _, x := range []struct{ file, port, lookup, lookupFile, net, tgList, root string }{
		{"ysf2nxdn", "YSF2NXDNPort", "NXDN Id Lookup", "NXDN.csv", "NXDN Network", "TGList_NXDN.txt", "YSF2NXDN"},
		{"ysf2p25", "YSF2P25Port", "DMR Id Lookup", "DMRIds.dat", "P25 Network", "TGList_P25.txt", "YSF2P25"},
	} {
		if !c.has(x.file) {
			continue
		}
		if c.has(g) {
			if v := p.value(g, "YSF Network", x.port); v != "" {
				p.set(x.file, "YSF Network", "LocalPort", v)
			}
			if v := p.value(g, "YSF Network", "Port"); v != "" {
				p.set(x.file, "YSF Network", "DstPort", v)
			}
		}
		p.set(x.file, "YSF Network", "Daemon", "1")
		p.set(x.file, "YSF Network", "EnableWiresX", "1")
		dsyDefault(c, p, x.file, "Enabled", "Enabled", "0")
		p.set(x.file, x.lookup, "File", hostsPath(x.lookupFile))
		p.set(x.file, x.net, "TGListFile", hostsPath(x.tgList))
		p.set(x.file, "Log", "DisplayLevel", "0")
		p.set(x.file, "Log", "FileLevel", "0")
		p.set(x.file, "Log", "FilePath", gospotLogDir)
		p.set(x.file, "Log", "FileRoot", x.root)
		if x.file == "ysf2nxdn" {
			dsyDefault(c, p, x.file, "aprs.fi", "Enable", "0")
		} else {
			dsyDelSection(c, p, x.file, "aprs.fi")
		}
		dsyDefault(c, p, x.file, "YSF Network", "WiresXMakeUpper", "1")
		dsyDefault(c, p, x.file, "YSF Network", "DT1", dsyDT1)
		dsyDefault(c, p, x.file, "YSF Network", "DT2", dsyDT2)
	}

	// DGIdGateway (3027-3037). Empty source ports are not copied (fix).
	if c.has("dgidgateway") {
		if v := p.value("mmdvmhost", "System Fusion Network", "GatewayPort"); v != "" {
			p.set("dgidgateway", "General", "LocalPort", v)
		}
		if v := p.value("mmdvmhost", "System Fusion Network", "LocalPort"); v != "" {
			p.set("dgidgateway", "General", "RptPort", v)
		}
		p.set("dgidgateway", "Log", "DisplayLevel", "1")
		p.set("dgidgateway", "Log", "FileLevel", "1")
		p.set("dgidgateway", "Log", "FilePath", gospotLogDir)
		p.set("dgidgateway", "Log", "FileRoot", "DGIdGateway")
		p.set("dgidgateway", "Log", "FileRotate", "0")
		p.set("dgidgateway", "YSF Network", "Hosts", hostsPath("YSFHosts.txt"))
	}
}
