package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

const fakeRead = `{"revision":"r1","files":{},"layout":{
 "mmdvmhost":[["General",[["Callsign","M0ABC"],["Display","None"]]],["DMR Network",[["Password","\"pw\""],["Options",""]]]],
 "ircddbgateway":[["",[["gatewayCallsign","M0ABC"],["ircddbPassword",""]]]]},"system":{}}`

func fakeConfigure(t *testing.T, applied *configChange, stale bool) configHelperFunc {
	return func(_ context.Context, verb string, input []byte) ([]byte, error) {
		if verb == "read" {
			return []byte(fakeRead), nil
		}
		if err := json.Unmarshal(input, applied); err != nil {
			t.Fatal(err)
		}
		if stale {
			return []byte(`{"error":"changed","stale":true}`), errConfigStale
		}
		return []byte(`{"ok":true}`), nil
	}
}

func TestQuickEditRead(t *testing.T) {
	w := httptest.NewRecorder()
	serveExpert(w, maintenanceRequest("GET", "/manage/api/expert/ircddbgateway", "", false), fakeConfigure(t, nil, false))
	var r quickReply
	if json.Unmarshal(w.Body.Bytes(), &r) != nil || r.Revision != "r1" || len(r.Sections) != 1 || r.Sections[0].Name != "ircddbgateway" || r.Sections[0].Keys[0] != [2]string{"gatewayCallsign", "M0ABC"} {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	serveExpert(w, maintenanceRequest("GET", "/manage/api/expert/mmdvmhost", "", false), fakeConfigure(t, nil, false))
	if !strings.Contains(w.Body.String(), `["Password","pw"]`) {
		t.Fatalf("quotes must be stripped for display: %s", w.Body.String())
	}
	w = httptest.NewRecorder()
	serveExpert(w, maintenanceRequest("GET", "/manage/api/expert/passwd", "", false), fakeConfigure(t, nil, false))
	if w.Code != 404 {
		t.Fatalf("unknown file: %d", w.Code)
	}
}

func TestQuickEditApplyRules(t *testing.T) {
	var got configChange
	body := `{"revision":"r1","values":{"General":{"Callsign":"M0ABC","Display":""},"DMR Network":{"Password":"new\"pw","Options":""}}}`
	w := httptest.NewRecorder()
	serveExpert(w, maintenanceRequest("POST", "/manage/api/expert/mmdvmhost", body, true), fakeConfigure(t, &got, false))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	val := func(f, s, k string) string {
		if v := got.Changes[f][s][k]; v != nil {
			return *v
		}
		return "<unchanged>"
	}
	if val("mmdvmhost", "General", "Callsign") != "<unchanged>" || val("mmdvmhost", "General", "Display") != "none" ||
		val("mmdvmhost", "DMR Network", "Password") != `"newpw"` || val("mmdvmhost", "DMR Network", "Options") != "<unchanged>" ||
		strings.Join(got.Restart, ",") != "mmdvmhost.service" || got.Revision != "r1" {
		t.Fatalf("%+v", got)
	}
	got = configChange{}
	w = httptest.NewRecorder()
	serveExpert(w, maintenanceRequest("POST", "/manage/api/expert/ircddbgateway", `{"revision":"r1","values":{"ircddbgateway":{"gatewayCallsign":"M0XYZ","ircddbPassword":""}}}`, true), fakeConfigure(t, &got, false))
	if w.Code != 200 || val("ircddbgateway", "", "gatewayCallsign") != "M0XYZ" || val("ircddbgateway", "", "ircddbPassword") != "<unchanged>" || got.Restart[0] != "ircddbgateway.service" {
		t.Fatalf("flat: %d %+v", w.Code, got)
	}
	w = httptest.NewRecorder()
	serveExpert(w, maintenanceRequest("POST", "/manage/api/expert/mmdvmhost", body, true), fakeConfigure(t, &got, true))
	if w.Code != 409 {
		t.Fatalf("stale: %d", w.Code)
	}
	w = httptest.NewRecorder()
	serveExpert(w, maintenanceRequest("POST", "/manage/api/expert/mmdvmhost", body, false), fakeConfigure(t, &got, false))
	if w.Code != 403 {
		t.Fatalf("csrf: %d", w.Code)
	}
}

func TestFullEdit(t *testing.T) {
	var verbs []string
	run := func(_ context.Context, verb string, input []byte) ([]byte, error) {
		verbs = append(verbs, verb)
		switch verb {
		case "full-read cron":
			return []byte(`{"exists":true,"text":"# cron\n","revision":"r"}`), nil
		case "full-write cron":
			if strings.Contains(string(input), "base64") {
				return []byte(`{"ok":false,"blocked":["line 1 (references base64)"]}`), nil
			}
			return []byte(`{"ok":true}`), nil
		}
		return nil, errConfigStale
	}
	w := httptest.NewRecorder()
	serveExpert(w, maintenanceRequest("GET", "/manage/api/expert/full/cron", "", false), run)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"text":"# cron\n"`) || !strings.Contains(w.Body.String(), `"csrf"`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	serveExpert(w, maintenanceRequest("POST", "/manage/api/expert/full/cron", `{"revision":"r","text":"* * * * * root echo base64"}`, true), run)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "blocked") {
		t.Fatalf("blocked: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	serveExpert(w, maintenanceRequest("POST", "/manage/api/expert/full/rssidat", `{"revision":"r","text":"x"}`, true), run)
	if w.Code != 409 {
		t.Fatalf("stale: %d", w.Code)
	}
	w = httptest.NewRecorder()
	serveExpert(w, maintenanceRequest("GET", "/manage/api/expert/full/shadow", "", false), run)
	if w.Code != 404 || strings.Join(verbs, "|") != "full-read cron|full-write cron|full-write rssidat" {
		t.Fatalf("%d %v", w.Code, verbs)
	}
}

func TestQuickEditUnchangedFormWritesNothing(t *testing.T) {
	var got configChange
	body := `{"revision":"r1","values":{"General":{"Callsign":"M0ABC","Display":"None"},"DMR Network":{"Password":"pw","Options":""}}}`
	w := httptest.NewRecorder()
	serveExpert(w, maintenanceRequest("POST", "/manage/api/expert/mmdvmhost", body, true), fakeConfigure(t, &got, false))
	if w.Code != 200 || got.Changes != nil || got.Restart != nil {
		t.Fatalf("an unchanged form must not write or restart: %d %+v", w.Code, got)
	}
}

func TestQuickEditClearedFlatValue(t *testing.T) {
	var got configChange
	w := httptest.NewRecorder()
	serveExpert(w, maintenanceRequest("POST", "/manage/api/expert/ircddbgateway", `{"revision":"r1","values":{"ircddbgateway":{"gatewayCallsign":"","ircddbPassword":""}}}`, true), fakeConfigure(t, &got, false))
	v := got.Changes["ircddbgateway"][""]["gatewayCallsign"]
	if w.Code != 200 || v == nil || *v != " " || len(got.Changes["ircddbgateway"][""]) != 1 {
		t.Fatalf("%d %+v", w.Code, got)
	}
}
