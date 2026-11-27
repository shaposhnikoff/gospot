package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
)

// Pi-Star layout: MMDVMHost and DMRGateway write daily files named after the
// UTC date (FileRotate=1), e.g. /var/log/pi-star/MMDVM-2026-11-27.log.
// One poller per file follows appends and moves to the next day's file.

const (
	tailPoll  = 500 * time.Millisecond
	tailPrime = 512 << 10 // bytes read from the end of a file when attaching
)

// "M: 2026-11-27 06:19:52.211 GoDMRServer, Logged into ..."
var logStamp = regexp.MustCompile(`^[A-Z]: ([0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{3}) `)

type logFile struct {
	dir, root, unit string
	// file state
	path   string
	offset int64
	rest   []byte
}

func (f *logFile) name(day time.Time) string {
	return filepath.Join(f.dir, f.root+"-"+day.UTC().Format("2006-01-02")+".log")
}

// poll reads whatever was appended since the last call and hands complete
// lines to emit. On a new UTC day it finishes the old file first.
func (f *logFile) poll(now time.Time, emit func(unit, line string, at time.Time)) {
	want := f.name(now)
	if f.path != want {
		if f.path != "" {
			f.read(emit, false) // drain the previous day
		} else if prev := f.name(now.Add(-24 * time.Hour)); fileExists(prev) {
			// First attach: yesterday's tail gives history right after midnight.
			f.path, f.offset, f.rest = prev, -1, nil
			f.read(emit, true)
		}
		f.path, f.offset, f.rest = want, -1, nil
	}
	f.read(emit, f.offset < 0)
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular()
}

func (f *logFile) read(emit func(unit, line string, at time.Time), prime bool) {
	file, err := os.Open(f.path)
	if err != nil {
		return
	}
	defer file.Close()
	st, err := file.Stat()
	if err != nil {
		return
	}
	size := st.Size()
	if f.offset < 0 || size < f.offset { // first attach or truncated/replaced
		f.offset, f.rest = 0, nil
		if prime && size > tailPrime {
			f.offset = size - tailPrime
		}
	}
	if size == f.offset {
		return
	}
	if _, err := file.Seek(f.offset, io.SeekStart); err != nil {
		return
	}
	chunk, err := io.ReadAll(io.LimitReader(file, size-f.offset))
	if err != nil {
		return
	}
	skipFirst := f.offset > 0 && f.rest == nil && prime
	f.offset += int64(len(chunk))
	data := append(f.rest, chunk...)
	lines := bytes.Split(data, []byte{'\n'})
	f.rest = append([]byte(nil), lines[len(lines)-1]...)
	for i, raw := range lines[:len(lines)-1] {
		if i == 0 && skipFirst {
			continue // started mid-line
		}
		line := string(bytes.TrimRight(raw, "\r"))
		m := logStamp.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		at, err := time.ParseInLocation("2006-01-02 15:04:05.000", m[1], time.UTC)
		if err != nil {
			continue
		}
		emit(f.unit, line, at)
	}
}

// followFiles feeds the same consumers as the journal follower.
func followFiles(ctx context.Context) {
	files := []*logFile{
		{dir: pistarLogDir, root: "MMDVM", unit: modemUnit},
		{dir: pistarLogDir, root: "DMRGateway", unit: masterUnit},
	}
	emit := func(unit, line string, at time.Time) {
		applyRecord(followRecord{Message: line, Unit: unit, Invocation: "file",
			Stamp: strconv.FormatInt(at.UnixMicro(), 10)}, time.Now())
	}
	primeFirmwareFiles(files[0], time.Now())
	ticker := time.NewTicker(tailPoll)
	defer ticker.Stop()
	for {
		now := time.Now()
		for _, f := range files {
			f.poll(now, emit)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// primeFirmwareFiles finds the last modem banner in yesterday's and today's
// log, like the classic getDVModemFirmware(); the banner is written once at
// start-up, usually far before the tail the follower begins with.
func primeFirmwareFiles(f *logFile, now time.Time) {
	for _, day := range []time.Time{now.Add(-24 * time.Hour), now} {
		file, err := os.Open(f.name(day))
		if err != nil {
			continue
		}
		data, _ := io.ReadAll(io.LimitReader(file, 64<<20))
		file.Close()
		if i := bytes.LastIndex(data, []byte("MMDVM protocol version")); i >= 0 {
			start := bytes.LastIndexByte(data[:i], '\n') + 1
			end := bytes.IndexByte(data[i:], '\n')
			if end < 0 {
				end = len(data) - i
			}
			journal.observe(f.unit, string(data[start:i+end]))
		}
	}
}

// followRadioLogs picks the configured log source.
func followRadioLogs(ctx context.Context) {
	if radioLogSource == "journal" {
		followJournal(ctx)
		return
	}
	followFiles(ctx)
}
