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

const dmrTestHostsFile = `# DMR_Hosts.txt test fixture
# Name   Id   Address   Password   Port
DMRGateway	0000	127.0.0.1	none	62031
DMR2YSF	0000	127.0.0.2	none	62033
DMR2NXDN	0000	127.0.0.3	none	62035
BM_2341_United_Kingdom	2341	44.131.4.1	passw0rd	62031
BM_2621_Germany	2621	2.3.4.5	passw0rd	62031
BM_2341_United_Kingdom	2341	44.131.4.1	passw0rd	62031
DMR+_IPSC2-QUADNET	0000	89.185.97.38	PASSWORD	55555
DMR+_IPSC2-EU	0000	5.6.7.8	PASSWORD	55555
TGIF_Network	0000	tgif.network	passw0rd	62031
FreeDMR_UK	0000	freedmr.uk	passw0rd	62031
HB_Test	0000	hb.example	pw	62031
FD_Test	0000	fd.example	pw	62031
FreeSTAR_Test	0000	fs.example	pw	62031
XLX_950	0000	1.1.1.1	passw0rd	62030
XLX_999	0000	2.2.2.2	passw0rd	62030
  Indented	0000	9.9.9.1	pw	62031
Bad#Name	0000	9.9.9.2	pw	62031

Comma	0000	9.9.9.3	a,b$c	62031
`

// dmrTestState is a saved state after a classic Apply with the BrandMeister
// UK master selected directly; mods set ("file|section|key" -> value) or
// delete (value "\x00") keys, and "marker" replaces the modem marker text.
func dmrTestState(t *testing.T, mods map[string]string) *cfgContext {
	t.Helper()
	files := map[string]map[string]map[string]string{
		"mmdvmhost": {
			"General": {"Id": "2345678"},
			"DMR":     {"Enable": "1", "Id": "234567801", "ColorCode": "1", "EmbeddedLCOnly": "0", "DumpTAData": "0"},
			"DMR Network": {"Enable": "1", "Address": "44.131.4.1", "RemoteAddress": "44.131.4.1", "Port": "62031",
				"RemotePort": "62031", "Password": `"passw0rd"`, "Type": "Direct"},
		},
		"dmrgateway": {
			"DMR Network 1": {"Enabled": "1", "Address": "44.131.4.1", "Port": "62031", "Password": `"passw0rd"`, "Name": "BM_2341_United_Kingdom", "Id": "234567801"},
			"DMR Network 2": {"Enabled": "0", "Address": "89.185.97.38", "Port": "55555", "Password": `"PASSWORD"`, "Name": "DMR+_IPSC2-QUADNET", "Id": "2345678"},
			"XLX Network":   {"Enabled": "0", "Startup": "950", "TG": "6"},
		},
		"ysf2dmr":  {"DMR Network": {"Id": "2345678"}},
		"dmr2ysf":  {"DMR Network": {"LocalAddress": "127.0.0.1"}},
		"dmr2nxdn": {"DMR Network": {"LocalAddress": "127.0.0.1"}},
	}
	marker := "[Modem]\nHardware=zumspotusb\n"
	for k, v := range mods {
		if k == "marker" {
			marker = v
			continue
		}
		p := strings.SplitN(k, "|", 3)
		if len(p) == 1 { // a whole file is dropped below
			continue
		}
		if files[p[0]] == nil {
			files[p[0]] = map[string]map[string]string{}
		}
		if files[p[0]][p[1]] == nil {
			files[p[0]][p[1]] = map[string]string{}
		}
		if v == "\x00" {
			delete(files[p[0]][p[1]], p[2])
		} else {
			files[p[0]][p[1]][p[2]] = v
		}
	}
	for k := range mods {
		if !strings.Contains(k, "|") && k != "marker" {
			delete(files, k)
		}
	}
	var st configState
	st.Revision = "rev"
	st.Files = files
	st.System.Markers = map[string]*string{"mmdvmhost": &marker, "dstarrepeater": nil}
	return &cfgContext{St: st}
}

// Saved states for each master type, as a classic Apply leaves them.
var dmrModes = map[string]map[string]string{
	"bm": {},
	"gateway": {"mmdvmhost|DMR Network|Address": "127.0.0.1", "mmdvmhost|DMR Network|RemoteAddress": "127.0.0.1",
		"mmdvmhost|DMR Network|Type": "Gateway", "mmdvmhost|DMR Network|Local": "62032", "mmdvmhost|DMR Network|LocalPort": "62032",
		"dmrgateway|DMR Network 2|Options": `"StartRef=4000;RelinkTime=15;"`, "ysf2dmr|DMR Network|Local": "62032"},
	"dmrplus": {"mmdvmhost|DMR Network|Address": "89.185.97.38", "mmdvmhost|DMR Network|RemoteAddress": "89.185.97.38",
		"mmdvmhost|DMR Network|Port": "55555", "mmdvmhost|DMR Network|RemotePort": "55555",
		"mmdvmhost|DMR Network|Options": `"StartRef=4000;"`, "dmrgateway|DMR Network 2|Options": `"StartRef=4000;"`},
	"freedmr": {"mmdvmhost|DMR Network|Address": "freedmr.uk", "mmdvmhost|DMR Network|RemoteAddress": "freedmr.uk",
		"mmdvmhost|DMR Network|Options": `"TS2=2350;"`, "dmrgateway|DMR Network 2|Options": `"TS2=2350;"`},
	"hb":       {"mmdvmhost|DMR Network|Address": "hb.example", "mmdvmhost|DMR Network|RemoteAddress": "hb.example"},
	"fd":       {"mmdvmhost|DMR Network|Address": "fd.example", "mmdvmhost|DMR Network|RemoteAddress": "fd.example"},
	"freestar": {"mmdvmhost|DMR Network|Address": "fs.example", "mmdvmhost|DMR Network|RemoteAddress": "fs.example"},
	"tgif": {"mmdvmhost|DMR Network|Address": "tgif.network", "mmdvmhost|DMR Network|RemoteAddress": "tgif.network",
		"marker": "[Modem]\nHardware=zumspotusb\n\n[TGIF]\nPassword=\"tgifpw\"\n", "mmdvmhost|DMR Network|Password": `"tgifpw"`},
	"dmr2ysf": {"mmdvmhost|DMR Network|Address": "127.0.0.2", "mmdvmhost|DMR Network|RemoteAddress": "127.0.0.2",
		"mmdvmhost|DMR Network|Port": "62033", "mmdvmhost|DMR Network|RemotePort": "62033",
		"mmdvmhost|DMR Network|Local": "62034", "mmdvmhost|DMR Network|LocalPort": "62034", "dmr2ysf|DMR Network|LocalAddress": "127.0.0.2"},
	"dmr2nxdn": {"mmdvmhost|DMR Network|Address": "127.0.0.3", "mmdvmhost|DMR Network|RemoteAddress": "127.0.0.3",
		"mmdvmhost|DMR Network|Port": "62035", "mmdvmhost|DMR Network|RemotePort": "62035",
		"mmdvmhost|DMR Network|Local": "62036", "mmdvmhost|DMR Network|LocalPort": "62036", "dmr2nxdn|DMR Network|LocalAddress": "127.0.0.3"},
	"nomatch": {"mmdvmhost|DMR Network|Address": "10.0.0.1", "mmdvmhost|DMR Network|RemoteAddress": "10.0.0.1"},
}

func dmrMode(t *testing.T, mode string, extra map[string]string) *cfgContext {
	t.Helper()
	mods := map[string]string{}
	for k, v := range dmrModes[mode] {
		mods[k] = v
	}
	for k, v := range extra {
		mods[k] = v
	}
	return dmrTestState(t, mods)
}

func dmrSetup(t *testing.T) {
	t.Helper()
	savedDir, savedSections := gospotHostsDir, cfgSections
	t.Cleanup(func() { gospotHostsDir, cfgSections = savedDir, savedSections })
	gospotHostsDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(gospotHostsDir, "DMR_Hosts.txt"), []byte(dmrTestHostsFile), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgSections = nil
	registerCfgSection(cfgSectionImpl{ID: "dmr", Order: 50, Build: buildDMRSection, Apply: applyDMRSection})
}

