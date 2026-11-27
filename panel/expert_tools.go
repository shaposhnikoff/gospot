package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Expert > API Keys (fulledit_apikeys.php), FW (modem_fw_upgrade.php) and
// SSH Access (ssh_access.php). Dispatch only after wizardHandler's checks.

var modemHelper, modemFlashLog, shellinaboxDefaults string

// Friendly names from modem_fw_upgrade.php.
var modemNames = map[string]string{
	"hs_hat": "MMDVM_HS_Hat (14.7456MHz TCXO) GPIO", "hs_hat-12mhz": "MMDVM_HS_Hat (12.2880MHz TCXO) GPIO",
	"hs_dual_hat": "MMDVM_HS_Dual_Hat (14.7456MHz TCXO) GPIO", "hs_dual_hat-12mhz": "MMDVM_HS_Dual_Hat (12.2880MHz TCXO) GPIO",
	"zum_rpi": "ZUMSpot RPi boards/hotspots GPIO", "zum_rpi-duplex": "ZUMSpot RPi duplex GPIO board/hotspots",
	"zum_usb": "ZUMspot USB stick", "zum_libre": "ZUMspot Libre Kit or generic MMDVM_HS board",
	"skybridge": "SkyBridge board/hotspots (14.7456MHz TCXO) GPIO", "euronode": "DVMega-EuroNode hotspots (14.7456MHz TCXO) GPIO",
	"nanodv_npi": "NANO_DV NPi GPIO by BG4TGO", "nanodv_usb": "NANO_DV USB by BG4TG",
	"hs_hat_ambe": "HS_HAT_AMBE (14.7456MHz TCXO) GPIO", "hs_hat_lonestar-usb": "LoneStar LS MMDVM USB (14.7456MHz TCXO) USB",
	"hs_hat_generic": "MMDVM_HS_GENERIC (14.7456MHz TCXO) GPIO", "hs_hat_generic_duplex": "MMDVM_HS_GENERIC_DUPLEX (14.7456MHz TCXO) GPIO",
	"hs_hat_generic_duplex-usb": "MMDVM_HS_GENERIC_DUPLEX (14.7456MHz TCXO) USB",
	"hs_hat_nano_hotspot":       "Nano_hotSPOT by BI7JTA (14.7456MHz TCXO) GPIO", "hs_hat_nano_hotspot-duplex": "Nano_hotSPOT Dual by VR2VYE & BI7JTA (14.7456MHz TCXO) GPIO",
	"dvmega_gpio": "DV-Mega Raspberry Pi Hat (Single or Dual Band) GPIO", "dvmega_usb_uno": "DV-Mega Arduino Uno Shield USB (ttyUSB0)",
	"dvmega_usb_mega": "DV-Mega Arduino Mega Shield USB (ttyUSB0)", "rpt_nucleo-64": "Repeater - Nucleo64 F446RE GPIO",
	"rpt_nucleo-144": "Repeater - Nucleo144 F767ZI GPIO", "rpt_mmdvm_hat-0.2": "Repeater - MMDVM_RPT_Hat 0.2 GPIO",
	"rpt_mmdvm_hat-0.3": "Repeater - MMDVM_RPT_Hat 0.3 GPIO", "rpt_zum-0.9": "Repeater - ZUM Radio MMDVM for Pi v0.9 GPIO",
	"rpt_zum-1.0": "Repeater - ZUM Radio MMDVM for Pi v1.0 GPIO", "rpt_builder_v3": "Repeater - Repeater Builder STM32_DVM_PiHat V3 GPIO",
	"rpt_builder_v4": "Repeater - Repeater Builder STM32_DVM_PiHat V4 GPIO", "rpt_builder_v5": "Repeater - Repeater Builder STM32_DVM_PiHat V5 GPIO",
}

type runFunc func(ctx context.Context, args ...string) ([]byte, error)

func runSudo(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/sudo", append([]string{"-n"}, args...)...)
	cmd.WaitDelay = time.Second
	return cmd.Output()
}

