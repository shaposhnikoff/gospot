package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"regexp"
	"strings"
)

// CSS Tool (admin/expert/edit_dashboard.php) and the colours it feeds into
// css/pistar-css.php, index.php (BannerH1/BannerExtText) and the callsign
// lookup links (lh.php, localtx.php).

var gospotCSSFile string

// The classic file the editor shows when /etc/gospot-css.ini is missing.
const defaultGoSpotCSS = "[Background]\nPage=edf0f5\nContent=ffffff\nBanners=dd4b39\n\n" +
	"[Text]\nBanners=ffffff\nBannersDrop=303030\n\n" +
	"[Tables]\nHeadDrop=8b0000\nBgEven=f7f7f7\nBgOdd=d0d0d0\n\n" +
	"[Content]\nText=000000\n\n" +
	"[BannerH1]\nEnabled=0\nText=\"Some Text\"\n\n" +
	"[BannerExtText]\nEnabled=0\nText=\"Some long text entry\"\n\n" +
	"[Lookup]\nService=\"RadioID\"\n"

type themeData struct {
	Page, Content, Banners, BannerText, BannerDrop, HeadDrop, Text, Even, Odd string
	BannerH1, BannerExt                                                       string
	Lookup                                                                    string
}

var gospotCSS = newINIFile()

var cssColour = regexp.MustCompile(`^(?:[0-9A-Fa-f]{3}|[0-9A-Fa-f]{6})$`)

// currentTheme follows pistar-css.php: the file's colours, or the classic
// palette when the file is missing; a value that is not a hex colour keeps
// the default (the classic page would emit broken CSS).
func currentTheme() themeData {
	if gospotCSS.location() != gospotCSSFile {
		gospotCSS.setPath(gospotCSSFile)
	}
	d := gospotCSS.load()
	t := themeData{Page: "edf0f5", Content: "ffffff", Banners: "dd4b39", BannerText: "ffffff", BannerDrop: "303030",
		HeadDrop: "8b0000", Text: "000000", Even: "f7f7f7", Odd: "d0d0d0", Lookup: "RadioID"}
	for _, f := range []struct {
		dst          *string
		section, key string
	}{
		{&t.Page, "Background", "Page"}, {&t.Content, "Background", "Content"}, {&t.Banners, "Background", "Banners"},
		{&t.BannerText, "Text", "Banners"}, {&t.BannerDrop, "Text", "BannersDrop"}, {&t.HeadDrop, "Tables", "HeadDrop"},
		{&t.Text, "Content", "Text"}, {&t.Even, "Tables", "BgEven"}, {&t.Odd, "Tables", "BgOdd"},
	} {
		if v := d.get(f.section, f.key); cssColour.MatchString(v) {
			*f.dst = strings.ToLower(v)
		}
	}
	truthy := func(v string) bool {
		switch strings.ToLower(v) {
		case "1", "true", "on", "yes":
			return true
		}
		return false
	}
	if truthy(d.get("BannerH1", "Enabled")) {
		t.BannerH1 = d.get("BannerH1", "Text")
	}
	if truthy(d.get("BannerExtText", "Enabled")) {
		t.BannerExt = d.get("BannerExtText", "Text")
	}
	if d.get("Lookup", "Service") == "QRZ" {
		t.Lookup = "QRZ"
	}
	return t
}

type cssSection struct {
	Name string      `json:"name"`
	Keys [][2]string `json:"keys"`
}

// cssLayout lists sections and keys in file order with unquoted values; the
// classic editor hides Lookup popupWidth/popupHeight (and so drops them).
func cssLayout(text string) []cssSection {
	var out []cssSection
	s := bufio.NewScanner(strings.NewReader(text))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		switch {
		case line == "" || line[0] == '#' || line[0] == ';':
		case line[0] == '[' && strings.HasSuffix(line, "]"):
			out = append(out, cssSection{Name: line[1 : len(line)-1]})
		case len(out) > 0:
			k, v, ok := strings.Cut(line, "=")
			k = strings.TrimSpace(k)
			if !ok || (out[len(out)-1].Name == "Lookup" && (k == "popupWidth" || k == "popupHeight")) {
				continue
			}
			out[len(out)-1].Keys = append(out[len(out)-1].Keys, [2]string{k, unquote(strings.TrimSpace(v))})
		}
	}
	return out
}

func cssRevision(b []byte, exists bool) string {
	if !exists {
		b = []byte("\x00missing")
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// serveCSSTool: GET shows the file (or the classic defaults), POST saves the
// edited values or resets the theme through gospot-configure.
func serveCSSTool(w http.ResponseWriter, r *http.Request, run configHelperFunc) {
	switch r.Method {
	case http.MethodGet:
		csrf := editorToken(w, r)
		if csrf == "" {
			return
		}
		b, err := os.ReadFile(gospotCSSFile)
		exists := err == nil
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			http.Error(w, "Could not read the CSS settings", 502)
			return
		}
		text := string(b)
		if !exists {
			text = defaultGoSpotCSS
		}
		writeJSON(w, 200, map[string]any{"csrf": csrf, "revision": cssRevision(b, exists), "exists": exists, "sections": cssLayout(text)})
	case http.MethodPost:
		var in struct {
			Revision string                       `json:"revision"`
			Action   string                       `json:"action"`
			Values   map[string]map[string]string `json:"values"`
			Confirm  string                       `json:"confirm"`
		}
		if !maintenancePost(w, r, &in) {
			return
		}
		verb := "css-write"
		switch in.Action {
		case "save":
		case "reset":
			if in.Confirm != "RESET" {
				http.Error(w, "Server-side confirmation did not match. Factory reset cancelled.", 422)
				return
			}
			verb = "css-reset"
		default:
			http.Error(w, "Unknown action", 400)
			return
		}
		body, _ := json.Marshal(map[string]any{"revision": in.Revision, "values": in.Values})
		out, err := run(r.Context(), verb, body)
		switch {
		case errors.Is(err, errConfigStale):
			http.Error(w, "The CSS settings changed since this page was loaded; reload it", 409)
		case err != nil:
			var e struct{ Error string }
			_ = json.Unmarshal(bytes.TrimSpace(out), &e)
			if e.Error == "" {
				e.Error = "Save failed"
			}
			http.Error(w, e.Error, 422)
		default:
			writeJSON(w, 200, map[string]bool{"ok": true})
		}
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Метод не поддерживается", 405)
	}
}
