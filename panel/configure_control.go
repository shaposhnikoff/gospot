package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Configuration page, spec part 01 (docs/configure-spec/01-control-mmdvm-general.md):
// "Gateway Hardware Information", "Control Software", "MMDVMHost
// Configuration" and "General Configuration" of admin/configure.php.
//
// The classic page submits one form and its handlers run in source order,
// later handlers overriding earlier ones (frequency -> trxMode -> hardware ->
// modes, spec 7.2). The four sections therefore share one applier, attached
// to "General Configuration" (always shown), that runs the fields in the
// classic order; "Control Software" and "MMDVMHost Configuration" have no
// applier of their own.
//
// Deliberate deviations from the reference are marked "Deviation:".
// Common ones:
//   - PHP empty() treats "0" as "not submitted", so 0 was never saved. Here
//     only an empty (blank) field is "not submitted".
//   - Rows the classic page hides post hidden "OFF" defaults that still
//     wrote files (cross-mode toggles, DMR access list). Here a field that
//     is not rendered is never written.
//   - Input is validated server-side; bad input refuses the whole Apply
//     with an English cfgError instead of being silently stripped.
//   - Files that do not exist are never created or written (c.has).

func init() {
	registerCfgSection(cfgSectionImpl{ID: "hardware-info", Order: 10, Build: buildCtlHardwareInfo})
	registerCfgSection(cfgSectionImpl{ID: "control-software", Order: 20, Build: buildCtlControl})
	registerCfgSection(cfgSectionImpl{ID: "mmdvmhost", Order: 30, Build: buildCtlMMDVM})
	registerCfgSection(cfgSectionImpl{ID: "general", Order: 40, Build: buildCtlGeneral, Apply: applyCtl})
}

// Environment probes, replaceable in tests.
var (
	ctlSysRoot fs.FS = os.DirFS("/")
	// Device nodes are a kernel interface, not a Pi-Star path.
	ctlDevDir        = "/dev"
	ctlPlatform      = func() string { return systemPlatform(context.Background()) }
	ctlAddresses     = systemAddresses
	ctlTimezones     = ctlListTimezones
	ctlPistarVersion = func() string { return releaseINI().get("Pi-Star", "Version") }
	ctlAPRSHostsFile = func() string { return filepath.Join(pistarHostsDir, "APRSHosts.txt") }
)

const (
	ctlMM         = "mmdvmhost"
	ctlIRC        = "ircddbgateway"
	ctlDSR        = "dstarrepeater"
	ctlPowerCycle = "After applying your Configuration Settings, you will need to powercycle your Pi."
)

// ---------------------------------------------------------------------------
// Small builders.

func ctlHeader(valueBody string) cfgRow {
	return cfgRow{Cells: []cfgCell{
		{Header: true, Style: "width: 200px;", Label: "Setting", Tooltip: tip("Setting", "")},
		{Header: true, Colspan: 2, Label: "Value", Tooltip: tip("Value", valueBody)},
	}}
}

func ctlLabel(label, title, body string) cfgCell {
	return cfgCell{Align: "left", Label: label, Tooltip: tip(title, body)}
}

func ctlSpan(cell cfgCell) cfgCell {
	cell.Colspan = 2
	return cell
}

func ctlText(name, value, size, maxlength string) cfgControl {
	return text(name, value, map[string]string{"size": size, "maxlength": maxlength})
}

func ctlRadio(name, value string, options ...linkOption) cfgControl {
	return cfgControl{Kind: "radio", Name: name, Value: value, Options: options}
}

func ctlToggle(name string, on bool, aria string) cfgControl {
	t := toggle(name, on)
	t.Attrs = map[string]string{"label": aria}
	return t
}

func ctlOptionValues(options []linkOption) []string {
	out := make([]string, len(options))
	for i, o := range options {
		out[i] = o.Value
	}
	return out
}

// ---------------------------------------------------------------------------
// State helpers.

func (c *cfgContext) ctlStored(file, section, key string) bool {
	_, ok := c.St.Files[file][section][key]
	return ok
}

func ctlHasMarker(c *cfgContext, name string) bool {
	_, ok := c.marker(name)
	return ok
}

// ctlModemHardware is [Modem] Hardware of the modem marker; like the
// reference (l.209-218) the mmdvmhost marker wins when both exist.
func ctlModemHardware(c *cfgContext) string {
	for _, name := range []string{"mmdvmhost", "dstarrepeater"} {
		if text, ok := c.marker(name); ok {
			return parseINI(strings.NewReader(text)).get("Modem", "Hardware")
		}
	}
	return ""
}

// ctlVisible* are the classic visibility rules, evaluated on the stored
// (page-load) state as the reference does.
func ctlMMDVMVisible(c *cfgContext) bool { return ctlHasMarker(c, "mmdvmhost") }

func ctlDMRIdVisible(c *cfgContext) bool {
	return ctlMMDVMVisible(c) && (c.get(ctlMM, "DMR", "Enable") == "1" || c.get(ctlMM, "P25", "Enable") == "1" ||
		c.get(ctlMM, "System Fusion", "Enable") == "1")
}

func ctlNXDNIdVisible(c *cfgContext) bool {
	return ctlMMDVMVisible(c) && c.get(ctlMM, "NXDN", "Enable") == "1"
}

// PHP: $configmmdvm['DMR']['SelfOnly'] == 0 (loose: missing or "" too).
func ctlPublic(c *cfgContext) bool {
	v := c.get(ctlMM, "DMR", "SelfOnly")
	return v == "" || v == "0"
}

func ctlWhiteListVisible(c *cfgContext) bool {
	return ctlMMDVMVisible(c) && c.get(ctlMM, "DMR", "Enable") == "1" && ctlPublic(c)
}

func ctlHardwareVisible(c *cfgContext) bool {
	return ctlHasMarker(c, "mmdvmhost") || ctlHasMarker(c, "dstarrepeater")
}

func ctlDuplex(c *cfgContext) bool {
	return c.get(ctlMM, "Info", "RXFrequency") != c.get(ctlMM, "Info", "TXFrequency")
}

func ctlDevExists(name string) bool {
	_, err := os.Stat(filepath.Join(ctlDevDir, name))
	return err == nil
}

// ---------------------------------------------------------------------------
// Gateway Hardware Information (l.333-350), display only.

func buildCtlHardwareInfo(c *cfgContext) *cfgSection {
	root := ctlSysRoot
	host := systemLabel(root, "proc/sys/kernel/hostname")
	if len(host) >= 16 {
		host = host[:14] + ".."
	}
	hostCell := cfgCell{Label: host}
	// The IP addresses of the host as a tooltip on the name (operator request).
	if addrs := ctlAddresses(); len(addrs) > 0 {
		hostCell.Tooltip = tip("IP Address", strings.Join(addrs, "\n"))
	}
	load := []string{"0.00", "0.00", "0.00"}
	if f := strings.Fields(systemText(root, "proc/loadavg")); len(f) >= 3 {
		for i := range load {
			if n, err := strconv.ParseFloat(f[i], 64); err == nil {
				load[i] = strconv.FormatFloat(n, 'f', 2, 64)
			}
		}
	}
	tempC, tempF, colour := ctlCPUTemp(systemText(root, "sys/class/thermal/thermal_zone0/temp"))
	return &cfgSection{
		ID: "hardware-info", Title: "Gateway Hardware Information",
		Rows: []cfgRow{
			{Cells: []cfgCell{
				th("Hostname", tip("Hostname", "The name of host\nrunning the Pi-Star Software.")),
				th("Kernel", tip("Release", "This is the version\nnumber of the Linux Kernel running\non this Raspberry Pi.")),
				{Header: true, Colspan: 2, Label: "Platform", Tooltip: tip("Uptime:", strings.ReplaceAll(ctlUptime(systemText(root, "proc/uptime")), ",", ",\n"))},
				th("CPU Load", tip("CPU Load", "This is the standard Linux\nsystem load indicator.")),
				th("CPU Temp", tip("CPU Temp", "")),
			}},
			{ID: "hardware", Cells: []cfgCell{
				hostCell,
				{Label: systemLabel(root, "proc/sys/kernel/osrelease")},
				{Colspan: 2, Label: ctlPlatform()},
				{Label: strings.Join(load, " / ")},
				{Style: "background: " + colour, Label: tempC + "°C / " + tempF + "°F"},
			}},
		},
	}
}

// ctlCPUTemp follows l.72-78: millidegrees above 1000 are divided by 1000,
// both scales rounded to one decimal and printed the way PHP echoes floats.
func ctlCPUTemp(raw string) (c, f, colour string) {
	v, _ := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if v > 1000 {
		v /= 1000
	}
	v = math.Round(v*10) / 10
	fv := math.Round((v*9/5+32)*10) / 10
	colour = "#1d1"
	if v >= 50 {
		colour = "#fa0"
	}
	if v >= 69 {
		colour = "#f00"
	}
	return strconv.FormatFloat(v, 'f', -1, 64), strconv.FormatFloat(fv, 'f', -1, 64), colour
}

// ctlUptime formats /proc/uptime like `uptime -p` (procps).
func ctlUptime(proc string) string {
	f := strings.Fields(proc)
	if len(f) == 0 {
		return ""
	}
	secs, err := strconv.ParseFloat(f[0], 64)
	if err != nil || secs < 0 {
		return ""
	}
	m := int64(secs) / 60
	parts := []string{}
	for _, u := range []struct {
		name string
		size int64
	}{{"week", 7 * 24 * 60}, {"day", 24 * 60}, {"hour", 60}, {"minute", 1}} {
		n := m / u.size
		m %= u.size
		if n == 0 {
			continue
		}
		s := strconv.FormatInt(n, 10) + " " + u.name
		if n != 1 {
			s += "s"
		}
		parts = append(parts, s)
	}
	if len(parts) == 0 {
		parts = []string{"0 minutes"}
	}
	return "up " + strings.Join(parts, ", ")
}

// ---------------------------------------------------------------------------
// Control Software (l.4008-4040).

func ctlControllerValue(c *cfgContext) string {
	if !ctlHasMarker(c, "mmdvmhost") && ctlHasMarker(c, "dstarrepeater") {
		return "DSTAR"
	}
	return "MMDVM"
}

func ctlTRXValue(c *cfgContext) string {
	if ctlDuplex(c) {
		return "DUPLEX"
	}
	return "SIMPLEX"
}

func buildCtlControl(c *cfgContext) *cfgSection {
	controller := ctlRadio("controllerSoft", ctlControllerValue(c),
		linkOption{"DSTAR", "DStarRepeater "}, linkOption{"MMDVM", "MMDVMHost (DV-Mega Minimum Firmware 3.07 Required)"})
	// The reference alerts when the operator picks the other controller.
	other := "DSTAR"
	if controller.Value == "DSTAR" {
		other = "MMDVM"
	}
	controller.Attrs = map[string]string{"alert": ctlPowerCycle, "alertValue": other}
	return &cfgSection{
		ID: "control-software", Title: "Control Software", Apply: true,
		Rows: []cfgRow{
			ctlHeader("The current value from\nthe configuration files"),
			{ID: "controllerSoft", Cells: []cfgCell{
				ctlLabel("Controller Software:", "Radio Control Software", "Choose the software used to control the DV Radio Module. Please note that DV Mega hardware will require a firmware upgrade."),
				ctlSpan(td(controller)),
			}},
			{ID: "trxMode", Cells: []cfgCell{
				ctlLabel("Controller Mode:", "TRX Mode", "Choose the mode type Simplex node or Duplex repeater."),
				ctlSpan(td(ctlRadio("trxMode", ctlTRXValue(c),
					linkOption{"SIMPLEX", "Simplex Node "}, linkOption{"DUPLEX", "Duplex Repeater (or Half-Duplex on Hotspots)"}))),
			}},
		},
	}
}

