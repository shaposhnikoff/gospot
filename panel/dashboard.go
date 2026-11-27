package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// dashboardVersion is set at build time (-X main.dashboardVersion=...).
var dashboardVersion = "dev"

// Locations come from pistar.conf via applySettings.
var (
	mmdvmINI, dmrGWINI, dapnetINI, ircddbINI, dstarRptINI = newINIFile(), newINIFile(), newINIFile(), newINIFile(), newINIFile()
	ysf2dmrINI, ysf2nxdnINI, ysf2p25INI                   = newINIFile(), newINIFile(), newINIFile()
	dmr2ysfINI, dmr2nxdnINI                               = newINIFile(), newINIFile()
)

// Process names as they appear in /proc/<pid>/comm. Source builds install
// MMDVMHost as "MMDVM-Host" on some systems.
var processAliases = map[string][]string{
	"MMDVMHost": {"MMDVMHost", "MMDVM-Host"},
}

type dashCell struct {
	Label string `json:"label"`
	// State is "on" (green), "fail" (red), "warn" (yellow) or "off" (grey).
	State string `json:"state"`
}

type dashTRX struct {
	// Kind: tx, rx, hang, listening, offline, pocsag, none.
	Kind  string `json:"kind"`
	Mode  string `json:"mode"`
	Label string `json:"label"`
}

type dashRow struct {
	Time     time.Time `json:"time"`
	Clock    string    `json:"clock"`
	Mode     string    `json:"mode"`
	Callsign string    `json:"callsign"`
	Target   string    `json:"target"`
	Source   string    `json:"source"`
	Duration *float64  `json:"duration"`
	Loss     *float64  `json:"loss"`
	BER      *float64  `json:"ber"`
	RSSI     *float64  `json:"rssi"`
	Live     bool      `json:"live"`
}

type dashDStar struct {
	RPT1, RPT2, APRS, IRC string
}

type dashDMR struct {
	ID, CC   string
	TS1, TS2 bool
	Network  bool
	Masters  []string
}

type dashLink struct {
	Code, Link string
}

type dashPOCSAG struct {
	TX, Master string
}

type dashboard struct {
	Hostname   string
	Callsign   string
	Version    string
	Controller string
	Modes      []dashCell
	Networks   []dashCell
	TRX        dashTRX
	TX, RX     string
	Firmware   string
	TCXO       string
	DStar      *dashDStar
	DMR        *dashDMR
	YSF        *dashLink
	P25        *dashLink
	NXDN       *dashLink
	M17        *dashLink
	POCSAG     *dashPOCSAG
	Heard      []dashRow
	Local      []dashRow
	HeardReady bool
	TimeZone   string
	Now        time.Time
}

func runningProcesses() map[string]bool {
	seen := map[string]bool{}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return seen
	}
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		b, err := os.ReadFile(filepath.Join("/proc", e.Name(), "comm"))
		if err == nil {
			seen[strings.TrimSpace(string(b))] = true
		}
	}
	return seen
}

type dashInputs struct {
	host, gw, dapnet, ircddb, dstar iniData
	ysf2dmr, ysf2nxdn, ysf2p25      iniData
	dmr2ysf, dmr2nxdn               iniData
	procs                           map[string]bool
	login                           map[string]int
	firmware, tcxo                  string
	rows, active                    []HeardRow
	heardOK                         bool
	hostname                        string
	controller                      string
	now                             time.Time
}

func (in dashInputs) running(name string) bool {
	for _, alias := range append([]string{name}, processAliases[name]...) {
		if in.procs[alias] {
			return true
		}
	}
	return false
}

func (in dashInputs) enabled(section string) bool { return in.host.on(section, "Enable") }

// dmrViaGateway mirrors the classic check: MMDVMHost points its DMR network
// at the local DMRGateway.
func (in dashInputs) dmrViaGateway() bool {
	addr := in.host.get("DMR Network", "Address")
	if addr == "" {
		addr = in.host.get("DMR Network", "GatewayAddress")
	}
	return addr == "127.0.0.1"
}

