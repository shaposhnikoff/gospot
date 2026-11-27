package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func archiveRequest(action, data string) *http.Request {
	const host = "https://node:8788"
	r := httptest.NewRequest("POST", host+archiveRestorePath, strings.NewReader(data))
	token := strings.Repeat("a", 64)
	r.AddCookie(&http.Cookie{Name: wizardCookie, Value: token})
	r.Header.Set("X-PiStar-CSRF", token)
	r.Header.Set("Origin", host)
	r.Header.Set("X-PiStar-Action", action)
	r.Header.Set("Content-Type", "application/zip")
	return r
}

func TestArchiveRejectBeforeHelper(t *testing.T) {
	for _, tc := range []struct {
		name  string
		code  int
		alter func(*http.Request)
	}{
		{"no origin", 403, func(r *http.Request) { r.Header.Del("Origin") }},
		{"foreign origin", 403, func(r *http.Request) { r.Header.Set("Origin", "https://node:8788.other") }},
		{"unclean origin", 403, func(r *http.Request) { r.Header.Set("Origin", "https://node:8788/") }},
		{"duplicate origin", 403, func(r *http.Request) { r.Header.Add("Origin", "https://node:8788") }},
		{"no cookie", 403, func(r *http.Request) { r.Header.Del("Cookie") }},
		{"duplicate cookie", 403, func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: wizardCookie, Value: strings.Repeat("a", 64)})
		}},
		{"no header", 403, func(r *http.Request) { r.Header.Del("X-PiStar-CSRF") }},
		{"different token", 403, func(r *http.Request) { r.Header.Set("X-PiStar-CSRF", strings.Repeat("b", 64)) }},
		{"duplicate action", 400, func(r *http.Request) { r.Header.Add("X-PiStar-Action", "apply") }},
		{"apply without receipt", 400, func(r *http.Request) { r.Header.Set("X-PiStar-Action", "apply") }},
		{"query", 403, func(r *http.Request) { r.URL.RawQuery = "action=apply" }},
		{"no TLS", 403, func(r *http.Request) { r.TLS = nil }},
		{"wrong method", 405, func(r *http.Request) { r.Method = "GET" }},
		{"wrong download method", 405, func(r *http.Request) { r.URL.Path = archiveBackupPath }},
		{"wrong media type", 415, func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }},
		{"compressed upload", 415, func(r *http.Request) { r.Header.Set("Content-Encoding", "gzip") }},
		{"empty upload", 400, func(r *http.Request) { r.Body = http.NoBody }},
		{"oversized upload", 413, func(r *http.Request) { r.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", archiveMax+1))) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := archiveRequest("preview", "private-data")
			tc.alter(r)
			w := httptest.NewRecorder()
			handleArchive(w, r, func(context.Context, string, []byte) ([]byte, error) {
				t.Fatal("unexpected privileged operation")
				return nil, nil
			})
			if w.Code != tc.code || strings.Contains(w.Body.String(), "private-data") {
				t.Fatalf("unexpected rejection, HTTP %d", w.Code)
			}
		})
	}
}

func TestArchiveAuthenticatedDispatch(t *testing.T) {
	wizardSetup(t)
	mux := wizardTestMux(nil, t.TempDir())
	for _, path := range []string{archiveBackupPath, archiveRestorePath} {
		r := httptest.NewRequest("GET", "https://node"+path, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("%s did not require credentials", path)
		}
		// Wrong method exercises dispatch without ever invoking sudo.
		r.SetBasicAuth(wizardUser, "admin-secret")
		if path == archiveBackupPath {
			r.Method = "POST"
		}
		w = httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 405 || w.Header().Get("Allow") != map[string]string{archiveBackupPath: "GET", archiveRestorePath: "POST"}[path] {
			t.Fatalf("archive route not wired: %s", path)
		}
	}
}

func TestArchiveDownloadAndConfirmation(t *testing.T) {
	r := httptest.NewRequest("GET", "https://node"+archiveBackupPath, nil)
	w := httptest.NewRecorder()
	handleArchive(w, r, func(_ context.Context, verb string, input []byte) ([]byte, error) {
		if verb != "backup" || len(input) != 0 {
			t.Fatal("unexpected backup arguments")
		}
		return []byte("ZIP\x00fixture"), nil
	})
	if w.Body.String() != "ZIP\x00fixture" || w.Header().Get("Content-Type") != "application/zip" ||
		!strings.Contains(w.Header().Get("Content-Disposition"), "PRIVATE.zip") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("incorrect private download")
	}
	receipt := strings.Repeat("b", 64)
	r = archiveRequest("preview", "private-data")
	w = httptest.NewRecorder()
	handleArchive(w, r, func(_ context.Context, verb string, input []byte) ([]byte, error) {
		if verb != "preview" || string(input) != "private-data" {
			t.Fatal("incorrect preview payload")
		}
		return []byte(`{"changed":["DMRGateway.ini"],"receipt":"` + receipt + `","extra":"private-data"}`), nil
	})
	if w.Code != 200 || !strings.Contains(w.Body.String(), "DMRGateway.ini") || !strings.Contains(w.Body.String(), receipt) || strings.Contains(w.Body.String(), "private-data") {
		t.Fatal("expected only allowlisted preview response")
	}
	r = archiveRequest("apply", "private-data")
	r.Header.Set("X-PiStar-Receipt", receipt)
	cancelled, cancel := context.WithCancel(r.Context())
	cancel()
	w = httptest.NewRecorder()
	handleArchive(w, r.WithContext(cancelled), func(ctx context.Context, verb string, input []byte) ([]byte, error) {
		if ctx.Err() != nil || verb != "apply" || string(input) != receipt+"\nprivate-data" {
			t.Fatal("incorrect transaction context or payload")
		}
		return []byte("private-data"), nil
	})
	if w.Body.String() != `{"ok":true}` || w.Code != 200 {
		t.Fatal("unexpected apply response")
	}
}

func TestArchiveErrorsStayPrivate(t *testing.T) {
	for _, tc := range []struct {
		out  string
		err  error
		code int
	}{
		{"private-data", errors.New("private-data"), 502},
		{"private-data", errArchiveInvalid, 422},
		{"private-data", errArchiveBusy, 503},
		{`{"changed":["private-data"],"receipt":"` + strings.Repeat("b", 64) + `"}`, nil, 502},
	} {
		w := httptest.NewRecorder()
		handleArchive(w, archiveRequest("preview", "private-data"), func(context.Context, string, []byte) ([]byte, error) {
			return []byte(tc.out), tc.err
		})
		if w.Code != tc.code || strings.Contains(w.Body.String(), "private-data") {
			t.Fatalf("unexpected helper error response, HTTP %d", w.Code)
		}
	}
}
