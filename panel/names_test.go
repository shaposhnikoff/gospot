package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
)

// pistar.uk/downloads/DMRIds.dat.gz lines (ID TAB callsign TAB first name).
const gospotDMRIds = "1023001\tVE3THW\tWayne\n1023002\tVA3ECM\tMathieu\n" +
	"# comment\n\n" +
	"2550430\tut3udx\tMax\r\n" + // CRLF, lower-case callsign (CUserDB upper-cases)
	"2550431\tUT3UDX\tMaksym\n" + // second ID of the same callsign
	"3130665\tKM6SMV\tSarah\n" +
	"3130665\tKM6SMV\tSara\n" + // repeated ID: the later line wins
	"notanid\tX1X\tBad\n" +
	"0\tZERO1\tZero\n" +
	"99999999999\tBIG1\tTooBig\n" +
	"4000001\t\tNoCall\n" +
	"4000002\tN0NAME\n" + // no name column
	"4000003\tM1ABC\t  Jo\x07hn \t\n"

func TestParseUserDBClassicFormat(t *testing.T) {
	db := parseUserDB(strings.NewReader(gospotDMRIds))
	for from, want := range map[string]string{
		"1023001": "Wayne", "VA3ECM": "Mathieu", "2550430": "Max", "2550431": "Maksym",
		"UT3UDX": "Max", "ut3udx": "Max", "UT3UDX-1": "Max", "UT3UDX/P": "Max",
		"3130665": "Sara", "KM6SMV": "Sarah", // byCall keeps the first listed entry
		"4000002": "", "N0NAME": "", "M1ABC": "John",
		"notanid": "", "0": "", "4000001": "", "": "", "N0CALL": "", "1": "",
	} {
		if got := db.name(from); got != want {
			t.Errorf("name(%q) = %q, want %q", from, got, want)
		}
	}
	if len(db.byID) != 7 {
		t.Errorf("byID has %d entries: %v", len(db.byID), db.byID)
	}
	var nilDB *userDB
	if nilDB.name("1023001") != "" {
		t.Fatal("missing table must resolve nothing")
	}
}

func TestParseUserDBHeaderAndCSV(t *testing.T) {
	// radioid.net user.csv as MMDVMHost reads it: header names the columns.
	csv := "RADIO_ID,CALLSIGN,FIRST_NAME,LAST_NAME,CITY,STATE,COUNTRY\n" +
		"1023001,VE3THW,Wayne,Smith,Toronto,Ontario,Canada\n" +
		"2550430,UT3UDX,Max,,Kyiv,,Ukraine\n"
	db := parseUserDB(strings.NewReader(csv))
	if db.name("1023001") != "Wayne Smith" || db.name("UT3UDX") != "Max" || db.name("RADIO_ID") != "" {
		t.Fatalf("csv header: %v", db.byID)
	}
	// Reordered columns are followed through the header.
	db = parseUserDB(strings.NewReader("CALLSIGN\tRADIO_ID\tFIRST_NAME\nM1ABC\t2341001\tAnn\n"))
	if db.name("2341001") != "Ann" || db.name("M1ABC") != "Ann" {
		t.Fatalf("reordered header: %v", db.byID)
	}
	// Without a header the first line is data (RADIO_ID, CALLSIGN, FIRST_NAME).
	db = parseUserDB(strings.NewReader("1023001,VE3THW,Wayne\n1023002,VA3ECM,Mathieu\n"))
	if db.name("1023001") != "Wayne" || db.name("VA3ECM") != "Mathieu" {
		t.Fatalf("headerless csv: %v", db.byID)
	}
	if db := parseUserDB(strings.NewReader("")); len(db.byID) != 0 || db.name("1") != "" {
		t.Fatal("empty file")
	}
}

func TestCleanName(t *testing.T) {
	long := strings.Repeat("A", 60)
	for in, want := range map[string]string{
		"Wayne": "Wayne", "  Jo  hn ": "Jo hn", "a\x00b\x1bc": "abc", "Józef": "Józef",
		long: long[:nameMaxRunes], "<script>": "<script>", "‮evil": "evil",
	} {
		if got := cleanName(in); got != want {
			t.Errorf("cleanName(%q) = %q, want %q", in, got, want)
		}
	}
	if n := len([]rune(cleanName(strings.Repeat("Ж ", 50)))); n > nameMaxRunes {
		t.Fatalf("bounded name has %d runes", n)
	}
}

