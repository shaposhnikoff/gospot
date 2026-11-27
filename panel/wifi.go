package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// Wi-Fi page (admin/wifi.php): interface status, the configured networks,
// scan and adapter reset through the root helper's wifi-* commands. The
// helper never returns a PSK; replies are re-encoded through the typed structs
// below so nothing else can pass to the browser. Dispatch only after
// wizardHandler's TLS and Basic Auth checks.

type wifiNetwork struct {
	ID      string `json:"id"`
	SSID    string `json:"ssid"`
	Secured bool   `json:"secured"`
}

type wifiStatus struct {
	Present   bool   `json:"present"`
	Up        bool   `json:"up"`
	MAC       string `json:"mac,omitempty"`
	IP        string `json:"ip,omitempty"`
	Netmask   string `json:"netmask,omitempty"`
	RxPackets *int64 `json:"rxPackets,omitempty"`
	RxBytes   *int64 `json:"rxBytes,omitempty"`
	TxPackets *int64 `json:"txPackets,omitempty"`
	TxBytes   *int64 `json:"txBytes,omitempty"`
	SSID      string `json:"ssid,omitempty"`
	BSSID     string `json:"bssid,omitempty"`
	Freq      string `json:"freq,omitempty"`
	Signal    string `json:"signal,omitempty"`
	Bitrate   string `json:"bitrate,omitempty"`
	TxPower   string `json:"txPower,omitempty"`
}

type wifiState struct {
	CSRF      string        `json:"csrf"`
	Revision  string        `json:"revision"`
	Backend   string        `json:"backend"`
	Interface string        `json:"interface"`
	Country   string        `json:"country"`
	Countries []string      `json:"countries"`
	Networks  []wifiNetwork `json:"networks"`
	Status    wifiStatus    `json:"status"`
}

type wifiScanRow struct {
	BSSID    string `json:"bssid"`
	Freq     string `json:"freq"`
	Signal   string `json:"signal"`
	Security string `json:"security"`
	SSID     string `json:"ssid"`
}

type wifiSaveNetwork struct {
	ID   string `json:"id,omitempty"`
	SSID string `json:"ssid"`
	PSK  string `json:"psk"`
}

type wifiReply struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
	Note    string `json:"note,omitempty"`
}

// The helper validates every value; these bounds only keep requests small.
const wifiMaxNetworks = 32

func serveWifi(w http.ResponseWriter, r *http.Request, run configHelperFunc) {
	switch r.Method {
	case http.MethodGet:
		csrf := editorToken(w, r)
		if csrf == "" {
			return
		}
		out, err := run(r.Context(), "wifi-read", nil)
		var state wifiState
		if err != nil {
			http.Error(w, wifiHelperError(out, "Could not read the Wi-Fi configuration"), 502)
			return
		}
		if json.Unmarshal(out, &state) != nil {
			http.Error(w, "Could not read the Wi-Fi configuration", 502)
			return
		}
		state.CSRF = csrf
		if state.Networks == nil {
			state.Networks = []wifiNetwork{}
		}
		writeJSON(w, 200, state)
	case http.MethodPost:
		var in struct {
			Action   string            `json:"action"`
			Revision string            `json:"revision"`
			Country  string            `json:"country"`
			Networks []wifiSaveNetwork `json:"networks"`
		}
		if !maintenancePost(w, r, &in) {
			return
		}
		switch in.Action {
		case "save":
			if len(in.Networks) > wifiMaxNetworks {
				http.Error(w, "network count out of range", 422)
				return
			}
			for _, n := range in.Networks {
				if len(n.ID) > 64 || len(n.SSID) > 256 || len(n.PSK) > 256 {
					http.Error(w, "invalid network", 422)
					return
				}
			}
			networks := in.Networks
			if networks == nil {
				networks = []wifiSaveNetwork{}
			}
			body, _ := json.Marshal(struct {
				Revision string            `json:"revision"`
				Country  string            `json:"country"`
				Networks []wifiSaveNetwork `json:"networks"`
			}{in.Revision, in.Country, networks})
			// A closed tab must not interrupt a write or the reconnect after it.
			out, err := run(context.WithoutCancel(r.Context()), "wifi-write", body)
			wifiDone(w, out, err, "Save failed")
		case "scan":
			out, err := run(r.Context(), "wifi-scan", nil)
			if err != nil {
				http.Error(w, wifiHelperError(out, "Scan failed"), 422)
				return
			}
			var scan struct {
				Networks []wifiScanRow `json:"networks"`
			}
			if json.Unmarshal(out, &scan) != nil {
				http.Error(w, "Scan failed", 502)
				return
			}
			if scan.Networks == nil {
				scan.Networks = []wifiScanRow{}
			}
			writeJSON(w, 200, scan)
		case "up", "down", "reset":
			// Resetting the adapter drops a Wi-Fi client's connection mid-request.
			out, err := run(context.WithoutCancel(r.Context()), "wifi-action "+in.Action, nil)
			wifiDone(w, out, err, "The command failed")
		default:
			http.Error(w, "Неизвестное действие", 400)
		}
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Метод не поддерживается", 405)
	}
}

func wifiDone(w http.ResponseWriter, out []byte, err error, fallback string) {
	switch {
	case errors.Is(err, errConfigStale):
		http.Error(w, "The Wi-Fi configuration changed since this page was loaded; reload it", 409)
	case err != nil:
		http.Error(w, wifiHelperError(out, fallback), 422)
	default:
		var reply wifiReply
		if json.Unmarshal(out, &reply) != nil || !reply.OK {
			http.Error(w, fallback, 502)
			return
		}
		writeJSON(w, 200, reply)
	}
}

// wifiHelperError returns the helper's reason (it never contains a PSK).
func wifiHelperError(out []byte, fallback string) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(out, &e) != nil || strings.TrimSpace(e.Error) == "" {
		return fallback
	}
	return e.Error
}