func dmrRowIDs(s *cfgSection) []string {
	var ids []string
	for _, r := range s.Rows[1:] {
		ids = append(ids, r.ID)
	}
	return ids
}

func dmrFindRow(t *testing.T, s *cfgSection, id string) cfgRow {
	t.Helper()
	for _, r := range s.Rows {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("row %q not rendered; rows %v", id, dmrRowIDs(s))
	return cfgRow{}
}

func dmrControl(t *testing.T, s *cfgSection, name string) cfgControl {
	t.Helper()
	for _, r := range s.Rows {
		for _, c := range r.Cells {
			for _, ctl := range c.Controls {
				if ctl.Name == name {
					return ctl
				}
			}
		}
	}
	t.Fatalf("control %q not rendered", name)
	return cfgControl{}
}

// dmrForm is what the page posts when nothing is edited.
func dmrForm(s *cfgSection) cfgForm {
	f := cfgForm{}
	for _, r := range s.Rows {
		for _, c := range r.Cells {
			for _, ctl := range c.Controls {
				if ctl.Name != "" && ctl.Kind != "static" && ctl.Kind != "link" {
					if _, dup := f[ctl.Name]; !dup {
						f[ctl.Name] = ctl.Value
					}
				}
			}
		}
	}
	return f
}

func dmrFlatten(ch configChange) []string {
	var out []string
	for file, sections := range ch.Changes {
		for section, keys := range sections {
			for key, v := range keys {
				val := "<del>"
				if v != nil {
					val = *v
				}
				out = append(out, file+"|"+section+"|"+key+"="+val)
			}
		}
	}
	if m, ok := ch.System["modem_ini"].(map[string]map[string]*string); ok {
		for section, keys := range m {
			for key, v := range keys {
				val := "<del>"
				if v != nil {
					val = *v
				}
				out = append(out, "modem|"+section+"|"+key+"="+val)
			}
		}
	}
	sort.Strings(out)
	return out
}

func TestDMRVisibility(t *testing.T) {
	dmrSetup(t)
	other := "[Modem]\n"
	cases := []struct {
		name string
		ctx  func() *cfgContext
		want bool
	}{
		{"mmdvmhost controller, DMR on", func() *cfgContext { return dmrTestState(t, nil) }, true},
		{"DMR mode off", func() *cfgContext { return dmrTestState(t, map[string]string{"mmdvmhost|DMR|Enable": "0"}) }, false},
		{"DMR Enable missing", func() *cfgContext { return dmrTestState(t, map[string]string{"mmdvmhost|DMR|Enable": "\x00"}) }, false},
		{"no mmdvmhost file", func() *cfgContext { return dmrTestState(t, map[string]string{"mmdvmhost": ""}) }, false},
		{"D-Star Repeater controller", func() *cfgContext {
			c := dmrTestState(t, nil)
			c.St.System.Markers = map[string]*string{"mmdvmhost": nil, "dstarrepeater": &other}
			return c
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := tc.ctx()
			got := buildDMRSection(c) != nil
			if got != tc.want {
				t.Fatalf("visible = %v, want %v", got, tc.want)
			}
			// A hidden section changes nothing, whatever is posted.
			if !tc.want {
				ch, err := planCfgRaw(c, "rev", cfgForm{"dmrColorCode": "5", "dmrGatewayNet1En": "OFF"})
				if err != nil || len(ch.Changes) != 0 || len(ch.System) != 0 {
					t.Fatalf("hidden section applied: %v %v", dmrFlatten(ch), err)
				}
			}
		})
	}
}

func TestDMRHostListParsing(t *testing.T) {
	dmrSetup(t)
	s := buildDMRSection(dmrTestState(t, nil))
	var got []string
	for _, o := range dmrControl(t, s, "dmrMasterHost").Options {
		got = append(got, o.Label+"="+o.Value)
	}
	want := []string{
		"DMRGateway=127.0.0.1,none,62031,DMRGateway",
		"DMR2YSF=127.0.0.2,none,62033,DMR2YSF",
		"DMR2NXDN=127.0.0.3,none,62035,DMR2NXDN",
		"BM_2341_United_Kingdom=44.131.4.1,passw0rd,62031,BM_2341_United_Kingdom",
		"BM_2621_Germany=2.3.4.5,passw0rd,62031,BM_2621_Germany",
		"DMR+_IPSC2-QUADNET=89.185.97.38,PASSWORD,55555,DMR+_IPSC2-QUADNET",
		"DMR+_IPSC2-EU=5.6.7.8,PASSWORD,55555,DMR+_IPSC2-EU",
		"TGIF_Network=tgif.network,passw0rd,62031,TGIF_Network",
		"FreeDMR_UK=freedmr.uk,passw0rd,62031,FreeDMR_UK",
		"HB_Test=hb.example,pw,62031,HB_Test",
		"FD_Test=fd.example,pw,62031,FD_Test",
		"FreeSTAR_Test=fs.example,pw,62031,FreeSTAR_Test",
		"Comma=9.9.9.3,a,b$c,62031,Comma",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DMR Master options\n got %v\nwant %v", got, want)
	}
	// Short lines read missing columns as "".
	if err := os.WriteFile(filepath.Join(gospotHostsDir, "DMR_Hosts.txt"), []byte("Short\nBM_X 1 1.2.3.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s = buildDMRSection(dmrTestState(t, nil))
	if o := dmrControl(t, s, "dmrMasterHost").Options; len(o) != 2 || o[0].Value != ",,,Short" || o[1].Value != "1.2.3.4,,,BM_X" {
		t.Fatalf("short lines: %v", o)
	}
	// No host file: empty select, nothing selected.
	os.Remove(filepath.Join(gospotHostsDir, "DMR_Hosts.txt"))
	s = buildDMRSection(dmrTestState(t, nil))
	if ctl := dmrControl(t, s, "dmrMasterHost"); len(ctl.Options) != 0 || ctl.Value != "" {
		t.Fatalf("missing host file: %+v", ctl)
	}
}

func TestDMRBuildRowsByMaster(t *testing.T) {
	dmrSetup(t)
	tail := []string{"dmrColorCode", "dmrEmbeddedLCOnly", "dmrDumpTAData"}
	gateway := []string{"dmrMasterHost", "dmrMasterHost1", "bmHSSecurity", "bmExtendedId", "dmrGatewayNet1En", "bmLinks",
		"dmrMasterHost2", "dmrNetworkOptions", "dmrPlusExtendedId", "dmrGatewayNet2En", "dmrMasterHost3", "dmrMasterHost3StartupModule", "dmrGatewayXlxEn"}
	cases := []struct {
		mode  string
		extra map[string]string
		rows  []string
	}{
		{"gateway", nil, append(gateway, tail...)},
		{"gateway", map[string]string{"dmrgateway|XLX Network|TG": "\x00"},
			append(append([]string{}, gateway[:11]...), append([]string{"dmrGatewayXlxEn"}, tail...)...)},
		{"gateway", map[string]string{"dmrgateway|XLX Network 1|Startup": "4001"},
			append(append([]string{}, gateway[:11]...), append([]string{"dmrMasterHost3Startup", "dmrMasterHost3StartupModule", "dmrGatewayXlxEn"}, tail...)...)},
		{"bm", nil, append([]string{"dmrMasterHost", "bmHSSecurity", "bmLinks", "dmrExtendedId"}, tail...)},
		{"freedmr", nil, append([]string{"dmrMasterHost", "dmrNetworkOptions", "freeDMRHelp", "dmrExtendedId"}, tail...)},
		{"dmrplus", nil, append([]string{"dmrMasterHost", "dmrNetworkOptions", "dmrExtendedId"}, tail...)},
		{"hb", nil, append([]string{"dmrMasterHost", "dmrNetworkOptions", "dmrExtendedId"}, tail...)},
		{"fd", nil, append([]string{"dmrMasterHost", "dmrNetworkOptions", "dmrExtendedId"}, tail...)},
		{"freestar", nil, append([]string{"dmrMasterHost", "dmrNetworkOptions", "dmrExtendedId"}, tail...)},
		{"tgif", nil, append([]string{"dmrMasterHost", "tgifHSSecurity", "dmrExtendedId"}, tail...)},
		{"dmr2ysf", nil, append([]string{"dmrMasterHost", "dmrExtendedId"}, tail...)},
		{"dmr2nxdn", nil, append([]string{"dmrMasterHost", "dmrExtendedId"}, tail...)},
		{"nomatch", nil, append([]string{"dmrMasterHost", "dmrExtendedId"}, tail...)},
	}
	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			s := buildDMRSection(dmrMode(t, tc.mode, tc.extra))
			if s.ID != "dmr" || s.Title != "DMR Configuration" || !s.Apply || s.Form != "" {
				t.Fatalf("section header %+v", s)
			}
			if got := dmrRowIDs(s); !reflect.DeepEqual(got, tc.rows) {
				t.Fatalf("rows\n got %v\nwant %v", got, tc.rows)
			}
			for _, r := range s.Rows {
				for _, c := range r.Cells {
					for _, ctl := range c.Controls {
						if ctl.Kind == "hidden" || ctl.Name == "dmrDMRnetJitterBufer" {
							t.Fatalf("unexpected control %+v", ctl)
						}
					}
				}
			}
		})
	}
}

