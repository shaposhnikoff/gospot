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

func withCSSFile(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gospot-css.ini")
	if text != "" {
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := gospotCSSFile
	gospotCSSFile = path
	t.Cleanup(func() { gospotCSSFile = old })
	return path
}

func TestThemeDefaultsAndOverrides(t *testing.T) {
	withCSSFile(t, "")
	if th := currentTheme(); th.Banners != "dd4b39" || th.Odd != "d0d0d0" || th.BannerH1 != "" || th.Lookup != "RadioID" {
		t.Fatalf("defaults: %+v", th)
	}
	withCSSFile(t, "[Background]\nBanners=0000FF\nPage=red;}body{x\n[Tables]\nBgOdd=abc\n"+
		"[BannerH1]\nEnabled=1\nText=\"Hello <b>\"\n[BannerExtText]\nEnabled=0\nText=x\n[Lookup]\nService=QRZ\n")
	th := currentTheme()
	if th.Banners != "0000ff" || th.Page != "edf0f5" || th.Odd != "abc" || th.BannerH1 != "Hello <b>" || th.BannerExt != "" || th.Lookup != "QRZ" {
		t.Fatalf("overrides: %+v", th)
	}
	if d := gatherDashboard(time.Now()); d.Theme != th {
		t.Fatalf("dashboard theme %+v", d.Theme)
	}
}

func TestCSSToolEditor(t *testing.T) {
	withCSSFile(t, "")
	var verbs []string
	var last string
	run := func(_ context.Context, verb string, input []byte) ([]byte, error) {
		verbs, last = append(verbs, verb), string(input)
		if strings.Contains(last, `"stale"`) {
			return []byte(`{"error":"changed","stale":true}`), errConfigStale
		}
		return []byte(`{"ok":true}`), nil
	}
	w := httptest.NewRecorder()
	serveCSSTool(w, maintenanceRequest("GET", "/manage/api/expert/css", "", false), run)
	body := w.Body.String()
	if w.Code != 200 || !strings.Contains(body, `"exists":false`) || !strings.Contains(body, `{"name":"Lookup","keys":[["Service","RadioID"]]}`) ||
		!strings.Contains(body, `["Text","Some Text"]`) {
		t.Fatalf("%d %s", w.Code, body)
	}
	post := func(b string, csrf bool) int {
		w := httptest.NewRecorder()
		serveCSSTool(w, maintenanceRequest("POST", "/manage/api/expert/css", b, csrf), run)
		return w.Code
	}
	if c := post(`{"revision":"r","action":"save","values":{"Background":{"Page":"000000"}}}`, false); c != 403 {
		t.Fatalf("csrf %d", c)
	}
	if c := post(`{"revision":"r","action":"reset","confirm":"reset"}`, true); c != 422 {
		t.Fatalf("confirm %d", c)
	}
	if c := post(`{"revision":"r","action":"save","values":{"Background":{"Page":"000000"}}}`, true); c != 200 || !strings.Contains(last, `"Page":"000000"`) {
		t.Fatalf("save %d %s", c, last)
	}
	if c := post(`{"revision":"stale","action":"save","values":{}}`, true); c != 409 {
		t.Fatalf("stale %d", c)
	}
	if c := post(`{"revision":"r","action":"reset","confirm":"RESET"}`, true); c != 200 {
		t.Fatalf("reset %d", c)
	}
	if strings.Join(verbs, "|") != "css-write|css-write|css-reset" {
		t.Fatalf("%v", verbs)
	}
	withCSSFile(t, "[Lookup]\nService=QRZ\npopupWidth=1\n")
	w = httptest.NewRecorder()
	serveCSSTool(w, maintenanceRequest("GET", "/manage/api/expert/css", "", false), run)
	if !strings.Contains(w.Body.String(), `"sections":[{"name":"Lookup","keys":[["Service","QRZ"]]}]`) || !strings.Contains(w.Body.String(), `"exists":true`) {
		t.Fatalf("%s", w.Body.String())
	}
}

// The dashboard snapshot is cached for 500 ms; the theme must not be.
func TestDashboardThemeIsFresh(t *testing.T) {
	path := withCSSFile(t, "[Background]\nBanners=0000ff\n")
	h := dashboardHandler()
	get := func() string {
		w := httptest.NewRecorder()
		h(w, httptest.NewRequest("GET", "/api/dashboard", nil))
		return w.Body.String()
	}
	if !strings.Contains(get(), `"Banners":"0000ff"`) {
		t.Fatal("saved theme not served")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(get(), `"Banners":"dd4b39"`) {
		t.Fatal("reset theme served from the cached snapshot")
	}
}
