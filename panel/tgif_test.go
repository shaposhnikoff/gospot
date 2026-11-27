package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const testTGIFToken = "TGIFSTG1.0123456789abcdef.fedcba9876543210.abcdefghijklmnopqrstuvwxyzABCDEFGHIJ_-0123"

// tgifFixture is linksFixture plus the release file and the TGIF key file.
func tgifFixture(t *testing.T, host, gw, release string, token bool) string {
	t.Helper()
	linksFixture(t, host)
	dir := filepath.Dir(mmdvmINI.location())
	savedKey, savedRelease := tgifKeyFile, gospotReleaseFile
	t.Cleanup(func() { tgifKeyFile, gospotReleaseFile = savedKey, savedRelease })
	os.WriteFile(filepath.Join(dir, "dmrgateway"), []byte(gw), 0o644)
	gospotReleaseFile = filepath.Join(dir, "gospot-release")
	os.WriteFile(gospotReleaseFile, []byte("[GoSpot]\nVersion = "+release+"\n"), 0o644)
	tgifKeyFile = filepath.Join(dir, "tgifapi.key")
	if token {
		os.WriteFile(tgifKeyFile, []byte("[key]\napikey="+testTGIFToken+"\n"), 0o600)
	} else {
		os.WriteFile(tgifKeyFile, []byte("[key]\napikey=None\n"), 0o600)
	}
	return dir
}

const (
	tgifViaGateway = "[DMR]\nEnable=1\n[DMR Network]\nEnable=1\nAddress=127.0.0.1\nSlot1=1\nSlot2=1\n"
	tgifGateway    = "[DMR Network 1]\nEnabled=1\nAddress=3.3.3.3\nId=234123499\n" +
		"[DMR Network 2]\nEnabled=1\nAddress=tgif.network\nId=234123401\n" +
		"[DMR Network 3]\nEnabled=0\nAddress=tgif.network\nId=234123403\n"
)