// ---------------------------------------------------------------------------
// MMDVMHost Configuration (l.4041-4276).

type ctlNativeMode struct {
	id, label, title, toggle, aria, section, rf, net string
}

// Page order: DMR, D-Star, M17, YSF, P25, NXDN.
var ctlNativeModes = []ctlNativeMode{
	{"dmr", "DMR Mode:", "DMR Mode", "MMDVMModeDMR", "DMR Mode", "DMR", "dmrRfHangTime", "dmrNetHangTime"},
	{"dstar", "D-Star Mode:", "D-Star Mode", "MMDVMModeDSTAR", "DStar Mode", "D-Star", "dstarRfHangTime", "dstarNetHangTime"},
	{"m17", "M17 Mode:", "M17 Mode", "MMDVMModeM17", "M 17 Mode", "M17", "m17RfHangTime", "m17NetHangTime"},
	{"ysf", "YSF Mode:", "YSF Mode", "MMDVMModeFUSION", "Y S F Mode", "System Fusion", "ysfRfHangTime", "ysfNetHangTime"},
	{"p25", "P25 Mode:", "P25 Mode", "MMDVMModeP25", "P 25 Mode", "P25", "p25RfHangTime", "p25NetHangTime"},
	{"nxdn", "NXDN Mode:", "NXDN Mode", "MMDVMModeNXDN", "NXDN Mode", "NXDN", "nxdnRfHangTime", "nxdnNetHangTime"},
}

type ctlCrossMode struct {
	id, label, title, toggle, aria, file, note string
	span                                       bool
}

var ctlCrossModes = []ctlCrossMode{
	{"ysf2dmr", "YSF2DMR:", "YSF2DMR Mode", "MMDVMModeYSF2DMR", "Y S F 2 DMR Mode", "ysf2dmr", "", true},
	{"ysf2nxdn", "YSF2NXDN:", "YSF2NXDN Mode", "MMDVMModeYSF2NXDN", "Y S F 2 NXDN Mode", "ysf2nxdn", "", true},
	{"ysf2p25", "YSF2P25:", "YSF2P25 Mode", "MMDVMModeYSF2P25", "Y S F 2 P 25 Mode", "ysf2p25", "", true},
	{"dmr2ysf", "DMR2YSF:", "DMR2YSF Mode", "MMDVMModeDMR2YSF", "DMR 2 Y S F Mode", "dmr2ysf", "Uses 7 prefix on DMRGateway", false},
	{"dmr2nxdn", "DMR2NXDN:", "DMR2NXDN Mode", "MMDVMModeDMR2NXDN", "DMR 2 NXDN Mode", "dmr2nxdn", "Uses 7 prefix on DMRGateway", false},
	{"pocsag", "POCSAG:", "POCSAG Mode", "MMDVMModePOCSAG", "POCSAG Mode", "dapnetgateway", "POCSAG Paging Features", false},
}

// ctlCrossVisible: YSF2DMR is shown without a file check (l.4138); the
// others need their file; POCSAG needs /etc/dapnetgateway.
func ctlCrossVisible(c *cfgContext, m ctlCrossMode) bool {
	return m.id == "ysf2dmr" || c.has(m.file)
}

func ctlCrossOn(c *cfgContext, m ctlCrossMode) bool {
	if m.id == "pocsag" {
		return c.get(ctlMM, "POCSAG", "Enable") == "1"
	}
	return c.get(m.file, "Enabled", "Enabled") == "1"
}

func ctlHang(c *cfgContext, section string) string {
	if c.ctlStored(ctlMM, section, "ModeHang") {
		return c.get(ctlMM, section, "ModeHang")
	}
	return "20"
}

var ctlDisplayTypes = []linkOption{
	{"None", "None"}, {"OLED3", "OLED Type 3"}, {"OLED6", "OLED Type 6"}, {"Nextion", "Nextion"},
	{"HD44780", "HD44780"}, {"TFT Serial", "TFT Serial"}, {"LCDproc", "LCDproc"},
}

var ctlNextionLayouts = []struct{ value, label, stored string }{
	{"G4KLX", "G4KLX", "0"}, {"ON7LDSL2", "ON7LDS L2", "2"}, {"ON7LDSL3", "ON7LDS L3", "3"}, {"ON7LDSL3HS", "ON7LDS L3 HS", "4"},
}

func ctlDisplayType(c *cfgContext) string {
	switch d := c.get(ctlMM, "General", "Display"); d {
	case "", "None":
		return "None"
	case "OLED":
		if t := c.get(ctlMM, "OLED", "Type"); t == "3" || t == "6" {
			return "OLED" + t
		}
	case "Nextion", "HD44780", "TFT Serial", "LCDproc":
		return d
	}
	// No option matches: the browser shows the first one (l.4224-4232).
	return "None"
}

// ctlDisplayPorts builds the Nextion port list (l.4234-4262).
// Deviation: the reference's "current port" option has value "" (reversed
// str_replace arguments, l.4250); here it carries the port without "/dev/",
// and duplicate values are merged.
func ctlDisplayPorts(c *cfgContext) (options []linkOption, selected string) {
	port := c.get(ctlMM, "Nextion", "Port")
	set := c.ctlStored(ctlMM, "Nextion", "Port")
	options = []linkOption{{"None", "None"}}
	if set {
		options = append(options, linkOption{"modem", "modem"})
		if port != "None" && port != "" {
			options = append(options, linkOption{strings.TrimPrefix(port, "/dev/"), port})
		}
	}
	if entries, err := os.ReadDir(ctlDevDir); err == nil {
		names := []string{}
		for _, e := range entries {
			if n := e.Name(); strings.Contains(n, "ttyA") || strings.Contains(n, "ttyUSB") {
				names = append(names, n)
			}
		}
		sort.Strings(names)
		for _, n := range names {
			options = append(options, linkOption{n, "/dev/" + n})
		}
	}
	for _, n := range []string{"ttyS2", "ttyNextionDriver"} {
		if ctlDevExists(n) {
			options = append(options, linkOption{n, "/dev/" + n})
		}
	}
	seen := map[string]bool{}
	out := options[:0]
	for _, o := range options {
		if !seen[o.Value] {
			seen[o.Value] = true
			out = append(out, o)
		}
	}
	selected = "None"
	if set && port != "None" && port != "" {
		selected = strings.TrimPrefix(port, "/dev/")
	}
	return out, selected
}

func ctlNextionLayout(c *cfgContext) string {
	stored := c.get(ctlMM, "Nextion", "ScreenLayout")
	for _, l := range ctlNextionLayouts {
		if l.stored == stored {
			return l.value
		}
	}
	return "G4KLX"
}

func buildCtlMMDVM(c *cfgContext) *cfgSection {
	if !ctlMMDVMVisible(c) {
		return nil
	}
	rows := []cfgRow{ctlHeader("The current value from\nthe configuration files")}
	for _, m := range ctlNativeModes {
		rows = append(rows, cfgRow{ID: m.toggle, Cells: []cfgCell{
			ctlLabel(m.label, m.title, "Turn on "+strings.TrimSuffix(m.title, " Mode")+" Features"),
			td(ctlToggle(m.toggle, c.get(ctlMM, m.section, "Enable") == "1", m.aria)),
			{Controls: []cfgControl{
				static("RF Hangtime: "), ctlText(m.rf, ctlHang(c, m.section), "7", "3"),
				static("Net Hangtime: "), ctlText(m.net, ctlHang(c, m.section+" Network"), "7", "3"),
			}},
		}})
	}
	for _, m := range ctlCrossModes {
		if !ctlCrossVisible(c, m) {
			continue
		}
		cells := []cfgCell{
			ctlLabel(m.label, m.title, "Turn on "+strings.TrimSuffix(m.title, " Mode")+" Features"),
			td(ctlToggle(m.toggle, ctlCrossOn(c, m), m.aria)),
		}
		if m.span {
			cells[1] = ctlSpan(cells[1])
		} else {
			cells = append(cells, cfgCell{Controls: []cfgControl{static(m.note)}})
		}
		rows = append(rows, cfgRow{ID: m.toggle, Cells: cells})
	}
	ports, port := ctlDisplayPorts(c)
	layouts := make([]linkOption, len(ctlNextionLayouts))
	for i, l := range ctlNextionLayouts {
		layouts[i] = linkOption{l.value, l.label}
	}
	rows = append(rows, cfgRow{ID: "mmdvmDisplay", Cells: []cfgCell{
		ctlLabel("MMDVM Display Type:", "Display Type", "Choose your display type, if you have one."),
		ctlSpan(td(
			selectBox("mmdvmDisplayType", ctlDisplayType(c), ctlDisplayTypes),
			static("Port: "), selectBox("mmdvmDisplayPort", port, ports),
			static("Nextion Layout: "), selectBox("mmdvmNextionDisplayType", ctlNextionLayout(c), layouts),
		)),
	}})
	return &cfgSection{ID: "mmdvmhost", Title: "MMDVMHost Configuration", Rows: rows, Apply: true}
}

// ---------------------------------------------------------------------------
// General Configuration (l.4277-4486).

// ctlMHz is number_format(round(Hz/1000000, 6), 6, '.', ”).
func ctlMHz(hz string) string {
	n, err := strconv.ParseInt(strings.TrimSpace(hz), 10, 64)
	if err != nil {
		f, ferr := strconv.ParseFloat(strings.TrimSpace(hz), 64)
		if ferr != nil {
			f = 0
		}
		n = int64(math.Round(f))
	}
	sign := ""
	if n < 0 {
		sign, n = "-", -n
	}
	return fmt.Sprintf("%s%d.%06d", sign, n/1000000, n%1000000)
}

type ctlModem struct {
	value, label string
	cond         string      // "icom": /dev/icom_ta exists; "ttyS2": /dev/ttyS2 exists
	dsr          [][2]string // /etc/dstarrepeater keys
	port         string      // [Modem]Port, "" leaves it unchanged
	duplex       int         // -1 unchanged; 0: Duplex=0 + [DMR Network]Slot1=0; 1: Duplex=1
	fm           bool        // [FM]Enable=0 if present
	speed        string      // [Modem]UARTSpeed
	nxdnM17      bool        // [NXDN]/[M17] Enable=0 if present
	timer60      bool        // OnStartupSec=60 instead of 30
}

func ctlDVMega(port, variant string) [][2]string {
	return [][2]string{{"modemType", "DVMEGA"}, {"dvmegaPort", port}, {"dvmegaVariant", variant}}
}

