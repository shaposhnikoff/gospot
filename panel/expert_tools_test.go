package main

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFirmwareTool(t *testing.T) {
	var calls []string
	sudo := func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args[1:], " "))
		if args[1] == "list" {
			return []byte("hs_hat\nhs_dual_hat-12mhz\n"), nil
		}
		return []byte("started"), nil
	}
	do := func(method, body string, csrf bool) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		serveExpertTools(w, maintenanceRequest(method, "/manage/api/expert/fw", body, csrf), nil, sudo, "fw")
		return w
	}
	if w := do("GET", "", false); w.Code != 200 || !strings.Contains(w.Body.String(), "MMDVM_HS_Dual_Hat (12.2880MHz TCXO) GPIO") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if w := do("POST", `{"modem":"hs_hat; reboot"}`, true); w.Code != 400 {
		t.Fatalf("unknown type: %d", w.Code)
	}
	if w := do("POST", `{"modem":"hs_dual_hat-12mhz"}`, false); w.Code != 403 {
		t.Fatalf("csrf: %d", w.Code)
	}
	if w := do("POST", `{"modem":"hs_dual_hat-12mhz"}`, true); w.Code != 202 {
		t.Fatalf("flash: %d", w.Code)
	}
	if strings.Join(calls, "|") != "list|list|list|flash hs_dual_hat-12mhz" {
		t.Fatalf("%v", calls)
	}
}

func TestShellPort(t *testing.T) {
	saved := shellinaboxDefaults
	defer func() { shellinaboxDefaults = saved }()
	shellinaboxDefaults = filepath.Join(t.TempDir(), "shellinabox")
	if shellPort() != 0 {
		t.Fatal("missing file must give 0")
	}
	os.WriteFile(shellinaboxDefaults, []byte("SHELLINABOX_DAEMON_START=1\nSHELLINABOX_PORT=2222\n"), 0o644)
	if shellPort() != 2222 {
		t.Fatal(shellPort())
	}
}
