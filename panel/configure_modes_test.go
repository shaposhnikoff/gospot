package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Tests of the P25, NXDN, M17, POCSAG and Mobile GPS sections
// (docs/configure-spec/04-p25-nxdn-m17-pocsag-gps.md).

const modesDel = "<deleted>"

var modesSections = []cfgSectionImpl{modesP25Section, modesNXDNSection, modesM17Section, modesPOCSAGSection, modesGPSSection}

// modesFixture installs host lists and serial devices and restores the paths afterwards.
func modesFixture(t *testing.T) {
	t.Helper()
	hosts, dev := t.TempDir(), t.TempDir()
	write := func(name, text string) {
		if err := os.WriteFile(filepath.Join(hosts, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("P25Hosts.txt", "# TG\tAddress\tPort\n10\tparrot.example\t41000\n10200\tp25.example\t41000\n\n10201\tp25b.example\t41000\n")
	write("P25HostsLocal.txt", "31337\tlocal.example\t41000\n")
	write("NXDNHosts.txt", "65000 nxdn.example 41400\n10 parrot.example 41400\n#99 hidden 1\n")
	write("M17Hosts.txt", "M17-M17 192.0.2.1 17000\nM17-XOR 192.0.2.2 17000\n")
	for _, n := range []string{"ttyACM0", "ttyUSB0", "ttyAMA0", "tty1", "null"} {
		if err := os.WriteFile(filepath.Join(dev, n), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	oldHosts, oldLog, oldDev, oldSections := gospotHostsDir, gospotLogDir, modesDevDir, cfgSections
	gospotHostsDir, gospotLogDir, modesDevDir = hosts, "/var/log/gospot", dev
	cfgSections = append([]cfgSectionImpl{}, modesSections...)
	t.Cleanup(func() {
		gospotHostsDir, gospotLogDir, modesDevDir, cfgSections = oldHosts, oldLog, oldDev, oldSections
	})
}

// modesState parses INI texts as gospot-configure read does (first key wins).
func modesState(files map[string]string, marker bool) configState {
	var s configState
	s.Files = map[string]map[string]map[string]string{}
	s.Layout = map[string][]struct {
		Name string
		Keys [][2]string
	}{}
	s.System.Markers = map[string]*string{"mmdvmhost": nil, "dstarrepeater": nil}
	if marker {
		empty := ""
		s.System.Markers["mmdvmhost"] = &empty
	}
	for name, text := range files {
		data := map[string]map[string]string{}
		section := ""
		index := map[string]int{}
		layout := s.Layout[name]
		add := func(sec string) {
			if _, ok := index[sec]; !ok {
				index[sec] = len(layout)
				layout = append(layout, struct {
					Name string
					Keys [][2]string
				}{Name: sec})
			}
			if data[sec] == nil {
				data[sec] = map[string]string{}
			}
		}
		for _, line := range strings.Split(text, "\n") {
			l := strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(l, "[") && strings.HasSuffix(l, "]"):
				section = l[1 : len(l)-1]
				add(section)
			case l != "" && !strings.ContainsAny(l[:1], "#;") && strings.Contains(l, "="):
				k, v, _ := strings.Cut(l, "=")
				k, v = strings.TrimSpace(k), strings.TrimSpace(v)
				add(section)
				if _, dup := data[section][k]; !dup {
					data[section][k] = v
					layout[index[section]].Keys = append(layout[index[section]].Keys, [2]string{k, v})
				}
			}
		}
		s.Files[name] = data
		s.Layout[name] = layout
	}
	return s
}

// modesBase has every default the sections add, so only field writes show up.
func modesBase() map[string]string {
	return map[string]string{
		"mmdvmhost": `[General]
Callsign=M1ABC
[P25]
Enable=1
NAC=293
[P25 Network]
Enable=1
[NXDN]
Enable=1
RAN=1
[NXDN Network]
Enable=1
LocalAddress=127.0.0.1
LocalPort=14021
GatewayAddress=127.0.0.1
GatewayPort=14020
[M17]
Enable=1
CAN=0
[M17 Network]
Enable=1
[POCSAG]
Enable=1
Frequency=439987500
[POCSAG Network]
Enable=1
LocalAddress=127.0.0.1
LocalPort=3800
GatewayAddress=127.0.0.1
GatewayPort=4800
ModeHang=5
Debug=0
[Mobile GPS]
Enable=0
Address=127.0.0.1
Port=7834
`,
		"p25gateway": `[General]
Callsign=M1ABC
[Log]
DisplayLevel=1
FileLevel=1
[Network]
Static=10200
P252DMRAddress=127.0.0.1
P252DMRPort=42012
[Remote Commands]
Enable=1
Port=6074
`,
		"ysf2p25": "[P25 Network]\nStartupDstId=10200\n",
		"nxdngateway": `[General]
Callsign=M1ABC
RptProtocol=Icom
[Info]
Power=1
Height=0
[Log]
DisplayLevel=1
FileLevel=1
[Network]
Static=65000
HostsFile1=/usr/local/etc/NXDNHosts.txt
HostsFile2=/usr/local/etc/NXDNHostsLocal.txt
[APRS]
Enable=1
Address=127.0.0.1
Port=8673
Suffix=N
Description=Pi-Star_RPT
[GPSD]
Enable=0
Address=127.0.0.1
Port=2947
[Remote Commands]
Enable=1
Port=6075
`,
		"ysfgateway": "[General]\nSuffix=RPT\n[Info]\nName=Pi-Star\n[GPSD]\nEnable=0\n",
		"ysf2nxdn":   "[NXDN Network]\nStartupDstId=65000\n",
		"dmr2nxdn":   "[Enabled]\nEnabled=0\n",
		"m17gateway": "[General]\nCallsign=M1ABC\nDaemon=1\n[Network]\nStartup=M17-M17_C\n",
		"dapnetgateway": `[General]
Callsign=M1ABC
RptAddress=127.0.0.1
RptPort=3800
LocalAddress=127.0.0.1
LocalPort=4800
Daemon=0
WhiteList=1234,5678
[Log]
DisplayLevel=0
FileLevel=2
FilePath=/var/log/gospot
FileRoot=DAPNETGateway
[DAPNET]
Address=dapnet.afu.rwth-aachen.de
Port=43434
AuthKey=TOPSECRET
SuppressTimeWhenBusy=1
Debug=0
`,
		"mobilegps": `[General]
Callsign=M1ABC
Daemon=0
Debug=0
[Log]
DisplayLevel=0
FileLevel=1
FilePath=/var/log/gospot
[GPS]
Port=/dev/ttyACM0
Speed=4800
Debug=0
[Network]
Address=127.0.0.1
Port=7834
Debug=0
[Enabled]
Enabled=0
`,
	}
}

// modesEdit replaces "key=old" with "key=new" (or removes the line when new is
// modesDel) in a fixture file, failing when the line is missing.
func modesEdit(t *testing.T, files map[string]string, file, old, repl string) {
	t.Helper()
	if !strings.Contains(files[file], old+"\n") {
		t.Fatalf("%s has no line %q", file, old)
	}
	if repl == modesDel {
		repl = ""
	} else {
		repl += "\n"
	}
	files[file] = strings.Replace(files[file], old+"\n", repl, 1)
}

func modesBuild(c *cfgContext) map[string]cfgSection {
	out := map[string]cfgSection{}
	for _, s := range buildCfgPage(c) {
		out[s.ID] = s
	}
	return out
}

// modesForm is what the page posts for an untouched form.
func modesForm(c *cfgContext) cfgForm {
	f := cfgForm{}
	for _, s := range buildCfgPage(c) {
		for _, r := range s.Rows {
			for _, cell := range r.Cells {
				for _, ctl := range cell.Controls {
					if ctl.Name != "" {
						f[ctl.Name] = ctl.Value
					}
				}
			}
		}
	}
	return f
}

// modesPlan runs the framework planner and flattens the changes.
func modesPlan(t *testing.T, st configState, edits cfgForm) (map[string]string, error) {
	t.Helper()
	c := &cfgContext{St: st}
	form := modesForm(c)
	for k, v := range edits {
		if v == modesDel {
			delete(form, k)
		} else {
			form[k] = v
		}
	}
	ch, err := planCfgRaw(c, "rev", form)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for file, secs := range ch.Changes {
		for sec, keys := range secs {
			for k, v := range keys {
				if v == nil {
					out[file+"/"+sec+"/"+k] = modesDel
				} else {
					out[file+"/"+sec+"/"+k] = *v
				}
			}
		}
	}
	if len(out) > 0 && !reflect.DeepEqual(ch.Restart, cfgRadioUnits) {
		t.Errorf("restart = %v, want every radio unit", ch.Restart)
	}
	return out, nil
}

func modesFind(s cfgSection, name string) (cfgRow, cfgControl, bool) {
	for _, r := range s.Rows {
		for _, cell := range r.Cells {
			for _, ctl := range cell.Controls {
				if ctl.Name == name {
					return r, ctl, true
				}
			}
		}
	}
	return cfgRow{}, cfgControl{}, false
}

func modesValues(opts []linkOption) []string {
	var out []string
	for _, o := range opts {
		out = append(out, o.Value+"="+o.Label)
	}
	return out
}

func TestConfigureModesVisibility(t *testing.T) {
	modesFixture(t)
	cases := []struct {
		name   string
		marker bool
		edit   [][3]string // file, old line, new line
		drop   []string
		want   []string
	}{
		{name: "all enabled", marker: true, want: []string{"p25", "nxdn", "m17", "pocsag", "mobilegps"}},
		{name: "D-Star Repeater controller keeps POCSAG only", marker: false, want: []string{"pocsag"}},
		{name: "P25 network off", marker: true, edit: [][3]string{{"mmdvmhost", "[P25 Network]\nEnable=1", "[P25 Network]\nEnable=0"}},
			want: []string{"nxdn", "m17", "pocsag", "mobilegps"}},
		{name: "P25 RF enable does not matter", marker: true, edit: [][3]string{{"mmdvmhost", "[P25]\nEnable=1", "[P25]\nEnable=0"}},
			want: []string{"p25", "nxdn", "m17", "pocsag", "mobilegps"}},
		{name: "NXDN network off", marker: true, edit: [][3]string{{"mmdvmhost", "[NXDN Network]\nEnable=1", "[NXDN Network]\nEnable=0"}},
			want: []string{"p25", "m17", "pocsag", "mobilegps"}},
		{name: "NXDN via DMR2NXDN", marker: true, edit: [][3]string{
			{"mmdvmhost", "[NXDN Network]\nEnable=1", "[NXDN Network]\nEnable=0"}, {"dmr2nxdn", "Enabled=0", "Enabled=1"}},
			want: []string{"p25", "nxdn", "m17", "pocsag", "mobilegps"}},
		{name: "M17 network off", marker: true, edit: [][3]string{{"mmdvmhost", "[M17 Network]\nEnable=1", "[M17 Network]\nEnable=0"}},
			want: []string{"p25", "nxdn", "pocsag", "mobilegps"}},
		{name: "POCSAG off", marker: true, edit: [][3]string{{"mmdvmhost", "[POCSAG]\nEnable=1", "[POCSAG]\nEnable=0"}},
			want: []string{"p25", "nxdn", "m17", "mobilegps"}},
		{name: "no mobilegps file", marker: true, drop: []string{"mobilegps"}, want: []string{"p25", "nxdn", "m17", "pocsag"}},
		{name: "gateway files missing keep sections", marker: true, drop: []string{"p25gateway", "nxdngateway", "m17gateway", "dapnetgateway"},
			want: []string{"p25", "nxdn", "m17", "pocsag", "mobilegps"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := modesBase()
			for _, e := range tc.edit {
				modesEdit(t, files, e[0], e[1], e[2])
			}
			for _, d := range tc.drop {
				delete(files, d)
			}
			var got []string
			for _, s := range buildCfgPage(&cfgContext{St: modesState(files, tc.marker)}) {
				got = append(got, s.ID)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("sections = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestConfigureModesBuildLayout(t *testing.T) {
	modesFixture(t)
	secs := modesBuild(&cfgContext{St: modesState(modesBase(), true)})
	type row struct{ label, title, body string }
	want := map[string]struct {
		title string
		rows  []row
	}{
		"p25": {"P25 Configuration", []row{
			{"P25 Startup Host:", "P25 Host", "Set your prefered P25 Host here"},
			{"P25 NAC:", "P25 NAC", "Set your NAC code here"}}},
		"nxdn": {"NXDN Configuration", []row{
			{"NXDN Startup Host:", "NXDN Host", "Set your prefered NXDN Host here"},
			{"NXDN RAN:", "NXDN RAN", "Set your RAN code here, sane values are 1-64"}}},
		"m17": {"M17 Configuration", []row{
			{"M17 Startup Host:", "M17 Host", "Set your prefered M17 Host here"},
			{"M17 CAN:", "M17 CAN", "Set your CAN code here, sane values are 0-64"}}},
		"pocsag": {"POCSAG Configuration", []row{
			{"POCSAG Server:", "POCSAG Server", "Set the POCSAG Network here"},
			{"POCSAG Node Callsign:", "POCSAG Callsign", "Set your paging callsign here"},
			{"POCSAG Radio Frequency:", "POCSAG Frequency", "Set your paging frequency here"},
			{"DAPNET AuthKey:", "DAPNET AuthKey", "Set your DAPNET AuthKey here"},
			{"POCSAG Whitelist:", "POCSAG Whitelist", "Set your POCSAG RIC Whitelist here, if these are set ONLY these RICs will be transmitted. List is comma seperated."},
			{"POCSAG Blacklist:", "POCSAG Blacklist", "Set your POCSAG RIC Blacklist here, if these are set any other RIC will be transmitted, but not these. List is comma seperated."}}},
		"mobilegps": {"Mobile GPS Configuration", []row{
			{"MobileGPS Enable:", "MobileGPS", "Enable the MobileGPS service, this uses a GPS dongle to update the position information in MMDVMHost."},
			{"GPS Port:", "GPS Port", "The port used by the Mobile GPS service."},
			{"GPS Port Speed:", "GPS Port Speed", "Port speed for the Mobile GPS port."}}},
	}
	for id, w := range want {
		s, ok := secs[id]
		if !ok {
			t.Fatalf("section %s missing", id)
		}
		if s.Title != w.title || !s.Apply {
			t.Errorf("%s: title %q apply %v", id, s.Title, s.Apply)
		}
		head := s.Rows[0].Cells
		if len(head) != 2 || !head[0].Header || head[0].Label != "Setting" || head[1].Colspan != 2 || head[1].Label != "Value" ||
			head[1].Tooltip.Body != "The current value from the\nconfiguration files" {
			t.Errorf("%s: header %+v", id, head)
		}
		if len(s.Rows)-1 != len(w.rows) {
			t.Fatalf("%s: %d rows, want %d", id, len(s.Rows)-1, len(w.rows))
		}
		for i, r := range w.rows {
			cells := s.Rows[i+1].Cells
			if len(cells) != 2 || cells[0].Label != r.label || cells[0].Tooltip.Title != r.title || cells[0].Tooltip.Body != r.body {
				t.Errorf("%s row %d: %+v, want %+v", id, i, cells[0], r)
			}
		}
	}
}

func TestConfigureModesBuildControls(t *testing.T) {
	modesFixture(t)
	type check struct {
		section, name, kind, value string
		options                    []string          // value=label; nil skips the check
		attrs                      map[string]string // checked keys only
		absent                     bool
	}
	cases := []struct {
		name   string
		edit   [][3]string
		drop   []string
		checks []check
	}{
		{name: "base", checks: []check{
			{section: "p25", name: "p25StartupHost", kind: "select", value: "10200", options: []string{
				"none=None", "10=10 - Parrot", "10200=10200 - p25.example", "10201=10201 - p25b.example", "31337=31337 - local.example"}},
			{section: "p25", name: "p25nac", kind: "text", value: "293", attrs: map[string]string{"size": "13", "maxlength": "3"}},
			{section: "nxdn", name: "nxdnStartupHost", kind: "select", value: "65000", options: []string{
				"none=None", "10=10 - Parrot", "65000=65000 - nxdn.example"}},
			{section: "nxdn", name: "nxdnran", kind: "text", value: "1", attrs: map[string]string{"size": "13", "maxlength": "2"}},
			{section: "m17", name: "m17StartupHost", kind: "select", value: "M17-M17", options: []string{
				"none=None", "M17-M17=M17-M17", "M17-XOR=M17-XOR"}},
			{section: "m17", name: "m17StartupRoom", kind: "select", value: "C"},
			{section: "m17", name: "m17can", kind: "text", value: "0", attrs: map[string]string{"size": "13", "maxlength": "2"}},
			{section: "pocsag", name: "pocsagServer", kind: "select", value: "dapnet.afu.rwth-aachen.de", options: []string{
				"dapnet.afu.rwth-aachen.de=dapnet.afu.rwth-aachen.de", "db0dbn.ig-funk-siebengebirge.de=db0dbn.ig-funk-siebengebirge.de",
				"dapnet.db0sda.ampr.org=dapnet.db0sda.ampr.org (HAMNET)", "node1.dapnet-italia.it=node1.dapnet-italia.it"}},
			{section: "pocsag", name: "pocsagCallsign", kind: "text", value: "M1ABC", attrs: map[string]string{"size": "13", "maxlength": "12"}},
			{section: "pocsag", name: "pocsagFrequency", kind: "text", value: "439.987500",
				attrs: map[string]string{"size": "13", "maxlength": "12", "style": "background-color: #66cc66;"}},
			{section: "pocsag", name: "pocsagAuthKey", kind: "password", value: "TOPSECRET", attrs: map[string]string{"size": "30", "maxlength": "50"}},
			{section: "pocsag", name: "pocsagWhitelist", kind: "text", value: "1234,5678", attrs: map[string]string{"size": "60", "maxlength": "350"}},
			{section: "pocsag", name: "pocsagBlacklist", kind: "text", value: ""},
			{section: "mobilegps", name: "mobilegps_enable", kind: "toggle", value: "OFF"},
			{section: "mobilegps", name: "mobilegps_port", kind: "select", value: "ttyACM0", options: []string{
				"ttyACM0=/dev/ttyACM0", "ttyAMA0=/dev/ttyAMA0", "ttyUSB0=/dev/ttyUSB0"}},
			{section: "mobilegps", name: "mobilegps_speed", kind: "text", value: "4800", attrs: map[string]string{"size": "13", "maxlength": "6"}},
		}},
		{name: "startup fallbacks", edit: [][3]string{
			{"p25gateway", "Static=10200", "Startup=10\nStatic=10200"},
			{"nxdngateway", "Static=65000", modesDel},
			{"m17gateway", "Startup=M17-M17_C", "Startup=M17-XOR_9"},
		}, checks: []check{
			{section: "p25", name: "p25StartupHost", value: "10"},
			{section: "nxdn", name: "nxdnStartupHost", value: "none"},
			{section: "m17", name: "m17StartupHost", value: "M17-XOR"},
			{section: "m17", name: "m17StartupRoom", value: "9", options: append([]string{"9=9"}, modesLetters()...)},
		}},
		{name: "empty startup is None", edit: [][3]string{
			{"p25gateway", "Static=10200", "Static="}, {"m17gateway", "Startup=M17-M17_C", modesDel}}, checks: []check{
			{section: "p25", name: "p25StartupHost", value: "none"},
			{section: "m17", name: "m17StartupHost", value: "none"},
			{section: "m17", name: "m17StartupRoom", value: "A", options: modesLetters()},
		}},
		{name: "no startup keys", edit: [][3]string{{"p25gateway", "Static=10200", modesDel}}, checks: []check{
			{section: "p25", name: "p25StartupHost", value: "none"},
		}},
		{name: "NAC and RAN rows hidden when falsy", edit: [][3]string{
			{"mmdvmhost", "NAC=293", "NAC=0"}, {"mmdvmhost", "RAN=1", modesDel}}, checks: []check{
			{section: "p25", name: "p25nac", absent: true},
			{section: "nxdn", name: "nxdnran", absent: true},
		}},
		{name: "NAC 000 is shown", edit: [][3]string{{"mmdvmhost", "NAC=293", "NAC=000"}}, checks: []check{
			{section: "p25", name: "p25nac", value: "000"},
		}},
		{name: "without gateways", drop: []string{"nxdngateway", "m17gateway", "dapnetgateway"}, checks: []check{
			{section: "nxdn", name: "nxdnStartupHost", value: "176.9.1.168", options: []string{"176.9.1.168=D2FET Test Host - 176.9.1.168"}},
			{section: "m17", name: "m17StartupHost", value: ""},
			{section: "pocsag", name: "pocsagServer", value: "", options: []string{"=",
				"dapnet.afu.rwth-aachen.de=dapnet.afu.rwth-aachen.de", "db0dbn.ig-funk-siebengebirge.de=db0dbn.ig-funk-siebengebirge.de",
				"dapnet.db0sda.ampr.org=dapnet.db0sda.ampr.org (HAMNET)", "node1.dapnet-italia.it=node1.dapnet-italia.it"}},
			{section: "pocsag", name: "pocsagAuthKey", value: ""},
		}},
		{name: "custom POCSAG server first, bad frequency red", edit: [][3]string{
			{"dapnetgateway", "Address=dapnet.afu.rwth-aachen.de", "Address=dapnet.example"},
			{"mmdvmhost", "Frequency=439987500", "Frequency=436000000"}}, checks: []check{
			{section: "pocsag", name: "pocsagServer", value: "dapnet.example", options: []string{"dapnet.example=dapnet.example",
				"dapnet.afu.rwth-aachen.de=dapnet.afu.rwth-aachen.de", "db0dbn.ig-funk-siebengebirge.de=db0dbn.ig-funk-siebengebirge.de",
				"dapnet.db0sda.ampr.org=dapnet.db0sda.ampr.org (HAMNET)", "node1.dapnet-italia.it=node1.dapnet-italia.it"}},
			{section: "pocsag", name: "pocsagFrequency", value: "436.000000", attrs: map[string]string{"style": "background-color: #ff6666;"}},
		}},
		{name: "mobile GPS on, port not a device", edit: [][3]string{
			{"mmdvmhost", "[Mobile GPS]\nEnable=0", "[Mobile GPS]\nEnable=1"}, {"mobilegps", "Port=/dev/ttyACM0", "Port=COM1"}}, checks: []check{
			{section: "mobilegps", name: "mobilegps_enable", value: "ON"},
			{section: "mobilegps", name: "mobilegps_port", value: "", options: []string{"=", "ttyACM0=/dev/ttyACM0", "ttyAMA0=/dev/ttyAMA0", "ttyUSB0=/dev/ttyUSB0"}},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := modesBase()
			for _, e := range tc.edit {
				modesEdit(t, files, e[0], e[1], e[2])
			}
			for _, d := range tc.drop {
				delete(files, d)
			}
			secs := modesBuild(&cfgContext{St: modesState(files, true)})
			for _, ck := range tc.checks {
				_, ctl, ok := modesFind(secs[ck.section], ck.name)
				if ck.absent {
					if ok {
						t.Errorf("%s shown", ck.name)
					}
					continue
				}
				if !ok {
					t.Errorf("%s missing", ck.name)
					continue
				}
				if (ck.kind != "" && ctl.Kind != ck.kind) || ctl.Value != ck.value {
					t.Errorf("%s = %s %q, want %s %q", ck.name, ctl.Kind, ctl.Value, ck.kind, ck.value)
				}
				if ck.options != nil && !reflect.DeepEqual(modesValues(ctl.Options), ck.options) {
					t.Errorf("%s options = %v\nwant %v", ck.name, modesValues(ctl.Options), ck.options)
				}
				for k, v := range ck.attrs {
					if ctl.Attrs[k] != v {
						t.Errorf("%s attr %s = %q, want %q", ck.name, k, ctl.Attrs[k], v)
					}
				}
			}
		})
	}
}

func modesLetters() []string {
	var out []string
	for r := 'A'; r <= 'Z'; r++ {
		out = append(out, string(r)+"="+string(r))
	}
	return out
}

func TestConfigureModesUnchangedFormWritesNothing(t *testing.T) {
	modesFixture(t)
	for _, marker := range []bool{true, false} {
		got, err := modesPlan(t, modesState(modesBase(), marker), nil)
		if err != nil || len(got) != 0 {
			t.Errorf("marker %v: changes %v, err %v", marker, got, err)
		}
	}
	// Stored values the validators would refuse are left alone when untouched.
	files := modesBase()
	modesEdit(t, files, "mmdvmhost", "CAN=0", "CAN=99")
	modesEdit(t, files, "mmdvmhost", "NAC=293", "NAC=29G")
	modesEdit(t, files, "dapnetgateway", "Callsign=M1ABC", "Callsign=m1 abc")
	if got, err := modesPlan(t, modesState(files, true), nil); err != nil || len(got) != 0 {
		t.Errorf("odd stored values: changes %v, err %v", got, err)
	}
}

func TestConfigureModesApply(t *testing.T) {
	modesFixture(t)
	cases := []struct {
		name    string
		edit    [][3]string
		drop    []string
		form    cfgForm
		want    map[string]string
		wantErr string
	}{
		// P25
		{name: "P25 host", form: cfgForm{"p25StartupHost": "10201"},
			want: map[string]string{"p25gateway/Network/Static": "10201", "ysf2p25/P25 Network/StartupDstId": "10201"}},
		{name: "P25 local host", form: cfgForm{"p25StartupHost": "31337"},
			want: map[string]string{"p25gateway/Network/Static": "31337", "ysf2p25/P25 Network/StartupDstId": "31337"}},
		{name: "P25 none", form: cfgForm{"p25StartupHost": "none"},
			want: map[string]string{"p25gateway/Network/Static": modesDel, "ysf2p25/P25 Network/StartupDstId": modesDel}},
		{name: "P25 host without ysf2p25", drop: []string{"ysf2p25"}, form: cfgForm{"p25StartupHost": "10"},
			want: map[string]string{"p25gateway/Network/Static": "10"}},
		{name: "P25 unknown host", form: cfgForm{"p25StartupHost": "99999"}, wantErr: "P25 Startup Host"},
		{name: "P25 injected host", form: cfgForm{"p25StartupHost": "10200\nStatic=1"}, wantErr: "P25 Startup Host"},
		{name: "P25 NAC lower-cased", form: cfgForm{"p25nac": "ABC"}, want: map[string]string{"mmdvmhost/P25/NAC": "abc"}},
		{name: "P25 NAC empty ignored", form: cfgForm{"p25nac": ""}, want: map[string]string{}},
		{name: "P25 NAC too short", form: cfgForm{"p25nac": "12"}, wantErr: "P25 NAC"},
		{name: "P25 NAC not hex", form: cfgForm{"p25nac": "xyz"}, wantErr: "P25 NAC"},
		{name: "P25 NAC row hidden", edit: [][3]string{{"mmdvmhost", "NAC=293", "NAC=0"}}, form: cfgForm{"p25nac": "abc"},
			want: map[string]string{}},
		{name: "P25 section hidden", edit: [][3]string{{"mmdvmhost", "[P25 Network]\nEnable=1", "[P25 Network]\nEnable=0"}},
			form: cfgForm{"p25StartupHost": "none", "p25nac": "abc"}, want: map[string]string{}},
		// NXDN
		{name: "NXDN host", form: cfgForm{"nxdnStartupHost": "10"},
			want: map[string]string{"nxdngateway/Network/Static": "10", "ysf2nxdn/NXDN Network/StartupDstId": "10"}},
		{name: "NXDN none writes NONE to YSF2NXDN", form: cfgForm{"nxdnStartupHost": "none"},
			want: map[string]string{"nxdngateway/Network/Static": modesDel, "ysf2nxdn/NXDN Network/StartupDstId": "NONE"}},
		{name: "NXDN commented host refused", form: cfgForm{"nxdnStartupHost": "#99"}, wantErr: "NXDN Startup Host"},
		{name: "NXDN direct host without NXDNGateway", drop: []string{"nxdngateway"},
			want: map[string]string{"mmdvmhost/NXDN Network/GatewayAddress": "176.9.1.168", "mmdvmhost/NXDN Network/GatewayPort": "41007"}},
		{name: "NXDN direct host other value", drop: []string{"nxdngateway"}, form: cfgForm{"nxdnStartupHost": "10"}, wantErr: "NXDN Startup Host"},
		{name: "NXDN RAN", form: cfgForm{"nxdnran": "07"}, want: map[string]string{"mmdvmhost/NXDN/RAN": "07"}},
		{name: "NXDN RAN 64", form: cfgForm{"nxdnran": "64"}, want: map[string]string{"mmdvmhost/NXDN/RAN": "64"}},
		{name: "NXDN RAN 0", form: cfgForm{"nxdnran": "0"}, wantErr: "NXDN RAN"},
		{name: "NXDN RAN 65", form: cfgForm{"nxdnran": "65"}, wantErr: "NXDN RAN"},
		{name: "NXDN RAN letters", form: cfgForm{"nxdnran": "a1"}, wantErr: "NXDN RAN"},
		{name: "NXDN hidden", edit: [][3]string{{"mmdvmhost", "[NXDN Network]\nEnable=1", "[NXDN Network]\nEnable=0"}},
			form: cfgForm{"nxdnStartupHost": "10", "nxdnran": "5"}, want: map[string]string{}},
		// M17
		{name: "M17 host and room", form: cfgForm{"m17StartupHost": "M17-XOR", "m17StartupRoom": "D"},
			want: map[string]string{"m17gateway/Network/Startup": "M17-XOR_D"}},
		{name: "M17 room only", form: cfgForm{"m17StartupRoom": "B"}, want: map[string]string{"m17gateway/Network/Startup": "M17-M17_B"}},
		{name: "M17 none", form: cfgForm{"m17StartupHost": "none", "m17StartupRoom": "Z"},
			want: map[string]string{"m17gateway/Network/Startup": modesDel}},
		{name: "M17 from none", edit: [][3]string{{"m17gateway", "Startup=M17-M17_C", modesDel}},
			form: cfgForm{"m17StartupHost": "M17-M17"}, want: map[string]string{"m17gateway/Network/Startup": "M17-M17_A"}},
		{name: "M17 unknown reflector", form: cfgForm{"m17StartupHost": "M17-BAD"}, wantErr: "M17 Startup Host"},
		{name: "M17 unknown module", form: cfgForm{"m17StartupRoom": "c"}, wantErr: "M17 Startup Host"},
		{name: "M17 without gateway", drop: []string{"m17gateway"}, form: cfgForm{"m17StartupHost": "M17-XOR", "m17StartupRoom": "D"},
			want: map[string]string{}},
		{name: "M17 CAN", form: cfgForm{"m17can": "12"}, want: map[string]string{"mmdvmhost/M17/CAN": "12"}},
		{name: "M17 CAN missing key", edit: [][3]string{{"mmdvmhost", "CAN=0", modesDel}}, form: cfgForm{"m17can": "0"},
			want: map[string]string{"mmdvmhost/M17/CAN": "0"}},
		{name: "M17 CAN 65", form: cfgForm{"m17can": "65"}, wantErr: "M17 CAN"},
		{name: "M17 CAN letters", form: cfgForm{"m17can": "ab"}, wantErr: "M17 CAN"},
		{name: "M17 CAN empty ignored", form: cfgForm{"m17can": ""}, want: map[string]string{}},
		// POCSAG
		{name: "POCSAG server", form: cfgForm{"pocsagServer": "node1.dapnet-italia.it"},
			want: map[string]string{"dapnetgateway/DAPNET/Address": "node1.dapnet-italia.it"}},
		{name: "POCSAG unknown server", form: cfgForm{"pocsagServer": "evil.example"}, wantErr: "POCSAG Server"},
		{name: "POCSAG callsign upper-cased", form: cfgForm{"pocsagCallsign": "dl1abc"},
			want: map[string]string{"dapnetgateway/General/Callsign": "DL1ABC"}},
		{name: "POCSAG callsign bad", form: cfgForm{"pocsagCallsign": "DL1 ABC"}, wantErr: "POCSAG Node Callsign"},
		{name: "POCSAG frequency", form: cfgForm{"pocsagFrequency": "439.9875"},
			want: map[string]string{}},
		{name: "POCSAG frequency new", form: cfgForm{"pocsagFrequency": "439.9625"},
			want: map[string]string{"mmdvmhost/POCSAG/Frequency": "439962500"}},
		{name: "POCSAG frequency below 100 MHz (reference defect fixed)", form: cfgForm{"pocsagFrequency": "50.5"},
			want: map[string]string{"mmdvmhost/POCSAG/Frequency": "50500000"}},
		{name: "POCSAG frequency 23 cm", form: cfgForm{"pocsagFrequency": "1296.5"},
			want: map[string]string{"mmdvmhost/POCSAG/Frequency": "1296500000"}},
		{name: "POCSAG frequency text", form: cfgForm{"pocsagFrequency": "439,9875"}, wantErr: "POCSAG Radio Frequency"},
		{name: "POCSAG frequency dot", form: cfgForm{"pocsagFrequency": "."}, wantErr: "POCSAG Radio Frequency"},
		{name: "POCSAG frequency zero", form: cfgForm{"pocsagFrequency": "0"}, wantErr: "POCSAG Radio Frequency"},
		{name: "DAPNET AuthKey stored verbatim", form: cfgForm{"pocsagAuthKey": "ab$c;d"},
			want: map[string]string{"dapnetgateway/DAPNET/AuthKey": "ab$c;d"}},
		{name: "DAPNET AuthKey with space", form: cfgForm{"pocsagAuthKey": "ab c"}, wantErr: "DAPNET AuthKey"},
		{name: "DAPNET AuthKey empty ignored", form: cfgForm{"pocsagAuthKey": ""}, want: map[string]string{}},
		{name: "POCSAG whitelist spaces removed", form: cfgForm{"pocsagWhitelist": "1, 2,3"},
			want: map[string]string{"dapnetgateway/General/WhiteList": "1,2,3"}},
		{name: "POCSAG whitelist cleared", form: cfgForm{"pocsagWhitelist": ""},
			want: map[string]string{"dapnetgateway/General/WhiteList": modesDel}},
		{name: "POCSAG blacklist added", form: cfgForm{"pocsagBlacklist": "99"},
			want: map[string]string{"dapnetgateway/General/BlackList": "99"}},
		{name: "POCSAG whitelist bad", form: cfgForm{"pocsagWhitelist": "1;2"}, wantErr: "POCSAG Whitelist"},
		{name: "POCSAG blacklist bad", form: cfgForm{"pocsagBlacklist": "1,,2"}, wantErr: "POCSAG Blacklist"},
		{name: "POCSAG hidden keeps lists", edit: [][3]string{{"mmdvmhost", "[POCSAG]\nEnable=1", "[POCSAG]\nEnable=0"}},
			form: cfgForm{"pocsagWhitelist": "", "pocsagCallsign": "X"}, want: map[string]string{}},
		{name: "DAPNETGateway missing", drop: []string{"dapnetgateway"}, form: cfgForm{"pocsagCallsign": "DL1ABC"}, wantErr: "DAPNETGateway"},
		{name: "DAPNETGateway missing, frequency only", drop: []string{"dapnetgateway"}, form: cfgForm{"pocsagFrequency": "439.9625"},
			want: map[string]string{"mmdvmhost/POCSAG/Frequency": "439962500"}},
		// Mobile GPS
		{name: "GPS on", form: cfgForm{"mobilegps_enable": "ON"},
			want: map[string]string{"mmdvmhost/Mobile GPS/Enable": "1", "mobilegps/Enabled/Enabled": "1"}},
		{name: "GPS on also sets gateway keys that exist",
			edit: [][3]string{{"ysfgateway", "[GPSD]\nEnable=0", "[Mobile GPS]\nEnable=0"}}, form: cfgForm{"mobilegps_enable": "ON"},
			want: map[string]string{"mmdvmhost/Mobile GPS/Enable": "1", "mobilegps/Enabled/Enabled": "1",
				"ysfgateway/Mobile GPS/Enable": "1", "ysfgateway/Mobile GPS/Address": "127.0.0.1", "ysfgateway/Mobile GPS/Port": "7834"}},
		{name: "GPS bad toggle", form: cfgForm{"mobilegps_enable": "yes"}, wantErr: "MobileGPS Enable"},
		{name: "GPS port", form: cfgForm{"mobilegps_port": "ttyUSB0"}, want: map[string]string{"mobilegps/GPS/Port": "/dev/ttyUSB0"}},
		{name: "GPS port not listed", form: cfgForm{"mobilegps_port": "ttyS0"}, wantErr: "GPS Port"},
		{name: "GPS port path", form: cfgForm{"mobilegps_port": "../sda"}, wantErr: "GPS Port"},
		{name: "GPS speed", form: cfgForm{"mobilegps_speed": "9600"}, want: map[string]string{"mobilegps/GPS/Speed": "9600"}},
		{name: "GPS speed bad", form: cfgForm{"mobilegps_speed": "96a"}, wantErr: "GPS Port Speed"},
		{name: "GPS flat keys: first line only, never added",
			edit: [][3]string{{"mobilegps", "[General]\nCallsign=M1ABC\nDaemon=0\nDebug=0", "[General]\nCallsign=M1ABC\nDaemon=0\nDebug=1"},
				{"mobilegps", "Speed=4800\nDebug=0", "Speed=4800\nDebug=1"}, {"mobilegps", "Enabled=0", modesDel}},
			form: cfgForm{"mobilegps_enable": "ON"},
			want: map[string]string{"mmdvmhost/Mobile GPS/Enable": "1", "mobilegps/General/Debug": "0"}},
		{name: "GPS hidden without controller marker", form: cfgForm{"mobilegps_enable": "ON", "mobilegps_speed": "1"},
			drop: []string{"__nomarker"}, want: map[string]string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := modesBase()
			for _, e := range tc.edit {
				modesEdit(t, files, e[0], e[1], e[2])
			}
			marker := true
			for _, d := range tc.drop {
				if d == "__nomarker" {
					marker = false
				}
				delete(files, d)
			}
			got, err := modesPlan(t, modesState(files, marker), tc.form)
			if tc.wantErr != "" {
				var ce cfgError
				if !errors.As(err, &ce) || !strings.Contains(ce.Error(), tc.wantErr) {
					t.Fatalf("err = %#v, want a cfgError with %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("changes:\n%s\nwant:\n%s", modesDump(got), modesDump(tc.want))
			}
		})
	}
}

func modesDump(m map[string]string) string {
	var out []string
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}

// The defaults each section adds on Apply to the files it owns.
func TestConfigureModesDefaults(t *testing.T) {
	modesFixture(t)
	cases := []struct {
		name string
		edit func(files map[string]string)
		form cfgForm
		want map[string]string
	}{
		{name: "P25Gateway migration", edit: func(f map[string]string) {
			f["p25gateway"] = "[General]\nCallsign=M1ABC\nAnnouncements=1\n[Network]\nStartup=10200\n"
		}, want: map[string]string{
			"p25gateway/Remote Commands/Enable": "1", "p25gateway/Remote Commands/Port": "6074",
			"p25gateway/General/Announcements": modesDel, "p25gateway/Log/DisplayLevel": "1", "p25gateway/Log/FileLevel": "1",
			"p25gateway/Network/P252DMRAddress": "127.0.0.1", "p25gateway/Network/P252DMRPort": "42012",
			"p25gateway/Network/Static": "10200", "p25gateway/Network/Startup": modesDel,
		}},
		{name: "P25 host replaces old Startup", edit: func(f map[string]string) {
			f["p25gateway"] = strings.Replace(f["p25gateway"], "Static=10200", "Startup=10200", 1)
		}, form: cfgForm{"p25StartupHost": "10201"}, want: map[string]string{
			"p25gateway/Network/Static": "10201", "p25gateway/Network/Startup": modesDel, "ysf2p25/P25 Network/StartupDstId": "10201",
		}},
		{name: "NXDNGateway clean-up and migration", edit: func(f map[string]string) {
			f["nxdngateway"] = "[General]\nCallsign=M1ABC\n[Network]\nStartup=65000\nHostsFile=/usr/local/etc/NXDNHosts.txt\n" +
				"[aprs.fi]\nEnable=1\nServer=euro.aprs2.net\n[Mobile GPS]\nEnable=1\nPort=7834\n[APRS]\nDescription=old\n"
			f["mmdvmhost"] = strings.Replace(f["mmdvmhost"], "LocalPort=14021", "LocalPort=3300", 1)
			f["mmdvmhost"] = strings.Replace(f["mmdvmhost"], "GatewayPort=14020", "GatewayPort=41007", 1)
		}, want: map[string]string{
			"nxdngateway/Network/HostsFile1": "/usr/local/etc/NXDNHosts.txt", "nxdngateway/Network/HostsFile": modesDel,
			"nxdngateway/Network/HostsFile2":   filepath.Join(gospotHostsDir, "NXDNHostsLocal.txt"),
			"mmdvmhost/NXDN Network/LocalPort": "14021", "mmdvmhost/NXDN Network/GatewayPort": "14020",
			"nxdngateway/Info/Power": "1", "nxdngateway/Info/Height": "0",
			"nxdngateway/Remote Commands/Enable": "1", "nxdngateway/Remote Commands/Port": "6075",
			"nxdngateway/aprs.fi/Enable": modesDel, "nxdngateway/aprs.fi/Server": modesDel,
			"nxdngateway/Mobile GPS/Enable": modesDel, "nxdngateway/Mobile GPS/Port": modesDel,
			"nxdngateway/General/RptProtocol": "Icom", "nxdngateway/Log/DisplayLevel": "1", "nxdngateway/Log/FileLevel": "1",
			"nxdngateway/APRS/Enable": "1", "nxdngateway/APRS/Address": "127.0.0.1", "nxdngateway/APRS/Port": "8673",
			"nxdngateway/APRS/Suffix": "N", "nxdngateway/APRS/Description": "Pi-Star_RPT",
			"nxdngateway/GPSD/Enable": "0", "nxdngateway/GPSD/Address": "127.0.0.1", "nxdngateway/GPSD/Port": "2947",
			"nxdngateway/Network/Static": "65000", "nxdngateway/Network/Startup": modesDel,
		}},
		{name: "M17Gateway daemon", edit: func(f map[string]string) {
			f["m17gateway"] = strings.Replace(f["m17gateway"], "Daemon=1", "Daemon=0", 1)
		}, want: map[string]string{"m17gateway/General/Daemon": "1"}},
		{name: "M17Gateway daemon not added", edit: func(f map[string]string) {
			f["m17gateway"] = strings.Replace(f["m17gateway"], "Daemon=1\n", "", 1)
		}, want: map[string]string{}},
		{name: "DAPNET and POCSAG defaults", edit: func(f map[string]string) {
			f["dapnetgateway"] = "[General]\nCallsign=DL1ABC\n"
			f["mmdvmhost"] = strings.Replace(f["mmdvmhost"], "ModeHang=5\nDebug=0\n", "ModeHang=10\n", 1)
			f["mmdvmhost"] = strings.Replace(f["mmdvmhost"], "GatewayPort=4800\n", "", 1)
		}, want: map[string]string{
			"dapnetgateway/General/RptAddress": "127.0.0.1", "dapnetgateway/General/RptPort": "3800",
			"dapnetgateway/General/LocalAddress": "127.0.0.1", "dapnetgateway/General/LocalPort": "4800", "dapnetgateway/General/Daemon": "0",
			"dapnetgateway/Log/DisplayLevel": "0", "dapnetgateway/Log/FileLevel": "2", "dapnetgateway/Log/FilePath": "/var/log/gospot",
			"dapnetgateway/Log/FileRoot": "DAPNETGateway", "dapnetgateway/DAPNET/Address": "dapnet.afu.rwth-aachen.de",
			"dapnetgateway/DAPNET/Port": "43434", "dapnetgateway/DAPNET/AuthKey": "TOPSECRET",
			"dapnetgateway/DAPNET/SuppressTimeWhenBusy": "1", "dapnetgateway/DAPNET/Debug": "0",
			"mmdvmhost/POCSAG Network/ModeHang": "5", "mmdvmhost/POCSAG Network/Debug": "0",
			"mmdvmhost/POCSAG Network/GatewayPort": "4800",
		}},
		{name: "Mobile GPS defaults", edit: func(f map[string]string) {
			f["mmdvmhost"] = strings.Replace(f["mmdvmhost"], "[Mobile GPS]\nEnable=0\nAddress=127.0.0.1\nPort=7834\n", "", 1)
			f["ysfgateway"] = "[General]\nSuffix=RPT\n[Info]\nName=Pi-Star\n"
			f["mobilegps"] = strings.Replace(f["mobilegps"], "FilePath=/var/log/gospot", "FilePath=.", 1)
			f["mobilegps"] = strings.Replace(f["mobilegps"], "Daemon=0", "Daemon=1", 1)
		}, want: map[string]string{
			"mmdvmhost/Mobile GPS/Enable": "0", "mmdvmhost/Mobile GPS/Address": "127.0.0.1", "mmdvmhost/Mobile GPS/Port": "7834",
			"ysfgateway/Mobile GPS/Enable": "0", "ysfgateway/Mobile GPS/Address": "127.0.0.1", "ysfgateway/Mobile GPS/Port": "7834",
			"mobilegps/Log/FilePath": "/var/log/gospot", "mobilegps/General/Daemon": "0",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := modesBase()
			tc.edit(files)
			got, err := modesPlan(t, modesState(files, true), tc.form)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("changes:\n%s\nwant:\n%s", modesDump(got), modesDump(tc.want))
			}
		})
	}
}

// Defaults are only added to files that exist; gateway files are never created.
func TestConfigureModesMissingFilesNotCreated(t *testing.T) {
	modesFixture(t)
	files := modesBase()
	for _, f := range []string{"p25gateway", "ysf2p25", "nxdngateway", "ysf2nxdn", "m17gateway", "dapnetgateway", "ysfgateway"} {
		delete(files, f)
	}
	got, err := modesPlan(t, modesState(files, true), cfgForm{"p25StartupHost": "10201", "m17StartupRoom": "D"})
	if err != nil {
		t.Fatal(err)
	}
	for k := range got {
		if !strings.HasPrefix(k, "mmdvmhost/") && !strings.HasPrefix(k, "mobilegps/") {
			t.Errorf("change to a missing file: %s", k)
		}
	}
	want := map[string]string{"mmdvmhost/NXDN Network/GatewayAddress": "176.9.1.168", "mmdvmhost/NXDN Network/GatewayPort": "41007"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("changes:\n%s\nwant:\n%s", modesDump(got), modesDump(want))
	}
}
