package main

// BrandMeister Hoseline listen-only relay. Browsers cannot open the spotter
// WebSocket themselves (it checks Origin), so the board does: it reads the
// public token from HOSELINE_PAGE_URL, opens HOSELINE_SPOTTER_URL with the
// "spotter" subprotocol, subscribes to one talkgroup and forwards μ-law audio
// as binary frames and call start/end as small JSON text frames.

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Set from pistar.conf by applySettings.
var (
	hoselinePageURL, hoselineSpotterURL string
	hoselineMaxListeners                int
	hoselineTalkgroups                  []int
)

const (
	hoselineMinTG     = 91
	hoselineMaxTG     = 99999999
	hoselineTokenTTL  = 15 * time.Minute
	hoselineGroupsTTL = 5 * time.Minute
	// μ-law at 8 kHz: one second of audio is far above a spotter frame.
	hoselineMaxAudio = 8000
)

var hoselineTGPattern = regexp.MustCompile(`^[1-9][0-9]{1,7}$`)

type hoseline struct {
	page, spotter, origin string
	tokenRef              *regexp.Regexp
	max                   int
	configured            []int
	client                *http.Client
	tlsConfig             *tls.Config   // spotter TLS (nil: system roots)
	idle                  time.Duration // a silent browser or spotter is dropped after this
	ping                  time.Duration
	bm                    *bmManager

	mu        sync.Mutex
	listeners int

	tokenMu    sync.Mutex
	token      string
	tokenUntil time.Time

	groupsMu    sync.Mutex
	bmID        string
	bmGroups    []int
	groupsUntil time.Time
}

func newHoseline() *hoseline {
	h := &hoseline{
		page: hoselinePageURL, spotter: hoselineSpotterURL, max: hoselineMaxListeners,
		configured: slices.Clone(hoselineTalkgroups),
		client:     &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		idle:       75 * time.Second, ping: 20 * time.Second,
		bm: newBMManager(nil),
	}
	if u, err := url.Parse(h.page); err == nil {
		h.origin = u.Scheme + "://" + u.Host
	}
	// The page creates its client as new X("<spotter URL>",token).
	h.tokenRef = regexp.MustCompile(`new\s+\w+\("` + regexp.QuoteMeta(h.spotter) + `",(\w+)\)`)
	return h
}

// The live page publishes credentials such as "test:test" (checked 2026-11-27).
var hoselineTokenValue = regexp.MustCompile(`^[A-Za-z0-9._~+/=:@-]{1,1000}$`)

// spotterToken reads the token published by the Hoseline page (cached).
func (h *hoseline) spotterToken(ctx context.Context) (string, error) {
	h.tokenMu.Lock()
	defer h.tokenMu.Unlock()
	if h.token != "" && time.Now().Before(h.tokenUntil) {
		return h.token, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.page, nil)
	if err != nil {
		return "", err
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return "", errors.New("Hoseline page unavailable")
	}
	defer resp.Body.Close()
	page, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK || err != nil {
		return "", errors.New("Hoseline page unavailable")
	}
	ref := h.tokenRef.FindSubmatch(page)
	if ref == nil {
		return "", errors.New("Hoseline token reference not found")
	}
	value := regexp.MustCompile(`\b` + regexp.QuoteMeta(string(ref[1])) + `="([^"]+)"`).FindSubmatch(page)
	if value == nil || !hoselineTokenValue.Match(value[1]) {
		return "", errors.New("Hoseline token value not found")
	}
	h.token, h.tokenUntil = string(value[1]), time.Now().Add(hoselineTokenTTL)
	return h.token, nil
}

func (h *hoseline) forgetToken() {
	h.tokenMu.Lock()
	h.token = ""
	h.tokenMu.Unlock()
}

// hotspotBMGroups lists the BrandMeister profile talkgroups of this hotspot
// (static and dynamic, as the admin BM table shows them). The profile is
// public: no API key is read or sent.
func (h *hoseline) hotspotBMGroups(ctx context.Context) []int {
	host := mmdvmINI.load()
	if !host.on("DMR", "Enable") {
		return nil
	}
	name, id := dmrMaster(host, dmrGWINI.load())
	if !strings.HasPrefix(name, "BM") || !regexp.MustCompile(`^[0-9]{1,10}$`).MatchString(id) {
		return nil
	}
	h.groupsMu.Lock()
	defer h.groupsMu.Unlock()
	if h.bmID == id && time.Now().Before(h.groupsUntil) {
		return slices.Clone(h.bmGroups)
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	static, dynamic, err := bmProfile(ctx, h.bm, host, id)
	h.bmID, h.bmGroups, h.groupsUntil = id, nil, time.Now().Add(hoselineGroupsTTL)
	if err != nil {
		h.groupsUntil = time.Now().Add(time.Minute)
		return nil
	}
	tg := regexp.MustCompile(`^TG([0-9]+)`)
	for _, s := range append(static, dynamic...) {
		if m := tg.FindStringSubmatch(s); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil {
				h.bmGroups = append(h.bmGroups, n)
			}
		}
	}
	return slices.Clone(h.bmGroups)
}

