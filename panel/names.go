package main

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// Operator and talkgroup names for heard rows, from the lists Pi-Star's
// HostFilesUpdate.sh keeps in PISTAR_HOSTS_DIR:
//
//   - DMRIds.dat (pistar.uk/downloads/DMRIds.dat.gz, ~250k lines), read the
//     way MMDVMHost's CUserDB does: fields split on ',' or TAB, an optional
//     first-line header naming the columns (RADIO_ID,CALLSIGN,FIRST_NAME,...),
//     otherwise RADIO_ID, CALLSIGN, FIRST_NAME; '#' starts a comment line.
//   - TGList_BM.txt, TGList_P25.txt, TGList_NXDN.txt: "Dest ID;Option;Name;
//     Description" lines (Option: TG 0, REF 1, PC 2).
//
// The files are downloaded from the internet, so names are bounded and
// stripped of control characters before they reach the JSON.

const (
	userDBFile    = "DMRIds.dat"
	userDBLimit   = 64 << 20 // read bound; the pistar.uk file is ~3 MiB, CSV variants with more columns are larger
	nameMaxRunes  = 40
	userDBMaxLine = 512
)

// userDB keeps no pointers (the garbage collector never scans it) and stores
// each distinct name once: ~13 MiB for 250k IDs (6.5 MiB for the
// 145k-line pistar.uk file) instead of ~36 MiB as string maps.
type userDB struct {
	byID   map[uint32]uint32 // DMR ID -> name offset
	byCall map[uint64]uint32 // callKey -> name offset of the first listed ID
	names  []byte            // length-prefixed names; offset 0 is ""
}

var userDBKeys = map[string]bool{"RADIO_ID": true, "CALLSIGN": true, "FIRST_NAME": true, "LAST_NAME": true, "CITY": true, "STATE": true, "COUNTRY": true}

// userFields mirrors CUserDB::tokenize: every ',', TAB, CR or LF ends a
// field, so empty fields keep their column number.
func userFields(line []byte, out [][]byte) [][]byte {
	out = out[:0]
	for len(line) > 0 {
		i := bytes.IndexAny(line, ",\t\r\n")
		if i < 0 {
			out = append(out, line)
			break
		}
		out = append(out, line[:i])
		line = line[i+1:]
	}
	return out
}

// cleanName keeps printable text, collapses blanks and bounds the length.
func cleanName(s string) string {
	clean := len(s) <= nameMaxRunes && strings.TrimSpace(s) == s && !strings.Contains(s, "  ")
	for i := 0; clean && i < len(s); i++ {
		if c := s[i]; c < 0x20 || c >= 0x7f {
			clean = false
		}
	}
	if clean {
		return s
	}
	var b strings.Builder
	n, space := 0, false
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			space = b.Len() > 0
			continue
		case r == utf8.RuneError || !unicode.IsPrint(r):
			continue
		}
		if space {
			if n+2 > nameMaxRunes {
				break
			}
			b.WriteByte(' ')
			n, space = n+1, false
		}
		if n >= nameMaxRunes {
			break
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}

// callKey packs a heard or listed callsign into a number: upper case,
// without a portable/SSID suffix ("M1ABC/P", "MW0MWZ-1") or D-Star module
// ("M1ABC  B"); [A-Z0-9]{1,10} in base 37. 0 means "not a callsign".
func callKey[T string | []byte](v T) uint64 {
	s := bytes.TrimSpace([]byte(v))
	if i := bytes.IndexAny(s, "/- "); i >= 0 {
		s = s[:i]
	}
	if len(s) == 0 || len(s) > 10 {
		return 0
	}
	var k uint64
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c >= '0' && c <= '9':
			k = k*37 + uint64(c-'0') + 1
		case c >= 'A' && c <= 'Z':
			k = k*37 + uint64(c-'A') + 11
		case c >= 'a' && c <= 'z':
			k = k*37 + uint64(c-'a') + 11
		default:
			return 0
		}
	}
	return k
}

// userID parses a decimal DMR ID (1..2^32-1) without allocating.
func userID(b []byte) uint32 {
	if len(b) == 0 || len(b) > 10 {
		return 0
	}
	var n uint64
	for _, c := range b {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + uint64(c-'0')
	}
	if n > 1<<32-1 {
		return 0
	}
	return uint32(n)
}

func parseUserDB(r io.Reader) *userDB {
	db := &userDB{byID: map[uint32]uint32{}, byCall: map[uint64]uint32{}, names: []byte{0}}
	intern := map[string]uint32{"": 0} // raw name -> offset, only while parsing
	idCol, callCol, firstCol, lastCol := 0, 1, 2, -1
	br := bufio.NewReaderSize(r, 64<<10)
	var fields [][]byte
	var raw []byte
	field := func(i int) []byte {
		if i >= 0 && i < len(fields) {
			return bytes.TrimSpace(fields[i])
		}
		return nil
	}
	for first := true; ; first = false {
		line, err := br.ReadSlice('\n')
		for err == bufio.ErrBufferFull { // an overlong line is skipped whole
			line = nil
			_, err = br.ReadSlice('\n')
		}
		if len(line) > userDBMaxLine {
			line = nil
		}
		fields = userFields(line, fields)
		if first {
			header := map[string]int{"LAST_NAME": -1, "FIRST_NAME": -1}
			for i, f := range fields {
				if key := string(f); userDBKeys[key] {
					header[key] = i
				}
			}
			if id, ok := header["RADIO_ID"]; ok {
				if call, ok := header["CALLSIGN"]; ok {
					idCol, callCol, firstCol, lastCol, line = id, call, header["FIRST_NAME"], header["LAST_NAME"], nil
				}
			}
		}
		if len(line) > 0 && line[0] != '#' {
			id, call := userID(field(idCol)), callKey(field(callCol))
			if id != 0 && call != 0 {
				raw = append(raw[:0], field(firstCol)...)
				if last := field(lastCol); len(last) > 0 {
					raw = append(append(raw, ' '), last...)
				}
				off, ok := intern[string(raw)]
				if !ok {
					name := cleanName(string(raw))
					off = uint32(len(db.names))
					db.names = append(append(db.names, byte(len(name))), name...)
					intern[string(raw)] = off
				}
				// MMDVMHost's table assignment: a repeated ID takes the later line.
				db.byID[id] = off
				if old, seen := db.byCall[call]; !seen || old == 0 {
					db.byCall[call] = off
				}
			}
		}
		if err != nil {
			break
		}
	}
	db.names = slices.Clip(db.names)
	return db
}

