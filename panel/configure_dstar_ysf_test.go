package main

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// Tests for configure_dstar_ysf.go (docs/configure-spec/03-dstar-ysf.md).

const dsyLog = "/var/log/pi-star"

func dsyEnv(t *testing.T) string {
	t.Helper()
	savedHosts, savedLog := pistarHostsDir, pistarLogDir
	t.Cleanup(func() { pistarHostsDir, pistarLogDir = savedHosts, savedLog })
	dir := t.TempDir()
	pistarHostsDir, pistarLogDir = dir, dsyLog
	files := map[string]string{
		"DCS_Hosts.txt":    "# DCS reflectors\nDCS001\tdcs001.xreflector.net\nDCS002\tdcs002.xreflector.net\nXLX000\txlx000.example\nDCS003\tXLX-linked.example\n",
		"DPlus_Hosts.txt":  "REF001\tref001.dstargateway.org\tL\nREF002\tref002.dstargateway.org\tL\nXRF757\txrf757.example\tL\n#REF999\tcommented\n",
		"DExtra_Hosts.txt": "XRF001\txrf001.example\nXRF757\txrf757.example\n",
		"YSFHosts.txt":     "12345;GB_TEST;Test room;1.2.3.4;42000;003;\n# comment;x;y\n54321;US_ROOM;America;5.6.7.8;42000;001;\n",
		"FCSHosts.txt":     "FCS00290;FCS Room 290;x\nXYZ01;not an FCS room\n",
		"DMR_Hosts.txt": "# Name Id Address Password Port\n" +
			"BM_2341_UK\t0000\t2341.master.brandmeister.network\tpassw0rd\t62031\n" +
			"BM_2342_UK\t0000\t2342.master.brandmeister.network\tpass2\t62032\n" +
			"XLX_123\t0000\txlx.example\tpassw0rd\t62030\n" +
			"DMRGateway\t0000\t127.0.0.1\tnone\t62031\n" +
			"DMR2YSF\t0000\t127.0.0.2\tnone\t62033\n",
		"NXDNHosts.txt":      "65000\tnxdn.example\t41400\n",
		"NXDNHostsLocal.txt": "31337\tlocal.example\t41400\n",
		"P25Hosts.txt":       "10100\tp25.example\t41000\n10200\tp25b.example\t41000\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func dsyStr(s string) *string { return &s }

// dsyRawState is a typical MMDVMHost hotspot with YSF, YSF2DMR, YSF2NXDN and
// YSF2P25 enabled; values are raw, as the helper reads them.
func dsyRawState() configState {
	var st configState
	st.Files = map[string]map[string]map[string]string{
		"mmdvmhost": {
			"General":               {"Callsign": "M1ABC", "Id": "1234501"},
			"Info":                  {"RXFrequency": "438800000", "TXFrequency": "438800000"},
			"D-Star":                {"Enable": "1", "Module": "B"},
			"System Fusion Network": {"Enable": "1", "LocalPort": "3200", "GatewayPort": "4200"},
		},
		"dstarrepeater": {"": {"callsign": "M1ABC  B", "gateway": "M1ABC  G", "beaconText": "M1ABC  B", "dvmegaTXFrequency": "438800000"}},
		"ircddbgateway": {"": {"gatewayCallsign": "M1ABC", "repeaterCall1": "M1ABC", "repeaterBand1": "B", "remotePassword": "raspberry",
			"remotePort": "10022", "reflector1": "REF001 C", "atStartup1": "1", "language": "0", "ircddbEnabled": "1",
			"ircddbHostname": "ircv4.openquad.net", "ccsEnabled": "0", "ccsHost": "CCS704"}},
		"timeserver": {"": {"callsign": "M1ABC", "sendA": "0", "sendB": "1", "sendC": "0", "sendD": "0", "sendE": "0", "language": "0"}},
		"ysfgateway": {
			"General":     {"Callsign": "M1ABC", "Suffix": "ND", "WiresXMakeUpper": "1", "WiresXCommandPassthrough": "0"},
			"Info":        {"Name": "M1ABC_Pi-Star"},
			"Network":     {"Startup": "GB_TEST", "DataPort": "4200"},
			"YSF Network": {"Port": "42000"},
			"FCS Network": {"Enable": "1"},
			"Mobile GPS":  {"Enable": "0", "Address": "127.0.0.1", "Port": "7834"},
			"aprs.fi":     {"Enable": "1"},
		},
		"ysf2dmr": {
			"Enabled":     {"Enabled": "1"},
			"Info":        {"Enabled": "0"},
			"DMR Network": {"Id": "123450101", "Address": "2341.master.brandmeister.network", "Port": "62031", "Password": `"secret"`, "StartupDstId": "31672"},
		},
		"ysf2nxdn":    {"Enabled": {"Enabled": "1"}, "NXDN Network": {"Id": "12345", "StartupDstId": "10"}},
		"ysf2p25":     {"Enabled": {"Enabled": "1"}, "P25 Network": {"Id": "1234501", "StartupDstId": "10200"}, "aprs.fi": {"Enable": "0"}},
		"p25gateway":  {"Network": {"Static": "10200"}},
		"nxdngateway": {"Network": {"Static": "10"}},
		"dgidgateway": {"General": {"Callsign": "M1ABC"}},
		"dmr2ysf":     {"Enabled": {"Enabled": "0"}, "DMR Network": {"DefaultDstTG": "12345"}},
	}
	st.System.Markers = map[string]*string{
		"mmdvmhost":          dsyStr("[Modem]\nHardware=zum\n\n[BrandMeister]\nPassword=\"secret\"\n"),
		"dstarrepeater":      nil,
		dsyMarkerTimeDisable: nil,
		dsyMarkerNoDExtra:    nil,
		dsyMarkerYSFUpper:    nil,
	}
	return st
}

func dsyClone(st configState) configState {
	out := st
	out.Files = map[string]map[string]map[string]string{}
	for f, secs := range st.Files {
		out.Files[f] = map[string]map[string]string{}
		for s, keys := range secs {
			out.Files[f][s] = map[string]string{}
			for k, v := range keys {
				out.Files[f][s][k] = v
			}
		}
	}
	out.System.Markers = map[string]*string{}
	for k, v := range st.System.Markers {
		out.System.Markers[k] = v
	}
	return out
}

// dsyCommit applies a change the way the helper does (per key).
func dsyCommit(st configState, ch configChange) configState {
	out := dsyClone(st)
	for f, secs := range ch.Changes {
		for s, keys := range secs {
			if out.Files[f][s] == nil {
				out.Files[f][s] = map[string]string{}
			}
			for k, v := range keys {
				if v == nil {
					delete(out.Files[f][s], k)
				} else {
					out.Files[f][s][k] = *v
				}
			}
		}
	}
	return out
}

func dsyCtx(st configState) *cfgContext { return &cfgContext{St: st, Now: time.Unix(0, 0)} }

// dsyApply runs this file's appliers (visible sections only) and drops
// no-op edits like planCfgApply. pre stages edits of earlier sections.
func dsyApply(c *cfgContext, form cfgForm, pre func(p *cfgPlan)) (configChange, error) {
	p := &cfgPlan{ctx: c}
	if pre != nil {
		pre(p)
	}
	if buildDStarSection(c) != nil {
		if err := applyDStarSection(c, form, p); err != nil {
			return configChange{}, err
		}
	}
	if buildYSFSection(c) != nil {
		if err := applyYSFSection(c, form, p); err != nil {
			return configChange{}, err
		}
	}
	for file, secs := range p.change.Changes {
		for sec, keys := range secs {
			for key, v := range keys {
				stored, present := c.St.Files[file][sec][key]
				if (v == nil && !present) || (v != nil && present && *v == stored) {
					delete(keys, key)
				}
			}
			if len(keys) == 0 {
				delete(secs, sec)
			}
		}
		if len(secs) == 0 {
			delete(p.change.Changes, file)
		}
	}
	return p.change, nil
}

// dsyFlatChanges renders changes as "file|section|key" -> value ("<del>").
func dsyFlatChanges(ch configChange) map[string]string {
	out := map[string]string{}
	for f, secs := range ch.Changes {
		for s, keys := range secs {
			for k, v := range keys {
				if v == nil {
					out[f+"|"+s+"|"+k] = "<del>"
				} else {
					out[f+"|"+s+"|"+k] = *v
				}
			}
		}
	}
	return out
}

// dsyForm is what the UI posts for unchanged sections: every named control
// with its built value (the first occurrence wins).
func dsyForm(c *cfgContext) cfgForm {
	f := cfgForm{}
	for _, sec := range []*cfgSection{buildDStarSection(c), buildYSFSection(c)} {
		if sec == nil {
			continue
		}
		for _, row := range sec.Rows {
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

// dsyBaseline is the raw fixture after one save (all defaults in place).
func dsyBaseline(t *testing.T) configState {
	t.Helper()
	raw := dsyRawState()
	c := dsyCtx(raw)
	form := dsyForm(c)
	form["confFREQ"] = "438.800000"
	ch, err := dsyApply(c, form, nil)
	if err != nil {
		t.Fatalf("first save: %v", err)
	}
	return dsyCommit(raw, ch)
}

func dsyRowOf(t *testing.T, sec *cfgSection, id string) cfgRow {
	t.Helper()
	for _, r := range sec.Rows {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("row %q not found", id)
	return cfgRow{}
}

func dsyControlOf(sec *cfgSection, name string) *cfgControl {
	for _, r := range sec.Rows {
		for _, cell := range r.Cells {
			for i := range cell.Controls {
				if cell.Controls[i].Name == name {
					return &cell.Controls[i]
				}
			}
		}
	}
	return nil
}

func dsyValues(opts []linkOption) []string {
	var out []string
	for _, o := range opts {
		out = append(out, o.Value)
	}
	return out
}

func TestDStarYSFRegistered(t *testing.T) {
	want := map[string]int{"dstar": 60, "ysf": 70}
	for _, s := range cfgSections {
		if o, ok := want[s.ID]; ok {
			if s.Order != o {
				t.Fatalf("%s order %d", s.ID, s.Order)
			}
			delete(want, s.ID)
		}
	}
	if len(want) > 0 {
		t.Fatalf("not registered: %v", want)
	}
}

func TestDStarYSFVisibility(t *testing.T) {
	dsyEnv(t)
	cases := []struct {
		name               string
		mutate             func(st *configState)
		dstar, ysf         bool
		ysf2dmr, nxdn, p25 bool
	}{
		{"mmdvmhost with D-Star and YSF", func(st *configState) {}, true, true, true, true, true},
		{"D-Star disabled", func(st *configState) { st.Files["mmdvmhost"]["D-Star"]["Enable"] = "0" }, false, true, true, true, true},
		{"DStarRepeater controller", func(st *configState) {
			st.Files["mmdvmhost"]["D-Star"]["Enable"] = "0"
			st.System.Markers["mmdvmhost"] = nil
			st.System.Markers["dstarrepeater"] = dsyStr("")
		}, true, false, false, false, false},
		{"YSF network off", func(st *configState) { st.Files["mmdvmhost"]["System Fusion Network"]["Enable"] = "0" }, true, false, false, false, false},
		{"YSF off but DMR2YSF on", func(st *configState) {
			st.Files["mmdvmhost"]["System Fusion Network"]["Enable"] = "0"
			st.Files["dmr2ysf"]["Enabled"]["Enabled"] = "1"
		}, true, true, true, true, true},
		{"cross modes off", func(st *configState) {
			st.Files["ysf2dmr"]["Enabled"]["Enabled"] = "0"
			st.Files["ysf2nxdn"]["Enabled"]["Enabled"] = "0"
			delete(st.Files, "ysf2p25")
		}, true, true, false, false, false},
		{"only YSF2NXDN", func(st *configState) {
			st.Files["ysf2dmr"]["Enabled"]["Enabled"] = "0"
			st.Files["ysf2p25"]["Enabled"]["Enabled"] = "0"
		}, true, true, false, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := dsyRawState()
			tc.mutate(&st)
			c := dsyCtx(st)
			ds, ysf := buildDStarSection(c), buildYSFSection(c)
			if (ds != nil) != tc.dstar || (ysf != nil) != tc.ysf {
				t.Fatalf("dstar %v ysf %v", ds != nil, ysf != nil)
			}
			if ysf == nil {
				return
			}
			got := [3]bool{dsyControlOf(ysf, "ysf2dmrTg") != nil, dsyControlOf(ysf, "ysf2nxdnId") != nil, dsyControlOf(ysf, "ysf2p25Id") != nil}
			if got != [3]bool{tc.ysf2dmr, tc.nxdn, tc.p25} {
				t.Fatalf("sub-blocks %v", got)
			}
		})
	}
}

func TestDStarYSFLabels(t *testing.T) {
	dsyEnv(t)
	c := dsyCtx(dsyRawState())
	ds, ysf := buildDStarSection(c), buildYSFSection(c)
	if ds.Title != "D-Star Configuration" || ysf.Title != "Yaesu System Fusion Configuration" || !ds.Apply || !ysf.Apply {
		t.Fatalf("titles %q %q", ds.Title, ysf.Title)
	}
	for _, sec := range []*cfgSection{ds, ysf} {
		h := sec.Rows[0].Cells
		if len(h) != 2 || !h[0].Header || h[0].Label != "Setting" || *h[0].Tooltip != (cfgTooltip{"Setting", ""}) ||
			h[1].Colspan != 2 || h[1].Label != "Value" || *h[1].Tooltip != (cfgTooltip{"Value", "The current value from the\nconfiguration files"}) {
			t.Fatalf("%s header %+v", sec.ID, h)
		}
	}
	rows := []struct {
		sec              *cfgSection
		id, label, title string
		body             string
	}{
		{ds, "confDStarModuleSuffix", "RPT1 Callsign:", "RPT1 Callsign", "This is the RPT1 field for your radio"},
		{ds, "rpt2", "RPT2 Callsign:", "RPT2 Callsign", "This is the RPT2 field for your radio"},
		{ds, "confPassword", "Remote Password:", "Remote Password", "Used for ircDDBGateway remote control access"},
		{ds, "confDefRef", "Default Reflector:", "Default Reflector", "Used for setting the default reflector."},
		{ds, "ircDDBGatewayAnnounceLanguage", "ircDDBGateway Language:", "ircDDBGateway Language", "Set your prefered language here"},
		{ds, "confTimeAnnounce", "Time Announcements:", "Time Announce", "Announce time hourly"},
		{ds, "confircddbEnabled", "Callsign Routing:", "Callsign Routing", "Do you want callsign routing for D-Star"},
		{ds, "confHostFilesNoDExtra", "Use DPlus for XRF:", "No DExtra", "Should host files use DPlus Protocol for XRFs"},
		{ysf, "ysfStartupHost", "YSF Startup Host:", "YSF Host", "Set your prefered YSF Host here"},
		{ysf, "confHostFilesYSFUpper", "UPPERCASE Hostfiles:", "UPPERCASE Hostfiles", "Should host files use UPPERCASE only - fixes issues with FT-70D radios."},
		{ysf, "wiresXCommandPassthrough", "WiresX Passthrough:", "WiresX Auto Passthrough", "Use this to automatically send WiresX commands through to YSF2xxx cross-over modes."},
		{ysf, "ysf2dmrId", "(YSF2DMR)CCS7/DMR ID:", "CCS7/DMR ID", "Enter your CCS7 / DMR ID here"},
		{ysf, "ysf2dmrMasterHost", "DMR Master:", "DMR Master (YSF2DMR)", "Set your prefered DMR master here"},
		{ysf, "ysf2dmrHSSecurity", "Hotspot Security:", "DMR Master Password", "Override the Password for DMR with your own custom password, make sure you already configured this on your chosed DMR Master. Empty the field to use the default."},
		{ysf, "ysf2dmrTg", "DMR TG:", "YSF2DMR TG", "Enter your DMR TG here"},
		{ysf, "ysf2nxdnId", "(YSF2NXDN) NXDN ID:", "NXDN ID", "Enter your NXDN ID here"},
		{ysf, "ysf2nxdnStartupDstId", "NXDN Startup Host:", "NXDN Host", "Set your prefered NXDN Host here"},
		{ysf, "ysf2p25Id", "(YSF2P25) CCS7/DMR ID:", "DMR ID", "Enter your CCS7 / DMR ID here"},
		{ysf, "ysf2p25StartupDstId", "P25 Startup Host:", "P25 Host", "Set your prefered P25 Host here"},
	}
	var order []string
	for _, r := range ysf.Rows[1:] {
		order = append(order, r.ID)
	}
	for _, r := range ds.Rows[1:] {
		order = append(order, r.ID)
	}
	var want []string
	for _, r := range rows[8:] {
		want = append(want, r.id)
	}
	for _, r := range rows[:8] {
		want = append(want, r.id)
	}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("row order %v", order)
	}
	for _, r := range rows {
		row := dsyRowOf(t, r.sec, r.id)
		cell := row.Cells[0]
		if cell.Label != r.label || cell.Tooltip == nil || cell.Tooltip.Title != r.title || cell.Tooltip.Body != r.body {
			t.Fatalf("%s: %+v %+v", r.id, cell, cell.Tooltip)
		}
	}
	// Second cells: the notes next to the two-cell toggles.
	for id, note := range map[string]string{"confircddbEnabled": "Connect ircDDB for call routing", "confHostFilesNoDExtra": "Note: Update Required if changed"} {
		if got := dsyRowOf(t, ds, id).Cells[2].Controls[0]; got.Kind != "static" || got.Text != note {
			t.Fatalf("%s note %+v", id, got)
		}
	}
	if got := dsyRowOf(t, ysf, "confHostFilesYSFUpper").Cells[2].Controls[0].Text; got != "Note: Update Required if changed" {
		t.Fatalf("ysf upper note %q", got)
	}
}

func TestDStarBuildValues(t *testing.T) {
	dsyEnv(t)
	st := dsyRawState()
	c := dsyCtx(st)
	ds := buildDStarSection(c)
	rpt1 := dsyRowOf(t, ds, "confDStarModuleSuffix").Cells[1]
	if rpt1.Colspan != 2 || rpt1.Controls[0].Text != "M1ABC  " {
		t.Fatalf("RPT1 text %+v", rpt1)
	}
	wantSuffix := strings.Split("B A C D E F G H I J K L M N O P Q R S T U V W X Y Z", " ")
	suffix := dsyControlOf(ds, "confDStarModuleSuffix")
	if suffix.Value != "B" || !reflect.DeepEqual(dsyValues(suffix.Options), wantSuffix) {
		t.Fatalf("suffix %+v", suffix)
	}
	if got := dsyRowOf(t, ds, "rpt2").Cells[1].Controls[0].Text; got != "M1ABC  G" {
		t.Fatalf("RPT2 %q", got)
	}
	if pw := dsyControlOf(ds, "confPassword"); pw.Kind != "password" || pw.Value != "raspberry" || pw.Attrs["maxlength"] != "30" || pw.Attrs["size"] != "30" {
		t.Fatalf("password %+v", pw)
	}
	ref := dsyControlOf(ds, "confDefRef")
	wantRef := []string{"REF001", "customOption", "DCS001", "DCS002", "XLX000", "DCS003", "REF002", "XRF757", "XRF001"}
	if ref.Value != "REF001" || !reflect.DeepEqual(dsyValues(ref.Options), wantRef) || ref.Options[1].Label != "Text Entry" {
		t.Fatalf("reflector %+v", ref)
	}
	if ltr := dsyControlOf(ds, "confDefRefLtr"); ltr.Value != "C" || len(ltr.Options) != 26 || ltr.Options[0].Value != "C" {
		t.Fatalf("reflector letter %+v", ltr)
	}
	if txt := dsyControlOf(ds, "confDefRefText"); txt.Kind != "text" || txt.Value != "" || txt.Attrs["maxlength"] != "7" {
		t.Fatalf("text entry %+v", txt)
	}
	auto := dsyControlOf(ds, "confDefRefAuto")
	if auto.Kind != "radio" || auto.Value != "ON" || !reflect.DeepEqual(auto.Options, []linkOption{{"ON", "Startup"}, {"OFF", "Manual"}}) {
		t.Fatalf("auto %+v", auto)
	}
	if cell := dsyRowOf(t, ds, "confDefRef").Cells[2]; cell.Style != "width:300px" {
		t.Fatalf("radio cell %+v", cell)
	}
	lang := dsyControlOf(ds, "ircDDBGatewayAnnounceLanguage")
	if lang.Value != "0,0" || len(lang.Options) != 13 || lang.Options[1] != (linkOption{"1,4", "Deutsch"}) ||
		lang.Options[12] != (linkOption{"12,11", "Portugues"}) || lang.Options[0].Label != "English_(UK)" {
		t.Fatalf("language %+v", lang)
	}
	for name, want := range map[string]string{"confTimeAnnounce": "ON", "confircddbEnabled": "ON", "confHostFilesNoDExtra": "OFF"} {
		if got := dsyControlOf(ds, name); got.Kind != "toggle" || got.Value != want {
			t.Fatalf("%s %+v", name, got)
		}
	}

	// Variants of the stored state.
	variants := []struct {
		name   string
		mutate func(st *configState)
		check  func(t *testing.T, ds *cfgSection)
	}{
		{"short callsign gives an empty module option", func(st *configState) { st.Files["dstarrepeater"][""]["callsign"] = "M1ABC" }, func(t *testing.T, ds *cfgSection) {
			s := dsyControlOf(ds, "confDStarModuleSuffix")
			if s.Value != "" || len(s.Options) != 27 || s.Options[0] != (linkOption{"", ""}) {
				t.Fatalf("%+v", s)
			}
		}},
		{"manual reflector", func(st *configState) { st.Files["ircddbgateway"][""]["atStartup1"] = "0" }, func(t *testing.T, ds *cfgSection) {
			if v := dsyControlOf(ds, "confDefRefAuto").Value; v != "OFF" {
				t.Fatal(v)
			}
		}},
		{"unknown autostart selects no radio", func(st *configState) { delete(st.Files["ircddbgateway"][""], "atStartup1") }, func(t *testing.T, ds *cfgSection) {
			if v := dsyControlOf(ds, "confDefRefAuto").Value; v != "" {
				t.Fatal(v)
			}
		}},
		{"language Espanol", func(st *configState) { st.Files["ircddbgateway"][""]["language"] = "7" }, func(t *testing.T, ds *cfgSection) {
			if v := dsyControlOf(ds, "ircDDBGatewayAnnounceLanguage").Value; v != "7,9" {
				t.Fatal(v)
			}
		}},
		{"markers present", func(st *configState) {
			st.System.Markers[dsyMarkerTimeDisable] = dsyStr("")
			st.System.Markers[dsyMarkerNoDExtra] = dsyStr("")
			st.Files["ircddbgateway"][""]["ircddbEnabled"] = "0"
		}, func(t *testing.T, ds *cfgSection) {
			for name, want := range map[string]string{"confTimeAnnounce": "OFF", "confircddbEnabled": "OFF", "confHostFilesNoDExtra": "ON"} {
				if got := dsyControlOf(ds, name).Value; got != want {
					t.Fatalf("%s %s", name, got)
				}
			}
		}},
		{"quoted flat values", func(st *configState) { st.Files["ircddbgateway"][""]["remotePassword"] = `"pa ss"` }, func(t *testing.T, ds *cfgSection) {
			if v := dsyControlOf(ds, "confPassword").Value; v != "pa ss" {
				t.Fatal(v)
			}
		}},
	}
	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			st := dsyRawState()
			v.mutate(&st)
			v.check(t, buildDStarSection(dsyCtx(st)))
		})
	}
}

func TestYSFBuildValues(t *testing.T) {
	dsyEnv(t)
	c := dsyCtx(dsyRawState())
	ysf := buildYSFSection(c)
	host := dsyControlOf(ysf, "ysfStartupHost")
	wantHosts := []linkOption{{"none", "None"}, {"00001,ZZ Parrot", "YSF00001 - Parrot"}, {"00002,YSF2DMR", "YSF00002 - Link YSF2DMR"},
		{"00003,YSF2NXDN", "YSF00003 - Link YSF2NXDN"}, {"00004,YSF2P25", "YSF00004 - Link YSF2P25"},
		{"12345,GB_TEST", "YSF12345 - GB_TEST - Test room"}, {"54321,US_ROOM", "YSF54321 - US_ROOM - America"},
		{"FCS00290,FCS00290", "FCS00290 - FCS Room 290"}}
	if host.Value != "12345,GB_TEST" || !reflect.DeepEqual(host.Options, wantHosts) {
		t.Fatalf("startup host %+v", host)
	}
	for name, want := range map[string]string{"confHostFilesYSFUpper": "ON", "wiresXCommandPassthrough": "OFF"} {
		if got := dsyControlOf(ysf, name); got.Kind != "toggle" || got.Value != want {
			t.Fatalf("%s %+v", name, got)
		}
	}
	idRow := dsyRowOf(t, ysf, "ysf2dmrId").Cells[1]
	id := dsyControlOf(ysf, "ysf2dmrId")
	if idRow.Controls[0].Text != "1234501" || id.Value != "123450101" || len(id.Options) != 100 ||
		id.Options[0] != (linkOption{"1234501", "None"}) || id.Options[1] != (linkOption{"123450101", "01"}) || id.Options[99] != (linkOption{"123450199", "99"}) {
		t.Fatalf("ysf2dmrId %+v %+v", idRow.Controls[0], id)
	}
	master := dsyControlOf(ysf, "ysf2dmrMasterHost")
	if master.Value != "2341.master.brandmeister.network,passw0rd,62031,BM_2341_UK" || !reflect.DeepEqual(master.Options, []linkOption{
		{"2341.master.brandmeister.network,passw0rd,62031,BM_2341_UK", "BM_2341_UK"},
		{"2342.master.brandmeister.network,pass2,62032,BM_2342_UK", "BM_2342_UK"}}) {
		t.Fatalf("master %+v", master)
	}
	if sec := dsyControlOf(ysf, "ysf2dmrHSSecurity"); sec.Kind != "password" || sec.Value != "secret" || sec.Attrs["maxlength"] != "30" {
		t.Fatalf("bmHSSecurity %+v", sec)
	}
	if tg := dsyControlOf(ysf, "ysf2dmrTg"); tg.Value != "31672" || tg.Attrs["maxlength"] != "7" || tg.Attrs["size"] != "13" {
		t.Fatalf("tg %+v", tg)
	}
	if v := dsyControlOf(ysf, "ysf2nxdnId"); v.Value != "12345" || v.Attrs["maxlength"] != "5" {
		t.Fatalf("nxdn id %+v", v)
	}
	nx := dsyControlOf(ysf, "ysf2nxdnStartupDstId")
	if nx.Value != "10" || !reflect.DeepEqual(nx.Options, []linkOption{{"none", "None"}, {"10", "10 - Parrot"}, {"65000", "65000 - nxdn.example"}, {"31337", "31337 - local.example"}}) {
		t.Fatalf("nxdn host %+v", nx)
	}
	if v := dsyControlOf(ysf, "ysf2p25Id"); v.Value != "1234501" || v.Attrs["maxlength"] != "7" {
		t.Fatalf("p25 id %+v", v)
	}
	p25 := dsyControlOf(ysf, "ysf2p25StartupDstId")
	if p25.Value != "10200" || !reflect.DeepEqual(dsyValues(p25.Options), []string{"none", "10", "10100", "10200"}) {
		t.Fatalf("p25 host %+v", p25)
	}

	variants := []struct {
		name   string
		mutate func(st *configState)
		check  func(t *testing.T, ysf *cfgSection)
	}{
		{"no startup selects None", func(st *configState) { delete(st.Files["ysfgateway"]["Network"], "Startup") }, func(t *testing.T, y *cfgSection) {
			if v := dsyControlOf(y, "ysfStartupHost").Value; v != "none" {
				t.Fatal(v)
			}
		}},
		{"parrot by name", func(st *configState) { st.Files["ysfgateway"]["Network"]["Startup"] = "ZZ Parrot" }, func(t *testing.T, y *cfgSection) {
			if v := dsyControlOf(y, "ysfStartupHost").Value; v != "00001,ZZ Parrot" {
				t.Fatal(v)
			}
		}},
		{"id stored without FCS network is matched (defect fix)", func(st *configState) { st.Files["ysfgateway"]["Network"]["Startup"] = "54321" }, func(t *testing.T, y *cfgSection) {
			if v := dsyControlOf(y, "ysfStartupHost").Value; v != "54321,US_ROOM" {
				t.Fatal(v)
			}
		}},
		{"FCS room", func(st *configState) { st.Files["ysfgateway"]["Network"]["Startup"] = "FCS00290" }, func(t *testing.T, y *cfgSection) {
			if v := dsyControlOf(y, "ysfStartupHost").Value; v != "FCS00290,FCS00290" {
				t.Fatal(v)
			}
		}},
		{"unknown startup selects nothing", func(st *configState) { st.Files["ysfgateway"]["Network"]["Startup"] = "GONE" }, func(t *testing.T, y *cfgSection) {
			if v := dsyControlOf(y, "ysfStartupHost").Value; v != "" {
				t.Fatal(v)
			}
		}},
		{"toggles from values", func(st *configState) {
			st.Files["ysfgateway"]["General"]["WiresXMakeUpper"] = "0"
			st.Files["ysfgateway"]["General"]["WiresXCommandPassthrough"] = "1"
		}, func(t *testing.T, y *cfgSection) {
			if dsyControlOf(y, "confHostFilesYSFUpper").Value != "OFF" || dsyControlOf(y, "wiresXCommandPassthrough").Value != "ON" {
				t.Fatal("toggles")
			}
		}},
		{"missing WiresX keys are off", func(st *configState) {
			delete(st.Files["ysfgateway"]["General"], "WiresXMakeUpper")
			delete(st.Files["ysfgateway"]["General"], "WiresXCommandPassthrough")
		}, func(t *testing.T, y *cfgSection) {
			if dsyControlOf(y, "confHostFilesYSFUpper").Value != "OFF" || dsyControlOf(y, "wiresXCommandPassthrough").Value != "OFF" {
				t.Fatal("toggles")
			}
		}},
		{"no ESSID selects None", func(st *configState) { st.Files["ysf2dmr"]["DMR Network"]["Id"] = "1234501" }, func(t *testing.T, y *cfgSection) {
			if v := dsyControlOf(y, "ysf2dmrId").Value; v != "1234501" {
				t.Fatal(v)
			}
		}},
		{"default DMR ID hides the text", func(st *configState) {
			st.Files["mmdvmhost"]["General"]["Id"] = "1234567"
			st.Files["ysf2dmr"]["DMR Network"]["Id"] = "123456705"
		}, func(t *testing.T, y *cfgSection) {
			cell := dsyRowOf(t, y, "ysf2dmrId").Cells[1]
			if len(cell.Controls) != 1 || dsyControlOf(y, "ysf2dmrId").Value != "123456705" {
				t.Fatalf("%+v", cell)
			}
		}},
		{"three-digit ESSID keeps the stored Id", func(st *configState) { st.Files["ysf2dmr"]["DMR Network"]["Id"] = "1234501123" }, func(t *testing.T, y *cfgSection) {
			if v := dsyControlOf(y, "ysf2dmrId").Value; v != "1234501123" {
				t.Fatal(v)
			}
		}},
		{"unknown master selects nothing", func(st *configState) { st.Files["ysf2dmr"]["DMR Network"]["Address"] = "10.0.0.1" }, func(t *testing.T, y *cfgSection) {
			if v := dsyControlOf(y, "ysf2dmrMasterHost").Value; v != "" {
				t.Fatal(v)
			}
		}},
		{"literal none and empty startup TGs", func(st *configState) {
			st.Files["ysf2nxdn"]["NXDN Network"]["StartupDstId"] = "none"
			delete(st.Files["ysf2p25"]["P25 Network"], "StartupDstId")
		}, func(t *testing.T, y *cfgSection) {
			if dsyControlOf(y, "ysf2nxdnStartupDstId").Value != "none" || dsyControlOf(y, "ysf2p25StartupDstId").Value != "none" {
				t.Fatal("none")
			}
		}},
		{"modem password without quotes", func(st *configState) {
			st.Files["dmr2ysf"]["Enabled"]["Enabled"] = "1"
			st.System.Markers["mmdvmhost"] = dsyStr("[BrandMeister]\nPassword=other\n")
		}, func(t *testing.T, y *cfgSection) {
			if v := dsyControlOf(y, "ysf2dmrHSSecurity").Value; v != "other" {
				t.Fatal(v)
			}
		}},
	}
	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			st := dsyRawState()
			v.mutate(&st)
			v.check(t, buildYSFSection(dsyCtx(st)))
		})
	}
}

