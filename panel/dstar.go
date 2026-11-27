package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// D-Star panels of the classic dashboard (Pi-Star_DV_Dash 10bf34c9):
// dstarrepeater/gateway_software_config.php, active_reflector_links.php,
// css_connections.php with active_starnet_groups.php, last_herd.php and
// local_tx.php. ircDDBGateway writes Links.log, STARnet.log and Headers.log
// into PISTAR_LOG_DIR (config/ircddblocal.php); each file is parsed again only
// when its size or modification time changes, Headers.log incrementally.

type dstarGateway struct {
	IRCDDB string
	APRS   string // "" when APRS is disabled: the classic red marker
	Flags  []dashCell
}

type dstarLinkRow struct {
	Incoming  bool
	Radio     string
	Default   string
	Auto      string
	Timer     string
	Status    string
	Linked    string
	Mode      string
	Direction string
	Changed   string
}

type dstarCCSRow struct {
	Repeater, Remote, Direction, Changed string
}

type dstarStarNetGroup struct {
	Callsign, Logoff, Info, UserTimeout, GroupTimeout string
}

type dstarStarNetMember struct {
	Clock, Group, Callsign string
}

type dstarStarNet struct {
	Groups  []dstarStarNetGroup
	Members []dstarStarNetMember
}

type dstarHeardRow struct {
	Time     time.Time
	Clock    string
	Callsign string // the 8-character My field without spaces
	ID       string // the 4 characters after '/', without spaces
	Lookup   string // first word of the My field, for the lookup links
	Your     string
	RPT1     string
	RPT2     string
}

// dstarPanels is what /api/dashboard adds for D-Star. The page decides which
// panels the operator and admin variants show (classic index.php rules).
type dstarPanels struct {
	Network bool // MMDVMHost: [D-Star Network] Enable=1; DStarRepeater: always
	Gateway *dstarGateway
	Links   []dstarLinkRow
	CCS     []dstarCCSRow // shown only when Links.log has a "CCS link" line
	CCSShow bool
	StarNet *dstarStarNet
	Heard   []dstarHeardRow // DStarRepeater controller only
	Local   []dstarHeardRow
}

// dstarBand is one configured repeater module of /etc/ircddbgateway.
type dstarBand struct {
	index int
	call  string // repeater callsign padded to 7 characters + module letter
}

// The keys a classic partial reads exist only when non-empty ($configs).
func ircddbValue(cfg iniData, key string) string { return cfg.get("", key) }

func dstarBands(cfg iniData) []dstarBand {
	my := strings.ToUpper(ircddbValue(cfg, "gatewayCallsign"))
	var out []dstarBand
	for i := 1; i <= 4; i++ {
		module := ircddbValue(cfg, fmt.Sprintf("repeaterBand%d", i))
		if len(module) != 1 {
			continue
		}
		call := ircddbValue(cfg, fmt.Sprintf("repeaterCall%d", i))
		if call == "" {
			call = my
		}
		out = append(out, dstarBand{i, fmt.Sprintf("%-7.7s%-1.1s", call, module)})
	}
	return out
}

// phpSubstr is substr($s, start, length) for ASCII log fields.
func phpSubstr(s string, start, length int) string {
	if start >= len(s) {
		return ""
	}
	s = s[start:]
	if length >= 0 && length < len(s) {
		s = s[:length]
	}
	return s
}

// dstarClock turns a log time (UTC, first 19 characters) into the classic
// 'H:i:s M jS' local time.
func dstarClock(stamp string) (time.Time, string) {
	t, err := time.ParseInLocation("2006-01-02 15:04:05", phpSubstr(stamp, 0, 19), time.UTC)
	if err != nil {
		return time.Time{}, ""
	}
	return t, classicClock(t)
}

var dstarFlags = [][2]string{{"ccsEnabled", "CCS"}, {"dcsEnabled", "DCS"}, {"dextraEnabled", "DExtra"}, {"dplusEnabled", "DPlus"},
	{"dratsEnabled", "D-Rats"}, {"infoEnabled", "Info"}, {"ircddbEnabled", "ircDDB"}, {"echoEnabled", "Echo"}, {"logEnabled", "Log"}}

func dstarGatewayConfig(cfg iniData) *dstarGateway {
	g := &dstarGateway{IRCDDB: ircddbValue(cfg, "ircddbHostname")}
	if ircddbValue(cfg, "aprsEnabled") == "1" {
		g.APRS = ircddbValue(cfg, "aprsHostname")
	}
	for _, f := range dstarFlags {
		state := "off"
		if ircddbValue(cfg, f[0]) == "1" {
			state = "on"
		}
		g.Flags = append(g.Flags, dashCell{Label: f[1], State: state})
	}
	return g
}