func (db *userDB) text(off uint32) string {
	n := uint32(db.names[off])
	return string(db.names[off+1 : off+1+n])
}

func (db *userDB) name(from string) string {
	if db == nil {
		return ""
	}
	if id := userID([]byte(from)); id != 0 {
		return db.text(db.byID[id])
	}
	if k := callKey(from); k != 0 {
		return db.text(db.byCall[k])
	}
	return ""
}

// backgroundUserDB serves the last parsed version of DMRIds.dat and parses a
// changed version (path, modification time, size; as cachedHostFile) in the
// background, so a daily HostFilesUpdate.sh never stalls a dashboard build.
type backgroundUserDB struct {
	mu      sync.Mutex
	path    string
	mod     time.Time
	size    int64
	db      *userDB
	loading bool
	done    *sync.Cond
	parse   func(io.Reader) *userDB
}

var operatorNames = &backgroundUserDB{parse: parseUserDB}

// get returns the table for the current file, or the previous one while a
// new version is being parsed; nil when the file is missing.
func (b *backgroundUserDB) get(path string) *userDB {
	st, err := os.Stat(path)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.done == nil {
		b.done = sync.NewCond(&b.mu)
	}
	if err != nil || !st.Mode().IsRegular() {
		if !b.loading {
			b.path, b.db = "", nil
		}
		return nil
	}
	current := b.path == path && b.mod.Equal(st.ModTime()) && b.size == st.Size()
	if !current && !b.loading {
		b.loading = true
		go b.load(path)
	}
	if b.path != path {
		return nil
	}
	return b.db
}

func (b *backgroundUserDB) load(path string) {
	var db *userDB
	var mod time.Time
	var size int64
	if f, err := os.Open(path); err == nil {
		if st, err := f.Stat(); err == nil && st.Mode().IsRegular() {
			mod, size = st.ModTime(), st.Size()
			db = b.parse(io.LimitReader(f, userDBLimit))
		}
		f.Close()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.loading = false
	if db != nil {
		b.path, b.mod, b.size, b.db = path, mod, size, db
	}
	b.done.Broadcast()
}

// wait blocks until no parse is running (tests and start-up warm-up).
func (b *backgroundUserDB) wait() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for b.loading {
		b.done.Wait()
	}
}

// parseTGList reads "id;option;name;description" lines; the first entry for
// an ID wins, as in YSF2DMR's list order.
func parseTGList(r io.Reader) map[uint32]string {
	out := map[uint32]string{}
	scan := bufio.NewScanner(r)
	scan.Buffer(make([]byte, 0, 4096), 4096)
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		parts := strings.Split(line, ";")
		if len(parts) < 2 {
			continue
		}
		id, err := strconv.ParseUint(strings.TrimSpace(parts[0]), 10, 32)
		if err != nil {
			continue
		}
		name := parts[1]
		// "id;option;name": the option is a single digit.
		if opt := strings.TrimSpace(parts[1]); len(parts) >= 3 && len(opt) == 1 && opt[0] >= '0' && opt[0] <= '9' {
			name = parts[2]
		}
		if name = cleanName(strings.TrimSpace(name)); name != "" {
			if _, seen := out[uint32(id)]; !seen {
				out[uint32(id)] = name
			}
		}
	}
	return out
}

var tgListByMode = map[string]string{"DMR": "TGList_BM.txt", "P25": "TGList_P25.txt", "NXDN": "TGList_NXDN.txt"}

// heardNames resolves the operator and target names shown with heard rows.
type heardNames interface {
	operator(from string) string
	target(mode, to string) string
}

// hostNames holds the PISTAR_HOSTS_DIR lists for one dashboard build: one
// stat per file, a parse only when a file changed.
type hostNames struct {
	users *userDB
	tg    map[string]map[uint32]string // base mode -> TG list
}

func currentHostNames() hostNames {
	h := hostNames{users: operatorNames.get(filepath.Join(pistarHostsDir, userDBFile)), tg: map[string]map[uint32]string{}}
	for mode, file := range tgListByMode {
		h.tg[mode], _ = cachedHostFile("tglist", filepath.Join(pistarHostsDir, file), parseTGList)
	}
	return h
}

func (h hostNames) operator(from string) string { return h.users.name(from) }

func (h hostNames) talkgroup(mode, id string) string {
	n, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		return ""
	}
	return h.tg[mode][uint32(n)]
}

// target names a talkgroup ("TG 91" -> TGList), a DMR private call (a
// listed ID such as 9990 Parrot, otherwise the user's name) or nothing.
// mode is the base mode (trxMode): DMR, P25, NXDN, ...
func (h hostNames) target(mode, to string) string {
	if tg, ok := strings.CutPrefix(to, "TG "); ok {
		return h.talkgroup(mode, strings.TrimSpace(tg))
	}
	if mode != "DMR" {
		return ""
	}
	if name := h.talkgroup(mode, to); name != "" {
		return name
	}
	return h.users.name(to)
}
