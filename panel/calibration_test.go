package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// fakeCal stands in for `sudo pistar-calibrate run`: it records the bytes the
// panel writes, prints a version line and ends on q or EOF unless stubborn.
type fakeCal struct {
	mu       sync.Mutex
	input    []byte
	closed   bool
	termed   bool
	stubborn bool
	outW     *io.PipeWriter
	exit     chan struct{}
}

func (f *fakeCal) got() (string, bool, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return string(f.input), f.closed, f.termed
}

func (f *fakeCal) proc() *calProc {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	f.outW, f.exit = outW, make(chan struct{})
	var once sync.Once
	end := func() { once.Do(func() { outW.Write([]byte("Finnished...\r\n")); outW.Close(); close(f.exit) }) }
	go func() {
		outW.Write([]byte("Version: 1, description: fake\r\n"))
		buf := make([]byte, 64)
		for {
			n, err := inR.Read(buf)
			f.mu.Lock()
			f.input = append(f.input, buf[:n]...)
			quit := strings.Contains(string(buf[:n]), "q")
			stubborn := f.stubborn
			f.mu.Unlock()
			if (quit || err != nil) && !stubborn {
				end()
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return &calProc{stdin: closeRecorder{inW, f}, stdout: outR, wait: func() error { <-f.exit; return nil },
		term: func() error { f.mu.Lock(); f.termed = true; f.mu.Unlock(); end(); return nil }}
}

// closeRecorder notes that the panel closed the helper's stdin.
type closeRecorder struct {
	*io.PipeWriter
	f *fakeCal
}

func (c closeRecorder) Close() error {
	c.f.mu.Lock()
	c.f.closed = true
	c.f.mu.Unlock()
	return c.PipeWriter.Close()
}

type calRig struct {
	t       *testing.T
	fake    *fakeCal
	cal     *calibrator
	starts  int
	withTX  bool
	mu      sync.Mutex
	sudo    []string
	applied []string
	stale   bool
}

func newCalRig(t *testing.T) *calRig {
	rig := &calRig{t: t, fake: &fakeCal{}}
	start := func() (*calProc, error) {
		rig.mu.Lock()
		rig.starts++
		rig.mu.Unlock()
		return rig.fake.proc(), nil
	}
	sudo := func(_ context.Context, args ...string) ([]byte, error) {
		rig.mu.Lock()
		rig.sudo = append(rig.sudo, filepath.Base(args[0])+" "+strings.Join(args[1:], " "))
		rig.mu.Unlock()
		return nil, nil
	}
	rig.cal = newCalibrator(start, sudo)
	rig.cal.paramDelay, rig.cal.quitGrace, rig.cal.idle = time.Millisecond, time.Hour, time.Hour
	return rig
}

func (rig *calRig) helper(_ context.Context, verb string, input []byte) ([]byte, error) {
	switch verb {
	case "read":
		if rig.withTX {
			return []byte(`{"revision":"r1","files":{"mmdvmhost":{"Info":{"RXFrequency":"435000000"},"Modem":{"RXOffset":"100","TXOffset":"100"}}},"layout":{}}`), nil
		}
		return []byte(`{"revision":"r1","files":{"mmdvmhost":{"Info":{"RXFrequency":"435000000"},"Modem":{"RXOffset":"100"}}},"layout":{}}`), nil
	case "apply":
		rig.mu.Lock()
		rig.applied = append(rig.applied, string(input))
		rig.mu.Unlock()
		if rig.stale {
			return []byte(`{"error":"stale","stale":true}`), errConfigStale
		}
		return []byte(`{"ok":true}`), nil
	}
	return nil, errors.New("unexpected verb " + verb)
}

func (rig *calRig) do(method, target, body string, csrf bool) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	serveCalibration(w, maintenanceRequest(method, target, body, csrf), rig.cal, rig.helper)
	return w
}

func (rig *calRig) post(body string) int {
	return rig.do("POST", "/manage/api/calibration", body, true).Code
}

func (rig *calRig) calls() []string {
	rig.mu.Lock()
	defer rig.mu.Unlock()
	return append([]string(nil), rig.sudo...)
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(2 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestCalibrationCSRF(t *testing.T) {
	rig := newCalRig(t)
	for _, body := range []string{`{"action":"start"}`, `{"action":"cmd","cmd":"k"}`, `{"action":"saveoffset","param":"5","revision":"r1"}`} {
		if w := rig.do("POST", "/manage/api/calibration", body, false); w.Code != 403 {
			t.Fatalf("%s without CSRF: %d", body, w.Code)
		}
	}
	if rig.starts != 0 || len(rig.applied) != 0 {
		t.Fatalf("rejected requests must do nothing: %d starts, %v", rig.starts, rig.applied)
	}
}

func TestCalibrationSession(t *testing.T) {
	rig := newCalRig(t)
	// The page load: values shown by calibration.php and the output end.
	w := rig.do("GET", "/manage/api/calibration", "", false)
	var init struct {
		CSRF        string `json:"csrf"`
		Revision    string `json:"revision"`
		RXFrequency int    `json:"rxFrequency"`
		RXOffset    int    `json:"rxOffset"`
		Running     bool   `json:"running"`
		Offset      int64  `json:"offset"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &init) != nil || len(init.CSRF) != 64 || init.Revision != "r1" ||
		init.RXFrequency != 435000000 || init.RXOffset != 100 || init.Running {
		t.Fatalf("init: %d %s", w.Code, w.Body.String())
	}
	if code := rig.post(`{"action":"cmd","cmd":"k"}`); code != 409 {
		t.Fatalf("command without a session: %d", code)
	}
	if code := rig.post(`{"action":"start"}`); code != 202 {
		t.Fatalf("start: %d", code)
	}
	if code := rig.post(`{"action":"start"}`); code != 409 {
		t.Fatalf("second start must be refused: %d", code)
	}
	if rig.starts != 1 {
		t.Fatalf("one session only: %d starts", rig.starts)
	}
	// Unknown keys and arguments never reach MMDVMCal.
	for _, body := range []string{
		`{"action":"cmd","cmd":"x"}`, `{"action":"cmd","cmd":"kq"}`, `{"action":"cmd","cmd":"H"}`, `{"action":"cmd","cmd":""}`,
		`{"action":"cmd","cmd":"k","param":"1"}`, `{"action":"cmd","cmd":"z","param":"30"}`, `{"action":"cmd","cmd":"z"}`,
		`{"action":"cmd","cmd":"e"}`, `{"action":"cmd","cmd":"e","param":"43500000;reboot"}`, `{"action":"cmd","cmd":"e","param":"12345"}`,
		`{"action":"reboot"}`, `{"action":"cmd","cmd":"k","extra":1}`,
	} {
		if code := rig.post(body); code != 400 {
			t.Errorf("%s: %d", body, code)
		}
	}
	for _, body := range []string{`{"action":"cmd","cmd":"k"}`, `{"action":"cmd","cmd":"z","param":"50"}`,
		`{"action":"cmd","cmd":"e","param":"435000100"}`, `{"action":"cmd","cmd":"F"}`} {
		if code := rig.post(body); code != 200 {
			t.Fatalf("%s: %d", body, code)
		}
	}
	waitFor(t, "keys", func() bool { in, _, _ := rig.fake.got(); return in == "kz50\ne435000100\nF" })
	// Polling from the page-load offset returns the new output.
	waitFor(t, "output", func() bool {
		var poll tailReply
		w := rig.do("GET", "/manage/api/calibration?offset=0", "", false)
		return json.Unmarshal(w.Body.Bytes(), &poll) == nil && strings.Contains(poll.Data, "Version: 1") && *poll.Running
	})
	// Stop (q): MMDVMCal quits, stdin closes, the helper restores; then restore runs.
	if code := rig.post(`{"action":"cmd","cmd":"q"}`); code != 200 {
		t.Fatalf("stop: %d", code)
	}
	waitFor(t, "session end", func() bool { return len(rig.calls()) == 1 })
	in, closed, termed := rig.fake.got()
	if in != "kz50\ne435000100\nFq" || !closed || termed {
		t.Fatalf("stop: input %q closed %v termed %v", in, closed, termed)
	}
	if got := rig.calls(); got[0] != "pistar-calibrate restore" {
		t.Fatalf("restore after the session: %v", got)
	}
	waitFor(t, "not running", func() bool {
		var poll tailReply
		w := rig.do("GET", "/manage/api/calibration?offset=0", "", false)
		return json.Unmarshal(w.Body.Bytes(), &poll) == nil && !*poll.Running && strings.Contains(poll.Data, "Finnished...")
	})
	if code := rig.post(`{"action":"cmd","cmd":"k"}`); code != 409 {
		t.Fatalf("command after stop: %d", code)
	}
	rig.fake = &fakeCal{}
	if code := rig.post(`{"action":"start"}`); code != 202 || rig.starts != 2 {
		t.Fatalf("new session after stop: %d", code)
	}
	rig.cal.stop()
	waitFor(t, "second session end", func() bool { return len(rig.calls()) == 2 })
}

func TestCalibrationIdleTimeoutRestores(t *testing.T) {
	rig := newCalRig(t)
	rig.cal.idle = 40 * time.Millisecond
	if code := rig.post(`{"action":"start"}`); code != 202 {
		t.Fatalf("start: %d", code)
	}
	// Polls keep the session alive...
	for i := 0; i < 5; i++ {
		rig.do("GET", "/manage/api/calibration?offset=0", "", false)
		time.Sleep(15 * time.Millisecond)
	}
	if in, closed, _ := rig.fake.got(); in != "" || closed {
		t.Fatalf("polled session stopped: %q %v", in, closed)
	}
	// ...and a page that went away ends it like Stop.
	waitFor(t, "idle stop", func() bool { return len(rig.calls()) == 1 })
	in, closed, termed := rig.fake.got()
	if in != "q" || !closed || termed || rig.calls()[0] != "pistar-calibrate restore" {
		t.Fatalf("idle stop: %q %v %v %v", in, closed, termed, rig.calls())
	}
}

func TestCalibrationStubbornHelperGetsSIGTERM(t *testing.T) {
	rig := newCalRig(t)
	rig.fake.stubborn = true
	rig.cal.quitGrace = 20 * time.Millisecond
	if code := rig.post(`{"action":"start"}`); code != 202 {
		t.Fatalf("start: %d", code)
	}
	rig.post(`{"action":"cmd","cmd":"q"}`)
	waitFor(t, "restore", func() bool { return len(rig.calls()) == 1 })
	if _, _, termed := rig.fake.got(); !termed {
		t.Fatal("a helper that ignores q and EOF must get SIGTERM")
	}
}

func TestCalibrationSaveOffset(t *testing.T) {
	rig := newCalRig(t)
	for _, body := range []string{`{"action":"saveoffset","param":"12.5","revision":"r1"}`, `{"action":"saveoffset","param":"1e3","revision":"r1"}`,
		`{"action":"saveoffset","param":"","revision":"r1"}`, `{"action":"saveoffset","param":"1234567","revision":"r1"}`,
		`{"action":"saveoffset","param":"5"}`, `{"action":"saveoffset","param":"5\nTXOffset=9","revision":"r1"}`} {
		if code := rig.post(body); code != 400 {
			t.Errorf("%s: %d", body, code)
		}
	}
	if len(rig.applied) != 0 {
		t.Fatalf("invalid offsets must not apply: %v", rig.applied)
	}
	w := rig.do("POST", "/manage/api/calibration", `{"action":"saveoffset","param":"-0250","revision":"r1"}`, true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"revision":"r1"`) {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	want := `{"revision":"r1","changes":{"mmdvmhost":{"Modem":{"RXOffset":"-250"}}}}`
	if len(rig.applied) != 1 || rig.applied[0] != want {
		t.Fatalf("apply request: %v, want exactly %s (no restart)", rig.applied, want)
	}
	rig.stale = true
	if code := rig.post(`{"action":"saveoffset","param":"30","revision":"old"}`); code != 409 {
		t.Fatalf("stale revision: %d", code)
	}
	if rig.starts != 0 || len(rig.calls()) != 0 {
		t.Fatal("saving the offset must not start or restore anything")
	}
	// calibration.php also rewrites TXOffset when the key exists.
	rig.stale, rig.applied, rig.withTX = false, nil, true
	if code := rig.post(`{"action":"saveoffset","param":"40","revision":"r1"}`); code != 200 ||
		len(rig.applied) != 1 || rig.applied[0] != `{"revision":"r1","changes":{"mmdvmhost":{"Modem":{"RXOffset":"40","TXOffset":"40"}}}}` {
		t.Fatalf("TXOffset: %d %v", code, rig.applied)
	}
}

func TestJitterTest(t *testing.T) {
	saved := jitterLog
	defer func() { jitterLog = saved }()
	jitterLog = filepath.Join(t.TempDir(), "pi-star_icmptest.log")
	var targets []string
	release := make(chan error)
	j := &jitterRunner{start: func(target string) (<-chan error, error) {
		targets = append(targets, target)
		os.WriteFile(jitterLog, []byte(target+"_Host_1\t\tAvg RTT: 20.00ms\n"), 0o644)
		return release, nil
	}}
	do := func(method, target, body string, csrf bool) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		serveJitter(w, maintenanceRequest(method, target, body, csrf), j)
		return w
	}
	if w := do("POST", "/manage/api/expert/jitter", `{"group":"brandmeister"}`, false); w.Code != 403 {
		t.Fatalf("csrf: %d", w.Code)
	}
	for _, g := range []string{`"bm"`, `"BM"`, `"brandmeister;reboot"`, `1`} {
		if w := do("POST", "/manage/api/expert/jitter", `{"group":`+g+`}`, true); w.Code != 400 {
			t.Fatalf("group %s: %d", g, w.Code)
		}
	}
	if w := do("POST", "/manage/api/expert/jitter", `{"group":"brandmeister"}`, true); w.Code != 202 {
		t.Fatalf("start: %d", w.Code)
	}
	if w := do("POST", "/manage/api/expert/jitter", `{"group":"hblink"}`, true); w.Code != 409 {
		t.Fatalf("second run must be refused: %d", w.Code)
	}
	w := do("GET", "/manage/api/expert/jitter?offset=0", "", false)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `BM_Host_1`) || !strings.Contains(w.Body.String(), `"running":true`) {
		t.Fatalf("poll: %d %s", w.Code, w.Body.String())
	}
	close(release)
	waitFor(t, "run end", func() bool { return !j.active() })
	if w := do("POST", "/manage/api/expert/jitter", `{}`, true); w.Code != 202 {
		t.Fatalf("default group: %d", w.Code)
	}
	if strings.Join(targets, ",") != "BM,DMR+" {
		t.Fatalf("targets: %v", targets)
	}
}

func TestCalibrationSettings(t *testing.T) {
	if calibrationHelper != "/usr/lib/pistar-dashboard/pistar-calibrate" || jitterHelper != "/usr/lib/pistar-dashboard/pistar-jitter" ||
		jitterLog != "/var/log/pi-star/pi-star_icmptest.log" || jitterTimeout != 2*time.Minute || calibrationIdle != 30*time.Second {
		t.Fatalf("shipped settings: %s %s %s %v %v", calibrationHelper, jitterHelper, jitterLog, jitterTimeout, calibrationIdle)
	}
	s, _ := readSettings(shippedSettings)
	s["JITTER_TIMEOUT"], s["CALIBRATION_IDLE_TIMEOUT"], s["PISTAR_JITTER_LOG"] = "0", "1", "log"
	err := applySettings(s)
	for _, key := range []string{"JITTER_TIMEOUT", "CALIBRATION_IDLE_TIMEOUT", "PISTAR_JITTER_LOG"} {
		if err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("%s accepted: %v", key, err)
		}
	}
	if jitterTimeout != 2*time.Minute {
		t.Fatal("a rejected settings file must not change the active values")
	}
}

// TestCalibrationWithRealHelper runs the panel session against the real
// pistar-calibrate (unprivileged, its own settings file), the fake MMDVMCal of
// the QA stand and a recording systemctl: keys in, output out, Stop restores.
func TestCalibrationWithRealHelper(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not installed")
	}
	root := t.TempDir()
	helper, _ := filepath.Abs("helper/pistar-calibrate")
	fake, _ := filepath.Abs("../Pi-Star_dash_new/qa/fixtures/fake-mmdvmcal")
	for _, d := range []string{"bin", "fakebin"} {
		os.Mkdir(filepath.Join(root, d), 0o755)
	}
	os.Symlink(fake, filepath.Join(root, "bin", "MMDVMCal"))
	os.WriteFile(filepath.Join(root, "fakebin", "systemctl"), []byte("#!/bin/sh\necho \"$*\" >>\"$FAKE_SYSTEMCTL_LOG\"\nexit 0\n"), 0o755)
	os.WriteFile(filepath.Join(root, "mmdvmhost"), []byte("[Modem]\nUARTPort=/dev/ttyACM0\nUARTSpeed=115200\n"), 0o644)
	conf := filepath.Join(root, "pistar.conf")
	os.WriteFile(conf, []byte("CONF_MMDVMHOST="+root+"/mmdvmhost\nPISTAR_BIN_DIR="+root+"/bin\nDASHBOARD_STATE_DIR="+root+"/state\n"+
		"UNIT_MMDVMHOST=mmdvmhost.service\nCALIBRATION_HOLD_UNITS=pistar-watchdog.service\nCALIBRATION_WARMUP=0\nCALIBRATION_MAX_RUNTIME=60\n"), 0o600)
	env := append(os.Environ(), "PISTAR_SETTINGS="+conf, "PATH="+root+"/fakebin:/usr/bin:/bin",
		"FAKE_SYSTEMCTL_LOG="+root+"/systemctl.log", "FAKE_MMDVMCAL_LOG="+root+"/input")
	start := func() (*calProc, error) {
		cmd := exec.Command(helper, "run")
		cmd.Env = env
		in, _ := cmd.StdinPipe()
		out, _ := cmd.StdoutPipe()
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		return &calProc{stdin: in, stdout: out, wait: cmd.Wait, term: func() error { return cmd.Process.Signal(syscall.SIGTERM) }}, nil
	}
	restored := make(chan string, 4)
	sudo := func(_ context.Context, args ...string) ([]byte, error) {
		cmd := exec.Command(helper, args[1:]...)
		cmd.Env = env
		out, err := cmd.Output()
		restored <- string(out)
		return out, err
	}
	c := newCalibrator(start, sudo)
	c.paramDelay = 300 * time.Millisecond
	if err := c.begin(); err != nil {
		t.Fatal(err)
	}
	output := func() string { _, data, _ := c.tail(0); return data }
	waitFor(t, "version", func() bool { return strings.Contains(output(), "Version: 1") })
	if err := c.send("k", ""); err != nil {
		t.Fatal(err)
	}
	if err := c.send("e", "435000100"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "frequency", func() bool { return strings.Contains(output(), "New frequency: 435000100 Hz") })
	if err := c.send("q", ""); err != nil {
		t.Fatal(err)
	}
	select {
	case out := <-restored:
		if out != "nothing to restore\n" {
			t.Fatalf("the helper must have restored by itself: %q", out)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("session did not end")
	}
	in, _ := os.ReadFile(filepath.Join(root, "input"))
	calls, _ := os.ReadFile(filepath.Join(root, "systemctl.log"))
	if string(in) != "ke435000100\nq" || string(calls) != "is-active --quiet pistar-watchdog.service\nstop pistar-watchdog.service\n"+
		"stop mmdvmhost.service\nstart pistar-watchdog.service\nrestart mmdvmhost.service\n" || !strings.Contains(output(), "Finnished...") {
		t.Fatalf("input %q\nsystemctl %q\noutput %q", in, calls, output())
	}
}