func TestTGIFPanelSelection(t *testing.T) {
	cases := []struct {
		name, host, gw, release string
		token                   bool
		want                    *tgifInfo
	}{
		{"gateway: last enabled TGIF network, static", tgifViaGateway, tgifGateway, "4.3.9", true, &tgifInfo{"234123401", true}},
		{"gateway Type=Gateway, Enabled=on", "[DMR]\nEnable=1\n[DMR Network]\nType=Gateway\n", "[DMR Network 5]\nEnabled=on\nAddress=tgif.network\nId=2341234\n", "4.3.10", true, &tgifInfo{"2341234", true}},
		{"direct, DMR Id", "[General]\nId=1234567\n[DMR]\nEnable=1\nId=234123402\n[DMR Network]\nAddress=tgif.network\n", "", "4.3.9", true, &tgifInfo{"234123402", true}},
		{"direct RemoteAddress, General Id", "[General]\nId=2341234\n[DMR]\nEnable=1\n[DMR Network]\nRemoteAddress=tgif.network\n", "", "4.4.0", true, &tgifInfo{"2341234", true}},
		{"no token: legacy", tgifViaGateway, tgifGateway, "4.3.9", false, &tgifInfo{"234123401", false}},
		{"native release string: static", tgifViaGateway, tgifGateway, "4.3-native", true, &tgifInfo{"234123401", true}},
		{"no release file version: static", tgifViaGateway, tgifGateway, "", true, &tgifInfo{"234123401", true}},
		{"5 digit ID: legacy only", "[General]\nId=12345\n[DMR]\nEnable=1\n[DMR Network]\nAddress=tgif.network\n", "", "4.3.9", true, &tgifInfo{"12345", false}},
		{"only a disabled TGIF network", tgifViaGateway, "[DMR Network 3]\nEnabled=0\nAddress=tgif.network\nId=234123403\n", "4.3.9", true, nil},
		{"DMR off", "[DMR]\nEnable=0\n[DMR Network]\nAddress=tgif.network\n[General]\nId=234123401\n", "", "4.3.9", true, nil},
		{"BM master", "[DMR]\nEnable=1\nId=234123401\n[DMR Network]\nAddress=3.3.3.3\n", "", "4.3.9", true, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tgifFixture(t, c.host, c.gw, c.release, c.token)
			got := tgifPanel(mmdvmINI.load(), dmrGWINI.load())
			if (got == nil) != (c.want == nil) || (got != nil && *got != *c.want) {
				t.Fatalf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestTGIFTokenFormat(t *testing.T) {
	tgifFixture(t, tgifViaGateway, tgifGateway, "4.3.9", false)
	for body, ok := range map[string]bool{
		"[key]\napikey=" + testTGIFToken + "\n":                  true,
		"[key]\napikey=\"" + testTGIFToken + "\"\n":              true,
		"[key]\napikey=" + testTGIFToken[:len(testTGIFToken)-30]: false, // secret too short
		"[key]\napikey=TGIFSTG2" + testTGIFToken[8:] + "\n":      false,
		"[key]\napikey=" + testTGIFToken + ";rm\n":               false,
		"apikey=" + testTGIFToken + "\n":                         false,
	} {
		os.WriteFile(tgifKeyFile, []byte(body), 0o600)
		if got := readTGIFKey() != ""; got != ok {
			t.Errorf("%q: got %v", body, got)
		}
	}
	os.Remove(tgifKeyFile)
	if readTGIFKey() != "" {
		t.Fatal("missing file")
	}
}

func TestTGIFPHPCasts(t *testing.T) {
	for raw, want := range map[string]int{`91`: 91, `"91"`: 91, `"12abc"`: 12, `" 7"`: 7, `91.9`: 91, `true`: 1, `"x"`: 0, `null`: 0, `[1]`: 0, ``: 0} {
		if got := phpInt(json.RawMessage(raw)); got != want {
			t.Errorf("phpInt(%s) = %d, want %d", raw, got, want)
		}
	}
	for raw, want := range map[string]bool{`true`: false, `1`: false, `"1"`: false, `"true"`: false, `{"a":1}`: false,
		`false`: true, `0`: true, `"0"`: true, `""`: true, `null`: true, `[]`: true, ``: true} {
		if got := phpEmpty(json.RawMessage(raw)); got != want {
			t.Errorf("phpEmpty(%s) = %v", raw, got)
		}
	}
	for in, want := range map[string]string{"200": "OK", "\"404\"\n": "Not Found", "abc": "", "299": "299", "1200": "1200"} {
		if got := httpStatusText(in); got != want {
			t.Errorf("httpStatusText(%q) = %q, want %q", in, got, want)
		}
	}
}

type fakeTGIF struct {
	mu     sync.Mutex
	calls  []string
	reply  map[string]func(w http.ResponseWriter) // "METHOD /path" -> handler
	delay  time.Duration
	server *httptest.Server
}

func newFakeTGIF(t *testing.T) *fakeTGIF {
	f := &fakeTGIF{reply: map[string]func(http.ResponseWriter){}}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.calls = append(f.calls, strings.TrimSpace(r.Method+" "+r.URL.Path+" "+string(body)))
		h := f.reply[r.Method+" "+r.URL.Path]
		delay := f.delay
		f.mu.Unlock()
		if want := "Bearer " + testTGIFToken; strings.HasPrefix(r.URL.Path, "/v1/") &&
			(r.Header.Get("Authorization") != want || r.Header.Get("Accept") != "application/json" ||
				r.Header.Get("User-Agent") != "GoSpot TGIF TG Manager/234123401" ||
				(len(body) > 0) != (r.Header.Get("Content-Type") == "application/json")) {
			t.Errorf("headers of %s %s: %v", r.Method, r.URL.Path, r.Header)
		}
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Header.Get("Authorization") != "" {
			t.Errorf("legacy manager must not send the token")
		}
		if delay > 0 {
			time.Sleep(delay)
		}
		if h == nil {
			http.NotFound(w, r)
			return
		}
		h(w)
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeTGIF) on(key string, status int, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reply[key] = func(w http.ResponseWriter) { w.WriteHeader(status); w.Write([]byte(body)) }
}

func (f *fakeTGIF) take() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.calls
	f.calls = nil
	return c
}

func (f *fakeTGIF) client() *tgifClient {
	c := newTGIFClient()
	c.api, c.session = f.server.URL+"/v1/", f.server.URL+"/api/sessions/update/"
	return c
}

func tgifGet(t *testing.T, c *tgifClient) (int, string, tgifLinks) {
	t.Helper()
	w := httptest.NewRecorder()
	serveTGIF(w, maintenanceRequest("GET", "/manage/api/links/tgif", "", false), c)
	var out tgifLinks
	json.Unmarshal(w.Body.Bytes(), &out)
	if strings.Contains(w.Body.String(), testTGIFToken) || strings.Contains(w.Body.String(), "TGIFSTG1") {
		t.Fatalf("token in reply: %s", w.Body.String())
	}
	return w.Code, w.Body.String(), out
}

func TestTGIFConnections(t *testing.T) {
	tgifFixture(t, tgifViaGateway, tgifGateway, "4.3.9", true)
	f := newFakeTGIF(t)
	c := f.client()
	// Numbers as JSON numbers and as strings; reserved/foreign slots dropped.
	f.on("GET /v1/static-talkgroups/234123401", 200, `{"ok":true,"memberships":[{"slot":"1","talkgroup":"91"},{"slot":2,"talkgroup":235},`+
		`{"slot":3,"talkgroup":9},{"slot":"2","talkgroup":"0"},{"slot":2,"talkgroup":"31665"}]}`)
	f.on("GET /v1/dynamic-talkgroups/234123401", 200, `{"ok":"1","ts1_talkgroup":"0","ts2_talkgroup":9990}`)
	code, body, got := tgifGet(t, c)
	if code != 200 || got.ID != "234123401" || strings.Join(got.Static, " ") != "TG91(1) TG235(2) TG31665(2)" ||
		strings.Join(got.Dynamic, " ") != "TG9990(2)" || got.StaticState != "ok" || got.DynamicState != "ok" || len(got.Errors) != 0 {
		t.Fatalf("%d %s", code, body)
	}
	if calls := f.take(); len(calls) != 2 {
		t.Fatalf("calls: %v", calls)
	}
	// Empty lists are "None" on the page: state ok, no entries.
	f.on("GET /v1/static-talkgroups/234123401", 200, `{"ok":1,"memberships":[]}`)
	f.on("GET /v1/dynamic-talkgroups/234123401", 200, `{"ok":true}`)
	if _, body, got = tgifGet(t, c); got.StaticState != "ok" || len(got.Static) != 0 || got.DynamicState != "ok" || len(got.Dynamic) != 0 || len(got.Errors) != 0 {
		t.Fatal(body)
	}

	errorCases := []struct {
		status          int
		body, want      string
		dynState        string
		staticReplyOnly bool
	}{
		{401, `{"ok":false,"error":"invalid_token"}`, "The Static TG API token is invalid or has been revoked.", "unavailable", false},
		{403, `{"error":"dynamic_tg_permission_required"}`, "This TGIF API token cannot manage Dynamic Talkgroups. Re-create the token on tgif.network, then save the new one in Expert > API Keys.", "denied", false},
		{429, `{}`, "TGIF rate limited the request. Wait before trying again.", "unavailable", false},
		{503, `<html>down</html>`, "TGIF could not complete the request. Try again later.", "unavailable", false},
		{404, `{"error":"no_such"}`, "TGIF API returned HTTP 404 (no_such).", "unavailable", false},
		{404, `not json`, "TGIF API returned HTTP 404.", "unavailable", false},
		{200, `{"ok":"0"}`, "TGIF API returned HTTP 200.", "unavailable", false},
		{200, `{"ok":false,"error":"device_not_connected"}`, "The hotspot is not currently connected to TGIF.", "unavailable", false},
	}
	for _, e := range errorCases {
		f.on("GET /v1/static-talkgroups/234123401", e.status, e.body)
		f.on("GET /v1/dynamic-talkgroups/234123401", e.status, e.body)
		_, body, got := tgifGet(t, c)
		if got.StaticState != "unavailable" || got.DynamicState != e.dynState || len(got.Static)+len(got.Dynamic) != 0 ||
			strings.Join(got.Errors, "|") != "Static: "+e.want+"|Dynamic: "+e.want {
			t.Errorf("%d %s: %s", e.status, e.body, body)
		}
	}
	// A hung API is cut off by the client timeout.
	f.delay, c.client.Timeout = 300*time.Millisecond, 50*time.Millisecond
	start := time.Now()
	if _, body, got = tgifGet(t, c); strings.Join(got.Errors, "|") != "Static: No response from the TGIF API.|Dynamic: No response from the TGIF API." {
		t.Fatal(body)
	}
	if time.Since(start) > 250*time.Millisecond {
		t.Fatalf("the two requests must run in parallel and honour the timeout: %v", time.Since(start))
	}
	f.delay, c.client.Timeout = 0, 5*time.Second
	// Without a token there is no static panel: nothing is fetched.
	os.WriteFile(tgifKeyFile, []byte("[key]\napikey=None\n"), 0o600)
	f.take()
	if code, _, _ = tgifGet(t, c); code != 409 || len(f.take()) != 0 {
		t.Fatalf("no token: %d", code)
	}
}

func tgifPost(c *tgifClient, body string, csrf bool) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	serveTGIF(w, maintenanceRequest("POST", "/manage/api/links/tgif", body, csrf), c)
	return w
}

func TestTGIFStaticManager(t *testing.T) {
	tgifFixture(t, tgifViaGateway, tgifGateway, "4.3.9", true)
	f := newFakeTGIF(t)
	c := f.client()
	for _, k := range []string{"POST /v1/static-talkgroups/234123401", "DELETE /v1/static-talkgroups/234123401", "POST /v1/dynamic-talkgroups/234123401"} {
		f.on(k, 200, `{"ok":true}`)
	}
	ok := []struct{ body, call, output string }{
		{`{"kind":"static","action":"ADD","tg":"235","slot":"2"}`, `POST /v1/static-talkgroups/234123401 {"slot":2,"talkgroup":235}`, "Static Talkgroup added."},
		{`{"kind":"static","action":"ADD","tg":"TG 9-1","slot":"1"}`, `POST /v1/static-talkgroups/234123401 {"slot":1,"talkgroup":91}`, "Static Talkgroup added."},
		{`{"kind":"static","action":"DEL","tg":"00000235","slot":"2"}`, `DELETE /v1/static-talkgroups/234123401 {"slot":2,"talkgroup":235}`, "Static Talkgroup removed."},
		{`{"kind":"dynamic","action":"ADD","tg":"31665","slot":"1"}`, `POST /v1/dynamic-talkgroups/234123401 {"slot":1,"talkgroup":31665}`, "Dynamic Talkgroup set."},
		{`{"kind":"dynamic","action":"DEL","tg":"31665","slot":"2"}`, `POST /v1/dynamic-talkgroups/234123401 {"slot":2,"talkgroup":4000}`, "Dynamic Talkgroup unlinked."},
	}
	for _, o := range ok {
		w := tgifPost(c, o.body, true)
		var out map[string]string
		json.Unmarshal(w.Body.Bytes(), &out)
		if calls := f.take(); w.Code != 200 || out["output"] != o.output || strings.Join(calls, "\n") != o.call {
			t.Errorf("%s: %d %s %v", o.body, w.Code, w.Body.String(), calls)
		}
	}
	refused := []struct {
		body string
		code int
		msg  string
	}{
		{`{"kind":"static","action":"ADD","tg":"0","slot":"2"}`, 422, "Enter a valid talkgroup and timeslot."},
		{`{"kind":"static","action":"ADD","tg":"","slot":"2"}`, 422, "Enter a valid talkgroup and timeslot."},
		{`{"kind":"static","action":"ADD","tg":"123456789","slot":"2"}`, 422, "Enter a valid talkgroup and timeslot."},
		{`{"kind":"static","action":"ADD","tg":"235","slot":"3"}`, 422, "Enter a valid talkgroup and timeslot."},
		{`{"kind":"dynamic","action":"ADD","tg":"235","slot":"0"}`, 422, "Enter a valid talkgroup and timeslot."},
		{`{"kind":"static","action":"LINK","tg":"235","slot":"2"}`, 422, "Choose Add or Remove."},
		{`{"kind":"other","action":"ADD","tg":"235","slot":"2"}`, 400, "Unknown action"},
		{`{"kind":"legacy","action":"LINK","tg":"235","slot":"2"}`, 409, "TGIF Manager is not available"},
		{`{"kind":"static","action":"ADD","tg":235,"slot":"2"}`, 400, "Некорректный запрос"},
		{`{"kind":"static","action":"ADD","tg":"235","slot":"2","token":"x"}`, 400, "Некорректный запрос"},
	}
	for _, x := range refused {
		w := tgifPost(c, x.body, true)
		if w.Code != x.code || strings.TrimSpace(w.Body.String()) != x.msg {
			t.Errorf("%s: %d %s", x.body, w.Code, w.Body.String())
		}
	}
	if w := tgifPost(c, ok[0].body, false); w.Code != 403 {
		t.Fatalf("missing CSRF header: %d", w.Code)
	}
	r := maintenanceRequest("POST", "/manage/api/links/tgif", ok[0].body, true)
	r.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	if serveTGIF(w, r, c); w.Code != 403 {
		t.Fatalf("cross-origin: %d", w.Code)
	}
	if calls := f.take(); len(calls) != 0 {
		t.Fatalf("refused requests reached TGIF: %v", calls)
	}
	// TGIF refusals become the classic friendly text; the token never leaks.
	f.on("POST /v1/static-talkgroups/234123401", 409, `{"ok":false,"error":"limit_exceeded","token":"`+testTGIFToken+`"}`)
	w = tgifPost(c, ok[0].body, true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "The Static Talkgroup limit for this account has been reached.") || strings.Contains(w.Body.String(), "TGIFSTG1") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	f.on("POST /v1/static-talkgroups/234123401", 200, `{"ok":false}`)
	if w := tgifPost(c, ok[0].body, true); !strings.Contains(w.Body.String(), "TGIF API returned HTTP 200.") {
		t.Fatal(w.Body.String())
	}
	f.server.Close()
	if w := tgifPost(c, ok[0].body, true); !strings.Contains(w.Body.String(), "No response from the TGIF API.") {
		t.Fatal(w.Body.String())
	}
	// No token: the static manager is gone.
	os.Remove(tgifKeyFile)
	if w := tgifPost(c, ok[0].body, true); w.Code != 409 {
		t.Fatalf("no token: %d", w.Code)
	}
	w = httptest.NewRecorder()
	serveTGIF(w, maintenanceRequest("PUT", "/manage/api/links/tgif", "", false), c)
	if w.Code != 405 {
		t.Fatal(w.Code)
	}
}

func TestTGIFLegacyManager(t *testing.T) {
	tgifFixture(t, tgifViaGateway, tgifGateway, "4.3.9", false)
	f := newFakeTGIF(t)
	c := f.client()
	f.on("GET /api/sessions/update/234123401/0/91", 200, "200")
	f.on("GET /api/sessions/update/234123401/1/4000", 200, `"200"`)
	f.on("GET /api/sessions/update/234123401/1/235", 200, "nonsense")
	cases := []struct{ body, call, output string }{
		{`{"kind":"legacy","action":"LINK","tg":"91","slot":"1"}`, "GET /api/sessions/update/234123401/0/91", "TGIF API: OK"},
		{`{"kind":"legacy","action":"UNLINK","tg":"91","slot":"2"}`, "GET /api/sessions/update/234123401/1/4000", "TGIF API: OK"},
		{`{"kind":"legacy","action":"LINK","tg":"","slot":"2"}`, "GET /api/sessions/update/234123401/1/4000", "TGIF API: OK"},
		{`{"kind":"legacy","action":"LINK","tg":"TG235","slot":"2"}`, "GET /api/sessions/update/234123401/1/235", "TGIF API: "},
		{`{"kind":"legacy","action":"LINK","tg":"9","slot":"2"}`, "GET /api/sessions/update/234123401/1/9", "TGIF API: Not Found"},
	}
	for _, x := range cases {
		w := tgifPost(c, x.body, true)
		var out map[string]string
		json.Unmarshal(w.Body.Bytes(), &out)
		if calls := f.take(); w.Code != 200 || out["output"] != x.output || strings.Join(calls, "\n") != x.call {
			t.Errorf("%s: %d %s %v", x.body, w.Code, w.Body.String(), calls)
		}
	}
	for body, msg := range map[string]string{
		`{"kind":"legacy","action":"LINK","tg":"12345678","slot":"2"}`: "Enter a valid talkgroup and timeslot.",
		`{"kind":"legacy","action":"LINK","tg":"91","slot":"3"}`:       "Enter a valid talkgroup and timeslot.",
		`{"kind":"legacy","action":"ADD","tg":"91","slot":"2"}`:        "Choose Link or UnLink.",
		`{"kind":"static","action":"ADD","tg":"91","slot":"2"}`:        "TGIF Static TG Manager is not available",
	} {
		w := tgifPost(c, body, true)
		if (w.Code != 422 && w.Code != 409) || strings.TrimSpace(w.Body.String()) != msg {
			t.Errorf("%s: %d %s", body, w.Code, w.Body.String())
		}
	}
	if w := tgifPost(c, cases[0].body, false); w.Code != 403 {
		t.Fatalf("CSRF: %d", w.Code)
	}
	if calls := f.take(); len(calls) != 0 {
		t.Fatalf("refused requests reached TGIF: %v", calls)
	}
	// Simplex hotspot (Slot1=0): always TS2, whatever the form says.
	os.WriteFile(mmdvmINI.location(), []byte(strings.Replace(tgifViaGateway, "Slot1=1", "Slot1=0", 1)+"\n"), 0o644)
	tgifPost(c, `{"kind":"legacy","action":"LINK","tg":"91","slot":"9"}`, true)
	if calls := f.take(); strings.Join(calls, "") != "GET /api/sessions/update/234123401/1/91" {
		t.Fatalf("simplex: %v", calls)
	}
	f.server.Close()
	if w := tgifPost(c, cases[0].body, true); !strings.Contains(w.Body.String(), "TGIF API: No Response") {
		t.Fatal(w.Body.String())
	}
	// Static context present: the legacy manager is not offered.
	os.WriteFile(tgifKeyFile, []byte("[key]\napikey="+testTGIFToken+"\n"), 0o600)
	if w := tgifPost(c, cases[0].body, true); w.Code != 409 {
		t.Fatalf("legacy with token: %d", w.Code)
	}
}

func TestLinksReplyHasTGIFPanel(t *testing.T) {
	tgifFixture(t, tgifViaGateway, tgifGateway, "4.3.9", true)
	get := func() (linksReply, string) {
		w := httptest.NewRecorder()
		serveLinks(w, maintenanceRequest("GET", "/manage/api/links", "", false), newBMManager(nil), nil)
		var r linksReply
		json.Unmarshal(w.Body.Bytes(), &r)
		return r, w.Body.String()
	}
	r, body := get()
	if r.TGIF == nil || *r.TGIF != (tgifInfo{"234123401", true}) || strings.Contains(body, "TGIFSTG1") {
		t.Fatalf("%s", body)
	}
	os.Remove(tgifKeyFile)
	if r, body = get(); r.TGIF == nil || r.TGIF.Static {
		t.Fatalf("legacy expected: %s", body)
	}
}

func TestTGIFSettingsValidated(t *testing.T) {
	bad := []struct{ key, value string }{
		{"TGIF_API_URL", "http://api.tgif.network/v1/"}, // the token never goes over plain HTTP
		{"TGIF_API_URL", "https://api.tgif.network/v1"},
		{"TGIF_API_URL", "https://user:pw@api.tgif.network/v1/"},
		{"TGIF_API_URL", "https://api.tgif.network/v1/?x=1"},
		{"TGIF_API_URL", ""},
		{"TGIF_SESSION_URL", "ftp://tgif.network/"},
		{"TGIF_SESSION_URL", "http://tgif.network:5040/api/sessions/update"},
		{"TGIF_SESSION_URL", "/api/sessions/update/"},
	}
	for _, b := range bad {
		s, _ := readSettings(shippedSettings)
		if s["TGIF_API_URL"] != "https://api.tgif.network/v1/" || s["TGIF_SESSION_URL"] != "http://tgif.network:5040/api/sessions/update/" {
			t.Fatalf("shipped defaults: %v %v", s["TGIF_API_URL"], s["TGIF_SESSION_URL"])
		}
		s[b.key] = b.value
		if err := applySettings(s); err == nil || !strings.Contains(err.Error(), b.key) {
			t.Errorf("%s=%s accepted: %v", b.key, b.value, err)
		}
	}
	if tgifAPIURL != "https://api.tgif.network/v1/" || tgifSessionURL != "http://tgif.network:5040/api/sessions/update/" || tgifKeyFile != "/etc/tgifapi.key" {
		t.Fatalf("active: %s %s %s", tgifAPIURL, tgifSessionURL, tgifKeyFile)
	}
}