func ctlMMDVMType(port string) [][2]string {
	if port == "" {
		return [][2]string{{"modemType", "MMDVM"}}
	}
	return [][2]string{{"modemType", "MMDVM"}, {"mmdvmPort", port}}
}

// ctlModems: option order of l.4344-4407 and the per-model effects of
// l.1782-2412 (spec 5.3 confHardware).
var ctlModems = []ctlModem{
	{value: "icomTerminalAuto", label: "Icom Radio in Terminal Mode (DStarRepeater Only)", cond: "icom", duplex: -1,
		dsr: [][2]string{{"modemType", "Icom Access Point/Terminal Mode"}, {"icomPort", "/dev/icom_ta"}, {"rpt1Validation", "0"}}},
	{value: "idrp2c", label: "Icom Repeater Controller ID-RP2C (DStarRepeater Only)", duplex: -1},
	{value: "dvmpis", label: "DV-Mega Raspberry Pi Hat (GPIO) - Single Band (70cm)", dsr: ctlDVMega("/dev/ttyAMA0", "2"), port: "/dev/ttyAMA0", duplex: 0, fm: true},
	{value: "dvmpid", label: "DV-Mega Raspberry Pi Hat (GPIO) - Dual Band", dsr: ctlDVMega("/dev/ttyAMA0", "3"), port: "/dev/ttyAMA0", duplex: 0, fm: true},
	{value: "dvmuadu", label: "DV-Mega on Arduino (USB - /dev/ttyUSB0) - Dual Band", dsr: ctlDVMega("/dev/ttyUSB0", "3"), port: "/dev/ttyUSB0", duplex: 0, fm: true},
	{value: "dvmuada", label: "DV-Mega on Arduino (USB - /dev/ttyACM0) - Dual Band", dsr: ctlDVMega("/dev/ttyACM0", "3"), port: "/dev/ttyACM0", duplex: 0, fm: true},
	{value: "dvmuagmsku", label: "DV-Mega on Arduino (USB - /dev/ttyUSB0) - GMSK Modem", dsr: ctlDVMega("/dev/ttyUSB0", "0"), port: "/dev/ttyUSB0", duplex: -1, fm: true},
	{value: "dvmuagmska", label: "DV-Mega on Arduino (USB - /dev/ttyACM0) - GMSK Modem", dsr: ctlDVMega("/dev/ttyACM0", "0"), port: "/dev/ttyACM0", duplex: -1, fm: true},
	{value: "dvmbss", label: "DV-Mega on Bluestack (USB) - Single Band (70cm)", dsr: ctlDVMega("/dev/ttyUSB0", "2"), port: "/dev/ttyUSB0", duplex: 0, fm: true, timer60: true},
	{value: "dvmbsd", label: "DV-Mega on Bluestack (USB) - Dual Band", dsr: ctlDVMega("/dev/ttyUSB0", "3"), port: "/dev/ttyUSB0", duplex: 0, fm: true, timer60: true},
	{value: "dvmpicast", label: "DV-Mega Cast Base Radio (Main Unit)", cond: "ttyS2", dsr: ctlDVMega("/dev/ttyAMA0", "2"), port: "/dev/ttyAMA0", duplex: 0, fm: true},
	{value: "dvmpicasths", label: "DV-Mega Cast Hotspot - Single Band (70cm)", cond: "ttyS2", dsr: ctlDVMega("/dev/ttyS2", "3"), port: "/dev/ttyS2", duplex: 0, fm: true},
	{value: "dvmpicasthd", label: "DV-Mega Cast Hotspot - Dual Band (2m/70cm)", cond: "ttyS2", dsr: ctlDVMega("/dev/ttyS2", "3"), port: "/dev/ttyS2", duplex: 0, fm: true},
	{value: "gmsk_modem", label: "GMSK Modem (USB DStarRepeater Only)", dsr: [][2]string{{"modemType", "GMSK Modem"}}, duplex: -1, fm: true},
	{value: "dvrptr1", label: "DV-RPTR V1 (USB)", dsr: [][2]string{{"modemType", "DV-RPTR V1"}, {"dvrptr1Port", "/dev/ttyACM0"}}, port: "/dev/ttyACM0", duplex: -1, fm: true},
	{value: "dvrptr2", label: "DV-RPTR V2 (USB)", dsr: [][2]string{{"modemType", "DV-RPTR V2"}, {"dvrptr1Port", "/dev/ttyACM0"}}, port: "/dev/ttyACM0", duplex: -1, fm: true},
	{value: "dvrptr3", label: "DV-RPTR V3 (USB)", dsr: [][2]string{{"modemType", "DV-RPTR V3"}, {"dvrptr1Port", "/dev/ttyACM0"}}, port: "/dev/ttyACM0", duplex: -1, fm: true},
	{value: "dvap", label: "DVAP (USB)", dsr: [][2]string{{"modemType", "DVAP"}}, port: "/dev/ttyUSB0", duplex: -1, fm: true},
	{value: "zum", label: "MMDVM / MMDVM_HS / Teensy / ZUM (USB)", dsr: ctlMMDVMType("/dev/ttyACM0"), port: "/dev/ttyACM0", duplex: -1},
	{value: "stm32dvm", label: "STM32-DVM / MMDVM_HS - Raspberry Pi Hat (GPIO)", dsr: ctlMMDVMType("/dev/ttyAMA0"), port: "/dev/ttyAMA0", duplex: -1},
	{value: "stm32usb", label: "STM32-DVM (USB)", dsr: ctlMMDVMType("/dev/ttyUSB0"), port: "/dev/ttyUSB0", duplex: -1},
	{value: "zumspotlibre", label: "ZUMspot - Libre (USB)", dsr: ctlMMDVMType(""), port: "/dev/ttyACM0", duplex: 0, fm: true},
	{value: "zumspotusb", label: "ZUMspot - USB Stick", dsr: ctlMMDVMType(""), port: "/dev/ttyACM0", duplex: 0, fm: true},
	{value: "zumspotgpio", label: "ZUMspot - Single Band Raspberry Pi Hat (GPIO)", dsr: ctlMMDVMType(""), port: "/dev/ttyAMA0", duplex: 0, fm: true},
	{value: "zumspotdualgpio", label: "ZUMspot - Dual Band Raspberry Pi Hat (GPIO)", dsr: ctlMMDVMType(""), port: "/dev/ttyAMA0", duplex: 0, fm: true},
	{value: "zumspotduplexgpio", label: "ZUMspot - Duplex Raspberry Pi Hat (GPIO)", dsr: ctlMMDVMType(""), port: "/dev/ttyAMA0", duplex: 1, fm: true},
	{value: "zumradiopigpio", label: "ZUM Radio-MMDVM for Pi (GPIO)", dsr: ctlMMDVMType("/dev/ttyAMA0"), port: "/dev/ttyAMA0", duplex: -1},
	{value: "zumradiopiusb", label: "ZUM Radio-MMDVM-Nucleo (USB)", dsr: ctlMMDVMType(""), port: "/dev/ttyACM0", duplex: 0},
	{value: "mnnano-spot", label: "MicroNode Nano-Spot (Built In)", dsr: ctlMMDVMType(""), port: "/dev/ttyAMA0", duplex: 0, fm: true},
	{value: "mnnano-teensy", label: "MicroNode Teensy (USB)", dsr: ctlMMDVMType("/dev/ttyUSB0"), port: "/dev/ttyUSB0", duplex: 0},
	{value: "f4mgpio", label: "MMDVM F4M-GPIO (GPIO)", dsr: ctlMMDVMType("/dev/ttyAMA0"), port: "/dev/ttyAMA0", duplex: -1},
	{value: "f4mf7m", label: "MMDVM F4M/F7M Duplex (F0DEI) for USB", dsr: ctlMMDVMType(""), port: "/dev/ttyUSB0", duplex: 1},
	{value: "mmdvmhsdualbandgpio", label: "MMDVM_HS_Dual_Band for Pi (GPIO)", dsr: ctlMMDVMType(""), port: "/dev/ttyAMA0", duplex: 0, fm: true},
	{value: "mmdvmhshat", label: "MMDVM_HS_Hat (DB9MAT & DF2ET) for Pi (GPIO)", dsr: ctlMMDVMType(""), port: "/dev/ttyAMA0", duplex: 0, fm: true},
	{value: "mmdvmhsdualhatgpio", label: "MMDVM_HS_Duplex_Hat (DB9MAT, DF2ET & DO7EN) for Pi (GPIO)", dsr: ctlMMDVMType(""), port: "/dev/ttyAMA0", duplex: 1, fm: true},
	{value: "mmdvmhsdualhatusb", label: "MMDVM_HS_Duplex_Hat (DB9MAT, DF2ET & DO7EN) for Pi (USB)", dsr: ctlMMDVMType(""), port: "/dev/ttyACM0", duplex: 1, fm: true},
	{value: "mmdvmhshatambe", label: "MMDVM_HS_AMBE (D2RG HS_AMBE) for Pi (GPIO)", dsr: ctlMMDVMType(""), port: "/dev/ttySC0", duplex: 0, fm: true},
	{value: "mmdvmrpthat", label: "MMDVM_RPT_Duplex_Hat (DB9MAT, DF2ET & F0DEI) for Pi (GPIO)", dsr: ctlMMDVMType(""), port: "/dev/ttyAMA0", duplex: 1},
	{value: "mmdvmmdohat", label: "MMDVM_HS_MDO Hat (BG3MDO) for Pi (GPIO)", dsr: ctlMMDVMType(""), port: "/dev/ttyAMA0", duplex: 0, fm: true},
	{value: "mmdvmvyehat", label: "MMDVM_HS_NPi Hat (VR2VYE) for Nano Pi (GPIO)", dsr: ctlMMDVMType(""), port: "/dev/ttyAMA0", duplex: 0, fm: true},
	{value: "mmdvmvyehatdual", label: "MMDVM_HS_Hat_Duplex Hat (VR2VYE) for Pi (GPIO)", dsr: ctlMMDVMType(""), port: "/dev/ttyAMA0", duplex: 1, fm: true},
	{value: "genesyshat", label: "Genesis - HHDVM_HS_Hat for Pi (GPIO)", dsr: ctlMMDVMType(""), port: "/dev/ttyAMA0", duplex: 0, fm: true},
	{value: "genesysdualhat", label: "Genesis Duplex - HHDVM_HS_Hat_Dual for Pi (GPIO)", dsr: ctlMMDVMType(""), port: "/dev/ttyAMA0", duplex: 1, fm: true},
	{value: "lshshatgpio", label: "LoneStar - MMDVM_HS_Hat for Pi (GPIO)", dsr: ctlMMDVMType(""), port: "/dev/ttyAMA0", duplex: 0},
	{value: "lshsdualhatgpio", label: "LoneStar - MMDVM_HS_Duplex_Hat for Pi (GPIO)", dsr: ctlMMDVMType(""), port: "/dev/ttyAMA0", duplex: 1, fm: true},
	{value: "lsusb", label: "LoneStar - USB Stick", dsr: ctlMMDVMType(""), port: "/dev/ttyACM0", duplex: 0, fm: true},
	{value: "sbhsdualbandgpio", label: "SkyBridge - MMDVM_HS_Dual_Band for Pi (GPIO)", dsr: ctlMMDVMType(""), port: "/dev/ttyAMA0", duplex: 0, fm: true},
	{value: "nanodv", label: "MMDVM_NANO_DV (BG4TGO) for NanoPi AIR (GPIO)", dsr: ctlMMDVMType(""), port: "/dev/ttyAMA0", duplex: 0, fm: true},
	{value: "nanodvusb", label: "MMDVM_NANO_DV (BG4TGO) for NanoPi AIR (USB)", dsr: ctlMMDVMType(""), port: "/dev/ttyACM0", duplex: 0},
	{value: "opengd77", label: "OpenGD77 DMR hotspot (USB)", dsr: ctlMMDVMType(""), port: "/dev/ttyACM0", duplex: 0, fm: true},
	{value: "rptrbldrv1v2", label: "Repeater Builder v1/v2 Simplex (GPIO)", dsr: ctlMMDVMType("/dev/ttyAMA0"), port: "/dev/ttyAMA0", duplex: 0, speed: "115200", nxdnM17: true, fm: true},
	{value: "rptrbldrv1v2dup", label: "Repeater Builder v1/v2 Duplex (GPIO)", dsr: ctlMMDVMType("/dev/ttyAMA0"), port: "/dev/ttyAMA0", duplex: 1, speed: "115200", nxdnM17: true, fm: true},
	{value: "rptrbldrv3v4v5", label: "Repeater Builder v3/v4/v5 Simplex 115200 baud (GPIO)", dsr: ctlMMDVMType("/dev/ttyAMA0"), port: "/dev/ttyAMA0", duplex: 0, speed: "115200"},
	{value: "rptrbldrv3v4v5dup", label: "Repeater Builder v3/v4/v5 Duplex 115200 baud (GPIO)", dsr: ctlMMDVMType("/dev/ttyAMA0"), port: "/dev/ttyAMA0", duplex: 1, speed: "115200"},
	{value: "rptrbldrv3v4v5hs", label: "Repeater Builder v3/v4/v5 Simplex 460800 baud (GPIO)", dsr: ctlMMDVMType("/dev/ttyAMA0"), port: "/dev/ttyAMA0", duplex: 0, speed: "460800"},
	{value: "rptrbldrv3v4v5hsdup", label: "Repeater Builder v3/v4/v5 Duplex 460800 baud (GPIO)", dsr: ctlMMDVMType("/dev/ttyAMA0"), port: "/dev/ttyAMA0", duplex: 1, speed: "460800"},
}

