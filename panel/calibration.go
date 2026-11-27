package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Calibration (admin/calibration.php) and the Expert Jitter Test
// (admin/expert/jitter_test.php). Dispatch only after wizardHandler's TLS and
// Basic Auth checks.
//
// A calibration session is one long-running `sudo pistar-calibrate run`: the
// helper holds MMDVMHost, runs MMDVMCal in a pseudo-terminal and restores
// everything when MMDVMCal quits or its stdin closes. The panel keeps its
// stdin open while the page polls, writes only allowlisted keys into it and
// keeps the output for the page; `pistar-calibrate restore` runs after every
// session in case the helper died before it could restore.

// Set from settings by applySettings.
var calibrationHelper, jitterHelper, jitterLog string
var calibrationIdle, jitterTimeout time.Duration

func setCalibrationPaths(lib string, idle time.Duration, log string, limit time.Duration) {
	calibrationHelper, jitterHelper = filepath.Join(lib, "pistar-calibrate"), filepath.Join(lib, "pistar-jitter")
	calibrationIdle, jitterLog, jitterTimeout = idle, log, limit
}

// The MMDVMCal keys calibration.php sends: q stop, k/b/J/j/n BER test modes,
// f/F frequency down/up, z step size and e frequency (both with an argument).
var calKeys = map[string]bool{"q": true, "k": true, "b": true, "J": true, "j": true, "n": true, "f": true, "F": true, "z": true, "e": true}
var calSteps = map[string]bool{"25": true, "50": true, "100": true}

// 6..12 digits: HF to 23 cm in Hz, as calibration.php allows.
var calFrequency = regexp.MustCompile(`^[0-9]{6,12}$`)
var calOffsetValue = regexp.MustCompile(`^-?[0-9]{1,6}$`)

var errCalBusy = errors.New("a calibration is already running")
var errCalIdle = errors.New("no calibration is running")

// calProc is a started helper: its stdin, its stdout, and how to wait for or
// terminate it.
type calProc struct {
	stdin  io.WriteCloser
	stdout io.Reader
	wait   func() error
	term   func() error
}

type calStarter func() (*calProc, error)

func startCalibrationHelper() (*calProc, error) {
	cmd := exec.Command("/usr/bin/sudo", "-n", calibrationHelper, "run")
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr // the helper prints only reasons
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &calProc{stdin: in, stdout: out, wait: cmd.Wait, term: func() error { return cmd.Process.Signal(syscall.SIGTERM) }}, nil
}

// Output kept for the page; the oldest half goes when it grows past this.
const calRetain = 1 << 20

type calibrator struct {
	start      calStarter
	sudo       runFunc
	idle       time.Duration // stop when the page has not polled for this long
	paramDelay time.Duration // between a key and its argument, as calibration.php
	quitGrace  time.Duration // SIGTERM the helper if it has not ended by then
	now        func() time.Time

	write    sync.Mutex // one key (and its argument) at a time
	mu       sync.Mutex
	proc     *calProc
	done     chan struct{}
	stopping bool
	out      []byte
	base     int64 // output offset of out[0]
	seen     time.Time
}

func newCalibrator(start calStarter, sudo runFunc) *calibrator {
	return &calibrator{start: start, sudo: sudo, idle: calibrationIdle, paramDelay: 500 * time.Millisecond,
		quitGrace: time.Minute, now: time.Now}
}

func (c *calibrator) begin() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.proc != nil {
		return errCalBusy
	}
	p, err := c.start()
	if err != nil {
		return err
	}
	c.proc, c.done, c.stopping, c.seen = p, make(chan struct{}), false, c.now()
	go c.collect(p, c.done)
	go c.watch(c.done)
	return nil
}

// collect keeps the helper's output until it exits, then makes sure nothing
// stays held.
func (c *calibrator) collect(p *calProc, done chan struct{}) {
	buf := make([]byte, 4096)
	for {
		n, err := p.stdout.Read(buf)
		if n > 0 {
			c.mu.Lock()
			c.out = append(c.out, buf[:n]...)
			if len(c.out) > calRetain {
				drop := len(c.out) - calRetain/2
				c.out = append([]byte(nil), c.out[drop:]...)
				c.base += int64(drop)
			}
			c.mu.Unlock()
		}
		if err != nil {
			break
		}
	}
	_ = p.wait()
	c.write.Lock()
	_ = p.stdin.Close()
	c.write.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	_, _ = c.sudo(ctx, calibrationHelper, "restore")
	cancel()
	c.mu.Lock()
	if c.proc == p {
		c.proc = nil
	}
	c.mu.Unlock()
	close(done)
}

