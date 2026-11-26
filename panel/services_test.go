package main

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDMRReopenWhileReadInProgress(t *testing.T) {
	slot := make(chan struct{}, 1)
	slot <- struct{}{} // Previous page is still reading back the saved file.
	result := make(chan int, 1)
	started := make(chan struct{})
	go func() {
		r := httptest.NewRequest("GET", "/manage/api/services", nil)
		w := httptest.NewRecorder()
		close(started)
		serveDMRServices(w, r, func(_ context.Context, action string, _ []byte) ([]byte, error) {
			if action != "services-read" {
				t.Errorf("reopening called %q", action)
			}
			return []byte(`{"cc":12,"rules":[],"revision":"` + strings.Repeat("a", 64) + `"}`), nil
		}, slot)
		result <- w.Code
	}()
	<-started
	select {
	case code := <-result:
		t.Fatalf("read returned %d while the previous read held the slot", code)
	case <-time.After(50 * time.Millisecond):
	}
	<-slot
	select {
	case code := <-result:
		if code != 200 {
			t.Fatalf("reopened editor: HTTP %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reopened editor did not resume")
	}
}

func TestDMRCancelQueuedRead(t *testing.T) {
	slot := make(chan struct{}, 1)
	slot <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/manage/api/services", nil).WithContext(ctx)
	serveDMRServices(w, r, func(context.Context, string, []byte) ([]byte, error) {
		t.Fatal("cancelled queued read called helper")
		return nil, nil
	}, slot)
	if w.Code != 503 || len(slot) != 1 {
		t.Fatalf("HTTP %d; cancelled read disturbed occupied slot: %d", w.Code, len(slot))
	}
}
