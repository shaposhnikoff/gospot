package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	fakeHoselineToken = "QaSpotterToken.1234"
	fakeCallSource    = 2340001
)

// fakeHoseline stands in for hose.brandmeister.network: the page publishes a
// token, the spotter checks token, subprotocol, Origin and the subscription,
// then streams one call (11, 20, 13) and waits for the relay to go away.
type fakeHoseline struct {
	srv        *httptest.Server
	pageHits   atomic.Int32
	mu         sync.Mutex
	problems   []string
	subscribed []string
	closed     chan string
	token      string
	refuse     atomic.Bool
}

var (
	// [11, 1234567890123, 2340001, 91, "M0ABC", {"slot":2}, nil, true, -1, 1.5]
	fakeStart = "9a0bcf0000011f71fb04cbce0023b4a15ba54d3041424381a4736c6f7402c0c3ffcb3ff8000000000000"
	fakeEnd   = "920dcf0000011f71fb04cb" // [13, 1234567890123]
	fakeAudio = func() []byte {
		b := make([]byte, 160)
		for i := range b {
			b[i] = byte(i)
		}
		return b
	}()
)

func newFakeHoseline(t *testing.T) *fakeHoseline {
	f := &fakeHoseline{closed: make(chan string, 16), token: fakeHoselineToken}
	mux := http.NewServeMux()
	mux.HandleFunc("/{$}", func(w http.ResponseWriter, r *http.Request) {
		f.pageHits.Add(1)
		spotter := "wss://" + f.srv.Listener.Addr().String() + "/spotter/"
		fmt.Fprintf(w, `<html><script>var a1="other",k9="%s";window.c=new Spotter("%s",k9);</script></html>`, f.currentToken(), spotter)
	})
	mux.HandleFunc("/spotter/", func(w http.ResponseWriter, r *http.Request) {
		token := f.currentToken()
		f.mu.Lock()
		if got := r.URL.Query().Get("token"); got != token {
			f.problems = append(f.problems, "token "+got)
		}
		if !headerToken(r.Header, "Sec-WebSocket-Protocol", "spotter") {
			f.problems = append(f.problems, "subprotocol "+r.Header.Get("Sec-WebSocket-Protocol"))
		}
		if o := r.Header.Get("Origin"); o != f.srv.URL {
			f.problems = append(f.problems, "origin "+o)
		}
		f.mu.Unlock()
		if f.refuse.Load() || r.URL.Query().Get("token") != token {
			http.Error(w, "denied", http.StatusUnauthorized)
			return
		}
		c, err := wsUpgrade(w, r, "spotter", 1024)
		if err != nil {
			return
		}
		op, sub, err := c.readMessage()
		if err != nil || op != wsBinary {
			f.closed <- "no subscription"
			return
		}
		v, _ := msgpackDecode(sub)
		f.mu.Lock()
		f.subscribed = append(f.subscribed, fmt.Sprint(v))
		f.mu.Unlock()
		c.writeFrame(wsText, []byte("ignored text"))
		c.writeFrame(wsBinary, []byte{0xc1}) // malformed, skipped
		c.writeFrame(wsBinary, unhex(t, fakeStart))
		audio := append([]byte{0x92, 0x14, 0xc4, byte(len(fakeAudio))}, fakeAudio...)
		c.writeFrame(wsBinary, audio[:100]) // truncated msgpack, skipped
		c.writeFrame(wsPing, []byte("hb"))
		writeFragmented(c, audio[:100], audio[100:])
		c.writeFrame(wsBinary, unhex(t, fakeEnd))
		for {
			if _, _, err := c.readMessage(); err != nil {
				f.closed <- fmt.Sprint(v)
				return
			}
		}
	})
	f.srv = httptest.NewTLSServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// writeFragmented sends one binary message as a non-final frame and a final
// continuation frame.
func writeFragmented(c *wsConn, first, rest []byte) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	a := wsFrame(wsBinary, first, nil)
	a[0] &^= 0x80
	c.conn.Write(append(a, wsFrame(wsContinuation, rest, nil)...))
}

func (f *fakeHoseline) currentToken() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.token
}

func (f *fakeHoseline) check(t *testing.T) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.problems) > 0 {
		t.Fatalf("spotter saw: %v", f.problems)
	}
}

