package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
)

// Tests for configure_control.go (spec 01). The fixture below is a
// consistent MMDVMHost hotspot: simplex 438.800 MHz, M0ABC, ZUMspot GPIO,
// DMR + D-Star on, Pi-Star 4.2.1. Submitting its own form must change
// nothing; every case edits a few fields and checks the exact changes.

var ctlPass = ctlAPRSPass("M0ABC")

func ctlBaseFiles() map[string]string {
	return map[string]string{
		"mmdvmhost": `[General]
Callsign=M0ABC
Id=2345678
Duplex=0
Display=None
RFModeHang=300
NetModeHang=300

[Info]
RXFrequency=438800000
TXFrequency=438800000
Latitude=51.5
Longitude=-0.12
Location="London"
Description="United Kingdom"
URL=https://www.qrz.com/db/M0ABC

[Modem]
Port=/dev/ttyAMA0
Protocol=uart
UARTPort=/dev/ttyAMA0

[D-Star]
Enable=1
Module=B
SelfOnly=0
ModeHang=20

[D-Star Network]
Enable=1
ModeHang=20

[DMR]
Enable=1
Id=2345678
SelfOnly=0
ModeHang=20

[DMR Network]
Enable=1
Slot1=0
Slot2=1
ModeHang=20

[System Fusion]
Enable=0
SelfOnly=0
ModeHang=20

[System Fusion Network]
Enable=0
ModeHang=20

[P25]
Enable=0
SelfOnly=0
ModeHang=20

[P25 Network]
Enable=0
ModeHang=20

[NXDN]
Enable=0
SelfOnly=0
ModeHang=20

[NXDN Network]
Enable=0
ModeHang=20

[M17]
Enable=0
ModeHang=20

[M17 Network]
Enable=0
ModeHang=20

[POCSAG]
Enable=0

[POCSAG Network]
Enable=0

[FM]
Enable=0

[OLED]
Type=3

[Nextion]
Port=modem
ScreenLayout=0
`,
		"ircddbgateway": `gatewayType=1
gatewayCallsign=M0ABC
repeaterCall1=M0ABC
repeaterBand1=B
repeaterType1=0
repeaterAddress1=127.0.0.1
repeaterPort1=20011
frequency1=438.80000
offset1=0.0000
latitude=51.5
latitude1=51.5
longitude=-0.12
longitude1=-0.12
description1=London
description1_1=London
description2=United Kingdom
description1_2=United Kingdom
url=https://www.qrz.com/db/M0ABC
aprsHostname=euro.aprs2.net
aprsPort=8673
dplusLogin=M0ABC
ircddbHostname=ircv4.openquad.net
ircddbUsername=M0ABC
`,
		"dstarrepeater": `callsign=M0ABC  B
gateway=M0ABC  G
beaconText=M0ABC  B
mode=1
dvapFrequency=438800000
dvmegaRXFrequency=438800000
dvmegaTXFrequency=438800000
restriction=0
rpt1Validation=1
modemType=MMDVM
dvmegaPort=/dev/ttyAMA0
dvmegaVariant=2
`,
		"timeserver":    "callsign=M0ABC\nsendA=0\nsendB=1\nsendC=0\nsendD=0\nsendE=0\n",
		"starnetserver": "callsign=M0ABC\nircddbUsername=M0ABC\n",
		"ysfgateway": `[General]
Callsign=M0ABC
Id=2345678
Suffix=ND

[Info]
RXFrequency=438800000
TXFrequency=438800000
Latitude=51.5
Longitude=-0.12
Description="United Kingdom"
Name=M0ABC_Pi-Star

[aprs.fi]
Server=euro.aprs2.net
Password=` + ctlPass + `
Description=M0ABC_Pi-Star
`,
		"dmrgateway": `[General]
RFTimeout=20
NetTimeout=20

[Info]
RXFrequency=438800000
TXFrequency=438800000
Latitude=51.5
Longitude=-0.12
Location="London"
Description="United Kingdom"
URL=https://www.qrz.com/db/M0ABC

[XLX Network]
Id=2345678

[DMR Network 2]
Id=234567801

[DMR Network 3]
Enabled=0
Name=Old
Id=1
Address=10.0.0.1
Port=1
Local=2
TGRewrite1=x
`,
		"aprsgateway": `[General]
Callsign=M0ABC

[APRS-IS]
Server=euro.aprs2.net
Password=` + ctlPass + `

[Enabled]
Enabled=0
`,
		"p25gateway": "[General]\nCallsign=M0ABC\n\n[Network]\nRFHangTime=0\nNetHangTime=0\n",
		"nxdngateway": `[General]
Callsign=M0ABC
Suffix=ND

[Info]
RXFrequency=438800000
TXFrequency=438800000
Latitude=51.5
Longitude=-0.12
Name="London"
Description="United Kingdom"

[aprs.fi]
Description=M0ABC_Pi-Star
Password=` + ctlPass + `

[Network]
RFHangTime=0
NetHangTime=0
`,
		"m17gateway": "[General]\nCallsign=M0ABC\nSuffix=H\n\n[Info]\nRXFrequency=438800000\nTXFrequency=438800000\n\n[Network]\nHangTime=20\n",
		"ysf2dmr": `[Enabled]
Enabled=0

[Info]
RXFrequency=438800000
TXFrequency=438800000
Latitude=51.5
Longitude=-0.12
Location="London"
URL=https://www.qrz.com/db/M0ABC
Description=M0ABC_Pi-Star

[YSF Network]
Callsign=M0ABC
Suffix=ND

[aprs.fi]
Server=euro.aprs2.net
Enable=0
Password=` + ctlPass + `
Description=M0ABC_Pi-Star
`,
		"dmr2ysf":       "[Enabled]\nEnabled=0\n\n[YSF Network]\nCallsign=M0ABC\nSuffix=ND\n\n[DMR Network]\nId=2345678\n",
		"dapnetgateway": "[General]\nCallsign=M0ABC\n",
		"dgidgateway": `[General]
Callsign=M0ABC
Id=2345678
Suffix=ND
RFHangTime=20
NetHangTime=20

[Info]
RXFrequency=438800000
TXFrequency=438800000
Latitude=51.5
Longitude=-0.12
Description=M0ABC_Pi-Star

[YSF Network]
RFHangTime=20
NetHangTime=20

[FCS Network]
RFHangTime=20
NetHangTime=20

[IMRS Network]
RFHangTime=20
NetHangTime=20
`,
	}
}

type ctlLayoutSection = struct {
	Name string
	Keys [][2]string
}

// ctlParse mirrors the helper's Ini.data()/layout(): raw stripped values,
// first occurrence of a key wins, keys before any header are section "".
func ctlParse(text string) (map[string]map[string]string, []ctlLayoutSection) {
	data := map[string]map[string]string{}
	var layout []ctlLayoutSection
	index := map[string]int{}
	section := ""
	add := func(name string) {
		if _, ok := index[name]; !ok {
			index[name] = len(layout)
			layout = append(layout, ctlLayoutSection{Name: name})
			data[name] = map[string]string{}
		}
	}
	for _, line := range strings.Split(text, "\n") {
		s := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]"):
			section = s[1 : len(s)-1]
			add(section)
		case s != "" && s[0] != '#' && s[0] != ';' && strings.Contains(s, "="):
			add(section)
			k, v, _ := strings.Cut(s, "=")
			k, v = strings.TrimSpace(k), strings.TrimSpace(v)
			if _, seen := data[section][k]; !seen {
				data[section][k] = v
				layout[index[section]].Keys = append(layout[index[section]].Keys, [2]string{k, v})
			}
		}
	}
	return data, layout
}

func ctlMarker(hw string) *string {
	s := "[Modem]\nHardware=" + hw + "\n"
	return &s
}

func ctlState(files map[string]string, markers map[string]*string) configState {
	st := configState{Revision: "r1", Files: map[string]map[string]map[string]string{}}
	st.Layout = map[string][]struct {
		Name string
		Keys [][2]string
	}{}
	for name, text := range files {
		st.Files[name], st.Layout[name] = ctlParse(text)
	}
	st.System.Hostname, st.System.Timezone, st.System.Markers = "pi-star", "Europe/London", markers
	return st
}

func ctlBase() configState {
	return ctlState(ctlBaseFiles(), map[string]*string{"mmdvmhost": ctlMarker("zumspotgpio"), "dstarrepeater": nil})
}