func TestDStarYSFUnchangedFormWritesNothing(t *testing.T) {
	dsyEnv(t)
	base := dsyBaseline(t)
	for name, form := range map[string]func(c *cfgContext) cfgForm{
		"with frequency": func(c *cfgContext) cfgForm { f := dsyForm(c); f["confFREQ"] = "438.800000"; return f },
		"duplex": func(c *cfgContext) cfgForm {
			f := dsyForm(c)
			f["confFREQrx"], f["confFREQtx"] = "438.8", "438.8"
			return f
		},
		"no frequency": dsyForm,
	} {
		t.Run(name, func(t *testing.T) {
			c := dsyCtx(base)
			ch, err := dsyApply(c, form(c), nil)
			if err != nil || len(ch.Changes) != 0 || len(ch.System) != 0 {
				t.Fatalf("err %v changes %v system %v", err, dsyFlatChanges(ch), ch.System)
			}
		})
	}
	// A request carrying none of these controls (another form) writes
	// nothing, not even the defaults of the raw fixture.
	ch, err := dsyApply(dsyCtx(dsyRawState()), cfgForm{"adminPassword": "x"}, nil)
	if err != nil || len(ch.Changes) != 0 {
		t.Fatalf("other form: %v %v", err, dsyFlatChanges(ch))
	}
}

