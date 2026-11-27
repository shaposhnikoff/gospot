package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Configuration page: P25, NXDN, M17, POCSAG and Mobile GPS sections
// (admin/configure.php lines 5307-5579, POST handling 351-3944), as specified
// in docs/configure-spec/04-p25-nxdn-m17-pocsag-gps.md.
//
// Deliberate differences from the reference, all documented in the spec:
//   - A posted control whose value equals what the page showed is not written
//     again. The reference rewrites coupled keys (ysf2p25/ysf2nxdn
//     StartupDstId) on every Apply, which here would undo an edit made in the
//     YSF section of the same form, and refuses nothing.
//   - Input is validated and refused with an English message instead of being
//     passed through escapeshellcmd(), which persisted backslashes into the
//     files, or silently ignored.
//   - POCSAG MHz to Hz conversion is numeric; the reference text padding gave
//     wrong values below 100 MHz.
//   - WhiteList/BlackList are only touched while the POCSAG section is shown;
//     the reference deleted them on every Apply while POCSAG was disabled.
//   - Select options are de-duplicated (the page keys options by value).
//   - Version migrations of P25Gateway (> 20210201) and NXDNGateway
//     (> 20210131) are applied unconditionally: the shipped binaries
//     (Pi-Star_v4_Binaries_Bin, 2024 builds) are newer, so the startup
//     reflector is written to [Network] Static, as on a current Pi-Star.
//   - Gateway files are never created; DAPNETGateway settings need
//     /etc/dapnetgateway to exist (the reference created it).
//   - The per-file defaults run when the section that owns the file is shown;
//     the framework calls appliers of visible sections only.

func init() {
	registerCfgSection(modesP25Section)
	registerCfgSection(modesNXDNSection)
	registerCfgSection(modesM17Section)
	registerCfgSection(modesPOCSAGSection)
	registerCfgSection(modesGPSSection)
}

// modesDevDir is listed for GPS serial ports (ls /dev | egrep "ttyA|ttyUSB").
var modesDevDir = "/dev"

// --- shared helpers ----------------------------------------------------------

// modesStored returns the stored, unquoted value and whether the key exists (PHP isset).
func modesStored(c *cfgContext, file, section, key string) (string, bool) {
	v, ok := c.St.Files[file][section][key]
	return unquote(v), ok
}

// modesPresent is isset() after the changes planned so far.
func modesPresent(p *cfgPlan, file, section, key string) bool {
	if v, ok := p.change.Changes[file][section][key]; ok {
		return v != nil
	}
	_, ok := p.ctx.St.Files[file][section][key]
	return ok
}

// modesDefault adds a missing key to an installed file.
func modesDefault(p *cfgPlan, file, section, key, value string) {
	if p.ctx.has(file) && !modesPresent(p, file, section, key) {
		p.set(file, section, key, value)
	}
}

// modesForce sets a key of an installed file.
func modesForce(p *cfgPlan, file, section, key, value string) {
	if p.ctx.has(file) {
		p.set(file, section, key, value)
	}
}

// modesUnset removes a key of an installed file if it exists.
func modesUnset(p *cfgPlan, file, section, key string) {
	if p.ctx.has(file) && modesPresent(p, file, section, key) {
		p.del(file, section, key)
	}
}

// modesUnsetSection removes every stored and planned key of a section.
func modesUnsetSection(p *cfgPlan, file, section string) {
	keys := map[string]bool{}
	for k := range p.ctx.St.Files[file][section] {
		keys[k] = true
	}
	for k := range p.change.Changes[file][section] {
		keys[k] = true
	}
	for k := range keys {
		modesUnset(p, file, section, k)
	}
}

// modesIsOne is PHP's `$value == 1` on a parse_ini_file value (on/yes/true read as "1").
func modesIsOne(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "on", "yes", "true":
		return true
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	return err == nil && f == 1
}

// modesTruthy is PHP truthiness of a parse_ini_file value.
func modesTruthy(v string) bool {
	switch strings.ToLower(v) {
	case "", "0", "off", "no", "false", "none", "null":
		return false
	}
	return true
}

func modesMMDVMHost(c *cfgContext) bool {
	_, ok := c.marker("mmdvmhost")
	return ok
}

// modesEdited returns the posted value when the control was posted and
// differs from the value the page showed.
func modesEdited(form cfgForm, name, shown string) (string, bool) {
	v, ok := form[name]
	if !ok {
		return "", false
	}
	v = strings.TrimSpace(v)
	return v, v != shown
}

func modesHasOption(opts []linkOption, v string) bool {
	for _, o := range opts {
		if o.Value == v {
			return true
		}
	}
	return false
}

