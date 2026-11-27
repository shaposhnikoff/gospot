package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTailFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "MMDVM.log")
	os.WriteFile(p, []byte("one\n"), 0o644)
	r, err := tailFile(p, -1)
	if err != nil || r.Offset != 4 || r.Data != "" {
		t.Fatalf("start at end: %+v %v", r, err)
	}
	os.WriteFile(p, []byte("one\ntwo\n"), 0o644)
	if r, _ = tailFile(p, 4); r.Data != "two\n" || r.Offset != 8 {
		t.Fatalf("append: %+v", r)
	}
	os.WriteFile(p, []byte("new\n"), 0o644) // rotated/truncated
	if r, _ = tailFile(p, 8); r.Data != "new\n" || r.Offset != 4 {
		t.Fatalf("truncate: %+v", r)
	}
}

func maintenanceRequest(method, path, body string, csrf bool) *http.Request {
	r := httptest.NewRequest(method, "https://node"+path, strings.NewReader(body))
	r.TLS = &tls.ConnectionState{}
	if method == http.MethodPost {
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "https://node")
		token := strings.Repeat("ab", 32)
		r.AddCookie(&http.Cookie{Name: wizardCookie, Value: token})
		if csrf {
			r.Header.Set("X-PiStar-CSRF", token)
		}
	}
	return r
}

func TestPowerAndUpdate(t *testing.T) {
	var calls []string
	run := func(_ context.Context, helper, verb string) error {
		calls = append(calls, filepath.Base(helper)+" "+verb)
		return nil
	}
	running := false
	saved := updateRunning
	defer func() { updateRunning = saved }()
	updateRunning = func(context.Context) bool { return running }
	do := func(r *http.Request) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		serveMaintenance(w, r, run, time.Now())
		return w
	}
	for name, tc := range map[string]struct {
		req    *http.Request
		status int
	}{
		"no csrf":        {maintenanceRequest("POST", "/manage/api/power", `{"action":"reboot"}`, false), 403},
		"bad action":     {maintenanceRequest("POST", "/manage/api/power", `{"action":"format"}`, true), 400},
		"extra field":    {maintenanceRequest("POST", "/manage/api/power", `{"action":"reboot","x":1}`, true), 400},
		"no confirm":     {maintenanceRequest("POST", "/manage/api/update", `{"confirm":false}`, true), 400},
		"wrong method":   {maintenanceRequest("PUT", "/manage/api/power", ``, true), 405},
		"bad log offset": {maintenanceRequest("GET", "/manage/api/livelog?offset=-5", ``, false), 400},
	} {
		if w := do(tc.req); w.Code != tc.status {
			t.Errorf("%s: got %d %s", name, w.Code, w.Body.String())
		}
	}
	if len(calls) != 0 {
		t.Fatalf("rejected requests must not run helpers: %v", calls)
	}
	if w := do(maintenanceRequest("POST", "/manage/api/power", `{"action":"shutdown"}`, true)); w.Code != 202 {
		t.Fatalf("shutdown: %d", w.Code)
	}
	if w := do(maintenanceRequest("POST", "/manage/api/update", `{"confirm":true}`, true)); w.Code != 202 {
		t.Fatalf("update: %d", w.Code)
	}
	running = true
	if w := do(maintenanceRequest("POST", "/manage/api/update", `{"confirm":true}`, true)); w.Code != 409 {
		t.Fatalf("second update: %d", w.Code)
	}
	if strings.Join(calls, "|") != "pistar-power shutdown|pistar-native-update start" {
		t.Fatalf("calls: %v", calls)
	}
	w := do(maintenanceRequest("GET", "/manage/api/power", ``, false))
	var reply tailReply
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &reply) != nil || len(reply.CSRF) != 64 || !strings.Contains(w.Header().Get("Set-Cookie"), wizardCookie) {
		t.Fatalf("token: %d %s", w.Code, w.Body.String())
	}
}
