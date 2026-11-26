package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// All ten legacy fields are strings; /api/status exposes nullable numbers.
type legacyHeardRow struct {
	TimeUTC        string `json:"time_utc"`
	Mode           string `json:"mode"`
	Callsign       string `json:"callsign"`
	CallsignSuffix string `json:"callsign_suffix"`
	Target         string `json:"target"`
	Src            string `json:"src"`
	Duration       string `json:"duration"`
	Loss           string `json:"loss"`
	BER            string `json:"bit_error_rate"`
	RSSI           string `json:"rssi"`
}

func legacyHeardMetric(n *float64, precision int, unit string) string {
	if n == nil {
		return ""
	}
	return strconv.FormatFloat(*n, 'f', precision, 64) + unit
}

func legacyHeard(r HeardRow) legacyHeardRow {
	// functions.php drops portable suffixes for DMR; callsign_suffix was a
	// D-Star field. The full DMR ID is still available in /api/status.
	call, _, _ := strings.Cut(r.From, "/")
	row := legacyHeardRow{
		TimeUTC: r.At.UTC().Format("2006-01-02 15:04:05"), Mode: "DMR Slot " + r.Slot,
		Callsign: call, Target: r.To, Src: r.Source,
		Duration: legacyHeardMetric(r.Duration, 1, ""), Loss: legacyHeardMetric(r.Loss, 0, "%"),
		BER: legacyHeardMetric(r.BER, 1, "%"),
	}
	if r.RSSI != nil {
		dbm := int(*r.RSSI)
		rel := dbm + 93
		s := min(9, max(0, int(math.Round(float64(rel)/6+9))))
		strength := fmt.Sprintf("S%d", s)
		if rel > 0 {
			strength += fmt.Sprintf("+%ddB", rel)
		}
		row.RSSI = fmt.Sprintf("%s (%d dBm)", strength, dbm)
	}
	return row
}

func serveLegacyHeard(w http.ResponseWriter, r *http.Request, s *Status) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	rows := []legacyHeardRow{}
	switch {
	case s == nil || !s.HeardAvailable:
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusServiceUnavailable)
	default:
		query, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			break
		}
		limit := min(len(s.Heard), heardLimit)
		if values, ok := query["num_transmissions"]; ok {
			// An invalid, duplicate, empty or negative count returns no rows.
			// Saturate positive integers without converting untrusted lengths
			// to an allocation or overflowing on 32-bit targets.
			n := 0
			if len(values) == 1 {
				for _, c := range values[0] {
					if c < '0' || c > '9' {
						n = 0
						break
					}
					n = min(limit, 10*n+int(c-'0'))
				}
			}
			limit = n
		}
		for _, row := range s.Heard[:limit] {
			rows = append(rows, legacyHeard(row))
		}
	}
	_ = json.NewEncoder(w).Encode(rows)
}