func TestDMRBuildLabelsAndTooltips(t *testing.T) {
	dmrSetup(t)
	custom := "Override the Password for your DMR Host with your own custom password, make sure you already configured this with your chosen DMR Host too. Empty the field to use the default."
	type lt struct{ id, label, title, body string }
	cases := map[string][]lt{
		"gateway": {
			{"dmrMasterHost", "DMR Master:", "DMR Master (MMDVMHost)", "Set your prefered DMR master here"},
			{"dmrMasterHost1", "BrandMeister Master:", "BrandMeister Master", "Set your prefered DMR master here"},
			{"bmHSSecurity", "BM Hotspot Security:", "BrandMeister Password", "Override the Password for BrandMeister with your own custom password, make sure you already configured this using BM Self Care. Empty the field to use the default."},
			{"bmExtendedId", "BrandMeister Network ESSID:", "BrandMeister Extended ID", "This is the extended ID, to make your DMR ID 9 digits long"},
			{"dmrGatewayNet1En", "BrandMeister Network Enable:", "BrandMeister Network Enable", "Enable or disable BrandMeister Network"},
			{"bmLinks", "BrandMeister Network:", "BrandMeister Dashboards", "Direct links to your BrandMeister Dashboards"},
			{"dmrMasterHost2", "DMR+ Master:", "DMR+ Master", "Set your prefered DMR master here"},
			{"dmrNetworkOptions", "DMR+ Network:", "DMR+ Network", "Set your options= for DMR+ here"},
			{"dmrPlusExtendedId", "DMR+ Network ESSID:", "DMR Plus Extended ID", "This is the extended ID, to make your DMR ID 8 digits long"},
			{"dmrGatewayNet2En", "DMR+ Network Enable:", "DMR+ Network Enable", ""},
			{"dmrMasterHost3", "XLX Master:", "XLX Master", "Set your prefered XLX master here"},
			{"dmrMasterHost3StartupModule", "XLX Startup Module:", "XLX Startup Module override", "Default will use the host file option, or override it here."},
			{"dmrGatewayXlxEn", "XLX Master Enable:", "XLX Master Enable", "Turn your XLX connection on or off."},
			{"dmrColorCode", "DMR Colour Code:", "DMR Color Code", "Set your DMR Color Code here"},
			{"dmrEmbeddedLCOnly", "DMR EmbeddedLCOnly:", "DMR EmbeddedLCOnly", "Turn ON to disable extended message support, including GPS and Talker Alias data. This can help reduce problems with some DMR Radios that do not support such features."},
			{"dmrDumpTAData", "DMR DumpTAData:", "DMR DumpTAData", "Turn ON to dump GPS and Talker Alias data to MMDVMHost log file."},
		},
		"bm": {
			{"bmHSSecurity", "Hotspot Security:", "Custom Password", custom},
			{"bmLinks", "BrandMeister Network:", "BrandMeister Dashboards", "Direct links to your BrandMeister Dashboards"},
			{"dmrExtendedId", "DMR ESSID:", "DMR Extended ID", "This is the extended ID, to make your DMR ID 8 or 9 digits long"},
		},
		"freedmr": {
			{"dmrNetworkOptions", "DMR Options:", "DMR Network", "Set your options= for DMR here"},
			{"freeDMRHelp", "FreeDMR Help:", "FreeDMR Help", "FreeDMR Options Help"},
		},
		"hb":   {{"dmrNetworkOptions", "DMR Options:", "DMR Network", "Set your options= for DMR here"}},
		"tgif": {{"tgifHSSecurity", "Hotspot Security:", "Custom Password", custom}},
	}
	for mode, rows := range cases {
		s := buildDMRSection(dmrMode(t, mode, map[string]string{"dmrgateway|XLX Network 1|Startup": "4001"}))
		for _, want := range rows {
			r := dmrFindRow(t, s, want.id)
			c := r.Cells[0]
			if len(r.Cells) != 2 || c.Label != want.label || c.Tooltip == nil || c.Tooltip.Title != want.title || c.Tooltip.Body != want.body {
				t.Errorf("%s/%s: label %q tooltip %+v", mode, want.id, c.Label, c.Tooltip)
			}
		}
	}
	s := buildDMRSection(dmrMode(t, "gateway", map[string]string{"dmrgateway|XLX Network 1|Startup": "4001"}))
	if c := dmrFindRow(t, s, "dmrMasterHost3Startup").Cells[0]; c.Label != "XLX Startup TG:" || c.Tooltip.Title != "XLX Startup TG" || c.Tooltip.Body != "" {
		t.Errorf("XLX Startup TG label %+v", c)
	}
	head := s.Rows[0].Cells
	if len(head) != 2 || !head[0].Header || head[0].Label != "Setting" || head[0].Tooltip.Title != "Setting" ||
		head[1].Label != "Value" || head[1].Tooltip.Body != "The current value from the\nconfiguration files" {
		t.Errorf("header %+v", head)
	}
}