func TestDStarYSFApply(t *testing.T) {
	hosts := dsyEnv(t)
	base := dsyBaseline(t)
	fcsRooms := filepath.Join(hosts, "FCSHosts.txt")
	cases := []struct {
		name   string
		mutate func(st *configState)
		pre    func(p *cfgPlan)
		form   cfgForm // edits on top of the unchanged form; "" deletes nothing, "<absent>" removes
		want   map[string]string
		system map[string]any
	}{
		{name: "module suffix", form: cfgForm{"confDStarModuleSuffix": "C"}, want: map[string]string{
			"dstarrepeater||callsign": "M1ABC  C", "dstarrepeater||beaconText": "M1ABC  C", "ircddbgateway||repeaterBand1": "C",
			"mmdvmhost|D-Star|Module": "C", "timeserver||sendB": "0", "timeserver||sendC": "1"}},
		{name: "lower-case module suffix", form: cfgForm{"confDStarModuleSuffix": "d"}, want: map[string]string{
			"dstarrepeater||callsign": "M1ABC  D", "dstarrepeater||beaconText": "M1ABC  D", "ircddbgateway||repeaterBand1": "D",
			"mmdvmhost|D-Star|Module": "D", "timeserver||sendB": "0", "timeserver||sendD": "1"}},
		{name: "letter without a timeserver key", form: cfgForm{"confDStarModuleSuffix": "F"}, want: map[string]string{
			"dstarrepeater||callsign": "M1ABC  F", "dstarrepeater||beaconText": "M1ABC  F", "ircddbgateway||repeaterBand1": "F",
			"mmdvmhost|D-Star|Module": "F", "timeserver||sendB": "0"}},
		{name: "band from General's planned frequency and callsign",
			pre: func(p *cfgPlan) {
				p.set("mmdvmhost", "General", "Callsign", "M2XYZ")
				p.set("mmdvmhost", "Info", "TXFrequency", "145500000")
			},
			form: cfgForm{"confDStarModuleSuffix": ""},
			want: map[string]string{"mmdvmhost|General|Callsign": "M2XYZ", "mmdvmhost|Info|TXFrequency": "145500000",
				"dstarrepeater||callsign": "M2XYZ  C", "dstarrepeater||gateway": "M2XYZ  G", "dstarrepeater||beaconText": "M2XYZ  C",
				"ircddbgateway||repeaterBand1": "C", "ircddbgateway||repeaterCall1": "M2XYZ", "mmdvmhost|D-Star|Module": "C",
				"timeserver||sendB": "0", "timeserver||sendC": "1"}},
		{name: "23 cm band", pre: func(p *cfgPlan) { p.set("mmdvmhost", "Info", "TXFrequency", "1297000000") },
			form: cfgForm{"confDStarModuleSuffix": ""},
			want: map[string]string{"mmdvmhost|Info|TXFrequency": "1297000000", "dstarrepeater||callsign": "M1ABC  A",
				"dstarrepeater||beaconText": "M1ABC  A", "ircddbgateway||repeaterBand1": "A", "mmdvmhost|D-Star|Module": "A",
				"timeserver||sendB": "0", "timeserver||sendA": "1"}},
		{name: "out of band keeps RPT1 (defect fix)", pre: func(p *cfgPlan) { p.set("mmdvmhost", "Info", "TXFrequency", "900000000") },
			form: cfgForm{"confDStarModuleSuffix": ""},
			want: map[string]string{"mmdvmhost|Info|TXFrequency": "900000000", "timeserver||sendB": "0"}},
		{name: "no frequency posted ignores the suffix", form: cfgForm{"confFREQ": "<absent>", "confDStarModuleSuffix": "C"}, want: map[string]string{}},
		{name: "remote password", form: cfgForm{"confPassword": "n3wpass"}, want: map[string]string{"ircddbgateway||remotePassword": "n3wpass"}},
		{name: "empty remote password is ignored", form: cfgForm{"confPassword": ""}, want: map[string]string{}},
		{name: "reflector from the list", form: cfgForm{"confDefRef": "DCS001", "confDefRefLtr": "A"}, want: map[string]string{"ircddbgateway||reflector1": "DCS001 A"}},
		{name: "reflector letter only", form: cfgForm{"confDefRefLtr": "B"}, want: map[string]string{"ircddbgateway||reflector1": "REF001 B"}},
		{name: "reflector text entry", form: cfgForm{"confDefRef": "customOption", "confDefRefText": "xlx9", "confDefRefLtr": "B"}, want: map[string]string{"ircddbgateway||reflector1": "XLX9   B"}},
		{name: "seven-character text entry", form: cfgForm{"confDefRef": "customOption", "confDefRefText": "XLX1234", "confDefRefLtr": "D"}, want: map[string]string{"ircddbgateway||reflector1": "XLX1234D"}},
		{name: "own callsign as reflector is ignored", form: cfgForm{"confDefRef": "customOption", "confDefRefText": "M1ABC", "confDefRefLtr": "A"}, want: map[string]string{}},
		{name: "reflector manual", form: cfgForm{"confDefRefAuto": "OFF"}, want: map[string]string{"ircddbgateway||atStartup1": "0"}},
		{name: "reflector autostart on", mutate: func(st *configState) { st.Files["ircddbgateway"][""]["atStartup1"] = "0" },
			form: cfgForm{"confDefRefAuto": "ON"}, want: map[string]string{"ircddbgateway||atStartup1": "1"}},
		{name: "language", form: cfgForm{"ircDDBGatewayAnnounceLanguage": "1,4"}, want: map[string]string{"ircddbgateway||language": "1", "timeserver||language": "4"}},
		{name: "time announcements off", form: cfgForm{"confTimeAnnounce": "OFF"}, want: map[string]string{}, system: map[string]any{dsyOpTimeAnnounce: "off"}},
		{name: "time announcements on", mutate: func(st *configState) { st.System.Markers[dsyMarkerTimeDisable] = dsyStr("") },
			form: cfgForm{"confTimeAnnounce": "ON"}, want: map[string]string{}, system: map[string]any{dsyOpTimeAnnounce: "on"}},
		{name: "time marker not reported by the helper", mutate: func(st *configState) { delete(st.System.Markers, dsyMarkerTimeDisable) },
			form: cfgForm{"confTimeAnnounce": "OFF"}, want: map[string]string{}},
		{name: "callsign routing off", form: cfgForm{"confircddbEnabled": "OFF"}, want: map[string]string{"ircddbgateway||ircddbEnabled": "0"}},
		{name: "rr.openquad.net forces routing off", mutate: func(st *configState) { st.Files["ircddbgateway"][""]["ircddbHostname"] = "rr.openquad.net" },
			form: cfgForm{"confircddbEnabled": "ON"}, want: map[string]string{"ircddbgateway||ircddbEnabled": "0", "ircddbgateway||ircddbHostname": "ircv4.openquad.net"}},
		{name: "missing flat key is not appended", mutate: func(st *configState) {
			delete(st.Files["ircddbgateway"][""], "ircddbEnabled")
			delete(st.Files["ircddbgateway"][""], "remotePassword")
		}, form: cfgForm{"confircddbEnabled": "OFF", "confPassword": "n3wpass"}, want: map[string]string{}},
		{name: "DPlus for XRF on", form: cfgForm{"confHostFilesNoDExtra": "ON"}, want: map[string]string{}, system: map[string]any{dsyOpNoDExtra: "on"}},
		{name: "DPlus for XRF off", mutate: func(st *configState) { st.System.Markers[dsyMarkerNoDExtra] = dsyStr("") },
			form: cfgForm{"confHostFilesNoDExtra": "OFF"}, want: map[string]string{}, system: map[string]any{dsyOpNoDExtra: "off"}},

		{name: "YSF startup none", form: cfgForm{"ysfStartupHost": "none"}, want: map[string]string{"ysfgateway|Network|Startup": "<del>", "dmr2ysf|DMR Network|DefaultDstTG": "9"}},
		{name: "YSF startup pseudo host", form: cfgForm{"ysfStartupHost": "00002,YSF2DMR"}, want: map[string]string{"ysfgateway|Network|Startup": "YSF2DMR", "dmr2ysf|DMR Network|DefaultDstTG": "00002"}},
		{name: "YSF startup FCS room", form: cfgForm{"ysfStartupHost": "FCS00290,FCS00290"}, want: map[string]string{"ysfgateway|Network|Startup": "FCS00290", "dmr2ysf|DMR Network|DefaultDstTG": "9"}},
		{name: "YSF startup without FCS network stores the id",
			mutate: func(st *configState) { delete(st.Files["ysfgateway"], "FCS Network") },
			form:   cfgForm{"ysfStartupHost": "54321,US_ROOM"},
			want: map[string]string{"ysfgateway|Network|Startup": "54321", "dmr2ysf|DMR Network|DefaultDstTG": "54321",
				"ysfgateway|FCS Network|Enable": "1", "ysfgateway|FCS Network|Port": "42001", "ysfgateway|FCS Network|Rooms": fcsRooms}},
		{name: "YSF startup without dmr2ysf", mutate: func(st *configState) { delete(st.Files, "dmr2ysf") },
			form: cfgForm{"ysfStartupHost": "none"}, want: map[string]string{"ysfgateway|Network|Startup": "<del>"}},
		{name: "uppercase off", form: cfgForm{"confHostFilesYSFUpper": "OFF"}, want: map[string]string{"ysfgateway|General|WiresXMakeUpper": "0"}},
		{name: "legacy ysfupper marker removed", mutate: func(st *configState) { st.System.Markers[dsyMarkerYSFUpper] = dsyStr("") },
			form: cfgForm{}, want: map[string]string{}, system: map[string]any{dsyOpYSFUpper: "off"}},
		{name: "WiresX passthrough on", form: cfgForm{"wiresXCommandPassthrough": "ON"}, want: map[string]string{"ysfgateway|General|WiresXCommandPassthrough": "1"}},
		{name: "ESSID", form: cfgForm{"ysf2dmrId": "123450105"}, want: map[string]string{"ysf2dmr|DMR Network|Id": "123450105"}},
		{name: "ESSID None writes the base ID (defect fix)", form: cfgForm{"ysf2dmrId": "1234501"}, want: map[string]string{"ysf2dmr|DMR Network|Id": "1234501"}},
		{name: "DMR master keeps the password", form: cfgForm{"ysf2dmrMasterHost": "2342.master.brandmeister.network,pass2,62032,BM_2342_UK"},
			want: map[string]string{"ysf2dmr|DMR Network|Address": "2342.master.brandmeister.network", "ysf2dmr|DMR Network|Port": "62032"}},
		{name: "DMR master with an empty security password", form: cfgForm{"ysf2dmrMasterHost": "2342.master.brandmeister.network,pass2,62032,BM_2342_UK", "ysf2dmrHSSecurity": ""},
			want: map[string]string{"ysf2dmr|DMR Network|Address": "2342.master.brandmeister.network", "ysf2dmr|DMR Network|Port": "62032"}},
		{name: "DMR master without a stored password takes the host's", mutate: func(st *configState) { delete(st.Files["ysf2dmr"]["DMR Network"], "Password") },
			form: cfgForm{"ysf2dmrMasterHost": "2342.master.brandmeister.network,pass2,62032,BM_2342_UK", "ysf2dmrHSSecurity": ""},
			want: map[string]string{"ysf2dmr|DMR Network|Address": "2342.master.brandmeister.network", "ysf2dmr|DMR Network|Port": "62032",
				"ysf2dmr|DMR Network|Password": `"pass2"`}},
		{name: "hotspot security", form: cfgForm{"ysf2dmrHSSecurity": "n3w s3cret"}, want: map[string]string{"ysf2dmr|DMR Network|Password": `"n3w s3cret"`},
			system: map[string]any{"modem_ini": map[string]map[string]*string{"BrandMeister": {"Password": dmrPtr(`"n3w s3cret"`)}}}},
		{name: "YSF2DMR TG", form: cfgForm{"ysf2dmrTg": "91"}, want: map[string]string{"ysf2dmr|DMR Network|StartupDstId": "91"}},
		{name: "empty YSF2DMR TG is ignored", form: cfgForm{"ysf2dmrTg": ""}, want: map[string]string{}},
		{name: "YSF2NXDN ID", form: cfgForm{"ysf2nxdnId": "23456"}, want: map[string]string{"ysf2nxdn|NXDN Network|Id": "23456"}},
		{name: "YSF2NXDN startup none", form: cfgForm{"ysf2nxdnStartupDstId": "none"},
			want: map[string]string{"ysf2nxdn|NXDN Network|StartupDstId": "none", "nxdngateway|Network|Static": "<del>"}},
		{name: "YSF2NXDN startup host", form: cfgForm{"ysf2nxdnStartupDstId": "31337"},
			want: map[string]string{"ysf2nxdn|NXDN Network|StartupDstId": "31337", "nxdngateway|Network|Static": "31337"}},
		{name: "YSF2NXDN startup migrates Startup to Static", mutate: func(st *configState) {
			st.Files["nxdngateway"]["Network"] = map[string]string{"Startup": "10"}
		}, form: cfgForm{"ysf2nxdnStartupDstId": "65000"},
			want: map[string]string{"ysf2nxdn|NXDN Network|StartupDstId": "65000", "nxdngateway|Network|Static": "65000", "nxdngateway|Network|Startup": "<del>"}},
		{name: "YSF2P25 startup none", form: cfgForm{"ysf2p25StartupDstId": "none"},
			want: map[string]string{"ysf2p25|P25 Network|StartupDstId": "<del>", "p25gateway|Network|Static": "<del>"}},
		{name: "YSF2P25 startup host", form: cfgForm{"ysf2p25StartupDstId": "10100"},
			want: map[string]string{"ysf2p25|P25 Network|StartupDstId": "10100", "p25gateway|Network|Static": "10100"}},
		{name: "YSF2P25 startup without p25gateway", mutate: func(st *configState) { delete(st.Files, "p25gateway") },
			form: cfgForm{"ysf2p25StartupDstId": "10"}, want: map[string]string{"ysf2p25|P25 Network|StartupDstId": "10"}},
		{name: "YSF2P25 ID", form: cfgForm{"ysf2p25Id": "7654321"}, want: map[string]string{"ysf2p25|P25 Network|Id": "7654321"}},

		{name: "hidden YSF2DMR rows are not written", mutate: func(st *configState) { st.Files["ysf2dmr"]["Enabled"]["Enabled"] = "0" },
			form: cfgForm{"ysf2dmrTg": "1", "ysf2dmrId": "1", "ysf2dmrHSSecurity": "x", "ysf2dmrMasterHost": "bogus"}, want: map[string]string{}},
		{name: "hidden YSF section is not written", mutate: func(st *configState) { st.Files["mmdvmhost"]["System Fusion Network"]["Enable"] = "0" },
			form: cfgForm{"ysfStartupHost": "none", "wiresXCommandPassthrough": "ON", "ysf2p25Id": "1"}, want: map[string]string{}},
		{name: "hidden D-Star section is not written", mutate: func(st *configState) { st.Files["mmdvmhost"]["D-Star"]["Enable"] = "0" },
			form: cfgForm{"confDStarModuleSuffix": "C", "confPassword": "x", "confircddbEnabled": "OFF"}, want: map[string]string{}},
		{name: "missing files are never written", mutate: func(st *configState) {
			delete(st.Files, "timeserver")
			delete(st.Files, "dstarrepeater")
		}, form: cfgForm{"confDStarModuleSuffix": "C", "ircDDBGatewayAnnounceLanguage": "1,4"},
			want: map[string]string{"ircddbgateway||repeaterBand1": "C", "mmdvmhost|D-Star|Module": "C", "ircddbgateway||language": "1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := dsyClone(base)
			if tc.mutate != nil {
				tc.mutate(&st)
			}
			c := dsyCtx(st)
			form := dsyForm(c)
			form["confFREQ"] = "438.800000"
			for k, v := range tc.form {
				if v == "<absent>" {
					delete(form, k)
				} else {
					form[k] = v
				}
			}
			ch, err := dsyApply(c, form, tc.pre)
			if err != nil {
				t.Fatal(err)
			}
			if got := dsyFlatChanges(ch); !reflect.DeepEqual(got, tc.want) {
				keys := []string{}
				for k, v := range got {
					keys = append(keys, k+"="+v)
				}
				sort.Strings(keys)
				t.Fatalf("changes:\n%s", strings.Join(keys, "\n"))
			}
			if !reflect.DeepEqual(ch.System, tc.system) && !(len(ch.System) == 0 && len(tc.system) == 0) {
				t.Fatalf("system %v", ch.System)
			}
		})
	}
}

