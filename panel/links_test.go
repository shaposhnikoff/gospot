package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func linksFixture(t *testing.T, host string) {
	t.Helper()
	dir := t.TempDir()
	saved := []string{gospotHostsDir, mmdvmINI.location(), dmrGWINI.location(), ysfGWINI.location(), p25GWINI.location(), m17GWINI.location(), bmKeyFile}
	t.Cleanup(func() {
		gospotHostsDir = saved[0]
		mmdvmINI.setPath(saved[1])
		dmrGWINI.setPath(saved[2])
		ysfGWINI.setPath(saved[3])
		p25GWINI.setPath(saved[4])
		m17GWINI.setPath(saved[5])
		bmKeyFile = saved[6]
	})
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte(body), 0o644)
		return p
	}
	gospotHostsDir = dir
	write("YSFHosts.txt", "16710;GB SOUTH WEST;South West;1.2.3.4;42000;003;\n#comment\n")
	write("FCSHosts.txt", "FCS00290;Germany 90;x\n")
	write("P25Hosts.txt", "# TG address port\n10200\tp25.example 41000\n")
	write("M17Hosts.txt", "M17-M17\t1.2.3.4\t17000\n")
	write("DMR_Hosts.txt", "BM_2341_United_Kingdom\t0000\t3.3.3.3\tpassw0rd\t62031\n")
	mmdvmINI.setPath(write("mmdvmhost", host))
	dmrGWINI.setPath(write("dmrgateway", "[DMR Network 1]\nAddress=3.3.3.3\nId=234123401\n"))
	ysfGWINI.setPath(write("ysfgateway", "[Remote Commands]\nEnable=1\nPort=6073\n[Network]\nStartup=GB SOUTH WEST\n"))
	p25GWINI.setPath(write("p25gateway", "[Remote Commands]\nEnable=1\nPort=6074\n[Network]\nStartup=10200\n"))
	m17GWINI.setPath(write("m17gateway", "[Remote Commands]\nEnable=0\nPort=6075\n"))
	bmKeyFile = filepath.Join(dir, "bmapi.key")
}

