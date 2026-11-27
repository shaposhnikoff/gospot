package main

import (
	"bufio"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// iniData maps section -> key -> value. Only the first occurrence of a key in
// a section is kept, matching how MMDVMHost and the gateways read their files.
type iniData map[string]map[string]string

func (d iniData) get(section, key string) string { return d[section][key] }

func (d iniData) has(section, key string) bool {
	_, ok := d[section][key]
	return ok
}

func (d iniData) on(section, key string) bool { return d.get(section, key) == "1" }

func parseINI(r io.Reader) iniData {
	// Keys before any section header (flat files such as /etc/dstarrepeater)
	// land in section "".
	d := iniData{"": {}}
	section := ""
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 64<<10), 64<<10)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if line[0] == '[' && strings.HasSuffix(line, "]") {
			section = line[1 : len(line)-1]
			if d[section] == nil {
				d[section] = map[string]string{}
			}
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if _, seen := d[section][k]; !seen {
			d[section][k] = strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	return d
}

// iniFile re-reads the first existing candidate only when it changes, so the
// dashboard can poll every second without re-parsing unchanged files.
type iniFile struct {
	candidates []string
	mu         sync.Mutex
	path       string
	mod        time.Time
	size       int64
	data       iniData
}

func newINIFile(candidates ...string) *iniFile { return &iniFile{candidates: candidates} }

// setPath points the file at one configured location and drops the cache.
func (f *iniFile) setPath(path string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.candidates, f.path, f.data = []string{path}, "", nil
}

// location is the configured path (the first candidate).
func (f *iniFile) location() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.candidates) == 0 {
		return ""
	}
	return f.candidates[0]
}

func (f *iniFile) load() iniData {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, path := range f.candidates {
		st, err := os.Stat(path)
		if err != nil || !st.Mode().IsRegular() {
			continue
		}
		if path == f.path && st.ModTime().Equal(f.mod) && st.Size() == f.size && f.data != nil {
			return f.data
		}
		file, err := os.Open(path)
		if err != nil {
			continue
		}
		data := parseINI(io.LimitReader(file, 1<<20))
		file.Close()
		f.path, f.mod, f.size, f.data = path, st.ModTime(), st.Size(), data
		return data
	}
	f.path, f.data = "", nil
	return iniData{}
}

// exists reports whether any candidate file is present.
func (f *iniFile) exists() bool {
	for _, path := range f.candidates {
		if st, err := os.Stat(path); err == nil && st.Mode().IsRegular() {
			return true
		}
	}
	return false
}