// ctlModemOrder is the display order of l.4347-4405, which differs from the
// handler order of ctlModems.
var ctlModemOrder = []string{
	"icomTerminalAuto", "idrp2c", "dvmpis", "dvmpid", "dvmuadu", "dvmuada", "dvmuagmsku", "dvmuagmska", "dvmbss", "dvmbsd",
	"dvmpicast", "dvmpicasths", "dvmpicasthd", "gmsk_modem", "dvrptr1", "dvrptr2", "dvrptr3", "dvap", "zum", "stm32dvm",
	"stm32usb", "zumspotlibre", "zumspotusb", "zumspotgpio", "zumspotdualgpio", "zumspotduplexgpio", "zumradiopigpio",
	"zumradiopiusb", "mnnano-spot", "mnnano-teensy", "f4mgpio", "f4mf7m", "mmdvmhsdualbandgpio", "mmdvmhshat",
	"mmdvmhsdualhatgpio", "mmdvmhsdualhatusb", "mmdvmhshatambe", "mmdvmrpthat", "mmdvmmdohat", "mmdvmvyehat",
	"mmdvmvyehatdual", "genesyshat", "genesysdualhat", "lshshatgpio", "lshsdualhatgpio", "lsusb", "sbhsdualbandgpio",
	"nanodv", "nanodvusb", "opengd77", "rptrbldrv1v2", "rptrbldrv1v2dup", "rptrbldrv3v4v5", "rptrbldrv3v4v5dup",
	"rptrbldrv3v4v5hs", "rptrbldrv3v4v5hsdup",
}

func ctlModemByValue(v string) (ctlModem, bool) {
	for _, m := range ctlModems {
		if m.value == v {
			return m, true
		}
	}
	return ctlModem{}, false
}

func ctlHardwareOptions() []linkOption {
	out := []linkOption{{"", "--"}}
	icom, s2 := ctlDevExists("icom_ta"), ctlDevExists("ttyS2")
	for _, v := range ctlModemOrder {
		m, _ := ctlModemByValue(v)
		if (m.cond == "icom" && !icom) || (m.cond == "ttyS2" && !s2) {
			continue
		}
		out = append(out, linkOption{m.value, m.label})
	}
	return out
}

func ctlHardwareValue(c *cfgContext, options []linkOption) string {
	hw := ctlModemHardware(c)
	if slices.Contains(ctlOptionValues(options), hw) {
		return hw
	}
	return ""
}

// ctlAPRSHosts reads APRSHosts.txt as l.4429-4445: column 0 of each
// "host:port" line, skipping empty names and names containing ";".
func ctlAPRSHosts() []linkOption {
	f, err := os.Open(ctlAPRSHostsFile())
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []linkOption
	seen := map[string]bool{}
	s := bufio.NewScanner(io.LimitReader(f, 1<<20))
	for s.Scan() {
		host := strings.TrimSpace(strings.SplitN(s.Text(), ":", 2)[0])
		if host == "" || strings.Contains(host, ";") || seen[host] {
			continue
		}
		seen[host] = true
		out = append(out, linkOption{host, host})
	}
	return out
}

// Deviation: a stored host or time zone missing from the list is kept as an
// extra option instead of silently switching to the first entry on Apply.
func ctlWithCurrent(options []linkOption, current string) []linkOption {
	if current != "" && !slices.Contains(ctlOptionValues(options), current) {
		options = append(options, linkOption{current, current})
	}
	return options
}

func ctlAPRSOptions(c *cfgContext) []linkOption {
	return ctlWithCurrent(ctlAPRSHosts(), c.get(ctlIRC, "", "aprsHostname"))
}

func ctlTimezoneOptions(c *cfgContext) []linkOption {
	var out []linkOption
	for _, z := range ctlTimezones() {
		out = append(out, linkOption{z, z})
	}
	return ctlWithCurrent(out, c.St.System.Timezone)
}

var ctlTZ struct {
	sync.Mutex
	list []string
}

// ctlListTimezones runs `timedatectl list-timezones` once (l.4446-4459) and
// appends UTC when it is missing.
func ctlListTimezones() []string {
	ctlTZ.Lock()
	defer ctlTZ.Unlock()
	if ctlTZ.list == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		out, err := runDiagnostic(ctx, "timedatectl", "list-timezones")
		cancel()
		if err == nil {
			list := []string{}
			for _, z := range strings.Split(string(out), "\n") {
				if z = strings.TrimSpace(z); z != "" {
					list = append(list, z)
				}
			}
			if len(list) > 0 {
				ctlTZ.list = list
			}
		}
	}
	list := append([]string{}, ctlTZ.list...)
	if !slices.Contains(list, "UTC") {
		list = append(list, "UTC")
	}
	return list
}

func ctlURLAuto(c *cfgContext) string {
	if strings.Contains(c.get(ctlIRC, "", "url"), "www.qrz.com/db/"+c.get(ctlMM, "General", "Callsign")) {
		return "auto"
	}
	return "man"
}

func ctlNodeMode(c *cfgContext) string {
	if c.get(ctlMM, "DMR", "SelfOnly") == "1" {
		return "prv"
	}
	if ctlPublic(c) {
		return "pub"
	}
	return ""
}

