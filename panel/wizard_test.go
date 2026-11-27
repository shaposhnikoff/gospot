package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func wizardSetup(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CREDENTIALS_DIRECTORY", dir)
	if err := os.WriteFile(filepath.Join(dir, "admin-password"), []byte("admin-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

func wizardTestMux(helper wizardHelper, assetDir string) *http.ServeMux {
	mux := http.NewServeMux()
	registerWizardRoutes(mux, helper, assetDir)
	return mux
}

func TestWizardAuthentication(t *testing.T) {
	wizardSetup(t)
	never := func(context.Context, string, []byte) ([]byte, error) {
		t.Fatal("unexpected helper call")
		return nil, nil
	}
	paths := []string{"/", "/manage", "/manage/", "/manage/assets/app.js", "/manage/api/config"}
	assets := t.TempDir()
	mux := wizardTestMux(never, assets)
	for _, path := range paths {
		for _, tc := range []struct {
			base, user, pass string
			status           int
		}{
			{"https://node", "", "", 401},
			{"https://node", wizardUser, "bad", 401},
			{"https://node", "guest", "admin-secret", 401},
			{"http://node", wizardUser, "admin-secret", 503},
		} {
			r := httptest.NewRequest("GET", tc.base+path, nil)
			if tc.user != "" {
				r.SetBasicAuth(tc.user, tc.pass)
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != tc.status || (w.Code == 401 && !strings.HasPrefix(w.Header().Get("WWW-Authenticate"), "Basic ")) {
				t.Errorf("%s%s %q: got %d, want %d", tc.base, path, tc.user, w.Code, tc.status)
			}
		}
	}
	t.Setenv("CREDENTIALS_DIRECTORY", t.TempDir())
	mux = wizardTestMux(never, assets)
	for _, path := range paths {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "https://node"+path, nil)
		r.SetBasicAuth(wizardUser, "")
		mux.ServeHTTP(w, r)
		if w.Code != 503 {
			t.Errorf("%s missing credential: %d", path, w.Code)
		}
	}
}

func TestWizardForms(t *testing.T) {
	wizardSetup(t)
	calls := 0
	var action string
	var data []byte
	fail := false
	handle := wizardTestMux(func(_ context.Context, op string, input []byte) ([]byte, error) {
		calls++
		action, data = op, input
		if fail {
			return input, errors.New("network-secret")
		}
		return []byte(`{"callsign":"\"><script>","password":"network-secret"}`), nil
	}, t.TempDir()).ServeHTTP
	get := httptest.NewRequest("GET", "https://node/?action=rollback", nil)
	get.SetBasicAuth(wizardUser, "admin-secret")
	w := httptest.NewRecorder()
	handle(w, get)
	if w.Code != 200 || action != "read" || strings.Contains(w.Body.String(), "<script>") || strings.Contains(w.Body.String(), "secret") {
		t.Fatal("unexpected GET response or helper action")
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || !strings.Contains(w.Body.String(), cookies[0].Value) {
		t.Fatal("missing protected form token")
	}
	post := func(origin, body string, cookie bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "https://node/", strings.NewReader(body))
		r.SetBasicAuth(wizardUser, "admin-secret")
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", origin)
		if cookie {
			r.AddCookie(cookies[0])
		}
		w := httptest.NewRecorder()
		handle(w, r)
		return w
	}
	body := "csrf=" + cookies[0].Value + "&action=rollback"
	for _, tc := range []struct {
		origin, body string
		cookie       bool
		code         int
	}{
		{"https://other", body, true, 403},
		{"", body, true, 403},
		{"https://node", body, false, 403},
		{"https://node", "csrf=fake&action=rollback", true, 403},
		{"https://node", body + "&q=" + strings.Repeat("a", 8192), true, 413},
	} {
		if got := post(tc.origin, tc.body, tc.cookie).Code; got != tc.code || calls != 1 {
			t.Fatalf("rejected POST: status %d, calls %d", got, calls)
		}
	}
	if w := post("https://node", body, true); w.Code != 303 || action != "rollback" || string(data) != "{}" {
		t.Fatal("rollback failed")
	}
	form := url.Values{"action": {"apply"}, "csrf": {cookies[0].Value}, "callsign": {"R1ABC"}, "radio_id": {"1234567"}, "frequency": {"433500000"}, "uart": {"/dev/ttyAMA0"}, "claim": {"1234567"}, "master": {"node.local"}, "port": {"62031"}, "password": {""}}
	w = post("https://node", form.Encode(), true)
	var m map[string]any
	if w.Code != 303 || action != "apply" || json.Unmarshal(data, &m) != nil || len(m) != 8 || m["password"] != "" || m["frequency"] != float64(433500000) || m["port"] != float64(62031) || m["radio_id"] != float64(1234567) || m["claim"] != float64(1234567) {
		t.Fatal("apply JSON does not match helper contract")
	}
	fail = true
	form.Set("password", "network-secret")
	w = post("https://node", form.Encode(), true)
	if w.Code != 502 || strings.Contains(w.Body.String(), "secret") {
		t.Fatal("unsafe helper error response")
	}
}

func wizardRequest(method, path string, form url.Values, origin string, cookie *http.Cookie) *http.Request {
	r := httptest.NewRequest(method, "https://node"+path, strings.NewReader(form.Encode()))
	r.SetBasicAuth(wizardUser, "admin-secret")
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	return r
}

func TestWizardConfigAPI(t *testing.T) {
	wizardSetup(t)
	var ops []string
	mux := wizardTestMux(func(_ context.Context, op string, input []byte) ([]byte, error) {
		ops = append(ops, op)
		if string(input) != "{}" {
			t.Fatalf("unexpected helper input for %s", op)
		}
		return []byte(`{"callsign":"R1ABC","radio_id":1234567,"frequency":433500000,"uart":"/dev/ttyAMA0","claim":1234567,"master":"node.local","port":62031,"password":"network-secret","private":{"key":"private-key"}}`), nil
	}, filepath.Join(t.TempDir(), "absent"))
	do := func(r *http.Request) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	w := do(wizardRequest("GET", "/manage/api/config?action=apply", nil, "", nil))
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/json" || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("unexpected config response", w.Code, w.Header())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != wizardCookie || cookies[0].Path != "/" || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || len(cookies[0].Value) != 64 {
		t.Fatal("missing protected API cookie")
	}
	cookie := cookies[0]
	want := map[string]any{
		"csrf": cookie.Value,
		"config": map[string]any{
			"callsign": "R1ABC", "radio_id": float64(1234567), "frequency": float64(433500000),
			"uart": "/dev/ttyAMA0", "claim": float64(1234567), "master": "node.local", "port": float64(62031),
		},
	}
	var got map[string]any
	if json.Unmarshal(w.Body.Bytes(), &got) != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("JSON must contain only the public config and matching CSRF token")
	}
	for _, path := range []string{"/", "/manage/api/config"} {
		w = do(wizardRequest("GET", path, nil, "", cookie))
		if w.Code != 200 || len(w.Result().Cookies()) != 0 || !strings.Contains(w.Body.String(), cookie.Value) {
			t.Fatalf("%s did not reuse existing token", path)
		}
	}
	form := url.Values{"csrf": {cookie.Value}, "action": {"rollback"}}
	for _, path := range []string{"/manage/api/config", "/manage/", "/manage/index.html", "/manage/assets/app.js"} {
		if w = do(wizardRequest("POST", path, form, "https://node", cookie)); w.Code != 405 {
			t.Fatalf("POST %s: %d", path, w.Code)
		}
	}
	if w = do(wizardRequest("GET", "/", nil, "", nil)); w.Code != 200 || strings.Contains(w.Body.String(), cookie.Value) {
		t.Fatal("independent wizard request did not get a fresh token")
	}
	if w = do(wizardRequest("POST", "/", form, "https://other", cookie)); w.Code != 403 {
		t.Fatal("root accepted a cross-origin write with API token")
	}
	if w = do(wizardRequest("POST", "/", form, "https://node", nil)); w.Code != 403 {
		t.Fatal("root accepted API token without cookie")
	}
	if w = do(wizardRequest("POST", "/", form, "https://node", cookie)); w.Code != 303 || w.Header().Get("Location") != "/?done=rollback" {
		t.Fatal("root did not accept valid API token")
	}
	if !reflect.DeepEqual(ops, []string{"read", "read", "read", "read", "rollback"}) {
		t.Fatalf("unexpected helper operations: %v", ops)
	}
}

func TestWizardConfigReadFailure(t *testing.T) {
	wizardSetup(t)
	for _, err := range []error{nil, errors.New("helper-secret")} {
		mux := wizardTestMux(func(context.Context, string, []byte) ([]byte, error) {
			return []byte("malformed config-secret"), err
		}, t.TempDir())
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, wizardRequest("GET", "/manage/api/config", nil, "", nil))
		if w.Code != 502 || strings.Contains(w.Body.String(), "secret") || len(w.Result().Cookies()) != 0 {
			t.Fatal("unsafe config read error")
		}
	}
}

