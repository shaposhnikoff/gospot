package main

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestClassicBackupArchiveName(t *testing.T) {
	saved := hostnameFile
	t.Cleanup(func() { hostnameFile = saved })
	hostnameFile = filepath.Join(t.TempDir(), "hostname")
	run := func(context.Context, string, []byte) ([]byte, error) { return []byte("PK\x03\x04"), nil }
	now := time.Date(2026, 11, 27, 9, 0, 0, 0, time.Local)
	for host, want := range map[string]string{
		"pistar3\n": `attachment; filename="GoSpot_Config_pistar3_2026-Nov-27.zip"`,
		"gospot\n":  `attachment; filename="GoSpot_Config_2026-Nov-27.zip"`,
		"":          `attachment; filename="GoSpot_Config_2026-Nov-27.zip"`,
	} {
		os.WriteFile(hostnameFile, []byte(host), 0o644)
		w := httptest.NewRecorder()
		serveClassicBackup(w, maintenanceRequest("GET", "/manage/api/backup/classic", "", false), run, now)
		if got := w.Header().Get("Content-Disposition"); got != want {
			t.Errorf("hostname %q: %s, want %s", host, got, want)
		}
	}
}

func TestFactoryResetHandler(t *testing.T) {
	var verbs []string
	run := func(_ context.Context, verb string, input []byte) ([]byte, error) {
		verbs = append(verbs, verb+" "+string(input))
		return []byte(`{"ok":true,"output":["Loading fresh configuration file(s)..."]}`), nil
	}
	post := func(body string, csrf bool) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		serveFactoryReset(w, maintenanceRequest("POST", "/manage/api/configure/factory-reset", body, csrf), run)
		return w
	}
	if w := post(`{"confirm":"RESET"}`, false); w.Code != 403 {
		t.Fatalf("csrf %d", w.Code)
	}
	if w := post(`{"confirm":"yes"}`, true); w.Code != 422 {
		t.Fatalf("confirm %d", w.Code)
	}
	if w := post(`{"confirm":"RESET"}`, true); w.Code != 200 || !strings.Contains(w.Body.String(), "Loading fresh") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if strings.Join(verbs, "|") != `factory-reset {"confirm":"RESET"}` {
		t.Fatalf("%v", verbs)
	}
}
