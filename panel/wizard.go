package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Overridden by the Debian build; keep standalone deployment paths compatible.
// Set from DASHBOARD_UI_DIR and DASHBOARD_LIB_DIR by applySettings.
var adminAssets, configHelper string

// registerWizard is used only on the dedicated HTTPS management mux.
func registerWizard(mux *http.ServeMux) {
	registerWizardRoutes(mux, runWizardHelper, adminAssets)
}

func registerWizardRoutes(mux *http.ServeMux, helper wizardHelper, assetDir string) {
	handle := wizardHandler(helper, assetDir)
	mux.Handle("/{$}", handle)
	mux.Handle("/manage", handle)
	mux.Handle("/manage/", handle)
}

type wizardConfig struct {
	Callsign  string `json:"callsign"`
	RadioID   uint32 `json:"radio_id"`
	Frequency uint32 `json:"frequency"`
	UART      string `json:"uart"`
	Claim     uint32 `json:"claim"`
	Master    string `json:"master"`
	Port      uint16 `json:"port"`
}

type wizardHelper func(context.Context, string, []byte) ([]byte, error)

type wizardOutput struct{ bytes.Buffer }

func (b *wizardOutput) Write(p []byte) (int, error) {
	if len(p) > 16384-b.Len() {
		return 0, errors.New("helper output too large")
	}
	return b.Buffer.Write(p)
}

func runWizardHelper(ctx context.Context, action string, data []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "/usr/bin/sudo", "-n", configHelper, action)
	cmd.Stdin = bytes.NewReader(data)
	var out wizardOutput
	// Wrap to expose only Write, including when os/exec copies via io.Copy.
	cmd.Stdout = struct{ io.Writer }{&out}
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	return out.Bytes(), err
}

func wizardCredential() ([32]byte, bool) {
	dir := os.Getenv("CREDENTIALS_DIRECTORY")
	if filepath.IsAbs(dir) {
		f, err := os.Open(filepath.Join(dir, "admin-password"))
		if err == nil {
			defer f.Close()
			b, err := io.ReadAll(io.LimitReader(f, 4097))
			if err == nil && len(b) <= 4096 {
				b = bytes.TrimSuffix(bytes.TrimSuffix(b, []byte("\n")), []byte("\r"))
				if len(bytes.TrimSpace(b)) > 0 && !bytes.ContainsAny(b, "\r\n\x00") {
					return sha256.Sum256(b), true
				}
			}
		}
	}
	return [32]byte{}, false
}

// wizardUser is the classic Pi-Star administrator login.
const wizardUser = "pi-star"

var dashboardClient = &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

const wizardCookie = "__Host-pistar-csrf"
const wizardConfigPath = "/manage/api/config"

