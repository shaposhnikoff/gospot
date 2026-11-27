package main

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"strings"
	"unicode/utf8"
)

// Configuration page, spec part 05 (docs/configure-spec/05-firewall-wifi-password.md):
// "Firewall Configuration" (configure.php l.5581-5664), "Wireless Configuration"
// with the Auto AP PSK form (l.5667-5694) and "Remote Access Password"
// (l.5696-5711). Every change goes through the gospot-configure system ops
// upnp_ports, autoap, upnp, autoap_psk and password.

// cfgSys holds the gospot.conf paths this file reads. applySettings installs
// them through setCfgSystemPaths; while unset, the files count as missing
// (firewall rows show the classic missing-file defaults, Auto AP is hidden).
var cfgSys struct {
	upnpScript, crontab, hostapOff, hostapdConf string
	linuxUser                                   string
}

// setCfgSystemPaths validates and installs the settings used by this file.
func setCfgSystemPaths(s settings) error {
	var errs []string
	path := func(key string) string {
		v, err := s.abs(key)
		if err != nil {
			errs = append(errs, err.Error())
		}
		return v
	}
	upnp, cron, off, hostapd := path("GOSPOT_UPNP_SCRIPT"), path("CRONTAB_FILE"), path("HOSTAP_OFF_FLAG"), path("HOSTAPD_CONF")
	if len(errs) > 0 {
		return fmt.Errorf("invalid settings: %s", strings.Join(errs, "; "))
	}
	cfgSys.upnpScript, cfgSys.crontab, cfgSys.hostapOff, cfgSys.hostapdConf = upnp, cron, off, hostapd
	cfgSys.linuxUser = s["GOSPOT_LINUX_USER"]
	return nil
}

// cfgWirelessPresent mirrors file_exists('/sys/class/net/wlan0' | 'wlan1' |
// 'wlan0_ap') (l.5668) through the kernel's interface list.
var cfgWirelessPresent = func() bool {
	for _, name := range []string{"wlan0", "wlan1", "wlan0_ap"} {
		if _, err := net.InterfaceByName(name); err == nil {
			return true
		}
	}
	return false
}

// sysFirstCharHash mirrors exec('grep "PATTERN" FILE | cut -c 1') === '#': the
// first character of the last line containing pattern. A missing or
// unreadable file, or no matching line, gives false (classic: Public / On).
func sysFirstCharHash(file, pattern string) bool {
	if file == "" {
		return false
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return false
	}
	last, found := "", false
	for _, line := range strings.Split(string(b), "\n") {
		if strings.Contains(line, pattern) {
			last, found = line, true
		}
	}
	return found && strings.HasPrefix(last, "#")
}

func sysFileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

// hostapdInstalled decides whether the Auto AP row is shown. Deviation: the
// classic page tests /etc/default/hostapd (l.5637), which is not a gospot.conf
// setting; HOSTAPD_CONF is the file pistar-ap.service actually needs. A
// permission error still means the file is there.
func hostapdInstalled() bool {
	if cfgSys.hostapdConf == "" {
		return false
	}
	_, err := os.Stat(cfgSys.hostapdConf)
	return !errors.Is(err, fs.ErrNotExist)
}

// fwPort is one Private/Public UPnP row: the classic grep pattern "N N".
type fwPort struct {
	name, port, label, body string
}

var fwPorts = []fwPort{
	{"dashAccess", "80", "Dashboard Access", "Do you want the dashboard access to be publicly available? This modifies the uPNP firewall configuration."},
	{"ircRCAccess", "10022", "ircDDBGateway Remote", "Do you want the ircDDBGateway remote control access to be publicly available? This modifies the uPNP firewall Configuration."},
	{"sshAccess", "22", "SSH Access", "Do you want access to be publicly available over SSH (used for support issues)? This modifies the uPNP firewall Configuration."},
}

// fwState is the firewall block as the classic page reads it (l.5599-5654).
type fwState struct {
	public map[string]bool // port -> Public
	autoAP bool            // Auto AP visible
	apOn   bool            // HOSTAP_OFF_FLAG absent
	upnpOn bool            // gospot-upnp.service cron line not commented
}

func readFwState() fwState {
	st := fwState{public: map[string]bool{}}
	for _, p := range fwPorts {
		st.public[p.port] = !sysFirstCharHash(cfgSys.upnpScript, p.port+" "+p.port)
	}
	st.autoAP = hostapdInstalled()
	st.apOn = !sysFileExists(cfgSys.hostapOff)
	st.upnpOn = !sysFirstCharHash(cfgSys.crontab, "gospot-upnp.service")
	return st
}

func sysRadio(name, value string, options ...linkOption) cfgControl {
	return cfgControl{Kind: "radio", Name: name, Value: value, Options: options}
}

func sysChoose(b bool, yes, no string) string {
	if b {
		return yes
	}
	return no
}

