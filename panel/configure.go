package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Configuration page backend: the root helper pistar-configure reads every
// configured radio file and applies validated per-key changes atomically.

var configureHelper string

type configState struct {
	Revision string                                  `json:"revision"`
	Files    map[string]map[string]map[string]string `json:"files"`
	Layout   map[string][]struct {
		Name string
		Keys [][2]string
	} `json:"-"`
	RawLayout map[string][][2]json.RawMessage `json:"layout"`
	System    struct {
		Hostname string             `json:"hostname"`
		Timezone string             `json:"timezone"`
		Markers  map[string]*string `json:"markers"`
	} `json:"system"`
}

type configChange struct {
	Revision string                                   `json:"revision"`
	Changes  map[string]map[string]map[string]*string `json:"changes,omitempty"`
	Restart  []string                                 `json:"restart,omitempty"`
	System   map[string]any                           `json:"system,omitempty"`
}

type configHelperFunc func(ctx context.Context, verb string, input []byte) ([]byte, error)

var errConfigStale = errors.New("stale")

func runConfigureHelper(ctx context.Context, verb string, input []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	// verb is an internal constant such as "read" or "full-write cron".
	cmd := exec.CommandContext(ctx, "/usr/bin/sudo", append([]string{"-n", configureHelper}, strings.Fields(verb)...)...)
	cmd.Stdin = bytes.NewReader(input)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 3 {
		return out.Bytes(), errConfigStale
	}
	return out.Bytes(), err
}

// get returns the stored value or "".
func (s configState) get(file, section, key string) string { return s.Files[file][section][key] }

func (s configState) has(file string) bool { return s.Files[file] != nil }

func readConfigState(ctx context.Context, run configHelperFunc) (configState, error) {
	var s configState
	out, err := run(ctx, "read", nil)
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(out, &s); err != nil {
		return s, err
	}
	// layout: {file: [[section, [[key, value], ...]], ...]}
	s.Layout = map[string][]struct {
		Name string
		Keys [][2]string
	}{}
	for file, sections := range s.RawLayout {
		for _, pair := range sections {
			var sec struct {
				Name string
				Keys [][2]string
			}
			if json.Unmarshal(pair[0], &sec.Name) != nil || json.Unmarshal(pair[1], &sec.Keys) != nil {
				return s, errors.New("invalid layout")
			}
			s.Layout[file] = append(s.Layout[file], sec)
		}
	}
	return s, nil
}

func setConfigHelper(lib string) { configureHelper = filepath.Join(lib, "pistar-configure") }