func TestDMRBuildValues(t *testing.T) {
	dmrSetup(t)
	essid := func(extra ...string) []string {
		out := []string{"None"}
		out = append(out, extra...)
		for i := 1; i <= 99; i++ {
			s := string(rune('0'+i/10)) + string(rune('0'+i%10))
			out = append(out, s)
		}
		return out
	}
	values := func(o []linkOption) []string {
		var v []string
		for _, x := range o {
			v = append(v, x.Value)
		}
		return v
	}
	cases := []struct {
		name    string
		mode    string
		extra   map[string]string
		control string
		kind    string
		value   string
		options []string // nil: not checked
		attrs   map[string]string
	}{
		{"master selected", "bm", nil, "dmrMasterHost", "select", "44.131.4.1,passw0rd,62031,BM_2341_United_Kingdom", nil, nil},
		{"master no match selects nothing", "nomatch", nil, "dmrMasterHost", "select", "", nil, nil},
		{"master port must match too", "bm", map[string]string{"mmdvmhost|DMR Network|Port": "62032"}, "dmrMasterHost", "select", "", nil, nil},
		{"BM password from marker", "bm", map[string]string{"marker": "[BrandMeister]\nPassword=\"s3cret\"\n"}, "bmHSSecurity", "password", "s3cret", nil, map[string]string{"size": "30", "maxlength": "30"}},
		{"BM password missing", "bm", nil, "bmHSSecurity", "password", "", nil, nil},
		{"TGIF password from marker", "tgif", nil, "tgifHSSecurity", "password", "tgifpw", nil, map[string]string{"size": "30", "maxlength": "30"}},
		{"DMR ESSID 2 digits", "bm", nil, "dmrExtendedId", "select", "01", essid(), nil},
		{"DMR ESSID none", "bm", map[string]string{"mmdvmhost|DMR|Id": "2345678"}, "dmrExtendedId", "select", "None", essid(), nil},
		{"DMR ESSID missing Id", "bm", map[string]string{"mmdvmhost|DMR|Id": "\x00"}, "dmrExtendedId", "select", "None", essid(), nil},
		{"DMR ESSID 1 digit kept (fix)", "bm", map[string]string{"mmdvmhost|DMR|Id": "23456785"}, "dmrExtendedId", "select", "5", essid("5"), nil},
		{"BM ESSID from Net1 Id", "gateway", nil, "bmExtendedId", "select", "01", essid(), nil},
		{"BM ESSID Net1 Id short", "gateway", map[string]string{"dmrgateway|DMR Network 1|Id": "2345678"}, "bmExtendedId", "select", "None", essid(), nil},
		{"BM ESSID from 9-digit General Id", "gateway", map[string]string{"dmrgateway|DMR Network 1|Id": "\x00", "mmdvmhost|General|Id": "234567889"}, "bmExtendedId", "select", "89", essid(), nil},
		{"BM ESSID no Ids", "gateway", map[string]string{"dmrgateway|DMR Network 1|Id": "\x00"}, "bmExtendedId", "select", "None", essid(), nil},
		{"DMR+ ESSID", "gateway", map[string]string{"dmrgateway|DMR Network 2|Id": "234567842"}, "dmrPlusExtendedId", "select", "42", essid(), nil},
		{"BM master in gateway", "gateway", nil, "dmrMasterHost1", "select", "44.131.4.1,passw0rd,62031,BM_2341_United_Kingdom",
			[]string{"44.131.4.1,passw0rd,62031,BM_2341_United_Kingdom", "2.3.4.5,passw0rd,62031,BM_2621_Germany"}, nil},
		{"DMR+ master in gateway", "gateway", nil, "dmrMasterHost2", "select", "89.185.97.38,PASSWORD,55555,DMR+_IPSC2-QUADNET",
			[]string{"89.185.97.38,PASSWORD,55555,DMR+_IPSC2-QUADNET", "5.6.7.8,PASSWORD,55555,DMR+_IPSC2-EU"}, nil},
		{"DMR+ master unknown", "gateway", map[string]string{"dmrgateway|DMR Network 2|Address": "7.7.7.7"}, "dmrMasterHost2", "select", "", nil, nil},
		{"DMR+ options from dmrgateway", "gateway", nil, "dmrNetworkOptions", "text", "StartRef=4000;RelinkTime=15;", nil, map[string]string{"size": "65", "maxlength": "250"}},
		{"DMR options from mmdvmhost", "freedmr", nil, "dmrNetworkOptions", "text", "TS2=2350;", nil, map[string]string{"size": "65", "maxlength": "250"}},
		{"XLX master by Startup", "gateway", nil, "dmrMasterHost3", "select", "1.1.1.1,passw0rd,62030,XLX_950",
			[]string{"1.1.1.1,passw0rd,62030,XLX_950", "2.2.2.2,passw0rd,62030,XLX_999"}, nil},
		{"XLX master by legacy address", "gateway", map[string]string{"dmrgateway|XLX Network|Startup": "\x00", "dmrgateway|XLX Network 1|Address": "2.2.2.2"},
			"dmrMasterHost3", "select", "2.2.2.2,passw0rd,62030,XLX_999", nil, nil},
		{"XLX master unset", "gateway", map[string]string{"dmrgateway|XLX Network|Startup": "\x00"}, "dmrMasterHost3", "select", "", nil, nil},
		{"XLX Startup TG", "gateway", map[string]string{"dmrgateway|XLX Network 1|Startup": "4012"}, "dmrMasterHost3Startup", "select", "4012",
			[]string{"None", "4001", "4002", "4003", "4004", "4005", "4006", "4007", "4008", "4009", "4010", "4011", "4012", "4013",
				"4014", "4015", "4016", "4017", "4018", "4019", "4020", "4021", "4022", "4023", "4024", "4025", "4026"}, nil},
		{"XLX module not set", "gateway", nil, "dmrMasterHost3StartupModule", "select", "Default",
			append([]string{"Default", "@"}, strings.Split("A B C D E F G H I J K L M N O P Q R S T U V W X Y Z", " ")...), nil},
		{"XLX module None", "gateway", map[string]string{"dmrgateway|XLX Network|Module": "@"}, "dmrMasterHost3StartupModule", "select", "@",
			append([]string{"Default", "@"}, strings.Split("A B C D E F G H I J K L M N O P Q R S T U V W X Y Z", " ")...), nil},
		{"XLX module set, listed once", "gateway", map[string]string{"dmrgateway|XLX Network|Module": "C"}, "dmrMasterHost3StartupModule", "select", "C",
			append([]string{"C", "Default", "@"}, strings.Split("A B D E F G H I J K L M N O P Q R S T U V W X Y Z", " ")...), nil},
		{"Net1 enabled", "gateway", nil, "dmrGatewayNet1En", "toggle", "ON", nil, map[string]string{"label": "Enable BrandMeister DMR"}},
		{"Net2 disabled", "gateway", nil, "dmrGatewayNet2En", "toggle", "OFF", nil, map[string]string{"label": "Enable DMR+"}},
		{"XLX disabled", "gateway", nil, "dmrGatewayXlxEn", "toggle", "OFF", nil, map[string]string{"label": "Enable XLX Network"}},
		{"XLX enabled", "gateway", map[string]string{"dmrgateway|XLX Network|Enabled": "1"}, "dmrGatewayXlxEn", "toggle", "ON", nil, nil},
		{"XLX enabled by legacy section", "gateway", map[string]string{"dmrgateway|XLX Network 1|Enabled": "1"}, "dmrGatewayXlxEn", "toggle", "ON", nil, nil},
		{"colour code", "bm", nil, "dmrColorCode", "select", "1",
			[]string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15"}, nil},
		{"colour code missing shows 0", "bm", map[string]string{"mmdvmhost|DMR|ColorCode": "\x00"}, "dmrColorCode", "select", "0", nil, nil},
		{"colour code 07", "bm", map[string]string{"mmdvmhost|DMR|ColorCode": "07"}, "dmrColorCode", "select", "7", nil, nil},
		{"colour code invalid", "bm", map[string]string{"mmdvmhost|DMR|ColorCode": "abc"}, "dmrColorCode", "select", "", nil, nil},
		{"embedded LC on", "bm", map[string]string{"mmdvmhost|DMR|EmbeddedLCOnly": "1"}, "dmrEmbeddedLCOnly", "toggle", "ON", nil, map[string]string{"label": "Enable DMR Embedded LC Only"}},
		{"embedded LC off", "bm", nil, "dmrEmbeddedLCOnly", "toggle", "OFF", nil, nil},
		{"dump TA on", "bm", map[string]string{"mmdvmhost|DMR|DumpTAData": "1"}, "dmrDumpTAData", "toggle", "ON", nil, map[string]string{"label": "DMR Dump TA Data"}},
		{"dump TA missing", "bm", map[string]string{"mmdvmhost|DMR|DumpTAData": "\x00"}, "dmrDumpTAData", "toggle", "OFF", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctl := dmrControl(t, buildDMRSection(dmrMode(t, tc.mode, tc.extra)), tc.control)
			if ctl.Kind != tc.kind || ctl.Value != tc.value {
				t.Fatalf("kind %q value %q, want %q %q", ctl.Kind, ctl.Value, tc.kind, tc.value)
			}
			if tc.options != nil && !reflect.DeepEqual(values(ctl.Options), tc.options) {
				t.Fatalf("options\n got %v\nwant %v", values(ctl.Options), tc.options)
			}
			for k, v := range tc.attrs {
				if ctl.Attrs[k] != v {
					t.Fatalf("attr %s = %q, want %q", k, ctl.Attrs[k], v)
				}
			}
		})
	}
}