// modesDedupe keeps the first option of each value.
func modesDedupe(opts []linkOption) []linkOption {
	seen := map[string]bool{}
	out := opts[:0:0]
	for _, o := range opts {
		if !seen[o.Value] {
			seen[o.Value] = true
			out = append(out, o)
		}
	}
	return out
}

// modesHostOptions lists "{col0} - {col1}" options of host files in PISTAR_HOSTS_DIR.
func modesHostOptions(opts []linkOption, names ...string) []linkOption {
	for _, name := range names {
		for _, f := range hostsFile(name, "") {
			opts = append(opts, linkOption{Value: f[0], Label: f[0] + " - " + f[1]})
		}
	}
	return opts
}

func modesHeader() cfgRow {
	return cfgRow{Cells: []cfgCell{
		{Header: true, Style: "width: 200px;", Label: "Setting", Tooltip: tip("Setting", "")},
		{Header: true, Colspan: 2, Label: "Value", Tooltip: tip("Value", "The current value from the\nconfiguration files")},
	}}
}

func modesLabel(label string, t *cfgTooltip) cfgCell {
	return cfgCell{Align: "left", Label: label, Tooltip: t}
}

func modesStyled(controls ...cfgControl) cfgCell {
	return cfgCell{Style: "text-align: left;", Controls: controls}
}

func modesText(name, value, size, maxlength string) cfgControl {
	return text(name, value, map[string]string{"size": size, "maxlength": maxlength})
}

// modesStartupHost writes a P25/NXDN startup reflector to [Network] Static
// (Startup is migrated to Static on current gateways).
func modesStartupHost(p *cfgPlan, file, value string) {
	if strings.EqualFold(value, "none") {
		modesUnset(p, file, "Network", "Startup")
		modesUnset(p, file, "Network", "Static")
		return
	}
	modesForce(p, file, "Network", "Static", strings.ToUpper(value))
	modesUnset(p, file, "Network", "Startup")
}

// modesMigrateStartup moves [Network] Startup to Static.
func modesMigrateStartup(p *cfgPlan, file string) {
	if p.ctx.has(file) && modesPresent(p, file, "Network", "Startup") {
		p.set(file, "Network", "Static", p.value(file, "Network", "Startup"))
		p.del(file, "Network", "Startup")
	}
}

// --- P25 Configuration (5307-5359) -------------------------------------------

var modesNAC = regexp.MustCompile(`^[0-9a-f]{3}$`)

func modesP25Visible(c *cfgContext) bool {
	return modesMMDVMHost(c) && modesIsOne(c.get("mmdvmhost", "P25 Network", "Enable"))
}

func modesP25Host(c *cfgContext) (string, []linkOption) {
	cur := "none"
	if v, ok := modesStored(c, "p25gateway", "Network", "Startup"); ok {
		cur = v
	} else if v, ok := modesStored(c, "p25gateway", "Network", "Static"); ok {
		cur = v
	}
	if cur == "" {
		cur = "none"
	}
	opts := []linkOption{{Value: "none", Label: "None"}, {Value: "10", Label: "10 - Parrot"}}
	return cur, modesDedupe(modesHostOptions(opts, "P25Hosts.txt", "P25HostsLocal.txt"))
}

func modesP25NACShown(c *cfgContext) (string, bool) {
	v := c.get("mmdvmhost", "P25", "NAC")
	return v, modesTruthy(v)
}

var modesP25Section = cfgSectionImpl{ID: "p25", Order: 80,
	Build: func(c *cfgContext) *cfgSection {
		if !modesP25Visible(c) {
			return nil
		}
		host, opts := modesP25Host(c)
		rows := []cfgRow{modesHeader(), {Cells: []cfgCell{
			modesLabel("P25 Startup Host:", tip("P25 Host", "Set your prefered P25 Host here")),
			modesStyled(selectBox("p25StartupHost", host, opts)),
		}}}
		if nac, ok := modesP25NACShown(c); ok {
			rows = append(rows, cfgRow{Cells: []cfgCell{
				modesLabel("P25 NAC:", tip("P25 NAC", "Set your NAC code here")),
				td(modesText("p25nac", nac, "13", "3")),
			}})
		}
		return &cfgSection{ID: "p25", Title: "P25 Configuration", Rows: rows, Apply: true}
	},
	Apply: func(c *cfgContext, form cfgForm, p *cfgPlan) error {
		shown, opts := modesP25Host(c)
		if v, ok := modesEdited(form, "p25StartupHost", shown); ok {
			if !modesHasOption(opts, v) {
				return cfgError("P25 Startup Host: unknown host " + strconv.Quote(v))
			}
			modesStartupHost(p, "p25gateway", v)
			if strings.EqualFold(v, "none") {
				modesUnset(p, "ysf2p25", "P25 Network", "StartupDstId")
			} else {
				modesForce(p, "ysf2p25", "P25 Network", "StartupDstId", strings.ToUpper(v))
			}
		}
		if nac, ok := modesP25NACShown(c); ok {
			if v, ok := modesEdited(form, "p25nac", nac); ok && v != "" {
				v = strings.ToLower(v)
				if !modesNAC.MatchString(v) {
					return cfgError("P25 NAC must be 3 hexadecimal digits")
				}
				p.set("mmdvmhost", "P25", "NAC", v)
			}
		}
		// P25Gateway options added on every save (3084-3100).
		const gw = "p25gateway"
		modesDefault(p, gw, "Remote Commands", "Enable", "1")
		modesDefault(p, gw, "Remote Commands", "Port", "6074")
		modesUnset(p, gw, "General", "Announcements")
		modesDefault(p, gw, "Log", "DisplayLevel", "1")
		modesDefault(p, gw, "Log", "FileLevel", "1")
		modesDefault(p, gw, "Network", "P252DMRAddress", "127.0.0.1")
		modesDefault(p, gw, "Network", "P252DMRPort", "42012")
		modesMigrateStartup(p, gw)
		return nil
	},
}

