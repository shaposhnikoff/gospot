package main

import (
	"bufio"
	"context"
	"encoding/json"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// journalState holds what the classic dashboard derives from MMDVMHost and
// DMRGateway logs. A single long-running `journalctl -f` feeds it, so a page
// refresh never forks a log scan.
type journalState struct {
	mu       sync.Mutex
	firmware string
	tcxo     string
	// DMR master login per unit: 1 logged in, -1 failed/timed out, 0 unknown.
	login map[string]int
}

var journal = journalState{login: map[string]int{}}

var protocolLine = regexp.MustCompile(`MMDVM protocol version: [0-9]+, description: (.+)$`)
var tcxoPattern = regexp.MustCompile(`([0-9]{2}\.[0-9]{3,4}) ?M[Hh]z`)
var loginOK = regexp.MustCompile(`(?i)master.*successfully|successfully.*master`)
var loginFail = regexp.MustCompile(`(?i)master.*(failed|timed out)`)

func (j *journalState) observe(unit, message string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if m := protocolLine.FindStringSubmatch(message); m != nil {
		j.firmware = modemFirmware(m[1])
		j.tcxo = ""
		if t := tcxoPattern.FindStringSubmatch(m[1]); t != nil {
			j.tcxo = t[1] + " MHz"
		}
	}
	switch {
	case loginOK.MatchString(message):
		j.login[unit] = 1
	case loginFail.MatchString(message):
		j.login[unit] = -1
	}
}

func (j *journalState) snapshot() (firmware, tcxo string, login map[string]int) {
	j.mu.Lock()
	defer j.mu.Unlock()
	login = make(map[string]int, len(j.login))
	for k, v := range j.login {
		login[k] = v
	}
	return j.firmware, j.tcxo, login
}

var firmwareVersion = regexp.MustCompile(`^v?([0-9][0-9A-Za-z.]*)`)

// modemFirmware reproduces the classic "Type:version" labels from the modem
// description, e.g. "MMDVM_HS_Dual_Hat-v1.5.2 ..." -> "HS_Hat:v1.5.2".
func modemFirmware(desc string) string {
	token := func(rest string, trimV bool) string {
		rest = strings.Fields(rest + " ")[0]
		if trimV {
			rest = strings.TrimLeft(rest, "v")
		}
		if len(rest) > 12 {
			rest = rest[:12]
		}
		return rest
	}
	for _, p := range []struct {
		prefix, label string
		trimV         bool
	}{
		{"MMDVM_HS_Dual_Hat-", "HS_Hat:", false},
		{"MMDVM_HS_Hat-", "HS_Hat:", false},
		{"D2RG_MMDVM_HS-", "HS_Hat:", false},
		{"MMDVM_HS-", "MMDVM_HS:", true},
		{"MMDVM_HS ", "MMDVM_HS:", true},
		{"MMDVM_MDO ", "MMDVM_MDO:", true},
		{"ZUMspot-", "ZUMspot:", false},
		{"ZUMspot ", "ZUMspot:", false},
	} {
		if rest, ok := strings.CutPrefix(desc, p.prefix); ok {
			return p.label + token(rest, p.trimV)
		}
	}
	if strings.Contains(desc, "DVMEGA") {
		if len(desc) > 15 {
			return desc[:15]
		}
		return desc
	}
	if rest, ok := strings.CutPrefix(desc, "MMDVM "); ok {
		v := token(rest, false)
		if m := firmwareVersion.FindString(v); m != "" {
			v = m
		}
		if len(v) > 8 {
			v = v[:8]
		}
		return "MMDVM:" + v
	}
	return ""
}

type followRecord struct {
	Message    string `json:"MESSAGE"`
	Unit       string `json:"_SYSTEMD_UNIT"`
	Stamp      string `json:"__REALTIME_TIMESTAMP"`
	Cursor     string `json:"__CURSOR"`
	Invocation string `json:"_SYSTEMD_INVOCATION_ID"`
}

// followJournal keeps one journalctl process attached to the radio units and
// restarts it after the last seen cursor if it exits.
func followJournal(ctx context.Context) {
	primeFirmware(ctx)
	cursor := ""
	for ctx.Err() == nil {
		args := []string{"--system", "-q", "--no-pager", "-b", "-f", "-o", "json",
			"--output-fields=MESSAGE,_SYSTEMD_UNIT,_SYSTEMD_INVOCATION_ID",
			"-u", modemUnit, "-u", masterUnit}
		if cursor == "" {
			args = append(args, "-n", "400")
		} else {
			args = append(args, "--after-cursor="+cursor)
		}
		cmd := exec.CommandContext(ctx, "journalctl", args...)
		out, err := cmd.StdoutPipe()
		if err == nil && cmd.Start() == nil {
			s := bufio.NewScanner(out)
			s.Buffer(make([]byte, 64<<10), 64<<10)
			for s.Scan() {
				var r followRecord
				if json.Unmarshal(s.Bytes(), &r) != nil {
					continue
				}
				cursor = r.Cursor
				applyRecord(r, time.Now())
			}
			_ = cmd.Wait()
		}
		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
	}
}

func applyRecord(r followRecord, now time.Time) {
	if r.Unit != modemUnit && r.Unit != masterUnit {
		return
	}
	journal.observe(r.Unit, r.Message)
	if r.Unit == modemUnit {
		recentHeard.stream(heardRecordJSON{Message: r.Message, Stamp: r.Stamp, Cursor: r.Cursor, Invocation: r.Invocation}, now)
	}
}

// The protocol banner is written once at modem start, usually long before the
// tail the follower begins with.
func primeFirmware(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := runDiagnostic(ctx, "journalctl", "--system", "-q", "--no-pager", "-b", "-u", modemUnit,
		"--grep=MMDVM protocol version", "-n", "1", "-o", "cat")
	if err == nil {
		journal.observe(modemUnit, strings.TrimSpace(string(out)))
	}
}
