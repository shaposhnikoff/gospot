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
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type bmPeer struct {
	ID      uint32 `json:"id"`
	Simplex bool   `json:"simplex"`
}
type bmManager struct {
	helper    wizardHelper
	client    *http.Client
	base, dir string
	cacheMu   sync.Mutex
	cache     map[string]bmCached
	write     chan struct{}
}
type bmCached struct {
	body    []byte
	expires time.Time
}

func newBMManager(helper wizardHelper) *bmManager {
	return &bmManager{helper: helper, client: &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		base: "https://api.brandmeister.network/v2/", dir: "/var/lib/pistar-dashboard-bm", cache: make(map[string]bmCached), write: make(chan struct{}, 1)}
}
func (b *bmManager) request(ctx context.Context, method, path, key string, body []byte) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, b.base+path, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, resp.StatusCode, errors.New("upstream status")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if len(data) > 1<<20 {
		return nil, 0, errors.New("response too large")
	}
	return data, resp.StatusCode, err
}
func bmError(w http.ResponseWriter, status int) {
	msg := "BrandMeister API недоступен."
	if status > 0 {
		msg = fmt.Sprintf("BrandMeister API: HTTP %d.", status)
	}
	http.Error(w, msg, 502)
}

var bmKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]+$`)

func validBMKey(key string) bool {
	return len(key) >= 20 && len(key) <= 4096 && bmKeyPattern.MatchString(key)
}
func (b *bmManager) serve(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.EscapedPath(), "/manage/api/bm/")
	mut := path == "key" || path == "subscription"
	if (mut && r.Method != "POST") || (!mut && r.Method != "GET") {
		http.Error(w, "Метод не поддерживается", 405)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if path == "master" || path == "talkgroup" {
		b.cacheMu.Lock()
		c := b.cache[path]
		b.cacheMu.Unlock()
		if time.Now().Before(c.expires) {
			w.Write(c.body)
			return
		}
		data, status, err := b.request(r.Context(), "GET", path, "", nil)
		if err != nil || !json.Valid(data) {
			bmError(w, status)
			return
		}
		b.cacheMu.Lock()
		b.cache[path] = bmCached{data, time.Now().Add(30 * time.Minute)}
		b.cacheMu.Unlock()
		w.Write(data)
		return
	}
	if mut {
		if !archiveCSRF(r) {
			http.Error(w, "Перечитайте страницу", 403)
			return
		}
		if archiveHeader(r, "Content-Type") != "application/json" {
			http.Error(w, "Ожидается JSON", 415)
			return
		}
		select {
		case b.write <- struct{}{}:
			defer func() { <-b.write }()
		default:
			http.Error(w, "Операция уже выполняется", 409)
			return
		}
	}
	file := filepath.Join(b.dir, "api-key")
	if path == "key" {
		var in struct {
			Key string `json:"key"`
		}
		if !bmDecode(w, r, &in) || !validBMKey(in.Key) {
			http.Error(w, "Проверьте формат API-ключа", 400)
			return
		}
		f, err := os.CreateTemp(b.dir, ".key-")
		if err != nil {
			http.Error(w, "Не удалось сохранить ключ", 500)
			return
		}
		defer os.Remove(f.Name())
		_, err = f.WriteString(in.Key)
		cerr := f.Close()
		if err != nil || cerr != nil || os.Rename(f.Name(), file) != nil {
			http.Error(w, "Не удалось сохранить ключ", 500)
			return
		}
		w.WriteHeader(204)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	data, err := b.helper(ctx, "bm-peer", nil)
	var peer bmPeer
	if err != nil || json.Unmarshal(data, &peer) != nil || peer.ID == 0 {
		http.Error(w, "Не удалось прочитать ID пира из DMRGateway.ini", 502)
		return
	}
	key, err := os.ReadFile(file)
	if err != nil && !os.IsNotExist(err) {
		http.Error(w, "Не удалось прочитать ключ", 500)
		return
	}
	device := fmt.Sprintf("device/%d/", peer.ID)
	switch path {
	case "peer":
		csrf := editorToken(w, r)
		if csrf != "" {
			json.NewEncoder(w).Encode(map[string]any{"peer": peer, "hasKey": len(key) > 0, "csrf": csrf})
		}
	case "profile":
		data, status, err := b.request(ctx, "GET", device+"profile", "", nil)
		if err != nil {
			bmError(w, status)
			return
		}
		type sub struct {
			Slot  int `json:"slot"`
			Group int `json:"talkgroup"`
		}
		var profile struct {
			Static  []sub `json:"staticSubscriptions"`
			Dynamic []sub `json:"dynamicSubscriptions"`
		}
		if json.Unmarshal(data, &profile) != nil {
			bmError(w, 0)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"id": peer.ID, "static": profile.Static, "dynamic": profile.Dynamic})
	case "subscription":
		var in struct {
			Peer   uint32 `json:"peer"`
			Slot   *int   `json:"slot"`
			Group  int    `json:"group"`
			Action string `json:"action"`
		}
		if !bmDecode(w, r, &in) || in.Slot == nil || in.Group < 1 || in.Group > 16777215 || (in.Action != "add" && in.Action != "delete") {
			http.Error(w, "Некорректные параметры подписки", 400)
			return
		}
		if in.Peer != peer.ID {
			http.Error(w, "ID пира изменился. Перечитайте страницу", 409)
			return
		}
		slot := *in.Slot
		if (peer.Simplex && slot != 0) || (!peer.Simplex && slot != 1 && slot != 2) {
			http.Error(w, "Слот недоступен для этого пира", 409)
			return
		}
		if !validBMKey(string(key)) {
			http.Error(w, "Сначала сохраните API-ключ BrandMeister", 422)
			return
		}
		method, url := "POST", device+"talkgroup/"
		body, _ := json.Marshal(map[string]string{"slot": strconv.Itoa(slot), "group": strconv.Itoa(in.Group)})
		if in.Action == "delete" {
			method = "DELETE"
			url += fmt.Sprintf("%d/%d", slot, in.Group)
			body = nil
		}
		_, status, err := b.request(ctx, method, url, string(key), body)
		if err != nil {
			bmError(w, status)
			return
		}
		w.WriteHeader(204)
	default:
		http.NotFound(w, r)
	}
}
func bmDecode(w http.ResponseWriter, r *http.Request, out any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	dec.DisallowUnknownFields()
	return dec.Decode(out) == nil && dec.Decode(&struct{}{}) == io.EOF
}