// --- NXDN Configuration (5361-5416) ------------------------------------------

// Without NXDNGateway MMDVMHost talks to this host directly (5402-5404, 1289-1304).
const modesNXDNDirectHost = "176.9.1.168"

func modesNXDNVisible(c *cfgContext) bool {
	return modesMMDVMHost(c) && (modesIsOne(c.get("mmdvmhost", "NXDN Network", "Enable")) ||
		modesIsOne(c.get("dmr2nxdn", "Enabled", "Enabled")))
}

func modesNXDNHost(c *cfgContext) (string, []linkOption) {
	if !c.has("nxdngateway") {
		return modesNXDNDirectHost, []linkOption{{Value: modesNXDNDirectHost, Label: "D2FET Test Host - " + modesNXDNDirectHost}}
	}
	cur := ""
	if v, ok := modesStored(c, "nxdngateway", "Network", "Startup"); ok {
		cur = v
	} else if v, ok := modesStored(c, "nxdngateway", "Network", "Static"); ok {
		cur = v
	}
	if cur == "" {
		cur = "none"
	}
	opts := []linkOption{{Value: "none", Label: "None"}, {Value: "10", Label: "10 - Parrot"}}
	return cur, modesDedupe(modesHostOptions(opts, "NXDNHosts.txt", "NXDNHostsLocal.txt"))
}

func modesNXDNRANShown(c *cfgContext) (string, bool) {
	v := c.get("mmdvmhost", "NXDN", "RAN")
	return v, modesTruthy(v)
}