// Links.log lines, as matched by active_reflector_links.php and
// css_connections.php:
//
//	2011-09-22 02:15:06: DExtra link - Type: Repeater Rptr: DB0LJ  B Refl: XRF023 A Dir: Outgoing
//	2012-05-08 21:16:31: DPlus link - Type: Dongle User: W1CDG  H Dir: Incoming
//	2013-02-27 19:49:27: CCS link - Rptr: DB0LJ  B Remote: DL5DI    Dir: Incoming
var (
	dstarLinkOut  = regexp.MustCompile(`^(.{19}).*(D[A-Za-z]*).*Type: ([A-Za-z]*).*Rptr: (.{8}).*Refl: (.{8}).*Dir: Outgoing$`)
	dstarLinkIn   = regexp.MustCompile(`^(.{19}).*(D[A-Za-z]*).*Type: ([A-Za-z]*).*Rptr: (.{8}).*Refl: (.{8}).*Dir: Incoming$`)
	dstarLinkUser = regexp.MustCompile(`^(.{19}).*(D[A-Za-z]*).*Type: ([A-Za-z]*).*User: (.[^\s]+).*Dir: Incoming$`)
	dstarCCSLink  = regexp.MustCompile(`^(.{19}).*(C[A-Za-z]*).*Rptr: (.{8}).*Remote: (.{8}).*Dir: (.{8})$`)
)

type dstarLink struct {
	stamp, proto, rptr, refl string
}

type dstarLinksLog struct {
	out, in, users []dstarLink
	ccs            []dstarCCSRow
	ccsLine        bool
}

func parseLinksLog(r io.Reader) dstarLinksLog {
	var l dstarLinksLog
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 4096), 64<<10)
	for s.Scan() {
		line := strings.TrimRight(s.Text(), "\r")
		if strings.Contains(line, "CCS link") {
			l.ccsLine = true
		}
		if m := dstarLinkOut.FindStringSubmatch(line); m != nil {
			l.out = append(l.out, dstarLink{m[1], m[2], m[4], m[5]})
		}
		if m := dstarLinkIn.FindStringSubmatch(line); m != nil {
			l.in = append(l.in, dstarLink{m[1], m[2], m[4], m[5]})
		}
		if m := dstarLinkUser.FindStringSubmatch(line); m != nil {
			l.users = append(l.users, dstarLink{m[1], m[2], m[4], ""})
		}
		if m := dstarCCSLink.FindStringSubmatch(line); m != nil {
			_, clock := dstarClock(m[1])
			l.ccs = append(l.ccs, dstarCCSRow{Repeater: m[3], Remote: m[4], Direction: m[5], Changed: clock})
		}
	}
	return l
}

var dstarTimers = []string{"Never", "Fixed", "5min", "10min", "15min", "20min", "25min", "30min", "60min", "90min", "120min", "180min", ""}

func dstarTimer(v string) string {
	n, err := strconv.Atoi(v)
	if err != nil || v == "" {
		n = 0
	}
	if n > 12 {
		n = 12
	}
	if n < 0 {
		return ""
	}
	return dstarTimers[n]
}

// dstarLinkRows renders active_reflector_links.php: per module the outgoing
// link (or Down), then incoming repeater links to it, then the incoming
// dongle users (which the classic page lists under every module).
func dstarLinkRows(cfg iniData, links dstarLinksLog) []dstarLinkRow {
	var rows []dstarLinkRow
	for _, b := range dstarBands(cfg) {
		key := func(name string) string { return ircddbValue(cfg, fmt.Sprintf("%s%d", name, b.index)) }
		auto := "No"
		if key("atStartup") == "1" {
			auto = "Auto"
		}
		row := dstarLinkRow{Radio: phpSubstr(b.call, 0, 8), Default: phpSubstr(key("reflector"), 0, 8), Auto: auto, Timer: dstarTimer(key("reconnect")),
			Status: "Down", Linked: "None", Mode: "--", Direction: "----", Changed: "----"}
		for _, l := range links.out {
			if l.rptr == b.call {
				_, clock := dstarClock(l.stamp)
				row.Status, row.Linked, row.Mode, row.Direction, row.Changed = "Up", phpSubstr(l.refl, 0, 8), l.proto, "Outgoing", clock
				break
			}
		}
		rows = append(rows, row)
		incoming := func(l dstarLink, linked string) {
			_, clock := dstarClock(l.stamp)
			rows = append(rows, dstarLinkRow{Incoming: true, Radio: phpSubstr(b.call, 0, 8), Status: "Up", Linked: phpSubstr(linked, 0, 8),
				Mode: l.proto, Direction: "Incoming", Changed: clock})
		}
		for _, l := range links.in {
			if l.rptr == b.call {
				incoming(l, l.refl)
			}
		}
		for _, l := range links.users {
			incoming(l, l.rptr)
		}
	}
	return rows
}

