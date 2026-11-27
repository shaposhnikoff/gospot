package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"testing/fstest"
	"time"
)

func TestNativeSystemSnapshot(t *testing.T) {
	root := fstest.MapFS{
		"proc/sys/kernel/hostname":             {Data: []byte("pi-star\n")},
		"proc/sys/kernel/osrelease":            {Data: []byte("6.12.47+rpt-rpi-v8\n")},
		"sys/firmware/devicetree/base/model":   {Data: []byte("Raspberry Pi 4 Model B\x00")},
		"proc/uptime":                          {Data: []byte("1234.56 3456.78\n")},
		"proc/loadavg":                         {Data: []byte("0.25 0.50 1.25 2/100 1234\n")},
		"sys/class/thermal/thermal_zone0/temp": {Data: []byte("46500\n")},
		"proc/meminfo":                         {Data: []byte("MemTotal: 2048 kB\nMemFree: 128 kB\nMemAvailable: 1024 kB\nSwapTotal: 0 kB\nSwapFree: 0 kB\n")},
	}
	disk := func(path string, s *syscall.Statfs_t) error {
		if path != "/" {
			t.Fatalf("unexpected mount %q", path)
		}
		*s = syscall.Statfs_t{Bsize: 4096, Blocks: 1000, Bfree: 600, Bavail: 550}
		return nil
	}
	run := func(ctx context.Context, cmd string, args ...string) ([]byte, error) {
		if d, ok := ctx.Deadline(); cmd != "systemctl" || args[0] != "show" || !ok || time.Until(d) > 2*time.Second {
			t.Fatalf("expected bounded systemctl show")
		}
		return []byte("Id=dmrgateway.service\nActiveState=inactive\nLoadState=not-found\n\nId=mmdvmhost.service\nLoadState=loaded\nActiveState=active\n\nId=pistar-panel.service\nLoadState=loaded\nActiveState=unexpected-detail\n"), errors.New("missing unit")
	}
	s := readSystem(context.Background(), root, disk, run)
	if s.Hostname != "pi-star" || s.Model != "Raspberry Pi 4 Model B" || s.Kernel != "6.12.47+rpt-rpi-v8" ||
		s.Uptime == nil || *s.Uptime != 1234 || s.Load == nil || *s.Load != [3]float64{0.25, 0.5, 1.25} || s.Temp == nil || *s.Temp != 46.5 {
		t.Fatalf("hardware: %+v", s)
	}
	if s.Memory == nil || *s.Memory != (systemUsage{2097152, 1048576, 1048576}) || s.Swap == nil || *s.Swap != (systemUsage{}) ||
		s.RootDisk == nil || *s.RootDisk != (systemUsage{4096000, 1638400, 2252800}) {
		t.Fatalf("incorrect memory, disabled swap or reserved disk accounting: %+v", s)
	}
	states := map[string]string{}
	for _, service := range s.Services {
		states[service.Name] = service.State
	}
	if states["MMDVMHost"] != "active" || states["DMRGateway"] != "not-found" || states["Pi-Star Panel"] != "unknown" {
		t.Fatalf("unexpected service states: %v", states)
	}
}

func TestNativeSystemUnavailable(t *testing.T) {
	for _, tt := range []struct {
		name string
		root fstest.MapFS
	}{
		{"unmounted inputs", fstest.MapFS{}},
		{"malformed inputs", fstest.MapFS{
			"proc/uptime": {Data: []byte("NaN 0")}, "proc/loadavg": {Data: []byte("0 Inf 0")},
			"sys/class/thermal/thermal_zone0/temp": {Data: []byte("-273150")},
			"sys/firmware/devicetree/base/model":   {Data: []byte("model\nunexpected-detail")},
			"proc/meminfo":                         {Data: []byte("MemTotal: 9 kB\nMemAvailable: 10 kB\nSwapTotal: 0 kB\n")},
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			disk := func(string, *syscall.Statfs_t) error { return errors.New("unexpected-detail") }
			run := func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("unexpected-detail") }
			s := readSystem(context.Background(), tt.root, disk, run)
			if s.Hostname != "" || s.Model != "" || s.Kernel != "" || s.Uptime != nil || s.Temp != nil || s.Load != nil || s.Memory != nil || s.Swap != nil || s.RootDisk != nil {
				t.Fatalf("missing probe presented as a measurement: %+v", s)
			}
			for _, service := range s.Services {
				if service.State != "unknown" {
					t.Fatalf("missing systemd reported %v", service)
				}
			}
			b, err := json.Marshal(s)
			if err != nil || strings.Contains(string(b), "unexpected-detail") {
				t.Fatalf("invalid or excessive JSON: %s (%v)", b, err)
			}
		})
	}
}

func TestNativeSystemEndpointScope(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		code         int
	}{
		{"POST", "/manage/api/system", 405}, {"HEAD", "/manage/api/system", 405}, {"GET", "/api/system", 404},
	} {
		w := httptest.NewRecorder()
		serveSystemInfo(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != tc.code || (tc.code == 405 && w.Header().Get("Allow") != "GET") {
			t.Errorf("%s %s: %d, Allow %q", tc.method, tc.path, w.Code, w.Header().Get("Allow"))
		}
	}
}

func TestNativeSystemPartialSnapshotOnProbeTimeout(t *testing.T) {
	root := fstest.MapFS{"proc/uptime": {Data: []byte("0.25 0.01\n")}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	s := readSystem(ctx, root, func(string, *syscall.Statfs_t) error { return syscall.EACCES },
		func(ctx context.Context, _ string, _ ...string) ([]byte, error) {
			<-ctx.Done()
			// A killed child may have emitted some output before the deadline.
			return []byte("Id=mmdvmhost.service\nLoadState=loaded\nActiveState=active\n"), ctx.Err()
		})
	if ctx.Err() != context.DeadlineExceeded || s.Uptime == nil || *s.Uptime != 0 || s.RootDisk != nil {
		t.Fatalf("expected valid zero uptime and unavailable disk: %+v", s)
	}
	for _, svc := range s.Services {
		if svc.State != "unknown" {
			t.Fatalf("accepted a timed-out observation: %+v", svc)
		}
	}
}

func TestClassicServices(t *testing.T) {
	got := classicServices(map[string]bool{"MMDVM-Host": true, "timeserverd": true},
		[]string{"python3 " + pistarSbinDir + "/pistar-watchdog", "bash -c true"})
	want := []systemService{{"MMDVMHost", "running"}, {"DStarRepeater", "stopped"}, {"ircDDBGateway", "stopped"},
		{"TimeServer", "running"}, {"PiStar-Watchdog", "running"}, {"PiStar-Remote", "stopped"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v", got)
	}
}
