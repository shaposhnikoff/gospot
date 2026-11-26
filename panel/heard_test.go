package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func heardRecord(t *testing.T, at time.Time, msg string) []byte {
	t.Helper()
	stamp := strconv.FormatInt(at.UnixMicro(), 10)
	b, err := json.Marshal(map[string]string{"__CURSOR": stamp + msg, "__REALTIME_TIMESTAMP": stamp, "MESSAGE": msg})
	if err != nil {
		t.Fatal(err)
	}
	return append(b, '\n')
}

func TestHeardEvents(t *testing.T) {
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	var log bytes.Buffer
	const header = "DMR Slot 1, received RF voice header from G4KLX/P to TG 91"
	log.Write(heardRecord(t, now.Add(-2*time.Minute), "M: 2026-11-24 11:58:00.000 "+header))
	log.Write(heardRecord(t, now.Add(-time.Minute), "DMR Slot 2, received network late entry from 2345001 to G4KLX"))
	log.Write(heardRecord(t, now, "DMR Slot 2, received network data header from M0ABC to TG 9, 3 blocks"))
	log.Write(heardRecord(t, now.Add(-25*time.Hour), header))
	log.Write(heardRecord(t, now.Add(time.Minute), header))
	for _, msg := range []string{
		"MQTT " + header,
		header + "\nSECRET",
		header + ", 3 blocks",
		strings.Replace(header, "G4KLX/P", "<private>", 1),
		strings.Replace(header, "G4KLX/P", strings.Repeat("A", 32), 1),
		strings.Replace(header, "Slot 1", "Slot 3", 1),
		strings.Replace(header, "voice header", "end of voice transmission", 1) + ", 2.2 seconds, BER: 0.0%",
	} {
		log.Write(heardRecord(t, now, msg))
	}
	log.WriteString(`{"__REALTIME_TIMESTAMP":"123","MESSAGE":[0,1,2]}`)
	want := []HeardRow{
		{At: now, Source: "Net", From: "M0ABC", To: "TG 9", Slot: "2"},
		{At: now.Add(-time.Minute), Source: "Net", From: "2345001", To: "G4KLX", Slot: "2"},
		{At: now.Add(-2 * time.Minute), Source: "RF", From: "G4KLX/P", To: "TG 91", Slot: "1"},
	}
	h := heardHistory{}
	h.ingest(log.Bytes(), now)
	if got := h.rows; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v; want %+v", got, want)
	}
}

func TestHeardParallelCalls(t *testing.T) {
	base := time.Unix(1800000000, 0).UTC()
	var out bytes.Buffer
	add := func(seconds int, msg string) {
		out.Write(heardRecord(t, base.Add(time.Duration(seconds)*time.Second), msg))
	}
	// Same parties on two slots and in both directions.
	add(0, "DMR Slot 1, received RF voice header from M0ABC to TG 91")
	add(1, "DMR Slot 1, received network late entry from M0ABC to TG 91")
	add(2, "DMR Slot 2, received network voice header from M0ABC to TG 91")
	add(3, "DMR Slot 2, received RF late entry from M0ABC to TG 91")
	add(3, "DMR Slot 2, RF voice transmission lost from M0ABC to TG 91, 0.0 seconds, BER: 0.0%")
	add(4, "DMR Slot 1, received RF end of voice transmission from M0ABC to TG 91, 4.0 seconds, BER: 0.7%, RSSI: -109/-97/-102 dBm")
	add(5, "DMR Slot 2, network watchdog has expired, 2.5 seconds, 2% packet loss, BER: 1.3%")
	add(6, "DMR Slot 1, received network end of voice transmission from M0ABC to TG 91, 5.0 seconds, 0% packet loss, BER: 0.0%")
	// Orphan end cannot overwrite a completed call.
	add(7, "DMR Slot 1, RF voice transmission lost from M0ABC to TG 91, 7.0 seconds, BER: 9.9%")
	add(8, "DMR Slot 1, received RF data header from M0ABC to TG 91, 2 blocks")
	add(9, "DMR Slot 1, ended RF data transmission")
	h := heardHistory{}
	h.ingest(out.Bytes(), base.Add(time.Minute))
	if len(h.rows) != 5 {
		t.Fatalf("expected five transmissions; got %+v", h.rows)
	}
	// Check the public numeric JSON contract, including null vs zero.
	want := []string{
		`[null,null,null,null]`, `[0,0,null,null]`, `[2.5,1.3,2,null]`, `[5,0,0,null]`, `[4,0.7,null,-102]`,
	}
	for i, row := range h.rows {
		raw, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		var nums []string
		for _, key := range []string{"Duration", "BER", "Loss", "RSSI"} {
			nums = append(nums, string(fields[key]))
		}
		if got := "[" + strings.Join(nums, ",") + "]"; got != want[i] {
			t.Errorf("row %d metrics = %s, want %s", i, got, want[i])
		}
	}
}