// testHoseline is a relay pointed at the fake; the BrandMeister API is a
// local fake too, so no test reaches the real network.
func testHoseline(t *testing.T, f *fakeHoseline, bmAPI string) *hoseline {
	saved := []any{hoselinePageURL, hoselineSpotterURL, hoselineMaxListeners, hoselineTalkgroups}
	t.Cleanup(func() {
		hoselinePageURL, hoselineSpotterURL = saved[0].(string), saved[1].(string)
		hoselineMaxListeners, hoselineTalkgroups = saved[2].(int), saved[3].([]int)
	})
	hoselinePageURL = f.srv.URL + "/"
	hoselineSpotterURL = "wss://" + f.srv.Listener.Addr().String() + "/spotter/"
	hoselineMaxListeners, hoselineTalkgroups = 3, []int{91, 2504, 2550, 9990}
	h := newHoseline()
	h.client = f.srv.Client()
	h.tlsConfig = f.srv.Client().Transport.(*http.Transport).TLSClientConfig
	h.bm.base = bmAPI
	return h
}

func relayServer(t *testing.T, h *hoseline) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/hoseline", h.serve)
	mux.HandleFunc("/api/hoseline/groups", h.serve)
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func listen(s *httptest.Server, tg, origin string) (*wsConn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := wsDial(ctx, "ws"+strings.TrimPrefix(s.URL, "http")+"/api/hoseline?tg="+tg, origin, "", nil, 1<<16)
	if c != nil {
		c.idle = 5 * time.Second
	}
	return c, err
}

func nextEvent(t *testing.T, c *wsConn) (byte, []byte) {
	t.Helper()
	op, data, err := c.readMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return op, data
}

func wantEvent(t *testing.T, c *wsConn, want string) {
	t.Helper()
	if op, data := nextEvent(t, c); op != wsText || string(data) != want {
		t.Fatalf("got %d %q, want %s", op, data, want)
	}
}

func noBMFixture(t *testing.T) {
	linksFixture(t, "[DMR]\nEnable=0\n")
}

