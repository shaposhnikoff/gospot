package main

import (
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
)

// Shared request checks of the HTTPS management API: hex tokens, single
// headers and the double-submit CSRF check (cookie + X-PiStar-CSRF + Origin).

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