func fwLabel(label, body string) cfgCell {
	return cfgCell{Align: "left", Label: label + ":", Tooltip: tip(label, body)}
}

var (
	sysOptPrivate = []linkOption{{Value: "PRV", Label: "Private"}, {Value: "PUB", Label: "Public"}}
	sysOptOnOff   = []linkOption{{Value: "ON", Label: "On"}, {Value: "OFF", Label: "Off"}}
)

func buildFirewall(c *cfgContext) *cfgSection {
	st := readFwState()
	rows := []cfgRow{{Cells: []cfgCell{
		{Header: true, Label: "Setting", Tooltip: tip("Setting", ""), Style: "width: 200px;"},
		{Header: true, Colspan: 2, Label: "Value", Tooltip: tip("Value", "The current value from the\nconfiguration files")},
	}}}
	for _, p := range fwPorts {
		ctl := sysRadio(p.name, sysChoose(st.public[p.port], "PUB", "PRV"), sysOptPrivate...)
		rows = append(rows, cfgRow{ID: p.name, Cells: []cfgCell{fwLabel(p.label, p.body), {Align: "left", Colspan: 2, Controls: []cfgControl{ctl}}}})
	}
	if st.autoAP {
		rows = append(rows, cfgRow{ID: "autoAP", Cells: []cfgCell{
			fwLabel("Auto AP", "Do you want your Pi to create its own AP if it cannot connect to WiFi within 120 secs of boot"),
			td(sysRadio("autoAP", sysChoose(st.apOn, "ON", "OFF"), sysOptOnOff...)),
			td(static("Note: Reboot Required if changed")),
		}})
	}
	rows = append(rows, cfgRow{ID: "uPNP", Cells: []cfgCell{
		fwLabel("uPNP", "Do you want your Pi to create its own Firewall rules for use with D-Star."),
		{Align: "left", Colspan: 2, Controls: []cfgControl{sysRadio("uPNP", sysChoose(st.upnpOn, "ON", "OFF"), sysOptOnOff...)}},
	}})
	return &cfgSection{ID: "firewall", Title: "Firewall Configuration", Rows: rows, Apply: true}
}

// applyFirewall emits an op only for a value that differs from the files
// (spec §1.3: equivalent to the classic re-apply, except that the classic
// uPNP=ON also restarts gospot-upnp.service on every Apply). Unknown values
// are refused; the classic page silently ignored them.
func applyFirewall(c *cfgContext, form cfgForm, p *cfgPlan) error {
	st := readFwState()
	ports := map[string]string{}
	for _, fp := range fwPorts {
		if !form.has(fp.name) {
			continue
		}
		v := form[fp.name]
		if v != "PRV" && v != "PUB" {
			return cfgError("Invalid value for " + fp.label)
		}
		if (v == "PUB") != st.public[fp.port] {
			ports[fp.port] = sysChoose(v == "PUB", "on", "off")
		}
	}
	onOff := func(name, label string) (string, bool, error) {
		if !form.has(name) {
			return "", false, nil
		}
		v := form[name]
		if v != "ON" && v != "OFF" {
			return "", false, cfgError("Invalid value for " + label)
		}
		return v, true, nil
	}
	if st.autoAP {
		v, ok, err := onOff("autoAP", "Auto AP")
		if err != nil {
			return err
		}
		if ok && (v == "ON") != st.apOn {
			p.system("autoap", strings.ToLower(v))
		}
	}
	if len(ports) > 0 {
		p.system("upnp_ports", ports)
	}
	v, ok, err := onOff("uPNP", "uPNP")
	if err != nil {
		return err
	}
	if ok && (v == "ON") != st.upnpOn {
		p.system("upnp", strings.ToLower(v))
	}
	return nil
}

// sysPair returns the value of a "type it twice" pair; empty when neither
// field was posted or both are empty (an untouched form changes nothing).
// The classic forms post both inputs under one name and the server keeps the
// second; here they have separate names and must match.
func sysPair(form cfgForm, first, second, what string) (string, error) {
	a, b := form[first], form[second]
	if a == "" && b == "" {
		return "", nil
	}
	if a != b {
		return "", cfgError("The " + what + " entries do not match")
	}
	return a, nil
}