func (in dashInputs) xEnabled(d iniData) bool { return d.on("Enabled", "Enabled") }

// cell reproduces showMode() from the classic dashboard.
func (in dashInputs) cell(mode string) dashCell {
	label := strings.ReplaceAll(mode, "System Fusion", "YSF")
	label = strings.ReplaceAll(label, "Network", "Net")
	if strings.Contains(label, "YSF2") || strings.Contains(label, "DMR2") {
		label = strings.ReplaceAll(label, " Net", "")
	}
	c := dashCell{Label: label, State: "off"}
	onIf := func(ok bool) {
		if ok {
			c.State = "on"
		} else {
			c.State = "fail"
		}
	}
	greyOr := func(ok bool) {
		if ok {
			c.State = "on"
		}
	}
	gateways := map[string]string{"D-Star Network": "ircddbgatewayd", "System Fusion Network": "YSFGateway",
		"P25 Network": "P25Gateway", "NXDN Network": "NXDNGateway", "M17 Network": "M17Gateway", "POCSAG Network": "DAPNETGateway"}
	switch {
	case in.enabled(mode):
		if gw, ok := gateways[mode]; ok {
			onIf(in.running(gw))
		} else if mode == "DMR Network" {
			daemon, unit := "MMDVMHost", modemUnit
			if in.dmrViaGateway() {
				daemon, unit = "DMRGateway", masterUnit
			}
			onIf(in.running(daemon))
			if c.State == "on" && in.login[unit] < 0 {
				c.State = "warn"
			}
		} else {
			onIf(in.running("MMDVMHost"))
		}
	case mode == "YSF XMode" && in.enabled("System Fusion"):
		greyOr(in.running("MMDVMHost") && (in.running("YSF2DMR") || in.running("YSF2NXDN") || in.running("YSF2P25")))
	case mode == "DMR XMode" && in.enabled("DMR"):
		greyOr(in.running("MMDVMHost") && (in.running("DMR2YSF") || in.running("DMR2NXDN")))
	case strings.HasPrefix(mode, "YSF2") && in.enabled("System Fusion"):
		greyOr(in.running(strings.TrimSuffix(mode, " Network")))
	case strings.HasPrefix(mode, "DMR2") && in.enabled("DMR"):
		greyOr(in.running(strings.TrimSuffix(mode, " Network")))
	}
	return c
}

func mhz(hz string) string {
	if len(hz) < 4 {
		return hz
	}
	tail := hz[3:]
	if len(tail) > 6 {
		tail = tail[:6]
	}
	return hz[:3] + "." + tail + " MHz"
}

func shorten(s string) string {
	if len(s) > 19 {
		return s[:17] + ".."
	}
	return s
}

func heardMode(r HeardRow) string { return "DMR Slot " + r.Slot }

func (in dashInputs) hang(source string) time.Duration {
	seconds := in.host.get("General", "ModeHang")
	if seconds == "" {
		if source == "RF" {
			seconds = in.host.get("DMR", "ModeHang")
			if seconds == "" {
				seconds = in.host.get("General", "RFModeHang")
			}
		} else {
			seconds = in.host.get("DMR Network", "ModeHang")
			if seconds == "" {
				seconds = in.host.get("General", "NetModeHang")
			}
		}
	}
	n, err := strconv.Atoi(seconds)
	if err != nil || n < 0 {
		n = 10
	}
	return time.Duration(n) * time.Second
}

func (in dashInputs) trx() dashTRX {
	for _, a := range in.active {
		if a.Source != "RF" {
			return dashTRX{Kind: "tx", Mode: "DMR", Label: "TX " + heardMode(a)}
		}
	}
	if len(in.active) > 0 {
		return dashTRX{Kind: "rx", Mode: "DMR", Label: "RX DMR"}
	}
	if len(in.rows) > 0 && in.rows[0].Duration != nil {
		last := in.rows[0]
		end := last.At.Add(time.Duration(*last.Duration * float64(time.Second))).Add(in.hang(last.Source))
		if in.now.Before(end) {
			return dashTRX{Kind: "hang", Mode: "DMR", Label: "Listening DMR"}
		}
	}
	if in.running("MMDVMHost") {
		return dashTRX{Kind: "listening", Label: "Listening"}
	}
	return dashTRX{Kind: "offline", Label: "OFFLINE"}
}

