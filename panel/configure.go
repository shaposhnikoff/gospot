package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Configuration page backend: the root helper pistar-configure reads every
// configured radio file and applies validated per-key changes atomically.

var configureHelper string

type configState struct {
	Revision string                                  `json:"revision"`
	Files    map[string]map[string]map[string]string `json:"files"`
	Layout   map[string][]struct {
		Name string
		Keys [][2]string
	} `json:"-"`
	RawLayout map[string][][2]json.RawMessage `json:"layout"`
	System    struct {
		Hostname string             `json:"hostname"`
		Timezone string             `json:"timezone"`
		Markers  map[string]*string `json:"markers"`
	} `json:"system"`
}

type configChange struct {
	Revision string                                   `json:"revision"`
	Changes  map[string]map[string]map[string]*string `json:"changes,omitempty"`
	Restart  []string                                 `json:"restart,omitempty"`
	System   map[string]any                           `json:"system,omitempty"`
}

type configHelperFunc func(ctx context.Context, verb string, input []byte) ([]byte, error)

var errConfigStale = errors.New("stale")

// The helper's read is cached while every file it reports keeps its stat
// signature; any other helper command drops the cache. A stale entry cannot
// cause a wrong write: apply and the editors check the revision themselves.
var configReadCache struct {
	sync.Mutex
	sig  string
	body []byte
}

// configWatch lists the files behind "read" (CONF_*, MARKER_*, FLAG_*,
// HOSTNAME_FILE from pistar.conf) plus the system time zone link.
var configWatch []string

var configureSudo = "/usr/bin/sudo" // replaced in tests

func configSignature() string {
	var b strings.Builder
	for _, p := range configWatch {
		st, err := os.Lstat(p)
		if err != nil {
			fmt.Fprintf(&b, "%s:-\n", p)
			continue
		}
		link, _ := os.Readlink(p)
		fmt.Fprintf(&b, "%s:%d:%d:%o:%s\n", p, st.ModTime().UnixNano(), st.Size(), st.Mode(), link)
	}
	return b.String()
}

func runConfigureHelper(ctx context.Context, verb string, input []byte) ([]byte, error) {
	if verb != "read" {
		configReadCache.Lock()
		configReadCache.sig, configReadCache.body = "", nil
		configReadCache.Unlock()
		// Writes must finish even if the browser goes away mid-restart.
		ctx = context.WithoutCancel(ctx)
		return execConfigureHelper(ctx, verb, input)
	}
	sig := configSignature()
	configReadCache.Lock()
	if configReadCache.body != nil && configReadCache.sig == sig {
		body := configReadCache.body
		configReadCache.Unlock()
		return body, nil
	}
	configReadCache.Unlock()
	out, err := execConfigureHelper(ctx, verb, input)
	if err == nil {
		configReadCache.Lock()
		configReadCache.sig, configReadCache.body = sig, out
		configReadCache.Unlock()
	}
	return out, err
}

func execConfigureHelper(ctx context.Context, verb string, input []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	// verb is an internal constant such as "read" or "full-write cron".
	cmd := exec.CommandContext(ctx, configureSudo, append([]string{"-n", configureHelper}, strings.Fields(verb)...)...)
	cmd.Stdin = bytes.NewReader(input)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr // the helper prints only reasons, never values
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 3 {
		return out.Bytes(), errConfigStale
	}
	return out.Bytes(), err
}

// get returns the stored value or "".
func (s configState) get(file, section, key string) string { return s.Files[file][section][key] }

func (s configState) has(file string) bool { return s.Files[file] != nil }

func readConfigState(ctx context.Context, run configHelperFunc) (configState, error) {
	var s configState
	out, err := run(ctx, "read", nil)
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(out, &s); err != nil {
		return s, err
	}
	// layout: {file: [[section, [[key, value], ...]], ...]}
	s.Layout = map[string][]struct {
		Name string
		Keys [][2]string
	}{}
	for file, sections := range s.RawLayout {
		for _, pair := range sections {
			var sec struct {
				Name string
				Keys [][2]string
			}
			if json.Unmarshal(pair[0], &sec.Name) != nil || json.Unmarshal(pair[1], &sec.Keys) != nil {
				return s, errors.New("invalid layout")
			}
			s.Layout[file] = append(s.Layout[file], sec)
		}
	}
	return s, nil
}

func setConfigHelper(lib string, s settings) {
	configureHelper = filepath.Join(lib, "pistar-configure")
	var watch []string
	for k := range s {
		if strings.HasPrefix(k, "CONF_") || strings.HasPrefix(k, "MARKER_") || strings.HasPrefix(k, "FLAG_") || k == "HOSTNAME_FILE" {
			if p, err := s.abs(k); err == nil {
				watch = append(watch, p)
			}
		}
	}
	sort.Strings(watch)
	configWatch = append(watch, "/etc/localtime") // the OS time zone link (timedatectl)
	configReadCache.Lock()
	configReadCache.sig, configReadCache.body = "", nil
	configReadCache.Unlock()
}

// Backup/Restore (admin/config_backup.php) through the helper's backup and
// restore commands; archives are classic Pi-Star compatible zip files.

const restoreLimit = 256 * 1024

func serveClassicBackup(w http.ResponseWriter, r *http.Request, run configHelperFunc, now time.Time) {
	switch r.Method {
	case http.MethodGet:
		if r.URL.Query().Get("csrf") == "1" {
			csrf := editorToken(w, r)
			if csrf != "" {
				writeJSON(w, 200, map[string]string{"csrf": csrf})
			}
			return
		}
		out, err := run(r.Context(), "backup", nil)
		if err != nil || len(out) == 0 {
			http.Error(w, "Backup failed", 502)
			return
		}
		host, _ := os.ReadFile(hostnameFile)
		stamp := now.Local().Format("2006-Jan-02") // PHP Y-M-d
		name := "Pi-Star_Config_" + stamp + ".zip"
		if h := strings.TrimSpace(string(host)); h != "" && h != "pi-star" && !strings.ContainsAny(h, "\"/\\\r\n") {
			name = "Pi-Star_Config_" + h + "_" + stamp + ".zip"
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
		_, _ = w.Write(out)
	case http.MethodPost:
		if !archiveCSRF(r) {
			http.Error(w, "Перечитайте страницу", 403)
			return
		}
		if archiveHeader(r, "Content-Type") != "application/zip" {
			http.Error(w, "Expected a .zip file", 415)
			return
		}
		blob, err := io.ReadAll(http.MaxBytesReader(w, r.Body, restoreLimit+1))
		if err != nil || len(blob) > restoreLimit {
			http.Error(w, "Uploaded file is too large (limit 256 KB).", 413)
			return
		}
		out, err := run(r.Context(), "restore", blob)
		var reply struct {
			OK     bool     `json:"ok"`
			Output []string `json:"output"`
		}
		if json.Unmarshal(out, &reply) != nil {
			http.Error(w, "Restore failed", 502)
			return
		}
		_ = err
		writeJSON(w, 200, reply)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Метод не поддерживается", 405)
	}
}