func TestCallKey(t *testing.T) {
	same := [][]string{{"M1ABC", "m1abc", "M1ABC/P", "M1ABC-1", "M1ABC  B", " M1ABC "}, {"UT3UDX", "ut3udx"}}
	for _, group := range same {
		for _, c := range group {
			if callKey(c) == 0 || callKey(c) != callKey(group[0]) {
				t.Errorf("callKey(%q) != callKey(%q)", c, group[0])
			}
		}
	}
	for _, bad := range []string{"", "TOOLONGCALLSIGN", "A$B", "-M1ABC", "Ж1ABC"} {
		if callKey(bad) != 0 {
			t.Errorf("callKey(%q) should be 0", bad)
		}
	}
	// Distinct callsigns never collide, including prefixes and digits/letters.
	seen := map[uint64]string{}
	for _, c := range []string{"A", "AA", "A0", "0A", "Z9", "9Z", "ZZZZZZZZZZ", "9999999999", "M1ABC", "M1AB", "M1ABD", "1"} {
		if prev, dup := seen[callKey(c)]; dup {
			t.Fatalf("%q and %q share a key", c, prev)
		}
		seen[callKey(c)] = c
	}
}

const testTGList = "# TGList_BM.txt downloaded from http://www.pistar.uk/downloads/TGList_BM.txt\n" +
	"# Dest ID;Option;Name;Description\n#\n" +
	"91;0;WORLD-WIDE;TG91\n255;0;UKRAINE;TG255\n4000;1;DISCONNECT;REF4000\n9990;2;PARROT;PC9990\n" +
	"255;0;DUPLICATE;TG255\n" + // first entry wins
	"bad;0;X;Y\n;0;EMPTY;\n12;0;;\n" +
	"10200;North America\n" + // id;name lists
	"\r\n  25506;0;ZHYTOMYRSKA-OBL;TG25506  \r\n"

func TestParseTGList(t *testing.T) {
	got := parseTGList(strings.NewReader(testTGList))
	want := map[uint32]string{91: "WORLD-WIDE", 255: "UKRAINE", 4000: "DISCONNECT", 9990: "PARROT", 10200: "North America", 25506: "ZHYTOMYRSKA-OBL"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for id, name := range want {
		if got[id] != name {
			t.Errorf("%d = %q, want %q", id, got[id], name)
		}
	}
}

func withHostsDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old := gospotHostsDir
	gospotHostsDir = dir
	t.Cleanup(func() { gospotHostsDir = old })
	return dir
}