func TestHoselineRelaysOneCall(t *testing.T) {
	noBMFixture(t)
	f := newFakeHoseline(t)
	h := testHoseline(t, f, "http://127.0.0.1:1/")
	s := relayServer(t, h)
	c, err := listen(s, "2504", s.URL)
	if err != nil {
		t.Fatal(err)
	}
	wantEvent(t, c, `{"type":"listening","tg":2504}`)
	wantEvent(t, c, fmt.Sprintf(`{"type":"start","source":%d}`, fakeCallSource))
	if op, data := nextEvent(t, c); op != wsBinary || !bytes.Equal(data, fakeAudio) {
		t.Fatalf("audio: %d %x", op, data)
	}
	wantEvent(t, c, `{"type":"end"}`)
	f.check(t)
	if strings.Join(f.subscribed, ",") != "[1 [2504]]" {
		t.Fatalf("subscriptions %v", f.subscribed)
	}
	if h.count() != 1 {
		t.Fatalf("listeners %d", h.count())
	}
	// The browser goes away: the relay must close its upstream connection.
	c.close(1000, "")
	select {
	case got := <-f.closed:
		if got != "[1 [2504]]" {
			t.Fatalf("closed %s", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("upstream left open after the browser disconnected")
	}
	waitFor(t, "listener released", func() bool { return h.count() == 0 })

	// A second listener reuses the cached token.
	c, err = listen(s, "91", s.URL)
	if err != nil {
		t.Fatal(err)
	}
	wantEvent(t, c, `{"type":"listening","tg":91}`)
	c.conn.Close() // abrupt disconnect, no close frame
	select {
	case <-f.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream left open after an abrupt disconnect")
	}
	if n := f.pageHits.Load(); n != 1 {
		t.Fatalf("page fetched %d times; the token must be cached", n)
	}
}

func TestHoselineRefusals(t *testing.T) {
	noBMFixture(t)
	f := newFakeHoseline(t)
	h := testHoseline(t, f, "http://127.0.0.1:1/")
	s := relayServer(t, h)
	status := func(err error) int {
		var e *wsHandshakeError
		if errors.As(err, &e) {
			return e.Status
		}
		return 0
	}
	for _, c := range []struct {
		tg, origin string
		want       int
	}{
		{"91", "http://evil.example", 403},
		{"91", "https" + strings.TrimPrefix(s.URL, "http"), 403},
		{"91", "", 403},
		{"90", s.URL, 400},
		{"100000000", s.URL, 400},
		{"1234", s.URL, 400}, // valid number, not offered
		{"091", s.URL, 400},
		{"91&tg=2504", s.URL, 400},
		{"91&x=1", s.URL, 400},
		{"abc", s.URL, 400},
		{"", s.URL, 400},
	} {
		if _, err := listen(s, c.tg, c.origin); status(err) != c.want {
			t.Errorf("tg %q origin %q: %v, want %d", c.tg, c.origin, err, c.want)
		}
	}
	// Plain requests: not a WebSocket, wrong method.
	r, _ := http.NewRequest("GET", s.URL+"/api/hoseline?tg=91", nil)
	r.Header.Set("Origin", s.URL)
	if resp, err := http.DefaultClient.Do(r); err != nil || resp.StatusCode != 426 {
		t.Errorf("plain GET: %v %v", resp, err)
	}
	if resp, err := http.Post(s.URL+"/api/hoseline?tg=91", "text/plain", nil); err != nil || resp.StatusCode != 405 {
		t.Errorf("POST: %v %v", resp, err)
	}
	if h.count() != 0 || f.pageHits.Load() != 0 {
		t.Fatalf("refused requests reached the relay: %d listeners, %d page hits", h.count(), f.pageHits.Load())
	}

	// HOSELINE_MAX_LISTENERS=3: the fourth listener is refused with 503.
	var open []*wsConn
	for i := 0; i < 3; i++ {
		c, err := listen(s, "91", s.URL)
		if err != nil {
			t.Fatal(err)
		}
		wantEvent(t, c, `{"type":"listening","tg":91}`)
		open = append(open, c)
	}
	if _, err := listen(s, "9990", s.URL); status(err) != 503 {
		t.Fatalf("fourth listener: %v", err)
	}
	if w := httpGet(t, s.URL+"/api/hoseline/groups"); !strings.Contains(w, `"listeners":3,"max":3`) {
		t.Fatalf("groups %s", w)
	}
	open[0].close(1000, "")
	<-f.closed
	waitFor(t, "slot freed", func() bool { return h.count() == 2 })
	c, err := listen(s, "9990", s.URL)
	if err != nil {
		t.Fatalf("listener after a slot was freed: %v", err)
	}
	wantEvent(t, c, `{"type":"listening","tg":9990}`)
	for _, c := range append(open[1:], c) {
		c.close(1000, "")
	}
	waitFor(t, "all released", func() bool { return h.count() == 0 })
}

func TestHoselineUpstreamErrorsReachTheBrowser(t *testing.T) {
	noBMFixture(t)
	f := newFakeHoseline(t)
	h := testHoseline(t, f, "http://127.0.0.1:1/")
	s := relayServer(t, h)
	f.refuse.Store(true)
	c, err := listen(s, "91", s.URL)
	if err != nil {
		t.Fatal(err)
	}
	wantEvent(t, c, `{"type":"error","message":"Hoseline refused the connection"}`)
	if _, _, err := c.readMessage(); err != io.EOF {
		t.Fatalf("relay must close after the error: %v", err)
	}
	// A refused upgrade drops the cached token: the next listener refetches it.
	f.refuse.Store(false)
	f.mu.Lock()
	f.token = "RotatedToken"
	f.mu.Unlock()
	c, err = listen(s, "91", s.URL)
	if err != nil {
		t.Fatal(err)
	}
	wantEvent(t, c, `{"type":"listening","tg":91}`)
	c.close(1000, "")
	if f.pageHits.Load() != 2 {
		t.Fatalf("page hits %d", f.pageHits.Load())
	}
	waitFor(t, "released", func() bool { return h.count() == 0 })

	// A page without the token reference.
	h.forgetToken()
	h.tokenRef = regexp.MustCompile(`new\s+\w+\("wss://elsewhere\.invalid/spotter/",(\w+)\)`)
	c, err = listen(s, "91", s.URL)
	if err != nil {
		t.Fatal(err)
	}
	wantEvent(t, c, `{"type":"error","message":"Hoseline token reference not found"}`)
	waitFor(t, "released", func() bool { return h.count() == 0 })
}

func httpGet(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("GET %s: %d %s", url, resp.StatusCode, b)
	}
	return string(b)
}

func TestHoselineGroupsAddHotspotBMTalkgroupsWithoutTheKey(t *testing.T) {
	// TS2 only (Slot1=0): static slot 0 and dynamic slot 2 subscriptions count.
	linksFixture(t, "[DMR]\nEnable=1\n[DMR Network]\nEnable=1\nAddress=127.0.0.1\nSlot1=0\nSlot2=1\n")
	key := "SecretBMKey" + strings.Repeat("k", 220)
	if err := os.WriteFile(bmKeyFile, []byte("[key]\napikey="+key+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var calls []string
	var mu sync.Mutex
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path+" auth="+r.Header.Get("Authorization"))
		mu.Unlock()
		w.Write([]byte(`{"dynamicSubscriptions":[{"slot":"2","talkgroup":"9990"},{"slot":"2","talkgroup":"23426"}],` +
			`"staticSubscriptions":[{"talkgroup":"91","slot":"0"},{"talkgroup":"9","slot":"0"},{"talkgroup":"3100","slot":0}]}`))
	}))
	defer api.Close()
	f := newFakeHoseline(t)
	h := testHoseline(t, f, api.URL+"/v2/")
	s := relayServer(t, h)
	var reply struct {
		Groups         []int
		Listeners, Max int
	}
	body := httpGet(t, s.URL+"/api/hoseline/groups")
	if err := json.Unmarshal([]byte(body), &reply); err != nil {
		t.Fatal(err)
	}
	// TG 9 is below the Hoseline range; 91 and 9990 are in both lists.
	if !slices.Equal(reply.Groups, []int{91, 2504, 2550, 3100, 9990, 23426}) || reply.Max != 3 || reply.Listeners != 0 {
		t.Fatalf("groups %s", body)
	}
	if strings.Contains(body, "Secret") || strings.Contains(body, key) {
		t.Fatal("groups reply leaks the BM key")
	}
	// A BM profile talkgroup is selectable.
	c, err := listen(s, "23426", s.URL)
	if err != nil {
		t.Fatal(err)
	}
	wantEvent(t, c, `{"type":"listening","tg":23426}`)
	c.close(1000, "")
	httpGet(t, s.URL+"/api/hoseline/groups")
	mu.Lock()
	defer mu.Unlock()
	// One cached profile request for three lookups, never with the key.
	if strings.Join(calls, "\n") != "GET /v2/device/234123401/profile auth=" {
		t.Fatalf("BM API calls:\n%s", strings.Join(calls, "\n"))
	}
}