func wizardHandler(helper wizardHelper, assetDir string) http.HandlerFunc {
	password, ready := wizardCredential()
	slot := make(chan struct{}, 1)
	bm := newBMManager(helper)
	cal := newCalibrator(startCalibrationHelper, runSudo)
	tgif := newTGIFClient()
	hose := newHoseline()
	return func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'none'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		if r.TLS == nil || !ready {
			http.Error(w, "Управление недоступно", 503)
			return
		}
		user, pass, ok := r.BasicAuth()
		sum := sha256.Sum256([]byte(pass))
		if !ok || user != wizardUser || subtle.ConstantTimeCompare(sum[:], password[:]) != 1 {
			h.Set("WWW-Authenticate", `Basic realm="Pi-Star", charset="UTF-8"`)
			http.Error(w, "Требуется вход", 401)
			return
		}
		path := r.URL.EscapedPath()
		if strings.HasPrefix(path, "/manage/api/expert/") {
			serveExpert(w, r, runConfigureHelper)
			return
		}
		switch path {
		case "/manage/api/bm/master", "/manage/api/bm/talkgroup", "/manage/api/bm/peer", "/manage/api/bm/profile", "/manage/api/bm/key", "/manage/api/bm/subscription":
			bm.serve(w, r)
			return
		case "/manage/api/services", "/manage/api/protocols":
			serveDMRServices(w, r, helper, slot)
			return
		case "/manage/api/system":
			serveSystemInfo(w, r)
			return
		case "/manage/api/livelog", "/manage/api/livelog/download", "/manage/api/power", "/manage/api/update":
			serveMaintenance(w, r, runMaintenanceHelper, time.Now())
			return
		case "/manage/api/links", "/manage/api/links/bm":
			serveLinks(w, r, bm, sendRemoteCommand)
			return
		case "/manage/api/configure/factory-reset":
			serveFactoryReset(w, r, runConfigureHelper)
			return
		case "/manage/api/hoseline", "/manage/api/hoseline/groups":
			hose.serve(w, r)
			return
		case "/manage/api/links/dstar", "/manage/api/links/dstar/keeper":
			serveDStarLinks(w, r, runRemoteControl)
			return
		case "/manage/api/links/tgif":
			serveTGIF(w, r, tgif)
			return
		case "/manage/api/backup/classic":
			serveClassicBackup(w, r, runConfigureHelper, time.Now())
			return
		case "/manage/api/configure":
			serveConfigure(w, r, runConfigureHelper)
			return
		case "/manage/api/wifi":
			serveWifi(w, r, runConfigureHelper)
			return
		case "/manage/api/calibration":
			serveCalibration(w, r, cal, runConfigureHelper)
			return
		case "/manage/api/dashboard":
			// The classic admin page shows the same tables as the dashboard.
			if r.Method != http.MethodGet {
				h.Set("Allow", "GET")
				http.Error(w, "Метод не поддерживается", 405)
				return
			}
			operatorProxy(w, r, dashboardClient, fmt.Sprintf("http://127.0.0.1:%d", cfgPanelPort), "/api/dashboard", true)
			return
		case "/manage/api/log", "/manage/api/log/download":
			serveRadioLog(w, r)
			return
		}
		if path != "/" {
			if path != wizardConfigPath {
				serveWizardAsset(w, r, assetDir)
				return
			}
			if r.Method != http.MethodGet {
				h.Set("Allow", "GET")
				http.Error(w, "Метод не поддерживается", 405)
				return
			}
		}
		action, data := "read", []byte("{}")
		token := ""
		if c, err := r.Cookie(wizardCookie); err == nil && len(c.Value) == 64 {
			if _, err := hex.DecodeString(c.Value); err == nil {
				token = c.Value
			}
		}
		switch r.Method {
		case http.MethodGet:
		case http.MethodPost:
			if origin := r.Header.Values("Origin"); len(origin) != 1 || !strings.EqualFold(origin[0], "https://"+r.Host) || token == "" {
				http.Error(w, "Запрос отклонён. Обновите страницу.", 403)
				return
			}
			if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
				http.Error(w, "Неверный формат", 415)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 8192)
			if err := r.ParseForm(); err != nil {
				var large *http.MaxBytesError
				if errors.As(err, &large) {
					http.Error(w, "Слишком большой запрос", 413)
				} else {
					http.Error(w, "Неверная форма", 400)
				}
				return
			}
			form := r.PostForm
			if subtle.ConstantTimeCompare([]byte(token), []byte(form.Get("csrf"))) != 1 {
				http.Error(w, "Запрос отклонён. Обновите страницу.", 403)
				return
			}
			action = form.Get("action")
			switch action {
			case "apply":
				values := map[string]any{}
				for _, key := range []string{"callsign", "uart", "master", "password"} {
					v := form.Get(key)
					if (v == "" && key != "password") || len(v) > 1024 || strings.ContainsAny(v, "\r\n\x00") {
						http.Error(w, "Проверьте поля формы", 400)
						return
					}
					values[key] = v
				}
				for _, key := range []string{"radio_id", "frequency", "claim", "port"} {
					n, err := strconv.ParseUint(form.Get(key), 10, 32)
					if err != nil || n == 0 || (key == "port" && n > 65535) {
						http.Error(w, "Проверьте числовые поля", 400)
						return
					}
					values[key] = n
				}
				data, _ = json.Marshal(values)
			case "rollback":
			default:
				http.Error(w, "Неизвестное действие", 400)
				return
			}
		default:
			h.Set("Allow", "GET, POST")
			http.Error(w, "Метод не поддерживается", 405)
			return
		}
		select {
		case slot <- struct{}{}:
			defer func() { <-slot }()
		default:
			http.Error(w, "Настройки заняты. Повторите позже.", 503)
			return
		}
		parent, limit := r.Context(), 20*time.Second
		if action != "read" {
			// A closed browser tab must not interrupt an in-progress config transaction.
			parent, limit = context.WithoutCancel(parent), 110*time.Second
		}
		ctx, cancel := context.WithTimeout(parent, limit)
		defer cancel()
		out, err := helper(ctx, action, data)
		if err != nil {
			http.Error(w, "Не удалось выполнить операцию. Проверьте настройки и журнал службы.", 502)
			return
		}
		if action != "read" {
			http.Redirect(w, r, "/?done="+action, http.StatusSeeOther)
			return
		}
		var cfg wizardConfig
		if err := json.Unmarshal(out, &cfg); err != nil {
			http.Error(w, "Не удалось прочитать настройки", 502)
			return
		}
		if token == "" {
			var nonce [32]byte
			if _, err := rand.Read(nonce[:]); err != nil {
				http.Error(w, "Управление недоступно", 503)
				return
			}
			token = hex.EncodeToString(nonce[:])
			http.SetCookie(w, &http.Cookie{Name: wizardCookie, Value: token, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
		}
		if path == wizardConfigPath {
			h.Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(struct {
				Config wizardConfig `json:"config"`
				CSRF   string       `json:"csrf"`
			}{cfg, token})
			return
		}
		h.Set("Content-Type", "text/html; charset=utf-8")
		_ = wizardPage.Execute(w, struct {
			wizardConfig
			Token, Done string
		}{cfg, token, r.URL.Query().Get("done")})
	}
}

