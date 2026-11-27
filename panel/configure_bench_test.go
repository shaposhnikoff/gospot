package main

import (
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkConfigurePage builds the Configuration page from real board files:
// PISTAR_ROUNDTRIP_DIR (configs, "marker") and PISTAR_BENCH_HOSTS (host lists).
func BenchmarkConfigurePage(b *testing.B) {
	dir, hosts := os.Getenv("PISTAR_ROUNDTRIP_DIR"), os.Getenv("PISTAR_BENCH_HOSTS")
	if dir == "" || hosts == "" {
		b.Skip("set PISTAR_ROUNDTRIP_DIR and PISTAR_BENCH_HOSTS")
	}
	files := map[string]string{}
	var marker *string
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		data, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		s := string(data)
		if e.Name() == "marker" {
			marker = &s
			continue
		}
		files[e.Name()] = s
	}
	old := pistarHostsDir
	pistarHostsDir = hosts
	defer func() { pistarHostsDir = old }()
	c := &cfgContext{St: ctlState(files, map[string]*string{"mmdvmhost": marker, "dstarrepeater": nil})}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buildCfgPage(c)
	}
}