// watch stops a session whose page went away.
func (c *calibrator) watch(done chan struct{}) {
	t := time.NewTicker(min(c.idle/4, time.Second))
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			c.mu.Lock()
			idle := c.now().Sub(c.seen) > c.idle
			c.mu.Unlock()
			if idle {
				c.stop()
			}
		}
	}
}

// stop quits MMDVMCal and closes the helper's stdin; the helper then starts
// MMDVMHost again. A helper that does not end gets SIGTERM, which restores too.
func (c *calibrator) stop() {
	c.mu.Lock()
	p, done := c.proc, c.done
	if p == nil || c.stopping {
		c.mu.Unlock()
		return
	}
	c.stopping = true
	c.mu.Unlock()
	c.write.Lock()
	_, _ = io.WriteString(p.stdin, "q")
	_ = p.stdin.Close()
	c.write.Unlock()
	go func() {
		select {
		case <-done:
		case <-time.After(c.quitGrace):
			_ = p.term()
		}
	}()
}

// send writes one validated key, and its argument after paramDelay.
func (c *calibrator) send(key, param string) error {
	c.mu.Lock()
	p := c.proc
	if p == nil || c.stopping {
		c.mu.Unlock()
		return errCalIdle
	}
	c.mu.Unlock()
	if key == "q" {
		c.stop()
		return nil
	}
	c.write.Lock()
	defer c.write.Unlock()
	if _, err := io.WriteString(p.stdin, key); err != nil {
		return errCalIdle
	}
	if param != "" {
		time.Sleep(c.paramDelay)
		if _, err := io.WriteString(p.stdin, param+"\n"); err != nil {
			return errCalIdle
		}
	}
	return nil
}

// tail returns the output after offset; a negative offset starts at the end,
// like the classic page, which shows only what follows its load.
func (c *calibrator) tail(offset int64) (int64, string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen = c.now()
	running := c.proc != nil
	end := c.base + int64(len(c.out))
	if offset < 0 {
		return end, "", running
	}
	if offset > end || offset < c.base {
		offset = c.base
	}
	n := min(end-offset, logChunk)
	data := c.out[offset-c.base : offset-c.base+n]
	return offset + n, strings.ToValidUTF8(string(data), "�"), running
}

type calState struct {
	Revision    string `json:"revision"`
	RXFrequency int    `json:"rxFrequency"`
	RXOffset    int    `json:"rxOffset"`
}

// calibrationState reads the values calibration.php shows: RXFrequency
// ([Info], or [Modem] in files without [Info]) and [Modem] RXOffset.
func calibrationState(ctx context.Context, run configHelperFunc) (calState, error) {
	s, err := readConfigState(ctx, run)
	if err != nil {
		return calState{}, err
	}
	freq := s.get("mmdvmhost", "Info", "RXFrequency")
	if freq == "" {
		freq = s.get("mmdvmhost", "Modem", "RXFrequency")
	}
	f, _ := strconv.Atoi(freq)
	o, _ := strconv.Atoi(s.get("mmdvmhost", "Modem", "RXOffset"))
	return calState{Revision: s.Revision, RXFrequency: f, RXOffset: o}, nil
}

