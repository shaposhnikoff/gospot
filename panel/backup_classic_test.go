package main

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClassicBackupHandler(t *testing.T) {
	var verbs []string
	run := func(_ context.Context, verb string, input []byte) ([]byte, error) {
		verbs = append(verbs, verb)
		if verb == "backup" {
			return []byte("PK\x03\x04zip"), nil
		}
		if string(input) == "PKgood" {
			return []byte(`{"ok":true,"output":["Installed 3 file(s)."]}`), nil
		}
		return []byte(`{"ok":false,"output":["Refusing ZIP"]}`), nil
	}
	now := time.Date(2026, 11, 27, 9, 0, 0, 0, time.Local)
	w := httptest.NewRecorder()
	serveClassicBackup(w, maintenanceRequest("GET", "/manage/api/backup/classic", "", false), run, now)
	if w.Code != 200 || w.Body.String() != "PK\x03\x04zip" || !strings.Contains(w.Header().Get("Content-Disposition"), "_2026-Nov-27.zip") {
		t.Fatalf("download: %d %q %q", w.Code, w.Body.String(), w.Header().Get("Content-Disposition"))
	}
	post := func(body string, csrf bool, ctype string) *httptest.ResponseRecorder {
		r := maintenanceRequest("POST", "/manage/api/backup/classic", body, csrf)
		r.Header.Set("Content-Type", ctype)
		w := httptest.NewRecorder()
		serveClassicBackup(w, r, run, now)
		return w
	}
	if w := post("PKgood", false, "application/zip"); w.Code != 403 {
		t.Fatalf("csrf: %d", w.Code)
	}
	if w := post("PKgood", true, "text/plain"); w.Code != 415 {
		t.Fatalf("type: %d", w.Code)
	}
	if w := post(strings.Repeat("x", restoreLimit+10), true, "application/zip"); w.Code != 413 {
		t.Fatalf("size: %d", w.Code)
	}
	if w := post("PKgood", true, "application/zip"); w.Code != 200 || !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Fatalf("restore: %d %s", w.Code, w.Body.String())
	}
	if w := post("bad", true, "application/zip"); w.Code != 200 || !strings.Contains(w.Body.String(), "Refusing ZIP") {
		t.Fatalf("refused: %d %s", w.Code, w.Body.String())
	}
	if strings.Join(verbs, "|") != "backup|restore|restore" {
		t.Fatalf("%v", verbs)
	}
}
