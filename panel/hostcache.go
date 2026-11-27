package main

import (
	"io"
	"os"
	"slices"
	"sync"
	"time"
)

// Host lists (DMR_Hosts.txt, YSFHosts.txt, ...) are a few thousand lines and
// change once a day (HostFilesUpdate.sh); parse each one once per version
// (path, modification time, size) instead of on every page build.
type hostCacheEntry struct {
	mod  time.Time
	size int64
	val  any
}

var hostCache sync.Map // kind + "\x00" + path -> hostCacheEntry

func cachedHostFile[T any](kind, path string, parse func(io.Reader) T) (T, bool) {
	var zero T
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() {
		return zero, false
	}
	key := kind + "\x00" + path
	if e, ok := hostCache.Load(key); ok {
		if e := e.(hostCacheEntry); e.mod.Equal(st.ModTime()) && e.size == st.Size() {
			return e.val.(T), true
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return zero, false
	}
	defer f.Close()
	v := parse(io.LimitReader(f, 8<<20))
	hostCache.Store(key, hostCacheEntry{st.ModTime(), st.Size(), v})
	return v, true
}

// cloneRows returns a copy callers may modify without touching the cache.
func cloneRows[T any](rows []T) []T { return slices.Clone(rows) }
