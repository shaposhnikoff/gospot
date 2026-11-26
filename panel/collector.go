package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const modemUnit = "mmdvmhost.service"
const masterUnit = "dmrgateway.service"

type diagnosticRule struct {
	pattern, state, detail string
}

var modemRules = []diagnosticRule{
	{`MMDVM protocol version: [0-9]+, description:|The modem is answering again`, "up", "Модем ответил на запрос MMDVMHost."},
	{`Cannot open the modem device|No response from the modem|No reply from the modem|The MMDVM is not responding|Error when reading from the modem|Received a NAK to the SET_[A-Z0-9_]+ command from the modem|MMDVM protocol version: [0-9]+, unsupported|Closing the MMDVM`, "down", "MMDVMHost сообщил об ошибке или закрытии модема."},
	{`Opening the MMDVM`, "unknown", "Модем открывается; ответ ещё не подтверждён."},
}

var masterRules = []diagnosticRule{
	{`GoDMRServer, Logged into the master (via DTC protocol )?successfully`, "up", "DMRGateway подтвердил вход на мастер-сервер."},
	{`GoDMRServer, (Could not lookup the address of the master|Socket has failed|Login to the master (via DTC protocol )?has failed|Master is closing down|Connection to the master has timed out|Closing DMR Network)`, "down", "DMRGateway сообщил о потере связи или отказе мастер-сервера."},
	{`GoDMRServer, Opening DMR Network`, "unknown", "Открывается сеть DMR; вход ещё не подтверждён."},
}

// Only accept application events at the beginning of a log record, not arbitrary
// text embedded in an ID lookup, MQTT payload, configuration value, etc.
const eventPrefix = `^([A-Z]: [0-9-]+ [0-9:.]+ +)?`

type diagnosticSpec struct {
	unit, filter string
	matchers     []*regexp.Regexp
	rules        []diagnosticRule
}

func diagnosticFor(unit string, rules []diagnosticRule) diagnosticSpec {
	s := diagnosticSpec{unit: unit, rules: rules}
	var patterns []string
	for _, r := range rules {
		patterns = append(patterns, r.pattern)
		s.matchers = append(s.matchers, regexp.MustCompile(eventPrefix+"("+r.pattern+")"))
	}
	s.filter = eventPrefix + "(" + strings.Join(patterns, "|") + ")"
	return s
}

var modemDiagnostic = diagnosticFor(modemUnit, modemRules)
var masterDiagnostic = diagnosticFor(masterUnit, masterRules)
var invocationFormat = regexp.MustCompile(`^[0-9a-f]{32}$`)

type diagOutput struct{ buf bytes.Buffer }

func (b *diagOutput) Write(p []byte) (int, error) {
	if len(p) > 64*1024-b.buf.Len() {
		return 0, errors.New("diagnostic output too large")
	}
	return b.buf.Write(p)
}

type diagRun func(context.Context, string, ...string) ([]byte, error)

func runDiagnostic(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 200 * time.Millisecond
	var buf diagOutput
	cmd.Stdout = &buf
	err := cmd.Run()
	return buf.buf.Bytes(), err
}

type serviceObservation struct{ load, active, sub, id string }

func observeService(ctx context.Context, run diagRun, unit string) serviceObservation {
	out, err := run(ctx, "systemctl", "show", "--no-pager", "-p", "LoadState,ActiveState,SubState,InvocationID", unit)
	if err != nil {
		return serviceObservation{}
	}
	props := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			props[k] = v
		}
	}
	return serviceObservation{props["LoadState"], props["ActiveState"], props["SubState"], props["InvocationID"]}
}

func collectSignal(ctx context.Context, run diagRun, s diagnosticSpec) Signal {
	obs := observeService(ctx, run, s.unit)
	sig := Signal{State: "unknown", Source: "systemd: " + s.unit, Detail: "Текущее состояние службы не подтверждено.", At: time.Now().UTC()}
	if obs.load != "loaded" {
		return sig
	}
	if obs.active == "failed" || obs.active == "inactive" || obs.sub == "auto-restart" {
		sig.State = "down"
		sig.Detail = "Служба остановлена или перезапускается после сбоя."
		return sig
	}
	if obs.active != "active" || obs.sub != "running" || !invocationFormat.MatchString(obs.id) || obs.id == strings.Repeat("0", 32) {
		return sig
	}
	sig.Source = "journal: " + s.unit
	sig.Detail = "Нет подтверждённой записи текущего запуска."
	// Find the latest transition even if routine traffic has buried startup.
	out, err := run(ctx, "journalctl", "--no-pager", "-q", "-b", "-u", s.unit, "_SYSTEMD_INVOCATION_ID="+obs.id, "--grep="+s.filter, "-n", "1", "-o", "json", "--output-fields=MESSAGE,_SYSTEMD_INVOCATION_ID")
	if err != nil {
		return sig
	}
	var e struct {
		ID      string `json:"_SYSTEMD_INVOCATION_ID"`
		Message string `json:"MESSAGE"`
		Stamp   string `json:"__REALTIME_TIMESTAMP"`
	}
	if json.Unmarshal(out, &e) != nil || e.ID != obs.id {
		return sig
	}
	us, err := strconv.ParseInt(e.Stamp, 10, 64)
	if err != nil || us <= 0 || us > time.Now().UnixMicro() {
		return sig
	}
	if observeService(ctx, run, s.unit) != obs {
		sig.Detail = "Состояние службы изменилось во время проверки."
		return sig
	}
	for i, m := range s.matchers {
		if m.MatchString(e.Message) {
			sig.State, sig.Detail, sig.At = s.rules[i].state, s.rules[i].detail, time.UnixMicro(us).UTC()
			break
		}
	}
	return sig
}

func collect() Status {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	node, _ := os.Hostname()
	m := make(chan Signal, 1)
	go func() { m <- collectSignal(ctx, runDiagnostic, modemDiagnostic) }()
	master := collectSignal(ctx, runDiagnostic, masterDiagnostic)
	return Status{Node: node, Modem: <-m, Master: master}
}
