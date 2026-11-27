package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOperatorStaticAndRouting(t *testing.T) {
	assets := t.TempDir()
	if err := os.WriteFile(filepath.Join(assets, "index.html"), []byte("native dashboard"), 0644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret.html")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(assets, "escape.html")); err != nil {
		t.Fatal(err)
	}
	handler := operatorHandler(assets, "http://127.0.0.1:1", 8788)
	for _, scenario := range []struct {
		method, path string
		status       int
		body         string
	}{
		{"GET", "/", 200, "native dashboard"},
		{"GET", "/kiosk", 200, "native dashboard"},
		{"GET", "/kiosk?rotate=1", 200, "native dashboard"},
		{"HEAD", "/kiosk", 200, ""},
		{"POST", "/kiosk", 405, ""},
		{"GET", "/kiosk/assets/x.js", 404, ""},
		{"GET", "/kiosk.html", 404, ""},
		{"HEAD", "/", 200, ""},
		{"GET", "/health", 200, "ok\n"},
		{"GET", "/runtime.json", 200, `{"backend":"gospot-go","wizardPort":8788}`},
		{"POST", "/api/status", 405, ""},
		{"GET", "/escape.html", 404, ""},
		{"GET", "/%2e%2e/secret.html", 404, ""},
		{"GET", "/assets/../index.html", 404, ""},
		{"GET", "/unknown.php", 404, ""},
		{"GET", "/api/status?url=http://example.org", 404, ""},
		{"GET", "/api/last_heard.php?num_transmissions=1&command=restart", 404, ""},
	} {
		t.Run(scenario.method+scenario.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(scenario.method, scenario.path, nil))
			if response.Code != scenario.status {
				t.Fatalf("got %d, want %d", response.Code, scenario.status)
			}
			if scenario.status == 200 && response.Body.String() != scenario.body {
				t.Fatalf("unexpected body %q", response.Body.String())
			}
		})
	}
	for path, location := range map[string]string{
		"/admin/configure.php": "https://station.example:8788/manage/#configuration",
		"/admin/expert/":       "https://station.example:8788/manage/#expert",
		"/mmdvmhost/lh.php":    "/",
		"/kiosk/":              "/kiosk",
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", "http://station.example:8789"+path, nil))
		if response.Code != 302 || response.Header().Get("Location") != location {
			t.Fatalf("redirect %s: %d %s", path, response.Code, response.Header().Get("Location"))
		}
	}
}

func TestOperatorProxyIsBoundedAndDoesNotForwardCredentials(t *testing.T) {
	for _, scenario := range []struct {
		name, path, body string
		upstream, want   int
	}{
		{"status", "/api/status", `{"Node":"fixture"}`, 200, 200},
		{"heard", "/api/last_heard.php?num_transmissions=2", `[]`, 200, 200},
		{"unavailable", "/api/last_heard.php", `unavailable`, 503, 503},
		{"wrong shape", "/api/status", `[]`, 200, 502},
		{"null", "/api/last_heard.php", `null`, 200, 502},
		{"bad json", "/api/status", `{`, 200, 502},
		{"oversized", "/api/status", strings.Repeat(" ", (1<<20)+1), 200, 502},
		{"redirect", "/api/status", `redirect`, 302, 502},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			calls := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				calls++
				if request.RequestURI != scenario.path || request.Method != "GET" || request.Header.Get("Cookie") != "" || request.Header.Get("Authorization") != "" {
					t.Errorf("unexpected upstream request")
				}
				writer.Header().Set("Location", "/not-allowed")
				writer.WriteHeader(scenario.upstream)
				_, _ = io.WriteString(writer, scenario.body)
			}))
			defer upstream.Close()
			request := httptest.NewRequest("GET", scenario.path, nil)
			request.Header.Set("Authorization", "Bearer test-only")
			request.Header.Set("Cookie", "test-only=private")
			response := httptest.NewRecorder()
			operatorHandler(t.TempDir(), upstream.URL, 8788).ServeHTTP(response, request)
			if response.Code != scenario.want || calls != 1 {
				t.Fatalf("status %d; upstream calls %d", response.Code, calls)
			}
			if scenario.want == 200 && response.Body.String() != scenario.body {
				t.Fatal("upstream bytes changed")
			}
		})
	}
}

func TestOperatorUsesConfiguredManagementPort(t *testing.T) {
	handler := operatorHandler(t.TempDir(), "http://127.0.0.1:1", 8791)
	for _, host := range []string{"127.0.0.1", "station.example", "[::1]"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", "http://"+host+":8790/admin/configure.php", nil))
		if response.Code != 302 || response.Header().Get("Location") != "https://"+host+":8791/manage/#configuration" {
			t.Fatalf("incorrect configured redirect: %d %s", response.Code, response.Header().Get("Location"))
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/runtime.json", nil))
	if response.Body.String() != `{"backend":"gospot-go","wizardPort":8791}` {
		t.Fatalf("incorrect advertised runtime: %s", response.Body.String())
	}
}
