package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

const (
	archiveBackupPath  = "/manage/api/backup"
	archiveRestorePath = "/manage/api/restore"
	archiveMax         = (512 << 10) + 1024 // fixed stored ZIP; see pistar-archive
)

type archivePreview struct {
	Receipt string   `json:"receipt"`
	Changed []string `json:"changed"`
}

var archiveSlot = make(chan struct{}, 1)
var errArchiveInvalid = errors.New("invalid archive or preview")
var errArchiveBusy = errors.New("archive helper busy")

type archiveOutput struct {
	buf   bytes.Buffer
	limit int
}

func (o *archiveOutput) Write(p []byte) (int, error) {
	if len(p) > o.limit-o.buf.Len() {
		return 0, errors.New("archive helper output too large")
	}
	return o.buf.Write(p)
}

func runArchiveHelper(ctx context.Context, verb string, data []byte) ([]byte, error) {
	out := archiveOutput{limit: 1024}
	switch verb {
	case "backup":
		out.limit = archiveMax
	case "preview", "apply":
	default:
		return nil, errArchiveInvalid
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/sudo", "-n", archiveHelperPath, verb)
	cmd.Stdin, cmd.Stdout = bytes.NewReader(data), &out
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		switch exit.ExitCode() {
		case 2:
			return nil, errArchiveInvalid
		case 3:
			return nil, errArchiveBusy
		}
	}
	return out.buf.Bytes(), err
}

func archiveHex(v string) bool {
	if len(v) != 64 {
		return false
	}
	_, err := hex.DecodeString(v)
	return err == nil
}

func archiveHeader(r *http.Request, name string) string {
	if values := r.Header.Values(name); len(values) == 1 {
		return values[0]
	}
	return ""
}

func archiveCSRF(r *http.Request) bool {
	if r.TLS == nil || !strings.EqualFold(archiveHeader(r, "Origin"), "https://"+r.Host) {
		return false
	}
	var token string
	for _, c := range r.Cookies() {
		if c.Name == wizardCookie {
			if token != "" || !archiveHex(c.Value) {
				return false
			}
			token = c.Value
		}
	}
	return token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(archiveHeader(r, "X-PiStar-CSRF"))) == 1
}

// Dispatch ONLY after wizardHandler's TLS and Basic Auth checks.
func serveArchive(w http.ResponseWriter, r *http.Request) {
	handleArchive(w, r, runArchiveHelper)
}

func handleArchive(w http.ResponseWriter, r *http.Request, helper wizardHelper) {
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	if r.TLS == nil || r.URL.RawQuery != "" || r.URL.ForceQuery {
		http.Error(w, "Запрос отклонён.", http.StatusForbidden)
		return
	}
	var verb, receipt string
	switch r.URL.EscapedPath() {
	case archiveBackupPath:
		if r.Method != http.MethodGet {
			h.Set("Allow", "GET")
			http.Error(w, "Требуется GET.", 405)
			return
		}
		verb = "backup"
	case archiveRestorePath:
		if r.Method != http.MethodPost {
			h.Set("Allow", "POST")
			http.Error(w, "Требуется POST.", 405)
			return
		}
		if !archiveCSRF(r) {
			http.Error(w, "Запрос отклонён. Обновите страницу.", 403)
			return
		}
		if archiveHeader(r, "Content-Type") != "application/zip" || len(r.Header.Values("Content-Encoding")) != 0 {
			http.Error(w, "Требуется ZIP.", 415)
			return
		}
		verb = archiveHeader(r, "X-PiStar-Action")
		receipt = archiveHeader(r, "X-PiStar-Receipt")
		switch {
		case verb == "preview" && len(r.Header.Values("X-PiStar-Receipt")) == 0:
		case verb == "apply" && archiveHex(receipt):
		default:
			http.Error(w, "Требуется предпросмотр.", 400)
			return
		}
	default:
		http.NotFound(w, r)
		return
	}
	select {
	case archiveSlot <- struct{}{}:
		defer func() { <-archiveSlot }()
	default:
		http.Error(w, "Мастер занят.", 503)
		return
	}
	var data []byte
	if verb != "backup" {
		var err error
		data, err = io.ReadAll(http.MaxBytesReader(w, r.Body, archiveMax))
		if err != nil || len(data) == 0 {
			var large *http.MaxBytesError
			if errors.As(err, &large) {
				http.Error(w, "Архив слишком большой.", 413)
			} else {
				http.Error(w, "Не удалось прочитать архив.", 400)
			}
			return
		}
	}
	parent, limit := r.Context(), 15*time.Second
	if verb == "apply" {
		data = append([]byte(receipt+"\n"), data...)
		// Never strand a transaction due to a browser disconnect. The helper
		// uses a nonblocking lock and at most four bounded 20s restarts.
		parent, limit = context.WithoutCancel(parent), 105*time.Second
	}
	ctx, cancel := context.WithTimeout(parent, limit)
	defer cancel()
	out, err := helper(ctx, verb, data)
	if err != nil {
		switch {
		case errors.Is(err, errArchiveInvalid):
			http.Error(w, "Архив отклонён. Проверьте файл и повторите предпросмотр.", 422)
		case errors.Is(err, errArchiveBusy):
			http.Error(w, "Мастер занят.", 503)
		default:
			http.Error(w, "Операция не выполнена. Проверьте состояние станции.", 502)
		}
		return
	}
	if verb == "backup" {
		h.Set("Content-Type", "application/zip")
		h.Set("Content-Disposition", `attachment; filename="Pi-Star-PRIVATE.zip"`)
		_, _ = w.Write(out)
		return
	}
	h.Set("Content-Type", "application/json")
	if verb == "preview" {
		var p archivePreview
		if json.Unmarshal(out, &p) != nil || !archiveHex(p.Receipt) || p.Changed == nil || !validArchiveNames(p.Changed) {
			http.Error(w, "Не удалось проверить архив.", 502)
			return
		}
		_ = json.NewEncoder(w).Encode(p)
		return
	}
	_, _ = io.WriteString(w, `{"ok":true}`)
}

func validArchiveNames(names []string) bool {
	seen := map[string]bool{}
	for _, name := range names {
		if (name != "MMDVM-Host.ini" && name != "DMRGateway.ini") || seen[name] {
			return false
		}
		seen[name] = true
	}
	return true
}
