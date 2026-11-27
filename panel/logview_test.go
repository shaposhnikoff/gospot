package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func radioRecord(t *testing.T, unit string, msg any) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"__REALTIME_TIMESTAMP": "1700000000123456", "_SYSTEMD_UNIT": unit,
		"MESSAGE": msg, "PRIORITY": "6", "EXTRA": "PRIVATE"})
	if err != nil {
		t.Fatal(err)
	}
	return append(b, '\n')
}

func TestRadioLogRedaction(t *testing.T) {
	const prefix = "M: 2023-11-14 22:13:20.123 "
	const voice = "DMR Slot 2, received RF voice header from PRIVATE to TG 91"
	const gateway = "GoDMRServer, Logged into the master successfully"
	var out bytes.Buffer
	out.Write(radioRecord(t, modemUnit, prefix+voice))
	out.Write(radioRecord(t, masterUnit, gateway))
	out.Write(radioRecord(t, modemUnit, "I: 2023-11-14 22:13:20.123 MMDVM protocol version: 2, description: <PRIVATE>"))
	out.Write(radioRecord(t, modemUnit, "DMR Slot 1, received network end of voice transmission from PRIVATE to TG 9, 4.8 seconds, 2% packet loss, BER: 0.0%"))
	for _, e := range []struct {
		unit string
		msg  any
	}{
		{modemUnit, prefix + voice + "\nPRIVATE"}, {modemUnit, prefix + voice + "\x00"}, {modemUnit, prefix + prefix + voice},
		{modemUnit, prefix + voice + ", 3 blocks"}, {modemUnit, prefix + "MQTT " + voice}, {masterUnit, "Password=PRIVATE"},
		{modemUnit, "Opening the MMDVM: PRIVATE"}, {modemUnit, gateway}, {masterUnit, voice}, {"other.service", voice},
		{masterUnit, gateway + " PRIVATE"}, {masterUnit, "PRIVATE, Logged into the master successfully"},
		{modemUnit, []int{1, 2}}, {modemUnit, nil}, {modemUnit, strings.Repeat("P", 600)},
	} {
		out.Write(radioRecord(t, e.unit, e.msg))
	}
	out.WriteString("bad json\n")
	for _, badStamp := range []string{"not-a-time", "-10", "9223372036854775807"} {
		out.Write(bytes.Replace(radioRecord(t, modemUnit, voice), []byte("1700000000123456"), []byte(badStamp), 1))
	}
	at := time.UnixMicro(1700000000123456).UTC()
	want := []radioLogRow{
		{at, "MMDVMHost", "message", "DMR Slot 2, received RF voice header"},
		{at, "DMRGateway", "info", "Logged into the master successfully"},
		{at, "MMDVMHost", "info", "MMDVM protocol version: 2"},
		{at, "MMDVMHost", "info", "DMR Slot 1, network end of voice transmission, 4.8 seconds, BER: 0.0%"},
	}
	if got := radioLogRows(out.Bytes()); !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected radio events: %+v", got)
	}
	tail := bytes.Repeat(radioRecord(t, modemUnit, "unrelated"), radioLogLimit)
	if got := radioLogRows(append(tail, out.Bytes()...)); len(got) != 0 {
		t.Fatal("parser exceeded journal window")
	}
}

func TestRadioLogResponse(t *testing.T) {
	var readErr error
	out := radioRecord(t, masterUnit, "W: 2023-11-14 22:13:20.123 GoDMRServer, Login to the master has failed, retrying login ...")
	out = append(out, radioRecord(t, modemUnit, "MQTT password=PRIVATE")...)
	wantArgs := []string{"--system", "--quiet", "--no-pager", "--boot=0", "--reverse", "-n", "64", "-o", "json",
		"--output-fields=_SYSTEMD_UNIT,MESSAGE,PRIORITY", "_SYSTEMD_UNIT=mmdvmhost.service", "_SYSTEMD_UNIT=dmrgateway.service"}
	run := func(ctx context.Context, cmd string, args ...string) ([]byte, error) {
		deadline, ok := ctx.Deadline()
		if cmd != "/usr/bin/journalctl" || !reflect.DeepEqual(args, wantArgs) || !ok || time.Until(deadline) > 3*time.Second {
			t.Fatalf("unexpected command or deadline: %q %q %v", cmd, args, deadline)
		}
		return out, readErr
	}
	for _, path := range []string{radioLogPath, radioLogDownload} {
		fetch := func() *httptest.ResponseRecorder {
			w := httptest.NewRecorder()
			serveRadioLogWith(w, httptest.NewRequest("GET", path, nil), run)
			if strings.Contains(w.Body.String(), "PRIVATE") || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
				t.Fatal("private raw data or cacheable response")
			}
			return w
		}
		readErr = nil
		w := fetch()
		if w.Code != 200 {
			t.Fatal(w.Code)
		}
		if path == radioLogDownload {
			if w.Header().Get("Content-Disposition") != `attachment; filename="pi-star-radio-log.txt"` ||
				w.Header().Get("Content-Type") != "text/plain; charset=utf-8" ||
				w.Body.String() != "2023-11-14T22:13:20.123456Z\tDMRGateway\twarning\tLogin to the master has failed, retrying login ...\n" {
				t.Fatal("unexpected download:", w.Body.String())
			}
		} else {
			var result struct{ Rows []radioLogRow }
			if json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Rows) != 1 || result.Rows[0].Level != "warning" {
				t.Fatal("unexpected JSON:", w.Body.String())
			}
		}
		readErr = errors.New("PRIVATE")
		if w := fetch(); w.Code != 503 || w.Header().Get("Content-Disposition") != "" || strings.Contains(w.Body.String(), "DMRGateway") {
			t.Fatal("failed journal read returned data")
		}
	}
	out, readErr = nil, nil
	w := httptest.NewRecorder()
	serveRadioLogWith(w, httptest.NewRequest("GET", radioLogPath, nil), run)
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"rows":[]}` {
		t.Fatal("a quiet journal should be a successful empty snapshot")
	}
}

func TestRadioLogManagementBoundary(t *testing.T) {
	wizardSetup(t)
	mux := wizardTestMux(func(context.Context, string, []byte) ([]byte, error) {
		t.Fatal("log endpoint called configuration helper")
		return nil, nil
	}, t.TempDir())
	for _, path := range []string{radioLogPath, radioLogDownload} {
		for _, c := range []struct {
			scheme, password, method, query string
			want                            int
		}{
			{"http", "admin-secret", "GET", "", 503}, {"https", "", "GET", "", 401},
			{"https", "wrong", "GET", "", 401}, {"https", "admin-secret", "POST", "", 405},
			{"https", "admin-secret", "GET", "?unit=other.service", 400},
		} {
			req := httptest.NewRequest(c.method, c.scheme+"://node"+path+c.query, nil)
			req.SetBasicAuth(wizardUser, c.password)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != c.want {
				t.Errorf("%s %s: got %d; want %d", req.Method, req.URL, w.Code, c.want)
			}
		}
	}
}