// offered is the selectable list: hotspot BM talkgroups plus
// HOSELINE_TALKGROUPS, deduplicated, in numeric order.
func (h *hoseline) offered(ctx context.Context) []int {
	var out []int
	for _, n := range append(h.hotspotBMGroups(ctx), h.configured...) {
		if n >= hoselineMinTG && n <= hoselineMaxTG {
			out = append(out, n)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func (h *hoseline) acquire() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.listeners >= h.max {
		return false
	}
	h.listeners++
	return true
}

func (h *hoseline) release() {
	h.mu.Lock()
	h.listeners--
	h.mu.Unlock()
}

func (h *hoseline) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.listeners
}

// sameOrigin accepts exactly one Origin equal to this listener's own origin.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Values("Origin")
	scheme := "http://"
	if r.TLS != nil {
		scheme = "https://"
	}
	return len(origin) == 1 && r.Host != "" && strings.EqualFold(origin[0], scheme+r.Host)
}

// serve handles <prefix>/api/hoseline (the WebSocket) and
// <prefix>/api/hoseline/groups.
func (h *hoseline) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/groups") {
		writeJSON(w, 200, map[string]any{"groups": h.offered(r.Context()), "listeners": h.count(), "max": h.max})
		return
	}
	if !sameOrigin(r) {
		http.Error(w, "Open the player from this dashboard", http.StatusForbidden)
		return
	}
	q := r.URL.Query()
	tg, err := strconv.Atoi(q.Get("tg"))
	if len(q) != 1 || len(q["tg"]) != 1 || !hoselineTGPattern.MatchString(q.Get("tg")) || err != nil ||
		tg < hoselineMinTG || tg > hoselineMaxTG || !slices.Contains(h.offered(r.Context()), tg) {
		http.Error(w, "Invalid talkgroup", http.StatusBadRequest)
		return
	}
	if !h.acquire() {
		w.Header().Set("Retry-After", "30")
		http.Error(w, "Too many Hoseline listeners", http.StatusServiceUnavailable)
		return
	}
	defer h.release()
	browser, err := wsUpgrade(w, r, "", 1024)
	if err != nil {
		return
	}
	h.relay(browser, tg)
}

type hoselineEvent struct {
	Type    string `json:"type"`
	TG      int    `json:"tg,omitempty"`
	Source  int64  `json:"source,omitempty"`
	Message string `json:"message,omitempty"`
}

// relay runs one browser session until either side goes away.
func (h *hoseline) relay(browser *wsConn, tg int) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	browser.idle = h.idle
	go func() {
		defer cancel()
		for {
			if _, _, err := browser.readMessage(); err != nil {
				return
			}
		}
	}()
	go browser.keepalive(ctx, h.ping, cancel)
	err := h.stream(ctx, browser, tg)
	if ctx.Err() != nil {
		browser.close(1000, "")
		return
	}
	message := "Hoseline disconnected"
	if err != nil {
		log.Printf("Hoseline TG %d: %v", tg, err)
		var refused *wsHandshakeError
		switch {
		case strings.HasPrefix(err.Error(), "Hoseline "):
			message = err.Error()
		case errors.As(err, &refused):
			message = "Hoseline refused the connection"
		}
	}
	_ = browser.writeJSON(hoselineEvent{Type: "error", Message: message})
	browser.close(1011, "")
}

// stream subscribes to tg on the spotter and forwards its messages.
func (h *hoseline) stream(ctx context.Context, browser *wsConn, tg int) error {
	tctx, tcancel := context.WithTimeout(ctx, 15*time.Second)
	token, err := h.spotterToken(tctx)
	tcancel()
	if err != nil {
		return err
	}
	dctx, dcancel := context.WithTimeout(ctx, 15*time.Second)
	up, err := wsDial(dctx, h.spotter+"?token="+url.QueryEscape(token), h.origin, "spotter", h.tlsConfig, 1<<16)
	dcancel()
	if err != nil {
		var refused *wsHandshakeError
		if errors.As(err, &refused) {
			h.forgetToken() // a stale token is the usual reason
		}
		return err
	}
	defer up.close(1000, "")
	stop := context.AfterFunc(ctx, func() { up.close(1000, "") })
	defer stop()
	up.idle = h.idle
	go up.keepalive(ctx, h.ping, func() { up.close(1000, "") })
	if err := up.writeFrame(wsBinary, hoselineSubscribe(tg)); err != nil {
		return err
	}
	if err := browser.writeJSON(hoselineEvent{Type: "listening", TG: tg}); err != nil {
		return err
	}
	for {
		op, data, err := up.readMessage()
		if err != nil {
			return err
		}
		if op != wsBinary {
			continue
		}
		v, err := msgpackDecode(data)
		msg, _ := v.([]any)
		if err != nil || len(msg) == 0 {
			continue
		}
		kind, _ := mpInt(msg[0])
		switch kind {
		case 11:
			e := hoselineEvent{Type: "start"}
			if len(msg) > 2 {
				if id, ok := mpInt(msg[2]); ok && id > 0 && id <= 0xffffffff {
					e.Source = id
				}
			}
			err = browser.writeJSON(e)
		case 13:
			err = browser.writeJSON(hoselineEvent{Type: "end"})
		case 20:
			if len(msg) > 1 {
				if audio, ok := msg[1].([]byte); ok && len(audio) > 0 && len(audio) <= hoselineMaxAudio {
					err = browser.writeFrame(wsBinary, audio)
				}
			}
		}
		if err != nil {
			return err
		}
	}
}
