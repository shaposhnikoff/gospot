package main

import (
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkConfigurePage builds the Configuration page from real board files:
// GOSPOT_ROUNDTRIP_DIR (configs, "marker") and GOSPOT_BENCH_HOSTS (host lists).
func BenchmarkConfigurePage(b *testing.B) {
	dir, hosts := os.Getenv("GOSPOT_ROUNDTRIP_DIR"), os.Getenv("GOSPOT_BENCH_HOSTS")
	if dir == "" || hosts == "" {
		b.Skip("set GOSPOT_ROUNDTRIP_DIR and GOSPOT_BENCH_HOSTS")
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
	old := gospotHostsDir
	gospotHostsDir = hosts
	defer func() { gospotHostsDir = old }()
	c := &cfgContext{St: ctlState(files, map[string]*string{"mmdvmhost": marker, "dstarrepeater": nil})}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buildCfgPage(c)
	}
}