func (in dashInputs) dmr() *dashDMR {
	if !in.enabled("DMR") {
		return nil
	}
	d := &dashDMR{ID: in.host.get("General", "Id"), CC: in.host.get("DMR", "ColorCode"),
		TS1: in.host.on("DMR Network", "Slot1"), TS2: in.host.on("DMR Network", "Slot2"), Network: in.enabled("DMR Network")}
	if !d.Network {
		return d
	}
	if !in.dmrViaGateway() {
		d.Masters = []string{shorten(in.host.get("DMR Network", "Address"))}
		return d
	}
	name := func(section string) string {
		n := in.gw.get(section, "Name")
		if n == "" {
			n = in.gw.get(section, "Address")
		}
		return shorten(strings.ReplaceAll(n, "_", " "))
	}
	for _, s := range []string{"XLX Network 1", "XLX Network", "DMR Network 1", "DMR Network 2", "DMR Network 3", "DMR Network 4", "DMR Network 5", "DMR Network 6"} {
		if in.gw.on(s, "Enabled") {
			if s == "XLX Network" && in.gw.has("XLX Network 1", "Enabled") {
				continue
			}
			d.Masters = append(d.Masters, name(s))
		}
	}
	return d
}

func (in dashInputs) link(gateway string) string {
	if !in.running(gateway) {
		return "Service Not Started"
	}
	return "Not Linked"
}

func buildDashboard(in dashInputs) dashboard {
	callsign := strings.ToUpper(in.ircddb.get("", "gatewayCallsign"))
	if callsign == "" {
		callsign = strings.ToUpper(in.host.get("General", "Callsign"))
	}
	freq := func(key string) string {
		v := in.host.get("Info", key)
		if v == "" {
			v = in.host.get("Modem", key)
		}
		return mhz(v)
	}
	zone, _ := in.now.In(time.Local).Zone()
	d := dashboard{Hostname: in.hostname, Callsign: callsign, Version: dashboardVersion, Controller: in.controller,
		TX: freq("TXFrequency"), RX: freq("RXFrequency"), Firmware: in.firmware, TCXO: in.tcxo,
		HeardReady: in.heardOK, TimeZone: zone, Now: in.now.UTC()}
	if in.controller != "mmdvmhost" {
		return d
	}
	for _, m := range []string{"D-Star", "DMR", "M17", "NXDN", "P25", "System Fusion", "DMR XMode", "YSF XMode", "FM", "POCSAG"} {
		d.Modes = append(d.Modes, in.cell(m))
	}
	for _, m := range []string{"D-Star Network", "DMR Network", "M17 Network", "NXDN Network", "P25 Network", "System Fusion Network",
		"DMR2NXDN Network", "DMR2YSF Network", "YSF2DMR Network", "YSF2NXDN Network", "YSF2P25 Network", "POCSAG Network"} {
		d.Networks = append(d.Networks, in.cell(m))
	}
	d.TRX = in.trx()
	if in.enabled("D-Star") {
		d.DStar = &dashDStar{RPT1: in.dstar.get("", "callsign"), RPT2: in.dstar.get("", "gateway")}
		if in.ircddb.get("", "aprsEnabled") == "1" {
			d.DStar.APRS = shorten16(in.ircddb.get("", "aprsHostname"))
		}
		if in.ircddb.get("", "ircddbEnabled") == "1" {
			d.DStar.IRC = shorten16(in.ircddb.get("", "ircddbHostname"))
		}
	}
	d.DMR = in.dmr()
	if in.enabled("System Fusion Network") || in.xEnabled(in.dmr2ysf) {
		d.YSF = &dashLink{Link: in.link("YSFGateway")}
	}
	if in.enabled("P25 Network") || in.xEnabled(in.ysf2p25) {
		d.P25 = &dashLink{Code: orZero(in.host.get("P25", "NAC")), Link: in.link("P25Gateway")}
	}
	if in.enabled("NXDN Network") || in.xEnabled(in.ysf2nxdn) || in.xEnabled(in.dmr2nxdn) {
		d.NXDN = &dashLink{Code: orZero(in.host.get("NXDN", "RAN")), Link: in.link("NXDNGateway")}
	}
	if in.enabled("M17 Network") {
		d.M17 = &dashLink{Code: orZero(in.host.get("M17", "CAN")), Link: in.link("M17Gateway")}
	}
	if in.enabled("POCSAG Network") {
		d.POCSAG = &dashPOCSAG{TX: mhz(in.host.get("POCSAG", "Frequency")), Master: shorten(in.dapnet.get("DAPNET", "Address"))}
	}
	type callKey struct {
		at                     time.Time
		source, from, to, slot string
	}
	live := map[callKey]bool{}
	for _, a := range in.active {
		live[callKey{a.At, a.Source, a.From, a.To, a.Slot}] = true
	}
	for _, r := range in.rows {
		row := dashRow{Time: r.At, Clock: classicClock(r.At), Mode: heardMode(r), Callsign: r.From, Target: r.To, Source: r.Source,
			Duration: r.Duration, Loss: r.Loss, BER: r.BER, RSSI: r.RSSI, Live: r.Duration == nil && live[callKey{r.At, r.Source, r.From, r.To, r.Slot}]}
		if len(d.Heard) < 20 {
			d.Heard = append(d.Heard, row)
		}
		if r.Source == "RF" && len(d.Local) < 20 {
			d.Local = append(d.Local, row)
		}
	}
	return d
}