func buildCtlGeneral(c *cfgContext) *cfgSection {
	rows := []cfgRow{
		ctlHeader("The current value from the\nconfiguration files"),
		{ID: "confHostame", Cells: []cfgCell{
			ctlLabel("Hostname:", "System Hostname", "This is the system hostname, used for access to the dashboard etc."),
			ctlSpan(td(ctlText("confHostame", c.St.System.Hostname, "13", "15"), static("Do not add suffixes such as .local"))),
		}},
		{ID: "confCallsign", Cells: []cfgCell{
			ctlLabel("Node Callsign:", "Gateway Callsign", `This is your licenced callsign for use on this gateway, do not append the "G"`),
			ctlSpan(td(ctlText("confCallsign", ctlStoredCallsign(c), "13", "7"))),
		}},
	}
	if ctlDMRIdVisible(c) {
		id := c.get(ctlMM, "DMR", "Id")
		if c.ctlStored(ctlMM, "General", "Id") {
			id = c.get(ctlMM, "General", "Id")
		}
		rows = append(rows, cfgRow{ID: "dmrId", Cells: []cfgCell{
			ctlLabel("CCS7/DMR ID:", "CCS7/DMR ID", "Enter your CCS7 / DMR ID here"),
			ctlSpan(td(ctlText("dmrId", id, "13", "9"))),
		}})
	}
	if ctlNXDNIdVisible(c) {
		rows = append(rows, cfgRow{ID: "nxdnId", Cells: []cfgCell{
			ctlLabel("NXDN ID:", "NXDN ID", "Enter your NXDN ID here"),
			ctlSpan(td(ctlText("nxdnId", c.get(ctlMM, "NXDN", "Id"), "13", "5"))),
		}})
	}
	freq := func(name, hz string) cfgControl {
		t := ctlText(name, ctlMHz(hz), "13", "12")
		t.Attrs["id"] = name
		return t
	}
	if !ctlDuplex(c) {
		rows = append(rows, cfgRow{ID: "confFREQ", Cells: []cfgCell{
			ctlLabel("Radio Frequency:", "Radio Frequency", "This is the Frequency your\nPi-Star is on"),
			ctlSpan(td(freq("confFREQ", c.get(ctlMM, "Info", "RXFrequency")), static("MHz"))),
		}})
	} else {
		rows = append(rows,
			cfgRow{ID: "confFREQrx", Cells: []cfgCell{
				ctlLabel("Radio Frequency RX:", "Radio Frequency", "This is the Frequency your\nrepeater will listen on"),
				ctlSpan(td(freq("confFREQrx", c.get(ctlMM, "Info", "RXFrequency")), static("MHz"))),
			}},
			cfgRow{ID: "confFREQtx", Cells: []cfgCell{
				ctlLabel("Radio Frequency TX:", "Radio Frequency", "This is the Frequency your\nrepeater will transmit on"),
				ctlSpan(td(freq("confFREQtx", c.get(ctlMM, "Info", "TXFrequency")), static("MHz"))),
			}})
	}
	rows = append(rows,
		cfgRow{ID: "confLatitude", Cells: []cfgCell{
			ctlLabel("Latitude:", "Gateway Latitude", "This is the latitude where the gateway is located (positive number for North, negative number for South) - Set to 0 to hide your hotspot location"),
			ctlSpan(td(ctlText("confLatitude", c.get(ctlIRC, "", "latitude"), "13", "9"), static("degrees (positive value for North, negative for South)"))),
		}},
		cfgRow{ID: "confLongitude", Cells: []cfgCell{
			ctlLabel("Longitude:", "Gateway Longitude", "This is the longitude where the gateway is located (positive number for East, negative number for West) - Set to 0 to hide your hotspot location"),
			ctlSpan(td(ctlText("confLongitude", c.get(ctlIRC, "", "longitude"), "13", "9"), static("degrees (positive value for East, negative for West)"))),
		}},
		cfgRow{ID: "confDesc1", Cells: []cfgCell{
			ctlLabel("Town:", "Gateway Town", "The town where the gateway is located"),
			ctlSpan(td(ctlText("confDesc1", c.get(ctlIRC, "", "description1"), "30", "30"))),
		}},
		cfgRow{ID: "confDesc2", Cells: []cfgCell{
			ctlLabel("Country:", "Gateway Country", "The country where the gateway is located"),
			ctlSpan(td(ctlText("confDesc2", c.get(ctlIRC, "", "description2"), "30", "30"))),
		}},
		cfgRow{ID: "confURL", Cells: []cfgCell{
			ctlLabel("URL:", "Gateway URL", "The URL used to access this dashboard"),
			td(ctlText("confURL", c.get(ctlIRC, "", "url"), "30", "30")),
			{Style: "width: 300px;", Controls: []cfgControl{ctlRadio("urlAuto", ctlURLAuto(c), linkOption{"auto", "Auto"}, linkOption{"man", "Manual"})}},
		}},
	)
	if ctlHardwareVisible(c) {
		options := ctlHardwareOptions()
		rows = append(rows, cfgRow{ID: "confHardware", Cells: []cfgCell{
			ctlLabel("Radio/Modem Type:", "Radio/Modem", "What kind of radio or modem hardware do you have?"),
			ctlSpan(td(selectBox("confHardware", ctlHardwareValue(c, options), options))),
		}})
	}
	rows = append(rows, cfgRow{ID: "nodeMode", Cells: []cfgCell{
		ctlLabel("Node Type:", "Node Lock", `Set the public/private node type. "Private" limits access to your system to your ID/Callsign only, this may be a licence requirement for your country and helps prevent network loops.`),
		ctlSpan(td(ctlRadio("nodeMode", ctlNodeMode(c), linkOption{"prv", "Private"}, linkOption{"pub", "Public"}))),
	}})
	if ctlWhiteListVisible(c) {
		rows = append(rows, cfgRow{ID: "confDMRWhiteList", Cells: []cfgCell{
			ctlLabel("DMR Access List:", "DMR IDs", "Set the DMR IDs here that should have access to your hotspot, expected comma seperated list."),
			ctlSpan(td(ctlText("confDMRWhiteList", c.get(ctlMM, "DMR", "WhiteList"), "30", "50"))),
		}})
	}
	if c.has("aprsgateway") {
		rows = append(rows, cfgRow{ID: "APRSGatewayEnable", Cells: []cfgCell{
			ctlLabel("APRS Host Enable:", "APRS Host Enable", "Enabling this feature will make your location public."),
			ctlSpan(td(ctlToggle("APRSGatewayEnable", c.get("aprsgateway", "Enabled", "Enabled") == "1", "Enable APRS Position Reporting"))),
		}})
	}
	aprs := ctlAPRSOptions(c)
	aprsValue := c.get(ctlIRC, "", "aprsHostname")
	if aprsValue == "" && len(aprs) > 0 {
		aprsValue = aprs[0].Value
	}
	rows = append(rows,
		cfgRow{ID: "selectedAPRSHost", Cells: []cfgCell{
			ctlLabel("APRS Host:", "APRS Host", "Set your prefered APRS host here."),
			ctlSpan(td(selectBox("selectedAPRSHost", aprsValue, aprs))),
		}},
		cfgRow{ID: "systemTimezone", Cells: []cfgCell{
			ctlLabel("System Time Zone:", "System TimeZone", "Set the system timezone"),
			ctlSpan(td(selectBox("systemTimezone", c.St.System.Timezone, ctlTimezoneOptions(c)))),
		}},
	)
	// Dashboard Language (l.4460-4482) is not shown: the native dashboard has
	// no PHP language files (lang/*.php), the row's visibility condition.
	return &cfgSection{ID: "general", Title: "General Configuration", Rows: rows, Apply: true}
}

// ---------------------------------------------------------------------------
// Apply.

type ctlWriter struct {
	c *cfgContext
	p *cfgPlan
}

// set writes an INI key; missing files are never created.
func (w ctlWriter) set(file, section, key, value string) {
	if w.c.has(file) && !w.same(file, section, key, value) {
		w.p.set(file, section, key, value)
	}
}

// same reports a stored value that differs from value only by trailing
// blanks, which the helper strips when reading (e.g. dplusLogin is padded
// to 8 characters): rewriting it would change nothing.
func (w ctlWriter) same(file, section, key, value string) bool {
	stored, ok := w.c.St.Files[file][section][key]
	return ok && stored != value && stored == strings.TrimRight(value, " ")
}

// setIfKey is PHP's `if (isset($cfg[s][k])) $cfg[s][k] = v;`.
func (w ctlWriter) setIfKey(file, section, key, value string) {
	if w.c.ctlStored(file, section, key) {
		w.p.set(file, section, key, value)
	}
}

func (w ctlWriter) del(file, section, key string) {
	if w.c.ctlStored(file, section, key) {
		w.p.del(file, section, key)
	}
}

// flat is config_writer_stage_flat(): replace the first "key=" line of the
// file; a key that is not in the file is skipped, never appended.
func (w ctlWriter) flat(file, key, value string) {
	if !w.c.has(file) {
		return
	}
	for _, sec := range w.c.St.Layout[file] {
		for _, kv := range sec.Keys {
			if kv[0] == key {
				if !w.same(file, sec.Name, key, value) {
					w.p.set(file, sec.Name, key, value)
				}
				return
			}
		}
	}
	if w.c.St.Layout[file] == nil {
		sections := make([]string, 0, len(w.c.St.Files[file]))
		for s := range w.c.St.Files[file] {
			sections = append(sections, s)
		}
		sort.Strings(sections)
		for _, s := range sections {
			if w.c.ctlStored(file, s, key) {
				w.set(file, s, key, value)
				return
			}
		}
	}
}

// flatDel removes the first "key=" line of a flat file.
func (w ctlWriter) flatDel(file, key string) {
	for _, sec := range w.c.St.Layout[file] {
		for _, kv := range sec.Keys {
			if kv[0] == key {
				w.p.del(file, sec.Name, key)
				return
			}
		}
	}
	w.del(file, "", key)
}

func ctlErr(format string, args ...any) error { return cfgError(fmt.Sprintf(format, args...)) }

func ctlPost(f cfgForm, name string) string { return strings.TrimSpace(f[name]) }

// ctlChoice validates a posted select/radio value against its options.
func ctlChoice(f cfgForm, name, label string, allowed []string) (string, error) {
	v := ctlPost(f, name)
	if v == "" || slices.Contains(allowed, v) {
		return v, nil
	}
	return "", ctlErr("%s: unsupported value %q", label, v)
}

// ctlAPRSPass is aprspass() (l.220-237), the APRS-IS passcode.
func ctlAPRSPass(call string) string {
	if i := strings.Index(call, "-"); i > 0 {
		call = call[:i]
	}
	call = strings.ToUpper(call)
	if len(call) > 10 {
		call = call[:10]
	}
	hash := 0x73e2
	for i := 0; i < len(call); i += 2 {
		hash ^= int(call[i]) << 8
		if i+1 < len(call) {
			hash ^= int(call[i+1])
		}
	}
	return strconv.Itoa(hash & 0x7fff)
}

// ctlVersionAtLeast compares dotted versions numerically.
// Deviation: the reference compares strings (`>= "4.1.4"`, l.1197).
func ctlVersionAtLeast(v, min string) bool {
	if strings.TrimSpace(v) == "" {
		return false
	}
	a, b := strings.Split(strings.TrimSpace(v), "."), strings.Split(min, ".")
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y int
		if i < len(a) {
			x, _ = strconv.Atoi(a[i])
		}
		if i < len(b) {
			y, _ = strconv.Atoi(b[i])
		}
		if x != y {
			return x > y
		}
	}
	return true
}

var ctlFreqRe = regexp.MustCompile(`^(\d{1,5})(?:\.(\d{0,6}))?$`)

// ctlParseFreq turns MHz text into Hz. PHP strips everything but digits and
// dots first, so "438.8 MHz" is accepted.
// Deviation: the reference right-pads the digits to nine places, which
// multiplies frequencies below 100 MHz by ten (l.929-931, l.1038).
func ctlParseFreq(label, raw string) (int64, error) {
	s := strings.Map(func(r rune) rune {
		if (r >= '0' && r <= '9') || r == '.' {
			return r
		}
		return -1
	}, raw)
	m := ctlFreqRe.FindStringSubmatch(s)
	if m == nil {
		return 0, ctlErr("%s: %q is not a frequency in MHz", label, raw)
	}
	whole, _ := strconv.ParseInt(m[1], 10, 64)
	frac, _ := strconv.ParseInt((m[2] + "000000")[:6], 10, 64)
	hz := whole*1000000 + frac
	if hz <= 0 {
		return 0, ctlErr("%s: the frequency must be above 0 MHz", label)
	}
	return hz, nil
}

// ctlIRCFreq is number_format(MHz, 5, ".", "").
func ctlIRCFreq(hz int64) string {
	q := (hz + 5) / 10
	return fmt.Sprintf("%d.%05d", q/100000, q%100000)
}

// ctlOffset is number_format((rx-tx)/1000000, 4, '.', ”).
func ctlOffset(rx, tx int64) string {
	d := rx - tx
	sign := ""
	if d < 0 {
		sign, d = "-", -d
	}
	q := (d + 50) / 100
	if q == 0 {
		sign = ""
	}
	return fmt.Sprintf("%s%d.%04d", sign, q/10000, q%10000)
}

// ctlBand is the D-Star module chosen by TX frequency (l.972-997).
func ctlBand(tx int64) string {
	switch {
	case tx >= 1240000000 && tx <= 1300000000:
		return "A"
	case tx >= 420000000 && tx <= 450000000:
		return "B"
	case tx >= 218000000 && tx <= 226000000:
		return "A"
	case tx >= 144000000 && tx <= 148000000:
		return "C"
	}
	return ""
}

func ctlPad(s string, n int) string {
	if len(s) < n {
		return s + strings.Repeat(" ", n-len(s))
	}
	return s
}

