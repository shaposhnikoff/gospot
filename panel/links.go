package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Admin link managers of the classic index.php: bm_links.php, bm_manager.php,
// ysf_manager.php, p25_manager.php, nxdn_manager.php and m17_manager.php.
// Gateways are driven over their UDP remote-command port (what RemoteCommand
// does), so no root helper is needed. Dispatch only after wizardHandler's TLS
// and Basic Auth checks.

var (
	ysfGWINI, p25GWINI, nxdnGWINI, m17GWINI = newINIFile(), newINIFile(), newINIFile(), newINIFile()
	bmKeyFile                               string
)

type linkOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type linkManager struct {
	Mode     string       `json:"mode"`
	Selected string       `json:"selected"`
	Room     string       `json:"room,omitempty"`
	Options  []linkOption `json:"options"`
	port     int
}

type bmLinks struct {
	Master  string   `json:"master"`
	ID      string   `json:"id"`
	Static  []string `json:"static"`
	Dynamic []string `json:"dynamic"`
	Manager bool     `json:"manager"`
	Simplex bool     `json:"simplex"`
	Error   string   `json:"error,omitempty"`
}

type linksReply struct {
	CSRF     string        `json:"csrf"`
	BM       *bmLinks      `json:"bm"`
	Managers []linkManager `json:"managers"`
}

// hostsFile reads whitespace- or semicolon-separated host lists, skipping comments.
func hostsFile(name, sep string) [][]string {
	rows, _ := cachedHostFile("hosts"+sep, filepath.Join(pistarHostsDir, name), func(f io.Reader) [][]string { return parseHostRows(f, sep) })
	return cloneRows(rows)
}

func parseHostRows(f io.Reader, sep string) [][]string {
	var rows [][]string
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		var fields []string
		if sep == "" {
			fields = strings.Fields(line)
		} else {
			fields = strings.Split(line, sep)
		}
		if len(fields) >= 2 && fields[0] != "" && !strings.Contains(fields[0], "#") {
			rows = append(rows, fields)
		}
	}
	return rows
}

func cut30(s string) string {
	if len(s) >= 30 {
		return s[:27] + "..."
	}
	return s
}

func remotePort(gw iniData) (int, bool) {
	if !gw.on("Remote Commands", "Enable") {
		return 0, false
	}
	port, err := strconv.Atoi(gw.get("Remote Commands", "Port"))
	return port, err == nil && port > 0 && port < 65536
}

func ysfManager(gw iniData) linkManager {
	startup := gw.get("Network", "Startup")
	m := linkManager{Mode: "YSF", Selected: "none", Options: []linkOption{{"none", "None"},
		{"YSF00001", "YSF00001 - Parrot"}, {"YSF00002", "YSF00002 - Link YSF2DMR"},
		{"YSF00003", "YSF00003 - Link YSF2NXDN"}, {"YSF00004", "YSF00004 - Link YSF2P25"}}}
	for name, value := range map[string]string{"ZZ Parrot": "YSF00001", "YSF2DMR": "YSF00002", "YSF2NXDN": "YSF00003", "YSF2P25": "YSF00004"} {
		if startup == name {
			m.Selected = value
		}
	}
	for _, h := range hostsFile("YSFHosts.txt", ";") {
		if len(h) < 3 {
			continue
		}
		name := cut30(h[1])
		m.Options = append(m.Options, linkOption{"YSF" + h[0], "YSF" + h[0] + " - " + name + " - " + h[2]})
		if startup != "" && startup == name {
			m.Selected = "YSF" + h[0]
		}
	}
	for _, h := range hostsFile("FCSHosts.txt", ";") {
		if strings.HasPrefix(h[0], "FCS") {
			m.Options = append(m.Options, linkOption{h[0], h[0] + " - " + cut30(h[1])})
			if startup == h[0] {
				m.Selected = h[0]
			}
		}
	}
	return m
}

func tgManager(mode string, gw iniData, files ...string) linkManager {
	startup := gw.get("Network", "Startup")
	m := linkManager{Mode: mode, Selected: "none", Options: []linkOption{{"none", "None"}, {"10", "10 - Parrot"}}}
	if startup == "10" {
		m.Selected = "10"
	}
	for _, file := range files {
		for _, h := range hostsFile(file, "") {
			m.Options = append(m.Options, linkOption{h[0], h[0] + " - " + h[1]})
			if startup != "" && startup == h[0] {
				m.Selected = h[0]
			}
		}
	}
	return m
}