func TestHoselineRoutes(t *testing.T) {
	noBMFixture(t)
	assets := t.TempDir()
	op := operatorHandler(assets, "http://127.0.0.1:1", 8788)
	w := httptest.NewRecorder()
	op.ServeHTTP(w, httptest.NewRequest("GET", "http://node:8789/api/hoseline/groups", nil))
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"groups":[91,2504,2550,9990],"listeners":0,"max":3}` {
		t.Fatalf("operator groups: %d %s", w.Code, w.Body.String())
	}
	upgrade := func(r *http.Request, origin string) *http.Request {
		r.Header.Set("Origin", origin)
		r.Header.Set("Connection", "Upgrade")
		r.Header.Set("Upgrade", "websocket")
		r.Header.Set("Sec-WebSocket-Version", "13")
		r.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
		return r
	}
	for _, c := range []struct {
		url, origin string
		want        int
	}{
		{"http://node:8789/api/hoseline?tg=91", "http://evil:8789", 403},
		{"http://node:8789/api/hoseline?tg=91", "https://node:8789", 403},
		{"http://node:8789/api/hoseline?tg=1", "http://node:8789", 400},
	} {
		w := httptest.NewRecorder()
		op.ServeHTTP(w, upgrade(httptest.NewRequest("GET", c.url, nil), c.origin))
		if w.Code != c.want {
			t.Errorf("operator %s from %s: %d, want %d", c.url, c.origin, w.Code, c.want)
		}
	}

	// The admin listener serves the same handler behind its Basic auth.
	wizardSetup(t)
	mux := wizardTestMux(func(context.Context, string, []byte) ([]byte, error) { return nil, errors.New("unused") }, assets)
	for _, c := range []struct {
		path, origin string
		auth         bool
		want         int
	}{
		{"/manage/api/hoseline/groups", "", false, 401},
		{"/manage/api/hoseline?tg=91", "https://node", false, 401},
		{"/manage/api/hoseline/groups", "", true, 200},
		{"/manage/api/hoseline?tg=91", "http://node", true, 403},
		{"/manage/api/hoseline?tg=91", "https://evil", true, 403},
		{"/manage/api/hoseline?tg=95", "https://node", true, 400},
	} {
		r := upgrade(httptest.NewRequest("GET", "https://node"+c.path, nil), c.origin)
		if c.auth {
			r.SetBasicAuth(wizardUser, "admin-secret")
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != c.want {
			t.Errorf("admin %s from %q: %d, want %d", c.path, c.origin, w.Code, c.want)
		}
		if c.want == 200 && !strings.Contains(w.Body.String(), `"groups":[91,2504,2550,9990]`) {
			t.Errorf("admin groups %s", w.Body.String())
		}
	}
}

// The admin page CSP already covers the player: CSP3 'self' in connect-src
// matches the same-origin wss:// URL, and an AudioWorklet module is a script
// destination governed by script-src 'self' (no worker-src needed).
func TestAdminCSPAllowsTheHoselinePlayer(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/index.html", []byte("<!doctype html>"), 0o644)
	w := httptest.NewRecorder()
	serveWizardAsset(w, httptest.NewRequest("GET", "https://node/manage/", nil), dir)
	csp := w.Header().Get("Content-Security-Policy")
	for _, want := range []string{"script-src 'self';", "connect-src 'self';"} {
		if !strings.Contains(csp, want) {
			t.Fatalf("CSP %q lacks %q", csp, want)
		}
	}
	if strings.Contains(csp, "worker-src") || strings.Contains(csp, "media-src") {
		t.Fatalf("unexpected directive in %q", csp)
	}
}

func TestHoselineSettingsValidation(t *testing.T) {
	if hoselinePageURL != "https://hose.brandmeister.network/" || hoselineSpotterURL != "wss://hose.brandmeister.network/spotter/" ||
		hoselineMaxListeners != 3 || !slices.Equal(hoselineTalkgroups, []int{91, 2504, 2550, 9990}) {
		t.Fatalf("shipped defaults: %s %s %d %v", hoselinePageURL, hoselineSpotterURL, hoselineMaxListeners, hoselineTalkgroups)
	}
	h := newHoseline()
	if h.origin != "https://hose.brandmeister.network" {
		t.Fatalf("origin %s", h.origin)
	}
	page := `x=new a("wss://hose.brandmeister.network/spotter/",tk)`
	if m := h.tokenRef.FindStringSubmatch(page); m == nil || m[1] != "tk" {
		t.Fatalf("token reference regexp: %v", m)
	}
	for _, c := range [][2]string{
		{"HOSELINE_PAGE_URL", "http://hose.brandmeister.network/"},
		{"HOSELINE_PAGE_URL", "https://hose.brandmeister.network"},
		{"HOSELINE_SPOTTER_URL", "https://hose.brandmeister.network/spotter/"},
		{"HOSELINE_SPOTTER_URL", "wss://hose.brandmeister.network/spotter/?x=1"},
		{"HOSELINE_MAX_LISTENERS", "0"},
		{"HOSELINE_MAX_LISTENERS", "33"},
		{"HOSELINE_MAX_LISTENERS", "x"},
		{"HOSELINE_TALKGROUPS", ""},
		{"HOSELINE_TALKGROUPS", "91,90"},
		{"HOSELINE_TALKGROUPS", "91,,2504"},
		{"HOSELINE_TALKGROUPS", "91,100000000"},
		{"HOSELINE_TALKGROUPS", "091"},
	} {
		s, _ := readSettings(shippedSettings)
		s[c[0]] = c[1]
		if err := applySettings(s); err == nil || !strings.Contains(err.Error(), c[0]) {
			t.Errorf("%s=%s accepted: %v", c[0], c[1], err)
		}
	}
	if hoselineMaxListeners != 3 {
		t.Fatal("a rejected settings file changed the active values")
	}
}