var (
	ctlCallRe     = regexp.MustCompile(`^[A-Za-z0-9]{1,7}$`)
	ctlModuleRe   = regexp.MustCompile(`^[A-Z]$`)
	ctlHostRe     = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,13}[a-z0-9])?$`)
	ctlDigitsRe   = regexp.MustCompile(`^[0-9]+$`)
	ctlCoordRe    = regexp.MustCompile(`^-?[0-9]{1,3}(?:\.[0-9]*)?$`)
	ctlDescStrip  = regexp.MustCompile(`[^A-Za-z0-9.\s,\-]`)
	ctlURLStrip   = regexp.MustCompile(`[^A-Za-z0-9.\s,\-/:]`)
	ctlWhiteStrip = regexp.MustCompile(`[^0-9,]`)
)

// applyCtl runs the fields of the four sections in the reference's handler
// order (spec 7.2).
func applyCtl(c *cfgContext, f cfgForm, p *cfgPlan) error {
	w := ctlWriter{c, p}
	steps := []func(ctlWriter, cfgForm) error{
		ctlApplyController, ctlApplyLocation, ctlApplyAPRSHost, ctlApplyFrequency, ctlApplyCallsign,
		ctlApplyTRX, ctlApplyIDs, ctlApplyHangTimes, ctlApplyHardware, ctlApplyModes, ctlApplyDisplay,
		ctlApplyNode, ctlApplySystem,
	}
	for _, step := range steps {
		if err := step(w, f); err != nil {
			return err
		}
	}
	return nil
}

// controllerSoft (l.638-643). The reference deletes and re-creates the
// markers on every Apply; here the helper's "controller" operation runs only
// when the markers do not already match, and it keeps the marker contents
// (the modem selection) like the reference's write-back (spec 7.5).
func ctlApplyController(w ctlWriter, f cfgForm) error {
	v, err := ctlChoice(f, "controllerSoft", "Controller Software", []string{"DSTAR", "MMDVM"})
	if err != nil || v == "" {
		return err
	}
	want, other := "mmdvmhost", "dstarrepeater"
	if v == "DSTAR" {
		want, other = other, want
	}
	if !ctlHasMarker(w.c, want) || ctlHasMarker(w.c, other) {
		w.p.system("controller", want)
	}
	return nil
}

// confLatitude, confLongitude, confDesc1, confDesc2, confURL (l.734-814).
func ctlApplyLocation(w ctlWriter, f cfgForm) error {
	coord := func(name, label, flatKey, iniKey string, limit float64) error {
		v := ctlPost(f, name)
		if v == "" {
			return nil
		}
		n, err := strconv.ParseFloat(v, 64)
		if !ctlCoordRe.MatchString(v) || err != nil || math.Abs(n) > limit {
			return ctlErr("%s: %q is not a number between -%g and %g", label, v, limit, limit)
		}
		// Deviation: "0" is saved (PHP empty() skipped it, although the
		// tooltip says 0 hides the location).
		w.flat(ctlIRC, flatKey, v)
		w.flat(ctlIRC, flatKey+"1", v)
		for _, file := range []string{ctlMM, "ysfgateway", "ysf2dmr", "ysf2nxdn", "ysf2p25", "dgidgateway", "dmrgateway", "nxdngateway"} {
			w.set(file, "Info", iniKey, v)
		}
		w.setIfKey("m17gateway", "Info", iniKey, v)
		return nil
	}
	if err := coord("confLatitude", "Latitude", "latitude", "Latitude", 90); err != nil {
		return err
	}
	if err := coord("confLongitude", "Longitude", "longitude", "Longitude", 180); err != nil {
		return err
	}
	desc := func(name, label string) (string, error) {
		raw := ctlPost(f, name)
		if raw == "" {
			return "", nil
		}
		v := strings.TrimSpace(ctlDescStrip.ReplaceAllString(raw, ""))
		if v == "" || len(v) > 30 {
			return "", ctlErr("%s: use up to 30 letters, digits, spaces and . , - characters", label)
		}
		return v, nil
	}
	town, err := desc("confDesc1", "Town")
	if err != nil {
		return err
	}
	if town != "" {
		w.flat(ctlIRC, "description1", town)
		w.flat(ctlIRC, "description1_1", town)
		q := `"` + town + `"`
		for _, file := range []string{ctlMM, "dmrgateway", "ysf2dmr", "ysf2nxdn", "ysf2p25"} {
			w.set(file, "Info", "Location", q)
		}
		w.set("nxdngateway", "Info", "Name", q)
		w.setIfKey("m17gateway", "Info", "Name", q)
	}
	country, err := desc("confDesc2", "Country")
	if err != nil {
		return err
	}
	if country != "" {
		w.flat(ctlIRC, "description2", country)
		w.flat(ctlIRC, "description1_2", country)
		q := `"` + country + `"`
		for _, file := range []string{ctlMM, "dmrgateway", "ysfgateway", "nxdngateway"} {
			w.set(file, "Info", "Description", q)
		}
		w.setIfKey("m17gateway", "Info", "Description", q)
		w.set("dgidgateway", "Info", "Description", q)
	}
	auto, err := ctlChoice(f, "urlAuto", "URL", []string{"auto", "man"})
	if err != nil {
		return err
	}
	// Saved only when the text box is not empty, even with Auto (l.801).
	if raw := ctlPost(f, "confURL"); raw != "" {
		url := strings.ToLower(ctlURLStrip.ReplaceAllString(raw, ""))
		if auto == "auto" {
			url = "https://www.qrz.com/db/" + ctlCallsign(w, f)
		}
		if strings.TrimSpace(url) == "" {
			return ctlErr("URL: use letters, digits and . , - / : characters")
		}
		w.flat(ctlIRC, "url", url)
		for _, file := range []string{ctlMM, "ysf2dmr", "ysf2nxdn", "ysf2p25", "dmrgateway"} {
			w.set(file, "Info", "URL", url)
		}
	}
	return nil
}

// ctlStoredCallsign is the Node Callsign the page shows: ircDDBGateway's
// gatewayCallsign as in the reference, or MMDVMHost's [General] Callsign
// when /etc/ircddbgateway is missing (otherwise a saved callsign could never
// be shown again and looked unsaved).
func ctlStoredCallsign(c *cfgContext) string {
	if c.has(ctlIRC) {
		return c.get(ctlIRC, "", "gatewayCallsign")
	}
	return c.get(ctlMM, "General", "Callsign")
}

// ctlCallsign is the callsign used by the URL and RPT1/RPT2 rules: the
// posted one, or the stored one when the field is blank.
// Deviation: the reference uses the raw POST (escapeshellcmd only), so a
// blank field produced "       B" style RPT1 values.
func ctlCallsign(w ctlWriter, f cfgForm) string {
	if v := ctlPost(f, "confCallsign"); ctlCallRe.MatchString(v) {
		return strings.ToUpper(v)
	}
	return strings.ToUpper(ctlStoredCallsign(w.c))
}

// selectedAPRSHost (l.816-872).
// Deviation: an unknown host is refused with an error (the reference
// ignored it silently).
func ctlApplyAPRSHost(w ctlWriter, f cfgForm) error {
	host, err := ctlChoice(f, "selectedAPRSHost", "APRS Host", ctlOptionValues(ctlAPRSOptions(w.c)))
	if err != nil || host == "" {
		return err
	}
	w.flat(ctlIRC, "aprsHostname", host)
	for _, file := range []string{"ysfgateway", "ysf2dmr", "ysf2nxdn", "ysf2p25"} {
		w.set(file, "aprs.fi", "Server", host)
	}
	for _, file := range []string{"ysf2dmr", "ysf2nxdn", "ysf2p25"} {
		w.set(file, "aprs.fi", "Enable", "0")
	}
	if ctlVersionAtLeast(ctlPistarVersion(), "4.1.4") {
		w.flat("aprsgateway", "Server", host)
	}
	return nil
}

// confFREQ / confFREQrx + confFREQtx (l.881-1141).
func ctlApplyFrequency(w ctlWriter, f cfgForm) error {
	// A file without a stored frequency shows 0.000000; posting that display
	// back unchanged is not an edit and must not fail the whole form.
	unset := func(name, key string) bool {
		cur := w.c.get(ctlMM, "Info", key)
		n, err := strconv.ParseFloat(strings.TrimSpace(cur), 64)
		return (err != nil || n <= 0) && ctlPost(f, name) == ctlMHz(cur)
	}
	if unset("confFREQ", "RXFrequency") && ctlPost(f, "confFREQtx") == "" ||
		unset("confFREQrx", "RXFrequency") && unset("confFREQtx", "TXFrequency") {
		return nil
	}
	var rx, tx int64
	duplex := false
	switch {
	case ctlPost(f, "confFREQtx") != "" && ctlPost(f, "confFREQrx") != "":
		var err error
		if tx, err = ctlParseFreq("Radio Frequency TX", f["confFREQtx"]); err != nil {
			return err
		}
		if rx, err = ctlParseFreq("Radio Frequency RX", f["confFREQrx"]); err != nil {
			return err
		}
		duplex = true
	case ctlPost(f, "confFREQ") != "":
		var err error
		if tx, err = ctlParseFreq("Radio Frequency", f["confFREQ"]); err != nil {
			return err
		}
		rx = tx
	default:
		// Deviation: the reference clears timeserver sendA..sendE on every
		// Apply, even when no frequency was posted (l.881-886).
		return nil
	}
	suffix := strings.ToUpper(ctlPost(f, "confDStarModuleSuffix"))
	if suffix != "" && !ctlModuleRe.MatchString(suffix) {
		return ctlErr("D-Star module suffix must be a single letter A-Z")
	}
	for _, l := range "ABCDE" {
		w.flat("timeserver", "send"+string(l), "0")
	}
	RX, TX := strconv.FormatInt(rx, 10), strconv.FormatInt(tx, 10)
	mode, gwType, offset, ysfSuffix, m17Suffix := "1", "1", "0.0000", "ND", "H"
	if duplex {
		mode, gwType, offset, ysfSuffix, m17Suffix = "0", "0", ctlOffset(rx, tx), "RPT", "R"
	}
	w.flat(ctlIRC, "frequency1", ctlIRCFreq(tx))
	w.flat(ctlDSR, "dvapFrequency", RX)
	w.flat(ctlDSR, "dvmegaRXFrequency", RX)
	w.flat(ctlDSR, "dvmegaTXFrequency", TX)
	w.flat(ctlDSR, "mode", mode)
	w.flat(ctlIRC, "gatewayType", gwType)
	w.flat(ctlIRC, "offset1", offset)
	// Deviation: duplex ysf2dmr TXFrequency gets TX, not RX (l.953).
	for _, file := range []string{ctlMM, "dmrgateway", "ysfgateway", "ysf2dmr", "ysf2nxdn", "ysf2p25", "dgidgateway", "nxdngateway"} {
		w.set(file, "Info", "RXFrequency", RX)
		w.set(file, "Info", "TXFrequency", TX)
	}
	w.set("ysfgateway", "General", "Suffix", ysfSuffix)
	for _, file := range []string{"ysf2dmr", "ysf2nxdn", "ysf2p25", "dmr2ysf"} {
		w.set(file, "YSF Network", "Suffix", ysfSuffix)
	}
	w.set("dgidgateway", "General", "Suffix", ysfSuffix)
	w.set("nxdngateway", "General", "Suffix", ysfSuffix)
	w.setIfKey("m17gateway", "Info", "RXFrequency", RX)
	w.setIfKey("m17gateway", "Info", "TXFrequency", TX)
	w.setIfKey("m17gateway", "General", "Suffix", m17Suffix)

	call := ctlCallsign(w, f)
	module := suffix
	if module == "" {
		module = ctlBand(tx)
	}
	// Deviation: with an out-of-band TX frequency and no module suffix the
	// reference stages empty callsign/beaconText/repeaterBand1 (undefined
	// variables, l.972-997); here they are left unchanged.
	if module != "" {
		rpt1 := strings.ToUpper(ctlPad(call, 7) + module)
		w.flat("timeserver", "send"+module, "1")
		w.flat(ctlDSR, "callsign", rpt1)
		w.flat(ctlDSR, "beaconText", rpt1)
		w.flat(ctlIRC, "repeaterBand1", module)
		w.set(ctlMM, "D-Star", "Module", module)
	}
	w.flat(ctlDSR, "gateway", strings.ToUpper(ctlPad(call, 7)+"G"))
	w.flat(ctlIRC, "repeaterCall1", call)
	return nil
}

// confCallsign (l.1143-1240) and APRSGatewayEnable.
func ctlApplyCallsign(w ctlWriter, f cfgForm) error {
	// Deviation: the toggle is saved whenever it is shown; the reference
	// saved it only with a non-empty callsign on Pi-Star >= 4.1.4 (l.1230).
	if f.has("APRSGatewayEnable") && w.c.has("aprsgateway") {
		w.flat("aprsgateway", "Enabled", boolString(f.on("APRSGatewayEnable")))
	}
	raw := ctlPost(f, "confCallsign")
	if raw == "" {
		return nil
	}
	if !ctlCallRe.MatchString(raw) {
		return ctlErr("Node Callsign: use up to 7 letters and digits")
	}
	u := strings.ToUpper(raw)
	pass, desc := ctlAPRSPass(u), u+"_Pi-Star"
	w.flat(ctlIRC, "gatewayCallsign", u)
	w.flat(ctlIRC, "dplusLogin", ctlPad(u, 8))
	// config/ircddblocal.php ($callsign) does not exist in the native dashboard.
	w.flat("timeserver", "callsign", u)
	w.flat("starnetserver", "callsign", u)
	w.flat("starnetserver", "ircddbUsername", u)
	if strings.Contains(w.c.get(ctlIRC, "", "ircddbHostname"), "openquad.net") {
		w.flat(ctlIRC, "ircddbUsername", u)
	}
	w.set("ysfgateway", "General", "Callsign", u)
	w.set(ctlMM, "General", "Callsign", u)
	w.set("ysfgateway", "aprs.fi", "Password", pass)
	w.set("ysfgateway", "aprs.fi", "Description", desc)
	for _, file := range []string{"ysf2dmr", "ysf2nxdn", "ysf2p25"} {
		w.set(file, "aprs.fi", "Password", pass)
		w.set(file, "aprs.fi", "Description", desc)
		w.set(file, "YSF Network", "Callsign", u)
	}
	w.set("dmr2ysf", "YSF Network", "Callsign", u)
	w.set("p25gateway", "General", "Callsign", u)
	w.set("nxdngateway", "aprs.fi", "Description", desc)
	w.set("nxdngateway", "aprs.fi", "Password", pass)
	w.set("nxdngateway", "General", "Callsign", u)
	w.setIfKey("m17gateway", "General", "Callsign", u)
	w.set("ysfgateway", "Info", "Name", desc)
	for _, file := range []string{"ysf2dmr", "ysf2nxdn", "ysf2p25"} {
		w.set(file, "Info", "Description", desc)
	}
	w.set("dgidgateway", "General", "Callsign", u)
	w.set("dgidgateway", "Info", "Description", desc)
	if ctlVersionAtLeast(ctlPistarVersion(), "4.1.4") {
		w.flat("aprsgateway", "Callsign", u)
		w.flat("aprsgateway", "Password", pass)
		w.flat(ctlIRC, "aprsPort", "8673")
		// The reference deletes the aprsPassword= line (l.1214-1229).
		w.flatDel(ctlIRC, "aprsPassword")
	} else if w.c.get(ctlIRC, "", "aprsPassword") != "" {
		w.flat(ctlIRC, "aprsPassword", pass)
	}
	return nil
}

// trxMode (l.1448-1466), after the frequency handlers.
func ctlApplyTRX(w ctlWriter, f cfgForm) error {
	mode, err := ctlChoice(f, "trxMode", "Controller Mode", []string{"SIMPLEX", "DUPLEX"})
	if err != nil || mode == "" || !w.c.has(ctlMM) {
		return err
	}
	rx, tx := w.p.value(ctlMM, "Info", "RXFrequency"), w.p.value(ctlMM, "Info", "TXFrequency")
	if rx == tx && mode == "DUPLEX" {
		// Leaves the RX 1 Hz below TX so that the page shows both rows.
		if n, err := strconv.ParseInt(tx, 10, 64); err == nil {
			w.set(ctlMM, "Info", "RXFrequency", strconv.FormatInt(n-1, 10))
		}
	}
	if rx != tx && mode == "SIMPLEX" {
		w.set(ctlMM, "Info", "RXFrequency", tx)
	}
	if mode == "DUPLEX" {
		w.set(ctlMM, "General", "Duplex", "1")
		w.set(ctlMM, "DMR Network", "Slot1", "1")
	} else {
		w.set(ctlMM, "General", "Duplex", "0")
		w.set(ctlMM, "DMR Network", "Slot1", "0")
	}
	w.set(ctlMM, "DMR Network", "Slot2", "1")
	return nil
}

// dmrId (l.1468-1479) and nxdnId (l.1505-1510).
func ctlApplyIDs(w ctlWriter, f cfgForm) error {
	if v := ctlPost(f, "dmrId"); v != "" && ctlDMRIdVisible(w.c) {
		// Deviation: the reference truncates the ID to 7 digits although
		// the field accepts 9 (l.1471); the full ID is kept here.
		if !ctlDigitsRe.MatchString(v) || len(v) > 9 {
			return ctlErr("CCS7/DMR ID: use up to 9 digits")
		}
		w.set(ctlMM, "General", "Id", v)
		w.set(ctlMM, "DMR", "Id", v)
		w.set("ysfgateway", "General", "Id", v)
		w.set("dmrgateway", "XLX Network", "Id", v)
		w.set("dmr2ysf", "DMR Network", "Id", v)
		w.set("dmr2nxdn", "DMR Network", "Id", v)
		w.set("dgidgateway", "General", "Id", v)
	}
	if v := ctlPost(f, "nxdnId"); v != "" && ctlNXDNIdVisible(w.c) {
		// Deviation: an ID above 65535 is refused; the reference removed
		// the key from the file instead.
		n, err := strconv.Atoi(v)
		if !ctlDigitsRe.MatchString(v) || err != nil || n < 1 || n > 65535 {
			return ctlErr("NXDN ID: use a number from 1 to 65535")
		}
		w.set(ctlMM, "NXDN", "Id", v)
	}
	return nil
}

// Hang times (l.1717-1779), MMDVMHost section only.
func ctlApplyHangTimes(w ctlWriter, f cfgForm) error {
	if !ctlMMDVMVisible(w.c) {
		return nil
	}
	w.del(ctlMM, "General", "ModeHang")
	w.del("dmrgateway", "General", "Timeout")
	w.setIfKey(ctlMM, "General", "RFModeHang", "300")
	w.setIfKey(ctlMM, "General", "NetModeHang", "300")
	dgid := []string{"General", "YSF Network", "FCS Network", "IMRS Network"}
	for _, m := range ctlNativeModes {
		for i, name := range []string{m.rf, m.net} {
			v := ctlPost(f, name)
			if v == "" {
				continue
			}
			// Deviation: 0 is saved (PHP empty() skipped it).
			if !ctlDigitsRe.MatchString(v) || len(v) > 3 {
				return ctlErr("%s hang time: use 0 to 999 seconds", strings.TrimSuffix(m.title, " Mode"))
			}
			section := m.section
			if i == 1 {
				section += " Network"
			}
			w.set(ctlMM, section, "ModeHang", v)
			switch {
			case m.id == "dmr" && i == 0:
				w.set("dmrgateway", "General", "RFTimeout", v)
			case m.id == "dmr":
				w.set("dmrgateway", "General", "NetTimeout", v)
			case m.id == "ysf":
				key := "RFHangTime"
				if i == 1 {
					key = "NetHangTime"
				}
				for _, s := range dgid {
					w.set("dgidgateway", s, key, v)
				}
			case m.id == "p25" || m.id == "nxdn":
				// The gateways' own hang time is forced to 0 (l.1755-1771).
				file, key := "p25gateway", "RFHangTime"
				if m.id == "nxdn" {
					file = "nxdngateway"
				}
				if i == 1 {
					key = "NetHangTime"
				}
				w.set(file, "Network", key, "0")
			case m.id == "m17" && i == 1:
				w.setIfKey("m17gateway", "Network", "HangTime", v)
			}
		}
	}
	return nil
}

// confHardware (l.1781-2412).
func ctlApplyHardware(w ctlWriter, f cfgForm) error {
	if !ctlHardwareVisible(w.c) {
		return nil
	}
	hw, err := ctlChoice(f, "confHardware", "Radio/Modem Type", ctlOptionValues(ctlHardwareOptions()))
	if err != nil || hw == "" {
		return err
	}
	m, _ := ctlModemByValue(hw)
	if hw != ctlModemHardware(w.c) {
		w.p.system("modem", map[string]string{"Hardware": hw})
	}
	// Not ported (needs a helper operation): OnStartupSec=30|60 in the
	// dstarrepeater/mmdvmhost .timer units, and the eth0:1 alias appended to
	// /etc/network/interfaces for idrp2c.
	w.flat(ctlDSR, "rpt1Validation", "1")
	w.flat(ctlIRC, "repeaterAddress1", "127.0.0.1")
	w.flat(ctlIRC, "repeaterPort1", "20011")
	for _, kv := range m.dsr {
		w.flat(ctlDSR, kv[0], kv[1])
	}
	switch hw {
	case "idrp2c":
		w.flat(ctlIRC, "repeaterType1", "1")
		w.flat(ctlIRC, "repeaterAddress1", "172.16.0.1")
		w.flat(ctlIRC, "repeaterPort1", "20000")
	case "icomTerminalAuto":
	default:
		w.flat(ctlIRC, "repeaterType1", "0")
	}
	if !w.c.has(ctlMM) {
		return nil
	}
	if m.port != "" {
		w.set(ctlMM, "Modem", "Port", m.port)
	}
	switch m.duplex {
	case 0:
		w.set(ctlMM, "General", "Duplex", "0")
		w.set(ctlMM, "DMR Network", "Slot1", "0")
	case 1:
		w.set(ctlMM, "General", "Duplex", "1")
	}
	w.set(ctlMM, "Modem", "Protocol", "uart")
	w.set(ctlMM, "Modem", "UARTPort", w.p.value(ctlMM, "Modem", "Port"))
	if m.speed != "" {
		w.set(ctlMM, "Modem", "UARTSpeed", m.speed)
	}
	if m.nxdnM17 {
		w.setIfKey(ctlMM, "NXDN", "Enable", "0")
		w.setIfKey(ctlMM, "M17", "Enable", "0")
	}
	if m.fm {
		w.setIfKey(ctlMM, "FM", "Enable", "0")
	}
	return nil
}

// MMDVMMode* toggles (l.2460-2590), MMDVMHost section only, in the
// reference's order so that later toggles win.
func ctlApplyModes(w ctlWriter, f cfgForm) error {
	if !ctlMMDVMVisible(w.c) {
		return nil
	}
	shown := map[string]bool{}
	for _, m := range ctlNativeModes {
		shown[m.toggle] = true
	}
	for _, m := range ctlCrossModes {
		shown[m.toggle] = ctlCrossVisible(w.c, m)
	}
	// posted reports a rendered toggle's state; ok is false when the
	// toggle is hidden or absent. Deviation: hidden rows are not processed
	// (the reference posted hidden "OFF" defaults for them).
	posted := func(name string) (on, ok bool) {
		if !shown[name] || !f.has(name) {
			return false, false
		}
		v := ctlPost(f, name)
		if v != "ON" && v != "OFF" {
			return false, false
		}
		return v == "ON", true
	}
	for _, name := range []string{"MMDVMModeDMR", "MMDVMModeDSTAR", "MMDVMModeFUSION", "MMDVMModeP25", "MMDVMModeNXDN", "MMDVMModeM17",
		"MMDVMModeYSF2DMR", "MMDVMModeYSF2NXDN", "MMDVMModeYSF2P25", "MMDVMModeDMR2YSF", "MMDVMModeDMR2NXDN", "MMDVMModePOCSAG"} {
		if v := ctlPost(f, name); shown[name] && f.has(name) && v != "ON" && v != "OFF" {
			return ctlErr("%s: expected ON or OFF", name)
		}
	}
	native := func(name, section, crossFile string) {
		on, ok := posted(name)
		if !ok {
			return
		}
		w.set(ctlMM, section, "Enable", boolString(on))
		w.set(ctlMM, section+" Network", "Enable", boolString(on))
		if on && crossFile != "" {
			w.set(crossFile, "Enabled", "Enabled", "0")
		}
	}
	native("MMDVMModeDMR", "DMR", "ysf2dmr")
	native("MMDVMModeDSTAR", "D-Star", "")
	native("MMDVMModeFUSION", "System Fusion", "dmr2ysf")
	native("MMDVMModeP25", "P25", "ysf2p25")
	native("MMDVMModeNXDN", "NXDN", "ysf2nxdn")
	native("MMDVMModeM17", "M17", "")
	if on, ok := posted("MMDVMModeYSF2DMR"); ok {
		w.set("ysf2dmr", "Enabled", "Enabled", boolString(on))
	}
	if on, ok := posted("MMDVMModeYSF2NXDN"); ok {
		w.set("ysf2nxdn", "Enabled", "Enabled", boolString(on))
		if on {
			w.set(ctlMM, "NXDN", "Enable", "0")
			w.set(ctlMM, "NXDN Network", "Enable", "0")
		}
	}
	if on, ok := posted("MMDVMModeYSF2P25"); ok {
		w.set("ysf2p25", "Enabled", "Enabled", boolString(on))
		if on {
			w.set(ctlMM, "P25", "Enable", "0")
			w.set(ctlMM, "P25 Network", "Enable", "0")
		}
		if fusion, ok := posted("MMDVMModeFUSION"); ok && !fusion {
			w.set("ysf2p25", "Enabled", "Enabled", "0")
		}
	}
	// DMR Network 3 of DMRGateway is rebuilt for the cross-over (l.2516-2530).
	crossover := func(name, port, local string) {
		if !w.c.has("dmrgateway") {
			return
		}
		keys := [][2]string{
			{"Enabled", "0"}, {"Name", name}, {"Id", w.p.value("dmrgateway", "DMR Network 2", "Id")},
			{"Address", "127.0.0.1"}, {"Port", port}, {"Local", local},
			{"TGRewrite0", "2,7000001,2,1,999998"}, {"SrcRewrite0", "2,1,2,7000001,999998"}, {"PCRewrite0", "2,7000001,2,1,999998"},
			{"Password", `"PASSWORD"`}, {"Location", "0"}, {"Debug", "0"},
		}
		keep := map[string]bool{}
		for _, kv := range keys {
			keep[kv[0]] = true
		}
		for k := range w.c.St.Files["dmrgateway"]["DMR Network 3"] {
			if !keep[k] {
				w.p.del("dmrgateway", "DMR Network 3", k)
			}
		}
		for _, kv := range keys {
			w.p.set("dmrgateway", "DMR Network 3", kv[0], kv[1])
		}
	}
	d2y, d2yOK := posted("MMDVMModeDMR2YSF")
	if d2yOK {
		w.set("dmr2ysf", "Enabled", "Enabled", boolString(d2y))
		if d2y {
			crossover("DMR2YSF_Cross-over", "62033", "62034")
			w.set(ctlMM, "System Fusion", "Enable", "0")
			w.set(ctlMM, "System Fusion Network", "Enable", "0")
		} else {
			w.set("dmrgateway", "DMR Network 3", "Enabled", "0")
		}
	}
	d2n, d2nOK := posted("MMDVMModeDMR2NXDN")
	if d2nOK {
		if d2n {
			if d2yOK && d2y {
				w.set("dmr2ysf", "Enabled", "Enabled", "0")
			}
			if on, ok := posted("MMDVMModeYSF2NXDN"); ok && on {
				w.set("ysf2nxdn", "Enabled", "Enabled", "0")
			}
			w.set("dmr2nxdn", "Enabled", "Enabled", "1")
			crossover("DMR2NXDN_Cross-over", "62035", "62036")
			w.set(ctlMM, "NXDN", "Enable", "0")
			w.set(ctlMM, "NXDN Network", "Enable", "0")
		} else {
			w.set("dmr2nxdn", "Enabled", "Enabled", "0")
			w.set("dmrgateway", "DMR Network 3", "Enabled", "0")
		}
	}
	if d2yOK || d2nOK {
		w.set("dmrgateway", "DMR Network 3", "Enabled", boolString((d2yOK && d2y) || (d2nOK && d2n)))
	}
	if on, ok := posted("MMDVMModePOCSAG"); ok {
		w.set(ctlMM, "POCSAG", "Enable", boolString(on))
		w.set(ctlMM, "POCSAG Network", "Enable", boolString(on))
	}
	return nil
}

// mmdvmDisplayType / mmdvmDisplayPort / mmdvmNextionDisplayType
// (l.2592-2619) and the Nextion port rules of l.2823-2858.
func ctlApplyDisplay(w ctlWriter, f cfgForm) error {
	if !ctlMMDVMVisible(w.c) || !w.c.has(ctlMM) {
		return nil
	}
	t, err := ctlChoice(f, "mmdvmDisplayType", "MMDVM Display Type", ctlOptionValues(ctlDisplayTypes))
	if err != nil {
		return err
	}
	if strings.HasPrefix(t, "OLED") {
		w.set(ctlMM, "General", "Display", "OLED")
		w.set(ctlMM, "OLED", "Type", t[4:5])
		if t[4:5] == "6" {
			w.set(ctlMM, "OLED", "Scroll", "0")
		}
	} else if t != "" {
		w.set(ctlMM, "General", "Display", t)
	}
	ports, current := ctlDisplayPorts(w.c)
	port, err := ctlChoice(f, "mmdvmDisplayPort", "Display Port", ctlOptionValues(ports))
	if err != nil {
		return err
	}
	// The currently stored port is left as it is (see ctlDisplayPorts).
	if port != "" && port != current {
		if port == "None" || port == "modem" {
			w.set(ctlMM, "Nextion", "Port", port)
		} else {
			w.set(ctlMM, "Nextion", "Port", "/dev/"+port)
		}
	}
	layouts := []string{}
	for _, l := range ctlNextionLayouts {
		layouts = append(layouts, l.value)
	}
	layout, err := ctlChoice(f, "mmdvmNextionDisplayType", "Nextion Layout", layouts)
	if err != nil {
		return err
	}
	for _, l := range ctlNextionLayouts {
		if l.value == layout {
			w.set(ctlMM, "Nextion", "ScreenLayout", l.stored)
		}
	}
	// Nextion port normalisation. Not ported: creating a missing
	// [NextionDriver] section with defaults (part of the unconditional
	// normalisation, spec 7.3).
	if np := w.p.value(ctlMM, "Nextion", "Port"); np == "/dev/modem" {
		w.set(ctlMM, "Nextion", "Port", "modem")
	} else if _, driver := w.c.St.Files[ctlMM]["NextionDriver"]; driver && np != "" &&
		(np == w.p.value(ctlMM, "Modem", "Port") || np == w.p.value(ctlMM, "Modem", "UARTPort")) {
		w.set(ctlMM, "Nextion", "Port", "/dev/ttyNextionDriver")
	}
	return nil
}

// confDMRWhiteList (l.2626-2630) and nodeMode (l.2632-2650).
func ctlApplyNode(w ctlWriter, f cfgForm) error {
	// Deviation: the reference deletes the list on every Apply where the row
	// is hidden (l.2627); here it is only touched when shown.
	if ctlWhiteListVisible(w.c) && f.has("confDMRWhiteList") {
		v := ctlWhiteStrip.ReplaceAllString(ctlPost(f, "confDMRWhiteList"), "")
		if len(v) > 50 {
			return ctlErr("DMR Access List: at most 50 characters")
		}
		if v == "" {
			w.del(ctlMM, "DMR", "WhiteList")
		} else {
			w.set(ctlMM, "DMR", "WhiteList", v)
		}
	}
	mode, err := ctlChoice(f, "nodeMode", "Node Type", []string{"prv", "pub"})
	if err != nil || mode == "" {
		return err
	}
	v := "0"
	if mode == "prv" {
		v = "1"
	}
	for _, s := range []string{"DMR", "D-Star", "System Fusion", "P25", "NXDN"} {
		w.set(ctlMM, s, "SelfOnly", v)
	}
	w.flat(ctlDSR, "restriction", v)
	return nil
}

// confHostame (l.2652-2680) and systemTimezone (l.3862-3935): system
// operations of the root helper (hostnamectl + /etc/hosts + hostapd ssid,
// timedatectl). /etc/motd is not rewritten.
func ctlApplySystem(w ctlWriter, f cfgForm) error {
	if raw := ctlPost(f, "confHostame"); raw != "" {
		host := strings.ToLower(raw)
		if !ctlHostRe.MatchString(host) {
			return ctlErr("Hostname: use up to 15 letters, digits and hyphens, not starting or ending with a hyphen")
		}
		if host != w.c.St.System.Hostname {
			w.p.system("hostname", host)
		}
	}
	tz, err := ctlChoice(f, "systemTimezone", "System Time Zone", ctlOptionValues(ctlTimezoneOptions(w.c)))
	if err != nil {
		return err
	}
	if tz != "" && tz != w.c.St.System.Timezone {
		w.p.system("timezone", tz)
	}
	return nil
}
