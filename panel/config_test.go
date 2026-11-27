package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The repository copy is the shipped default; every test runs against it.
const shippedSettings = "../Pi-Star_dash_new/gospot.conf"

func TestMain(m *testing.M) {
	if err := loadSettings(shippedSettings); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func TestShippedSettings(t *testing.T) {
	s, err := readSettings(shippedSettings)
	if err != nil {
		t.Fatal(err)
	}
	if mmdvmINI.location() != "/etc/mmdvmhost" || modemUnit != "mmdvmhost.service" || configHelper != "/usr/lib/gospot/gospot-config" {
		t.Fatalf("unexpected defaults: %s %s %s", mmdvmINI.location(), modemUnit, configHelper)
	}
	for k, v := range s {
		if strings.HasPrefix(k, "CONF_") && !filepath.IsAbs(v) {
			t.Errorf("%s is not absolute", k)
		}
	}
}

func TestSettingsRejectUnsafeValues(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"quoted":    "CONF_MMDVMHOST=\"/etc/mmdvmhost\"\n",
		"spaces":    "CONF_MMDVMHOST=/etc/mmdvm host\n",
		"expansion": "CONF_MMDVMHOST=$HOME/x\n",
		"duplicate": "A=1\nA=2\n",
		"lowercase": "conf=/x\n",
	} {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte(body), 0o600)
		if _, err := readSettings(p); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	s, _ := readSettings(shippedSettings)
	s["CONF_MMDVMHOST"] = "etc/mmdvmhost"
	s["OPERATOR_PORT"] = "70000"
	err := applySettings(s)
	if err == nil || !strings.Contains(err.Error(), "CONF_MMDVMHOST") || !strings.Contains(err.Error(), "OPERATOR_PORT") {
		t.Fatalf("relative path / bad port accepted: %v", err)
	}
	if mmdvmINI.location() != "/etc/mmdvmhost" {
		t.Fatal("a rejected settings file must not change the active paths")
	}
}
