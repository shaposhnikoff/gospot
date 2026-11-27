package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Stock Pi-Star_Binaries_sbin/pistar-upnp.service lines (all commented: Private).
const stockUpnp = "#!/bin/bash\n" +
	"start)\n" +
	"#\t\t$DAEMON -u ${igdURL} -e ${hostVar}_SSH -a $ipVar 22 22 TCP > /dev/null 2>&1\n" +
	"#\t\t$DAEMON -u ${igdURL} -e ${hostVar}_Dash -a $ipVar 80 80 TCP > /dev/null 2>&1\n" +
	"\t\tif ! pgrep ircDDBGateway; then\n" +
	"#\t\t\t$DAEMON -u ${igdURL} -e ${hostVar}_Remote -a $ipVar 10022 10022 UDP > /dev/null 2>&1\n" +
	"stop)\n" +
	"\t\t$DAEMON -u ${igdURL} -d 10022 UDP > /dev/null 2>&1\n"

const stockCron = "SHELL=/bin/sh\n*/5 *\t* * *\troot\t/usr/local/sbin/pistar-upnp.service start > /dev/null 2>&1 &\n"

type sysFix struct {
	dir                      string
	upnp, cron, hostapd, off string
	wireless                 bool
}

// withSys points cfgSys at files in a temporary directory; empty contents
// mean "file missing".
func withSys(t *testing.T, upnp, cron string, hostapd, off, wireless bool) *sysFix {
	t.Helper()
	saved, savedWireless := cfgSys, cfgWirelessPresent
	t.Cleanup(func() { cfgSys, cfgWirelessPresent = saved, savedWireless })
	dir := t.TempDir()
	f := &sysFix{dir: dir, upnp: filepath.Join(dir, "pistar-upnp.service"), cron: filepath.Join(dir, "crontab"),
		hostapd: filepath.Join(dir, "hostapd.conf"), off: filepath.Join(dir, "hostap.off"), wireless: wireless}
	write := func(p, s string, ok bool) {
		if ok {
			if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	write(f.upnp, upnp, upnp != "")
	write(f.cron, cron, cron != "")
	write(f.hostapd, "ssid=pi-star\nwpa=2\nwpa_passphrase=raspberry\n", hostapd)
	write(f.off, "", off)
	cfgSys.upnpScript, cfgSys.crontab, cfgSys.hostapdConf, cfgSys.hostapOff = f.upnp, f.cron, f.hostapd, f.off
	cfgSys.linuxUser = "pi-star"
	cfgWirelessPresent = func() bool { return wireless }
	return f
}

func sysSection(t *testing.T, id string) cfgSectionImpl {
	t.Helper()
	for _, s := range cfgSections {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("section %s is not registered", id)
	return cfgSectionImpl{}
}

func sysCtx() *cfgContext {
	c := &cfgContext{}
	c.St.System.Hostname = "pi-star"
	return c
}

// sysApply runs one section's applier on a fresh plan.
func sysApply(t *testing.T, id string, form cfgForm) (map[string]any, error) {
	t.Helper()
	c := sysCtx()
	p := &cfgPlan{ctx: c}
	err := sysSection(t, id).Apply(c, form, p)
	if len(p.change.Changes) > 0 {
		t.Fatalf("%s must only plan system ops: %+v", id, p.change.Changes)
	}
	return p.change.System, err
}

func controls(sec *cfgSection) map[string]cfgControl {
	out := map[string]cfgControl{}
	for _, r := range sec.Rows {
		for _, c := range r.Cells {
			for _, ctl := range c.Controls {
				if ctl.Name != "" {
					out[ctl.Name] = ctl
				}
			}
		}
	}
	return out
}

func values(sec *cfgSection) map[string]string {
	out := map[string]string{}
	for n, c := range controls(sec) {
		out[n] = c.Value
	}
	return out
}

func TestCfgSystemSettings(t *testing.T) {
	saved := cfgSys
	t.Cleanup(func() { cfgSys = saved })
	s, err := readSettings(shippedSettings)
	if err != nil {
		t.Fatal(err)
	}
	if err := setCfgSystemPaths(s); err != nil {
		t.Fatal(err)
	}
	if cfgSys.upnpScript != s["PISTAR_UPNP_SCRIPT"] || cfgSys.crontab != s["CRONTAB_FILE"] || cfgSys.hostapOff != s["HOSTAP_OFF_FLAG"] ||
		cfgSys.hostapdConf != s["HOSTAPD_CONF"] || cfgSys.linuxUser != s["PISTAR_LINUX_USER"] || cfgSys.upnpScript == "" {
		t.Fatalf("settings not installed: %+v", cfgSys)
	}
	before := cfgSys
	s["CRONTAB_FILE"] = "etc/crontab"
	if err := setCfgSystemPaths(s); err == nil || !strings.Contains(err.Error(), "CRONTAB_FILE") || cfgSys != before {
		t.Fatalf("relative path accepted or paths changed: %v %+v", err, cfgSys)
	}
}

func TestFirewallBuildLayout(t *testing.T) {
	withSys(t, stockUpnp, stockCron, true, false, false)
	sec := sysSection(t, "firewall").Build(sysCtx())
	if sec == nil || sec.Title != "Firewall Configuration" || !sec.Apply || sec.Form != "" {
		t.Fatalf("section: %+v", sec)
	}
	head := sec.Rows[0].Cells
	if len(head) != 2 || !head[0].Header || head[0].Label != "Setting" || head[1].Colspan != 2 || head[1].Label != "Value" ||
		head[1].Tooltip.Body != "The current value from the\nconfiguration files" {
		t.Fatalf("header: %+v", head)
	}
	want := []struct{ id, label, title, body string }{
		{"dashAccess", "Dashboard Access:", "Dashboard Access", "Do you want the dashboard access to be publicly available? This modifies the uPNP firewall configuration."},
		{"ircRCAccess", "ircDDBGateway Remote:", "ircDDBGateway Remote", "Do you want the ircDDBGateway remote control access to be publicly available? This modifies the uPNP firewall Configuration."},
		{"sshAccess", "SSH Access:", "SSH Access", "Do you want access to be publicly available over SSH (used for support issues)? This modifies the uPNP firewall Configuration."},
		{"autoAP", "Auto AP:", "Auto AP", "Do you want your Pi to create its own AP if it cannot connect to WiFi within 120 secs of boot"},
		{"uPNP", "uPNP:", "uPNP", "Do you want your Pi to create its own Firewall rules for use with D-Star."},
	}
	if len(sec.Rows) != 1+len(want) {
		t.Fatalf("rows: %d", len(sec.Rows))
	}
	for i, w := range want {
		r := sec.Rows[i+1]
		l := r.Cells[0]
		if r.ID != w.id || l.Label != w.label || l.Tooltip.Title != w.title || l.Tooltip.Body != w.body {
			t.Errorf("row %d: %+v", i, r)
		}
		ctl := r.Cells[1].Controls[0]
		if ctl.Kind != "radio" || ctl.Name != w.id {
			t.Errorf("control %d: %+v", i, ctl)
		}
		opts := sysOptPrivate
		if w.id == "autoAP" || w.id == "uPNP" {
			opts = sysOptOnOff
		}
		if !reflect.DeepEqual(ctl.Options, opts) {
			t.Errorf("%s options %+v", w.id, ctl.Options)
		}
		if w.id == "autoAP" {
			if len(r.Cells) != 3 || r.Cells[1].Colspan != 0 || r.Cells[2].Controls[0].Text != "Note: Reboot Required if changed" {
				t.Errorf("auto AP cells: %+v", r.Cells)
			}
		} else if len(r.Cells) != 2 || r.Cells[1].Colspan != 2 {
			t.Errorf("%s cells: %+v", w.id, r.Cells)
		}
	}
	if got := values(sec); !reflect.DeepEqual(got, map[string]string{"dashAccess": "PRV", "ircRCAccess": "PRV", "sshAccess": "PRV", "autoAP": "ON", "uPNP": "ON"}) {
		t.Fatalf("stock values: %v", got)
	}
}

func TestFirewallReadsFileState(t *testing.T) {
	open := strings.NewReplacer("#\t\t$DAEMON -u ${igdURL} -e ${hostVar}_Dash", "\t\t$DAEMON -u ${igdURL} -e ${hostVar}_Dash",
		"#\t\t\t$DAEMON -u ${igdURL} -e ${hostVar}_Remote", "\t\t\t$DAEMON -u ${igdURL} -e ${hostVar}_Remote").Replace(stockUpnp)
	for name, tc := range map[string]struct {
		upnp, cron string
		hostapd    bool
		off        bool
		want       map[string]string
	}{
		"dash and remote public, AP off, uPNP off": {open, "#" + strings.Split(stockCron, "\n")[1] + "\n", true, true,
			map[string]string{"dashAccess": "PUB", "ircRCAccess": "PUB", "sshAccess": "PRV", "autoAP": "OFF", "uPNP": "OFF"}},
		// Missing files: the classic grep gives "" -> Public / On.
		"missing files": {"", "", false, false,
			map[string]string{"dashAccess": "PUB", "ircRCAccess": "PUB", "sshAccess": "PUB", "uPNP": "ON"}},
		// No cron line: On, as in the classic page.
		"no cron line": {stockUpnp, "SHELL=/bin/sh\n", false, false,
			map[string]string{"dashAccess": "PRV", "ircRCAccess": "PRV", "sshAccess": "PRV", "uPNP": "ON"}},
		// exec() returns the last matching line.
		"last line wins": {stockUpnp + "\t\t$DAEMON -a $ipVar 22 22 TCP\n", stockCron + "#x pistar-upnp.service\n", false, false,
			map[string]string{"dashAccess": "PRV", "ircRCAccess": "PRV", "sshAccess": "PUB", "uPNP": "OFF"}},
	} {
		t.Run(name, func(t *testing.T) {
			withSys(t, tc.upnp, tc.cron, tc.hostapd, tc.off, false)
			if got := values(sysSection(t, "firewall").Build(sysCtx())); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
	// Unset paths (settings not installed): files count as missing.
	saved := cfgSys
	cfgSys = struct {
		upnpScript, crontab, hostapOff, hostapdConf string
		linuxUser                                   string
	}{}
	defer func() { cfgSys = saved }()
	if got := values(buildFirewall(sysCtx())); got["dashAccess"] != "PUB" || got["uPNP"] != "ON" || len(got) != 4 {
		t.Fatalf("unset paths: %v", got)
	}
}

func TestFirewallApplyOps(t *testing.T) {
	withSys(t, stockUpnp, stockCron, true, false, false)
	untouched := cfgForm{"dashAccess": "PRV", "ircRCAccess": "PRV", "sshAccess": "PRV", "autoAP": "ON", "uPNP": "ON"}
	if ops, err := sysApply(t, "firewall", untouched); err != nil || ops != nil {
		t.Fatalf("untouched form planned %v %v", ops, err)
	}
	if ops, err := sysApply(t, "firewall", cfgForm{}); err != nil || ops != nil {
		t.Fatalf("absent controls planned %v %v", ops, err)
	}
	ops, err := sysApply(t, "firewall", cfgForm{"dashAccess": "PUB", "ircRCAccess": "PRV", "sshAccess": "PUB", "autoAP": "OFF", "uPNP": "OFF"})
	want := map[string]any{"upnp_ports": map[string]string{"80": "on", "22": "on"}, "autoap": "off", "upnp": "off"}
	if err != nil || !reflect.DeepEqual(ops, want) {
		t.Fatalf("got %#v %v", ops, err)
	}

	f := withSys(t, strings.ReplaceAll(stockUpnp, "#\t", "\t"), "#"+strings.Split(stockCron, "\n")[1]+"\n", true, true, false)
	ops, err = sysApply(t, "firewall", cfgForm{"dashAccess": "PRV", "ircRCAccess": "PRV", "sshAccess": "PUB", "autoAP": "ON", "uPNP": "ON"})
	want = map[string]any{"upnp_ports": map[string]string{"80": "off", "10022": "off"}, "autoap": "on", "upnp": "on"}
	if err != nil || !reflect.DeepEqual(ops, want) {
		t.Fatalf("got %#v %v", ops, err)
	}
	// Auto AP is not rendered without hostapd, so a posted value is ignored.
	os.Remove(f.hostapd)
	if ops, err := sysApply(t, "firewall", cfgForm{"autoAP": "ON"}); err != nil || ops != nil {
		t.Fatalf("hidden Auto AP planned %v %v", ops, err)
	}
	for _, form := range []cfgForm{{"dashAccess": "pub"}, {"sshAccess": ""}, {"ircRCAccess": "ON"}, {"uPNP": "on"}, {"uPNP": "PUB"}} {
		withSys(t, stockUpnp, stockCron, true, false, false)
		var ce cfgError
		if _, err := sysApply(t, "firewall", form); !errors.As(err, &ce) {
			t.Errorf("%v accepted: %v", form, err)
		}
	}
	withSys(t, stockUpnp, stockCron, true, false, false)
	var ce cfgError
	if _, err := sysApply(t, "firewall", cfgForm{"autoAP": "yes"}); !errors.As(err, &ce) {
		t.Errorf("invalid Auto AP accepted: %v", err)
	}
}

func TestWifiBuild(t *testing.T) {
	withSys(t, stockUpnp, stockCron, true, false, false)
	if sysSection(t, "wifi").Build(sysCtx()) != nil {
		t.Fatal("Wireless Configuration shown without a wlan interface")
	}
	withSys(t, stockUpnp, stockCron, false, false, true)
	sec := sysSection(t, "wifi").Build(sysCtx())
	if sec == nil || sec.Title != "Wireless Configuration" || sec.Form != "autoApPassForm" || sec.Apply {
		t.Fatalf("section: %+v", sec)
	}
	link := sec.Rows[0].Cells[0].Controls[0]
	if link.Kind != "link" || link.Attrs["href"] != "/manage/#wifi" || link.Text != "Wireless status and configuration" {
		t.Fatalf("link: %+v", link)
	}
	for _, c := range sec.Rows[0].Cells {
		for _, ctl := range c.Controls {
			if strings.Contains(ctl.Text+ctl.Attrs["href"], "wifi.php") {
				t.Fatal("the PHP wifi page must not be referenced")
			}
		}
	}
	head := sec.Rows[1].Cells
	if head[0].Label != "Auto AP SSID" || head[1].Label != "PSK" || head[1].Colspan != 3 {
		t.Fatalf("header: %+v", head)
	}
	row := sec.Rows[2].Cells
	if row[0].Label != "pi-star" || row[2].Align != "right" || row[2].Controls[0].Kind != "button" || row[2].Controls[0].Text != "Set PSK" {
		t.Fatalf("row: %+v", row)
	}
	inputs := row[1].Controls
	if len(inputs) != 4 || inputs[0].Text != "PSK:" || inputs[1].Kind != "password" || inputs[1].Name != "autoapPsk" || inputs[1].Attrs["size"] != "20" ||
		inputs[2].Text != "Confirm PSK:" || inputs[3].Kind != "password" || inputs[3].Name != "autoapPskConfirm" || inputs[1].Value != "" {
		t.Fatalf("inputs: %+v", inputs)
	}
}

func TestWifiApplyPSK(t *testing.T) {
	withSys(t, "", "", true, false, true)
	for _, psk := range []string{"12345678", "with spaces & $hell `chars`", " leading", strings.Repeat("x", 63)} {
		ops, err := sysApply(t, "wifi", cfgForm{"autoapPsk": psk, "autoapPskConfirm": psk})
		if err != nil || !reflect.DeepEqual(ops, map[string]any{"autoap_psk": psk}) {
			t.Errorf("%q: %v %v", psk, ops, err)
		}
	}
	for _, form := range []cfgForm{{}, {"autoapPsk": "", "autoapPskConfirm": ""}} {
		if ops, err := sysApply(t, "wifi", form); err != nil || ops != nil {
			t.Errorf("untouched %v planned %v %v", form, ops, err)
		}
	}
	for name, form := range map[string]cfgForm{
		"mismatch":      {"autoapPsk": "12345678", "autoapPskConfirm": "12345679"},
		"confirm only":  {"autoapPskConfirm": "12345678"},
		"short":         {"autoapPsk": "1234567", "autoapPskConfirm": "1234567"},
		"long":          {"autoapPsk": strings.Repeat("a", 64), "autoapPskConfirm": strings.Repeat("a", 64)},
		"non-ASCII":     {"autoapPsk": "pässword1", "autoapPskConfirm": "pässword1"},
		"control chars": {"autoapPsk": "pass\tword", "autoapPskConfirm": "pass\tword"},
	} {
		var ce cfgError
		if ops, err := sysApply(t, "wifi", form); !errors.As(err, &ce) || ops != nil {
			t.Errorf("%s accepted: %v %v", name, ops, err)
		}
	}
	withSys(t, "", "", false, false, true)
	var ce cfgError
	if _, err := sysApply(t, "wifi", cfgForm{"autoapPsk": "12345678", "autoapPskConfirm": "12345678"}); !errors.As(err, &ce) {
		t.Errorf("PSK accepted without hostapd.conf: %v", err)
	}
}

func TestAdminPassBuild(t *testing.T) {
	withSys(t, "", "", false, false, false)
	sec := sysSection(t, "adminpass").Build(sysCtx())
	if sec == nil || sec.Title != "Remote Access Password" || sec.Form != "adminPassForm" || sec.Apply {
		t.Fatalf("section: %+v", sec)
	}
	if h := sec.Rows[0].Cells; h[0].Label != "User Name" || h[1].Label != "Password" || h[1].Colspan != 3 {
		t.Fatalf("header: %+v", h)
	}
	row := sec.Rows[1].Cells
	in := row[1].Controls
	if row[0].Label != "pi-star" || in[0].Text != "Password:" || in[1].Name != "adminPassword" || in[1].Kind != "password" ||
		in[2].Text != "Confirm Password:" || in[3].Name != "adminPasswordConfirm" || in[3].Kind != "password" ||
		row[2].Controls[0].Kind != "button" || row[2].Controls[0].Text != "Set Password" {
		t.Fatalf("row: %+v", row)
	}
	note := sec.Rows[2].Cells[0]
	if note.Label != "WARNING:" || note.Colspan != 3 || note.Controls[0].Text != "This changes the password for this admin page\nAND the \"pi-star\" SSH account" {
		t.Fatalf("note: %+v", note)
	}
}

func TestAdminPassApply(t *testing.T) {
	withSys(t, "", "", false, false, false)
	for _, pw := range []string{"x", "  spaced \\ pass  ", strings.Repeat("ж", 128)} {
		ops, err := sysApply(t, "adminpass", cfgForm{"adminPassword": pw, "adminPasswordConfirm": pw})
		if err != nil || !reflect.DeepEqual(ops, map[string]any{"password": pw}) {
			t.Errorf("%q: %v %v", pw, ops, err)
		}
	}
	if ops, err := sysApply(t, "adminpass", cfgForm{"adminPassword": "", "adminPasswordConfirm": ""}); err != nil || ops != nil {
		t.Errorf("untouched form planned %v %v", ops, err)
	}
	for name, pw := range map[string][2]string{
		"mismatch":        {"secret1", "secret2"},
		"first only":      {"secret1", ""},
		"colon":           {"a:b", "a:b"},
		"newline":         {"a\nb", "a\nb"},
		"carriage return": {"a\rb", "a\rb"},
		"NUL":             {"a\x00b", "a\x00b"},
		"too long":        {strings.Repeat("a", 129), strings.Repeat("a", 129)},
		"factory default": {"raspberry", "raspberry"},
	} {
		var ce cfgError
		if ops, err := sysApply(t, "adminpass", cfgForm{"adminPassword": pw[0], "adminPasswordConfirm": pw[1]}); !errors.As(err, &ce) || ops != nil {
			t.Errorf("%s accepted: %v %v", name, ops, err)
		}
	}
}

// End to end through serveConfigure: page order and visibility, the helper
// request of each form, 422 for refused input and no helper call for an
// untouched form.
func TestCfgSystemServe(t *testing.T) {
	withSys(t, stockUpnp, stockCron, true, false, true)
	var applied []configChange
	run := func(_ context.Context, verb string, input []byte) ([]byte, error) {
		if verb == "read" {
			return []byte(`{"revision":"r1","files":{},"layout":{},"system":{"hostname":"pi-star","markers":{}}}`), nil
		}
		var c configChange
		if err := json.Unmarshal(input, &c); err != nil {
			t.Fatal(err)
		}
		applied = append(applied, c)
		return []byte(`{"ok":true}`), nil
	}
	w := httptest.NewRecorder()
	serveConfigure(w, maintenanceRequest("GET", "/manage/api/configure", "", false), run)
	var page cfgPage
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var ids []string
	for _, s := range page.Sections {
		if s.ID == "firewall" || s.ID == "wifi" || s.ID == "adminpass" {
			ids = append(ids, s.ID)
		}
	}
	if !reflect.DeepEqual(ids, []string{"firewall", "wifi", "adminpass"}) {
		t.Fatalf("sections: %v", ids)
	}
	post := func(form string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		serveConfigure(w, maintenanceRequest("POST", "/manage/api/configure", `{"revision":"r1","form":`+form+`}`, true), run)
		return w
	}
	if w := post(`{"dashAccess":"PRV","ircRCAccess":"PRV","sshAccess":"PRV","autoAP":"ON","uPNP":"ON"}`); w.Code != 200 || len(applied) != 0 {
		t.Fatalf("untouched firewall: %d %s %v", w.Code, w.Body.String(), applied)
	}
	if w := post(`{"adminPassword":"one","adminPasswordConfirm":"two"}`); w.Code != 422 || !strings.Contains(w.Body.String(), "do not match") || len(applied) != 0 {
		t.Fatalf("mismatch: %d %s", w.Code, w.Body.String())
	}
	if w := post(`{"autoapPsk":"short","autoapPskConfirm":"short"}`); w.Code != 422 || len(applied) != 0 {
		t.Fatalf("short PSK: %d %s", w.Code, w.Body.String())
	}
	if w := post(`{"adminPassword":"n3w-secret","adminPasswordConfirm":"n3w-secret"}`); w.Code != 200 || len(applied) != 1 {
		t.Fatalf("password: %d %s", w.Code, w.Body.String())
	}
	if got := applied[0]; got.Revision != "r1" || got.Changes != nil || !reflect.DeepEqual(got.System, map[string]any{"password": "n3w-secret"}) {
		t.Fatalf("password request: %+v", got)
	}
	if w := post(`{"dashAccess":"PUB","ircRCAccess":"PRV","sshAccess":"PRV","autoAP":"OFF","uPNP":"ON"}`); w.Code != 200 || len(applied) != 2 {
		t.Fatalf("firewall: %d %s", w.Code, w.Body.String())
	}
	want := map[string]any{"upnp_ports": map[string]any{"80": "on"}, "autoap": "off"}
	if got := applied[1]; !reflect.DeepEqual(got.System, want) {
		t.Fatalf("firewall request: %#v", got.System)
	}
}