func orZero(s string) string {
	if s == "" {
		return "0"
	}
	return s
}

func shorten16(s string) string {
	if len(s) > 16 {
		return s[:16]
	}
	return s
}

func controllerMode() string {
	_, mmdvm := os.Stat(markerMMDVMHost)
	_, dstar := os.Stat(markerDStarRepeater)
	switch {
	case mmdvm == nil:
		return "mmdvmhost"
	case dstar == nil:
		return "dstarrepeater"
	case mmdvmINI.exists():
		// Native installs without Pi-Star's marker files run MMDVMHost.
		return "mmdvmhost"
	}
	return "none"
}

func gatherDashboard(now time.Time) dashboard {
	firmware, tcxo, login := journal.snapshot()
	rows, active, ok := recentHeard.live()
	host, _ := os.ReadFile(hostnameFile)
	return buildDashboard(dashInputs{
		host: mmdvmINI.load(), gw: dmrGWINI.load(), dapnet: dapnetINI.load(), ircddb: ircddbINI.load(), dstar: dstarRptINI.load(),
		ysf2dmr: ysf2dmrINI.load(), ysf2nxdn: ysf2nxdnINI.load(), ysf2p25: ysf2p25INI.load(),
		dmr2ysf: dmr2ysfINI.load(), dmr2nxdn: dmr2nxdnINI.load(),
		procs: runningProcesses(), login: login, firmware: firmware, tcxo: tcxo,
		rows: rows, active: active, heardOK: ok, hostname: strings.TrimSpace(string(host)),
		controller: controllerMode(), now: now,
	})
}

// dashboardHandler serves one shared snapshot rebuilt at most every 500 ms,
// so several open browsers polling each second cost one build.
func dashboardHandler() http.HandlerFunc {
	var mu sync.Mutex
	var body []byte
	var built time.Time
	return func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if time.Since(built) > 500*time.Millisecond || body == nil {
			b, err := json.Marshal(gatherDashboard(time.Now()))
			if err == nil {
				body, built = b, time.Now()
			}
		}
		out := body
		mu.Unlock()
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(out)
	}
}

// classicClock formats like PHP date('H:i:s M jS') in the server time zone.
func classicClock(t time.Time) string {
	t = t.In(time.Local)
	day := t.Day()
	suffix := "th"
	if day%100 < 11 || day%100 > 13 {
		switch day % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return t.Format("15:04:05 Jan ") + strconv.Itoa(day) + suffix
}