func TestLinkCommands(t *testing.T) {
	ysf := linkManager{Mode: "YSF", Options: []linkOption{{"none", ""}, {"YSF16710", ""}, {"FCS00290", ""}}}
	m17 := linkManager{Mode: "M17", Options: []linkOption{{"none", ""}, {"M17-M17", ""}}}
	tg := func(mode string) linkManager {
		return linkManager{Mode: mode, Options: []linkOption{{"none", ""}, {"10", ""}, {"10200", ""}}}
	}
	cases := []struct {
		m                  linkManager
		link               bool
		target, room, want string
	}{
		{ysf, true, "YSF16710", "", "LinkYSF 16710"}, {ysf, true, "FCS00290", "", "LinkFCS 00290"},
		{ysf, true, "none", "", "UnLink"}, {ysf, false, "YSF16710", "", "UnLink"},
		{tg("P25"), true, "10200", "", "TalkGroup 10200"}, {tg("P25"), false, "10", "", "TalkGroup 9999"},
		{tg("NXDN"), true, "10", "", "TalkGroup 10"}, {tg("NXDN"), true, "none", "", "TalkGroup unlink"},
		{m17, true, "M17-M17", "C", "Reflector M17-M17 C"}, {m17, false, "M17-M17", "C", "Reflector unlink"},
	}
	for _, c := range cases {
		if got, err := linkCommand(c.m, c.link, c.target, c.room); err != nil || got != c.want {
			t.Errorf("%s %s: got %q %v, want %q", c.m.Mode, c.target, got, err, c.want)
		}
	}
	for _, bad := range []struct {
		m            linkManager
		target, room string
	}{{ysf, "YSF99999", ""}, {ysf, "YSF16710; reboot", ""}, {m17, "M17-M17", "Z"}, {tg("P25"), "10200 x", ""}} {
		if _, err := linkCommand(bad.m, true, bad.target, bad.room); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestLinkManagersFollowConfig(t *testing.T) {
	linksFixture(t, "[System Fusion Network]\nEnable=1\n[P25 Network]\nEnable=1\n[NXDN Network]\nEnable=0\n[M17 Network]\nEnable=1\n")
	ms := linkManagers(mmdvmINI.load())
	if len(ms) != 2 || ms[0].Mode != "YSF" || ms[1].Mode != "P25" {
		t.Fatalf("NXDN is off and M17 remote commands are disabled: %+v", ms)
	}
	if ms[0].port != 6073 || ms[0].Selected != "YSF16710" || ms[1].Selected != "10200" || ms[1].port != 6074 {
		t.Fatalf("%+v", ms)
	}
	var sent []string
	send := func(port int, cmd string) string { sent = append(sent, cmd); return "OK" }
	post := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		serveLinks(w, maintenanceRequest("POST", "/manage/api/links", body, true), newBMManager(nil), send)
		return w
	}
	if w := post(`{"mode":"YSF","action":"link","target":"YSF16710"}`); w.Code != 200 || !strings.Contains(w.Body.String(), "OK") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	for _, body := range []string{`{"mode":"YSF","action":"link","target":"YSF00009"}`, `{"mode":"NXDN","action":"link","target":"10"}`, `{"mode":"P25","action":"jump","target":"10"}`} {
		if w := post(body); w.Code != 400 {
			t.Errorf("%s: %d", body, w.Code)
		}
	}
	if strings.Join(sent, "|") != "LinkYSF 16710" {
		t.Fatalf("sent: %v", sent)
	}
}

func TestBrandMeisterManager(t *testing.T) {
	linksFixture(t, "[DMR]\nEnable=1\n[DMR Network]\nEnable=1\nAddress=127.0.0.1\nSlot1=0\nSlot2=1\n")
	var calls []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		auth := r.Header.Get("Authorization")
		if len(auth) > 7 {
			auth = auth[:7]
		}
		calls = append(calls, r.Method+" "+r.URL.Path+" "+string(body)+" "+auth)
		// The live API sends numbers as strings (api.brandmeister.network, 2026-11-27).
		w.Write([]byte(`{"autoStatic":null,"blockedGroups":[],"dynamicSubscriptions":[{"slot":"2","talkgroup":"9990"}],"clusters":[],` +
			`"staticSubscriptions":[{"talkgroup":"91","slot":"0","repeaterid":"234123401"}],"timedSubscriptions":[]}`))
	}))
	defer api.Close()
	bm := newBMManager(nil)
	bm.base = api.URL + "/v2/"
	get := func() linksReply {
		w := httptest.NewRecorder()
		serveLinks(w, maintenanceRequest("GET", "/manage/api/links", "", false), bm, nil)
		var r linksReply
		json.Unmarshal(w.Body.Bytes(), &r)
		return r
	}
	r := get()
	if r.BM == nil || r.BM.Master != "BM 2341 United Kingdom" || r.BM.ID != "234123401" || r.BM.Manager || strings.Join(r.BM.Static, ",") != "TG91" || strings.Join(r.BM.Dynamic, ",") != "TG9990(2)" {
		t.Fatalf("without key: %+v", r.BM)
	}
	post := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		serveLinks(w, maintenanceRequest("POST", "/manage/api/links/bm", body, true), bm, nil)
		return w
	}
	if w := post(`{"action":"add","tg":"235","slot":"2"}`); w.Code != 409 {
		t.Fatalf("no key must refuse: %d", w.Code)
	}
	os.WriteFile(bmKeyFile, []byte("[key]\napikey="+strings.Repeat("k", 220)+"\n"), 0o600)
	if r = get(); !r.BM.Manager {
		t.Fatal("v2 key not detected")
	}
	calls = nil
	for _, body := range []string{`{"action":"add","tg":"TG235","slot":"2"}`, `{"action":"delete","tg":"235","slot":"1"}`, `{"action":"drop-qso","slot":"2"}`} {
		if w := post(body); w.Code != 200 || !strings.Contains(w.Body.String(), "OK") {
			t.Fatalf("%s: %d %s", body, w.Code, w.Body.String())
		}
	}
	want := []string{
		`POST /v2/device/234123401/talkgroup/ {"group":"235","slot":"0"} Bearer `, // simplex: slot 0
		`DELETE /v2/device/234123401/talkgroup/0/235  Bearer `,
		`GET /v2/device/234123401/action/dropCallRoute/0  Bearer `,
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls:\n%s", strings.Join(calls, "\n"))
	}
	if w := post(`{"action":"add","tg":"0","slot":"2"}`); w.Code != 400 {
		t.Fatalf("tg 0: %d", w.Code)
	}
}
