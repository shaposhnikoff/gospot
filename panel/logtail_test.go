package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestLogFileFollow(t *testing.T) {
	dir := t.TempDir()
	day1 := time.Date(2026, 11, 26, 23, 59, 0, 0, time.UTC)
	day2 := day1.Add(2 * time.Minute)
	f := &logFile{dir: dir, root: "MMDVM", unit: "u"}
	write := func(day time.Time, text string, appendTo bool) {
		flags := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
		if appendTo {
			flags = os.O_CREATE | os.O_WRONLY | os.O_APPEND
		}
		file, err := os.OpenFile(f.name(day), flags, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		file.WriteString(text)
		file.Close()
	}
	var got []string
	emit := func(unit, line string, at time.Time) { got = append(got, at.Format("15:04:05")+" "+line[27:]) }

	write(day1, "M: 2026-11-26 23:58:00.000 first\nnot a log line\nM: 2026-11-26 23:58:30.5", false)
	f.poll(day1, emit)
	if len(got) != 1 || got[0] != "23:58:00 first" {
		t.Fatalf("prime: %q", got)
	}
	write(day1, "00 second\n", true) // completes the partial line
	f.poll(day1, emit)
	if len(got) != 2 || got[1] != "23:58:30 second" {
		t.Fatalf("append: %q", got)
	}
	write(day1, "M: 2026-11-26 23:59:59.000 last of day\n", true)
	write(day2, "M: 2026-11-27 00:00:01.000 new day\n", false)
	f.poll(day2, emit)
	if len(got) != 4 || got[2] != "23:59:59 last of day" || got[3] != "00:00:01 new day" {
		t.Fatalf("rotation: %q", got)
	}
	if filepath.Base(f.path) != "MMDVM-2026-11-27.log" {
		t.Fatal(f.path)
	}
	f.poll(day2, emit)
	if len(got) != 4 {
		t.Fatalf("no new data must emit nothing: %q", got)
	}
}

func TestLogFileFeedsHeardAndFirmware(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	stamp := func(d time.Duration) string { return now.Add(d).Format("2006-01-02 15:04:05.000") }
	f := &logFile{dir: dir, root: "MMDVM", unit: modemUnit}
	os.WriteFile(f.name(now), []byte(
		"I: "+stamp(-time.Hour)+" MMDVM protocol version: 1, description: MMDVM_HS_Hat-v1.4.17 20190529 14.7456MHz ADF7021\n"+
			"M: "+stamp(-10*time.Second)+" DMR Slot 2, received RF voice header from M0ABC to TG 91\n"+
			"M: "+stamp(-5*time.Second)+" DMR Slot 2, received RF end of voice transmission from M0ABC to TG 91, 5.0 seconds, BER: 0.4%, RSSI: -83/-83/-83 dBm\n"), 0o644)
	recentHeard.mu.Lock()
	recentHeard.rows, recentHeard.open, recentHeard.ok = nil, nil, false
	recentHeard.mu.Unlock()
	primeFirmwareFiles(f, now)
	f.poll(now, func(unit, line string, at time.Time) {
		applyRecord(followRecord{Message: line, Unit: unit, Invocation: "file", Stamp: itoa(at.UnixMicro())}, now)
	})
	fw, tcxo, _ := journal.snapshot()
	rows, _, ok := recentHeard.live()
	if fw != "HS_Hat:v1.4.17" || tcxo != "14.7456 MHz" || !ok || len(rows) != 1 || rows[0].From != "M0ABC" || rows[0].Duration == nil || *rows[0].Duration != 5 {
		t.Fatalf("fw=%q tcxo=%q ok=%v rows=%+v", fw, tcxo, ok, rows)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