// ctlEnv replaces every probe of the running system and keeps only this
// file's sections registered, so other sections cannot add changes.
func ctlEnv(t *testing.T, devices ...string) string {
	t.Helper()
	dev := t.TempDir()
	for _, d := range append([]string{"ttyAMA0", "ttyUSB0", "tty1", "null"}, devices...) {
		if err := os.WriteFile(filepath.Join(dev, d), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	hosts := filepath.Join(t.TempDir(), "APRSHosts.txt")
	if err := os.WriteFile(hosts, []byte("; APRS hosts\neuro.aprs2.net:14580\nnoam.aprs2.net:14580\n\nrotate.aprs2.net:14580\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, devDir, plat, addrs, zones, version, hostsFile := ctlSysRoot, ctlDevDir, ctlPlatform, ctlAddresses, ctlTimezones, ctlPistarVersion, ctlAPRSHostsFile
	sections := cfgSections
	t.Cleanup(func() {
		ctlSysRoot, ctlDevDir, ctlPlatform, ctlAddresses, ctlTimezones, ctlPistarVersion, ctlAPRSHostsFile = root, devDir, plat, addrs, zones, version, hostsFile
		cfgSections = sections
	})
	ctlSysRoot = fstest.MapFS{
		"proc/sys/kernel/hostname":             {Data: []byte("pi-star-hotspot-01\n")},
		"proc/sys/kernel/osrelease":            {Data: []byte("6.12.25-v7+\n")},
		"proc/uptime":                          {Data: []byte("97260.51 300000.00\n")},
		"proc/loadavg":                         {Data: []byte("0.15 0.2 1.234 1/100 999\n")},
		"sys/class/thermal/thermal_zone0/temp": {Data: []byte("51234\n")},
	}
	ctlDevDir = dev
	ctlPlatform = func() string { return "Raspberry Pi 2 Model B" }
	ctlAddresses = func() []string { return []string{"192.168.1.10", "fd00::10"} }
	ctlTimezones = func() []string { return []string{"Europe/London", "Europe/Kyiv", "UTC"} }
	ctlPistarVersion = func() string { return "4.2.1" }
	ctlAPRSHostsFile = func() string { return hosts }
	var mine []cfgSectionImpl
	for _, s := range sections {
		switch s.ID {
		case "hardware-info", "control-software", "mmdvmhost", "general":
			mine = append(mine, s)
		}
	}
	cfgSections = mine
	return dev
}

func ctlForm(c *cfgContext) cfgForm {
	f := cfgForm{}
	for _, s := range buildCfgPage(c) {
		for _, row := range s.Rows {
			for _, cell := range row.Cells {
				for _, ctl := range cell.Controls {
					if ctl.Name != "" && ctl.Kind != "static" && ctl.Kind != "button" && !f.has(ctl.Name) {
						f[ctl.Name] = ctl.Value
					}
				}
			}
		}
	}
	return f
}

const ctlDrop = "\x00drop"

// ctlRun submits the page's own form with edits (ctlDrop removes a name).
func ctlRun(st configState, edits map[string]string) (configChange, error) {
	c := &cfgContext{St: st}
	f := ctlForm(c)
	for k, v := range edits {
		if v == ctlDrop {
			delete(f, k)
		} else {
			f[k] = v
		}
	}
	return planCfgRaw(c, st.Revision, f)
}

// ctlList flattens a change into sorted "file|section|key=value" lines
// ("key<del>" for removals) followed by "system:op=value" lines.
func ctlList(ch configChange) []string {
	out := []string{}
	for file, sections := range ch.Changes {
		for section, keys := range sections {
			for key, v := range keys {
				if v == nil {
					out = append(out, file+"|"+section+"|"+key+"<del>")
				} else {
					out = append(out, file+"|"+section+"|"+key+"="+*v)
				}
			}
		}
	}
	for op, v := range ch.System {
		out = append(out, fmt.Sprintf("system:%s=%v", op, v))
	}
	sort.Strings(out)
	return out
}

func ctlCheck(t *testing.T, name string, ch configChange, err error, want []string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	got := ctlList(ch)
	if want == nil {
		want = []string{}
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s:\n got %q\nwant %q", name, got, want)
	}
	if (len(want) > 0) != (len(ch.Restart) > 0) || (len(want) > 0 && !reflect.DeepEqual(ch.Restart, cfgRadioUnits)) {
		t.Fatalf("%s: restart %v", name, ch.Restart)
	}
}

func ctlSection(t *testing.T, c *cfgContext, id string) *cfgSection {
	t.Helper()
	for _, s := range buildCfgPage(c) {
		if s.ID == id {
			return &s
		}
	}
	return nil
}

func ctlRowByID(s *cfgSection, id string) *cfgRow {
	if s == nil {
		return nil
	}
	for i := range s.Rows {
		if s.Rows[i].ID == id {
			return &s.Rows[i]
		}
	}
	return nil
}

func ctlControl(s *cfgSection, name string) *cfgControl {
	for _, row := range s.Rows {
		for _, cell := range row.Cells {
			for i := range cell.Controls {
				if cell.Controls[i].Name == name {
					return &cell.Controls[i]
				}
			}
		}
	}
	return nil
}

func ctlRowIDs(s *cfgSection) []string {
	var ids []string
	for _, r := range s.Rows {
		if r.ID != "" {
			ids = append(ids, r.ID)
		}
	}
	return ids
}

// ---------------------------------------------------------------------------

func TestCtlUnchangedFormWritesNothing(t *testing.T) {
	ctlEnv(t)
	ch, err := ctlRun(ctlBase(), nil)
	ctlCheck(t, "unchanged", ch, err, nil)
	if len(ch.Changes) != 0 || len(ch.System) != 0 || len(ch.Restart) != 0 || ch.Revision != "r1" {
		t.Fatalf("%+v", ch)
	}
}

func TestCtlPageLayout(t *testing.T) {
	ctlEnv(t)
	c := &cfgContext{St: ctlBase()}
	page := buildCfgPage(c)
	var titles []string
	for _, s := range page {
		titles = append(titles, s.ID+"="+s.Title)
	}
	want := []string{"hardware-info=Gateway Hardware Information", "control-software=Control Software",
		"mmdvmhost=MMDVMHost Configuration", "general=General Configuration"}
	if !reflect.DeepEqual(titles, want) {
		t.Fatalf("%v", titles)
	}
	if page[0].Apply || !page[1].Apply || !page[2].Apply || !page[3].Apply {
		t.Fatal("Apply buttons: only the hardware table has none")
	}
	for _, s := range page[1:] {
		h := s.Rows[0].Cells
		if !h[0].Header || h[0].Label != "Setting" || h[0].Style != "width: 200px;" || h[1].Colspan != 2 || h[1].Label != "Value" {
			t.Fatalf("%s header %+v", s.ID, h)
		}
	}
	if page[1].Rows[0].Cells[1].Tooltip.Body != "The current value from\nthe configuration files" ||
		page[3].Rows[0].Cells[1].Tooltip.Body != "The current value from the\nconfiguration files" {
		t.Fatal("Value tooltips differ between sections in the reference")
	}
}

func TestCtlHardwareInfo(t *testing.T) {
	ctlEnv(t)
	s := ctlSection(t, &cfgContext{St: ctlBase()}, "hardware-info")
	head, data := s.Rows[0].Cells, s.Rows[1].Cells
	labels := []string{}
	for _, h := range head {
		labels = append(labels, h.Label)
	}
	if !reflect.DeepEqual(labels, []string{"Hostname", "Kernel", "Platform", "CPU Load", "CPU Temp"}) || head[2].Colspan != 2 {
		t.Fatalf("%v", labels)
	}
	if *head[0].Tooltip != (cfgTooltip{"Hostname", "The name of host\nrunning the Pi-Star Software."}) ||
		*head[1].Tooltip != (cfgTooltip{"Release", "This is the version\nnumber of the Linux Kernel running\non this Raspberry Pi."}) ||
		*head[2].Tooltip != (cfgTooltip{"Uptime:", "up 1 day,\n 3 hours,\n 1 minute"}) ||
		*head[3].Tooltip != (cfgTooltip{"CPU Load", "This is the standard Linux\nsystem load indicator."}) ||
		*head[4].Tooltip != (cfgTooltip{"CPU Temp", ""}) {
		t.Fatalf("tooltips %+v %+v %+v", head[0].Tooltip, head[1].Tooltip, head[2].Tooltip)
	}
	if data[0].Label != "pi-star-hotspo.." || data[0].Tooltip.Body != "192.168.1.10\nfd00::10" || data[1].Label != "6.12.25-v7+" ||
		data[2].Label != "Raspberry Pi 2 Model B" || data[2].Colspan != 2 || data[3].Label != "0.15 / 0.20 / 1.23" ||
		data[4].Label != "51.2°C / 124.2°F" || data[4].Style != "background: #fa0" {
		t.Fatalf("%+v", data)
	}
	for _, tc := range []struct{ raw, c, f, colour string }{
		{"45000", "45", "113", "#1d1"}, {"49.96", "50", "122", "#fa0"}, {"69000", "69", "156.2", "#f00"}, {"", "0", "32", "#1d1"},
	} {
		c, f, colour := ctlCPUTemp(tc.raw)
		if c != tc.c || f != tc.f || colour != tc.colour {
			t.Fatalf("%q: %s %s %s", tc.raw, c, f, colour)
		}
	}
	for in, want := range map[string]string{"30.2 1": "up 0 minutes", "60 1": "up 1 minute", "1209600 1": "up 2 weeks", "694861 1": "up 1 week, 1 day, 1 hour, 1 minute"} {
		if got := ctlUptime(in); got != want {
			t.Fatalf("%q: %q", in, got)
		}
	}
}

func TestCtlControlSoftwareBuild(t *testing.T) {
	ctlEnv(t)
	for _, tc := range []struct {
		name              string
		mm, ds            bool
		rx, tx, ctrl, trx string
	}{
		{"mmdvmhost", true, false, "438800000", "438800000", "MMDVM", "SIMPLEX"},
		{"dstarrepeater", false, true, "438800000", "431200000", "DSTAR", "DUPLEX"},
		{"both markers", true, true, "1", "1", "MMDVM", "SIMPLEX"},
		{"no marker", false, false, "438800000", "438800001", "MMDVM", "DUPLEX"},
	} {
		files := ctlBaseFiles()
		files["mmdvmhost"] = strings.Replace(strings.Replace(files["mmdvmhost"], "RXFrequency=438800000", "RXFrequency="+tc.rx, 1), "TXFrequency=438800000", "TXFrequency="+tc.tx, 1)
		markers := map[string]*string{"mmdvmhost": nil, "dstarrepeater": nil}
		if tc.mm {
			markers["mmdvmhost"] = ctlMarker("zum")
		}
		if tc.ds {
			markers["dstarrepeater"] = ctlMarker("dvap")
		}
		s := ctlSection(t, &cfgContext{St: ctlState(files, markers)}, "control-software")
		ctl, trx := ctlControl(s, "controllerSoft"), ctlControl(s, "trxMode")
		if ctl.Kind != "radio" || ctl.Value != tc.ctrl || trx.Value != tc.trx {
			t.Fatalf("%s: %+v %+v", tc.name, ctl, trx)
		}
		other := map[string]string{"MMDVM": "DSTAR", "DSTAR": "MMDVM"}[tc.ctrl]
		if ctl.Attrs["alertValue"] != other || ctl.Attrs["alert"] != ctlPowerCycle {
			t.Fatalf("%s: alert %v", tc.name, ctl.Attrs)
		}
	}
	s := ctlSection(t, &cfgContext{St: ctlBase()}, "control-software")
	if !reflect.DeepEqual(ctlControl(s, "controllerSoft").Options, []linkOption{{"DSTAR", "DStarRepeater "}, {"MMDVM", "MMDVMHost (DV-Mega Minimum Firmware 3.07 Required)"}}) ||
		!reflect.DeepEqual(ctlControl(s, "trxMode").Options, []linkOption{{"SIMPLEX", "Simplex Node "}, {"DUPLEX", "Duplex Repeater (or Half-Duplex on Hotspots)"}}) {
		t.Fatal("radio options")
	}
	row := ctlRowByID(s, "controllerSoft")
	if row.Cells[0].Label != "Controller Software:" || row.Cells[0].Tooltip.Title != "Radio Control Software" || row.Cells[1].Colspan != 2 ||
		ctlRowByID(s, "trxMode").Cells[0].Tooltip.Body != "Choose the mode type Simplex node or Duplex repeater." {
		t.Fatalf("%+v", row)
	}
}

func TestCtlControlSoftwareApply(t *testing.T) {
	ctlEnv(t)
	ch, err := ctlRun(ctlBase(), map[string]string{"controllerSoft": "DSTAR"})
	ctlCheck(t, "to dstarrepeater", ch, err, []string{"system:controller=dstarrepeater"})

	both := ctlBase()
	both.System.Markers["dstarrepeater"] = ctlMarker("dvap")
	ch, err = ctlRun(both, nil)
	ctlCheck(t, "both markers collapse to the chosen one", ch, err, []string{"system:controller=mmdvmhost"})

	none := ctlBase()
	none.System.Markers["mmdvmhost"] = nil
	ch, err = ctlRun(none, nil)
	ctlCheck(t, "no marker creates the mmdvmhost one", ch, err, []string{"system:controller=mmdvmhost"})

	if _, err := ctlRun(ctlBase(), map[string]string{"controllerSoft": "OTHER"}); !ctlIsCfgError(err) {
		t.Fatalf("bad controller: %v", err)
	}
	// SIMPLEX -> DUPLEX with one frequency: RX becomes TX - 1 Hz; the
	// ZUMspot (simplex model) handler then forces Duplex/Slot1 back to 0.
	ch, err = ctlRun(ctlBase(), map[string]string{"trxMode": "DUPLEX"})
	ctlCheck(t, "duplex on a simplex modem", ch, err, []string{"mmdvmhost|Info|RXFrequency=438799999"})
	// With a duplex modem trxMode decides.
	ch, err = ctlRun(ctlBase(), map[string]string{"trxMode": "DUPLEX", "confHardware": "mmdvmhsdualhatgpio"})
	ctlCheck(t, "duplex modem", ch, err, []string{
		"mmdvmhost|Info|RXFrequency=438799999", "mmdvmhost|General|Duplex=1", "mmdvmhost|DMR Network|Slot1=1",
		"system:modem=map[Hardware:mmdvmhsdualhatgpio]"})
	if _, err := ctlRun(ctlBase(), map[string]string{"trxMode": "HALF"}); !ctlIsCfgError(err) {
		t.Fatalf("bad trxMode: %v", err)
	}
}

func ctlIsCfgError(err error) bool {
	var ce cfgError
	return errors.As(err, &ce) && ce != ""
}

func TestCtlMMDVMBuild(t *testing.T) {
	ctlEnv(t, "ttyS2")
	st := ctlBase()
	delete(st.Files["mmdvmhost"]["DMR Network"], "ModeHang")
	s := ctlSection(t, &cfgContext{St: st}, "mmdvmhost")
	want := []string{"MMDVMModeDMR", "MMDVMModeDSTAR", "MMDVMModeM17", "MMDVMModeFUSION", "MMDVMModeP25", "MMDVMModeNXDN",
		"MMDVMModeYSF2DMR", "MMDVMModeDMR2YSF", "MMDVMModePOCSAG", "mmdvmDisplay"}
	if !reflect.DeepEqual(ctlRowIDs(s), want) {
		t.Fatalf("rows %v", ctlRowIDs(s))
	}
	dmr := ctlRowByID(s, "MMDVMModeDMR")
	if dmr.Cells[0].Label != "DMR Mode:" || *dmr.Cells[0].Tooltip != (cfgTooltip{"DMR Mode", "Turn on DMR Features"}) ||
		dmr.Cells[1].Controls[0].Value != "ON" || dmr.Cells[1].Controls[0].Attrs["label"] != "DMR Mode" || dmr.Cells[1].Colspan != 0 {
		t.Fatalf("%+v", dmr)
	}
	hang := dmr.Cells[2].Controls
	if hang[0].Text != "RF Hangtime: " || hang[1].Name != "dmrRfHangTime" || hang[1].Value != "20" || hang[1].Attrs["size"] != "7" ||
		hang[1].Attrs["maxlength"] != "3" || hang[2].Text != "Net Hangtime: " || hang[3].Name != "dmrNetHangTime" || hang[3].Value != "20" {
		t.Fatalf("missing ModeHang shows 20: %+v", hang)
	}
	for name, aria := range map[string]string{"MMDVMModeDSTAR": "DStar Mode", "MMDVMModeM17": "M 17 Mode", "MMDVMModeFUSION": "Y S F Mode",
		"MMDVMModeP25": "P 25 Mode", "MMDVMModeNXDN": "NXDN Mode", "MMDVMModeYSF2DMR": "Y S F 2 DMR Mode", "MMDVMModeDMR2YSF": "DMR 2 Y S F Mode",
		"MMDVMModePOCSAG": "POCSAG Mode"} {
		if c := ctlControl(s, name); c.Attrs["label"] != aria || c.Kind != "toggle" {
			t.Fatalf("%s %+v", name, c)
		}
	}
	if ctlControl(s, "MMDVMModeDSTAR").Value != "ON" || ctlControl(s, "MMDVMModeFUSION").Value != "OFF" {
		t.Fatal("toggle state")
	}
	if r := ctlRowByID(s, "MMDVMModeYSF2DMR"); r.Cells[1].Colspan != 2 || len(r.Cells) != 2 ||
		*r.Cells[0].Tooltip != (cfgTooltip{"YSF2DMR Mode", "Turn on YSF2DMR Features"}) {
		t.Fatalf("%+v", r)
	}
	if r := ctlRowByID(s, "MMDVMModeDMR2YSF"); len(r.Cells) != 3 || r.Cells[2].Controls[0].Text != "Uses 7 prefix on DMRGateway" {
		t.Fatalf("%+v", r)
	}
	if r := ctlRowByID(s, "MMDVMModePOCSAG"); r.Cells[2].Controls[0].Text != "POCSAG Paging Features" || r.Cells[0].Label != "POCSAG:" {
		t.Fatalf("%+v", r)
	}
	disp := ctlRowByID(s, "mmdvmDisplay")
	if disp.Cells[0].Label != "MMDVM Display Type:" || disp.Cells[1].Colspan != 2 || disp.Cells[1].Controls[1].Text != "Port: " ||
		disp.Cells[1].Controls[3].Text != "Nextion Layout: " {
		t.Fatalf("%+v", disp)
	}
	if !reflect.DeepEqual(ctlControl(s, "mmdvmDisplayType").Options, ctlDisplayTypes) || ctlControl(s, "mmdvmDisplayType").Value != "None" {
		t.Fatal("display types")
	}
	if got := ctlControl(s, "mmdvmDisplayPort"); got.Value != "modem" || !reflect.DeepEqual(got.Options, []linkOption{
		{"None", "None"}, {"modem", "modem"}, {"ttyAMA0", "/dev/ttyAMA0"}, {"ttyUSB0", "/dev/ttyUSB0"}, {"ttyS2", "/dev/ttyS2"}}) {
		t.Fatalf("ports %+v", got)
	}
	if got := ctlControl(s, "mmdvmNextionDisplayType"); got.Value != "G4KLX" || !reflect.DeepEqual(got.Options, []linkOption{
		{"G4KLX", "G4KLX"}, {"ON7LDSL2", "ON7LDS L2"}, {"ON7LDSL3", "ON7LDS L3"}, {"ON7LDSL3HS", "ON7LDS L3 HS"}}) {
		t.Fatalf("layouts %+v", got)
	}

	// Display selection and the current-port option.
	for _, tc := range []struct{ display, oled, port, layout, wantType, wantPort, wantLayout string }{
		{"OLED", "6", "/dev/ttyUSB0", "4", "OLED6", "ttyUSB0", "ON7LDSL3HS"},
		{"OLED", "2", "None", "1", "None", "None", "G4KLX"},
		{"Nextion", "3", "/dev/ttyNextionDriver", "2", "Nextion", "ttyNextionDriver", "ON7LDSL2"},
		{"", "3", "", "3", "None", "None", "ON7LDSL3"},
	} {
		st := ctlBase()
		mm := st.Files["mmdvmhost"]
		mm["General"]["Display"], mm["OLED"]["Type"], mm["Nextion"]["Port"], mm["Nextion"]["ScreenLayout"] = tc.display, tc.oled, tc.port, tc.layout
		s := ctlSection(t, &cfgContext{St: st}, "mmdvmhost")
		port := ctlControl(s, "mmdvmDisplayPort")
		if ctlControl(s, "mmdvmDisplayType").Value != tc.wantType || port.Value != tc.wantPort || ctlControl(s, "mmdvmNextionDisplayType").Value != tc.wantLayout {
			t.Fatalf("%+v: %s %s", tc, ctlControl(s, "mmdvmDisplayType").Value, port.Value)
		}
		if !slicesContainsOption(port.Options, tc.wantPort) {
			t.Fatalf("selected port is not an option: %+v", port.Options)
		}
	}

	// Hidden cross-mode rows and no MMDVMHost section without its marker.
	files := ctlBaseFiles()
	delete(files, "dmr2ysf")
	delete(files, "dapnetgateway")
	delete(files, "ysf2dmr")
	files["ysf2nxdn"], files["ysf2p25"], files["dmr2nxdn"] = "[Enabled]\nEnabled=1\n", "[Enabled]\nEnabled=0\n", "[Enabled]\nEnabled=0\n"
	s = ctlSection(t, &cfgContext{St: ctlState(files, map[string]*string{"mmdvmhost": ctlMarker("zum")})}, "mmdvmhost")
	if got := ctlRowIDs(s)[6:]; !reflect.DeepEqual(got, []string{"MMDVMModeYSF2DMR", "MMDVMModeYSF2NXDN", "MMDVMModeYSF2P25", "MMDVMModeDMR2NXDN", "mmdvmDisplay"}) ||
		ctlControl(s, "MMDVMModeYSF2NXDN").Value != "ON" || ctlControl(s, "MMDVMModeYSF2DMR").Value != "OFF" {
		t.Fatalf("cross rows %v", got)
	}
	if ctlSection(t, &cfgContext{St: ctlState(files, map[string]*string{"dstarrepeater": ctlMarker("dvap")})}, "mmdvmhost") != nil {
		t.Fatal("MMDVMHost section needs /etc/dstar-radio.mmdvmhost")
	}
}

func slicesContainsOption(options []linkOption, v string) bool {
	for _, o := range options {
		if o.Value == v {
			return true
		}
	}
	return false
}

func TestCtlMMDVMApply(t *testing.T) {
	ctlEnv(t)
	network3 := func(name, port, local string) []string {
		return []string{
			"dmrgateway|DMR Network 3|Enabled=1", "dmrgateway|DMR Network 3|Name=" + name, "dmrgateway|DMR Network 3|Id=234567801",
			"dmrgateway|DMR Network 3|Address=127.0.0.1", "dmrgateway|DMR Network 3|Port=" + port, "dmrgateway|DMR Network 3|Local=" + local,
			"dmrgateway|DMR Network 3|TGRewrite0=2,7000001,2,1,999998", "dmrgateway|DMR Network 3|SrcRewrite0=2,1,2,7000001,999998",
			"dmrgateway|DMR Network 3|PCRewrite0=2,7000001,2,1,999998", `dmrgateway|DMR Network 3|Password="PASSWORD"`,
			"dmrgateway|DMR Network 3|Location=0", "dmrgateway|DMR Network 3|Debug=0", "dmrgateway|DMR Network 3|TGRewrite1<del>",
		}
	}
	cases := []struct {
		name  string
		edits map[string]string
		want  []string
	}{
		{"DMR off", map[string]string{"MMDVMModeDMR": "OFF"}, []string{"mmdvmhost|DMR|Enable=0", "mmdvmhost|DMR Network|Enable=0"}},
		{"YSF on disables DMR2YSF", map[string]string{"MMDVMModeFUSION": "ON"}, []string{
			"mmdvmhost|System Fusion|Enable=1", "mmdvmhost|System Fusion Network|Enable=1"}},
		{"P25 and NXDN on", map[string]string{"MMDVMModeP25": "ON", "MMDVMModeNXDN": "ON", "MMDVMModeM17": "ON"}, []string{
			"mmdvmhost|P25|Enable=1", "mmdvmhost|P25 Network|Enable=1", "mmdvmhost|NXDN|Enable=1", "mmdvmhost|NXDN Network|Enable=1",
			"mmdvmhost|M17|Enable=1", "mmdvmhost|M17 Network|Enable=1"}},
		{"YSF2DMR wins over DMR", map[string]string{"MMDVMModeYSF2DMR": "ON"}, []string{"ysf2dmr|Enabled|Enabled=1"}},
		{"DMR2YSF rebuilds DMR Network 3 and turns YSF off", map[string]string{"MMDVMModeDMR2YSF": "ON", "MMDVMModeFUSION": "ON"},
			append([]string{"dmr2ysf|Enabled|Enabled=1"}, network3("DMR2YSF_Cross-over", "62033", "62034")...)},
		{"POCSAG on", map[string]string{"MMDVMModePOCSAG": "ON"}, []string{"mmdvmhost|POCSAG|Enable=1", "mmdvmhost|POCSAG Network|Enable=1"}},
		{"hidden YSF2NXDN is ignored", map[string]string{"MMDVMModeYSF2NXDN": "ON", "MMDVMModeDMR2NXDN": "ON"}, []string{}},
		{"hang times, 0 is saved", map[string]string{"dmrRfHangTime": "0", "dmrNetHangTime": "25", "ysfRfHangTime": "30", "ysfNetHangTime": "40"}, []string{
			"mmdvmhost|DMR|ModeHang=0", "dmrgateway|General|RFTimeout=0", "mmdvmhost|DMR Network|ModeHang=25", "dmrgateway|General|NetTimeout=25",
			"mmdvmhost|System Fusion|ModeHang=30", "mmdvmhost|System Fusion Network|ModeHang=40",
			"dgidgateway|General|RFHangTime=30", "dgidgateway|YSF Network|RFHangTime=30", "dgidgateway|FCS Network|RFHangTime=30", "dgidgateway|IMRS Network|RFHangTime=30",
			"dgidgateway|General|NetHangTime=40", "dgidgateway|YSF Network|NetHangTime=40", "dgidgateway|FCS Network|NetHangTime=40", "dgidgateway|IMRS Network|NetHangTime=40"}},
		{"other hang times", map[string]string{"dstarRfHangTime": "5", "dstarNetHangTime": "6", "p25RfHangTime": "7", "p25NetHangTime": "8",
			"nxdnRfHangTime": "9", "nxdnNetHangTime": "10", "m17RfHangTime": "11", "m17NetHangTime": "12"}, []string{
			"mmdvmhost|D-Star|ModeHang=5", "mmdvmhost|D-Star Network|ModeHang=6", "mmdvmhost|P25|ModeHang=7", "mmdvmhost|P25 Network|ModeHang=8",
			"mmdvmhost|NXDN|ModeHang=9", "mmdvmhost|NXDN Network|ModeHang=10", "mmdvmhost|M17|ModeHang=11", "mmdvmhost|M17 Network|ModeHang=12",
			"m17gateway|Network|HangTime=12"}},
		{"blank hang time is left alone", map[string]string{"dmrRfHangTime": " "}, []string{}},
		{"OLED type 6", map[string]string{"mmdvmDisplayType": "OLED6"}, []string{
			"mmdvmhost|General|Display=OLED", "mmdvmhost|OLED|Type=6", "mmdvmhost|OLED|Scroll=0"}},
		{"Nextion on USB, layout L3 HS", map[string]string{"mmdvmDisplayType": "Nextion", "mmdvmDisplayPort": "ttyUSB0", "mmdvmNextionDisplayType": "ON7LDSL3HS"}, []string{
			"mmdvmhost|General|Display=Nextion", "mmdvmhost|Nextion|Port=/dev/ttyUSB0", "mmdvmhost|Nextion|ScreenLayout=4"}},
		{"port None", map[string]string{"mmdvmDisplayPort": "None", "mmdvmNextionDisplayType": "ON7LDSL2"}, []string{
			"mmdvmhost|Nextion|Port=None", "mmdvmhost|Nextion|ScreenLayout=2"}},
	}
	for _, tc := range cases {
		ch, err := ctlRun(ctlBase(), tc.edits)
		ctlCheck(t, tc.name, ch, err, tc.want)
	}
	for name, edits := range map[string]map[string]string{
		"hang time letters": {"dmrRfHangTime": "2x"}, "hang time 4 digits": {"nxdnNetHangTime": "1000"},
		"display type": {"mmdvmDisplayType": "OLED9"}, "display port": {"mmdvmDisplayPort": "../etc/passwd"},
		"layout": {"mmdvmNextionDisplayType": "L1"}, "toggle": {"MMDVMModeDMR": "yes"},
	} {
		if _, err := ctlRun(ctlBase(), edits); !ctlIsCfgError(err) {
			t.Fatalf("%s: %v", name, err)
		}
	}

	// DMR2NXDN overrides DMR2YSF and YSF2NXDN.
	files := ctlBaseFiles()
	// ysf2nxdn takes the same keys as ysf2dmr.
	files["dmr2nxdn"], files["ysf2nxdn"] = "[Enabled]\nEnabled=0\n\n[DMR Network]\nId=2345678\n", files["ysf2dmr"]
	st := ctlState(files, map[string]*string{"mmdvmhost": ctlMarker("zumspotgpio")})
	ch, err := ctlRun(st, map[string]string{"MMDVMModeDMR2YSF": "ON", "MMDVMModeDMR2NXDN": "ON", "MMDVMModeYSF2NXDN": "ON"})
	want := append([]string{"dmr2nxdn|Enabled|Enabled=1"},
		network3("DMR2NXDN_Cross-over", "62035", "62036")...)
	ctlCheck(t, "DMR2NXDN", ch, err, want)
	// YSF2P25 is switched off again when YSF is off.
	files["ysf2p25"] = files["ysf2dmr"]
	st = ctlState(files, map[string]*string{"mmdvmhost": ctlMarker("zumspotgpio")})
	ch, err = ctlRun(st, map[string]string{"MMDVMModeYSF2P25": "ON", "MMDVMModeP25": "ON"})
	ctlCheck(t, "YSF2P25 needs YSF", ch, err, nil)
	ch, err = ctlRun(st, map[string]string{"MMDVMModeYSF2P25": "ON", "MMDVMModeFUSION": "ON", "MMDVMModeP25": "ON"})
	ctlCheck(t, "YSF2P25", ch, err, []string{"ysf2p25|Enabled|Enabled=1", "mmdvmhost|System Fusion|Enable=1", "mmdvmhost|System Fusion Network|Enable=1"})

	// Old [General] ModeHang / DMRGateway Timeout are removed, RF/NetModeHang forced to 300.
	st = ctlBase()
	st.Files["mmdvmhost"]["General"]["ModeHang"], st.Files["mmdvmhost"]["General"]["RFModeHang"] = "10", "20"
	st.Files["dmrgateway"]["General"]["Timeout"] = "10"
	ch, err = ctlRun(st, nil)
	ctlCheck(t, "old hang settings", ch, err, []string{"mmdvmhost|General|ModeHang<del>", "mmdvmhost|General|RFModeHang=300", "dmrgateway|General|Timeout<del>"})

	// NextionDriver: a Nextion on the modem's UART goes through the driver.
	st = ctlBase()
	st.Files["mmdvmhost"]["NextionDriver"] = map[string]string{"Port": "modem"}
	ch, err = ctlRun(st, map[string]string{"mmdvmDisplayPort": "ttyAMA0"})
	ctlCheck(t, "NextionDriver", ch, err, []string{"mmdvmhost|Nextion|Port=/dev/ttyNextionDriver"})
	st = ctlBase()
	st.Files["mmdvmhost"]["Nextion"]["Port"] = "/dev/modem"
	ch, err = ctlRun(st, nil)
	ctlCheck(t, "/dev/modem", ch, err, []string{"mmdvmhost|Nextion|Port=modem"})
}

func TestCtlGeneralBuild(t *testing.T) {
	ctlEnv(t)
	s := ctlSection(t, &cfgContext{St: ctlBase()}, "general")
	want := []string{"confHostame", "confCallsign", "dmrId", "confFREQ", "confLatitude", "confLongitude", "confDesc1", "confDesc2", "confURL",
		"confHardware", "nodeMode", "confDMRWhiteList", "APRSGatewayEnable", "selectedAPRSHost", "systemTimezone"}
	if !reflect.DeepEqual(ctlRowIDs(s), want) {
		t.Fatalf("rows %v", ctlRowIDs(s))
	}
	values := map[string]string{"confHostame": "pi-star", "confCallsign": "M0ABC", "dmrId": "2345678", "confFREQ": "438.800000",
		"confLatitude": "51.5", "confLongitude": "-0.12", "confDesc1": "London", "confDesc2": "United Kingdom",
		"confURL": "https://www.qrz.com/db/M0ABC", "urlAuto": "auto", "confHardware": "zumspotgpio", "nodeMode": "pub",
		"confDMRWhiteList": "", "APRSGatewayEnable": "OFF", "selectedAPRSHost": "euro.aprs2.net", "systemTimezone": "Europe/London"}
	for name, v := range values {
		if c := ctlControl(s, name); c == nil || c.Value != v {
			t.Fatalf("%s: %+v", name, c)
		}
	}
	attrs := map[string][2]string{"confHostame": {"13", "15"}, "confCallsign": {"13", "7"}, "dmrId": {"13", "9"}, "confFREQ": {"13", "12"},
		"confLatitude": {"13", "9"}, "confLongitude": {"13", "9"}, "confDesc1": {"30", "30"}, "confDesc2": {"30", "30"}, "confURL": {"30", "30"},
		"confDMRWhiteList": {"30", "50"}}
	for name, a := range attrs {
		if c := ctlControl(s, name); c.Kind != "text" || c.Attrs["size"] != a[0] || c.Attrs["maxlength"] != a[1] {
			t.Fatalf("%s attrs %+v", name, c.Attrs)
		}
	}
	labels := map[string][3]string{
		"confHostame":       {"Hostname:", "System Hostname", "This is the system hostname, used for access to the dashboard etc."},
		"confCallsign":      {"Node Callsign:", "Gateway Callsign", `This is your licenced callsign for use on this gateway, do not append the "G"`},
		"dmrId":             {"CCS7/DMR ID:", "CCS7/DMR ID", "Enter your CCS7 / DMR ID here"},
		"confFREQ":          {"Radio Frequency:", "Radio Frequency", "This is the Frequency your\nPi-Star is on"},
		"confLatitude":      {"Latitude:", "Gateway Latitude", "This is the latitude where the gateway is located (positive number for North, negative number for South) - Set to 0 to hide your hotspot location"},
		"confLongitude":     {"Longitude:", "Gateway Longitude", "This is the longitude where the gateway is located (positive number for East, negative number for West) - Set to 0 to hide your hotspot location"},
		"confDesc1":         {"Town:", "Gateway Town", "The town where the gateway is located"},
		"confDesc2":         {"Country:", "Gateway Country", "The country where the gateway is located"},
		"confURL":           {"URL:", "Gateway URL", "The URL used to access this dashboard"},
		"confHardware":      {"Radio/Modem Type:", "Radio/Modem", "What kind of radio or modem hardware do you have?"},
		"nodeMode":          {"Node Type:", "Node Lock", `Set the public/private node type. "Private" limits access to your system to your ID/Callsign only, this may be a licence requirement for your country and helps prevent network loops.`},
		"confDMRWhiteList":  {"DMR Access List:", "DMR IDs", "Set the DMR IDs here that should have access to your hotspot, expected comma seperated list."},
		"APRSGatewayEnable": {"APRS Host Enable:", "APRS Host Enable", "Enabling this feature will make your location public."},
		"selectedAPRSHost":  {"APRS Host:", "APRS Host", "Set your prefered APRS host here."},
		"systemTimezone":    {"System Time Zone:", "System TimeZone", "Set the system timezone"},
	}
	for id, l := range labels {
		cell := ctlRowByID(s, id).Cells[0]
		if cell.Label != l[0] || cell.Tooltip.Title != l[1] || cell.Tooltip.Body != l[2] {
			t.Fatalf("%s: %+v %+v", id, cell, cell.Tooltip)
		}
	}
	texts := map[string]string{"confHostame": "Do not add suffixes such as .local", "confFREQ": "MHz",
		"confLatitude": "degrees (positive value for North, negative for South)", "confLongitude": "degrees (positive value for East, negative for West)"}
	for id, txt := range texts {
		if c := ctlRowByID(s, id).Cells[1].Controls; c[len(c)-1].Text != txt {
			t.Fatalf("%s: %+v", id, c)
		}
	}
	url := ctlRowByID(s, "confURL")
	if len(url.Cells) != 3 || url.Cells[1].Colspan != 0 || url.Cells[2].Style != "width: 300px;" ||
		!reflect.DeepEqual(url.Cells[2].Controls[0].Options, []linkOption{{"auto", "Auto"}, {"man", "Manual"}}) {
		t.Fatalf("%+v", url)
	}
	if c := ctlControl(s, "APRSGatewayEnable"); c.Kind != "toggle" || c.Attrs["label"] != "Enable APRS Position Reporting" {
		t.Fatalf("%+v", c)
	}
	if got := ctlControl(s, "selectedAPRSHost").Options; !reflect.DeepEqual(got, []linkOption{
		{"euro.aprs2.net", "euro.aprs2.net"}, {"noam.aprs2.net", "noam.aprs2.net"}, {"rotate.aprs2.net", "rotate.aprs2.net"}}) {
		t.Fatalf("aprs %v", got)
	}
	if got := ctlControl(s, "systemTimezone").Options; len(got) != 3 || got[2].Value != "UTC" {
		t.Fatalf("tz %v", got)
	}
	if got := ctlControl(s, "nodeMode").Options; !reflect.DeepEqual(got, []linkOption{{"prv", "Private"}, {"pub", "Public"}}) {
		t.Fatalf("%v", got)
	}
	hw := ctlControl(s, "confHardware").Options
	if len(hw) != 53 || hw[0] != (linkOption{"", "--"}) || hw[1].Value != "idrp2c" || hw[2] != (linkOption{"dvmpis", "DV-Mega Raspberry Pi Hat (GPIO) - Single Band (70cm)"}) ||
		hw[len(hw)-1] != (linkOption{"rptrbldrv3v4v5hsdup", "Repeater Builder v3/v4/v5 Duplex 460800 baud (GPIO)"}) ||
		slicesContainsOption(hw, "icomTerminalAuto") || slicesContainsOption(hw, "dvmpicast") {
		t.Fatalf("hardware options without /dev/icom_ta and /dev/ttyS2: %d %v", len(hw), hw[:3])
	}
	if hw[30] != (linkOption{"mmdvmhshat", "MMDVM_HS_Hat (DB9MAT & DF2ET) for Pi (GPIO)"}) {
		t.Fatalf("%v", hw[30])
	}
}

func TestCtlGeneralVisibility(t *testing.T) {
	dev := ctlEnv(t)
	for _, d := range []string{"icom_ta", "ttyS2"} {
		os.WriteFile(filepath.Join(dev, d), nil, 0o600)
	}
	hw := ctlControl(ctlSection(t, &cfgContext{St: ctlBase()}, "general"), "confHardware").Options
	if len(hw) != 57 || hw[1].Value != "icomTerminalAuto" || hw[11].Value != "dvmpicast" || hw[13].Value != "dvmpicasthd" {
		t.Fatalf("%d %v", len(hw), hw[:2])
	}

	type vis struct {
		name    string
		edit    func(st *configState)
		present []string
		absent  []string
	}
	for _, tc := range []vis{
		{"duplex rows", func(st *configState) { st.Files["mmdvmhost"]["Info"]["TXFrequency"] = "431200000" },
			[]string{"confFREQrx", "confFREQtx"}, []string{"confFREQ"}},
		{"DMR off, P25 on keeps the ID", func(st *configState) {
			st.Files["mmdvmhost"]["DMR"]["Enable"], st.Files["mmdvmhost"]["P25"]["Enable"] = "0", "1"
		}, []string{"dmrId"}, []string{"confDMRWhiteList"}},
		{"no DMR/P25/YSF hides the ID", func(st *configState) { st.Files["mmdvmhost"]["DMR"]["Enable"] = "0" }, nil, []string{"dmrId", "confDMRWhiteList"}},
		{"NXDN ID", func(st *configState) { st.Files["mmdvmhost"]["NXDN"]["Enable"] = "1" }, []string{"nxdnId"}, nil},
		{"private hides the access list", func(st *configState) { st.Files["mmdvmhost"]["DMR"]["SelfOnly"] = "1" }, nil, []string{"confDMRWhiteList"}},
		{"no aprsgateway", func(st *configState) { delete(st.Files, "aprsgateway") }, nil, []string{"APRSGatewayEnable"}},
		{"DStarRepeater", func(st *configState) {
			st.System.Markers = map[string]*string{"dstarrepeater": ctlMarker("dvap")}
		}, []string{"confHardware"}, []string{"dmrId", "nxdnId", "confDMRWhiteList"}},
		{"no controller", func(st *configState) { st.System.Markers = nil }, nil, []string{"confHardware", "dmrId"}},
	} {
		st := ctlBase()
		tc.edit(&st)
		s := ctlSection(t, &cfgContext{St: st}, "general")
		for _, id := range tc.present {
			if ctlRowByID(s, id) == nil {
				t.Fatalf("%s: %s missing", tc.name, id)
			}
		}
		for _, id := range tc.absent {
			if ctlRowByID(s, id) != nil {
				t.Fatalf("%s: %s shown", tc.name, id)
			}
		}
	}

	st := ctlBase()
	mm := st.Files["mmdvmhost"]
	mm["Info"]["RXFrequency"], mm["Info"]["TXFrequency"] = "431200000", "438800000"
	mm["NXDN"]["Enable"], mm["NXDN"]["Id"], mm["DMR"]["WhiteList"] = "1", "12345", "1234567,7654321"
	delete(mm["General"], "Id")
	mm["DMR"]["Id"] = "234567801"
	st.System.Markers["mmdvmhost"] = ctlMarker("unknownmodem")
	st.Files["ircddbgateway"][""]["url"] = "http://example.com"
	st.Files["ircddbgateway"][""]["aprsHostname"] = "my.aprs.host"
	st.Files["aprsgateway"]["Enabled"]["Enabled"] = "1"
	st.System.Timezone = "America/Phoenix"
	s := ctlSection(t, &cfgContext{St: st}, "general")
	for name, v := range map[string]string{"confFREQrx": "431.200000", "confFREQtx": "438.800000", "nxdnId": "12345", "dmrId": "234567801",
		"confDMRWhiteList": "1234567,7654321", "confHardware": "", "urlAuto": "man", "APRSGatewayEnable": "ON",
		"selectedAPRSHost": "my.aprs.host", "systemTimezone": "America/Phoenix"} {
		if c := ctlControl(s, name); c.Value != v {
			t.Fatalf("%s: %q", name, c.Value)
		}
	}
	if r := ctlRowByID(s, "confFREQrx"); r.Cells[0].Label != "Radio Frequency RX:" || r.Cells[0].Tooltip.Body != "This is the Frequency your\nrepeater will listen on" ||
		ctlRowByID(s, "confFREQtx").Cells[0].Tooltip.Body != "This is the Frequency your\nrepeater will transmit on" || ctlControl(s, "confFREQtx").Attrs["id"] != "confFREQtx" {
		t.Fatal("duplex labels")
	}
	if r := ctlRowByID(s, "nxdnId"); r.Cells[0].Label != "NXDN ID:" || ctlControl(s, "nxdnId").Attrs["maxlength"] != "5" {
		t.Fatal("nxdn")
	}
	if o := ctlControl(s, "selectedAPRSHost").Options; o[len(o)-1].Value != "my.aprs.host" {
		t.Fatal("stored APRS host kept as an option")
	}
	// Private: SelfOnly=1; other values select neither radio.
	for v, want := range map[string]string{"1": "prv", "0": "pub", "": "pub", "2": ""} {
		st := ctlBase()
		st.Files["mmdvmhost"]["DMR"]["SelfOnly"] = v
		if got := ctlControl(ctlSection(t, &cfgContext{St: st}, "general"), "nodeMode").Value; got != want {
			t.Fatalf("SelfOnly=%q: %q", v, got)
		}
	}
	for hz, want := range map[string]string{"438800000": "438.800000", "1296500000": "1296.500000", "70500000": "70.500000", "": "0.000000", "junk": "0.000000"} {
		if got := ctlMHz(hz); got != want {
			t.Fatalf("%q: %q", hz, got)
		}
	}
}

func TestCtlGeneralApply(t *testing.T) {
	ctlEnv(t)
	newPass := ctlAPRSPass("M0XYZ")
	cases := []struct {
		name  string
		edits map[string]string
		want  []string
	}{
		{"callsign", map[string]string{"confCallsign": "m0xyz"}, []string{
			"ircddbgateway||gatewayCallsign=M0XYZ", "ircddbgateway||dplusLogin=M0XYZ   ", "ircddbgateway||ircddbUsername=M0XYZ",
			"ircddbgateway||repeaterCall1=M0XYZ", "ircddbgateway||url=https://www.qrz.com/db/M0XYZ",
			"dstarrepeater||callsign=M0XYZ  B", "dstarrepeater||gateway=M0XYZ  G", "dstarrepeater||beaconText=M0XYZ  B",
			"timeserver||callsign=M0XYZ", "starnetserver||callsign=M0XYZ", "starnetserver||ircddbUsername=M0XYZ",
			"mmdvmhost|General|Callsign=M0XYZ", "mmdvmhost|Info|URL=https://www.qrz.com/db/M0XYZ",
			"ysfgateway|General|Callsign=M0XYZ", "ysfgateway|aprs.fi|Password=" + newPass, "ysfgateway|aprs.fi|Description=M0XYZ_Pi-Star",
			"ysfgateway|Info|Name=M0XYZ_Pi-Star",
			"ysf2dmr|aprs.fi|Password=" + newPass, "ysf2dmr|aprs.fi|Description=M0XYZ_Pi-Star", "ysf2dmr|YSF Network|Callsign=M0XYZ",
			"ysf2dmr|Info|Description=M0XYZ_Pi-Star", "ysf2dmr|Info|URL=https://www.qrz.com/db/M0XYZ",
			"dmr2ysf|YSF Network|Callsign=M0XYZ", "p25gateway|General|Callsign=M0XYZ",
			"nxdngateway|aprs.fi|Description=M0XYZ_Pi-Star", "nxdngateway|aprs.fi|Password=" + newPass, "nxdngateway|General|Callsign=M0XYZ",
			"m17gateway|General|Callsign=M0XYZ", "dgidgateway|General|Callsign=M0XYZ", "dgidgateway|Info|Description=M0XYZ_Pi-Star",
			"dmrgateway|Info|URL=https://www.qrz.com/db/M0XYZ",
			"aprsgateway|General|Callsign=M0XYZ", "aprsgateway|APRS-IS|Password=" + newPass}},
		{"APRS position reporting on", map[string]string{"APRSGatewayEnable": "ON"}, []string{"aprsgateway|Enabled|Enabled=1"}},
		{"simplex 2 m", map[string]string{"confFREQ": "145.5"}, []string{
			"mmdvmhost|Info|RXFrequency=145500000", "mmdvmhost|Info|TXFrequency=145500000", "mmdvmhost|D-Star|Module=C",
			"dmrgateway|Info|RXFrequency=145500000", "dmrgateway|Info|TXFrequency=145500000",
			"ysfgateway|Info|RXFrequency=145500000", "ysfgateway|Info|TXFrequency=145500000",
			"ysf2dmr|Info|RXFrequency=145500000", "ysf2dmr|Info|TXFrequency=145500000",
			"dgidgateway|Info|RXFrequency=145500000", "dgidgateway|Info|TXFrequency=145500000",
			"nxdngateway|Info|RXFrequency=145500000", "nxdngateway|Info|TXFrequency=145500000",
			"m17gateway|Info|RXFrequency=145500000", "m17gateway|Info|TXFrequency=145500000",
			"ircddbgateway||frequency1=145.50000", "ircddbgateway||repeaterBand1=C",
			"dstarrepeater||dvapFrequency=145500000", "dstarrepeater||dvmegaRXFrequency=145500000", "dstarrepeater||dvmegaTXFrequency=145500000",
			"dstarrepeater||callsign=M0ABC  C", "dstarrepeater||beaconText=M0ABC  C", "timeserver||sendB=0", "timeserver||sendC=1"}},
		{"4 m is not scaled by ten", map[string]string{"confFREQ": "70.4250"}, []string{
			"mmdvmhost|Info|RXFrequency=70425000", "mmdvmhost|Info|TXFrequency=70425000",
			"dmrgateway|Info|RXFrequency=70425000", "dmrgateway|Info|TXFrequency=70425000",
			"ysfgateway|Info|RXFrequency=70425000", "ysfgateway|Info|TXFrequency=70425000",
			"ysf2dmr|Info|RXFrequency=70425000", "ysf2dmr|Info|TXFrequency=70425000",
			"dgidgateway|Info|RXFrequency=70425000", "dgidgateway|Info|TXFrequency=70425000",
			"nxdngateway|Info|RXFrequency=70425000", "nxdngateway|Info|TXFrequency=70425000",
			"m17gateway|Info|RXFrequency=70425000", "m17gateway|Info|TXFrequency=70425000",
			"ircddbgateway||frequency1=70.42500",
			"dstarrepeater||dvapFrequency=70425000", "dstarrepeater||dvmegaRXFrequency=70425000", "dstarrepeater||dvmegaTXFrequency=70425000",
			"timeserver||sendB=0"}},
		{"module suffix from the D-Star section", map[string]string{"confDStarModuleSuffix": "e"}, []string{
			"mmdvmhost|D-Star|Module=E", "ircddbgateway||repeaterBand1=E", "dstarrepeater||callsign=M0ABC  E", "dstarrepeater||beaconText=M0ABC  E",
			"timeserver||sendB=0", "timeserver||sendE=1"}},
		{"latitude 0 is saved", map[string]string{"confLatitude": "0", "confLongitude": "-1.25"}, []string{
			"ircddbgateway||latitude=0", "ircddbgateway||latitude1=0", "ircddbgateway||longitude=-1.25", "ircddbgateway||longitude1=-1.25",
			"mmdvmhost|Info|Latitude=0", "ysfgateway|Info|Latitude=0", "ysf2dmr|Info|Latitude=0", "dgidgateway|Info|Latitude=0",
			"dmrgateway|Info|Latitude=0", "nxdngateway|Info|Latitude=0",
			"mmdvmhost|Info|Longitude=-1.25", "ysfgateway|Info|Longitude=-1.25", "ysf2dmr|Info|Longitude=-1.25", "dgidgateway|Info|Longitude=-1.25",
			"dmrgateway|Info|Longitude=-1.25", "nxdngateway|Info|Longitude=-1.25"}},
		{"town and country", map[string]string{"confDesc1": "Bath!", "confDesc2": "England, UK"}, []string{
			"ircddbgateway||description1=Bath", "ircddbgateway||description1_1=Bath", `mmdvmhost|Info|Location="Bath"`, `dmrgateway|Info|Location="Bath"`,
			`ysf2dmr|Info|Location="Bath"`, `nxdngateway|Info|Name="Bath"`,
			"ircddbgateway||description2=England, UK", "ircddbgateway||description1_2=England, UK", `mmdvmhost|Info|Description="England, UK"`,
			`dmrgateway|Info|Description="England, UK"`, `ysfgateway|Info|Description="England, UK"`, `nxdngateway|Info|Description="England, UK"`}},
		{"manual URL", map[string]string{"urlAuto": "man", "confURL": "HTTP://Example.com/Me_"}, []string{
			"ircddbgateway||url=http://example.com/me", "mmdvmhost|Info|URL=http://example.com/me", "ysf2dmr|Info|URL=http://example.com/me",
			"dmrgateway|Info|URL=http://example.com/me"}},
		{"empty URL saves nothing even with Auto", map[string]string{"confURL": ""}, []string{}},
		{"APRS host", map[string]string{"selectedAPRSHost": "noam.aprs2.net"}, []string{
			"ircddbgateway||aprsHostname=noam.aprs2.net", "ysfgateway|aprs.fi|Server=noam.aprs2.net", "ysf2dmr|aprs.fi|Server=noam.aprs2.net",
			"aprsgateway|APRS-IS|Server=noam.aprs2.net"}},
		{"DMR ID keeps 9 digits", map[string]string{"dmrId": "234567801"}, []string{
			"mmdvmhost|General|Id=234567801", "mmdvmhost|DMR|Id=234567801", "ysfgateway|General|Id=234567801", "dmrgateway|XLX Network|Id=234567801",
			"dmr2ysf|DMR Network|Id=234567801", "dgidgateway|General|Id=234567801"}},
		{"private node", map[string]string{"nodeMode": "prv"}, []string{
			"mmdvmhost|DMR|SelfOnly=1", "mmdvmhost|D-Star|SelfOnly=1", "mmdvmhost|System Fusion|SelfOnly=1", "mmdvmhost|P25|SelfOnly=1",
			"mmdvmhost|NXDN|SelfOnly=1", "dstarrepeater||restriction=1"}},
		{"access list", map[string]string{"confDMRWhiteList": "1234567, 7654321"}, []string{"mmdvmhost|DMR|WhiteList=1234567,7654321"}},
		{"hardware DV-Mega", map[string]string{"confHardware": "dvmpis"}, []string{
			"dstarrepeater||modemType=DVMEGA", "system:modem=map[Hardware:dvmpis]"}},
		{"hardware USB stick", map[string]string{"confHardware": "zumspotusb"}, []string{
			"mmdvmhost|Modem|Port=/dev/ttyACM0", "mmdvmhost|Modem|UARTPort=/dev/ttyACM0", "system:modem=map[Hardware:zumspotusb]"}},
		{"hardware ID-RP2C", map[string]string{"confHardware": "idrp2c"}, []string{
			"ircddbgateway||repeaterType1=1", "ircddbgateway||repeaterAddress1=172.16.0.1", "ircddbgateway||repeaterPort1=20000",
			"system:modem=map[Hardware:idrp2c]"}},
		{"hardware Repeater Builder v1/v2 duplex", map[string]string{"confHardware": "rptrbldrv1v2dup"}, []string{
			"mmdvmhost|General|Duplex=1", "mmdvmhost|Modem|UARTSpeed=115200", "system:modem=map[Hardware:rptrbldrv1v2dup]"}},
		{"hardware none selected", map[string]string{"confHardware": ""}, []string{}},
		{"hostname", map[string]string{"confHostame": "Pi-Star2"}, []string{"system:hostname=pi-star2"}},
		{"time zone", map[string]string{"systemTimezone": "UTC"}, []string{"system:timezone=UTC"}},
		{"fields not posted are left alone", map[string]string{"confFREQ": ctlDrop, "nodeMode": ctlDrop, "trxMode": ctlDrop,
			"confHardware": ctlDrop, "selectedAPRSHost": ctlDrop, "systemTimezone": ctlDrop, "confHostame": ctlDrop}, []string{}},
	}
	for _, tc := range cases {
		ch, err := ctlRun(ctlBase(), tc.edits)
		ctlCheck(t, tc.name, ch, err, tc.want)
	}

	// Rejected input: nothing is planned.
	for name, edits := range map[string]map[string]string{
		"callsign symbols": {"confCallsign": "M0-ABC"}, "callsign too long": {"confCallsign": "M0ABCDEF"},
		"frequency": {"confFREQ": "4.3.8"}, "zero frequency": {"confFREQ": "0"}, "module suffix": {"confDStarModuleSuffix": "AB"},
		"latitude range": {"confLatitude": "91"}, "longitude text": {"confLongitude": "west"},
		"town only symbols": {"confDesc1": "!!!"}, "url mode": {"urlAuto": "both"},
		"APRS host": {"selectedAPRSHost": "evil.example.com"}, "DMR ID": {"dmrId": "12345678901"}, "DMR ID letters": {"dmrId": "12a"},
		"hardware": {"confHardware": "dvmpicast"}, "node type": {"nodeMode": "open"},
		"hostname": {"confHostame": "-bad"}, "hostname length": {"confHostame": "a234567890123456"}, "hostname dot": {"confHostame": "pi.local"},
		"time zone": {"systemTimezone": "Mars/Base"}, "access list": {"confDMRWhiteList": strings.Repeat("1234567,", 7)},
	} {
		ch, err := ctlRun(ctlBase(), edits)
		if !ctlIsCfgError(err) || len(ch.Changes) != 0 || len(ch.System) != 0 {
			t.Fatalf("%s: %v %+v", name, err, ch)
		}
	}

	// Duplex repeater: offset, RPT suffixes and ysf2dmr TX = TX.
	st := ctlBase()
	st.Files["mmdvmhost"]["Info"]["TXFrequency"] = "431200000"
	st.System.Markers["mmdvmhost"] = ctlMarker("mmdvmrpthat")
	ch, err := ctlRun(st, map[string]string{"confFREQrx": "430.6", "confFREQtx": "438.2", "trxMode": "DUPLEX"})
	ctlCheck(t, "duplex", ch, err, []string{
		"mmdvmhost|Info|RXFrequency=430600000", "mmdvmhost|Info|TXFrequency=438200000",
		"dmrgateway|Info|RXFrequency=430600000", "dmrgateway|Info|TXFrequency=438200000",
		"ysfgateway|Info|RXFrequency=430600000", "ysfgateway|Info|TXFrequency=438200000", "ysfgateway|General|Suffix=RPT",
		"ysf2dmr|Info|RXFrequency=430600000", "ysf2dmr|Info|TXFrequency=438200000", "ysf2dmr|YSF Network|Suffix=RPT",
		"dgidgateway|Info|RXFrequency=430600000", "dgidgateway|Info|TXFrequency=438200000", "dgidgateway|General|Suffix=RPT",
		"dmr2ysf|YSF Network|Suffix=RPT",
		"nxdngateway|Info|RXFrequency=430600000", "nxdngateway|Info|TXFrequency=438200000", "nxdngateway|General|Suffix=RPT",
		"m17gateway|Info|RXFrequency=430600000", "m17gateway|Info|TXFrequency=438200000", "m17gateway|General|Suffix=R",
		"ircddbgateway||frequency1=438.20000", "ircddbgateway||gatewayType=0", "ircddbgateway||offset1=-7.6000",
		"dstarrepeater||dvapFrequency=430600000", "dstarrepeater||dvmegaRXFrequency=430600000", "dstarrepeater||dvmegaTXFrequency=438200000",
		"dstarrepeater||mode=0", "mmdvmhost|General|Duplex=1", "mmdvmhost|DMR Network|Slot1=1"})
	// Duplex -> simplex with both rows posted: RX follows TX.
	ch, err = ctlRun(st, map[string]string{"trxMode": "SIMPLEX"})
	// The duplex hat (mmdvmrpthat) forces Duplex=1 after trxMode, as in the reference.
	ctlCheck(t, "back to simplex", ch, err, []string{
		"mmdvmhost|Info|RXFrequency=431200000", "mmdvmhost|General|Duplex=1", "dmrgateway|Info|TXFrequency=431200000", "ysfgateway|Info|TXFrequency=431200000",
		"ysfgateway|General|Suffix=RPT", "ysf2dmr|Info|TXFrequency=431200000", "ysf2dmr|YSF Network|Suffix=RPT",
		"dgidgateway|Info|TXFrequency=431200000", "dgidgateway|General|Suffix=RPT", "dmr2ysf|YSF Network|Suffix=RPT",
		"nxdngateway|Info|TXFrequency=431200000", "nxdngateway|General|Suffix=RPT", "m17gateway|Info|TXFrequency=431200000",
		"m17gateway|General|Suffix=R", "ircddbgateway||frequency1=431.20000", "ircddbgateway||gatewayType=0", "ircddbgateway||offset1=7.6000",
		"dstarrepeater||dvmegaTXFrequency=431200000", "dstarrepeater||mode=0"})

	// Out-of-band TX without a module suffix leaves the D-Star module alone.
	ch, err = ctlRun(ctlBase(), map[string]string{"confFREQ": "900"})
	if err != nil || ctlHas(ch, "dstarrepeater||callsign=") || ctlHas(ch, "ircddbgateway||repeaterBand1=") || ctlHas(ch, "mmdvmhost|D-Star|Module=") ||
		!ctlHas(ch, "timeserver||sendB=0") {
		t.Fatalf("out of band: %v %v", err, ctlList(ch))
	}

	// Pi-Star before 4.1.4: no APRSGateway callsign, ircDDBGateway keeps its aprsPassword.
	ctlPistarVersion = func() string { return "4.1.2" }
	st = ctlBase()
	st.Files["ircddbgateway"][""]["aprsPassword"] = ctlPass
	st.Layout["ircddbgateway"][0].Keys = append(st.Layout["ircddbgateway"][0].Keys, [2]string{"aprsPassword", ctlPass})
	ch, err = ctlRun(st, map[string]string{"confCallsign": "M0XYZ", "selectedAPRSHost": "noam.aprs2.net"})
	if err != nil || !ctlHas(ch, "ircddbgateway||aprsPassword="+newPass) || ctlHas(ch, "aprsgateway|General|Callsign=") || ctlHas(ch, "aprsgateway|APRS-IS|Server=") {
		t.Fatalf("old release: %v %v", err, ctlList(ch))
	}
	ctlPistarVersion = func() string { return "4.10" }
	ch, err = ctlRun(st, nil)
	ctlCheck(t, "4.1.4+ deletes aprsPassword", ch, err, []string{"ircddbgateway||aprsPassword<del>"})

	// Missing files are never created; flat keys absent from a file are skipped.
	files := ctlBaseFiles()
	for _, f := range []string{"ysfgateway", "dmrgateway", "ysf2dmr", "dgidgateway", "nxdngateway", "m17gateway", "aprsgateway", "timeserver"} {
		delete(files, f)
	}
	files["starnetserver"] = "callsign=M0ABC\n"
	st = ctlState(files, map[string]*string{"mmdvmhost": ctlMarker("zumspotgpio")})
	ch, err = ctlRun(st, map[string]string{"confCallsign": "M0XYZ"})
	for _, l := range ctlList(ch) {
		file := strings.SplitN(l, "|", 2)[0]
		if st.Files[file] == nil || strings.HasPrefix(l, "starnetserver||ircddbUsername") {
			t.Fatalf("wrote %s", l)
		}
	}
	if err != nil {
		t.Fatal(err)
	}

	// Access list: blank removes it; hidden (private node) it is kept.
	st = ctlBase()
	st.Files["mmdvmhost"]["DMR"]["WhiteList"] = "1234567"
	ch, err = ctlRun(st, map[string]string{"confDMRWhiteList": ""})
	ctlCheck(t, "blank access list", ch, err, []string{"mmdvmhost|DMR|WhiteList<del>"})
	st.Files["mmdvmhost"]["DMR"]["SelfOnly"] = "1"
	for _, s := range []string{"D-Star", "System Fusion", "P25", "NXDN"} {
		st.Files["mmdvmhost"][s]["SelfOnly"] = "1"
	}
	st.Files["dstarrepeater"][""]["restriction"] = "1"
	ch, err = ctlRun(st, map[string]string{"confDMRWhiteList": ""})
	ctlCheck(t, "hidden access list", ch, err, nil)

	// NXDN ID.
	st = ctlBase()
	st.Files["mmdvmhost"]["NXDN"]["Enable"] = "1"
	st.Files["mmdvmhost"]["NXDN Network"]["Enable"] = "1"
	ch, err = ctlRun(st, map[string]string{"nxdnId": "65535"})
	ctlCheck(t, "NXDN ID", ch, err, []string{"mmdvmhost|NXDN|Id=65535"})
	for _, bad := range []string{"65536", "0", "1x"} {
		if _, err := ctlRun(st, map[string]string{"nxdnId": bad}); !ctlIsCfgError(err) {
			t.Fatalf("NXDN ID %q: %v", bad, err)
		}
	}
	// Hidden IDs are ignored.
	ch, err = ctlRun(ctlBase(), map[string]string{"nxdnId": "1"})
	ctlCheck(t, "hidden NXDN ID", ch, err, nil)

	// DStarRepeater mode: hardware and node type still write, MMDVMHost fields do not.
	st = ctlBase()
	st.System.Markers = map[string]*string{"dstarrepeater": ctlMarker("dvap")}
	ch, err = ctlRun(st, map[string]string{"controllerSoft": "DSTAR", "MMDVMModeDMR": "OFF", "dmrRfHangTime": "1", "dmrId": "1"})
	ctlCheck(t, "dstarrepeater", ch, err, []string{
		"dstarrepeater||modemType=DVAP", "mmdvmhost|Modem|Port=/dev/ttyUSB0", "mmdvmhost|Modem|UARTPort=/dev/ttyUSB0"})
}

func ctlHas(ch configChange, prefix string) bool {
	for _, l := range ctlList(ch) {
		if strings.HasPrefix(l, prefix) {
			return true
		}
	}
	return false
}

// ctlApplyState simulates the helper writing a change, then re-reading.
func ctlApplyState(st configState, ch configChange) configState {
	files := map[string]string{}
	for name := range st.Files {
		var b strings.Builder
		for _, sec := range st.Layout[name] {
			if sec.Name != "" {
				b.WriteString("[" + sec.Name + "]\n")
			}
			for _, kv := range sec.Keys {
				v, changed := ch.Changes[name][sec.Name][kv[0]]
				switch {
				case !changed:
					b.WriteString(kv[0] + "=" + kv[1] + "\n")
				case v != nil:
					b.WriteString(kv[0] + "=" + *v + "\n")
				}
			}
			for k, v := range ch.Changes[name][sec.Name] {
				if _, ok := st.Files[name][sec.Name][k]; !ok && v != nil {
					b.WriteString(k + "=" + *v + "\n")
				}
			}
		}
		for sec, keys := range ch.Changes[name] {
			if _, ok := st.Files[name][sec]; !ok {
				b.WriteString("[" + sec + "]\n")
				for k, v := range keys {
					if v != nil {
						b.WriteString(k + "=" + *v + "\n")
					}
				}
			}
		}
		files[name] = b.String()
	}
	markers := map[string]*string{}
	for k, v := range st.System.Markers {
		markers[k] = v
	}
	if c, ok := ch.System["controller"].(string); ok {
		var content *string
		for _, v := range markers {
			if v != nil {
				content = v
			}
		}
		if content == nil {
			empty := ""
			content = &empty
		}
		markers = map[string]*string{c: content}
	}
	if m, ok := ch.System["modem"].(map[string]string); ok {
		for k, v := range markers {
			if v != nil {
				markers[k] = ctlMarker(m["Hardware"])
			}
		}
	}
	out := ctlState(files, markers)
	out.System.Hostname, out.System.Timezone = st.System.Hostname, st.System.Timezone
	if h, ok := ch.System["hostname"].(string); ok {
		out.System.Hostname = h
	}
	if tz, ok := ch.System["timezone"].(string); ok {
		out.System.Timezone = tz
	}
	return out
}

// Applying a form and submitting the reloaded page again changes nothing.
// Switching a simplex node to duplex takes one extra round, as in the
// reference: the first Apply only moves RX 1 Hz below TX, the reloaded page
// then shows both frequency rows and their Apply syncs the other files.
func TestCtlApplyIsIdempotent(t *testing.T) {
	ctlEnv(t)
	for _, tc := range []struct {
		edits  map[string]string
		rounds int
	}{
		{map[string]string{"confCallsign": "M0XYZ", "confFREQ": "145.5", "trxMode": "SIMPLEX", "confHardware": "dvmpid"}, 0},
		{map[string]string{"trxMode": "DUPLEX", "confHardware": "mmdvmrpthat", "MMDVMModeDMR2YSF": "ON", "mmdvmDisplayType": "OLED6", "mmdvmDisplayPort": "ttyUSB0"}, 1},
		{map[string]string{"controllerSoft": "DSTAR", "nodeMode": "prv", "confHostame": "hotspot", "systemTimezone": "UTC", "confLatitude": "0", "urlAuto": "man"}, 0},
	} {
		ch, err := ctlRun(ctlBase(), tc.edits)
		if err != nil {
			t.Fatal(err)
		}
		st := ctlApplyState(ctlBase(), ch)
		for i := 0; i < tc.rounds; i++ {
			ch, err := ctlRun(st, nil)
			if err != nil || len(ch.Changes) == 0 {
				t.Fatalf("round %d after %v: %v %v", i, tc.edits, err, ctlList(ch))
			}
			st = ctlApplyState(st, ch)
		}
		again, err := ctlRun(st, nil)
		ctlCheck(t, fmt.Sprintf("second apply after %v", tc.edits), again, err, nil)
	}
}

func TestCtlHelpers(t *testing.T) {
	for call, want := range map[string]string{"N0CALL": "13023", "n0call-9": "13023", "": "29666"} {
		if got := ctlAPRSPass(call); got != want {
			t.Fatalf("%q: %s", call, got)
		}
	}
	for _, tc := range []struct {
		v    string
		want bool
	}{{"4.1.4", true}, {"4.1.3", false}, {"4.10", true}, {"4.2", true}, {"10.0", true}, {"", false}, {"4.1", false}} {
		if ctlVersionAtLeast(tc.v, "4.1.4") != tc.want {
			t.Fatalf("%q", tc.v)
		}
	}
	for _, tc := range []struct {
		raw string
		hz  int64
		ok  bool
	}{{"438.800", 438800000, true}, {"438.8 MHz", 438800000, true}, {"1296.123456", 1296123456, true}, {"50", 50000000, true},
		{"145.1234567", 0, false}, {"", 0, false}, {"0.000", 0, false}} {
		hz, err := ctlParseFreq("F", tc.raw)
		if (err == nil) != tc.ok || hz != tc.hz {
			t.Fatalf("%q: %d %v", tc.raw, hz, err)
		}
	}
	if ctlIRCFreq(438123456) != "438.12346" || ctlOffset(430600000, 438200000) != "-7.6000" || ctlOffset(5, 0) != "0.0000" ||
		ctlOffset(1000050, 0) != "1.0001" {
		t.Fatal("number_format")
	}
	for tx, want := range map[int64]string{1296000000: "A", 438800000: "B", 222000000: "A", 145500000: "C", 900000000: "", 148000001: ""} {
		if ctlBand(tx) != want {
			t.Fatalf("%d", tx)
		}
	}
}

// A file without [Info] frequencies displays 0.000000; an untouched Apply
// must not fail on it (regression from the stand's minimal MMDVMHost.ini).
func TestCtlUnsetFrequencyUntouched(t *testing.T) {
	c := &cfgContext{St: configState{Files: map[string]map[string]map[string]string{"mmdvmhost": {"General": {"Duplex": "0"}}}}}
	p := &cfgPlan{ctx: c}
	if err := ctlApplyFrequency(ctlWriter{c, p}, cfgForm{"confFREQ": ctlMHz("")}); err != nil || p.change.Changes != nil {
		t.Fatalf("%v %+v", err, p.change)
	}
	if err := ctlApplyFrequency(ctlWriter{c, p}, cfgForm{"confFREQ": "0"}); err == nil {
		t.Fatal("an explicit zero frequency must still be refused")
	}
}