var modesNXDNSection = cfgSectionImpl{ID: "nxdn", Order: 90,
	Build: func(c *cfgContext) *cfgSection {
		if !modesNXDNVisible(c) {
			return nil
		}
		host, opts := modesNXDNHost(c)
		rows := []cfgRow{modesHeader(), {Cells: []cfgCell{
			modesLabel("NXDN Startup Host:", tip("NXDN Host", "Set your prefered NXDN Host here")),
			modesStyled(selectBox("nxdnStartupHost", host, opts)),
		}}}
		if ran, ok := modesNXDNRANShown(c); ok {
			rows = append(rows, cfgRow{Cells: []cfgCell{
				modesLabel("NXDN RAN:", tip("NXDN RAN", "Set your RAN code here, sane values are 1-64")),
				td(modesText("nxdnran", ran, "13", "2")),
			}})
		}
		return &cfgSection{ID: "nxdn", Title: "NXDN Configuration", Rows: rows, Apply: true}
	},
	Apply: func(c *cfgContext, form cfgForm, p *cfgPlan) error {
		const gw = "nxdngateway"
		shown, opts := modesNXDNHost(c)
		if !c.has(gw) {
			// The only option names the host MMDVMHost is pointed at on every save.
			if form.has("nxdnStartupHost") {
				if v := strings.TrimSpace(form["nxdnStartupHost"]); v != modesNXDNDirectHost {
					return cfgError("NXDN Startup Host: unknown host " + strconv.Quote(v))
				}
				p.set("mmdvmhost", "NXDN Network", "GatewayAddress", modesNXDNDirectHost)
				p.set("mmdvmhost", "NXDN Network", "GatewayPort", "41007")
				// The reference also wrote the IP address into ysf2nxdn StartupDstId,
				// a talkgroup number; that defect is not copied.
			}
		} else if v, ok := modesEdited(form, "nxdnStartupHost", shown); ok {
			if !modesHasOption(opts, v) {
				return cfgError("NXDN Startup Host: unknown host " + strconv.Quote(v))
			}
			modesStartupHost(p, gw, v)
			// Written for None too: "NONE", as the reference does (YSF2NXDN reads it as 0).
			modesForce(p, "ysf2nxdn", "NXDN Network", "StartupDstId", strings.ToUpper(v))
		}
		if ran, ok := modesNXDNRANShown(c); ok {
			if v, ok := modesEdited(form, "nxdnran", ran); ok && v != "" {
				n, err := strconv.Atoi(v)
				if err != nil || len(v) > 2 || n < 1 || n > 64 {
					return cfgError("NXDN RAN must be a number from 1 to 64")
				}
				p.set("mmdvmhost", "NXDN", "RAN", v)
			}
		}
		if !c.has(gw) {
			return nil
		}
		// Clean up for NXDNGateway (3039-3075). Re-ordering [NXDN] SelfOnly and
		// ModeHang to the end of their sections is cosmetic and not copied.
		if modesPresent(p, gw, "Network", "HostsFile") {
			p.set(gw, "Network", "HostsFile1", p.value(gw, "Network", "HostsFile"))
			p.set(gw, "Network", "HostsFile2", filepath.Join(pistarHostsDir, "NXDNHostsLocal.txt"))
			p.del(gw, "Network", "HostsFile")
		}
		modesForce(p, "mmdvmhost", "NXDN Network", "LocalAddress", "127.0.0.1")
		modesForce(p, "mmdvmhost", "NXDN Network", "LocalPort", "14021")
		modesForce(p, "mmdvmhost", "NXDN Network", "GatewayAddress", "127.0.0.1")
		modesForce(p, "mmdvmhost", "NXDN Network", "GatewayPort", "14020")
		modesDefault(p, gw, "Info", "Power", "1")
		modesDefault(p, gw, "Info", "Height", "0")
		// NXDNGateway options (3102-3127); [aprs.fi] defaults (3067-3074) are
		// added and then removed with the section, so only the removal remains.
		modesDefault(p, gw, "Remote Commands", "Enable", "1")
		modesDefault(p, gw, "Remote Commands", "Port", "6075")
		modesUnsetSection(p, gw, "aprs.fi")
		modesUnsetSection(p, gw, "Mobile GPS")
		modesDefault(p, gw, "General", "RptProtocol", "Icom")
		modesDefault(p, gw, "Log", "DisplayLevel", "1")
		modesDefault(p, gw, "Log", "FileLevel", "1")
		modesDefault(p, gw, "APRS", "Enable", "1")
		modesDefault(p, gw, "APRS", "Address", "127.0.0.1")
		modesDefault(p, gw, "APRS", "Port", "8673")
		modesDefault(p, gw, "APRS", "Suffix", "N")
		if c.has("ysfgateway") {
			p.set(gw, "APRS", "Description", p.value("ysfgateway", "Info", "Name")+"_"+p.value("ysfgateway", "General", "Suffix"))
		}
		modesDefault(p, gw, "GPSD", "Enable", "0")
		modesDefault(p, gw, "GPSD", "Address", "127.0.0.1")
		modesDefault(p, gw, "GPSD", "Port", "2947")
		modesMigrateStartup(p, gw)
		return nil
	},
}

// --- M17 Configuration (5418-5495) -------------------------------------------

func modesM17Visible(c *cfgContext) bool {
	return modesMMDVMHost(c) && modesIsOne(c.get("mmdvmhost", "M17 Network", "Enable"))
}

// modesM17Rooms lists the module letters; a stored module outside A-Z comes first.
func modesM17Rooms(current string) []linkOption {
	var opts []linkOption
	if current < "A" || current > "Z" || len(current) != 1 {
		opts = append(opts, linkOption{Value: current, Label: current})
	}
	for r := 'A'; r <= 'Z'; r++ {
		opts = append(opts, linkOption{Value: string(r), Label: string(r)})
	}
	return opts
}

// modesM17Startup returns the shown reflector and module and their options.
func modesM17Startup(c *cfgContext) (host string, hosts []linkOption, room string, rooms []linkOption) {
	startup, set := modesStored(c, "m17gateway", "Network", "Startup")
	room = "A"
	if set {
		room = ""
		if startup != "" {
			room = startup[len(startup)-1:]
		}
	}
	if !c.has("m17gateway") {
		// The reference renders an empty reflector select.
		return "", nil, room, modesM17Rooms(room)
	}
	host = "none"
	if set {
		if h, _, _ := strings.Cut(startup, "_"); h != "" {
			host = h
		}
	}
	// M17Hosts.txt; the reference also read /root/M17Hosts.txt, which has no
	// pistar.conf setting yet.
	hosts = []linkOption{{Value: "none", Label: "None"}}
	for _, f := range hostsFile("M17Hosts.txt", "") {
		hosts = append(hosts, linkOption{Value: f[0], Label: f[0]})
	}
	return host, modesDedupe(hosts), room, modesM17Rooms(room)
}

