package main

import (
	"strings"
	"testing"
	"time"
)

const testHostINI = `[General]
Callsign=ut3udx
Id=2550430
RFModeHang=10
NetModeHang=3
[Modem]
RXFrequency=433800000
TXFrequency=433800000
[D-Star]
Enable=0
[DMR]
Enable=1
ColorCode=1
[System Fusion]
Enable=0
[POCSAG]
Enable=1
Frequency=439987500
[DMR Network]
Enable=1
GatewayAddress=127.0.0.1
Slot1=0
Slot2=1
[POCSAG Network]
Enable=0
`

const testGatewayINI = `[XLX Network]
Enabled=0
[DMR Network 1]
Enabled=1
Name=BM_2341_United_Kingdom_Long
Address=1.2.3.4
Password=secret
[DMR Network 2]
Enabled=0
`

func testInputs() dashInputs {
	return dashInputs{
		host: parseINI(strings.NewReader(testHostINI)), gw: parseINI(strings.NewReader(testGatewayINI)),
		procs: map[string]bool{"MMDVM-Host": true, "DMRGateway": true}, login: map[string]int{},
		controller: "mmdvmhost", now: time.Date(2026, 11, 27, 6, 0, 0, 0, time.UTC),
	}
}

func cellState(cells []dashCell, label string) string {
	for _, c := range cells {
		if c.Label == label {
			return c.State
		}
	}
	return "missing"
}

func TestDashboardCells(t *testing.T) {
	in := testInputs()
	d := buildDashboard(in)
	want := map[string]string{"D-Star": "off", "DMR": "on", "POCSAG": "on", "YSF": "off", "DMR XMode": "off", "YSF XMode": "off"}
	for label, state := range want {
		if got := cellState(d.Modes, label); got != state {
			t.Errorf("mode %s = %s, want %s", label, got, state)
		}
	}
	if got := cellState(d.Networks, "DMR Net"); got != "on" {
		t.Errorf("DMR Net = %s", got)
	}
	if got := cellState(d.Networks, "YSF2DMR"); got != "off" {
		t.Errorf("YSF2DMR label/state = %s", got)
	}
	in.login[masterUnit] = -1
	if got := cellState(buildDashboard(in).Networks, "DMR Net"); got != "warn" {
		t.Errorf("failed login should be warn, got %s", got)
	}
	delete(in.procs, "DMRGateway")
	if got := cellState(buildDashboard(in).Networks, "DMR Net"); got != "fail" {
		t.Errorf("stopped gateway should be fail, got %s", got)
	}
	delete(in.procs, "MMDVM-Host")
	if got := cellState(buildDashboard(in).Modes, "DMR"); got != "fail" {
		t.Errorf("stopped MMDVMHost should be fail, got %s", got)
	}
}

func TestDashboardInfo(t *testing.T) {
	d := buildDashboard(testInputs())
	if d.Callsign != "UT3UDX" || d.TX != "433.800000 MHz" || d.RX != "433.800000 MHz" {
		t.Fatalf("header/radio: %+v", d)
	}
	if d.DMR == nil || d.DMR.ID != "2550430" || d.DMR.CC != "1" || d.DMR.TS1 || !d.DMR.TS2 {
		t.Fatalf("dmr: %+v", d.DMR)
	}
	if len(d.DMR.Masters) != 1 || d.DMR.Masters[0] != "BM 2341 United Ki.." {
		t.Fatalf("masters: %q", d.DMR.Masters)
	}
	if d.POCSAG != nil {
		t.Fatal("POCSAG block needs the POCSAG network")
	}
	if d.TRX.Kind != "listening" {
		t.Fatalf("trx: %+v", d.TRX)
	}
}

