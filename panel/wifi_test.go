package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

// A test-only secret: the helper reply below carries it in extra fields that
// must never reach the browser.
const wifiTestSecret = "qa-wifi-secret-passphrase"

const fakeWifiRead = `{"revision":"r1","backend":"wpa_supplicant","interface":"wlan0","country":"GB",
 "countries":["DE","GB"],"networks":[{"id":"0","ssid":"QA-First","secured":true,"psk":"` + wifiTestSecret + `"},
 {"id":"1","ssid":"QA-Second","secured":false}],"status":{"present":true,"up":true,"ip":"192.0.2.5","rxBytes":10,
 "psk":"` + wifiTestSecret + `"},"psk":"` + wifiTestSecret + `"}`

type wifiCall struct {
	verb  string
	input string
}

func fakeWifi(calls *[]wifiCall, reply string, err error) configHelperFunc {
	return func(_ context.Context, verb string, input []byte) ([]byte, error) {
		*calls = append(*calls, wifiCall{verb, string(input)})
		if verb == "wifi-read" {
			return []byte(fakeWifiRead), nil
		}
		return []byte(reply), err
	}
}

func TestWifiGetHidesKeys(t *testing.T) {
	var calls []wifiCall
	w := httptest.NewRecorder()
	serveWifi(w, maintenanceRequest("GET", "/manage/api/wifi", "", false), fakeWifi(&calls, "", nil))
	body := w.Body.String()
	if w.Code != 200 || strings.Contains(body, wifiTestSecret) || strings.Contains(body, "psk") {
		t.Fatalf("%d %s", w.Code, body)
	}
	var s wifiState
	if json.Unmarshal(w.Body.Bytes(), &s) != nil || s.CSRF == "" || s.Revision != "r1" || s.Backend != "wpa_supplicant" ||
		len(s.Networks) != 2 || s.Networks[0] != (wifiNetwork{"0", "QA-First", true}) || !s.Status.Present || s.Status.IP != "192.0.2.5" ||
		s.Status.RxBytes == nil || *s.Status.RxBytes != 10 || s.Status.TxBytes != nil || len(s.Countries) != 2 {
		t.Fatalf("%+v", s)
	}
	if len(calls) != 1 || calls[0].verb != "wifi-read" {
		t.Fatalf("%v", calls)
	}
}