var wizardBundle = regexp.MustCompile(`^/manage/assets/([A-Za-z0-9_-][A-Za-z0-9._-]*\.(js|css|png))$`)

func serveWizardAsset(w http.ResponseWriter, r *http.Request, dir string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Метод не поддерживается", 405)
		return
	}
	name, contentType := "index.html", "text/html; charset=utf-8"
	bundle := false
	switch path := r.URL.EscapedPath(); path {
	case "/manage":
		http.Redirect(w, r, "/manage/", http.StatusMovedPermanently)
		return
	case "/manage/", "/manage/index.html":
	default:
		match := wizardBundle.FindStringSubmatch(path)
		if match == nil {
			http.NotFound(w, r)
			return
		}
		bundle, name, contentType = true, match[1], "text/javascript; charset=utf-8"
		switch match[2] {
		case "css":
			contentType = "text/css; charset=utf-8"
		case "png":
			contentType = "image/png"
		}
	}

	// Pin each directory and reject symlinks at every step, even during an update.
	const flags = syscall.O_RDONLY | syscall.O_NOFOLLOW | syscall.O_CLOEXEC
	root, err := syscall.Open(dir, flags|syscall.O_DIRECTORY, 0)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer syscall.Close(root)
	parent := root
	if bundle {
		parent, err = syscall.Openat(root, "assets", flags|syscall.O_DIRECTORY, 0)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer syscall.Close(parent)
	}
	fd, err := syscall.Openat(parent, name, flags|syscall.O_NONBLOCK, 0)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	frames := ""
	if port := shellPort(); port > 0 {
		// Expert > SSH Access embeds the shellinabox client on this host.
		frames = fmt.Sprintf(" frame-src http://*:%d https://*:%d;", port, port)
	}
	// The classic markup colours cells with style attributes; scripts stay same-origin only.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self'; connect-src 'self';"+frames+" form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	w.Header().Set("Content-Type", contentType)
	http.ServeContent(w, r, name, info.ModTime(), f)
}

var wizardPage = template.Must(template.New("wizard").Parse(`<!doctype html>
<html lang="ru"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Настройки Pi-Star</title><main><h1>Настройки Pi-Star</h1>
{{if eq .Done "apply"}}<p role="status">Настройки применены.</p>{{else if eq .Done "rollback"}}<p role="status">Предыдущие настройки восстановлены.</p>{{end}}
<form method="post" action="/"><input type="hidden" name="csrf" value="{{.Token}}">
<fieldset><legend>Радиостанция</legend>
<p><label>Позывной <input required name="callsign" value="{{.Callsign}}"></label>
<p><label>DMR ID <input required type="number" min="1" name="radio_id" value="{{.RadioID}}"></label>
<p><label>Частота, Гц <input required type="number" min="1" name="frequency" value="{{.Frequency}}"></label>
<p><label>Устройство UART <input required name="uart" value="{{.UART}}"></label></fieldset>
<fieldset><legend>Сеть DMR</legend>
<p><label>ID подключения (claim) <input required type="number" min="1" name="claim" value="{{.Claim}}"></label>
<p><label>Сервер <input required name="master" value="{{.Master}}"></label>
<p><label>Порт <input required type="number" min="1" max="65535" name="port" value="{{.Port}}"></label>
<p><label>Новый пароль сети <input type="password" name="password" autocomplete="new-password"></label>
<small>Оставьте пустым, чтобы сохранить текущий.</small></fieldset>
<p><button name="action" value="apply">Применить настройки</button></form>
<form method="post" action="/"><input type="hidden" name="csrf" value="{{.Token}}">
<button name="action" value="rollback">Восстановить предыдущие настройки</button></form></main></html>`))
