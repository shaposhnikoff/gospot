package main

import (
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestPreviewOriginAndNavigation(t *testing.T) {
	upstream, _ := url.Parse("https://127.0.0.1:1")
	handler := previewHandler(t.TempDir(), upstream, "fixture-only", x509.NewCertPool())
	for _, scenario := range []struct {
		method, host, origin, path string
		status                     int
	}{
		{"GET", "localhost:52996", "", "/runtime.json", 200},
		{"GET", "localhost:52996", "", "/admin/configure.php", 302},
		{"GET", "localhost:52996", "", "/api/status", 503},
		{"GET", "attacker.invalid", "", "/manage/", 403},
		{"GET", "localhost:52996", "http://attacker.invalid", "/manage/", 403},
		{"POST", "localhost:52996", "", "/manage/api/services", 403},
		{"POST", "localhost:52996", "http://localhost:8788", "/manage/api/services", 403},
		{"POST", "localhost:52996", "http://localhost:52996", "/assets/file", 403},
	} {
		request := httptest.NewRequest(scenario.method, "http://"+scenario.host+scenario.path, nil)
		request.Header.Set("Origin", scenario.origin)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != scenario.status {
			t.Fatalf("%+v: %d", scenario, response.Code)
		}
		if response.Code == 302 && response.Header().Get("Location") != "/manage/#configuration" {
			t.Fatalf("redirect left forwarded origin: %s", response.Header().Get("Location"))
		}
	}
}

func TestPreviewProxiesOnlyToAuthenticatedTLSFixture(t *testing.T) {
	calls := 0
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		user, password, ok := request.BasicAuth()
		if !ok || user != "pi-star" || password != "fixture-only" {
			t.Error("missing fixture authentication")
		}
		if request.Header.Get("Origin") != "https://"+request.Host {
			t.Error("upstream origin mismatch")
		}
		if request.Header.Get("Cookie") != upstreamCookie+"=test-csrf" {
			t.Error("unexpected cookies forwarded")
		}
		if body, _ := io.ReadAll(request.Body); string(body) != `{"revision":"test"}` {
			t.Error("body changed")
		}
		http.SetCookie(writer, &http.Cookie{Name: upstreamCookie, Value: "fresh-test-csrf", Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
		writer.WriteHeader(204)
	}))
	defer upstream.Close()
	roots := x509.NewCertPool()
	roots.AddCert(upstream.Certificate())
	target, _ := url.Parse(upstream.URL)
	handler := previewHandler(t.TempDir(), target, "fixture-only", roots)
	request := httptest.NewRequest("POST", "http://localhost:52996/manage/api/services", strings.NewReader(`{"revision":"test"}`))
	request.Header.Set("Origin", "http://localhost:52996")
	request.Header.Set("Cookie", previewCookie+"=test-csrf; unrelated=private")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 204 || calls != 1 {
		t.Fatalf("status %d, calls %d", response.Code, calls)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != previewCookie || cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("invalid local preview cookie")
	}
}
