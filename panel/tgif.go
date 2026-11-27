package main

// Native port of the classic TGIF panels of the admin page:
// mmdvmhost/tgif_static_common.php, tgif_static_links.php,
// tgif_static_manager.php (Static TG API, token in TGIF_API_KEY_FILE) and
// tgif_manager.php (legacy sessions/update link manager, no token).
//
// The token is only ever sent to TGIF_API_URL as a Bearer header; it is
// never logged and never part of a reply.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	tgifKeyFile                string
	tgifAPIURL, tgifSessionURL string
)

var (
	tgifTokenPattern = regexp.MustCompile(`^TGIFSTG1\.[A-Fa-f0-9]{16}\.[A-Fa-f0-9]{16}\.[A-Za-z0-9_-]{40,64}$`)
	tgifIDPattern    = regexp.MustCompile(`^[0-9]{6,9}$`)
	tgifTGPattern    = regexp.MustCompile(`^[0-9]{1,8}$`)
	nonDigits        = regexp.MustCompile(`[^0-9]`)
)

// tgifClient talks to TGIF. api and session default to pistar.conf values;
// tests point them at a fake server.
type tgifClient struct {
	client       *http.Client
	api, session string
}

func newTGIFClient() *tgifClient {
	return &tgifClient{
		// 'timeout' => 5, 'follow_location' => 0 as in tgif_api_request().
		client: &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		api:    tgifAPIURL, session: tgifSessionURL,
	}
}

// tgifInfo tells the admin page which TGIF panel to draw. It needs no
// network call, so /manage/api/links can include it in the first paint.
type tgifInfo struct {
	ID     string `json:"id"`
	Static bool   `json:"static"` // Static TG table + manager; otherwise the legacy manager
}

type tgifLinks struct {
	ID           string   `json:"id"`
	Static       []string `json:"static"`
	Dynamic      []string `json:"dynamic"`
	StaticState  string   `json:"staticState"`  // ok | unavailable
	DynamicState string   `json:"dynamicState"` // ok | unavailable | denied
	Errors       []string `json:"errors"`
}

// phpTruthy is PHP's !empty() for a parse_ini_file value (on/yes/true are
// "1", off/no/false/none are "").
func phpTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "0", "off", "no", "false", "none":
		return false
	}
	return true
}

func tgifMasterAddress(host iniData) string {
	if a := host.get("DMR Network", "Address"); a != "" {
		return a
	}
	return host.get("DMR Network", "RemoteAddress")
}

// tgifDMRID is the ID both classic managers derive: the last enabled
// DMRGateway network 1-5 pointing at tgif.network, or the MMDVMHost DMR ID
// when MMDVMHost talks to tgif.network directly. Digits only.
func tgifDMRID(host, gw iniData) string {
	if !host.on("DMR", "Enable") {
		return ""
	}
	id := ""
	if (dashInputs{host: host}).dmrViaGateway() {
		for n := 1; n <= 5; n++ {
			s := fmt.Sprintf("DMR Network %d", n)
			if gw.get(s, "Address") == "tgif.network" && phpTruthy(gw.get(s, "Enabled")) && gw.has(s, "Id") {
				id = gw.get(s, "Id")
			}
		}
	} else if tgifMasterAddress(host) == "tgif.network" {
		id = host.get("DMR", "Id")
		if !phpTruthy(id) {
			id = host.get("General", "Id")
		}
	}
	return nonDigits.ReplaceAllString(id, "")
}

func readTGIFKey() string {
	data, err := os.ReadFile(tgifKeyFile)
	if err != nil {
		return ""
	}
	key := strings.TrimSpace(parseINI(bytes.NewReader(data)).get("key", "apikey"))
	if !tgifTokenPattern.MatchString(key) {
		return ""
	}
	return key
}