func TestDMRBuildStaticAndLinks(t *testing.T) {
	dmrSetup(t)
	texts := func(c cfgCell) []string {
		var out []string
		for _, ctl := range c.Controls {
			out = append(out, ctl.Kind+":"+ctl.Text+"@"+ctl.Attrs["href"])
		}
		return out
	}
	cases := []struct {
		name, mode string
		extra      map[string]string
		row        string
		want       []string
	}{
		{"BM links from Net1 Id", "gateway", nil, "bmLinks", []string{
			"link:Device Information@https://brandmeister.network/#/device/234567801", "static: | @",
			"link:Edit Device (BrandMeister Selfcare)@https://brandmeister.network/#/device/234567801"}},
		{"BM links fall back to General Id", "gateway", map[string]string{"dmrgateway|DMR Network 1|Id": "\x00"}, "bmLinks", []string{
			"link:Device Information@https://brandmeister.network/#/device/2345678", "static: | @",
			"link:Edit Device (BrandMeister Selfcare)@https://brandmeister.network/#/device/2345678"}},
		{"BM direct links from DMR Id", "bm", nil, "bmLinks", []string{
			"link:Device Information@https://brandmeister.network/#/device/234567801", "static: | @",
			"link:Edit Device (BrandMeister Selfcare)@https://brandmeister.network/#/device/234567801"}},
		{"FreeDMR help", "freedmr", nil, "freeDMRHelp", []string{
			"link:FreeDMR Options Guide@http://www.freedmr.uk/index.php/static-talk-groups-pi-star/", "static: | @",
			"link:FreeDMR Options Calculator@http://www.freedmr.uk/index.php/dashboard/options-calculator/"}},
		{"ESSID prefix", "bm", nil, "dmrExtendedId", []string{"static:2345678@", "select:@"}},
		{"ESSID prefix of a 9-digit Id", "bm", map[string]string{"mmdvmhost|General|Id": "234567889"}, "dmrExtendedId", []string{"static:2345678@", "select:@"}},
		{"no prefix for placeholder Id", "bm", map[string]string{"mmdvmhost|General|Id": "1234567", "mmdvmhost|DMR|Id": "1234567"}, "dmrExtendedId", []string{"select:@"}},
		{"Options prefix", "hb", nil, "dmrNetworkOptions", []string{"static:Options=@", "text:@"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := dmrFindRow(t, buildDMRSection(dmrMode(t, tc.mode, tc.extra)), tc.row)
			got := texts(r.Cells[1])
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("cell\n got %v\nwant %v", got, tc.want)
			}
			for _, ctl := range r.Cells[1].Controls {
				if ctl.Kind == "link" && (ctl.Attrs["target"] != "_new" || ctl.Attrs["style"] != "color: #000;") {
					t.Fatalf("link attrs %v", ctl.Attrs)
				}
			}
		})
	}
}

func TestDMRUnchangedFormWritesNothing(t *testing.T) {
	dmrSetup(t)
	cases := map[string]map[string]string{
		"bm":                 nil,
		"bm with password":   {"marker": "[BrandMeister]\nPassword=\"s3cret\"\n", "mmdvmhost|DMR Network|Password": `"s3cret"`},
		"gateway":            nil,
		"gateway module":     {"dmrgateway|XLX Network|Module": "C"},
		"gateway legacy XLX": {"dmrgateway|XLX Network 1|Startup": "4005", "dmrgateway|XLX Network 1|Enabled": "0"},
		"dmrplus":            nil,
		"freedmr":            nil,
		"hb":                 nil,
		"fd":                 nil,
		"freestar":           nil,
		"tgif":               nil,
		"dmr2ysf":            nil,
		"dmr2nxdn":           nil,
		"nomatch":            nil,
		"8-digit DMR Id":     {"mmdvmhost|DMR|Id": "23456785"},
		"keys missing":       {"mmdvmhost|DMR|ColorCode": "\x00", "mmdvmhost|DMR|EmbeddedLCOnly": "\x00", "mmdvmhost|DMR|DumpTAData": "\x00"},
		"no ysf2dmr":         {"ysf2dmr": ""},
		"no dmrgateway":      {"dmrgateway": ""},
	}
	modes := map[string]string{"bm with password": "bm", "gateway module": "gateway", "gateway legacy XLX": "gateway",
		"8-digit DMR Id": "bm", "keys missing": "bm", "no ysf2dmr": "bm", "no dmrgateway": "gateway"}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			mode := modes[name]
			if mode == "" {
				mode = name
			}
			c := dmrMode(t, mode, extra)
			ch, err := planCfgRaw(c, "rev", dmrForm(buildDMRSection(c)))
			if err != nil {
				t.Fatal(err)
			}
			if len(ch.Changes) != 0 || len(ch.System) != 0 || len(ch.Restart) != 0 {
				t.Fatalf("unchanged form changed %v system %v", dmrFlatten(ch), ch.System)
			}
		})
	}
}