var modesM17Section = cfgSectionImpl{ID: "m17", Order: 100,
	Build: func(c *cfgContext) *cfgSection {
		if !modesM17Visible(c) {
			return nil
		}
		host, hosts, room, rooms := modesM17Startup(c)
		rows := []cfgRow{modesHeader(),
			{Cells: []cfgCell{
				modesLabel("M17 Startup Host:", tip("M17 Host", "Set your prefered M17 Host here")),
				modesStyled(selectBox("m17StartupHost", host, hosts), selectBox("m17StartupRoom", room, rooms)),
			}},
			{Cells: []cfgCell{
				modesLabel("M17 CAN:", tip("M17 CAN", "Set your CAN code here, sane values are 0-64")),
				td(modesText("m17can", c.get("mmdvmhost", "M17", "CAN"), "13", "2")),
			}},
		}
		return &cfgSection{ID: "m17", Title: "M17 Configuration", Rows: rows, Apply: true}
	},
	Apply: func(c *cfgContext, form cfgForm, p *cfgPlan) error {
		const gw = "m17gateway"
		host, hosts, room, rooms := modesM17Startup(c)
		newHost, hostEdited := modesEdited(form, "m17StartupHost", host)
		newRoom, roomEdited := modesEdited(form, "m17StartupRoom", room)
		if !form.has("m17StartupHost") {
			newHost = host
		}
		if !form.has("m17StartupRoom") {
			newRoom = room
		}
		if c.has(gw) && (hostEdited || roomEdited) {
			if !modesHasOption(hosts, newHost) {
				return cfgError("M17 Startup Host: unknown reflector " + strconv.Quote(newHost))
			}
			if !modesHasOption(rooms, newRoom) {
				return cfgError("M17 Startup Host: unknown module " + strconv.Quote(newRoom))
			}
			if strings.EqualFold(newHost, "none") {
				modesUnset(p, gw, "Network", "Startup")
			} else {
				if newRoom == "" {
					return cfgError("M17 Startup Host: choose a module")
				}
				p.set(gw, "Network", "Startup", strings.ToUpper(newHost)+"_"+strings.ToUpper(newRoom))
			}
		}
		if v, ok := modesEdited(form, "m17can", c.get("mmdvmhost", "M17", "CAN")); ok && v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || len(v) > 2 || n < 0 || n > 64 {
				return cfgError("M17 CAN must be a number from 0 to 64")
			}
			p.set("mmdvmhost", "M17", "CAN", v)
		}
		// Fix daemon mode on M17Gateway (3204-3205).
		if modesPresent(p, gw, "General", "Daemon") {
			p.set(gw, "General", "Daemon", "1")
		}
		return nil
	},
}

// --- POCSAG Configuration (5497-5536) ----------------------------------------

var (
	modesPOCSAGServers = []linkOption{
		{Value: "dapnet.afu.rwth-aachen.de", Label: "dapnet.afu.rwth-aachen.de"},
		{Value: "db0dbn.ig-funk-siebengebirge.de", Label: "db0dbn.ig-funk-siebengebirge.de"},
		{Value: "dapnet.db0sda.ampr.org", Label: "dapnet.db0sda.ampr.org (HAMNET)"},
		{Value: "node1.dapnet-italia.it", Label: "node1.dapnet-italia.it"},
	}
	modesFreqMHz  = regexp.MustCompile(`^[0-9]{1,4}(\.[0-9]+)?$`)
	modesCallsign = regexp.MustCompile(`^[A-Z0-9/-]{1,12}$`)
	modesAuthKey  = regexp.MustCompile(`^[\x21-\x7e]{1,50}$`)
	modesRICList  = regexp.MustCompile(`^[0-9]+(,[0-9]+)*$`)
)

func modesPOCSAGVisible(c *cfgContext) bool {
	return modesIsOne(c.get("mmdvmhost", "POCSAG", "Enable"))
}

func modesPOCSAGServer(c *cfgContext) (string, []linkOption) {
	cur := c.get("dapnetgateway", "DAPNET", "Address")
	return cur, modesDedupe(append([]linkOption{{Value: cur, Label: cur}}, modesPOCSAGServers...))
}

// modesPOCSAGFreq shows [POCSAG] Frequency (Hz) in MHz:
// number_format(round(Frequency/1000000, 6), 6, '.', ”).
func modesPOCSAGFreq(c *cfgContext) string {
	hz, _ := strconv.ParseFloat(strings.TrimSpace(c.get("mmdvmhost", "POCSAG", "Frequency")), 64)
	return fmt.Sprintf("%.6f", math.Round(hz)/1e6)
}