// Wireless Configuration. Deviation: the classic block iframes wifi.php; the
// native page links to the native Wi-Fi page instead (no PHP, no iframes).
// The link is a static control with Attrs["href"]; the heading and the PSK
// form follow the classic layout in one table.
func buildWifi(c *cfgContext) *cfgSection {
	if !cfgWirelessPresent() {
		return nil
	}
	host := c.St.System.Hostname
	if host == "" {
		host, _ = os.Hostname() // php_uname('n')
	}
	return &cfgSection{ID: "wifi", Title: "Wireless Configuration", Form: "autoApPassForm", Rows: []cfgRow{
		{ID: "wifiLink", Cells: []cfgCell{{Align: "left", Colspan: 4, Controls: []cfgControl{
			{Kind: "link", Text: "Wireless status and configuration", Attrs: map[string]string{"href": "/manage/#wifi"}},
		}}}},
		{Cells: []cfgCell{
			{Header: true, Label: "Auto AP SSID", Style: "width: 200px;"},
			{Header: true, Colspan: 3, Label: "PSK"},
		}},
		{ID: "autoApPsk", Cells: []cfgCell{
			{Align: "left", Style: "font-weight: bold;", Label: host},
			td(static("PSK:"), cfgControl{Kind: "password", Name: "autoapPsk", Attrs: map[string]string{"size": "20", "label": "PSK"}},
				static("Confirm PSK:"), cfgControl{Kind: "password", Name: "autoapPskConfirm", Attrs: map[string]string{"label": "Confirm PSK"}}),
			{Align: "right", Controls: []cfgControl{{Kind: "button", Text: "Set PSK", Attrs: map[string]string{"action": "autoApPassForm"}}}},
		}},
	}}
}

// applyWifi validates like the helper: 8-63 printable ASCII, not trimmed.
// Deviations: the classic regex also accepts 64 hex digits, and failures were
// only logged; here they are shown. A missing HOSTAPD_CONF is refused because
// the helper would otherwise report success without writing anything.
func applyWifi(c *cfgContext, form cfgForm, p *cfgPlan) error {
	psk, err := sysPair(form, "autoapPsk", "autoapPskConfirm", "PSK")
	if err != nil || psk == "" {
		return err
	}
	if len(psk) < 8 || len(psk) > 63 {
		return cfgError("The PSK must be 8 to 63 characters long")
	}
	for i := 0; i < len(psk); i++ {
		if psk[i] < 0x20 || psk[i] > 0x7e {
			return cfgError("The PSK may contain only printable ASCII characters")
		}
	}
	if !hostapdInstalled() {
		return cfgError("Auto AP is not installed (no hostapd configuration)")
	}
	p.system("autoap_psk", psk)
	return nil
}

// Remote Access Password. The admin UI uses Basic auth with the fixed user
// wizardUser (no nginx/.htpasswd); the helper rewrites the dashboard
// credential, sets GOSPOT_LINUX_USER through chpasswd and restarts the HTTPS
// service 2 s later.
func buildAdminPass(c *cfgContext) *cfgSection {
	user := cfgSys.linuxUser
	if user == "" {
		user = wizardUser
	}
	return &cfgSection{ID: "adminpass", Title: "Remote Access Password", Form: "adminPassForm", Rows: []cfgRow{
		{Cells: []cfgCell{
			{Header: true, Label: "User Name", Style: "width: 200px;"},
			{Header: true, Colspan: 3, Label: "Password"},
		}},
		{ID: "adminPassword", Cells: []cfgCell{
			{Align: "left", Style: "font-weight: bold;", Label: wizardUser},
			td(static("Password:"), cfgControl{Kind: "password", Name: "adminPassword", Attrs: map[string]string{"size": "20", "label": "Password"}},
				static("Confirm Password:"), cfgControl{Kind: "password", Name: "adminPasswordConfirm", Attrs: map[string]string{"label": "Confirm Password"}}),
			{Align: "right", Controls: []cfgControl{{Kind: "button", Text: "Set Password", Attrs: map[string]string{"action": "adminPassForm"}}}},
		}},
		{Cells: []cfgCell{{Align: "left", Colspan: 3, Label: "WARNING:", Controls: []cfgControl{
			static("This changes the password for this admin page\nAND the \"" + user + "\" SSH account"),
		}}}},
	}}
}

// applyAdminPass follows the helper's limits (1-128 characters, no ':', CR,
// LF or NUL) and the classic refusal of the factory default "raspberry".
// Deviation: the classic trim()/stripslashes() normalisation is not copied;
// the password is set exactly as typed.
func applyAdminPass(c *cfgContext, form cfgForm, p *cfgPlan) error {
	pw, err := sysPair(form, "adminPassword", "adminPasswordConfirm", "password")
	if err != nil || pw == "" {
		return err
	}
	switch {
	case !utf8.ValidString(pw) || utf8.RuneCountInString(pw) > 128:
		return cfgError("The password must be at most 128 characters long")
	case strings.ContainsAny(pw, ":\r\n\x00"):
		return cfgError("The password must not contain ':' or line breaks")
	case pw == "raspberry":
		return cfgError("The password must not be the factory default")
	}
	p.system("password", pw)
	return nil
}

func init() {
	registerCfgSection(cfgSectionImpl{ID: "firewall", Order: 130, Build: buildFirewall, Apply: applyFirewall})
	registerCfgSection(cfgSectionImpl{ID: "wifi", Order: 140, Build: buildWifi, Apply: applyWifi})
	registerCfgSection(cfgSectionImpl{ID: "adminpass", Order: 150, Build: buildAdminPass, Apply: applyAdminPass})
}
