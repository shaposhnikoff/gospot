package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	radioLogPath     = "/manage/api/log"
	radioLogDownload = radioLogPath + "/download"
	radioLogLimit    = 64
)

type radioLogRow struct {
	At      time.Time `json:"at"`
	Source  string    `json:"source"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
}

var radioPrefix = regexp.MustCompile(`^([DMIWEF]): \d{4}-\d\d-\d\d \d\d:\d\d:\d\d\.\d{3} `)
var radioLevels = map[string]string{"D": "debug", "M": "message", "I": "info", "W": "warning", "E": "error", "F": "fatal"}
var journalLevels = [...]string{"emergency", "alert", "critical", "error", "warning", "notice", "info", "debug"}

// Application records include lookup values, modem-provided text, config
// dumps and MQTT payloads. Format a small vocabulary of events instead of
// forwarding MESSAGE (even from the two trusted journal units).
const radioParties = ` from ` + heardID + ` to (?:TG )?` + heardID

var radioStart = regexp.MustCompile(`^DMR Slot ([12]), received (RF|network) (voice header|late entry|data header)` + radioParties + `(, \d{1,3} blocks)?$`)
var radioEnd = regexp.MustCompile(`^DMR Slot ([12]), received (RF|network) end of voice transmission` + radioParties + `, (\d{1,5}\.\d) seconds, (?:\d{1,3}% packet loss, )?BER: (\d{1,3}\.\d)%(?:, RSSI: -?\d{1,3}/-?\d{1,3}/-?\d{1,3} dBm)?$`)
var radioProtocol = regexp.MustCompile(`^MMDVM protocol version: ([12]), description: [ -~]{0,470}$`)

func radioLogMessage(unit, msg string) string {
	switch unit {
	case modemUnit:
		switch msg {
		case "Opening the MMDVM", "Closing the MMDVM", "The modem is answering again",
			"No reply from the modem for some time, resetting it", "Error when reading from the modem",
			"Unable to read the firmware version after six attempts", "MMDVM RX buffer has overflowed",
			"MMDVM TX buffer has overflowed", "Serial data buffer overflow, resetting",
			"DMR, Opening DMR Network", "DMR, Closing DMR Network":
			return msg // exact literals only
		}
		if m := radioProtocol.FindStringSubmatch(msg); m != nil {
			return "MMDVM protocol version: " + m[1]
		}
		if m := radioStart.FindStringSubmatch(msg); m != nil && (m[3] == "data header") == (m[4] != "") {
			return "DMR Slot " + m[1] + ", received " + m[2] + " " + m[3]
		}
		if m := radioEnd.FindStringSubmatch(msg); m != nil {
			return fmt.Sprintf("DMR Slot %s, %s end of voice transmission, %s seconds, BER: %s%%", m[1], m[2], m[3], m[4])
		}
	case masterUnit:
		event, ok := strings.CutPrefix(msg, "GoDMRServer, ")
		if !ok {
			break
		}
		switch event {
		case "Opening DMR Network", "Closing DMR Network", "Logged into the master successfully",
			"Logged into the master via DTC protocol successfully", "Master is closing down",
			"Could not lookup the address of the master", "Connection to the master has timed out, retrying connection",
			"Socket has failed, retrying connection to the master", "Socket has failed when writing data to the master, retrying connection",
			"Login to the master has failed, retrying login ...", "Login to the master has failed, retrying network ...",
			"Login to the master via DTC protocol has failed, retrying login ...", "Login to the master via DTC protocol has failed, retrying network ...":
			return event
		}
	}
	return ""
}

func radioLogRows(out []byte) []radioLogRow {
	rows := []radioLogRow{}
	lines := bytes.SplitN(out, []byte{'\n'}, radioLogLimit+1)
	for _, line := range lines[:min(len(lines), radioLogLimit)] {
		var e struct {
			Unit     string `json:"_SYSTEMD_UNIT"`
			Stamp    string `json:"__REALTIME_TIMESTAMP"`
			Message  string `json:"MESSAGE"`
			Priority string `json:"PRIORITY"`
		}
		if json.Unmarshal(line, &e) != nil || len(e.Message) > 512 || strings.ContainsAny(e.Message, "\x00\r\n") {
			continue
		}
		source := "MMDVMHost"
		if e.Unit == masterUnit {
			source = "DMRGateway"
		} else if e.Unit != modemUnit {
			continue
		}
		us, err := strconv.ParseInt(e.Stamp, 10, 64)
		at := time.UnixMicro(us).UTC()
		if err != nil || us <= 0 || at.Year() > 9999 {
			continue
		}
		level := "unknown"
		if p, err := strconv.Atoi(e.Priority); err == nil && p >= 0 && p < len(journalLevels) {
			level = journalLevels[p]
		}
		if prefix := radioPrefix.FindString(e.Message); prefix != "" {
			level = radioLevels[prefix[:1]]
			e.Message = e.Message[len(prefix):]
		}
		if msg := radioLogMessage(e.Unit, e.Message); msg != "" {
			rows = append(rows, radioLogRow{at, source, level, msg})
		}
	}
	return rows
}

var radioLogSlot = make(chan struct{}, 1)

// The dedicated HTTPS wizard checks admin credentials before dispatch here.
func serveRadioLog(w http.ResponseWriter, r *http.Request) {
	serveRadioLogWith(w, r, runDiagnostic)
}

func serveRadioLogWith(w http.ResponseWriter, r *http.Request, run diagRun) {
	h := w.Header()
	h.Set("Cache-Control", "private, no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	path := r.URL.EscapedPath()
	if path != radioLogPath && path != radioLogDownload {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		h.Set("Allow", "GET")
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.RawQuery != "" {
		http.Error(w, "Параметры запроса не поддерживаются", http.StatusBadRequest)
		return
	}
	select {
	case radioLogSlot <- struct{}{}:
		defer func() { <-radioLogSlot }()
	default:
		http.Error(w, "Журнал занят. Повторите позже.", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	// Repeated matches on the same journal field are ORed. -n counts
	// unfiltered records; runDiagnostic also limits stdout to 64 KiB.
	out, err := run(ctx, "/usr/bin/journalctl", "--system", "--quiet", "--no-pager", "--boot=0", "--reverse",
		"-n", strconv.Itoa(radioLogLimit), "-o", "json", "--output-fields=_SYSTEMD_UNIT,MESSAGE,PRIORITY",
		"_SYSTEMD_UNIT="+modemUnit, "_SYSTEMD_UNIT="+masterUnit)
	if err != nil {
		http.Error(w, "Не удалось прочитать журнал радиостанции", http.StatusServiceUnavailable)
		return
	}
	rows := radioLogRows(out)
	if path == radioLogDownload {
		h.Set("Content-Type", "text/plain; charset=utf-8")
		h.Set("Content-Disposition", `attachment; filename="gospot-radio-log.txt"`)
		for _, row := range rows {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", row.At.Format(time.RFC3339Nano), row.Source, row.Level, row.Message)
		}
		return
	}
	h.Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Rows []radioLogRow `json:"rows"`
	}{rows})
}
