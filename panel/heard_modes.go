package main

import (
	"regexp"
	"strings"
	"time"
)

// Non-DMR call records, following the line formats listed in the classic
// mmdvmhost/functions.php (getHeardList). Values from RF traffic are
// untrusted: only bounded callsign/target tokens are kept.

const logPrefix = `^(?:[A-Z]: [0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{3} +)?`
const heardToken = `[A-Za-z0-9][A-Za-z0-9/ -]{0,15}?`

var (
	// D-Star, received RF header from M1ABC   /ABCD to CQCQCQ   [via REF000 A]
	dstarStart = regexp.MustCompile(logPrefix + `D-Star, received (RF|network) header from ([A-Z0-9 ]{1,8}?) */ *([A-Z0-9 ]{0,4}?) +to (` + heardToken + `) *(?:via .*)?$`)
	// YSF, received RF data from MW0MWZ     to ALL   [at MB6IBK]
	ysfStart = regexp.MustCompile(logPrefix + `YSF, received (RF|network) data from (` + heardToken + `) +to (` + heardToken + `) *(?:at .*)?$`)
	// P25|NXDN, received RF transmission from M1ABC to TG 10200
	tgStart = regexp.MustCompile(logPrefix + `(P25|NXDN), received (RF|network) transmission from (` + heardToken + `) to (` + heardToken + `)$`)
	// M17, received RF [late entry ]voice transmission from M1ABC to INFO
	m17Start = regexp.MustCompile(logPrefix + `M17, received (RF|network) (?:late entry )?voice transmission from (` + heardToken + `) to (` + heardToken + `)$`)
	// <mode>, ... end of transmission / watchdog / lost / timed out
	otherEnd    = regexp.MustCompile(logPrefix + `(D-Star|YSF|P25|NXDN|M17), (.*)$`)
	pocsagLine  = regexp.MustCompile(logPrefix + `POCSAG, transmitted [0-9]+ frame\(s\)`)
	endSeconds  = regexp.MustCompile(`([0-9]{1,6}\.[0-9]) seconds`)
	endLoss     = regexp.MustCompile(`([0-9]{1,3})% packet loss`)
	endBER      = regexp.MustCompile(`BER: ([0-9]{1,6}\.[0-9])%`)
	endRSSI     = regexp.MustCompile(`RSSI: -?[0-9]{1,3}/-?[0-9]{1,3}/(-?[0-9]{1,3}) dBm`)
	endKeywords = []string{"end of transmission", "watchdog has expired", "transmission lost", "user has timed out", "invalid slow data header"}
	timeoutKeys = []string{"watchdog has expired", "user has timed out", "invalid slow data header"}
)

func heardSource(direction string) string {
	if direction == "network" {
		return "Net"
	}
	return "RF"
}

func (h *heardHistory) start(row HeardRow) {
	row.From, row.To = strings.TrimSpace(row.From), strings.Join(strings.Fields(row.To), " ")
	if row.From == "" || len(row.From) > 10 {
		return
	}
	// Classic renames for parrot and MMDVM network sources.
	if row.Source == "Net" && (row.Mode == "P25" || row.Mode == "NXDN") && row.To == "TG 10" {
		row.From = "PARROT"
	}
	if row.Source == "Net" && row.Mode == "P25" && row.From == "10999" {
		row.From = "MMDVM"
	}
	lane := heardLane{source: row.Source, slot: row.Mode}
	h.open[lane] = row
	if h.rowIndex(row) < 0 {
		h.rows = append(h.rows, row)
	}
}

func (h *heardHistory) observeOther(at time.Time, message string) {
	if m := dstarStart.FindStringSubmatch(message); m != nil {
		h.start(HeardRow{At: at, Mode: "D-Star", Source: heardSource(m[1]), From: m[2], Suffix: strings.TrimSpace(m[3]), To: m[4]})
		return
	}
	if m := ysfStart.FindStringSubmatch(message); m != nil {
		h.start(HeardRow{At: at, Mode: "YSF", Source: heardSource(m[1]), From: m[2], To: m[3]})
		return
	}
	if m := tgStart.FindStringSubmatch(message); m != nil {
		h.start(HeardRow{At: at, Mode: m[1], Source: heardSource(m[2]), From: m[3], To: m[4]})
		return
	}
	if m := m17Start.FindStringSubmatch(message); m != nil {
		h.start(HeardRow{At: at, Mode: "M17", Source: heardSource(m[1]), From: m[2], To: m[3]})
		return
	}
	if pocsagLine.MatchString(message) {
		row := HeardRow{At: at, Mode: "POCSAG", Source: "Net", From: "DAPNET", To: "DAPNET User", Note: "POCSAG Data"}
		if h.rowIndex(row) < 0 {
			h.rows = append(h.rows, row)
		}
		return
	}
	m := otherEnd.FindStringSubmatch(message)
	if m == nil {
		return
	}
	body := m[2]
	ended := false
	for _, k := range endKeywords {
		ended = ended || strings.Contains(body, k)
	}
	if !ended {
		return
	}
	source := "RF"
	if strings.Contains(body, "network") {
		source = "Net"
	}
	lane := heardLane{source: source, slot: m[1]}
	row, open := h.open[lane]
	delete(h.open, lane)
	if !open || at.Before(row.At) {
		return
	}
	if v := endSeconds.FindStringSubmatch(body); v != nil {
		row.Duration = heardNumber(v[1])
	} else {
		for _, k := range timeoutKeys {
			if strings.Contains(body, k) {
				row.Note = "TOut"
			}
		}
		if row.Note == "" {
			return // no measurements to show
		}
	}
	if v := endLoss.FindStringSubmatch(body); v != nil {
		row.Loss = heardNumber(v[1])
	}
	if v := endBER.FindStringSubmatch(body); v != nil {
		row.BER = heardNumber(v[1])
	}
	if v := endRSSI.FindStringSubmatch(body); v != nil {
		row.RSSI = heardNumber(v[1])
	}
	if row.Duration != nil && (at.Sub(row.At).Seconds()-*row.Duration > 5 || *row.Duration-at.Sub(row.At).Seconds() > 5) {
		return // stale pairing
	}
	if i := h.rowIndex(row); i >= 0 {
		h.rows[i] = row
	}
}
