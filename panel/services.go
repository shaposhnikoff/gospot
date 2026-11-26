package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os/exec"
	"time"
)

type dmrServices struct {
	DMR      int      `json:"dmr"`
	CC       int      `json:"cc"`
	Network  int      `json:"network"`
	TS1      int      `json:"ts1"`
	TS2      int      `json:"ts2"`
	Gateway  int      `json:"gateway"`
	Master   string   `json:"master"`
	Port     int      `json:"port"`
	Rules    []string `json:"rules"`
	Revision string   `json:"revision"`
}

func serveDMRServices(w http.ResponseWriter, r *http.Request, helper wizardHelper, slot chan struct{}) {
	action := "services-read"
	var data []byte
	parent, limit := r.Context(), 20*time.Second
	switch r.Method {
	case http.MethodGet:
	case http.MethodPost:
		if !archiveCSRF(r) {
			http.Error(w, "Обновите страницу", 403)
			return
		}
		if archiveHeader(r, "Content-Type") != "application/json" {
			http.Error(w, "Ожидается JSON", 415)
			return
		}
		var err error
		data, err = io.ReadAll(http.MaxBytesReader(w, r.Body, 8192))
		if err != nil {
			http.Error(w, "Не удалось прочитать запрос", 413)
			return
		}
		if !json.Valid(data) {
			http.Error(w, "Неверные параметры", 400)
			return
		}
		action = "services-apply"
		parent, limit = context.WithoutCancel(parent), 110*time.Second
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Метод не поддерживается", 405)
		return
	}
	ctx, cancel := context.WithTimeout(parent, limit)
	defer cancel()
	if r.Method == http.MethodGet {
		// A reopened editor can overlap the previous page's post-save read.
		// Share one timeout for waiting and reading; never repeat a write.
		select {
		case slot <- struct{}{}:
		case <-ctx.Done():
			http.Error(w, "Настройки заняты", 503)
			return
		}
	} else {
		select {
		case slot <- struct{}{}:
		default:
			http.Error(w, "Настройки заняты", 503)
			return
		}
	}
	defer func() { <-slot }()
	out, err := helper(ctx, action, data)
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 3 {
			http.Error(w, "Настройки изменились. Перечитайте их.", 409)
			return
		}
		if errors.As(err, &exit) && exit.ExitCode() == 4 {
			http.Error(w, "Проверьте значения полей", 422)
			return
		}
		http.Error(w, "Не удалось выполнить операцию. Проверьте журнал службы.", 502)
		return
	}
	if r.Method == http.MethodPost {
		w.WriteHeader(204)
		return
	}
	var cfg dmrServices
	if json.Unmarshal(out, &cfg) != nil || !archiveHex(cfg.Revision) || cfg.Rules == nil {
		http.Error(w, "Не удалось прочитать настройки", 502)
		return
	}
	c, err := r.Cookie(wizardCookie)
	if err != nil || !archiveHex(c.Value) {
		var nonce [32]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			http.Error(w, "Управление недоступно", 503)
			return
		}
		c = &http.Cookie{Name: wizardCookie, Value: hex.EncodeToString(nonce[:]), Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode}
		http.SetCookie(w, c)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(struct {
		Config dmrServices `json:"config"`
		CSRF   string      `json:"csrf"`
	}{cfg, c.Value})
}