// tgifStaticContext is tgif_static_context(): a valid TGIF DMR ID and a
// stored Static TG token.
// Deviation: the reference also required Pi-Star >= 4.3.9 in
// /etc/pistar-release, which guarded older Pi-Star builds without the Static
// TG manager; the native dashboard always has it (its release reads
// "4.3-native"), so the version is not checked.
func tgifStaticContext(host, gw iniData) (id, token string, ok bool) {
	if id = tgifDMRID(host, gw); !tgifIDPattern.MatchString(id) {
		return "", "", false
	}
	if token = readTGIFKey(); token == "" {
		return "", "", false
	}
	return id, token, true
}

// tgifPanel decides which TGIF panel the admin page shows (index.php).
func tgifPanel(host, gw iniData) *tgifInfo {
	if id, _, ok := tgifStaticContext(host, gw); ok {
		return &tgifInfo{ID: id, Static: true}
	}
	if id := tgifDMRID(host, gw); id != "" {
		return &tgifInfo{ID: id}
	}
	return nil
}

type tgifReply struct {
	status int
	json   map[string]json.RawMessage
}

func (t tgifReply) ok() bool { return t.status == 200 && !phpEmpty(t.json["ok"]) }

func (t *tgifClient) request(ctx context.Context, token, id, resource, method string, body any) tgifReply {
	var payload io.Reader
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
		payload = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, t.api+resource+"/"+url.PathEscape(id), payload)
	if err != nil {
		return tgifReply{}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "Pi-Star TGIF TG Manager/"+id)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return tgifReply{}
	}
	defer resp.Body.Close()
	reply := tgifReply{status: resp.StatusCode}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if json.Unmarshal(raw, &reply.json) != nil {
		reply.json = nil
	}
	return reply
}

// phpEmpty is PHP's empty() for a decoded JSON value.
func phpEmpty(raw json.RawMessage) bool {
	var v any
	if len(raw) == 0 || json.Unmarshal(raw, &v) != nil {
		return true
	}
	switch x := v.(type) {
	case nil:
		return true
	case bool:
		return !x
	case float64:
		return x == 0
	case string:
		return x == "" || x == "0"
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	}
	return false
}

// phpInt is PHP's (int) cast of a JSON number or numeric string ("91", 91, 91.0).
func phpInt(raw json.RawMessage) int {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return 0
	}
	switch x := v.(type) {
	case bool:
		if x {
			return 1
		}
	case float64:
		if !math.IsNaN(x) && math.Abs(x) < 1<<31 {
			return int(x)
		}
	case string:
		m := regexp.MustCompile(`^\s*[+-]?[0-9]+`).FindString(x)
		n, _ := strconv.Atoi(strings.TrimSpace(m))
		return n
	}
	return 0
}

func phpString(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		if x {
			return "1"
		}
	}
	return ""
}

var tgifFriendly = map[string]string{
	"invalid_token":                  "The Static TG API token is invalid or has been revoked.",
	"token_device_mismatch":          "This token was created for a different hotspot ID / ESSID.",
	"device_not_connected":           "The hotspot is not currently connected to TGIF.",
	"device_not_owned":               "TGIF does not see this hotspot as belonging to the token account.",
	"secure_hotspot_required":        "TGIF Hotspot Security is required for Static Talkgroups.",
	"static_tg_not_available":        "Static Talkgroups are not available for this account.",
	"static_tg_disabled":             "Static Talkgroups are currently disabled on TGIF.",
	"dynamic_tg_permission_required": "This TGIF API token cannot manage Dynamic Talkgroups. Re-create the token on tgif.network, then save the new one in Expert > API Keys.",
	"unsupported_slot":               "That timeslot is not supported by this hotspot session.",
	"invalid_or_reserved_talkgroup":  "That talkgroup is invalid or reserved for another TGIF function.",
	"limit_exceeded":                 "The Static Talkgroup limit for this account has been reached.",
	"membership_conflict":            "The Static Talkgroup state changed at the same time. Refresh and try again.",
}

