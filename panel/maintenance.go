package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Classic Live Logs, Power and Update pages (admin/live_modem_log.php,
// power.php, update.php). Dispatch only after wizardHandler's TLS and Basic
// Auth checks.

const logChunk = 256 << 10

// Set from settings by applySettings.
var powerHelper, updateHelper, updateLog string

type maintenanceRunner func(ctx context.Context, helper, verb string) error

func runMaintenanceHelper(ctx context.Context, helper, verb string) error {
	cmd := exec.CommandContext(ctx, "/usr/bin/sudo", "-n", helper, verb)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, io.Discard, io.Discard
	cmd.WaitDelay = time.Second
	return cmd.Run()
}

// liveLogPath follows live_modem_log.php: today's MMDVMHost log, or the
// DStarRepeater log when that controller is configured.
func liveLogPath(now time.Time) string {
	day := now.UTC().Format("2006-01-02")
	if _, err := os.Stat(markerDStarRepeater); err == nil {
		for _, root := range []string{"dstarrepeaterd", "DStarRepeater"} {
			if p := filepath.Join(pistarLogDir, root+"-"+day+".log"); fileExists(p) {
				return p
			}
		}
		return ""
	}
	return filepath.Join(pistarLogDir, "MMDVM-"+day+".log")
}

type tailReply struct {
	Offset  int64  `json:"offset"`
	Data    string `json:"data"`
	Running *bool  `json:"running,omitempty"`
	CSRF    string `json:"csrf,omitempty"`
}

// tailFile returns what was appended after offset. A negative offset starts
// at the current end (the classic pages show only new lines); a file that
// shrank restarts at zero.
func tailFile(path string, offset int64) (tailReply, error) {
	f, err := os.Open(path)
	if err != nil {
		return tailReply{Offset: 0}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return tailReply{}, err
	}
	size := st.Size()
	if offset < 0 {
		return tailReply{Offset: size}, nil
	}
	if offset > size {
		offset = 0
	}
	n := min(size-offset, logChunk)
	buf := make([]byte, n)
	if _, err := f.ReadAt(buf, offset); err != nil && err != io.EOF {
		return tailReply{}, err
	}
	return tailReply{Offset: offset + n, Data: strings.ToValidUTF8(string(buf), "�")}, nil
}

func queryOffset(r *http.Request) (int64, bool) {
	values, ok := r.URL.Query()["offset"]
	if !ok {
		return -1, true
	}
	if len(values) != 1 {
		return 0, false
	}
	n, err := strconv.ParseInt(values[0], 10, 64)
	return n, err == nil && n >= 0
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func serveMaintenance(w http.ResponseWriter, r *http.Request, run maintenanceRunner, now time.Time) {
	path := r.URL.EscapedPath()
	get := r.Method == http.MethodGet
	switch {
	case path == "/manage/api/livelog" && get:
		offset, ok := queryOffset(r)
		if !ok {
			http.Error(w, "Некорректная позиция", 400)
			return
		}
		file := liveLogPath(now)
		reply, err := tailFile(file, offset)
		if err != nil && !os.IsNotExist(err) {
			http.Error(w, "Журнал недоступен", 503)
			return
		}
		writeJSON(w, 200, reply)
	case path == "/manage/api/livelog/download" && get:
		file := liveLogPath(now)
		f, err := os.Open(file)
		if err != nil {
			http.Error(w, "Журнал недоступен", 404)
			return
		}
		defer f.Close()
		host, _ := os.ReadFile(hostnameFile)
		name := "Pi-Star_" + filepath.Base(file)
		if h := strings.TrimSpace(string(host)); h != "" && h != "pi-star" && !strings.ContainsAny(h, "\"/\\\r\n") {
			name = "Pi-Star_" + h + "_" + filepath.Base(file)
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
		_, _ = io.Copy(w, f)
	case (path == "/manage/api/power" || path == "/manage/api/update") && get:
		csrf := editorToken(w, r)
		if csrf == "" {
			return
		}
		if path == "/manage/api/power" {
			writeJSON(w, 200, tailReply{CSRF: csrf})
			return
		}
		offset, ok := queryOffset(r)
		if !ok {
			http.Error(w, "Некорректная позиция", 400)
			return
		}
		reply, _ := tailFile(updateLog, max(offset, 0))
		running := updateRunning(r.Context())
		reply.Running, reply.CSRF = &running, csrf
		writeJSON(w, 200, reply)
	case path == "/manage/api/power" && r.Method == http.MethodPost:
		var in struct {
			Action string `json:"action"`
		}
		if !maintenancePost(w, r, &in) {
			return
		}
		if in.Action != "reboot" && in.Action != "shutdown" {
			http.Error(w, "Неизвестное действие", 400)
			return
		}
		if run(r.Context(), powerHelper, in.Action) != nil {
			http.Error(w, "Команда не выполнена", 502)
			return
		}
		writeJSON(w, 202, map[string]string{"action": in.Action})
	case path == "/manage/api/update" && r.Method == http.MethodPost:
		var in struct {
			Confirm bool `json:"confirm"`
		}
		if !maintenancePost(w, r, &in) {
			return
		}
		if !in.Confirm {
			http.Error(w, "Нужно подтверждение", 400)
			return
		}
		if updateRunning(r.Context()) {
			http.Error(w, "Обновление уже выполняется", 409)
			return
		}
		if run(r.Context(), updateHelper, "start") != nil {
			http.Error(w, "Не удалось запустить обновление", 502)
			return
		}
		writeJSON(w, 202, map[string]bool{"started": true})
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Метод не поддерживается", 405)
	}
}

func maintenancePost(w http.ResponseWriter, r *http.Request, out any) bool {
	if !archiveCSRF(r) {
		http.Error(w, "Перечитайте страницу", 403)
		return false
	}
	if archiveHeader(r, "Content-Type") != "application/json" {
		http.Error(w, "Ожидается JSON", 415)
		return false
	}
	if !bmDecode(w, r, out) {
		http.Error(w, "Некорректный запрос", 400)
		return false
	}
	return true
}

var updateRunning = func(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, _ := runDiagnostic(ctx, "systemctl", "is-active", "pistar-native-update.service")
	return strings.TrimSpace(string(out)) == "active"
}