func modemTypes(ctx context.Context, run runFunc) ([]linkOption, error) {
	out, err := run(ctx, modemHelper, "list")
	if err != nil {
		return nil, err
	}
	var opts []linkOption
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			name := modemNames[l]
			if name == "" {
				name = l
			}
			opts = append(opts, linkOption{l, name})
		}
	}
	return opts, nil
}

func flashRunning(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, _ := runDiagnostic(ctx, "systemctl", "is-active", "gospot-modem-flash.service")
	return strings.TrimSpace(string(out)) == "active"
}

// shellPort reads SHELLINABOX_PORT from the shellinabox defaults file.
func shellPort() int {
	f, err := os.Open(shellinaboxDefaults)
	if err != nil {
		return 0
	}
	defer f.Close()
	s := bufio.NewScanner(io.LimitReader(f, 64<<10))
	for s.Scan() {
		if v, ok := strings.CutPrefix(strings.TrimSpace(s.Text()), "SHELLINABOX_PORT="); ok {
			if n, err := strconv.Atoi(strings.Trim(v, `"'`)); err == nil && n > 0 && n < 65536 {
				return n
			}
		}
	}
	return 0
}

func serveExpertTools(w http.ResponseWriter, r *http.Request, run configHelperFunc, sudo runFunc, tool string) {
	switch {
	case tool == "keys" && r.Method == http.MethodGet:
		csrf := editorToken(w, r)
		if csrf == "" {
			return
		}
		out, err := run(r.Context(), "keys-read", nil)
		var reply map[string]any
		if err != nil || json.Unmarshal(out, &reply) != nil {
			http.Error(w, "Could not read the API keys", 502)
			return
		}
		reply["csrf"] = csrf
		writeJSON(w, 200, reply)
	case tool == "keys" && r.Method == http.MethodPost:
		var in struct {
			Target string                       `json:"target"`
			Values map[string]map[string]string `json:"values"`
		}
		if !maintenancePost(w, r, &in) {
			return
		}
		body, _ := json.Marshal(in)
		if out, err := run(r.Context(), "keys-write", body); err != nil {
			var e struct{ Error string }
			_ = json.Unmarshal(out, &e)
			if e.Error == "" {
				e.Error = "save failed"
			}
			http.Error(w, e.Error, 422)
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	case tool == "fw" && r.Method == http.MethodGet:
		csrf := editorToken(w, r)
		if csrf == "" {
			return
		}
		opts, err := modemTypes(r.Context(), sudo)
		if err != nil {
			http.Error(w, "Error executing the command.", 502)
			return
		}
		offset, ok := queryOffset(r)
		if !ok {
			http.Error(w, "Некорректная позиция", 400)
			return
		}
		reply, _ := tailFile(modemFlashLog, max(offset, 0))
		running := flashRunning(r.Context())
		writeJSON(w, 200, map[string]any{"csrf": csrf, "options": opts, "running": running, "offset": reply.Offset, "data": reply.Data})
	case tool == "fw" && r.Method == http.MethodPost:
		var in struct {
			Modem string `json:"modem"`
		}
		if !maintenancePost(w, r, &in) {
			return
		}
		opts, err := modemTypes(r.Context(), sudo)
		known := false
		for _, o := range opts {
			known = known || o.Value == in.Modem
		}
		if err != nil || !known {
			http.Error(w, "Unknown modem type", 400)
			return
		}
		if flashRunning(r.Context()) {
			http.Error(w, "A firmware upgrade is already running", 409)
			return
		}
		if _, err := sudo(r.Context(), modemHelper, "flash", in.Modem); err != nil {
			http.Error(w, "Could not start the upgrade", 502)
			return
		}
		writeJSON(w, 202, map[string]bool{"started": true})
	case tool == "ssh" && r.Method == http.MethodGet:
		writeJSON(w, 200, map[string]int{"port": shellPort()})
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Метод не поддерживается", 405)
	}
}

func setToolPaths(lib string, s settings) {
	modemHelper = filepath.Join(lib, "gospot-modem")
	modemFlashLog, shellinaboxDefaults = s["MODEM_FLASH_LOG"], s["SHELLINABOX_DEFAULTS"]
}