func TestDStarYSFRejectsBadInput(t *testing.T) {
	dsyEnv(t)
	base := dsyBaseline(t)
	cases := []struct {
		name string
		form cfgForm
		pre  func(p *cfgPlan)
	}{
		{"module digit", cfgForm{"confDStarModuleSuffix": "1"}, nil},
		{"module two letters", cfgForm{"confDStarModuleSuffix": "AB"}, nil},
		{"invalid planned callsign", cfgForm{"confDStarModuleSuffix": "C"}, func(p *cfgPlan) { p.set("mmdvmhost", "General", "Callsign", "M1 ABC") }},
		{"password with quote", cfgForm{"confPassword": `pa"ss`}, nil},
		{"password too long", cfgForm{"confPassword": strings.Repeat("x", 31)}, nil},
		{"password control character", cfgForm{"confPassword": "pa\tss"}, nil},
		{"reflector metacharacters", cfgForm{"confDefRef": "customOption", "confDefRefText": "REF;01", "confDefRefLtr": "A"}, nil},
		{"reflector too long", cfgForm{"confDefRef": "customOption", "confDefRefText": "REF00123", "confDefRefLtr": "A"}, nil},
		{"reflector letter digit", cfgForm{"confDefRef": "DCS001", "confDefRefLtr": "1"}, nil},
		{"changed reflector without letter", cfgForm{"confDefRef": "DCS001", "confDefRefLtr": ""}, nil},
		{"autostart value", cfgForm{"confDefRefAuto": "MAYBE"}, nil},
		{"unknown language", cfgForm{"ircDDBGatewayAnnounceLanguage": "99,0"}, nil},
		{"toggle value", cfgForm{"confTimeAnnounce": "1"}, nil},
		{"routing toggle value", cfgForm{"confircddbEnabled": "yes"}, nil},
		{"nodextra toggle value", cfgForm{"confHostFilesNoDExtra": ""}, nil},
		{"unknown YSF host", cfgForm{"ysfStartupHost": "99999,NOPE"}, nil},
		{"YSF upper toggle value", cfgForm{"confHostFilesYSFUpper": "on"}, nil},
		{"passthrough toggle value", cfgForm{"wiresXCommandPassthrough": "true"}, nil},
		{"unknown DMR master", cfgForm{"ysf2dmrMasterHost": "1.2.3.4,x,62031,EVIL"}, nil},
		{"excluded DMR master", cfgForm{"ysf2dmrMasterHost": "xlx.example,passw0rd,62030,XLX_123"}, nil},
		{"security password quote", cfgForm{"ysf2dmrHSSecurity": `a"b`}, nil},
		{"TG letters", cfgForm{"ysf2dmrTg": "12a"}, nil},
		{"TG too long", cfgForm{"ysf2dmrTg": "12345678"}, nil},
		{"DMR ID letters", cfgForm{"ysf2dmrId": "None"}, nil},
		{"NXDN ID too large", cfgForm{"ysf2nxdnId": "70000"}, nil},
		{"NXDN ID zero", cfgForm{"ysf2nxdnId": "0"}, nil},
		{"unknown NXDN host", cfgForm{"ysf2nxdnStartupDstId": "777"}, nil},
		{"unknown P25 host", cfgForm{"ysf2p25StartupDstId": "777"}, nil},
		{"P25 ID too long", cfgForm{"ysf2p25Id": "12345678"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := dsyCtx(base)
			form := dsyForm(c)
			form["confFREQ"] = "438.800000"
			for k, v := range tc.form {
				form[k] = v
			}
			_, err := dsyApply(c, form, tc.pre)
			if _, ok := err.(cfgError); !ok {
				t.Fatalf("want cfgError, got %v", err)
			}
		})
	}
}