func writeHostFile(t *testing.T, path, body string, mod time.Time) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func TestBackgroundUserDBReloadsOnChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "DMRIds.dat")
	b := &backgroundUserDB{parse: parseUserDB}
	if b.get(path) != nil {
		t.Fatal("missing file must give no table")
	}
	b.wait()
	start := time.Date(2026, 11, 27, 4, 0, 0, 0, time.UTC)
	writeHostFile(t, path, "2550430\tUT3UDX\tMax\n", start)
	// The first request starts the parse and is not held up by it.
	if db := b.get(path); db != nil && db.name("UT3UDX") != "Max" {
		t.Fatal("unexpected table before the first parse")
	}
	b.wait()
	if db := b.get(path); db.name("UT3UDX") != "Max" {
		t.Fatalf("first version: %v", db)
	}
	// Same version: no new parse.
	parses := 0
	b.parse = func(r io.Reader) *userDB { parses++; return parseUserDB(r) }
	for i := 0; i < 5; i++ {
		b.get(path)
	}
	b.wait()
	if parses != 0 {
		t.Fatalf("unchanged file parsed %d times", parses)
	}
	// HostFilesUpdate.sh replaced the file: the old table is served until
	// the new one is parsed, then the new one.
	writeHostFile(t, path, "2550430\tUT3UDX\tMaksym\n", start.Add(24*time.Hour))
	if db := b.get(path); db == nil || db.name("UT3UDX") != "Max" {
		t.Fatalf("while reloading the previous table stays: %v", db)
	}
	b.wait()
	if db := b.get(path); db.name("UT3UDX") != "Maksym" || parses != 1 {
		t.Fatalf("after reload: %v, %d parses", db, parses)
	}
	// Same mtime, different size is a new version too (cachedHostFile key).
	writeHostFile(t, path, "2550430\tUT3UDX\tMaksym S\n", start.Add(24*time.Hour))
	b.get(path)
	b.wait()
	if db := b.get(path); db.name("2550430") != "Maksym S" {
		t.Fatalf("size change: %v", db)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if b.get(path) != nil {
		t.Fatal("deleted file must drop the table")
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if b.get(path) != nil {
		t.Fatal("a directory is not a list")
	}
}

func TestHostNamesLookups(t *testing.T) {
	dir := withHostsDir(t)
	old := operatorNames
	operatorNames = &backgroundUserDB{parse: parseUserDB}
	t.Cleanup(func() { operatorNames = old })
	mod := time.Date(2026, 11, 27, 4, 0, 0, 0, time.UTC)

	// Nothing installed yet: every lookup is empty, nothing fails.
	h := currentHostNames()
	operatorNames.wait()
	if h.operator("UT3UDX") != "" || h.target("DMR", "TG 91") != "" || h.target("DMR", "9990") != "" {
		t.Fatal("missing files must resolve nothing")
	}

	writeHostFile(t, filepath.Join(dir, "DMRIds.dat"), gospotDMRIds, mod)
	writeHostFile(t, filepath.Join(dir, "TGList_BM.txt"), testTGList, mod)
	writeHostFile(t, filepath.Join(dir, "TGList_P25.txt"), "10200;0;North America;TG10200\n", mod)
	writeHostFile(t, filepath.Join(dir, "TGList_NXDN.txt"), "65000;0;World Wide;TG65000\n", mod)
	currentHostNames()
	operatorNames.wait()
	h = currentHostNames()
	for _, c := range []struct{ mode, to, want string }{
		{"DMR", "TG 91", "WORLD-WIDE"}, {"DMR", "TG 255", "UKRAINE"}, {"DMR", "TG 1", ""},
		{"DMR", "9990", "PARROT"}, {"DMR", "3130665", "Sara"}, {"DMR", "KM6SMV", "Sarah"}, {"DMR", "4000", "DISCONNECT"},
		{"P25", "TG 10200", "North America"}, {"NXDN", "TG 65000", "World Wide"}, {"NXDN", "TG 91", ""},
		{"YSF", "ALL", ""}, {"D-Star", "CQCQCQ", ""}, {"YSF", "TG 91", ""}, {"DMR", "TG x", ""},
	} {
		if got := h.target(c.mode, c.to); got != c.want {
			t.Errorf("target(%s, %s) = %q, want %q", c.mode, c.to, got, c.want)
		}
	}
	if h.operator("UT3UDX") != "Max" || h.operator("1023001") != "Wayne" {
		t.Fatal("operator names")
	}
	// TG lists are cached per version and re-read after a change.
	writeHostFile(t, filepath.Join(dir, "TGList_BM.txt"), "91;0;WORLDWIDE-NEW;TG91\n", mod.Add(time.Hour))
	if got := currentHostNames().target("DMR", "TG 91"); got != "WORLDWIDE-NEW" {
		t.Fatalf("TG list reload: %q", got)
	}
}

// A DMRIds.dat as large as the current pistar.uk download and beyond.
func TestUserDBLargeFileMemoryAndLatency(t *testing.T) {
	const lines = 250_000
	var buf bytes.Buffer
	buf.Grow(lines * 24)
	for i := 0; i < lines; i++ {
		fmt.Fprintf(&buf, "%d\tC%05dX\tName%d\n", 1000000+i, i, i%5000)
	}
	path := filepath.Join(t.TempDir(), "DMRIds.dat")
	writeHostFile(t, path, buf.String(), time.Now().Add(-time.Hour))
	size := buf.Len()
	buf = bytes.Buffer{}

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	b := &backgroundUserDB{parse: parseUserDB}
	begin := time.Now()
	if b.get(path) != nil {
		t.Fatal("first request must not wait for the parse")
	}
	if wait := time.Since(begin); wait > 50*time.Millisecond {
		t.Fatalf("first request blocked for %v", wait)
	}
	b.wait()
	parse := time.Since(begin)
	db := b.get(path)
	runtime.GC()
	runtime.ReadMemStats(&after)
	retained := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	t.Logf("%d lines, %d bytes: parsed in %v, retained heap %.1f MiB", lines, size, parse, float64(retained)/(1<<20))
	if len(db.byID) != lines || len(db.byCall) != lines {
		t.Fatalf("loaded %d ids, %d callsigns", len(db.byID), len(db.byCall))
	}
	if parse > 5*time.Second {
		t.Fatalf("parse took %v", parse)
	}
	if retained > 24<<20 {
		t.Fatalf("table retains %d bytes", retained)
	}
	// Lookups for a dashboard build (40 rows) cost microseconds.
	probes := []string{"1000000", "1249999", "C12345X", "C00000X", "N0CALL", "9999999"}
	begin = time.Now()
	const rounds = 200_000
	for i := 0; i < rounds; i++ {
		db.name(probes[i%len(probes)])
	}
	per := time.Since(begin) / rounds
	t.Logf("lookup %v", per)
	if per > 5*time.Microsecond {
		t.Fatalf("lookup takes %v", per)
	}
	if db.name("1249999") != "Name4999" || db.name("C12345X") != "Name2345" {
		t.Fatal("wrong lookup result")
	}
	runtime.KeepAlive(db)
}

type stubNames map[string]string

func (s stubNames) operator(from string) string   { return s["op:"+from] }
func (s stubNames) target(mode, to string) string { return s["tg:"+mode+":"+to] }

// /api/dashboard keeps every field it had before the kiosk, plus the names.
func TestDashboardJSONShapeWithNames(t *testing.T) {
	in := testInputs()
	dur, ber := 3.2, 0.1
	rf := HeardRow{At: in.now.Add(-time.Minute), Source: "RF", From: "UT3UDX", To: "TG 255", Slot: "2", Mode: "DMR Slot 2", Duration: &dur, BER: &ber}
	net := HeardRow{At: in.now.Add(-2 * time.Second), Source: "Net", From: "2341234", To: "TG 91", Slot: "1", Mode: "DMR Slot 1"}
	page := HeardRow{At: in.now.Add(-time.Hour), Mode: "POCSAG", Source: "Net", From: "DAPNET", To: "DAPNET User", Note: "POCSAG Data"}
	in.rows, in.active = []HeardRow{net, rf, page}, []HeardRow{net}
	in.names = stubNames{"op:UT3UDX": "Max", "tg:DMR:TG 255": "UKRAINE", "tg:DMR:TG 91": "WORLD-WIDE", "op:DAPNET": "never"}
	out, err := json.Marshal(buildDashboard(in))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	keys := func(m map[string]json.RawMessage) []string {
		var k []string
		for key := range m {
			k = append(k, key)
		}
		sort.Strings(k)
		return k
	}
	top := []string{"Callsign", "Controller", "DMR", "DStar", "DStarInfo", "Firmware", "Heard", "HeardReady", "Hostname", "Local", "M17", "Modes",
		"NXDN", "Networks", "Now", "P25", "POCSAG", "RX", "Release", "TCXO", "TRX", "TX", "Theme", "TimeZone", "Version", "YSF"}
	if got := keys(doc); !slices.Equal(got, top) {
		t.Fatalf("top-level keys changed:\n got %v\nwant %v", got, top)
	}
	var heard []map[string]json.RawMessage
	if err := json.Unmarshal(doc["Heard"], &heard); err != nil || len(heard) != 3 {
		t.Fatalf("heard: %v %s", err, doc["Heard"])
	}
	rowKeys := []string{"ber", "callsign", "clock", "duration", "live", "loss", "mode", "name", "note", "rssi", "source", "suffix", "target", "targetName", "time"}
	for _, row := range heard {
		if got := keys(row); !slices.Equal(got, rowKeys) {
			t.Fatalf("row keys:\n got %v\nwant %v", got, rowKeys)
		}
	}
	str := func(row map[string]json.RawMessage, key string) string {
		var s string
		_ = json.Unmarshal(row[key], &s)
		return s
	}
	if str(heard[0], "callsign") != "2341234" || str(heard[0], "name") != "" || str(heard[0], "targetName") != "WORLD-WIDE" || string(heard[0]["live"]) != "true" {
		t.Fatalf("live net row: %v", heard[0])
	}
	if str(heard[1], "name") != "Max" || str(heard[1], "targetName") != "UKRAINE" || string(heard[1]["duration"]) != "3.2" || string(heard[1]["ber"]) != "0.1" {
		t.Fatalf("rf row: %v", heard[1])
	}
	if str(heard[2], "name") != "" || str(heard[2], "note") != "POCSAG Data" {
		t.Fatalf("POCSAG rows are not looked up: %v", heard[2])
	}
	var local []map[string]json.RawMessage
	if err := json.Unmarshal(doc["Local"], &local); err != nil || len(local) != 1 || str(local[0], "name") != "Max" {
		t.Fatalf("local: %s", doc["Local"])
	}
	// Without lookups (nil names) the fields are present and empty.
	in.names = nil
	d := buildDashboard(in)
	if d.Heard[1].Name != "" || d.Heard[1].TargetName != "" {
		t.Fatal("nil names must leave the fields empty")
	}
}