// tgifErrorText is tgif_static_error_text().
func tgifErrorText(r tgifReply) string {
	code := phpString(r.json["error"])
	if msg, ok := tgifFriendly[code]; ok {
		return msg
	}
	switch {
	case r.status == 429:
		return "TGIF rate limited the request. Wait before trying again."
	case r.status >= 500:
		return "TGIF could not complete the request. Try again later."
	case r.status == 0:
		return "No response from the TGIF API."
	}
	if code != "" {
		return fmt.Sprintf("TGIF API returned HTTP %d (%s).", r.status, code)
	}
	return fmt.Sprintf("TGIF API returned HTTP %d.", r.status)
}

// links is tgif_static_links.php: both lists, fetched in parallel.
func (t *tgifClient) links(ctx context.Context, id, token string) tgifLinks {
	var static, dynamic tgifReply
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); static = t.request(ctx, token, id, "static-talkgroups", "GET", nil) }()
	go func() { defer wg.Done(); dynamic = t.request(ctx, token, id, "dynamic-talkgroups", "GET", nil) }()
	wg.Wait()
	out := tgifLinks{ID: id, Static: []string{}, Dynamic: []string{}, Errors: []string{}, StaticState: "unavailable", DynamicState: "unavailable"}
	if static.ok() {
		out.StaticState = "ok"
		var members []map[string]json.RawMessage
		_ = json.Unmarshal(static.json["memberships"], &members)
		for _, m := range members {
			slot, tg := phpInt(m["slot"]), phpInt(m["talkgroup"])
			if tg > 0 && (slot == 1 || slot == 2) {
				out.Static = append(out.Static, fmt.Sprintf("TG%d(%d)", tg, slot))
			}
		}
	} else {
		out.Errors = append(out.Errors, "Static: "+tgifErrorText(static))
	}
	if dynamic.ok() {
		out.DynamicState = "ok"
		for slot, field := range []string{"ts1_talkgroup", "ts2_talkgroup"} {
			if tg := phpInt(dynamic.json[field]); tg > 0 {
				out.Dynamic = append(out.Dynamic, fmt.Sprintf("TG%d(%d)", tg, slot+1))
			}
		}
	} else {
		if phpString(dynamic.json["error"]) == "dynamic_tg_permission_required" {
			out.DynamicState = "denied"
		}
		out.Errors = append(out.Errors, "Dynamic: "+tgifErrorText(dynamic))
	}
	return out
}

var httpStatusTexts = map[int]string{
	100: "Continue", 101: "Switching Protocols", 200: "OK", 201: "Created", 202: "Accepted",
	203: "Non-Authoritative Information", 204: "No Content", 205: "Reset Content", 206: "Partial Content",
	300: "Multiple Choices", 302: "Found", 303: "See Other", 304: "Not Modified", 305: "Use Proxy",
	400: "Bad Request", 401: "Unauthorized", 402: "Payment Required", 403: "Forbidden", 404: "Not Found",
	405: "Method Not Allowed", 406: "Not Acceptable", 407: "Proxy Authentication Required", 408: "Request Timeout",
	409: "Conflict", 410: "Gone", 411: "Length Required", 412: "Precondition Failed", 413: "Request Entity Too Large",
	414: "Request-URI Too Long", 415: "Unsupported Media Type", 416: "Requested Range Not Satisfiable",
	417: "Expectation Failed", 500: "Internal Server Error", 501: "Not Implemented", 502: "Bad Gateway",
	503: "Service Unavailable", 504: "Gateway Timeout", 505: "HTTP Version Not Supported",
}

// httpStatusText is tgif_manager.php's httpStatusText(): the digits of the
// reply, named when they are a known HTTP status.
func httpStatusText(s string) string {
	code := nonDigits.ReplaceAllString(s, "")
	if n, err := strconv.Atoi(code); err == nil && len(code) == 3 {
		if text, ok := httpStatusTexts[n]; ok {
			return text
		}
	}
	return code
}

