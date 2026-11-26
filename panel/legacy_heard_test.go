package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func requestLegacyHeard(method, query string, s *Status) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	serveLegacyHeard(w, httptest.NewRequest(method, "/api/last_heard.php"+query, nil), s)
	return w
}

func TestLegacyHeardResponse(t *testing.T) {
	at := time.Date(2026, 11, 24, 13, 15, 3, 400_000_000, time.FixedZone("UTC+1", 3600))
	zero, rssi, loss := 0.0, -88.0, 12.0
	s := &Status{HeardAvailable: true, Heard: []HeardRow{
		{At: at, Slot: "2", From: "M0ABC/P", To: "TG 91", Source: "RF", BER: &zero, Duration: &zero, RSSI: &rssi},
		{At: at.Add(-time.Minute), Slot: "1", From: "1234567", To: "M0ABC", Source: "Net", Loss: &loss},
	}}
	w := requestLegacyHeard(http.MethodGet, "", s)
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/json" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("response %d %v", w.Code, w.Header())
	}
	var rows []map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	want := []map[string]string{
		{"time_utc": "2026-11-24 12:15:03", "mode": "DMR Slot 2", "callsign": "M0ABC", "callsign_suffix": "", "target": "TG 91", "src": "RF", "duration": "0.0", "bit_error_rate": "0.0%", "loss": "", "rssi": "S9+5dB (-88 dBm)"},
		{"time_utc": "2026-11-24 12:14:03", "mode": "DMR Slot 1", "callsign": "1234567", "callsign_suffix": "", "target": "M0ABC", "src": "Net", "duration": "", "bit_error_rate": "", "loss": "12%", "rssi": ""},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("got %+v, want %+v", rows, want)
	}
}

func TestLegacyHeardLimitAndAvailability(t *testing.T) {
	s := &Status{HeardAvailable: true, Heard: make([]HeardRow, 25)}
	for q, want := range map[string]int{
		"": 20, "?num_transmissions=3": 3, "?num_transmissions=" + strings.Repeat("9", 200): 20,
		"?num_transmissions=0": 0, "?num_transmissions=-2": 0, "?num_transmissions=": 0,
		"?num_transmissions=text": 0, "?num_transmissions=2&num_transmissions=3": 0,
	} {
		w := requestLegacyHeard(http.MethodGet, q, s)
		var rows []legacyHeardRow
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &rows) != nil || rows == nil || len(rows) != want {
			t.Errorf("query %q: got %d, %s", q, w.Code, w.Body.String())
		}
	}
	for _, snapshot := range []*Status{nil, {Heard: s.Heard}, {HeardAvailable: true}} {
		w := requestLegacyHeard(http.MethodGet, "", snapshot)
		want := 503
		if snapshot != nil && snapshot.HeardAvailable {
			want = 200
		}
		if w.Code != want || strings.TrimSpace(w.Body.String()) != "[]" {
			t.Errorf("snapshot %v: got %d %s, want %d []", snapshot, w.Code, w.Body.String(), want)
		}
	}
	if w := requestLegacyHeard(http.MethodPost, "", s); w.Code != 405 {
		t.Errorf("POST: got %d", w.Code)
	}
}