// modesPOCSAGFreqColour is functions.js checkFrequency() for the POCSAG box,
// run on page load.
func modesPOCSAGFreqColour(mhz string) string {
	f, err := strconv.ParseFloat(mhz, 64)
	good := err == nil && ((144 <= f && f <= 148) || (220 <= f && f <= 225) || (420 <= f && f <= 450) || (842 <= f && f <= 950)) &&
		!(145.8 <= f && f <= 146) && !(435 <= f && f <= 438)
	if good {
		return "background-color: #66cc66;"
	}
	return "background-color: #ff6666;"
}

var modesPOCSAGSection = cfgSectionImpl{ID: "pocsag", Order: 110,
	Build: func(c *cfgContext) *cfgSection {
		if !modesPOCSAGVisible(c) {
			return nil
		}
		server, servers := modesPOCSAGServer(c)
		freq := modesPOCSAGFreq(c)
		freqBox := modesText("pocsagFrequency", freq, "13", "12")
		freqBox.Attrs["style"] = modesPOCSAGFreqColour(freq)
		row := func(label string, t *cfgTooltip, cell cfgCell) cfgRow {
			return cfgRow{Cells: []cfgCell{modesLabel(label, t), cell}}
		}
		rows := []cfgRow{modesHeader(),
			row("POCSAG Server:", tip("POCSAG Server", "Set the POCSAG Network here"),
				modesStyled(selectBox("pocsagServer", server, servers))),
			row("POCSAG Node Callsign:", tip("POCSAG Callsign", "Set your paging callsign here"),
				td(modesText("pocsagCallsign", c.get("dapnetgateway", "General", "Callsign"), "13", "12"))),
			row("POCSAG Radio Frequency:", tip("POCSAG Frequency", "Set your paging frequency here"), td(freqBox)),
			row("DAPNET AuthKey:", tip("DAPNET AuthKey", "Set your DAPNET AuthKey here"),
				td(cfgControl{Kind: "password", Name: "pocsagAuthKey", Value: c.get("dapnetgateway", "DAPNET", "AuthKey"),
					Attrs: map[string]string{"size": "30", "maxlength": "50"}})),
			row("POCSAG Whitelist:", tip("POCSAG Whitelist", "Set your POCSAG RIC Whitelist here, if these are set ONLY these RICs will be transmitted. List is comma seperated."),
				td(modesText("pocsagWhitelist", c.get("dapnetgateway", "General", "WhiteList"), "60", "350"))),
			row("POCSAG Blacklist:", tip("POCSAG Blacklist", "Set your POCSAG RIC Blacklist here, if these are set any other RIC will be transmitted, but not these. List is comma seperated."),
				td(modesText("pocsagBlacklist", c.get("dapnetgateway", "General", "BlackList"), "60", "350"))),
		}
		return &cfgSection{ID: "pocsag", Title: "POCSAG Configuration", Rows: rows, Apply: true}
	},
	Apply: func(c *cfgContext, form cfgForm, p *cfgPlan) error {
		const gw = "dapnetgateway"
		dapnet := func(section, key, value string) error {
			if !c.has(gw) {
				return cfgError("DAPNETGateway is not installed; its settings cannot be saved")
			}
			p.set(gw, section, key, value)
			return nil
		}
		server, servers := modesPOCSAGServer(c)
		if v, ok := modesEdited(form, "pocsagServer", server); ok && v != "" {
			if !modesHasOption(servers, v) {
				return cfgError("POCSAG Server: unknown server " + strconv.Quote(v))
			}
			if err := dapnet("DAPNET", "Address", v); err != nil {
				return err
			}
		}
		if v, ok := modesEdited(form, "pocsagCallsign", c.get(gw, "General", "Callsign")); ok && v != "" {
			v = strings.ToUpper(v)
			if !modesCallsign.MatchString(v) {
				return cfgError("POCSAG Node Callsign may contain only letters, digits, / and - (up to 12)")
			}
			if err := dapnet("General", "Callsign", v); err != nil {
				return err
			}
		}
		if v, ok := modesEdited(form, "pocsagFrequency", modesPOCSAGFreq(c)); ok && v != "" {
			f, err := strconv.ParseFloat(v, 64)
			hz := int64(math.Round(f * 1e6))
			if !modesFreqMHz.MatchString(v) || err != nil || hz <= 0 {
				return cfgError("POCSAG Radio Frequency must be given in MHz, for example 439.987500")
			}
			p.set("mmdvmhost", "POCSAG", "Frequency", strconv.FormatInt(hz, 10))
		}
		if v, ok := modesEdited(form, "pocsagAuthKey", c.get(gw, "DAPNET", "AuthKey")); ok && v != "" {
			if !modesAuthKey.MatchString(v) {
				return cfgError("DAPNET AuthKey must be up to 50 printable characters without spaces")
			}
			if err := dapnet("DAPNET", "AuthKey", v); err != nil {
				return err
			}
		}
		for _, l := range []struct{ name, key, label string }{
			{"pocsagWhitelist", "WhiteList", "POCSAG Whitelist"},
			{"pocsagBlacklist", "BlackList", "POCSAG Blacklist"},
		} {
			v, ok := modesEdited(form, l.name, c.get(gw, "General", l.key))
			if !ok {
				continue
			}
			v = strings.Join(strings.Fields(v), "")
			if v == "" {
				if !c.has(gw) {
					return cfgError("DAPNETGateway is not installed; its settings cannot be saved")
				}
				modesUnset(p, gw, "General", l.key)
				continue
			}
			if !modesRICList.MatchString(v) {
				return cfgError(l.label + " must be a comma separated list of RIC numbers")
			}
			if err := dapnet("General", l.key, v); err != nil {
				return err
			}
		}
		// DAPNET and POCSAG defaults (3177-3202).
		for _, d := range [][3]string{
			{"General", "Callsign", "M1ABC"}, {"General", "RptAddress", "127.0.0.1"}, {"General", "RptPort", "3800"},
			{"General", "LocalAddress", "127.0.0.1"}, {"General", "LocalPort", "4800"}, {"General", "Daemon", "0"},
			{"Log", "DisplayLevel", "0"}, {"Log", "FileLevel", "2"}, {"Log", "FilePath", pistarLogDir},
			{"Log", "FileRoot", "DAPNETGateway"},
			{"DAPNET", "Address", "dapnet.afu.rwth-aachen.de"}, {"DAPNET", "Port", "43434"}, {"DAPNET", "AuthKey", "TOPSECRET"},
			{"DAPNET", "SuppressTimeWhenBusy", "1"}, {"DAPNET", "Debug", "0"},
		} {
			modesDefault(p, gw, d[0], d[1], d[2])
		}
		for _, d := range [][3]string{
			{"POCSAG", "Enable", "0"}, {"POCSAG", "Frequency", "439987500"},
			{"POCSAG Network", "Enable", "0"}, {"POCSAG Network", "LocalAddress", "127.0.0.1"},
			{"POCSAG Network", "LocalPort", "3800"}, {"POCSAG Network", "GatewayAddress", "127.0.0.1"},
			{"POCSAG Network", "GatewayPort", "4800"}, {"POCSAG Network", "Debug", "0"},
		} {
			modesDefault(p, "mmdvmhost", d[0], d[1], d[2])
		}
		modesForce(p, "mmdvmhost", "POCSAG Network", "ModeHang", "5")
		return nil
	},
}

