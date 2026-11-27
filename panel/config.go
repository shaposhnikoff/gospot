package main

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// settingsPath is the installed copy of Pi-Star_dash_new/gospot.conf, the
// single source of system paths (see AGENTS.md).
var settingsPath = "/etc/gospot/gospot.conf"

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

func (s settings) seconds(key string, low, high int) (time.Duration, error) {
	n, err := strconv.Atoi(s[key])
	if err != nil || n < low || n > high {
		return 0, fmt.Errorf("%s must be %d to %d seconds", key, low, high)
	}
	return time.Duration(n) * time.Second, nil
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
	gospotLogDir, radioLogSource, gospotReleaseFile  string
	gospotHostsDir                                   string
	gospotBinDir, gospotSbinDir                      string
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
	seconds := func(key string, low, high int) time.Duration {
		v, err := s.seconds(key, low, high)
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
	logDir, release, hosts := path("GOSPOT_LOG_DIR"), path("GOSPOT_RELEASE_FILE"), path("GOSPOT_HOSTS_DIR")
	cssFile := path("GOSPOT_CSS_FILE")
	keeperLog := path("GOSPOT_KEEPER_LOG")
	update := path("GOSPOT_UPDATE_LOG")
	path("MODEM_FLASH_LOG")
	jitter, jitterLimit := path("GOSPOT_JITTER_LOG"), seconds("JITTER_TIMEOUT", 1, 9999)
	calIdle := seconds("CALIBRATION_IDLE_TIMEOUT", 5, 3600)
	path("SHELLINABOX_DEFAULTS")
	bmKey := path("BM_API_KEY_FILE")
	tgifKey := path("TGIF_API_KEY_FILE")
	baseURL := func(key string, schemes ...string) string {
		v := s[key]
		u, err := url.Parse(v)
		if err != nil || !slices.Contains(schemes, u.Scheme) || u.Host == "" || u.User != nil ||
			u.RawQuery != "" || u.Fragment != "" || !strings.HasSuffix(u.Path, "/") {
			errs = append(errs, fmt.Sprintf("%s must be a %s URL ending with /", key, strings.Join(schemes, " or ")))
		}
		return v
	}
	tgifAPI, tgifSession := baseURL("TGIF_API_URL", "https"), baseURL("TGIF_SESSION_URL", "http", "https")
	hosePage, hoseSpotter := baseURL("HOSELINE_PAGE_URL", "https"), baseURL("HOSELINE_SPOTTER_URL", "wss")
	hoseMax, err := strconv.Atoi(s["HOSELINE_MAX_LISTENERS"])
	if err != nil || hoseMax < 1 || hoseMax > 32 {
		errs = append(errs, "HOSELINE_MAX_LISTENERS must be 1 to 32")
	}
	hoseGroups, err := talkgroupList(s["HOSELINE_TALKGROUPS"])
	if err != nil {
		errs = append(errs, "HOSELINE_TALKGROUPS "+err.Error())
	}
	binDir, sbinDir := path("GOSPOT_BIN_DIR"), path("GOSPOT_SBIN_DIR")
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
	configHelper = filepath.Join(lib, "gospot-config")
	powerHelper, updateHelper, updateLog = filepath.Join(lib, "gospot-power"), filepath.Join(lib, "gospot-native-update"), update
	setConfigHelper(lib, s)
	setToolPaths(lib, s)
	setCalibrationPaths(lib, calIdle, jitter, jitterLimit)
	bmStateDir = bm
	gospotLogDir, radioLogSource, gospotReleaseFile, gospotHostsDir = logDir, source, release, hosts
	gospotBinDir, gospotSbinDir = binDir, sbinDir
	bmKeyFile = bmKey
	tgifKeyFile, tgifAPIURL, tgifSessionURL = tgifKey, tgifAPI, tgifSession
	hoselinePageURL, hoselineSpotterURL, hoselineMaxListeners, hoselineTalkgroups = hosePage, hoseSpotter, hoseMax, hoseGroups
	gospotCSSFile = cssFile
	gospotKeeperLog = keeperLog
	cfgPanelPort, cfgManagementPort, cfgOperatorPort = pp, mp, op
	modemDiagnostic, masterDiagnostic = diagnosticFor(modem, modemRules), diagnosticFor(master, masterRules)
	return nil
}

// talkgroupList parses a comma separated list of BrandMeister talkgroups.
func talkgroupList(v string) ([]int, error) {
	parts := strings.Split(v, ",")
	if v == "" || len(parts) > 64 {
		return nil, fmt.Errorf("must list 1 to 64 talkgroups")
	}
	var out []int
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || !hoselineTGPattern.MatchString(p) || n < hoselineMinTG || n > hoselineMaxTG {
			return nil, fmt.Errorf("must be comma separated talkgroups %d to %d", hoselineMinTG, hoselineMaxTG)
		}
		out = append(out, n)
	}
	return out, nil
}

func loadSettings(path string) error {
	s, err := readSettings(path)
	if err != nil {
		return err
	}
	return applySettings(s)
}