func m17Manager(gw iniData) linkManager {
	startup := gw.get("Network", "Startup")
	host := strings.Split(startup, "_")[0]
	m := linkManager{Mode: "M17", Selected: "none", Options: []linkOption{{"none", "None"}}}
	if startup != "" {
		m.Room = startup[len(startup)-1:]
	}
	for _, h := range hostsFile("M17Hosts.txt", "") {
		m.Options = append(m.Options, linkOption{h[0], h[0]})
		if host != "" && host == h[0] {
			m.Selected = h[0]
		}
	}
	return m
}

func linkManagers(host iniData) []linkManager {
	var out []linkManager
	add := func(network string, gw *iniFile, build func(iniData) linkManager) {
		if !host.on(network, "Enable") {
			return
		}
		cfg := gw.load()
		port, ok := remotePort(cfg)
		if !ok {
			return
		}
		m := build(cfg)
		m.port = port
		out = append(out, m)
	}
	add("System Fusion Network", ysfGWINI, ysfManager)
	add("P25 Network", p25GWINI, func(g iniData) linkManager { return tgManager("P25", g, "P25Hosts.txt", "P25HostsLocal.txt") })
	add("NXDN Network", nxdnGWINI, func(g iniData) linkManager { return tgManager("NXDN", g, "NXDNHosts.txt", "NXDNHostsLocal.txt") })
	add("M17 Network", m17GWINI, m17Manager)
	return out
}

// linkCommand builds the RemoteCommand text exactly as the classic managers do.
func linkCommand(m linkManager, link bool, target, room string) (string, error) {
	known := false
	for _, o := range m.Options {
		known = known || o.Value == target
	}
	if !known {
		return "", fmt.Errorf("unknown target")
	}
	unlink := !link || target == "none"
	switch m.Mode {
	case "YSF":
		if unlink {
			return "UnLink", nil
		}
		// "YSF12345" -> "LinkYSF 12345", "FCS00290" -> "LinkFCS 00290"
		if v := regexp.MustCompile(`^([A-Za-z]+)([0-9]+)$`).FindStringSubmatch(target); v != nil {
			return "Link" + v[1] + " " + v[2], nil
		}
	case "P25":
		if unlink {
			return "TalkGroup 9999", nil
		}
		return "TalkGroup " + target, nil
	case "NXDN":
		if unlink {
			return "TalkGroup unlink", nil
		}
		return "TalkGroup " + target, nil
	case "M17":
		if unlink {
			return "Reflector unlink", nil
		}
		if len(room) == 1 && room[0] >= 'A' && room[0] <= 'K' {
			return "Reflector " + target + " " + room, nil
		}
	}
	return "", fmt.Errorf("invalid target")
}