func TestDashboardTRX(t *testing.T) {
	in := testInputs()
	call := HeardRow{At: in.now.Add(-2 * time.Second), Source: "Net", From: "2341234", To: "TG 91", Slot: "2"}
	in.rows, in.active = []HeardRow{call}, []HeardRow{call}
	d := buildDashboard(in)
	if d.TRX.Kind != "tx" || d.TRX.Label != "TX DMR Slot 2" || !d.Heard[0].Live {
		t.Fatalf("network call: %+v %+v", d.TRX, d.Heard)
	}
	if len(d.Local) != 0 {
		t.Fatal("network call is not local RF")
	}
	dur := 1.0
	call.Duration = &dur
	in.rows, in.active = []HeardRow{call}, nil
	if d := buildDashboard(in); d.TRX.Kind != "hang" || d.Heard[0].Live {
		t.Fatalf("within NetModeHang: %+v", d.TRX)
	}
	in.now = in.now.Add(10 * time.Second)
	if d := buildDashboard(in); d.TRX.Kind != "listening" {
		t.Fatalf("after hang: %+v", d.TRX)
	}
	rf := HeardRow{At: in.now, Source: "RF", From: "UT3UDX", To: "TG 91", Slot: "2"}
	in.rows, in.active = []HeardRow{rf}, []HeardRow{rf}
	if d := buildDashboard(in); d.TRX.Kind != "rx" || len(d.Local) != 1 {
		t.Fatalf("rf call: %+v", d.TRX)
	}
}

func TestModemFirmware(t *testing.T) {
	cases := map[string]string{
		"MMDVM_HS_Dual_Hat-v1.5.2 20201108 12.2880MHz dual ADF7021 FW by CA6JAU GitID #89daa20": "HS_Hat:v1.5.2",
		"MMDVM_HS-v1.4.17 20190529 14.7456MHz ADF7021 FW by CA6JAU":                             "MMDVM_HS:1.4.17",
		"ZUMspot-v1.5.2 20201108 14.7456MHz ADF7021 FW by CA6JAU":                               "ZUMspot:v1.5.2",
		"MMDVM 20200815 TCXO (D-Star/DMR/System Fusion/P25/NXDN/POCSAG/FM)":                     "MMDVM:20200815",
		"something else": "",
	}
	for desc, want := range cases {
		if got := modemFirmware(desc); got != want {
			t.Errorf("%q -> %q, want %q", desc, got, want)
		}
	}
	var j journalState
	j.login = map[string]int{}
	j.observe(modemUnit, "I: 2026-11-26 13:36:48.403 MMDVM protocol version: 1, description: MMDVM_HS_Dual_Hat-v1.5.2 20201108 12.2880MHz dual ADF7021")
	j.observe(masterUnit, "E: 2026-11-27 05:15:35.297 GoDMRServer, Connection to the master has timed out, retrying connection")
	fw, tcxo, login := j.snapshot()
	if fw != "HS_Hat:v1.5.2" || tcxo != "12.2880 MHz" || login[masterUnit] != -1 {
		t.Fatalf("%q %q %v", fw, tcxo, login)
	}
	j.observe(masterUnit, "M: 2026-11-27 05:16:35.297 GoDMRServer, Logged into the master successfully")
	if _, _, login = j.snapshot(); login[masterUnit] != 1 {
		t.Fatal("login success not recorded")
	}
}

func TestParseINIFlatAndFirstKey(t *testing.T) {
	d := parseINI(strings.NewReader("callsign=UT3UDX  B\n# x=1\n[A]\nk=1\nk=2\n; c\n"))
	if d.get("", "callsign") != "UT3UDX  B" || d.get("A", "k") != "1" || d.has("A", "c") {
		t.Fatalf("%v", d)
	}
}

func TestClassicClock(t *testing.T) {
	loc := time.Local
	time.Local = time.UTC
	defer func() { time.Local = loc }()
	for day, want := range map[int]string{1: "1st", 2: "2nd", 3: "3rd", 4: "4th", 11: "11th", 12: "12th", 13: "13th", 21: "21st", 22: "22nd", 23: "23rd"} {
		if got := classicClock(time.Date(2026, 11, day, 5, 6, 7, 0, time.UTC)); got != "05:06:07 Nov "+want {
			t.Errorf("%d: %q", day, got)
		}
	}
}

func TestReleaseVersionSections(t *testing.T) {
	for text, want := range map[string]string{
		"[GoSpot]\nVersion = 4.3-native\n":                             "4.3-native",
		"[Pi-Star]\nVersion = 4.1.6\nHardware = RPi\n":                 "4.1.6",
		"[Pi-Star]\nVersion = 4.1.6\n[GoSpot]\nVersion = 4.3-native\n": "4.3-native",
		"": "",
	} {
		if got := releaseVersion(parseINI(strings.NewReader(text))); got != want {
			t.Errorf("%q: %q, want %q", text, got, want)
		}
	}
}