// --- Mobile GPS Configuration (5538-5579) ------------------------------------

// /etc/mobilegps is edited like config_writer_stage_flat(): the first
// "Key=" line of the file, whatever its section, and never a new key.

func modesFlatLines(c *cfgContext, file string) [][3]string {
	var out [][3]string
	for _, sec := range c.St.Layout[file] {
		for _, kv := range sec.Keys {
			out = append(out, [3]string{sec.Name, kv[0], kv[1]})
		}
	}
	return out
}

func modesFlatSet(p *cfgPlan, file, key, value string) {
	for _, l := range modesFlatLines(p.ctx, file) {
		if l[1] == key {
			p.set(file, l[0], key, value)
			return
		}
	}
}

func modesGPSVisible(c *cfgContext) bool {
	return c.has("mobilegps") && modesMMDVMHost(c)
}

// modesGPSPort returns the current port (the last "Port=/dev/..." line) and the options.
func modesGPSPort(c *cfgContext) (string, []linkOption) {
	var path string
	for _, l := range modesFlatLines(c, "mobilegps") {
		if line := l[1] + "=" + l[2]; strings.Contains(line, "Port=/dev/") {
			path = strings.Split(line, "=")[1]
		}
	}
	cur := ""
	if parts := strings.Split(path, "/"); len(parts) > 2 {
		cur = parts[2]
	}
	opts := []linkOption{{Value: cur, Label: path}}
	if entries, err := os.ReadDir(modesDevDir); err == nil {
		for _, e := range entries {
			if n := e.Name(); strings.Contains(n, "ttyA") || strings.Contains(n, "ttyUSB") {
				opts = append(opts, linkOption{Value: n, Label: "/dev/" + n})
			}
		}
	}
	return cur, modesDedupe(opts)
}

