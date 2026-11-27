package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The helper read is reused while the watched files keep their stat
// signature, re-run after any change, and dropped by every other command.
func TestConfigureReadCache(t *testing.T) {
	dir := t.TempDir()
	count := filepath.Join(dir, "count")
	sudo := filepath.Join(dir, "sudo")
	// fake sudo: drop -n, run the helper; the fake helper counts its runs.
	if err := os.WriteFile(sudo, []byte("#!/bin/sh\nshift\nexec \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(dir, "pistar-configure")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\necho x >> "+count+"\necho \"{\\\"verb\\\":\\\"$1\\\"}\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	watched := filepath.Join(dir, "mmdvmhost")
	if err := os.WriteFile(watched, []byte("[General]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldSudo, oldHelper, oldWatch := configureSudo, configureHelper, configWatch
	t.Cleanup(func() { configureSudo, configureHelper, configWatch = oldSudo, oldHelper, oldWatch })
	configureSudo, configureHelper, configWatch = sudo, helper, []string{watched, filepath.Join(dir, "missing-marker")}
	configReadCache.sig, configReadCache.body = "", nil
	runs := func() int {
		b, _ := os.ReadFile(count)
		return strings.Count(string(b), "x")
	}
	read := func() string {
		out, err := runConfigureHelper(context.Background(), "read", nil)
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(out))
	}
	if read() != `{"verb":"read"}` || read() != `{"verb":"read"}` || runs() != 1 {
		t.Fatalf("second read must come from the cache: %d runs", runs())
	}
	later := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(watched, later, later); err != nil {
		t.Fatal(err)
	}
	read()
	if runs() != 2 {
		t.Fatalf("a changed file must re-run the helper: %d", runs())
	}
	if err := os.WriteFile(filepath.Join(dir, "missing-marker"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	read()
	if runs() != 3 {
		t.Fatalf("a new marker must re-run the helper: %d", runs())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // a write still runs when the browser has gone
	if out, err := runConfigureHelper(ctx, "apply", nil); err != nil || !strings.Contains(string(out), "apply") {
		t.Fatalf("apply: %v %s", err, out)
	}
	read()
	if runs() != 5 {
		t.Fatalf("apply must drop the cache: %d", runs())
	}
}