// legacy is tgif_manager.php: GET <session>/<id>/<slot>/<tg>.
func (t *tgifClient) legacy(ctx context.Context, id string, slot, tg int) string {
	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s%s/%d/%d", t.session, url.PathEscape(id), slot, tg), nil)
	if err != nil {
		return "TGIF API: No Response"
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return "TGIF API: No Response"
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "TGIF API: " + httpStatusText(strconv.Itoa(resp.StatusCode))
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	return "TGIF API: " + httpStatusText(string(body))
}

func serveTGIF(w http.ResponseWriter, r *http.Request, t *tgifClient) {
	host, gw := mmdvmINI.load(), dmrGWINI.load()
	switch r.Method {
	case http.MethodGet:
		id, token, ok := tgifStaticContext(host, gw)
		if !ok {
			http.Error(w, "TGIF Static TG Manager is not available", 409)
			return
		}
		writeJSON(w, 200, t.links(r.Context(), id, token))
	case http.MethodPost:
		var in struct {
			Kind   string `json:"kind"`   // static | dynamic | legacy
			Action string `json:"action"` // ADD | DEL, or LINK | UNLINK for legacy
			TG     string `json:"tg"`
			Slot   string `json:"slot"`
		}
		if !maintenancePost(w, r, &in) {
			return
		}
		id, token, static := tgifStaticContext(host, gw)
		switch in.Kind {
		case "static", "dynamic":
			if !static {
				http.Error(w, "TGIF Static TG Manager is not available", 409)
				return
			}
			slot, _ := strconv.Atoi(in.Slot)
			digits := nonDigits.ReplaceAllString(in.TG, "")
			tg, _ := strconv.Atoi(digits)
			if (slot != 1 && slot != 2) || !tgifTGPattern.MatchString(digits) || tg < 1 {
				http.Error(w, "Enter a valid talkgroup and timeslot.", 422)
				return
			}
			if in.Action != "ADD" && in.Action != "DEL" {
				http.Error(w, "Choose Add or Remove.", 422)
				return
			}
			add := in.Action == "ADD"
			resource, method, done := "static-talkgroups", "POST", "Static Talkgroup added."
			if in.Kind == "static" && !add {
				method, done = "DELETE", "Static Talkgroup removed."
			}
			if in.Kind == "dynamic" {
				resource, done = "dynamic-talkgroups", "Dynamic Talkgroup set."
				if !add {
					tg, done = 4000, "Dynamic Talkgroup unlinked."
				}
			}
			reply := t.request(r.Context(), token, id, resource, method, map[string]int{"slot": slot, "talkgroup": tg})
			if !reply.ok() {
				done = tgifErrorText(reply)
			}
			writeJSON(w, 200, map[string]string{"output": done})
		case "legacy":
			id = tgifDMRID(host, gw)
			if static || id == "" {
				http.Error(w, "TGIF Manager is not available", 409)
				return
			}
			slot := 1 // DMR Network Slot1=0 (simplex): always TS2
			if host.get("DMR Network", "Slot1") != "0" {
				switch in.Slot {
				case "1":
					slot = 0
				case "2":
					slot = 1
				default:
					http.Error(w, "Enter a valid talkgroup and timeslot.", 422)
					return
				}
			}
			digits := nonDigits.ReplaceAllString(in.TG, "")
			if len(digits) > 7 { // maxlength="7" of the classic field
				http.Error(w, "Enter a valid talkgroup and timeslot.", 422)
				return
			}
			if in.Action != "LINK" && in.Action != "UNLINK" {
				http.Error(w, "Choose Link or UnLink.", 422)
				return
			}
			tg, _ := strconv.Atoi(digits)
			if tg < 1 || in.Action == "UNLINK" {
				tg = 4000
			}
			writeJSON(w, 200, map[string]string{"output": t.legacy(r.Context(), id, slot, tg)})
		default:
			http.Error(w, "Unknown action", 400)
		}
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Метод не поддерживается", 405)
	}
}