// sendRemoteCommand does what Pi-Star's RemoteCommand binary does.
func sendRemoteCommand(port int, command string) string {
	conn, err := net.DialTimeout("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
	if err != nil {
		return "Unable to send the command"
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(command)); err != nil {
		return "Unable to send the command"
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 1024)
	if n, err := conn.Read(buf); err == nil && n > 0 {
		return strings.ToValidUTF8(strings.TrimSpace(string(buf[:n])), "?")
	}
	return fmt.Sprintf("Command sent: %q to port: %d", command, port)
}

// dmrMaster resolves the DMR master name and peer ID like bm_links.php.
func dmrMaster(host, gw iniData) (name, id string) {
	in := dashInputs{host: host}
	address := host.get("DMR Network", "Address")
	if address == "" {
		address = host.get("DMR Network", "RemoteAddress")
	}
	switch {
	case in.dmrViaGateway():
		address, id = gw.get("DMR Network 1", "Address"), gw.get("DMR Network 1", "Id")
	case host.get("DMR", "Id") != "":
		id = host.get("DMR", "Id")
	default:
		id = host.get("General", "Id")
	}
	name = address
	for _, h := range hostsFile("DMR_Hosts.txt", "") {
		if len(h) >= 3 && h[2] == address {
			name = strings.ReplaceAll(h[0], "_", " ")
		}
	}
	return name, id
}

func readBMKey() string {
	data, err := os.ReadFile(bmKeyFile)
	if err != nil {
		return ""
	}
	key := parseINI(strings.NewReader(string(data))).get("key", "apikey")
	if len(key) < 200 || !validBMKey(key) { // API v2 keys only, as in bm_manager.php
		return ""
	}
	return key
}

func bmProfile(ctx context.Context, b *bmManager, host iniData, id string) ([]string, []string, error) {
	data, status, err := b.request(ctx, "GET", "device/"+id+"/profile", "", nil)
	if err != nil {
		return nil, nil, fmt.Errorf("BrandMeister API: %d", status)
	}
	type sub struct {
		Slot  int `json:"slot"`
		Group int `json:"talkgroup"`
	}
	var p struct{ Static, Dynamic []sub }
	var raw struct {
		Static  []sub `json:"staticSubscriptions"`
		Dynamic []sub `json:"dynamicSubscriptions"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return nil, nil, fmt.Errorf("BrandMeister API: invalid reply")
	}
	p.Static, p.Dynamic = raw.Static, raw.Dynamic
	ts1, ts2 := host.on("DMR Network", "Slot1"), host.on("DMR Network", "Slot2")
	list := func(subs []sub) []string {
		var out []string
		for _, s := range subs {
			switch {
			case ts1 && s.Slot == 1, ts2 && s.Slot == 2:
				out = append(out, fmt.Sprintf("TG%d(%d)", s.Group, s.Slot))
			case !ts1 && ts2 && s.Slot == 0:
				out = append(out, fmt.Sprintf("TG%d", s.Group))
			}
		}
		return out
	}
	return list(p.Static), list(p.Dynamic), nil
}

func serveLinks(w http.ResponseWriter, r *http.Request, bm *bmManager, send func(int, string) string) {
	host := mmdvmINI.load()
	switch {
	case r.URL.EscapedPath() == "/manage/api/links" && r.Method == http.MethodGet:
		csrf := editorToken(w, r)
		if csrf == "" {
			return
		}
		reply := linksReply{CSRF: csrf, Managers: linkManagers(host)}
		if host.on("DMR", "Enable") {
			if name, id := dmrMaster(host, dmrGWINI.load()); strings.HasPrefix(name, "BM") {
				ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
				static, dynamic, err := bmProfile(ctx, bm, host, id)
				cancel()
				reply.BM = &bmLinks{Master: name, ID: id, Static: static, Dynamic: dynamic, Manager: readBMKey() != "",
					Simplex: host.get("DMR Network", "Slot1") == "0"}
				if err != nil {
					reply.BM.Error = err.Error()
				}
			}
		}
		writeJSON(w, 200, reply)
	case r.URL.EscapedPath() == "/manage/api/links" && r.Method == http.MethodPost:
		var in struct {
			Mode, Action, Target, Room string
		}
		if !maintenancePost(w, r, &in) {
			return
		}
		for _, m := range linkManagers(host) {
			if m.Mode != in.Mode {
				continue
			}
			if in.Action != "link" && in.Action != "unlink" {
				break
			}
			command, err := linkCommand(m, in.Action == "link", in.Target, in.Room)
			if err != nil {
				http.Error(w, "Somthing wrong with your input - please try again", 400)
				return
			}
			writeJSON(w, 200, map[string]string{"output": send(m.port, command)})
			return
		}
		http.Error(w, "Somthing wrong with your input - please try again", 400)
	case r.URL.EscapedPath() == "/manage/api/links/bm" && r.Method == http.MethodPost:
		var in struct {
			Action string `json:"action"`
			TG     string `json:"tg"`
			Slot   string `json:"slot"`
		}
		if !maintenancePost(w, r, &in) {
			return
		}
		name, id := dmrMaster(host, dmrGWINI.load())
		key := readBMKey()
		if !host.on("DMR", "Enable") || !strings.HasPrefix(name, "BM") || key == "" {
			http.Error(w, "BrandMeister Manager is not available", 409)
			return
		}
		slot := in.Slot
		if host.get("DMR Network", "Slot1") == "0" {
			slot = "0"
		} else if slot != "1" && slot != "2" {
			http.Error(w, "Invalid slot", 400)
			return
		}
		tg, _ := strconv.Atoi(regexp.MustCompile(`[^0-9]`).ReplaceAllString(in.TG, ""))
		method, path, body := "GET", "device/"+id+"/", []byte(nil)
		switch in.Action {
		case "drop-dynamic":
			path += "action/dropDynamicGroups/" + slot
		case "drop-qso":
			path += "action/dropCallRoute/" + slot
		case "add", "delete":
			if tg < 1 || tg > 9999999 {
				http.Error(w, "Invalid talkgroup", 400)
				return
			}
			if in.Action == "add" {
				method, path = "POST", path+"talkgroup/"
				body, _ = json.Marshal(map[string]string{"slot": slot, "group": strconv.Itoa(tg)})
			} else {
				method, path = "DELETE", path+"talkgroup/"+slot+"/"+strconv.Itoa(tg)
			}
		default:
			http.Error(w, "Unknown action", 400)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
		defer cancel()
		_, _, err := bm.request(ctx, method, path, key, body)
		output := "BrandMeister APIv2: OK"
		if err != nil {
			output = "BrandMeister APIv2: No Response"
		}
		writeJSON(w, 200, map[string]string{"output": output})
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Метод не поддерживается", 405)
	}
}