// STARnet.log: "2013-02-27 19:49:27: Adding DL5DI    to StarNet group STN001 A".
var dstarStarNetLine = regexp.MustCompile(`^(.{19}).*(Adding|Removing) (.{8}).*StarNet group (.{8}).*$`)

type starNetEvent struct {
	stamp, action, member, group string
}

func parseStarNetLog(r io.Reader) []starNetEvent {
	var out []starNetEvent
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 4096), 64<<10)
	for s.Scan() {
		if m := dstarStarNetLine.FindStringSubmatch(strings.TrimRight(s.Text(), "\r")); m != nil {
			out = append(out, starNetEvent{m[1], m[2], m[3], m[4]})
		}
	}
	return out
}

// dstarStarNetPanel renders active_starnet_groups.php: configured groups, and
// the members that were added and not removed since, in login order.
func dstarStarNetPanel(cfg iniData, events []starNetEvent) *dstarStarNet {
	sn := &dstarStarNet{}
	index := map[string]int{}
	for i := 1; i <= 5; i++ {
		key := func(name string) string { return ircddbValue(cfg, fmt.Sprintf("%s%d", name, i)) }
		name := key("starNetCallsign")
		if name == "" {
			continue
		}
		index[strings.TrimRight(phpSubstr(name, 0, 8), " ")] = i
		sn.Groups = append(sn.Groups, dstarStarNetGroup{Callsign: phpSubstr(name, 0, 8), Logoff: phpSubstr(key("starNetLogoff"), 0, 8),
			Info: key("starNetInfo"), UserTimeout: key("starNetUserTimeout"), GroupTimeout: key("starNetGroupTimeout")})
	}
	if len(sn.Groups) == 0 {
		return nil
	}
	type member struct{ group, call, stamp string }
	byGroup := map[int][]member{}
	for _, e := range events {
		guid, ok := index[strings.TrimRight(e.group, " ")]
		if !ok {
			continue
		}
		list := byGroup[guid]
		at := -1
		for i, m := range list {
			if m.group == e.group && m.call == e.member {
				at = i
			}
		}
		switch {
		case e.action == "Adding" && at >= 0:
			list[at].stamp = e.stamp
		case e.action == "Adding":
			list = append(list, member{e.group, e.member, e.stamp})
		case at >= 0:
			list = append(list[:at], list[at+1:]...)
		}
		byGroup[guid] = list
	}
	for i := 1; i <= 5; i++ {
		for _, m := range byGroup[i] {
			_, clock := dstarClock(m.stamp)
			sn.Members = append(sn.Members, dstarStarNetMember{Clock: clock, Group: m.group, Callsign: strings.ReplaceAll(m.call, " ", "")})
		}
	}
	return sn
}

// Headers.log lines, as matched by last_herd.php and local_tx.php:
//
//	2012-06-05 12:18:41: DCS header - My: PU2ZHZ  /T     Your: CQCQCQ    Rpt1: PU2ZHZ B  Rpt2: DCS007 B  Flags: 00 00 00
var dstarHeaderLine = regexp.MustCompile(`^(.{19}).*My: (.*).*Your: (.*).*Rpt1: (.*).*Rpt2: (.*).*Flags: (.*)$`)

const (
	dstarHeardMax = 20  // last_herd.php: head -20
	dstarLocalMax = 500 // local_tx.php has no limit; bound the JSON
)

// A long Headers.log is read from its last dstarHeadersPrim bytes on attach.
var dstarHeadersPrim int64 = 32 << 20

type dstarHeader struct {
	line string
	row  dstarHeardRow
}

func parseHeaderLine(line string) (dstarHeader, string, bool) {
	m := dstarHeaderLine.FindStringSubmatch(line)
	if m == nil {
		return dstarHeader{}, "", false
	}
	call := phpSubstr(m[2], 0, 8)
	at, clock := dstarClock(m[1])
	lookup := ""
	if f := strings.Fields(call); len(f) > 0 {
		lookup = f[0]
	}
	row := dstarHeardRow{Time: at, Clock: clock, Callsign: strings.ReplaceAll(call, " ", ""), ID: strings.ReplaceAll(phpSubstr(m[2], 9, 4), " ", ""),
		Lookup: lookup, Your: phpSubstr(m[3], 0, 8), RPT1: phpSubstr(m[4], 0, 8), RPT2: phpSubstr(m[5], 0, 8)}
	// sort -u -k7,8: one entry per callsign and suffix.
	return dstarHeader{line, row}, strings.TrimRight(phpSubstr(m[2], 0, 13), " "), true
}

