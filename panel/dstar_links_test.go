package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// fakeRemoteControl installs a remotecontrold test double in GOSPOT_BIN_DIR
// that records its argv, environment and ".Remote Control" to record.
func fakeRemoteControl(t *testing.T, reply string) (record string) {
	t.Helper()
	bin := t.TempDir()
	saved := gospotBinDir
	t.Cleanup(func() { gospotBinDir = saved })
	gospotBinDir = bin
	record = filepath.Join(t.TempDir(), "calls")
	script := `#!/bin/sh
{ echo "argc=$#"; for a in "$@"; do echo "arg=[$a]"; done
  stat -c 'home=%a' "$HOME"; stat -c 'conf=%a' "$HOME/.Remote Control"; cat "$HOME/.Remote Control"; env | sort | sed 's/^/env=/'
  echo "$HOME" >"` + record + `.home"; echo; } >>"` + record + `"
` + reply + "\n"
	if err := os.WriteFile(filepath.Join(bin, "remotecontrold"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return record
}

const accepted = `echo "remotecontrold: command accepted by the gateway/starnetserver"`

func dstarManagerFixture(t *testing.T, controller string) {
	t.Helper()
	dir, _ := dstarFixture(t)
	savedKeeper, savedRunning := gospotKeeperLog, keeperRunning
	t.Cleanup(func() { gospotKeeperLog, keeperRunning = savedKeeper, savedRunning })
	gospotKeeperLog = filepath.Join(dir, "gospot-keeper.log")
	keeperRunning = func() bool { return false }
	switch controller {
	case "dstarrepeater":
		os.WriteFile(markerDStarRepeater, nil, 0o644)
	case "mmdvmhost":
		os.WriteFile(markerMMDVMHost, nil, 0o644)
		os.WriteFile(mmdvmINI.location(), []byte("[D-Star]\nEnable=1\n[D-Star Network]\nEnable=1\n"), 0o644)
	}
}

func dstarPost(body string, csrf bool) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	serveDStarLinks(w, maintenanceRequest("POST", "/manage/api/links/dstar", body, csrf), runRemoteControl)
	return w
}

func noSecrets(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	for _, s := range []string{"rc-Secret-7", "irc-Secret-3"} {
		if strings.Contains(w.Body.String(), s) {
			t.Fatalf("%s in reply %q", s, w.Body.String())
		}
	}
}

func TestDStarManagerOptions(t *testing.T) {
	dstarManagerFixture(t, "dstarrepeater")
	w := httptest.NewRecorder()
	serveDStarLinks(w, maintenanceRequest("GET", "/manage/api/links/dstar", "", false), runRemoteControl)
	noSecrets(t, w)
	var r dstarManagerReply
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil || w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if len(r.CSRF) != 64 || !r.Available || !r.Remote || r.Keeper != nil || r.DefaultName != "DCS001" || r.DefaultLetter != "C" ||
		!reflect.DeepEqual(r.Modules, []string{"M1ABC  B", "M1ABC  C"}) {
		t.Fatalf("%+v", r)
	}
	// link_manager.php order; commented lines skipped
	want := []string{"DCS001", "DCS002", "XLX000", "REF001", "XRF757", "XRF001", "XRF023"}
	if !reflect.DeepEqual(r.Reflectors, want) {
		t.Fatalf("reflectors %v", r.Reflectors)
	}
	// GoSpot-Keeper running: the last five logbook lines
	var lines []string
	for i := 1; i <= 7; i++ {
		lines = append(lines, "2026-11-27 entry "+string(rune('0'+i)))
	}
	os.WriteFile(gospotKeeperLog, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	keeperRunning = func() bool { return true }
	w = httptest.NewRecorder()
	serveDStarLinks(w, maintenanceRequest("GET", "/manage/api/links/dstar", "", false), runRemoteControl)
	json.Unmarshal(w.Body.Bytes(), &r)
	if !reflect.DeepEqual(r.Keeper, lines[2:]) {
		t.Fatalf("keeper %v", r.Keeper)
	}
	w = httptest.NewRecorder()
	serveDStarLinks(w, maintenanceRequest("GET", "/manage/api/links/dstar/keeper", "", false), runRemoteControl)
	if w.Code != 200 || w.Body.String() != strings.Join(lines, "\n")+"\n" || !strings.Contains(w.Header().Get("Content-Disposition"), `"gospot-keeper.log"`) {
		t.Fatalf("download %d %q", w.Code, w.Header())
	}
	os.Remove(gospotKeeperLog)
	w = httptest.NewRecorder()
	serveDStarLinks(w, maintenanceRequest("GET", "/manage/api/links/dstar/keeper", "", false), runRemoteControl)
	if w.Code != 404 {
		t.Fatalf("missing logbook %d", w.Code)
	}
}

func TestDStarManagerLinkAndUnlink(t *testing.T) {
	for _, controller := range []string{"dstarrepeater", "mmdvmhost"} {
		t.Run(controller, func(t *testing.T) {
			dstarManagerFixture(t, controller)
			record := fakeRemoteControl(t, accepted)
			calls := func() []string {
				data, _ := os.ReadFile(record)
				os.Remove(record)
				return strings.Split(strings.TrimSpace(string(data)), "\n\n")
			}
			w := dstarPost(`{"action":"LINK","module":"M1ABC  B","refName":"DCS002","letter":"B"}`, true)
			if w.Code != 200 || !strings.Contains(w.Body.String(), "remotecontrold: command accepted by the gateway/starnetserver") {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			noSecrets(t, w)
			got := calls()
			home, _ := os.ReadFile(record + ".home")
			want := strings.Join([]string{"argc=4", "arg=[M1ABC  B]", "arg=[link]", "arg=[never]", "arg=[DCS002 B]", "home=700", "conf=600",
				"address=127.0.0.1", "port=10022", `password="rc-Secret-7"`,
				"env=HOME=" + strings.TrimSpace(string(home)), "env=LANG=C", "env=PATH=/usr/bin:/bin"}, "\n")
			if len(got) != 1 || !strings.HasPrefix(got[0], want) {
				t.Fatalf("remotecontrold call:\n%s\nwant prefix\n%s", strings.Join(got, "\n--\n"), want)
			}
			if _, err := os.Stat(strings.TrimSpace(string(home))); !os.IsNotExist(err) {
				t.Fatal("the private HOME with the password must be removed")
			}
			// unlink: the module only; the reflector fields are not needed
			if w := dstarPost(`{"action":"UNLINK","module":"M1ABC C","refName":"","letter":""}`, true); w.Code != 200 {
				t.Fatalf("unlink %d %s", w.Code, w.Body.String())
			}
			if got := calls(); len(got) != 1 || !strings.HasPrefix(got[0], "argc=2\narg=[M1ABC  C]\narg=[unlink]\n") {
				t.Fatalf("unlink call %q", got)
			}
			// Text Entry: any 1-7 letters/digits, upper-cased and padded
			if w := dstarPost(`{"action":"LINK","module":"M1ABC  C","refName":"xlx123","letter":"A","custom":true}`, true); w.Code != 200 {
				t.Fatalf("custom %d %s", w.Code, w.Body.String())
			}
			if got := calls(); len(got) != 1 || !strings.Contains(got[0], "arg=[XLX123 A]\n") {
				t.Fatalf("custom call %q", got)
			}
			// the default reflector is listed even when the host lists lack it
			if w := dstarPost(`{"action":"LINK","module":"M1ABC  B","refName":"DCS001","letter":"C"}`, true); w.Code != 200 {
				t.Fatalf("default %d", w.Code)
			}
			calls()
		})
	}
}

func TestDStarManagerRefusals(t *testing.T) {
	dstarManagerFixture(t, "dstarrepeater")
	record := fakeRemoteControl(t, accepted)
	cases := []struct {
		body string
		csrf bool
		code int
		text string
	}{
		{`{"action":"LINK","module":"M1ABC  B","refName":"DCS002","letter":"B"}`, false, 403, ""},
		{`{"action":"JUMP","module":"M1ABC  B","refName":"DCS002","letter":"B"}`, true, 400, dstarInputError},
		{`{"action":"link","module":"M1ABC  B","refName":"DCS002","letter":"B"}`, true, 400, dstarInputError},
		{`{"action":"LINK","module":"M1ABC  B","refName":"DCS002;reboot","letter":"B","custom":true}`, true, 400, dstarInputError},
		{`{"action":"LINK","module":"M1ABC  B","refName":"DCS0021X","letter":"B","custom":true}`, true, 400, dstarInputError},
		{`{"action":"LINK","module":"M1ABC  B","refName":"","letter":"B"}`, true, 400, dstarInputError},
		{`{"action":"LINK","module":"M1ABC  B","refName":"DCS002","letter":"b"}`, true, 400, dstarInputError},
		{`{"action":"LINK","module":"M1ABC  B","refName":"DCS002","letter":""}`, true, 400, dstarInputError},
		{`{"action":"UNLINK","module":"m1abc  b"}`, true, 400, dstarInputError},
		{`{"action":"UNLINK","module":"M1ABC \"B"}`, true, 400, dstarInputError},
		{`{"action":"UNLINK","module":"M1ABC  B","extra":1}`, true, 400, ""},
		{`not json`, true, 400, ""},
		{`{"action":"UNLINK","module":"M1ABC  D"}`, true, 422, "Unknown radio module"},
		{`{"action":"UNLINK","module":"G0XYZ  B"}`, true, 422, "Unknown radio module"},
		{`{"action":"LINK","module":"M1ABC  B","refName":"REF999","letter":"A"}`, true, 422, "Unknown reflector"},
	}
	for _, c := range cases {
		w := dstarPost(c.body, c.csrf)
		if w.Code != c.code || !strings.Contains(w.Body.String(), c.text) {
			t.Errorf("%s: %d %q", c.body, w.Code, w.Body.String())
		}
		noSecrets(t, w)
	}
	// linking a module to itself: the classic message, nothing run
	w := dstarPost(`{"action":"LINK","module":"M1ABC  B","refName":"M1ABC","letter":"B","custom":true}`, true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Cannot link to myself - Aborting link request!") {
		t.Fatalf("self link %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(record); !os.IsNotExist(err) {
		data, _ := os.ReadFile(record)
		t.Fatalf("refused requests ran remotecontrold:\n%s", data)
	}
	// wrong method
	w = httptest.NewRecorder()
	serveDStarLinks(w, maintenanceRequest("PUT", "/manage/api/links/dstar", "", false), runRemoteControl)
	if w.Code != 405 {
		t.Fatalf("PUT %d", w.Code)
	}
}

func TestDStarManagerUnavailable(t *testing.T) {
	link := `{"action":"LINK","module":"M1ABC  B","refName":"DCS002","letter":"B"}`
	t.Run("MMDVMHost without D-Star network", func(t *testing.T) {
		dstarManagerFixture(t, "mmdvmhost")
		os.WriteFile(mmdvmINI.location(), []byte("[D-Star]\nEnable=1\n[D-Star Network]\nEnable=0\n"), 0o644)
		record := fakeRemoteControl(t, accepted)
		if w := dstarPost(link, true); w.Code != 409 {
			t.Fatalf("%d", w.Code)
		}
		if _, err := os.Stat(record); err == nil {
			t.Fatal("ran remotecontrold")
		}
	})
	for name, edit := range map[string][2]string{
		"remote disabled":         {"remoteEnabled=1", "remoteEnabled=0"},
		"no remote port":          {"remotePort=10022", "remotePort="},
		"no remote password":      {"remotePassword=rc-Secret-7", "remotePassword="},
		"unreadable for wxConfig": {"remotePassword=rc-Secret-7", "remotePassword=rc-$HOME-7"},
	} {
		t.Run(name, func(t *testing.T) {
			dstarManagerFixture(t, "dstarrepeater")
			record := fakeRemoteControl(t, accepted)
			path := ircddbINI.location()
			data, _ := os.ReadFile(path)
			os.WriteFile(path, []byte(strings.Replace(string(data), edit[0], edit[1], 1)), 0o644)
			os.Chtimes(path, time.Now(), time.Now().Add(time.Minute)) // same size: a later mtime marks the edit
			w := dstarPost(link, true)
			if w.Code != 409 || !strings.Contains(w.Body.String(), "Remote Control is not enabled") {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "HOME") {
				t.Fatal("password text in the reply")
			}
			if _, err := os.Stat(record); err == nil {
				t.Fatal("ran remotecontrold")
			}
		})
	}
}

func TestDStarManagerOutput(t *testing.T) {
	dstarManagerFixture(t, "dstarrepeater")
	link := `{"action":"LINK","module":"M1ABC  B","refName":"DCS002","letter":"B"}`
	// errors go to stderr: shown instead of the classic blank cell
	fakeRemoteControl(t, `echo "remotecontrold: invalid password sent to the gateway/starnetserver" >&2; exit 1`)
	if w := dstarPost(link, true); w.Code != 200 || !strings.Contains(w.Body.String(), "invalid password sent") {
		t.Fatalf("stderr %d %s", w.Code, w.Body.String())
	}
	// whatever the tool prints, the password is masked
	fakeRemoteControl(t, `sed -n 's/^password=//p' "$HOME/.Remote Control"`)
	w := dstarPost(link, true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `\"********\"`) {
		t.Fatalf("masked %d %s", w.Code, w.Body.String())
	}
	noSecrets(t, w)
	// not installed
	fakeRemoteControl(t, accepted)
	os.Remove(filepath.Join(gospotBinDir, "remotecontrold"))
	if w := dstarPost(link, true); w.Code != 502 {
		t.Fatalf("missing tool %d", w.Code)
	}
}

func TestDStarSettings(t *testing.T) {
	if gospotKeeperLog != "/var/gospot-keeper/gospot-keeper.log" || filepath.Join(gospotBinDir, "remotecontrold") != "/usr/local/bin/remotecontrold" {
		t.Fatalf("shipped D-Star paths: %s %s", gospotKeeperLog, gospotBinDir)
	}
	s, _ := readSettings(shippedSettings)
	for _, bad := range []string{"", "var/gospot-keeper.log", "/var/../keeper.log"} {
		s["GOSPOT_KEEPER_LOG"] = bad
		if err := applySettings(s); err == nil || !strings.Contains(err.Error(), "GOSPOT_KEEPER_LOG") {
			t.Fatalf("%q accepted: %v", bad, err)
		}
	}
	if gospotKeeperLog != "/var/gospot-keeper/gospot-keeper.log" {
		t.Fatal("a rejected settings file must not change the active paths")
	}
}