func TestHeardWatchdogContinuity(t *testing.T) {
	const start = "DMR Slot 2, received network late entry from M0ABC to TG 91"
	const end = "DMR Slot 2, network watchdog has expired, 8.0 seconds, 3% packet loss, BER: 0.0%"
	base := time.Unix(1800000000, 0).UTC()
	tail := heardRecord(t, base.Add(time.Second), "unrelated")
	for _, tc := range []struct {
		name, middle string
		elapsed      int
		gap, restart bool
		wantMatched  bool
	}{
		{name: "continuous", elapsed: 8, wantMatched: true},
		{name: "journal gap", elapsed: 8, gap: true},
		{name: "service restart", elapsed: 8, restart: true},
		{name: "stale", elapsed: 120},
		{name: "before start", elapsed: -1},
		{name: "unusable next header", elapsed: 8, middle: strings.Replace(start, "M0ABC", "<bad>", 1)},
		{name: "multiline next header", elapsed: 8, middle: start + "\nunsafe"},
		{name: "data replaced voice", elapsed: 8, middle: "DMR Slot 2, received network data header from M0ABC to TG 91, 2 blocks"},
		{name: "other caller", elapsed: 8, middle: strings.Replace(start, "M0ABC", "M1XYZ", 1)},
		{name: "already ended", elapsed: 8, middle: "DMR Slot 2, network watchdog has expired"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := heardHistory{}
			h.ingest(append(heardRecord(t, base, start), tail...), base.Add(time.Second))
			var next bytes.Buffer
			if !tc.gap {
				next.Write(tail)
			}
			if tc.middle != "" {
				next.Write(heardRecord(t, base.Add(2*time.Second), tc.middle))
			}
			event := heardRecord(t, base.Add(time.Duration(tc.elapsed)*time.Second), end)
			if tc.restart {
				event = bytes.Replace(event, []byte(`{"`), []byte(`{"_SYSTEMD_INVOCATION_ID":"new","`), 1)
			}
			next.Write(event)
			h.ingest(next.Bytes(), base.Add(time.Hour))
			row := h.rows[len(h.rows)-1]
			if row.From != "M0ABC" || (row.Duration != nil) != tc.wantMatched {
				t.Fatalf("original row %+v, want metrics=%v", row, tc.wantMatched)
			}
		})
	}
}

func TestHeardEndParties(t *testing.T) {
	base := time.Unix(1800000000, 0).UTC()
	const start = "DMR Slot 2, received RF late entry from M0ABC to TG 91"
	const end = "DMR Slot 2, received RF end of voice transmission from M0ABC to TG 91, 1.0 seconds, BER: 0.0%"
	for _, msg := range []string{
		strings.Replace(end, "M0ABC", "M0ABC/P", 1),
		strings.Replace(end, "TG 91", "91", 1),
		strings.Replace(end, "Slot 2", "Slot 1", 1),
		strings.Replace(end, "RF", "network", 1),
		end + ", RSSI: garbage",
	} {
		h := heardHistory{}
		h.ingest(append(heardRecord(t, base, start), heardRecord(t, base.Add(time.Second), msg)...), base.Add(time.Minute))
		if len(h.rows) != 1 || h.rows[0].Duration != nil {
			t.Errorf("unexpected match: %s => %+v", msg, h.rows)
		}
	}
}

func TestHeardRefresh(t *testing.T) {
	now := time.Unix(1800000000, 0).UTC()
	const msg = "DMR Slot 2, received RF late entry from 1234567 to TG 9"
	var out bytes.Buffer
	for i := 0; i < heardLimit+5; i++ {
		out.Write(heardRecord(t, now.Add(time.Duration(-i)*time.Minute), msg))
	}
	payload := out.Bytes()
	var runErr error
	count := 0
	run := func(_ context.Context, cmd string, args ...string) ([]byte, error) {
		count++
		a := " " + strings.Join(args, " ") + " "
		for _, scope := range []string{" -b ", " _SYSTEMD_UNIT=mmdvmhost.service ", " --since=-24h ", " -n 64 "} {
			if cmd != "journalctl" || !strings.Contains(a, scope) || strings.Contains(a, "--grep") {
				t.Fatalf("unbounded or unexpected journal read: %s %s", cmd, a)
			}
		}
		return payload, runErr
	}
	h := heardHistory{}
	collect := func() ([]HeardRow, bool) {
		return h.collect(context.Background(), run, func() time.Time { return now })
	}
	rows, ok := collect()
	if !ok || len(rows) != heardLimit || !rows[0].At.Equal(now) || !rows[heardLimit-1].At.Equal(now.Add(-(heardLimit-1)*time.Minute)) {
		t.Fatalf("unexpected initial rows: %+v (%v)", rows, ok)
	}
	now = now.Add(5 * time.Second)
	collect()
	if count != 1 {
		t.Fatal("journal reread on next panel tick")
	}
	now = now.Add(heardRefresh)
	payload = heardRecord(t, rows[0].At, msg) // only one event remains in the tail
	got, ok := collect()
	if !ok || !reflect.DeepEqual(got, rows) || count != 2 {
		t.Fatalf("lost or duplicated overlapping observations: %+v", got)
	}
	now = now.Add(heardRefresh)
	runErr = errors.New("permission denied")
	if got, ok := collect(); ok || len(got) != 0 {
		t.Fatal("failed read reported as available")
	}
	now = now.Add(heardAge)
	payload, runErr = nil, nil
	if got, ok := collect(); !ok || len(got) != 0 {
		t.Fatalf("readable quiet journal after records expired: %+v (%v)", got, ok)
	}
}
