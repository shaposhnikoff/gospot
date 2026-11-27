package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// settingsPath is the installed copy of Pi-Star_dash_new/pistar.conf, the
// single source of system paths (see AGENTS.md).
var settingsPath = "/etc/pistar-dashboard/pistar.conf"

type settings map[string]string

var settingKey = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

func readSettings(path string) (settings, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	s := settings{}
	scan := bufio.NewScanner(f)
	for n := 1; scan.Scan(); n++ {
		line := strings.TrimSpace(scan.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || !settingKey.MatchString(k) || strings.ContainsAny(v, " \t\"'$`\\") {
			return nil, fmt.Errorf("%s:%d: expected KEY=value without quotes or spaces", path, n)
		}
		if _, dup := s[k]; dup {
			return nil, fmt.Errorf("%s:%d: duplicate %s", path, n, k)
		}
		s[k] = v
	}
	return s, scan.Err()
}

func (s settings) abs(key string) (string, error) {
	v := s[key]
	if v == "" || !filepath.IsAbs(v) || filepath.Clean(v) != v {
		return "", fmt.Errorf("%s must be a clean absolute path", key)
	}
	return v, nil
}

func (s settings) port(key string) (int, error) {
	n, err := strconv.Atoi(s[key])
	if err != nil || n < 1 || n > 65535 {
		return 0, fmt.Errorf("%s must be a TCP port", key)
	}
	return n, nil
}

func (s settings) unit(key string) (string, error) {
	v := s[key]
	if !regexp.MustCompile(`^[A-Za-z0-9@_.-]+\.service$`).MatchString(v) {
		return "", fmt.Errorf("%s must be a systemd service name", key)
	}
	return v, nil
}

// Paths and ports taken from settings by applySettings.
var (
	modemUnit, masterUnit                            string
	markerMMDVMHost, markerDStarRepeater             string
	hostnameFile                                     string
	bmStateDir                                       string
	pistarLogDir, radioLogSource, pistarReleaseFile  string
	pistarHostsDir                                   string
	pistarBinDir, pistarSbinDir                      string
	cfgPanelPort, cfgManagementPort, cfgOperatorPort int
)

// applySettings validates every key the panel uses and installs the values.
func applySettings(s settings) error {
	var errs []string
	path := func(key string) string {
		v, err := s.abs(key)
		if err != nil {
			errs = append(errs, err.Error())
		}
		return v
	}
	unit := func(key string) string {
		v, err := s.unit(key)
		if err != nil {
			errs = append(errs, err.Error())
		}
		return v
	}
	port := func(key string) int {
		v, err := s.port(key)
		if err != nil {
			errs = append(errs, err.Error())
		}
		return v
	}
	conf := map[*iniFile]string{
		mmdvmINI: "CONF_MMDVMHOST", dmrGWINI: "CONF_DMRGATEWAY", dapnetINI: "CONF_DAPNETGATEWAY",
		ircddbINI: "CONF_IRCDDBGATEWAY", dstarRptINI: "CONF_DSTARREPEATER",
		ysfGWINI: "CONF_YSFGATEWAY", p25GWINI: "CONF_P25GATEWAY", nxdnGWINI: "CONF_NXDNGATEWAY", m17GWINI: "CONF_M17GATEWAY",
		ysf2dmrINI: "CONF_YSF2DMR", ysf2nxdnINI: "CONF_YSF2NXDN", ysf2p25INI: "CONF_YSF2P25",
		dmr2ysfINI: "CONF_DMR2YSF", dmr2nxdnINI: "CONF_DMR2NXDN",
	}
	files := map[*iniFile]string{}
	for f, key := range conf {
		files[f] = path(key)
	}
	modem, master := unit("UNIT_MMDVMHOST"), unit("UNIT_DMRGATEWAY")
	mm, ds, host := path("MARKER_MMDVMHOST"), path("MARKER_DSTARREPEATER"), path("HOSTNAME_FILE")
	lib, ui, bm := path("DASHBOARD_LIB_DIR"), path("DASHBOARD_UI_DIR"), path("DASHBOARD_BM_STATE_DIR")
	logDir, release, hosts := path("PISTAR_LOG_DIR"), path("PISTAR_RELEASE_FILE"), path("PISTAR_HOSTS_DIR")
	cssFile := path("PISTAR_CSS_FILE")
	update := path("PISTAR_UPDATE_LOG")
	path("MODEM_FLASH_LOG")
	path("SHELLINABOX_DEFAULTS")
	bmKey := path("BM_API_KEY_FILE")
	binDir, sbinDir := path("PISTAR_BIN_DIR"), path("PISTAR_SBIN_DIR")
	source := s["RADIO_LOG_SOURCE"]
	if source != "files" && source != "journal" {
		errs = append(errs, "RADIO_LOG_SOURCE must be files or journal")
	}
	// The Wi-Fi page's helper reads these; refuse a file it would reject.
	if b := s["WIFI_BACKEND"]; b != "networkmanager" && b != "wpa_supplicant" {
		errs = append(errs, "WIFI_BACKEND must be networkmanager or wpa_supplicant")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_.-]{1,15}$`).MatchString(s["WIFI_INTERFACE"]) {
		errs = append(errs, "WIFI_INTERFACE must be an interface name")
	}
	for _, key := range []string{"WPA_SUPPLICANT_CONF", "SYSFS_NET_DIR", "WPA_CTRL_DIR", "WIFI_REGDOM_CONF", "NMCLI_BIN", "WPA_CLI_BIN", "IP_BIN", "IW_BIN"} {
		path(key)
	}
	pp, mp, op := port("PANEL_PORT"), port("MANAGEMENT_PORT"), port("OPERATOR_PORT")
	if len(errs) > 0 {
		return fmt.Errorf("invalid settings: %s", strings.Join(errs, "; "))
	}
	if err := setCfgSystemPaths(s); err != nil {
		return err
	}
	for f, p := range files {
		f.setPath(p)
	}
	modemUnit, masterUnit = modem, master
	markerMMDVMHost, markerDStarRepeater, hostnameFile = mm, ds, host
	adminAssets, operatorAssets = ui, ui
	configHelper = filepath.Join(lib, "pistar-config")
	powerHelper, updateHelper, updateLog = filepath.Join(lib, "pistar-power"), filepath.Join(lib, "pistar-native-update"), update
	setConfigHelper(lib)
	setToolPaths(lib, s)
	bmStateDir = bm
	pistarLogDir, radioLogSource, pistarReleaseFile, pistarHostsDir = logDir, source, release, hosts
	pistarBinDir, pistarSbinDir = binDir, sbinDir
	bmKeyFile = bmKey
	pistarCSSFile = cssFile
	cfgPanelPort, cfgManagementPort, cfgOperatorPort = pp, mp, op
	modemDiagnostic, masterDiagnostic = diagnosticFor(modem, modemRules), diagnosticFor(master, masterRules)
	return nil
}

func loadSettings(path string) error {
	s, err := readSettings(path)
	if err != nil {
		return err
	}
	return applySettings(s)
}
