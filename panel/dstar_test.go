package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Fixtures in testdata/dstar are in the classic formats: /etc/ircddbgateway,
// ircDDBGateway's Links.log, Headers.log and STARnet.log, and the D-Star host
// lists (samples from the comments of Pi-Star_DV_Dash dstarrepeater/*.php).

func dstarFixture(t *testing.T, logs ...string) (dir string, cfg iniData) {
	t.Helper()
	loc := time.Local
	time.Local = time.UTC
	dir = t.TempDir()
	savedLog, savedHosts := gospotLogDir, gospotHostsDir
	savedINI, savedHost := ircddbINI.location(), mmdvmINI.location()
	savedMarkers := []string{markerMMDVMHost, markerDStarRepeater}
	t.Cleanup(func() {
		time.Local = loc
		gospotLogDir, gospotHostsDir = savedLog, savedHosts
		ircddbINI.setPath(savedINI)
		mmdvmINI.setPath(savedHost)
		markerMMDVMHost, markerDStarRepeater = savedMarkers[0], savedMarkers[1]
		dstarHeaders.reset("")
	})
	for _, name := range append([]string{"ircddbgateway", "DCS_Hosts.txt", "DPlus_Hosts.txt", "DExtra_Hosts.txt"}, logs...) {
		data, err := os.ReadFile(filepath.Join("testdata/dstar", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gospotLogDir, gospotHostsDir = dir, dir
	ircddbINI.setPath(filepath.Join(dir, "ircddbgateway"))
	mmdvmINI.setPath(filepath.Join(dir, "mmdvmhost"))
	markerMMDVMHost, markerDStarRepeater = filepath.Join(dir, "dstar-radio.mmdvmhost"), filepath.Join(dir, "dstar-radio.dstarrepeater")
	dstarHeaders.reset("")
	return dir, ircddbINI.load()
}

var allDStarLogs = []string{"Links.log", "Headers.log", "STARnet.log"}

func TestDStarRepeaterPanels(t *testing.T) {
	_, cfg := dstarFixture(t, allDStarLogs...)
	p := dstarDashboard("dstarrepeater", iniData{}, cfg)
	if p == nil || !p.Network {
		t.Fatal("DStarRepeater always shows the D-Star panels")
	}
	// gateway_software_config.php
	g := p.Gateway
	flags := []dashCell{{"CCS", "off"}, {"DCS", "on"}, {"DExtra", "on"}, {"DPlus", "on"}, {"D-Rats", "off"},
		{"Info", "on"}, {"ircDDB", "on"}, {"Echo", "on"}, {"Log", "on"}}
	if g.IRCDDB != "ircv4.openquad.net" || g.APRS != "rotate.aprs2.net" || !reflect.DeepEqual(g.Flags, flags) {
		t.Fatalf("gateway: %+v", g)
	}
	// active_reflector_links.php: module B linked out, an incoming repeater
	// and a dongle; module C down (default blank, 5min timer) with the dongle
	// repeated as the classic loop does. repeaterBand4=DD is not a module.
	want := []dstarLinkRow{
		{Radio: "M1ABC  B", Default: "DCS001 C", Auto: "Auto", Timer: "Never", Status: "Up", Linked: "DCS001 C", Mode: "DCS", Direction: "Outgoing", Changed: "06:10:01 Nov 27th"},
		{Incoming: true, Radio: "M1ABC  B", Status: "Up", Linked: "XRF023 A", Mode: "DExtra", Direction: "Incoming", Changed: "06:11:02 Nov 27th"},
		{Incoming: true, Radio: "M1ABC  B", Status: "Up", Linked: "W1CDG", Mode: "DPlus", Direction: "Incoming", Changed: "06:12:03 Nov 27th"},
		{Radio: "M1ABC  C", Default: "", Auto: "No", Timer: "5min", Status: "Down", Linked: "None", Mode: "--", Direction: "----", Changed: "----"},
		{Incoming: true, Radio: "M1ABC  C", Status: "Up", Linked: "W1CDG", Mode: "DPlus", Direction: "Incoming", Changed: "06:12:03 Nov 27th"},
	}
	if !reflect.DeepEqual(p.Links, want) {
		t.Fatalf("links:\n%+v\nwant\n%+v", p.Links, want)
	}
	// css_connections.php: shown because Links.log has a CCS line (ccsEnabled=0 does not matter)
	if !p.CCSShow || !reflect.DeepEqual(p.CCS, []dstarCCSRow{{"M1ABC  B", "DL5DI   ", "Incoming", "06:13:04 Nov 27th"}}) {
		t.Fatalf("ccs: %v %+v", p.CCSShow, p.CCS)
	}
	// active_starnet_groups.php: removed and re-added members move to the end; unknown groups are ignored
	sn := p.StarNet
	if sn == nil || !reflect.DeepEqual(sn.Groups, []dstarStarNetGroup{{"STN001 A", "STN001 L", "QA Net", "300", "0"}}) {
		t.Fatalf("starnet groups: %+v", sn)
	}
	if !reflect.DeepEqual(sn.Members, []dstarStarNetMember{{"06:31:00 Nov 27th", "STN001 A", "2E0ABC"}, {"06:33:00 Nov 27th", "STN001 A", "M0XYZ"}}) {
		t.Fatalf("starnet members: %+v", sn.Members)
	}
	// last_herd.php: newest per station first, "  /TIME" announcements dropped
	type hv struct{ Clock, Call, ID, Lookup, Your, R1, R2 string }
	view := func(rows []dstarHeardRow) []hv {
		var out []hv
		for _, r := range rows {
			out = append(out, hv{r.Clock, r.Callsign, r.ID, r.Lookup, r.Your, r.RPT1, r.RPT2})
		}
		return out
	}
	heard := []hv{
		{"06:25:00 Nov 27th", "2E0ABC", "", "2E0ABC", "CQCQCQ  ", "M1ABC  B", "M1ABC  G"},
		{"06:23:00 Nov 27th", "PD1RB", "IC92", "PD1RB", "CQCQCQ  ", "PE1RJV B", "REF017 A"},
		{"06:22:00 Nov 27th", "M0XYZ", "ID51", "M0XYZ", "DCS001CL", "M1ABC  B", "M1ABC  G"},
		{"06:21:00 Nov 27th", "PU2ZHZ", "T", "PU2ZHZ", "CQCQCQ  ", "PU2ZHZ B", "DCS007 B"},
	}
	if got := view(p.Heard); !reflect.DeepEqual(got, heard) {
		t.Fatalf("heard:\n%+v", got)
	}
	// local_tx.php: Repeater header lines only
	if got := view(p.Local); !reflect.DeepEqual(got, []hv{heard[0], heard[2]}) {
		t.Fatalf("local:\n%+v", got)
	}
}

func TestDStarMMDVMHostPanels(t *testing.T) {
	_, cfg := dstarFixture(t, allDStarLogs...)
	if p := dstarDashboard("mmdvmhost", parseINI(strings.NewReader("[D-Star]\nEnable=1\n[D-Star Network]\nEnable=0\n")), cfg); p != nil {
		t.Fatalf("no D-Star network, no panels: %+v", p)
	}
	host := parseINI(strings.NewReader("[D-Star]\nEnable=1\n[D-Star Network]\nEnable=1\n"))
	p := dstarDashboard("mmdvmhost", host, cfg)
	if p == nil || !p.Network || p.Gateway != nil || p.Heard != nil || p.Local != nil || len(p.Links) != 5 || !p.CCSShow || p.StarNet == nil {
		t.Fatalf("MMDVMHost shows links, CCS and StarNet only: %+v", p)
	}
	if p := dstarDashboard("none", host, cfg); p != nil {
		t.Fatal("no controller, no panels")
	}
}

func TestDStarMissingFiles(t *testing.T) {
	_, cfg := dstarFixture(t)
	p := dstarDashboard("dstarrepeater", iniData{}, cfg)
	if p.CCSShow || p.CCS != nil || p.Heard != nil || p.Local != nil {
		t.Fatalf("no logs: %+v", p)
	}
	if len(p.Links) != 2 || p.Links[0].Status != "Down" || p.Links[1].Status != "Down" || p.Links[0].Linked != "None" {
		t.Fatalf("modules without Links.log are Down: %+v", p.Links)
	}
	if p.StarNet == nil || len(p.StarNet.Groups) != 1 || p.StarNet.Members != nil {
		t.Fatalf("groups without STARnet.log: %+v", p.StarNet)
	}
	// no /etc/ircddbgateway: no modules, empty gateway cells
	p = dstarDashboard("dstarrepeater", iniData{}, iniData{})
	if p.Links != nil || p.StarNet != nil || p.Gateway.IRCDDB != "" || p.Gateway.APRS != "" || p.Gateway.Flags[1].State != "off" {
		t.Fatalf("no gateway config: %+v", p)
	}
}

func TestDStarFilesReparsedOnChange(t *testing.T) {
	dir, cfg := dstarFixture(t, allDStarLogs...)
	links := filepath.Join(dir, "Links.log")
	// ircDDBGateway rewrites Links.log with the current links
	os.WriteFile(links, []byte("2026-11-27 07:00:00: DPlus link - Type: Repeater Rptr: M1ABC  C Refl: REF001 C Dir: Outgoing\n"), 0o644)
	os.Chtimes(links, time.Now(), time.Now().Add(time.Minute))
	p := dstarDashboard("dstarrepeater", iniData{}, cfg)
	if len(p.Links) != 2 || p.Links[0].Status != "Down" || p.Links[1].Linked != "REF001 C" || p.Links[1].Mode != "DPlus" || p.CCSShow {
		t.Fatalf("rewritten Links.log: %+v", p.Links)
	}
	// Headers.log grows: only the appended part is parsed; a partial line waits
	headers := filepath.Join(dir, "Headers.log")
	f, _ := os.OpenFile(headers, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("2026-11-27 07:01:00: Repeater header - My: M0NEW   /    Your: CQCQCQ    Rpt1: M1ABC  B  Rpt2: M1ABC  G  Flags: 00 00 00\n")
	f.WriteString("2026-11-27 07:02:00: Repeater header - My: PU2ZHZ  /T  ")
	f.Close()
	p = dstarDashboard("dstarrepeater", iniData{}, cfg)
	if len(p.Heard) != 5 || p.Heard[0].Callsign != "M0NEW" || p.Local[0].Callsign != "M0NEW" || len(p.Local) != 3 {
		t.Fatalf("appended header: %+v", p.Heard)
	}
	f, _ = os.OpenFile(headers, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("   Your: CQCQCQ    Rpt1: M1ABC  B  Rpt2: M1ABC  G  Flags: 00 00 00\n")
	f.Close()
	p = dstarDashboard("dstarrepeater", iniData{}, cfg)
	if p.Heard[0].Callsign != "PU2ZHZ" || p.Heard[0].Clock != "07:02:00 Nov 27th" || len(p.Heard) != 5 || p.Local[0].Callsign != "PU2ZHZ" {
		t.Fatalf("completed line replaces the station's older entry: %+v", p.Heard)
	}
	// truncated (log cleanup): start again from the new content
	os.WriteFile(headers, []byte("2026-11-28 01:00:00: DCS header - My: G0AAA   /    Your: CQCQCQ    Rpt1: G0AAA  B  Rpt2: DCS001 C  Flags: 00 00 00\n"), 0o644)
	p = dstarDashboard("dstarrepeater", iniData{}, cfg)
	if len(p.Heard) != 1 || p.Heard[0].Callsign != "G0AAA" || p.Local != nil && len(p.Local) != 0 {
		t.Fatalf("truncated Headers.log: %+v %+v", p.Heard, p.Local)
	}
	os.Remove(headers)
	if p = dstarDashboard("dstarrepeater", iniData{}, cfg); p.Heard != nil {
		t.Fatalf("removed Headers.log: %+v", p.Heard)
	}
}

func TestDStarHeardLimit(t *testing.T) {
	dir, cfg := dstarFixture(t)
	var b strings.Builder
	for i := 0; i < 30; i++ {
		b.WriteString("2026-11-27 08:" + string(rune('0'+i/10)) + string(rune('0'+i%10)) + ":00: Repeater header - My: M0A" +
			string(rune('A'+i%26)) + string(rune('A'+i/26)) + "  /    Your: CQCQCQ    Rpt1: M1ABC  B  Rpt2: M1ABC  G  Flags: 00 00 00\n")
	}
	os.WriteFile(filepath.Join(dir, "Headers.log"), []byte(b.String()), 0o644)
	p := dstarDashboard("dstarrepeater", iniData{}, cfg)
	if len(p.Heard) != 20 || len(p.Local) != 30 || p.Heard[0].Clock != "08:29:00 Nov 27th" || p.Heard[19].Clock != "08:10:00 Nov 27th" {
		t.Fatalf("head -20: %d %d %+v", len(p.Heard), len(p.Local), p.Heard[0])
	}
}

func TestDashboardDStarJSON(t *testing.T) {
	dir, _ := dstarFixture(t, allDStarLogs...)
	os.WriteFile(markerDStarRepeater, nil, 0o644)
	w := httptest.NewRecorder()
	dashboardHandler()(w, httptest.NewRequest("GET", "/api/dashboard", nil))
	body := w.Body.String()
	var d struct {
		Controller string
		DStarInfo  *dstarPanels
	}
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil || d.Controller != "dstarrepeater" || d.DStarInfo == nil || len(d.DStarInfo.Heard) != 4 {
		t.Fatalf("%v %s", err, body)
	}
	for _, secret := range []string{"rc-Secret-7", "irc-Secret-3", "remotePassword", "ircddbPassword"} {
		if strings.Contains(body, secret) {
			t.Fatalf("%s in /api/dashboard", secret)
		}
	}
	_ = dir
}

func TestDStarTimersAndClock(t *testing.T) {
	for v, want := range map[string]string{"": "Never", "0": "Never", "1": "Fixed", "2": "5min", "8": "60min", "11": "180min", "12": "", "40": "", "x": "Never", "-1": ""} {
		if got := dstarTimer(v); got != want {
			t.Errorf("reconnect=%q: %q, want %q", v, got, want)
		}
	}
	if at, clock := dstarClock("garbage"); !at.IsZero() || clock != "" {
		t.Fatal("unparsable log time")
	}
}

func TestDStarLongHeadersLog(t *testing.T) {
	dir, cfg := dstarFixture(t)
	saved := dstarHeadersPrim
	t.Cleanup(func() { dstarHeadersPrim = saved })
	line := func(call, at string) string {
		return "2026-11-27 " + at + ": Repeater header - My: " + call + "  /    Your: CQCQCQ    Rpt1: M1ABC  B  Rpt2: M1ABC  G  Flags: 00 00 00\n"
	}
	old, recent := line("G0OLD ", "05:00:00"), line("G0NEW ", "06:00:00")
	dstarHeadersPrim = int64(len(recent) + 10) // the cut lands inside the old line
	os.WriteFile(filepath.Join(dir, "Headers.log"), []byte(old+old+recent), 0o644)
	p := dstarDashboard("dstarrepeater", iniData{}, cfg)
	if len(p.Heard) != 1 || p.Heard[0].Callsign != "G0NEW" {
		t.Fatalf("only whole lines of the tail: %+v", p.Heard)
	}
}
