package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Every section together: building the page and posting every displayed
// value back (as Configure.svelte does, one form at a time) must plan no
// change at all. Uses the QA stand's minimal INIs; PISTAR_ROUNDTRIP_DIR may
// point at a directory of real board files named after configState files
// (mmdvmhost, dmrgateway, ...; a "marker" file is the controller marker).
func TestConfigureUntouchedRoundTrip(t *testing.T) {
	stand := filepath.Join("..", "image", "stage-mmdvm", "00-install", "files")
	host, err := os.ReadFile(filepath.Join(stand, "MMDVM-Host.ini"))
	if err != nil {
		t.Fatal(err)
	}
	gw, err := os.ReadFile(filepath.Join(stand, "DMRGateway.ini"))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]configState{
		"stand": ctlState(map[string]string{
			"mmdvmhost":  strings.NewReplacer("Callsign=NOCALL\n", "Callsign=DEMO1\n", "Id=1\n", "Id=10001\n").Replace(string(host)),
			"dmrgateway": strings.NewReplacer("Address=master.invalid\n", "Address=192.0.2.11\n", "Id=1\n", "Id=10001\n", "Password=\n", "Password=test-only\n").Replace(string(gw)),
		}, map[string]*string{"mmdvmhost": ctlMarker("zumspotgpio"), "dstarrepeater": nil}),
		"classic base": ctlBase(),
	}
	if dir := os.Getenv("PISTAR_ROUNDTRIP_DIR"); dir != "" {
		files := map[string]string{}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		var marker *string
		for _, e := range entries {
			b, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if e.Name() == "marker" {
				s := string(b)
				marker = &s
				continue
			}
			files[e.Name()] = string(b)
		}
		cases["board"] = ctlState(files, map[string]*string{"mmdvmhost": marker, "dstarrepeater": nil})
	}
	all := cfgSections
	for name, st := range cases {
		t.Run(name, func(t *testing.T) {
			ctlEnv(t)
			cfgSections = all
			c := &cfgContext{St: st}
			page := buildCfgPage(c)
			if len(page) < 4 {
				t.Fatalf("only %d sections", len(page))
			}
			values := ctlForm(c)
			for _, formName := range []string{"config", "autoApPassForm", "adminPassForm"} {
				posted := cfgForm{}
				for _, s := range page {
					f := s.Form
					if f == "" {
						f = "config"
					}
					if f != formName {
						continue
					}
					for _, row := range s.Rows {
						for _, cell := range row.Cells {
							for _, ctl := range cell.Controls {
								if v, ok := values[ctl.Name]; ok {
									posted[ctl.Name] = v
								}
							}
						}
					}
				}
				req, err := planCfgApply(c, st.Revision, posted)
				if err != nil {
					t.Fatalf("%s: untouched form refused: %v", formName, err)
				}
				var diffs []string
				for file, secs := range req.Changes {
					for sec, keys := range secs {
						for k, v := range keys {
							nv := "<delete>"
							if v != nil {
								nv = *v
							}
							old, has := st.Files[file][sec][k]
							if !has {
								old = "<missing>"
							}
							diffs = append(diffs, file+" ["+sec+"] "+k+": "+old+" -> "+nv)
						}
					}
				}
				for op := range req.System {
					diffs = append(diffs, "system "+op)
				}
				sort.Strings(diffs)
				if len(diffs) > 0 || req.Restart != nil {
					t.Errorf("%s: untouched form plans changes:\n%s", formName, strings.Join(diffs, "\n"))
				}
			}
		})
	}
}

// An edit keeps exactly its own effects: the stand's Colour Code change is the
// one line MMDVMHost needs, without the defaults the appliers stage on every
// save, and it restarts the radio services.
func TestConfigureEditKeepsOnlyItsEffects(t *testing.T) {
	all := cfgSections
	ctlEnv(t)
	cfgSections = all
	host, _ := os.ReadFile(filepath.Join("..", "image", "stage-mmdvm", "00-install", "files", "MMDVM-Host.ini"))
	st := ctlState(map[string]string{"mmdvmhost": string(host)}, map[string]*string{"mmdvmhost": ctlMarker("zumspotgpio"), "dstarrepeater": nil})
	c := &cfgContext{St: st}
	form := ctlForm(c)
	form["dmrColorCode"] = "7"
	req, err := planCfgApply(c, "r1", form)
	if err != nil {
		t.Fatal(err)
	}
	v := req.Changes["mmdvmhost"]["DMR"]["ColorCode"]
	if len(req.Changes) != 1 || len(req.Changes["mmdvmhost"]) != 1 || len(req.Changes["mmdvmhost"]["DMR"]) != 1 || v == nil || *v != "7" ||
		req.System != nil || len(req.Restart) != len(cfgRadioUnits) {
		t.Fatalf("%+v", req)
	}
	// The password form restarts nothing.
	req, err = planCfgApply(c, "r1", cfgForm{"adminPassword": "n3w-Pass", "adminPasswordConfirm": "n3w-Pass"})
	if err != nil || req.Changes != nil || req.System["password"] != "n3w-Pass" || req.Restart != nil {
		t.Fatalf("password: %v %+v", err, req)
	}
}

// Without /etc/ircddbgateway the Node Callsign shows MMDVMHost's callsign, and
// a new callsign is written to MMDVMHost and shown again after saving.
func TestConfigureCallsignWithoutIrcDDBGateway(t *testing.T) {
	all := cfgSections
	ctlEnv(t)
	cfgSections = all
	host, _ := os.ReadFile(filepath.Join("..", "image", "stage-mmdvm", "00-install", "files", "MMDVM-Host.ini"))
	text := strings.Replace(string(host), "Callsign=NOCALL\n", "Callsign=UT3UDX\n", 1)
	st := ctlState(map[string]string{"mmdvmhost": text}, map[string]*string{"mmdvmhost": ctlMarker("zumspotgpio"), "dstarrepeater": nil})
	c := &cfgContext{St: st}
	form := ctlForm(c)
	if form["confCallsign"] != "UT3UDX" {
		t.Fatalf("shown %q", form["confCallsign"])
	}
	form["confCallsign"] = "M0XYZ"
	req, err := planCfgApply(c, "r1", form)
	if err != nil {
		t.Fatal(err)
	}
	if v := req.Changes["mmdvmhost"]["General"]["Callsign"]; v == nil || *v != "M0XYZ" {
		t.Fatalf("%+v", req.Changes)
	}
}
