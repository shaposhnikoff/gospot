package main

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type HeardRow struct {
	At                        time.Time
	Source, From, To, Slot    string
	Duration, BER, Loss, RSSI *float64
}

const (
	heardRefresh = 30 * time.Second
	heardAge     = 24 * time.Hour
	heardLimit   = 20
)

// MMDVMHost's DMRSlot.cpp logs a voice header or a late entry at call start,
// and a data header (with a block count) at data start. Lookup values in these
// messages are not trusted: publish only bounded callsign/ID tokens.
const heardID = `[A-Za-z0-9][A-Za-z0-9/-]{0,15}`

var heardSlot = regexp.MustCompile(`(?s)^(?:M: [0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{3} )?DMR Slot ([12]), (received |ended )?(RF|network) (.*)$`)
var heardStart = regexp.MustCompile(`^(voice header|late entry|data header) from (` + heardID + `) to ((?:TG )?` + heardID + `)(, [0-9]{1,3} blocks)?$`)
var heardParties = regexp.MustCompile(`^from (` + heardID + `) to ((?:TG )?` + heardID + `)$`)

const heardDecimal = `([0-9]{1,6}\.[0-9])`

var heardRFMetrics = regexp.MustCompile(`^` + heardDecimal + ` seconds, BER: ` + heardDecimal + `%(?:, RSSI: -?[0-9]{1,3}/-?[0-9]{1,3}/(-?[0-9]{1,3}) dBm)?$`)
var heardNetMetrics = regexp.MustCompile(`^` + heardDecimal + ` seconds, ([0-9]{1,3})% packet loss, BER: ` + heardDecimal + `%$`)

type heardHistory struct {
	mu      sync.Mutex
	refresh time.Time
	rows    []HeardRow
	ok      bool
	cursor  string
	// One open voice call per (direction, slot). Data/rejected headers and
	// terminal events retire the previous candidate, even without metrics.
	open       map[heardLane]HeardRow
	invocation string
}

type heardLane struct{ source, slot string }

type heardRecordJSON struct {
	Message    string `json:"MESSAGE"`
	Stamp      string `json:"__REALTIME_TIMESTAMP"`
	Cursor     string `json:"__CURSOR"`
	Invocation string `json:"_SYSTEMD_INVOCATION_ID"`
}

var recentHeard heardHistory

func collectHeard(ctx context.Context) ([]HeardRow, bool) {
	return recentHeard.collect(ctx, runDiagnostic, time.Now)
}

func (h *heardHistory) collect(ctx context.Context, run diagRun, now func() time.Time) ([]HeardRow, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !now().Before(h.refresh) {
		// Tail records, not matches: --grep -n can walk the whole boot when
		// there are few calls. runDiagnostic also caps output at 64 KiB.
		out, err := run(ctx, "journalctl", "--system", "-q", "--no-pager", "-b", "_SYSTEMD_UNIT="+modemUnit, "--since=-24h", "-n", "64", "-o", "json", "--output-fields=MESSAGE,_SYSTEMD_INVOCATION_ID")
		at := now()
		h.refresh = at.Add(heardRefresh)
		h.ok = err == nil
		if h.ok {
			h.ingest(out, at)
		}
	}
	if !h.ok {
		return nil, false
	}
	return append([]HeardRow{}, h.rows...), true
}

func (h *heardHistory) ingest(out []byte, now time.Time) {
	cutoff := now.Add(-heardAge)
	var events []heardRecordJSON
	overlap := false
	for _, line := range bytes.Split(bytes.TrimSpace(out), []byte{'\n'}) {
		var e heardRecordJSON
		if json.Unmarshal(line, &e) != nil {
			e = heardRecordJSON{} // cannot follow a call across an unreadable message
		}
		if h.cursor != "" && e.Cursor == h.cursor {
			events, overlap = events[:0], true
			continue
		}
		events = append(events, e)
	}
	if !overlap {
		// The previous tail rolled off. Retain display history, but an unseen
		// start or end may have replaced any of the four active calls.
		h.open = make(map[heardLane]HeardRow)
		h.cursor = ""
	}
	for _, e := range events {
		h.cursor = e.Cursor
		if h.invocation != e.Invocation {
			clear(h.open)
			h.invocation = e.Invocation
		}
		us, err := strconv.ParseInt(e.Stamp, 10, 64)
		at := time.UnixMicro(us).UTC()
		if e.Message == "" || err != nil || us <= 0 || at.Before(cutoff) || at.After(now) {
			clear(h.open)
			continue
		}
		h.observe(at, e.Message)
	}
	sort.SliceStable(h.rows, func(i, j int) bool { return h.rows[i].At.After(h.rows[j].At) })
	keep := make([]HeardRow, 0, heardLimit)
	for _, row := range h.rows {
		if len(keep) == heardLimit || row.At.Before(cutoff) {
			break
		}
		if !row.At.After(now) {
			keep = append(keep, row)
		}
	}
	h.rows = keep
}