func TestWizardNativeAssets(t *testing.T) {
	wizardSetup(t)
	root := t.TempDir()
	dir := filepath.Join(root, "ui")
	bundles := filepath.Join(dir, "assets")
	mux := wizardTestMux(func(context.Context, string, []byte) ([]byte, error) {
		t.Fatal("static route called helper")
		return nil, nil
	}, dir)
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, wizardRequest("GET", path, nil, "", nil))
		return w
	}
	if w := get("/manage/"); w.Code != 404 {
		t.Fatalf("missing UI: %d", w.Code)
	}
	if err := os.MkdirAll(bundles, 0700); err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string]string{
		filepath.Join(dir, "index.html"):          "<title>Native settings</title>",
		filepath.Join(bundles, "index-Ab_c1.js"):  "export default 42",
		filepath.Join(bundles, "index-Ab_c1.css"): "body{color:blue}",
		filepath.Join(bundles, "index.js.map"):    "secret",
		filepath.Join(dir, "other.html"):          "secret",
		filepath.Join(root, "outside.js"):         "secret",
	} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(root, "outside.js"), filepath.Join(bundles, "link.js")); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ path, mime, content string }{
		{"/manage/?action=rollback", "text/html", "<title>Native settings</title>"},
		{"/manage/index.html", "text/html", "<title>Native settings</title>"},
		{"/manage/assets/index-Ab_c1.js", "text/javascript", "export default 42"},
		{"/manage/assets/index-Ab_c1.css", "text/css", "body{color:blue}"},
	} {
		w := get(tc.path)
		if w.Code != 200 || w.Body.String() != tc.content || !strings.HasPrefix(w.Header().Get("Content-Type"), tc.mime) || len(w.Result().Cookies()) != 0 {
			t.Fatalf("unexpected asset response for %s: %d", tc.path, w.Code)
		}
	}
	for _, path := range []string{
		"/manage/assets", "/manage/assets/", "/manage/assets/index.js.map", "/manage/other.html",
		"/manage/assets/link.js", "/manage/assets/%2e%2e/outside.js", "/manage/assets/%2e%2e%2foutside.js",
		"/manage/assets%2Findex-Ab_c1.js", "/manage/api/config/", "/manage/assets/.hidden.js",
	} {
		w := get(path)
		if w.Code != 404 || strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), dir) {
			t.Errorf("unsafe asset response for %s: %d", path, w.Code)
		}
	}
	if err := os.Rename(bundles, filepath.Join(root, "saved")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root, bundles); err != nil {
		t.Fatal(err)
	}
	if w := get("/manage/assets/outside.js"); w.Code != 404 {
		t.Fatal("followed symlinked asset directory")
	}
}
