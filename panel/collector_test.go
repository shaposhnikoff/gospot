package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestCollectorEvidence(t *testing.T) {
	const id = "0123456789abcdef0123456789abcdef"
	const other = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const running = "LoadState=loaded\nActiveState=active\nSubState=running\nInvocationID=" + id
	for _, c := range []struct {
		name                               string
		spec                               diagnosticSpec
		message, id, service, after, state string
	}{
		{"protocol", modemDiagnostic, "I: 2026-11-25 14:30:31.831 MMDVM protocol version: 1, description: PRIVATE", id, running, running, "up"},
		{"no reply", modemDiagnostic, "No reply from the modem for some time, resetting it", id, running, running, "down"},
		{"unsupported", modemDiagnostic, "MMDVM protocol version: 3, unsupported by this version of the MMDVM Host", id, running, running, "down"},
		{"opening", modemDiagnostic, "Opening the MMDVM", id, running, running, "unknown"},
		{"login", masterDiagnostic, "M: 2026-11-25 14:30:39.746 GoDMRServer, Logged into the master successfully", id, running, running, "up"},
		{"timeout", masterDiagnostic, "GoDMRServer, Connection to the master has timed out, retrying connection", id, running, running, "down"},
		{"other network", masterDiagnostic, "Other, Logged into the master successfully", id, running, running, "unknown"},
		{"mqtt", masterDiagnostic, "MQTT Warning: Unable to connect immediately: Connection refused", id, running, running, "unknown"},
		{"old invocation", masterDiagnostic, "GoDMRServer, Logged into the master successfully", other, running, running, "unknown"},
		{"restart race", masterDiagnostic, "GoDMRServer, Logged into the master successfully", id, running, strings.ReplaceAll(running, id, other), "unknown"},
		{"failed unit", masterDiagnostic, "", id, strings.Replace(running, "ActiveState=active", "ActiveState=failed", 1), "", "down"},
		{"missing unit", masterDiagnostic, "", id, "LoadState=not-found\nActiveState=inactive", "", "unknown"},
		{"no access", masterDiagnostic, "", id, running, running, "unknown"},
	} {
		t.Run(c.name, func(t *testing.T) {
			reads := 0
			run := func(_ context.Context, cmd string, args ...string) ([]byte, error) {
				if cmd == "systemctl" {
					reads++
					if reads == 1 {
						return []byte(c.service), nil
					}
					return []byte(c.after), nil
				}
				if cmd != "journalctl" || !strings.Contains(strings.Join(args, " "), " -b -u "+c.spec.unit+" _SYSTEMD_INVOCATION_ID="+id+" ") {
					t.Fatalf("unscoped diagnostic: %v %v", cmd, args)
				}
				if c.message == "" {
					return nil, errors.New("PRIVATE")
				}
				return json.Marshal(map[string]string{"MESSAGE": c.message, "__REALTIME_TIMESTAMP": "1700000000123456", "_SYSTEMD_INVOCATION_ID": c.id})
			}
			s := collectSignal(context.Background(), run, c.spec)
			if s.State != c.state || strings.Contains(s.Detail+s.Source, "PRIVATE") {
				t.Fatalf("unexpected signal: %+v", s)
			}
			if c.state == "up" && s.At.UnixMicro() != 1700000000123456 {
				t.Fatalf("wrong evidence timestamp: %v", s.At)
			}
		})
	}
}