// Journal records without an MMDVMHost prefix are also emitted by some builds.
func (h *heardHistory) observe(at time.Time, message string) {
	m := heardSlot.FindStringSubmatch(message)
	if m == nil {
		return
	}
	action, direction, body := m[2], m[3], m[4]
	lane := heardLane{source: "RF", slot: m[1]}
	if direction == "network" {
		lane.source = "Net"
	}

	active, open := h.open[lane]
	var parties, metrics string
	switch {
	case strings.HasPrefix(body, "voice header "), strings.HasPrefix(body, "late entry "), strings.HasPrefix(body, "data header "):
		delete(h.open, lane)
		start := heardStart.FindStringSubmatch(body)
		if action != "received " || start == nil || (start[1] == "data header") != (start[4] != "") {
			return
		}
		row := HeardRow{At: at, From: start[2], To: start[3], Source: lane.source, Slot: lane.slot}
		if h.rowIndex(row) < 0 {
			h.rows = append(h.rows, row)
		}
		if start[1] != "data header" {
			h.open[lane] = row
		}
		return
	case action == "received " && strings.HasPrefix(body, "end of voice transmission "),
		action == "" && direction == "RF" && strings.HasPrefix(body, "voice transmission lost "):
		parties, metrics, _ = strings.Cut(body, ", ")
		if action == "received " {
			parties = strings.TrimPrefix(parties, "end of voice transmission ")
		} else {
			parties = strings.TrimPrefix(parties, "voice transmission lost ")
		}
		ids := heardParties.FindStringSubmatch(parties)
		if ids == nil || ids[1] != active.From || ids[2] != active.To {
			return
		}
	case action == "" && direction == "network" && strings.HasPrefix(body, "watchdog has expired, "):
		metrics = strings.TrimPrefix(body, "watchdog has expired, ")
	// "user has timed out" mutes forwarding, but DMRSlot can still log
	// the call's eventual terminator and its measurements.
	case action == "ended " || body == "watchdog has expired" || strings.HasPrefix(body, "data transmission lost "):
		delete(h.open, lane)
		return
	default:
		return
	}
	if !open || at.Before(active.At) {
		return
	}
	delete(h.open, lane)
	if lane.source == "RF" {
		values := heardRFMetrics.FindStringSubmatch(metrics)
		if values == nil {
			return
		}
		active.Duration, active.BER, active.RSSI = heardNumber(values[1]), heardNumber(values[2]), heardNumber(values[3])
	} else {
		values := heardNetMetrics.FindStringSubmatch(metrics)
		if values == nil {
			return
		}
		active.Duration, active.Loss, active.BER = heardNumber(values[1]), heardNumber(values[2]), heardNumber(values[3])
	}
	// Frame-count duration can differ from journal elapsed time (late entry,
	// jitter and watchdog delay). Reject stale/impossible pairings.
	delta := at.Sub(active.At).Seconds() - *active.Duration
	if delta < -5 || delta > 5 || *active.BER > 100 || (active.Loss != nil && *active.Loss > 100) {
		return
	}
	if i := h.rowIndex(active); i >= 0 {
		h.rows[i] = active
	}
}

func (h *heardHistory) rowIndex(row HeardRow) int {
	for i, old := range h.rows {
		if old.At == row.At && old.Source == row.Source && old.Slot == row.Slot && old.From == row.From && old.To == row.To {
			return i
		}
	}
	return -1
}

// Only called on bounded decimal captures from the numeric log formats.
func heardNumber(s string) *float64 {
	if s == "" {
		return nil
	}
	value, _ := strconv.ParseFloat(s, 64)
	return &value
}