func TestDMRApplyChanges(t *testing.T) {
	dmrSetup(t)
	const (
		bmUK   = "44.131.4.1,passw0rd,62031,BM_2341_United_Kingdom"
		bmDE   = "2.3.4.5,passw0rd,62031,BM_2621_Germany"
		gwHost = "127.0.0.1,none,62031,DMRGateway"
		plusEU = "5.6.7.8,PASSWORD,55555,DMR+_IPSC2-EU"
	)
	cases := []struct {
		name  string
		mode  string
		extra map[string]string
		form  cfgForm // overrides of the unchanged form
		want  []string
	}{
		{"colour code 0 is saved (fix)", "bm", nil, cfgForm{"dmrColorCode": "0"}, []string{"mmdvmhost|DMR|ColorCode=0"}},
		{"colour code 15", "bm", nil, cfgForm{"dmrColorCode": "15"}, []string{"mmdvmhost|DMR|ColorCode=15"}},
		{"colour code added when edited", "bm", map[string]string{"mmdvmhost|DMR|ColorCode": "\x00"}, cfgForm{"dmrColorCode": "3"}, []string{"mmdvmhost|DMR|ColorCode=3"}},
		{"colour code normalised", "bm", map[string]string{"mmdvmhost|DMR|ColorCode": "07"}, nil, []string{"mmdvmhost|DMR|ColorCode=7"}},
		{"invalid colour code left alone", "bm", map[string]string{"mmdvmhost|DMR|ColorCode": "abc"}, nil, nil},
		{"toggles on", "bm", nil, cfgForm{"dmrEmbeddedLCOnly": "ON", "dmrDumpTAData": "ON"},
			[]string{"mmdvmhost|DMR|DumpTAData=1", "mmdvmhost|DMR|EmbeddedLCOnly=1"}},
		{"toggles off", "bm", map[string]string{"mmdvmhost|DMR|EmbeddedLCOnly": "1", "mmdvmhost|DMR|DumpTAData": "1"},
			cfgForm{"dmrEmbeddedLCOnly": "OFF", "dmrDumpTAData": "OFF"}, []string{"mmdvmhost|DMR|DumpTAData=0", "mmdvmhost|DMR|EmbeddedLCOnly=0"}},
		{"toggle absent from form is left alone", "bm", nil, cfgForm{"dmrEmbeddedLCOnly": "\x00"}, nil},
		{"toggle missing key added when switched on", "bm", map[string]string{"mmdvmhost|DMR|DumpTAData": "\x00"}, cfgForm{"dmrDumpTAData": "ON"}, []string{"mmdvmhost|DMR|DumpTAData=1"}},
		{"direct master ignores DMRGateway toggles (fix)", "bm", nil,
			cfgForm{"dmrGatewayNet1En": "OFF", "dmrGatewayNet2En": "OFF", "dmrGatewayXlxEn": "OFF", "dmrMasterHost1": bmDE, "bmExtendedId": "05"}, nil},
		{"JitterEnabled dropped", "bm", map[string]string{"mmdvmhost|DMR Network|JitterEnabled": "1"}, nil, []string{"mmdvmhost|DMR Network|JitterEnabled=<del>"}},
		{"DMR ESSID None", "bm", nil, cfgForm{"dmrExtendedId": "None"}, []string{"mmdvmhost|DMR|Id=2345678"}},
		{"DMR ESSID 42", "bm", nil, cfgForm{"dmrExtendedId": "42"}, []string{"mmdvmhost|DMR|Id=234567842"}},
		{"DMR ESSID kept for 8 digits (fix)", "bm", map[string]string{"mmdvmhost|DMR|Id": "23456785"}, cfgForm{"dmrExtendedId": "07"}, []string{"mmdvmhost|DMR|Id=234567807"}},
		{"BM password set", "bm", nil, cfgForm{"bmHSSecurity": "n3w pw"},
			[]string{"mmdvmhost|DMR Network|Password=\"n3w pw\"", "modem|BrandMeister|Password=\"n3w pw\""}},
		{"BM password cleared keeps host password", "bm", map[string]string{"marker": "[BrandMeister]\nPassword=\"s3cret\"\n", "mmdvmhost|DMR Network|Password": `"s3cret"`},
			cfgForm{"bmHSSecurity": ""}, []string{"modem|BrandMeister|Password=<del>"}},
		{"BM password restored into mmdvmhost", "bm", map[string]string{"marker": "[BrandMeister]\nPassword=\"s3cret\"\n"}, nil,
			[]string{"mmdvmhost|DMR Network|Password=\"s3cret\""}},
		{"missing mmdvmhost password from host file", "bm", map[string]string{"mmdvmhost|DMR Network|Password": "\x00"}, nil,
			[]string{"mmdvmhost|DMR Network|Password=\"passw0rd\""}},
		{"switch BM to BM keeps password", "bm", nil, cfgForm{"dmrMasterHost": bmDE},
			[]string{"mmdvmhost|DMR Network|Address=2.3.4.5", "mmdvmhost|DMR Network|RemoteAddress=2.3.4.5"}},
		{"BM clears Options, Local and ysf2dmr", "bm", map[string]string{"mmdvmhost|DMR Network|Options": `"x"`, "mmdvmhost|DMR Network|Local": "62032",
			"mmdvmhost|DMR Network|LocalPort": "62032", "dmrgateway|DMR Network 2|Options": `"y"`, "ysf2dmr|DMR Network|Options": `"z"`, "ysf2dmr|DMR Network|Local": "62032"}, nil,
			[]string{"dmrgateway|DMR Network 2|Options=<del>", "mmdvmhost|DMR Network|Local=<del>", "mmdvmhost|DMR Network|LocalPort=<del>",
				"mmdvmhost|DMR Network|Options=<del>", "ysf2dmr|DMR Network|Local=<del>", "ysf2dmr|DMR Network|Options=<del>"}},
		{"BM without ysf2dmr file", "bm", map[string]string{"ysf2dmr": "", "mmdvmhost|DMR Network|Local": "62032"}, nil,
			[]string{"mmdvmhost|DMR Network|Local=<del>"}},
		{"switch BM to DMRGateway", "bm", map[string]string{"dmr2ysf|DMR Network|LocalAddress": "127.0.0.2", "dmr2nxdn|DMR Network|LocalAddress": "127.0.0.3"},
			cfgForm{"dmrMasterHost": gwHost, "bmHSSecurity": "ignored"},
			[]string{"dmr2nxdn|DMR Network|LocalAddress=127.0.0.1", "dmr2ysf|DMR Network|LocalAddress=127.0.0.1",
				"mmdvmhost|DMR Network|Address=127.0.0.1", "mmdvmhost|DMR Network|Local=62032", "mmdvmhost|DMR Network|LocalPort=62032",
				"mmdvmhost|DMR Network|RemoteAddress=127.0.0.1", "mmdvmhost|DMR Network|Type=Gateway", "ysf2dmr|DMR Network|Local=62032"}},
		{"DMRGateway without dmr2ysf LocalAddress", "bm", map[string]string{"dmr2ysf|DMR Network|LocalAddress": "\x00", "dmr2nxdn": ""},
			cfgForm{"dmrMasterHost": gwHost},
			[]string{"mmdvmhost|DMR Network|Address=127.0.0.1", "mmdvmhost|DMR Network|Local=62032", "mmdvmhost|DMR Network|LocalPort=62032",
				"mmdvmhost|DMR Network|RemoteAddress=127.0.0.1", "mmdvmhost|DMR Network|Type=Gateway", "ysf2dmr|DMR Network|Local=62032"}},
		{"switch to DMR2YSF", "bm", nil, cfgForm{"dmrMasterHost": "127.0.0.2,none,62033,DMR2YSF"},
			[]string{"dmr2ysf|DMR Network|LocalAddress=127.0.0.2", "mmdvmhost|DMR Network|Address=127.0.0.2", "mmdvmhost|DMR Network|Local=62034",
				"mmdvmhost|DMR Network|LocalPort=62034", "mmdvmhost|DMR Network|Port=62033", "mmdvmhost|DMR Network|RemoteAddress=127.0.0.2",
				"mmdvmhost|DMR Network|RemotePort=62033"}},
		{"switch to DMR2NXDN", "bm", nil, cfgForm{"dmrMasterHost": "127.0.0.3,none,62035,DMR2NXDN"},
			[]string{"dmr2nxdn|DMR Network|LocalAddress=127.0.0.3", "mmdvmhost|DMR Network|Address=127.0.0.3", "mmdvmhost|DMR Network|Local=62036",
				"mmdvmhost|DMR Network|LocalPort=62036", "mmdvmhost|DMR Network|Port=62035", "mmdvmhost|DMR Network|RemoteAddress=127.0.0.3",
				"mmdvmhost|DMR Network|RemotePort=62035"}},
		{"switch to TGIF clears Options", "dmrplus", nil, cfgForm{"dmrMasterHost": "tgif.network,passw0rd,62031,TGIF_Network"},
			[]string{"mmdvmhost|DMR Network|Address=tgif.network", "mmdvmhost|DMR Network|Options=<del>", "mmdvmhost|DMR Network|Port=62031",
				"mmdvmhost|DMR Network|RemoteAddress=tgif.network", "mmdvmhost|DMR Network|RemotePort=62031"}},
		{"switch BM to DMR+ without Options", "bm", map[string]string{"mmdvmhost|DMR Network|Local": "1"}, cfgForm{"dmrMasterHost": plusEU},
			[]string{"mmdvmhost|DMR Network|Address=5.6.7.8", "mmdvmhost|DMR Network|Local=<del>", "mmdvmhost|DMR Network|Port=55555",
				"mmdvmhost|DMR Network|RemoteAddress=5.6.7.8", "mmdvmhost|DMR Network|RemotePort=55555"}},
		{"DMR+ Options edited", "dmrplus", nil, cfgForm{"dmrNetworkOptions": `Start"Ref=4001;`},
			[]string{"dmrgateway|DMR Network 2|Options=\"StartRef=4001;\"", "mmdvmhost|DMR Network|Options=\"StartRef=4001;\""}},
		{"DMR+ Options cleared", "dmrplus", map[string]string{"ysf2dmr|DMR Network|Options": `"z"`}, cfgForm{"dmrNetworkOptions": ""},
			[]string{"dmrgateway|DMR Network 2|Options=<del>", "mmdvmhost|DMR Network|Options=<del>", "ysf2dmr|DMR Network|Options=<del>"}},
		{"FreeDMR Options edited", "freedmr", nil, cfgForm{"dmrNetworkOptions": "TS2=2351;"},
			[]string{"dmrgateway|DMR Network 2|Options=\"TS2=2351;\"", "mmdvmhost|DMR Network|Options=\"TS2=2351;\""}},
		{"HB Options added", "hb", map[string]string{"ysf2dmr|DMR Network|Local": "62032"}, cfgForm{"dmrNetworkOptions": "A=1"},
			[]string{"dmrgateway|DMR Network 2|Options=\"A=1\"", "mmdvmhost|DMR Network|Options=\"A=1\"", "ysf2dmr|DMR Network|Local=<del>"}},
		{"TGIF password set", "tgif", nil, cfgForm{"tgifHSSecurity": "t2"},
			[]string{"mmdvmhost|DMR Network|Password=\"t2\"", "modem|TGIF|Password=\"t2\""}},
		{"TGIF password cleared", "tgif", nil, cfgForm{"tgifHSSecurity": ""}, []string{"modem|TGIF|Password=<del>"}},
		{"BM password not applied to TGIF master", "bm", nil, cfgForm{"dmrMasterHost": "tgif.network,passw0rd,62031,TGIF_Network", "bmHSSecurity": "x"},
			[]string{"mmdvmhost|DMR Network|Address=tgif.network", "mmdvmhost|DMR Network|RemoteAddress=tgif.network"}},
		{"empty master leaves it alone", "nomatch", map[string]string{"mmdvmhost|DMR Network|Options": `"x"`}, cfgForm{"dmrMasterHost": ""}, nil},
		{"pick master from no match", "nomatch", nil, cfgForm{"dmrMasterHost": bmUK},
			[]string{"mmdvmhost|DMR Network|Address=44.131.4.1", "mmdvmhost|DMR Network|RemoteAddress=44.131.4.1"}},
		{"host password with comma kept (fix)", "bm", map[string]string{"mmdvmhost|DMR Network|Password": "\x00"}, cfgForm{"dmrMasterHost": "9.9.9.3,a,b$c,62031,Comma"},
			[]string{"mmdvmhost|DMR Network|Address=9.9.9.3", "mmdvmhost|DMR Network|Password=\"a,b$c\"", "mmdvmhost|DMR Network|RemoteAddress=9.9.9.3"}},
		// DMRGateway mode.
		{"gateway BM master switched", "gateway", nil, cfgForm{"dmrMasterHost1": bmDE},
			[]string{"dmrgateway|DMR Network 1|Address=2.3.4.5", "dmrgateway|DMR Network 1|Name=BM_2621_Germany"}},
		{"gateway BM password", "gateway", nil, cfgForm{"bmHSSecurity": "gwpw"}, []string{"dmrgateway|DMR Network 1|Password=\"gwpw\""}},
		{"gateway BM password missing from host file", "gateway", map[string]string{"dmrgateway|DMR Network 1|Password": "\x00"}, nil,
			[]string{"dmrgateway|DMR Network 1|Password=\"passw0rd\""}},
		{"gateway BM ESSID", "gateway", nil, cfgForm{"bmExtendedId": "05"}, []string{"dmrgateway|DMR Network 1|Id=234567805"}},
		{"gateway DMR+ ESSID", "gateway", nil, cfgForm{"dmrPlusExtendedId": "09"}, []string{"dmrgateway|DMR Network 2|Id=234567809"}},
		{"gateway DMR+ master overwrites password", "gateway", map[string]string{"dmrgateway|DMR Network 2|Password": `"mine"`}, cfgForm{"dmrMasterHost2": plusEU},
			[]string{"dmrgateway|DMR Network 2|Address=5.6.7.8", "dmrgateway|DMR Network 2|Name=DMR+_IPSC2-EU", "dmrgateway|DMR Network 2|Password=\"PASSWORD\""}},
		{"gateway DMR+ Options edited", "gateway", nil, cfgForm{"dmrNetworkOptions": "StartRef=4002;"}, []string{"dmrgateway|DMR Network 2|Options=\"StartRef=4002;\""}},
		{"gateway DMR+ Options cleared", "gateway", nil, cfgForm{"dmrNetworkOptions": ""}, []string{"dmrgateway|DMR Network 2|Options=<del>"}},
		{"gateway Options kept with unknown DMR+ host (fix)", "gateway", map[string]string{"dmrgateway|DMR Network 2|Address": "7.7.7.7"}, nil, nil},
		{"gateway XLX master", "gateway", nil, cfgForm{"dmrMasterHost3": "2.2.2.2,passw0rd,62030,XLX_999"}, []string{"dmrgateway|XLX Network|Startup=999"}},
		{"gateway XLX module A", "gateway", nil, cfgForm{"dmrMasterHost3StartupModule": "A"}, []string{"dmrgateway|XLX Network|Module=A"}},
		{"gateway XLX module None writes @ (fix)", "gateway", nil, cfgForm{"dmrMasterHost3StartupModule": "@"}, []string{"dmrgateway|XLX Network|Module=@"}},
		{"gateway XLX module Default", "gateway", map[string]string{"dmrgateway|XLX Network|Module": "C"}, cfgForm{"dmrMasterHost3StartupModule": "Default"},
			[]string{"dmrgateway|XLX Network|Module=<del>"}},
		{"gateway legacy XLX Startup TG", "gateway", map[string]string{"dmrgateway|XLX Network 1|Startup": "4005"}, cfgForm{"dmrMasterHost3Startup": "4010"},
			[]string{"dmrgateway|XLX Network 1|Startup=4010"}},
		{"gateway legacy XLX Startup TG None", "gateway", map[string]string{"dmrgateway|XLX Network 1|Startup": "4005"}, cfgForm{"dmrMasterHost3Startup": "None"},
			[]string{"dmrgateway|XLX Network 1|Startup=<del>"}},
		{"gateway toggles", "gateway", nil, cfgForm{"dmrGatewayNet1En": "OFF", "dmrGatewayNet2En": "ON", "dmrGatewayXlxEn": "ON"},
			[]string{"dmrgateway|DMR Network 1|Enabled=0", "dmrgateway|DMR Network 2|Enabled=1", "dmrgateway|XLX Network|Enabled=1"}},
		{"gateway XLX toggle keeps legacy section in step", "gateway", map[string]string{"dmrgateway|XLX Network 1|Enabled": "1", "dmrgateway|XLX Network|Enabled": "1"},
			cfgForm{"dmrGatewayXlxEn": "OFF"}, []string{"dmrgateway|XLX Network 1|Enabled=0", "dmrgateway|XLX Network|Enabled=0"}},
		{"gateway to DMR+ direct keeps the new Options (fix)", "gateway", nil, cfgForm{"dmrMasterHost": plusEU},
			[]string{"dmrgateway|DMR Network 2|Options=\"StartRef=4000;RelinkTime=15;\"" /* re-written unchanged: dropped */, "mmdvmhost|DMR Network|Address=5.6.7.8",
				"mmdvmhost|DMR Network|Local=<del>", "mmdvmhost|DMR Network|LocalPort=<del>", "mmdvmhost|DMR Network|Options=\"StartRef=4000;RelinkTime=15;\"",
				"mmdvmhost|DMR Network|Port=55555", "mmdvmhost|DMR Network|RemoteAddress=5.6.7.8", "mmdvmhost|DMR Network|RemotePort=55555",
				"mmdvmhost|DMR Network|Type=Direct", "ysf2dmr|DMR Network|Local=<del>"}},
		{"gateway to BM direct with password", "gateway", nil, cfgForm{"dmrMasterHost": bmUK, "bmHSSecurity": "pw2"},
			[]string{"dmrgateway|DMR Network 1|Password=\"pw2\"", "mmdvmhost|DMR Network|Address=44.131.4.1",
				"mmdvmhost|DMR Network|Local=<del>", "mmdvmhost|DMR Network|LocalPort=<del>", "mmdvmhost|DMR Network|Password=\"pw2\"",
				"mmdvmhost|DMR Network|RemoteAddress=44.131.4.1", "mmdvmhost|DMR Network|Type=Direct", "modem|BrandMeister|Password=\"pw2\"",
				"ysf2dmr|DMR Network|Local=<del>"}},
		{"gateway without dmrgateway file", "gateway", map[string]string{"dmrgateway": ""},
			cfgForm{"dmrGatewayNet1En": "OFF", "bmExtendedId": "05", "dmrNetworkOptions": "x", "dmrMasterHost3StartupModule": "\x00"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := dmrMode(t, tc.mode, tc.extra)
			form := dmrForm(buildDMRSection(c))
			for k, v := range tc.form {
				if v == "\x00" {
					delete(form, k)
				} else {
					form[k] = v
				}
			}
			ch, err := planCfgRaw(c, "rev", form)
			if err != nil {
				t.Fatal(err)
			}
			want := append([]string{}, tc.want...)
			// A value re-written unchanged is dropped by the framework.
			for i := 0; i < len(want); i++ {
				p := strings.SplitN(want[i], "=", 2)
				fk := strings.SplitN(p[0], "|", 3)
				if fk[0] != "modem" && c.St.Files[fk[0]][fk[1]][fk[2]] == p[1] {
					want = append(want[:i], want[i+1:]...)
					i--
				}
			}
			sort.Strings(want)
			got := dmrFlatten(ch)
			if len(got) == 0 {
				got = nil
			}
			if len(want) == 0 {
				want = nil
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("changes\n got %v\nwant %v", got, want)
			}
			if (len(got) > 0) != (len(ch.Restart) > 0) {
				t.Fatalf("restart %v for changes %v", ch.Restart, got)
			}
			if _, ok := ch.System["modem"]; ok {
				t.Fatalf("passwords must not go to [Modem]: %v", ch.System)
			}
		})
	}
}

