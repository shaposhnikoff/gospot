package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// D-Star Link Manager of the classic admin page (dstarrepeater/link_manager.php)
// and its PiStar-Keeper logbook. The classic page runs
//
//	sudo remotecontrold "<module>" link never "<reflector>"
//	sudo remotecontrold "<module>" unlink
//
// with the password in /root/.Remote Control. Here remotecontrold (in
// PISTAR_BIN_DIR) runs without root and without a shell: its argv carries only
// the module and reflector, and its HOME is a private temporary directory whose
// ".Remote Control" holds the ircDDBGateway remote port and password from
// /etc/ircddbgateway (remotePort, remotePassword). The password never reaches
// the browser, the argv or a log. Dispatch only after wizardHandler's TLS and
// Basic Auth checks.

// Set from PISTAR_KEEPER_LOG by applySettings.
var pistarKeeperLog string

type dstarManagerReply struct {
	CSRF          string   `json:"csrf"`
	Available     bool     `json:"available"`
	Remote        bool     `json:"remote"`
	Modules       []string `json:"modules"`
	Reflectors    []string `json:"reflectors"`
	DefaultName   string   `json:"defaultName"`
	DefaultLetter string   `json:"defaultLetter"`
	Keeper        []string `json:"keeper"` // nil when pistar-keeper is not running
}

const dstarInputError = "Somthing wrong with your input, try again"

// dstarReflectors lists the classic Reflector options after the default:
// DCS and XLX lines of DCS_Hosts.txt, REF and XRF lines of DPlus_Hosts.txt and
// XRF lines of DExtra_Hosts.txt, each cut to 6 characters, as listed.
func dstarReflectors() []string {
	read := func(name string, marks ...string) []string {
		list, _ := cachedHostFile("dstar-"+strings.Join(marks, ","), filepath.Join(pistarHostsDir, name), func(r io.Reader) []string {
			var out []string
			data, _ := io.ReadAll(r)
			for _, line := range strings.Split(string(data), "\n") {
				if strings.Contains(line, "#") {
					continue
				}
				for _, mark := range marks {
					if strings.Contains(line, mark) {
						out = append(out, phpSubstr(strings.TrimRight(line, "\r"), 0, 6))
					}
				}
			}
			return out
		})
		return list
	}
	var out []string
	out = append(out, read("DCS_Hosts.txt", "DCS", "XLX")...)
	out = append(out, read("DPlus_Hosts.txt", "REF", "XRF")...)
	out = append(out, read("DExtra_Hosts.txt", "XRF")...)
	return out
}

// dstarManagerAvailable follows index.php: the manager is shown for the
// DStarRepeater controller, and for MMDVMHost with the D-Star network enabled.
func dstarManagerAvailable() bool {
	switch controllerMode() {
	case "dstarrepeater":
		return true
	case "mmdvmhost":
		return mmdvmINI.load().on("D-Star Network", "Enable")
	}
	return false
}

// wxFileConfig (remotecontrold's ".Remote Control") unescapes backslashes and
// expands $VAR and %VAR%; refuse what it would read back differently.
var dstarRemotePassword = regexp.MustCompile(`^[!#&'()*+,\-./0-9:;<=>?@A-Z\[\]^_` + "`" + `a-z{|}~]{1,64}$`)

type dstarRemote struct {
	port     int
	password string
}

func dstarRemoteSettings(cfg iniData) (dstarRemote, bool) {
	port, err := strconv.Atoi(ircddbValue(cfg, "remotePort"))
	r := dstarRemote{port, ircddbValue(cfg, "remotePassword")}
	return r, ircddbValue(cfg, "remoteEnabled") == "1" && err == nil && port > 0 && port < 65536 && dstarRemotePassword.MatchString(r.password)
}

// readKeeperTail is `tail -n 5` of the PiStar-Keeper log.
func readKeeperTail() []string {
	f, err := os.Open(pistarKeeperLog)
	if err != nil {
		return []string{}
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Size() > 64<<10 {
		f.Seek(st.Size()-64<<10, io.SeekStart)
	}
	data, _ := io.ReadAll(io.LimitReader(f, 64<<10))
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > 5 {
		lines = lines[len(lines)-5:]
	}
	out := []string{}
	for _, l := range lines {
		if l != "" {
			out = append(out, strings.ToValidUTF8(strings.TrimRight(l, "\r"), "?"))
		}
	}
	return out
}

var keeperRunning = func() bool { return runningProcesses()["pistar-keeper"] }

type remoteControlRunner func(ctx context.Context, remote dstarRemote, args ...string) (string, error)

var errRemoteControlMissing = errors.New("remotecontrold is not installed")

type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := 16384 - b.Len(); room < len(p) {
		if room > 0 {
			b.Buffer.Write(p[:room])
		}
		return len(p), nil
	}
	return b.Buffer.Write(p)
}

