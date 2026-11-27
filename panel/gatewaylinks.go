package main

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Link state of the YSF/P25/NXDN/M17 gateways, ported from the classic
// getActualLink(): the newest matching line of today's (or yesterday's)
// gateway log decides.

const linkTail = 64 << 10

func gatewayLogLines(root string, now time.Time) []string {
	for _, day := range []time.Time{now, now.Add(-24 * time.Hour)} {
		path := filepath.Join(pistarLogDir, root+"-"+day.UTC().Format("2006-01-02")+".log")
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		st, err := f.Stat()
		if err == nil && st.Size() > linkTail {
			_, err = f.Seek(st.Size()-linkTail, io.SeekStart)
		}
		var data []byte
		if err == nil {
			data, _ = io.ReadAll(io.LimitReader(f, linkTail))
		}
		f.Close()
		var lines []string
		for _, l := range bytes.Split(data, []byte{'\n'}) {
			if bytes.HasPrefix(l, []byte("M: ")) {
				lines = append(lines, string(l))
			}
		}
		if len(lines) > 0 {
			for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
				lines[i], lines[j] = lines[j], lines[i]
			}
			return lines
		}
	}
	return nil
}

var (
	ysfLinked     = regexp.MustCompile(`Linked to (.{1,16})`)
	ysfAutoFCS    = regexp.MustCompile(`Automatic \(re-\)connection to (FCS[0-9]{5})`)
	ysfAuto       = regexp.MustCompile(`Automatic (?:\(re-\))?connection to ([0-9]{5})`)
	ysfConnect    = regexp.MustCompile(`Connect to ([0-9]{5})`)
	ysfNotLinked  = regexp.MustCompile(`Disconnect via DTMF|Opening YSF network connection|Link has failed|DISCONNECT Reply`)
	tgReflector   = regexp.MustCompile(`(?:Linked (?:at startup )?to|Statically linked to|Switched to) reflector ([0-9]{1,5})`)
	tgUnlinked    = regexp.MustCompile(`Starting (?:P25|NXDN)Gateway|unlinking|Unlinking|Unlinked`)
	m17Linked     = regexp.MustCompile(`Linked .* reflector (M17-.{3} [A-Z])`)
	m17NotLinked  = regexp.MustCompile(`Starting M17Gateway|unlinking|Unlinking|Unlinked`)
	gatewayByMode = map[string]string{"YSF": "YSFGateway", "P25": "P25Gateway", "NXDN": "NXDNGateway", "M17": "M17Gateway"}
)

func ysfLink(lines []string) string {
	for _, l := range lines {
		to := ""
		if m := ysfLinked.FindStringSubmatch(l); m != nil && !strings.Contains(l, "Linked to MMDVM") {
			to = strings.TrimSpace(m[1])
			if strings.HasPrefix(to, "FCS") {
				to = strings.NewReplacer(" ", "", "-", "").Replace(to)
			}
		}
		if m := ysfAutoFCS.FindStringSubmatch(l); m != nil {
			to = m[1]
		} else if m := ysfAuto.FindStringSubmatch(l); m != nil {
			to = m[1]
		}
		if m := ysfConnect.FindStringSubmatch(l); m != nil {
			to = m[1]
		}
		if ysfNotLinked.MatchString(l) {
			to = "Not Linked"
		}
		if to != "" {
			return to
		}
	}
	return "Not Linked"
}

func reflectorLink(lines []string, linked, unlinked *regexp.Regexp, prefix string) string {
	for _, l := range lines {
		if m := linked.FindStringSubmatch(l); m != nil {
			return prefix + m[1]
		}
		if unlinked.MatchString(l) {
			return "Not Linked"
		}
	}
	return "Not Linked"
}

// gatewayLinks returns the link text per mode for running gateways.
func gatewayLinks(now time.Time, modes []string) map[string]string {
	links := map[string]string{}
	for _, mode := range modes {
		lines := gatewayLogLines(gatewayByMode[mode], now)
		switch mode {
		case "YSF":
			links[mode] = ysfRoomName(ysfLink(lines))
		case "P25", "NXDN":
			links[mode] = reflectorLink(lines, tgReflector, tgUnlinked, "TG ")
		case "M17":
			links[mode] = reflectorLink(lines, m17Linked, m17NotLinked, "")
		}
	}
	return links
}

// YSFHosts.txt ("id;name;description;address;port;...") maps a linked room id
// or name to its name, as repeaterinfo.php does.
var ysfHosts struct {
	sync.Mutex
	mod   time.Time
	names map[string]string
}

func ysfRoomName(linked string) string {
	if linked == "Not Linked" || linked == "Service Not Started" {
		return linked
	}
	path := filepath.Join(pistarHostsDir, "YSFHosts.txt")
	ysfHosts.Lock()
	defer ysfHosts.Unlock()
	if st, err := os.Stat(path); err == nil && !st.ModTime().Equal(ysfHosts.mod) {
		names := map[string]string{}
		if f, err := os.Open(path); err == nil {
			s := bufio.NewScanner(io.LimitReader(f, 4<<20))
			for s.Scan() {
				fields := strings.Split(s.Text(), ";")
				if len(fields) >= 2 && fields[0] != "" && fields[1] != "" {
					names[fields[0]] = fields[1]
					names[fields[1]] = fields[1]
				}
			}
			f.Close()
			ysfHosts.names, ysfHosts.mod = names, st.ModTime()
		}
	}
	name := linked
	if n, ok := ysfHosts.names[linked]; ok {
		name = n
	}
	return shorten(strings.ReplaceAll(name, "_", " "))
}