func TestDMRESSIDFollowsGeneralId(t *testing.T) {
	dmrSetup(t)
	// The General section (earlier on the page) changes [General] Id first.
	registerCfgSection(cfgSectionImpl{ID: "general-test", Order: 10,
		Build: func(c *cfgContext) *cfgSection { return &cfgSection{ID: "general-test"} },
		Apply: func(c *cfgContext, f cfgForm, p *cfgPlan) error {
			p.set("mmdvmhost", "General", "Id", "7654321")
			return nil
		}})
	c := dmrMode(t, "bm", nil)
	ch, err := planCfgRaw(c, "rev", dmrForm(buildDMRSection(c)))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"mmdvmhost|DMR|Id=765432101", "mmdvmhost|General|Id=7654321"}
	if got := dmrFlatten(ch); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestDMRModemMerge(t *testing.T) {
	dmrSetup(t)
	// Another section's marker changes are kept.
	registerCfgSection(cfgSectionImpl{ID: "other", Order: 10,
		Build: func(c *cfgContext) *cfgSection { return &cfgSection{ID: "other"} },
		Apply: func(c *cfgContext, f cfgForm, p *cfgPlan) error {
			v := `"keep"`
			p.system("modem_ini", map[string]map[string]*string{"Other": {"Key": &v}})
			return nil
		}})
	c := dmrMode(t, "bm", nil)
	form := dmrForm(buildDMRSection(c))
	form["bmHSSecurity"] = "pw"
	ch, err := planCfgRaw(c, "rev", form)
	if err != nil {
		t.Fatal(err)
	}
	m := ch.System["modem_ini"].(map[string]map[string]*string)
	if *m["Other"]["Key"] != `"keep"` || *m["BrandMeister"]["Password"] != `"pw"` {
		t.Fatalf("modem ops %v", m)
	}
}