// headersLog follows Headers.log by offset: the newest header per station for
// the Gateway Activity list (without "  /TIME" announcements) and for the
// Local RF Activity list ("Repeater header" lines only).
type headersLog struct {
	mu           sync.Mutex
	path         string
	info         os.FileInfo
	offset       int64
	rest         []byte
	heard, local map[string]dstarHeader
	dirty        bool
	outH, outL   []dstarHeardRow
}

var dstarHeaders = &headersLog{}

func (h *headersLog) reset(path string) {
	h.path, h.info, h.offset, h.rest = path, nil, 0, nil
	h.heard, h.local, h.dirty = map[string]dstarHeader{}, map[string]dstarHeader{}, true
}

func (h *headersLog) add(line string) {
	line = strings.TrimRight(line, "\r")
	e, key, ok := parseHeaderLine(line)
	if !ok {
		return
	}
	keep := func(m map[string]dstarHeader) {
		if old, seen := m[key]; !seen || line >= old.line {
			m[key] = e
			h.dirty = true
		}
	}
	if !strings.Contains(line, "  /TIME") {
		keep(h.heard)
	}
	if strings.Contains(line, "Repeater header") {
		keep(h.local)
	}
}

func (h *headersLog) rows(path string) (heard, local []dstarHeardRow) {
	h.mu.Lock()
	defer h.mu.Unlock()
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() {
		h.reset(path)
		h.dirty = false
		return nil, nil
	}
	if path != h.path || h.info == nil || !os.SameFile(h.info, st) || st.Size() < h.offset {
		h.reset(path)
	}
	if st.Size() > h.offset {
		h.read(st)
	}
	h.info = st
	if h.dirty {
		h.outH, h.outL, h.dirty = newestHeaders(h.heard, dstarHeardMax), newestHeaders(h.local, dstarLocalMax), false
	}
	return h.outH, h.outL
}

func (h *headersLog) read(st os.FileInfo) {
	f, err := os.Open(h.path)
	if err != nil {
		return
	}
	defer f.Close()
	skip := false
	if h.offset == 0 && st.Size() > dstarHeadersPrim {
		h.offset, skip = st.Size()-dstarHeadersPrim, true
	}
	if _, err := f.Seek(h.offset, io.SeekStart); err != nil {
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, st.Size()-h.offset))
	if err != nil {
		return
	}
	h.offset += int64(len(data))
	data = append(h.rest, data...)
	cut := strings.LastIndexByte(string(data), '\n')
	if cut < 0 {
		h.rest = data
		return
	}
	text := string(data[:cut])
	h.rest = append([]byte(nil), data[cut+1:]...)
	if skip {
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			text = text[i+1:]
		} else {
			text = ""
		}
	}
	for _, line := range strings.Split(text, "\n") {
		h.add(line)
	}
}

func newestHeaders(m map[string]dstarHeader, limit int) []dstarHeardRow {
	list := make([]dstarHeader, 0, len(m))
	for _, e := range m {
		list = append(list, e)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].line > list[j].line }) // sort -r
	if len(list) > limit {
		list = list[:limit]
	}
	out := make([]dstarHeardRow, len(list))
	for i, e := range list {
		out[i] = e.row
	}
	return out
}

func dstarLogPath(name string) string { return filepath.Join(pistarLogDir, name) }

// dstarDashboard returns the D-Star panels for the configured controller, or
// nil when the classic page shows none.
func dstarDashboard(controller string, host, cfg iniData) *dstarPanels {
	p := &dstarPanels{}
	switch controller {
	case "dstarrepeater":
		p.Network = true
		p.Gateway = dstarGatewayConfig(cfg)
		p.Heard, p.Local = dstarHeaders.rows(dstarLogPath("Headers.log"))
	case "mmdvmhost":
		if !host.on("D-Star Network", "Enable") {
			return nil
		}
		p.Network = true
	default:
		return nil
	}
	links, _ := cachedHostFile("dstar-links", dstarLogPath("Links.log"), parseLinksLog)
	p.Links = dstarLinkRows(cfg, links)
	p.CCSShow, p.CCS = links.ccsLine, links.ccs
	var events []starNetEvent
	if ircddbStarNetSet(cfg) {
		events, _ = cachedHostFile("dstar-starnet", dstarLogPath("STARnet.log"), parseStarNetLog)
	}
	p.StarNet = dstarStarNetPanel(cfg, events)
	return p
}

func ircddbStarNetSet(cfg iniData) bool {
	for i := 1; i <= 5; i++ {
		if ircddbValue(cfg, fmt.Sprintf("starNetCallsign%d", i)) != "" {
			return true
		}
	}
	return false
}