func TestDStarYSFDefaults(t *testing.T) {
	hosts := dsyEnv(t)
	raw := dsyRawState()
	raw.Files["ircddbgateway"][""]["ccsEnabled"] = "1"
	raw.Files["ircddbgateway"][""]["ccsHost"] = "CCS999"
	c := dsyCtx(raw)
	form := dsyForm(c)
	form["confFREQ"] = "438.800000"
	ch, err := dsyApply(c, form, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := dsyFlatChanges(ch)
	h := func(name string) string { return filepath.Join(hosts, name) }
	want := map[string]string{
		// ircDDBGateway
		"ircddbgateway||ircddbEnabled2": "0", "ircddbgateway||ccsHost": "CCS701",
		// DMR2YSF
		"dmr2ysf|YSF Network|FCSRooms": h("FCSHosts.txt"), "dmr2ysf|DMR Network|TGUnlink": "4000",
		"dmr2ysf|DMR Network|TGListFile": h("TGList_YSF.txt"), "dmr2ysf|Log|DisplayLevel": "0", "dmr2ysf|Log|FileLevel": "2",
		"dmr2ysf|YSF Network|DT1": dsyDT1, "dmr2ysf|YSF Network|DT2": dsyDT2, "dmr2ysf|YSF Network|Debug": "0",
		// YSFGateway
		"ysfgateway|Network|Revert": "0", "ysfgateway|Network|Port": "42000", "ysfgateway|Network|DataPort": "<del>",
		"ysfgateway|Network|InactivityTimeout": "0", "ysfgateway|Network|Debug": "0",
		"ysfgateway|YSF Network|Enable": "1", "ysfgateway|YSF Network|Hosts": h("YSFHosts.txt"),
		"ysfgateway|YSF Network|ReloadTime": "60", "ysfgateway|YSF Network|ParrotAddress": "127.0.0.1", "ysfgateway|YSF Network|ParrotPort": "42012",
		"ysfgateway|YSF Network|YSF2DMRAddress": "127.0.0.1", "ysfgateway|YSF Network|YSF2DMRPort": "42013",
		"ysfgateway|YSF Network|YSF2NXDNAddress": "127.0.0.1", "ysfgateway|YSF Network|YSF2NXDNPort": "42014",
		"ysfgateway|YSF Network|YSF2P25Address": "127.0.0.1", "ysfgateway|YSF Network|YSF2P25Port": "42015",
		"ysfgateway|FCS Network|Port": "42001", "ysfgateway|FCS Network|Rooms": h("FCSHosts.txt"),
		"ysfgateway|Remote Commands|Enable": "1", "ysfgateway|Remote Commands|Port": "6073", "ysfgateway|General|Debug": "0",
		"ysfgateway|GPSD|Enable": "0", "ysfgateway|GPSD|Address": "127.0.0.1", "ysfgateway|GPSD|Port": "2947",
		"ysfgateway|APRS|Enable": "1", "ysfgateway|APRS|Address": "127.0.0.1", "ysfgateway|APRS|Port": "8673",
		"ysfgateway|APRS|Description": "M1ABC_Pi-Star_ND", "ysfgateway|APRS|Suffix": "Y",
		"ysfgateway|Mobile GPS|Enable": "<del>", "ysfgateway|Mobile GPS|Address": "<del>", "ysfgateway|Mobile GPS|Port": "<del>",
		"ysfgateway|aprs.fi|Enable": "<del>",
		// YSF2DMR
		"ysf2dmr|Info|Power": "1", "ysf2dmr|Info|Height": "0", "ysf2dmr|Info|Enabled": "<del>",
		"ysf2dmr|YSF Network|DstAddress": "127.0.0.1", "ysf2dmr|YSF Network|DstPort": "42000",
		"ysf2dmr|YSF Network|LocalAddress": "127.0.0.1", "ysf2dmr|YSF Network|LocalPort": "42013",
		"ysf2dmr|YSF Network|Daemon": "1", "ysf2dmr|YSF Network|EnableWiresX": "1",
		"ysf2dmr|YSF Network|WiresXMakeUpper": "1", "ysf2dmr|YSF Network|DT1": dsyDT1, "ysf2dmr|YSF Network|DT2": dsyDT2,
		"ysf2dmr|DMR Network|StartupPC": "0", "ysf2dmr|DMR Network|Jitter": "500", "ysf2dmr|DMR Network|EnableUnlink": "1",
		"ysf2dmr|DMR Network|TGUnlink": "4000", "ysf2dmr|DMR Network|PCUnlink": "0", "ysf2dmr|DMR Network|Debug": "0",
		"ysf2dmr|DMR Id Lookup|File": h("DMRIds.dat"), "ysf2dmr|DMR Id Lookup|Time": "24", "ysf2dmr|DMR Id Lookup|DropUnknown": "0",
		"ysf2dmr|Log|DisplayLevel": "0", "ysf2dmr|Log|FileLevel": "0", "ysf2dmr|Log|FilePath": dsyLog, "ysf2dmr|Log|FileRoot": "YSF2DMR",
		"ysf2dmr|aprs.fi|Enable": "0", "ysf2dmr|aprs.fi|Port": "14580", "ysf2dmr|aprs.fi|Refresh": "240",
		// DGIdGateway
		"dgidgateway|General|LocalPort": "4200", "dgidgateway|General|RptPort": "3200", "dgidgateway|Log|DisplayLevel": "1",
		"dgidgateway|Log|FileLevel": "1", "dgidgateway|Log|FilePath": dsyLog, "dgidgateway|Log|FileRoot": "DGIdGateway",
		"dgidgateway|Log|FileRotate": "0", "dgidgateway|YSF Network|Hosts": h("YSFHosts.txt"),
	}
	for _, x := range []struct{ file, port, lookup, lookupFile, net, tg, root string }{
		{"ysf2nxdn", "42014", "NXDN Id Lookup", "NXDN.csv", "NXDN Network", "TGList_NXDN.txt", "YSF2NXDN"},
		{"ysf2p25", "42015", "DMR Id Lookup", "DMRIds.dat", "P25 Network", "TGList_P25.txt", "YSF2P25"},
	} {
		for k, v := range map[string]string{"YSF Network|LocalPort": x.port, "YSF Network|DstPort": "42000", "YSF Network|Daemon": "1",
			"YSF Network|EnableWiresX": "1", x.lookup + "|File": h(x.lookupFile), x.net + "|TGListFile": h(x.tg),
			"Log|DisplayLevel": "0", "Log|FileLevel": "0", "Log|FilePath": dsyLog, "Log|FileRoot": x.root,
			"YSF Network|WiresXMakeUpper": "1", "YSF Network|DT1": dsyDT1, "YSF Network|DT2": dsyDT2} {
			want[x.file+"|"+k] = v
		}
	}
	want["ysf2nxdn|aprs.fi|Enable"] = "0"
	want["ysf2p25|aprs.fi|Enable"] = "<del>"
	if !reflect.DeepEqual(got, want) {
		var diff []string
		for k, v := range got {
			if want[k] != v {
				diff = append(diff, "got  "+k+"="+v)
			}
		}
		for k, v := range want {
			if got[k] != v {
				diff = append(diff, "want "+k+"="+v)
			}
		}
		sort.Strings(diff)
		t.Fatalf("defaults differ:\n%s", strings.Join(diff, "\n"))
	}

	// Idempotent: a second save of the committed result writes nothing.
	next := dsyCommit(raw, ch)
	c2 := dsyCtx(next)
	form2 := dsyForm(c2)
	form2["confFREQ"] = "438.800000"
	if ch2, err := dsyApply(c2, form2, nil); err != nil || len(ch2.Changes) != 0 {
		t.Fatalf("second save %v %v", err, dsyFlatChanges(ch2))
	}

	// ircddbEnabled2 is not added when ircddbHostname2 exists; TGListFile
	// is defaulted only when TGList_BM.txt exists; a missing file is never
	// created.
	raw = dsyRawState()
	raw.Files["ircddbgateway"][""]["ircddbHostname2"] = "ircv4.example"
	delete(raw.Files, "dgidgateway")
	delete(raw.Files, "ysf2p25")
	if err := os.WriteFile(h("TGList_BM.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	c = dsyCtx(raw)
	form = dsyForm(c)
	ch, err = dsyApply(c, form, nil)
	if err != nil {
		t.Fatal(err)
	}
	got = dsyFlatChanges(ch)
	if _, ok := got["ircddbgateway||ircddbEnabled2"]; ok {
		t.Fatal("ircddbEnabled2 added despite ircddbHostname2")
	}
	if got["ysf2dmr|DMR Network|TGListFile"] != h("TGList_BM.txt") {
		t.Fatalf("TGListFile %q", got["ysf2dmr|DMR Network|TGListFile"])
	}
	if ch.Changes["dgidgateway"] != nil || ch.Changes["ysf2p25"] != nil {
		t.Fatal("missing file written")
	}
}

// The DStarRepeater controller hides the YSF section, but the D-Star
// applier still runs the shared defaults for files that exist.
func TestDStarRepeaterControllerApply(t *testing.T) {
	dsyEnv(t)
	st := dsyBaseline(t)
	st.System.Markers["mmdvmhost"] = nil
	st.System.Markers["dstarrepeater"] = dsyStr("[BrandMeister]\nPassword=x\n")
	st.Files["mmdvmhost"]["D-Star"]["Enable"] = "0"
	c := dsyCtx(st)
	if buildYSFSection(c) != nil || buildDStarSection(c) == nil {
		t.Fatal("visibility")
	}
	form := dsyForm(c)
	form["confFREQ"] = "438.800000"
	form["confDStarModuleSuffix"] = "E"
	ch, err := dsyApply(c, form, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"dstarrepeater||callsign": "M1ABC  E", "dstarrepeater||beaconText": "M1ABC  E",
		"ircddbgateway||repeaterBand1": "E", "mmdvmhost|D-Star|Module": "E", "timeserver||sendB": "0", "timeserver||sendE": "1"}
	if got := dsyFlatChanges(ch); !reflect.DeepEqual(got, want) {
		t.Fatalf("changes %v", got)
	}
}