func serveCalibration(w http.ResponseWriter, r *http.Request, c *calibrator, run configHelperFunc) {
	switch r.Method {
	case http.MethodGet:
		csrf := editorToken(w, r)
		if csrf == "" {
			return
		}
		offset, ok := queryOffset(r)
		if !ok {
			http.Error(w, "Некорректная позиция", 400)
			return
		}
		next, data, running := c.tail(offset)
		reply := map[string]any{"csrf": csrf, "offset": next, "data": data, "running": running}
		if _, polling := r.URL.Query()["offset"]; !polling {
			state, err := calibrationState(r.Context(), run)
			if err != nil {
				http.Error(w, "Could not read the MMDVMHost configuration", 502)
				return
			}
			reply["revision"], reply["rxFrequency"], reply["rxOffset"] = state.Revision, state.RXFrequency, state.RXOffset
		}
		writeJSON(w, 200, reply)
	case http.MethodPost:
		var in struct {
			Action   string `json:"action"`
			Cmd      string `json:"cmd"`
			Param    string `json:"param"`
			Revision string `json:"revision"`
		}
		if !maintenancePost(w, r, &in) {
			return
		}
		switch in.Action {
		case "start":
			switch err := c.begin(); {
			case errors.Is(err, errCalBusy):
				http.Error(w, "A calibration is already running", 409)
			case err != nil:
				http.Error(w, "Could not start the calibration", 502)
			default:
				writeJSON(w, 202, map[string]bool{"started": true})
			}
		case "cmd":
			valid := calKeys[in.Cmd]
			switch in.Cmd {
			case "z":
				valid = calSteps[in.Param]
			case "e":
				valid = calFrequency.MatchString(in.Param)
			default:
				valid = valid && in.Param == ""
			}
			if !valid {
				http.Error(w, "Unknown command", 400)
				return
			}
			if errors.Is(c.send(in.Cmd, in.Param), errCalIdle) {
				http.Error(w, "The calibration is not running", 409)
				return
			}
			writeJSON(w, 200, map[string]bool{"ok": true})
		case "saveoffset":
			if !calOffsetValue.MatchString(in.Param) || in.Revision == "" {
				http.Error(w, "Invalid offset", 400)
				return
			}
			n, _ := strconv.Atoi(in.Param)
			v := strconv.Itoa(n)
			body, _ := json.Marshal(configChange{Revision: in.Revision,
				Changes: map[string]map[string]map[string]*string{"mmdvmhost": {"Modem": {"RXOffset": &v}}}})
			out, err := run(r.Context(), "apply", body)
			switch {
			case errors.Is(err, errConfigStale):
				http.Error(w, "The configuration changed since this page was loaded; reload it", 409)
				return
			case err != nil:
				var e struct{ Error string }
				_ = json.Unmarshal(out, &e)
				if e.Error == "" {
					e.Error = "Save failed"
				}
				http.Error(w, e.Error, 422)
				return
			}
			state, err := calibrationState(r.Context(), run)
			if err != nil {
				http.Error(w, "Saved; could not reread the configuration", 502)
				return
			}
			writeJSON(w, 200, map[string]any{"ok": true, "revision": state.Revision, "rxOffset": state.RXOffset})
		default:
			http.Error(w, "Неизвестное действие", 400)
		}
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Метод не поддерживается", 405)
	}
}

// Jitter test: jitter_test.php?group=brandmeister|dmrplus|hblink, DMR+ when
// no group is given.
var jitterGroups = map[string]string{"": "DMR+", "brandmeister": "BM", "dmrplus": "DMR+", "hblink": "HB"}

type jitterRunner struct {
	start   func(target string) (<-chan error, error)
	mu      sync.Mutex
	running bool
}

var jitterTest = &jitterRunner{start: startJitterHelper}

// startJitterHelper returns once the helper has cleared the log.
func startJitterHelper(target string) (<-chan error, error) {
	ctx, cancel := context.WithTimeout(context.Background(), jitterTimeout+30*time.Second)
	cmd := exec.CommandContext(ctx, "/usr/bin/sudo", "-n", jitterHelper, "run", target)
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 10 * time.Second
	cmd.Stderr = os.Stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	line, _ := bufio.NewReader(io.LimitReader(out, 64)).ReadString('\n')
	if line != "started\n" {
		_ = cmd.Wait()
		cancel()
		return nil, errors.New("jitter test did not start")
	}
	done := make(chan error, 1)
	go func() {
		_, _ = io.Copy(io.Discard, out)
		done <- cmd.Wait()
		cancel()
	}()
	return done, nil
}

func (j *jitterRunner) begin(target string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.running {
		return errCalBusy
	}
	done, err := j.start(target)
	if err != nil {
		return err
	}
	j.running = true
	go func() {
		<-done
		j.mu.Lock()
		j.running = false
		j.mu.Unlock()
	}()
	return nil
}

func (j *jitterRunner) active() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.running
}

func serveJitter(w http.ResponseWriter, r *http.Request, j *jitterRunner) {
	switch r.Method {
	case http.MethodGet:
		csrf := editorToken(w, r)
		if csrf == "" {
			return
		}
		offset, ok := queryOffset(r)
		if !ok {
			http.Error(w, "Некорректная позиция", 400)
			return
		}
		reply, _ := tailFile(jitterLog, offset)
		writeJSON(w, 200, map[string]any{"csrf": csrf, "running": j.active(), "offset": reply.Offset, "data": reply.Data})
	case http.MethodPost:
		var in struct {
			Group string `json:"group"`
		}
		if !maintenancePost(w, r, &in) {
			return
		}
		target, ok := jitterGroups[in.Group]
		if !ok {
			http.Error(w, "Unknown group", 400)
			return
		}
		switch err := j.begin(target); {
		case errors.Is(err, errCalBusy):
			http.Error(w, "A jitter test is already running", 409)
		case err != nil:
			http.Error(w, "Could not start the jitter test", 502)
		default:
			writeJSON(w, 202, map[string]string{"target": target})
		}
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Метод не поддерживается", 405)
	}
}