// modesGPSSpeed is the last line containing "Speed", text after the first "=".
func modesGPSSpeed(c *cfgContext) string {
	speed := ""
	for _, l := range modesFlatLines(c, "mobilegps") {
		if line := l[1] + "=" + l[2]; strings.Contains(line, "Speed") {
			speed = strings.Split(line, "=")[1]
		}
	}
	return speed
}

func modesGPSOn(c *cfgContext) bool {
	v, ok := modesStored(c, "mmdvmhost", "Mobile GPS", "Enable")
	return ok && modesTruthy(v)
}

var (
	modesGPSPortName = regexp.MustCompile(`^[A-Za-z0-9]+$`)
	modesGPSBaud     = regexp.MustCompile(`^[0-9]{1,6}$`)
)

var modesGPSSection = cfgSectionImpl{ID: "mobilegps", Order: 120,
	Build: func(c *cfgContext) *cfgSection {
		if !modesGPSVisible(c) {
			return nil
		}
		port, ports := modesGPSPort(c)
		enable := toggle("mobilegps_enable", modesGPSOn(c))
		enable.Attrs = map[string]string{"label": "Mobile GPS"}
		rows := []cfgRow{modesHeader(),
			{Cells: []cfgCell{
				modesLabel("MobileGPS Enable:", tip("MobileGPS", "Enable the MobileGPS service, this uses a GPS dongle to update the position information in MMDVMHost.")),
				td(enable),
			}},
			{Cells: []cfgCell{
				modesLabel("GPS Port:", tip("GPS Port", "The port used by the Mobile GPS service.")),
				td(selectBox("mobilegps_port", port, ports)),
			}},
			{Cells: []cfgCell{
				modesLabel("GPS Port Speed:", tip("GPS Port Speed", "Port speed for the Mobile GPS port.")),
				td(modesText("mobilegps_speed", modesGPSSpeed(c), "13", "6")),
			}},
		}
		return &cfgSection{ID: "mobilegps", Title: "Mobile GPS Configuration", Rows: rows, Apply: true}
	},
	Apply: func(c *cfgContext, form cfgForm, p *cfgPlan) error {
		port, ports := modesGPSPort(c)
		newPort, portEdited := modesEdited(form, "mobilegps_port", port)
		if portEdited && newPort != "" && (!modesGPSPortName.MatchString(newPort) || !modesHasOption(ports, newPort)) {
			return cfgError("GPS Port: unknown serial port " + strconv.Quote(newPort))
		}
		newSpeed, speedEdited := modesEdited(form, "mobilegps_speed", modesGPSSpeed(c))
		if speedEdited && newSpeed != "" && !modesGPSBaud.MatchString(newSpeed) {
			return cfgError("GPS Port Speed must be a number of up to 6 digits")
		}
		if v := strings.ToUpper(strings.TrimSpace(form["mobilegps_enable"])); v != "" {
			if v != "ON" && v != "OFF" {
				return cfgError("MobileGPS Enable must be ON or OFF")
			}
			on := boolString(v == "ON")
			modesDefault(p, "mmdvmhost", "Mobile GPS", "Enable", "0")
			modesDefault(p, "mmdvmhost", "Mobile GPS", "Address", "127.0.0.1")
			modesDefault(p, "mmdvmhost", "Mobile GPS", "Port", "7834")
			_, gpsd := c.St.Files["ysfgateway"]["GPSD"]
			if !gpsd && p.change.Changes["ysfgateway"]["GPSD"] == nil {
				modesDefault(p, "ysfgateway", "Mobile GPS", "Enable", "0")
				modesDefault(p, "ysfgateway", "Mobile GPS", "Address", "127.0.0.1")
				modesDefault(p, "ysfgateway", "Mobile GPS", "Port", "7834")
			}
			for _, kv := range [][2]string{{"Daemon", "0"}, {"Debug", "0"}, {"DisplayLevel", "0"}, {"FileLevel", "1"}, {"FilePath", pistarLogDir}} {
				modesFlatSet(p, "mobilegps", kv[0], kv[1])
			}
			p.set("mmdvmhost", "Mobile GPS", "Enable", on)
			for _, gw := range []string{"ysfgateway", "nxdngateway"} {
				if modesPresent(p, gw, "Mobile GPS", "Enable") {
					p.set(gw, "Mobile GPS", "Enable", on)
				}
			}
			modesFlatSet(p, "mobilegps", "Enabled", on)
		}
		if portEdited && newPort != "" {
			modesFlatSet(p, "mobilegps", "Port", "/dev/"+newPort)
		}
		if speedEdited && newSpeed != "" {
			modesFlatSet(p, "mobilegps", "Speed", newSpeed)
		}
		return nil
	},
}
