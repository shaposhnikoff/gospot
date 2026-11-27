package main

import (
	"testing"
	"time"
)

// Sample lines are the ones documented in the classic functions.php.
func TestHeardAllModes(t *testing.T) {
	base := time.Date(2026, 11, 27, 6, 0, 0, 0, time.UTC)
	h := heardHistory{open: map[heardLane]HeardRow{}}
	at := func(s float64) time.Time { return base.Add(time.Duration(s * float64(time.Second))) }
	feed := []struct {
		at   float64
		line string
	}{
		{0, "M: 2026-11-27 06:00:00.000 D-Star, received RF header from M1ABC   /ABCD to CQCQCQ  "},
		{2, "M: 2026-11-27 06:00:02.000 D-Star, received RF end of transmission from M1ABC   /ABCD to CQCQCQ  , 2.0 seconds, BER: 0.4%, RSSI: -43/-43/-43 dBm"},
		{10, "M: 2026-11-27 06:00:10.000 YSF, received network data from M1ABC      to ALL        at MB6IBK"},
		{15, "M: 2026-11-27 06:00:15.000 YSF, network watchdog has expired, 5.0 seconds, 0% packet loss, BER: 0.0%"},
		{20, "M: 2026-11-27 06:00:20.000 P25, received network transmission from 10999 to TG 10200"},
		{22, "M: 2026-11-27 06:00:22.000 P25, network end of transmission, 1.8 seconds, 0% packet loss"},
		{30, "M: 2026-11-27 06:00:30.000 NXDN, received RF transmission from MW0MWZ to TG 65000"},
		{30.4, "M: 2026-11-27 06:00:30.400 NXDN, received RF end of transmission, 0.4 seconds, BER: 0.0%"},
		{40, "M: 2026-11-27 06:00:40.000 M17, received RF late entry voice transmission from M1ABC to INFO"},
		{42, "M: 2026-11-27 06:00:42.100 M17, received RF end of transmission from M1ABC to INFO, 2.1 seconds, BER: 0.2%, RSSI: -60/-60/-60 dBm"},
		{50, "M: 2026-11-27 06:00:50.000 YSF, received RF data from MW0MWZ     to ALL"},
		{60, "M: 2026-11-27 06:01:00.000 YSF, RF user has timed out"},
		{70, "M: 2026-11-27 06:01:10.000 POCSAG, transmitted 1 frame(s) of data from 1 message(s)"},
		{80, "M: 2026-11-27 06:01:20.000 P25, received RF transmission from M1ABC to TG 10200"},
	}
	for _, f := range feed {
		h.observe(at(f.at), f.line)
	}
	h.trim(at(90))
	type want struct {
		mode, from, suffix, to, source, note string
		dur, ber, loss, rssi                 float64 // -1: not set
	}
	num := func(p *float64) float64 {
		if p == nil {
			return -1
		}
		return *p
	}
	expect := []want{
		{"P25", "M1ABC", "", "TG 10200", "RF", "", -1, -1, -1, -1}, // still open
		{"POCSAG", "DAPNET", "", "DAPNET User", "Net", "POCSAG Data", -1, -1, -1, -1},
		{"YSF", "MW0MWZ", "", "ALL", "RF", "TOut", -1, -1, -1, -1},
		{"M17", "M1ABC", "", "INFO", "RF", "", 2.1, 0.2, -1, -60},
		{"NXDN", "MW0MWZ", "", "TG 65000", "RF", "", 0.4, 0, -1, -1},
		{"P25", "MMDVM", "", "TG 10200", "Net", "", 1.8, -1, 0, -1},
		{"YSF", "M1ABC", "", "ALL", "Net", "", 5, 0, 0, -1},
		{"D-Star", "M1ABC", "ABCD", "CQCQCQ", "RF", "", 2, 0.4, -1, -43},
	}
	if len(h.rows) != len(expect) {
		t.Fatalf("rows: %+v", h.rows)
	}
	for i, w := range expect {
		r := h.rows[i]
		got := want{r.Mode, r.From, r.Suffix, r.To, r.Source, r.Note, num(r.Duration), num(r.BER), num(r.Loss), num(r.RSSI)}
		if got != w {
			t.Errorf("row %d: got %+v, want %+v", i, got, w)
		}
	}
	if len(h.open) != 1 {
		t.Fatalf("open calls: %+v", h.open)
	}
}

func TestHeardRejectsLongCallsign(t *testing.T) {
	h := heardHistory{open: map[heardLane]HeardRow{}}
	h.observe(time.Now(), "M: 2026-11-27 06:00:00.000 P25, received RF transmission from ABCDEFGHIJKL to TG 1")
	if len(h.rows) != 0 {
		t.Fatalf("%+v", h.rows)
	}
}

func TestDashboardClassicDedup(t *testing.T) {
	in := testInputs()
	one, two := 1.0, 2.0
	in.rows = []HeardRow{
		{At: in.now.Add(-10 * time.Second), Source: "Net", From: "M0ABC", To: "TG 91", Slot: "2", Mode: "DMR Slot 2", Duration: &one},
		{At: in.now.Add(-60 * time.Second), Source: "Net", From: "M0ABC", To: "TG 235", Slot: "2", Mode: "DMR Slot 2", Duration: &two},
		{At: in.now.Add(-70 * time.Second), Source: "RF", From: "M0ABC", To: "TG 91", Slot: "2", Mode: "DMR Slot 2", Duration: &two},
		{At: in.now.Add(-80 * time.Second), Source: "Net", From: "DAPNET", To: "DAPNET User", Mode: "POCSAG", Note: "POCSAG Data"},
	}
	d := buildDashboard(in)
	if len(d.Heard) != 3 || d.Heard[0].Target != "TG 91" || d.Heard[1].Source != "RF" || d.Heard[2].Note != "POCSAG Data" {
		t.Fatalf("heard: %+v", d.Heard)
	}
	if len(d.Local) != 1 || d.Local[0].Source != "RF" {
		t.Fatalf("local: %+v", d.Local)
	}
}