func TestWifiGetHelperFailure(t *testing.T) {
	run := func(context.Context, string, []byte) ([]byte, error) {
		return []byte(`{"error":"NetworkManager is not available (nmcli failed)"}`), errors.New("exit 2")
	}
	w := httptest.NewRecorder()
	serveWifi(w, maintenanceRequest("GET", "/manage/api/wifi", "", false), run)
	if w.Code != 502 || !strings.Contains(w.Body.String(), "NetworkManager is not available") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

func TestWifiSave(t *testing.T) {
	var calls []wifiCall
	body := `{"action":"save","revision":"r1","country":"GB","networks":[{"id":"1","ssid":"QA-Second","psk":""},{"ssid":"QA-New","psk":"` + wifiTestSecret + `"}]}`
	w := httptest.NewRecorder()
	serveWifi(w, maintenanceRequest("POST", "/manage/api/wifi", body, true),
		fakeWifi(&calls, `{"ok":true,"message":"Wifi Settings Updated Successfully","note":"Interface wlan0 is not present"}`, nil))
	if w.Code != 200 || strings.Contains(w.Body.String(), wifiTestSecret) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var reply wifiReply
	if json.Unmarshal(w.Body.Bytes(), &reply) != nil || reply != (wifiReply{true, "Wifi Settings Updated Successfully", "Interface wlan0 is not present"}) {
		t.Fatalf("%+v", reply)
	}
	want := `{"revision":"r1","country":"GB","networks":[{"id":"1","ssid":"QA-Second","psk":""},{"ssid":"QA-New","psk":"` + wifiTestSecret + `"}]}`
	if len(calls) != 1 || calls[0].verb != "wifi-write" || calls[0].input != want {
		t.Fatalf("forwarded %v", calls)
	}
}

func TestWifiRefusals(t *testing.T) {
	save := `{"action":"save","revision":"r1","country":"GB","networks":[]}`
	for _, c := range []struct {
		name, body string
		csrf       bool
		reply      string
		err        error
		code       int
		text       string
		helper     bool
	}{
		{"missing CSRF", save, false, "", nil, 403, "", false},
		{"stale form", save, true, `{"error":"changed","stale":true}`, errConfigStale, 409, "changed since this page was loaded", true},
		{"invalid input", save, true, `{"error":"network 0: PSK must be 8-63 printable ASCII characters or exactly 64 hex characters"}`,
			errors.New("exit 2"), 422, "network 0: PSK must be", true},
		{"helper without reason", save, true, ``, errors.New("exit 1"), 422, "Save failed", true},
		{"too many networks", `{"action":"save","revision":"r1","country":"GB","networks":[` + strings.Repeat(`{"ssid":"x"},`, 32) + `{"ssid":"x"}]}`,
			true, "", nil, 422, "network count", false},
		{"oversized value", `{"action":"save","revision":"r1","country":"GB","networks":[{"ssid":"` + strings.Repeat("x", 300) + `"}]}`,
			true, "", nil, 422, "invalid network", false},
		{"unknown field", `{"action":"save","revision":"r1","psk":"x"}`, true, "", nil, 400, "", false},
		{"unknown action", `{"action":"reboot"}`, true, "", nil, 400, "", false},
		{"missing interface", `{"action":"reset"}`, true, `{"error":"Interface wlan0 is not present"}`, errors.New("exit 2"), 422,
			"Interface wlan0 is not present", true},
	} {
		var calls []wifiCall
		w := httptest.NewRecorder()
		serveWifi(w, maintenanceRequest("POST", "/manage/api/wifi", c.body, c.csrf), fakeWifi(&calls, c.reply, c.err))
		if w.Code != c.code || !strings.Contains(w.Body.String(), c.text) || (len(calls) > 0) != c.helper {
			t.Errorf("%s: %d %q calls=%v", c.name, w.Code, w.Body.String(), calls)
		}
	}
}

func TestWifiScanAndActions(t *testing.T) {
	var calls []wifiCall
	w := httptest.NewRecorder()
	serveWifi(w, maintenanceRequest("POST", "/manage/api/wifi", `{"action":"scan"}`, true),
		fakeWifi(&calls, `{"networks":[{"bssid":"aa:bb:cc:dd:ee:01","freq":"2437","signal":"-45 dBm","security":"[ESS]","ssid":"QA","extra":"x"}]}`, nil))
	if w.Code != 200 || w.Body.String() != `{"networks":[{"bssid":"aa:bb:cc:dd:ee:01","freq":"2437","signal":"-45 dBm","security":"[ESS]","ssid":"QA"}]}`+"\n" {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	for _, action := range []string{"up", "down", "reset"} {
		w = httptest.NewRecorder()
		serveWifi(w, maintenanceRequest("POST", "/manage/api/wifi", `{"action":"`+action+`"}`, true),
			fakeWifi(&calls, `{"ok":true,"message":"WiFi adapter reset"}`, nil))
		if w.Code != 200 || !strings.Contains(w.Body.String(), "WiFi adapter reset") {
			t.Fatalf("%s: %d %s", action, w.Code, w.Body.String())
		}
	}
	var verbs []string
	for _, c := range calls {
		verbs = append(verbs, c.verb)
	}
	if strings.Join(verbs, "|") != "wifi-scan|wifi-action up|wifi-action down|wifi-action reset" {
		t.Fatalf("%v", verbs)
	}
	w = httptest.NewRecorder()
	serveWifi(w, maintenanceRequest("DELETE", "/manage/api/wifi", "", false), fakeWifi(&calls, "", nil))
	if w.Code != 405 {
		t.Fatalf("method: %d", w.Code)
	}
}

func TestWifiSettingsValidated(t *testing.T) {
	s, _ := readSettings(shippedSettings)
	if s["WIFI_BACKEND"] != "networkmanager" || s["WIFI_INTERFACE"] != "wlan0" {
		t.Fatalf("shipped Wi-Fi defaults: %q %q", s["WIFI_BACKEND"], s["WIFI_INTERFACE"])
	}
	s["WIFI_BACKEND"] = "ifupdown"
	s["WIFI_INTERFACE"] = "wlan0; reboot"
	s["NMCLI_BIN"] = "nmcli"
	err := applySettings(s)
	if err == nil || !strings.Contains(err.Error(), "WIFI_BACKEND") || !strings.Contains(err.Error(), "WIFI_INTERFACE") || !strings.Contains(err.Error(), "NMCLI_BIN") {
		t.Fatalf("invalid Wi-Fi settings accepted: %v", err)
	}
}