func TestDMRApplyRejects(t *testing.T) {
	dmrSetup(t)
	long := strings.Repeat("x", 31)
	cases := []struct {
		name, mode string
		extra      map[string]string
		form       cfgForm
		msg        string
	}{
		{"unknown master", "bm", nil, cfgForm{"dmrMasterHost": "6.6.6.6,pw,62031,BM_Fake"}, "DMR Master: unknown host"},
		{"XLX host as main master", "bm", nil, cfgForm{"dmrMasterHost": "1.1.1.1,passw0rd,62030,XLX_950"}, "DMR Master: unknown host"},
		{"unknown BM master", "gateway", nil, cfgForm{"dmrMasterHost1": "89.185.97.38,PASSWORD,55555,DMR+_IPSC2-QUADNET"}, "BrandMeister Master: unknown host"},
		{"unknown DMR+ master", "gateway", nil, cfgForm{"dmrMasterHost2": "x"}, "DMR+ Master: unknown host"},
		{"unknown XLX master", "gateway", nil, cfgForm{"dmrMasterHost3": "x"}, "XLX Master: unknown host"},
		{"ESSID 100", "bm", nil, cfgForm{"dmrExtendedId": "100"}, "DMR ESSID: invalid value"},
		{"ESSID 00", "bm", nil, cfgForm{"dmrExtendedId": "00"}, "DMR ESSID: invalid value"},
		{"ESSID text", "bm", nil, cfgForm{"dmrExtendedId": "ab"}, "DMR ESSID: invalid value"},
		{"BM ESSID", "gateway", nil, cfgForm{"bmExtendedId": "1"}, "BrandMeister Network ESSID: invalid value"},
		{"DMR+ ESSID", "gateway", nil, cfgForm{"dmrPlusExtendedId": "x"}, "DMR+ Network ESSID: invalid value"},
		{"colour 16", "bm", nil, cfgForm{"dmrColorCode": "16"}, "DMR Colour Code"},
		{"colour -1", "bm", nil, cfgForm{"dmrColorCode": "-1"}, "DMR Colour Code"},
		{"colour 01", "bm", nil, cfgForm{"dmrColorCode": "01"}, "DMR Colour Code"},
		{"toggle value", "bm", nil, cfgForm{"dmrDumpTAData": "yes"}, "DMR DumpTAData: invalid value"},
		{"gateway toggle value", "gateway", nil, cfgForm{"dmrGatewayNet1En": "1"}, "BrandMeister Network Enable: invalid value"},
		{"password quote", "bm", nil, cfgForm{"bmHSSecurity": `a"b`}, "Hotspot Security: double quotes"},
		{"gateway password quote", "gateway", nil, cfgForm{"bmHSSecurity": `a"b`}, "BM Hotspot Security: double quotes"},
		{"password control", "tgif", nil, cfgForm{"tgifHSSecurity": "a\tb"}, "Hotspot Security: control characters"},
		{"password newline", "bm", nil, cfgForm{"bmHSSecurity": "a\nb"}, "Hotspot Security: control characters"},
		{"password too long", "bm", nil, cfgForm{"bmHSSecurity": long}, "Hotspot Security: at most 30"},
		{"options too long", "hb", nil, cfgForm{"dmrNetworkOptions": strings.Repeat("o", 251)}, "DMR Options: at most 250"},
		{"options newline", "gateway", nil, cfgForm{"dmrNetworkOptions": "a\nb"}, "DMR Options: control characters"},
		{"module", "gateway", nil, cfgForm{"dmrMasterHost3StartupModule": "AA"}, "XLX Startup Module: invalid value"},
		{"module space", "gateway", nil, cfgForm{"dmrMasterHost3StartupModule": " "}, "XLX Startup Module: invalid value"},
		{"startup TG", "gateway", map[string]string{"dmrgateway|XLX Network 1|Startup": "4001"}, cfgForm{"dmrMasterHost3Startup": "5000"}, "XLX Startup TG: invalid value"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := dmrMode(t, tc.mode, tc.extra)
			form := dmrForm(buildDMRSection(c))
			for k, v := range tc.form {
				form[k] = v
			}
			ch, err := planCfgRaw(c, "rev", form)
			var ce cfgError
			if !errors.As(err, &ce) || !strings.Contains(ce.Error(), tc.msg) {
				t.Fatalf("err %v, want cfgError containing %q", err, tc.msg)
			}
			if ch.Changes != nil || ch.System != nil {
				t.Fatalf("rejected apply returned changes %v", ch)
			}
			if strings.Contains(ce.Error(), "s3cret") || strings.Contains(ce.Error(), tc.form["bmHSSecurity"]+"x") {
				t.Fatalf("message leaks input: %q", ce)
			}
		})
	}
	// A stored password longer than 30 characters is accepted when unedited.
	c := dmrMode(t, "bm", map[string]string{"marker": "[BrandMeister]\nPassword=\"" + long + "\"\n", "mmdvmhost|DMR Network|Password": `"` + long + `"`})
	if ch, err := planCfgRaw(c, "rev", dmrForm(buildDMRSection(c))); err != nil || len(ch.Changes) != 0 {
		t.Fatalf("unedited long password: %v %v", dmrFlatten(ch), err)
	}
	// Fields of rows that are not rendered are not validated either.
	c = dmrMode(t, "bm", nil)
	form := dmrForm(buildDMRSection(c))
	form["dmrMasterHost1"], form["tgifHSSecurity"], form["dmrPlusExtendedId"] = "bogus", `"`, "bogus"
	if ch, err := planCfgRaw(c, "rev", form); err != nil || len(ch.Changes) != 0 {
		t.Fatalf("hidden fields: %v %v", dmrFlatten(ch), err)
	}
}