func lastLine(b []byte) string {
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

var remoteControlMu sync.Mutex

// runRemoteControl runs PISTAR_BIN_DIR/remotecontrold with a private HOME and
// returns the last line it printed (stdout, else stderr), as exec() did.
func runRemoteControl(ctx context.Context, remote dstarRemote, args ...string) (string, error) {
	remoteControlMu.Lock()
	defer remoteControlMu.Unlock()
	bin := filepath.Join(pistarBinDir, "remotecontrold")
	if st, err := os.Stat(bin); err != nil || !st.Mode().IsRegular() {
		return "", errRemoteControlMissing
	}
	home, err := os.MkdirTemp("", "pistar-remotecontrol-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(home)
	conf := fmt.Sprintf("address=127.0.0.1\nport=%d\npassword=\"%s\"\n", remote.port, remote.password)
	if err := os.WriteFile(filepath.Join(home, ".Remote Control"), []byte(conf), 0o600); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin", "LANG=C"}
	cmd.Dir = home
	var stdout, stderr limitedBuffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = time.Second
	runErr := cmd.Run()
	var exit *exec.ExitError
	if runErr != nil && !errors.As(runErr, &exit) {
		return "", runErr
	}
	out := lastLine(stdout.Bytes())
	if out == "" {
		out = lastLine(stderr.Bytes())
	}
	if out == "" && runErr != nil {
		out = "remotecontrold: " + runErr.Error()
	}
	out = strings.ReplaceAll(strings.ToValidUTF8(out, "?"), remote.password, "********")
	return out, nil
}

var (
	dstarRefName = regexp.MustCompile(`^[A-Za-z0-9]{1,7} *$`)
	dstarLetter  = regexp.MustCompile(`^[A-Z]$`)
	dstarModule  = regexp.MustCompile(`^[A-Z0-9 ]{2,8}$`)
)

func serveDStarLinks(w http.ResponseWriter, r *http.Request, run remoteControlRunner) {
	path := r.URL.EscapedPath()
	switch {
	case path == "/manage/api/links/dstar/keeper" && r.Method == http.MethodGet:
		f, err := os.Open(pistarKeeperLog)
		if err != nil {
			http.Error(w, "Logbook not found", 404)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(pistarKeeperLog)))
		_, _ = io.Copy(w, f)
	case path == "/manage/api/links/dstar" && r.Method == http.MethodGet:
		csrf := editorToken(w, r)
		if csrf == "" {
			return
		}
		cfg := ircddbINI.load()
		reply := dstarManagerReply{CSRF: csrf, Available: dstarManagerAvailable(), Modules: []string{}, Reflectors: dstarReflectors()}
		_, reply.Remote = dstarRemoteSettings(cfg)
		for _, b := range dstarBands(cfg) {
			reply.Modules = append(reply.Modules, b.call)
		}
		if reply.Reflectors == nil {
			reply.Reflectors = []string{}
		}
		def := ircddbValue(cfg, "reflector1")
		reply.DefaultName, reply.DefaultLetter = phpSubstr(def, 0, 6), phpSubstr(def, 7, -1)
		if keeperRunning() {
			reply.Keeper = readKeeperTail()
		}
		writeJSON(w, 200, reply)
	case path == "/manage/api/links/dstar" && r.Method == http.MethodPost:
		var in struct {
			Action  string `json:"action"`
			Module  string `json:"module"`
			RefName string `json:"refName"`
			Letter  string `json:"letter"`
			Custom  bool   `json:"custom"`
		}
		if !maintenancePost(w, r, &in) {
			return
		}
		if !dstarManagerAvailable() {
			http.Error(w, "D-Star Link Manager is not available", 409)
			return
		}
		link := in.Action == "LINK"
		if !link && in.Action != "UNLINK" || !dstarModule.MatchString(in.Module) ||
			link && (!dstarRefName.MatchString(in.RefName) || !dstarLetter.MatchString(in.Letter)) {
			http.Error(w, dstarInputError, 400)
			return
		}
		cfg := ircddbINI.load()
		// Fix the module to 8 characters: callsign padded to 7 + band letter.
		module := fmt.Sprintf("%-7s%s", strings.TrimRight(in.Module[:len(in.Module)-1], " "), in.Module[len(in.Module)-1:])
		known := false
		for _, b := range dstarBands(cfg) {
			known = known || b.call == module
		}
		if !known {
			http.Error(w, "Unknown radio module", 422)
			return
		}
		args := []string{module, "unlink"}
		if link {
			name := strings.ToUpper(strings.TrimRight(in.RefName, " "))
			listed := name == strings.ToUpper(phpSubstr(ircddbValue(cfg, "reflector1"), 0, 6)) || slices.Contains(dstarReflectors(), name)
			if !in.Custom && !listed {
				http.Error(w, "Unknown reflector", 422)
				return
			}
			target := fmt.Sprintf("%-7s%s", name, in.Letter)
			if target == module { // the classic sanity check
				writeJSON(w, 200, map[string]string{"output": "Cannot link to myself - Aborting link request!"})
				return
			}
			args = []string{module, "link", "never", target}
		}
		remote, ok := dstarRemoteSettings(cfg)
		if !ok {
			http.Error(w, "ircDDBGateway Remote Control is not enabled", 409)
			return
		}
		out, err := run(r.Context(), remote, args...)
		if err != nil {
			http.Error(w, "remotecontrold could not be started", 502)
			return
		}
		writeJSON(w, 200, map[string]string{"output": out})
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Метод не поддерживается", 405)
	}
}
