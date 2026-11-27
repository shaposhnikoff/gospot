package main

import (
	"context"
	"encoding/json"
	"io/fs"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
)

// Native scope of admin/sysinfo.php and dstarrepeater/system.php: report host
// health, RAM and the root filesystem, without enumerating arbitrary mounts,
// running gateway binaries for banners, or exposing addresses or gateway config.
type systemUsage struct {
	Total     uint64 `json:"totalBytes"`
	Used      uint64 `json:"usedBytes"`
	Available uint64 `json:"availableBytes"`
}

type systemService struct {
	Name  string `json:"name"`
	State string `json:"state"`
}

type systemSnapshot struct {
	At       time.Time       `json:"at"`
	Hostname string          `json:"hostname"`
	Model    string          `json:"model"`
	Kernel   string          `json:"kernel"`
	Uptime   *int64          `json:"uptimeSeconds"`
	Load     *[3]float64     `json:"loadAverage"`
	Temp     *float64        `json:"cpuTempC"`
	Memory   *systemUsage    `json:"memory"`
	Swap     *systemUsage    `json:"swap"`
	RootDisk *systemUsage    `json:"rootDisk"`
	Services []systemService `json:"services"`
	// Classic system.php fields: addresses (hostname -I), platformDetect.sh
	// output and the six process-based service cells.
	Addresses []string        `json:"addresses"`
	Platform  string          `json:"platform"`
	Classic   []systemService `json:"classicServices"`
}

// systemUnits is built on use: the radio unit names come from pistar.conf.
func systemUnits() []struct{ unit, name string } {
	return []struct{ unit, name string }{
		{modemUnit, "MMDVMHost"}, {masterUnit, "DMRGateway"}, {"pistar-panel.service", "Pi-Star Panel"},
		{"dstarrepeater.service", "DStarRepeater"}, {"ircddbgateway.service", "ircDDBGateway"},
		{"timeserver.service", "TimeServer"}, {"pistar-watchdog.service", "Pi-Star Watchdog"}, {"pistar-remote.service", "Pi-Star Remote"},
	}
}

func systemServices(ctx context.Context, run diagRun) []systemService {
	args := []string{"show", "--no-pager", "--property=Id,LoadState,ActiveState"}
	units := systemUnits()
	services := make([]systemService, len(units))
	for i, s := range units {
		args = append(args, s.unit)
		services[i] = systemService{s.name, "unknown"}
	}
	// systemctl can exit nonzero when some units don't exist; keep complete
	// records for the others. Never expose raw errors or arbitrary properties.
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, _ := run(ctx, "systemctl", args...)
	if ctx.Err() != nil {
		return services
	}
	for _, block := range strings.Split(string(out), "\n\n") {
		var id, load, active string
		for _, line := range strings.Split(block, "\n") {
			k, v, _ := strings.Cut(line, "=")
			switch k {
			case "Id":
				id = v
			case "LoadState":
				load = v
			case "ActiveState":
				active = v
			}
		}
		for i, s := range units {
			if id != s.unit {
				continue
			}
			switch load {
			case "not-found", "masked":
				services[i].State = load
			case "loaded":
				switch active {
				case "active", "inactive", "failed", "activating", "deactivating", "reloading":
					services[i].State = active
				}
			}
		}
	}
	return services
}

func systemText(root fs.FS, path string) string {
	b, err := fs.ReadFile(root, path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.TrimRight(string(b), "\x00"))
}

func systemLabel(root fs.FS, path string) string {
	s := systemText(root, path)
	if len(s) > 255 || strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return ""
	}
	return s
}

func systemMemory(text string) (*systemUsage, *systemUsage) {
	values := map[string]uint64{}
	for _, line := range strings.Split(text, "\n") {
		key, rest, _ := strings.Cut(line, ":")
		switch key {
		case "MemTotal", "MemAvailable", "SwapTotal", "SwapFree":
			f := strings.Fields(rest)
			if len(f) == 2 && f[1] == "kB" {
				if n, err := strconv.ParseUint(f[0], 10, 64); err == nil && n <= math.MaxUint64/1024 {
					values[key] = n * 1024
				}
			}
		}
	}
	usage := func(total, available string) *systemUsage {
		t, ok := values[total]
		a, known := values[available]
		if !ok || !known || a > t || (total == "MemTotal" && t == 0) {
			return nil
		}
		return &systemUsage{t, t - a, a}
	}
	return usage("MemTotal", "MemAvailable"), usage("SwapTotal", "SwapFree")
}

func systemRootDisk(statfs func(string, *syscall.Statfs_t) error) *systemUsage {
	var s syscall.Statfs_t
	if statfs("/", &s) != nil || s.Bsize <= 0 || s.Blocks == 0 || s.Bfree > s.Blocks || s.Bavail > s.Bfree {
		return nil
	}
	size := uint64(s.Bsize)
	if s.Blocks > math.MaxUint64/size {
		return nil
	}
	// Unlike Bfree, Bavail excludes blocks reserved for the superuser.
	return &systemUsage{s.Blocks * size, (s.Blocks - s.Bfree) * size, s.Bavail * size}
}

func readSystem(ctx context.Context, root fs.FS, statfs func(string, *syscall.Statfs_t) error, run diagRun) systemSnapshot {
	s := systemSnapshot{
		At: time.Now().UTC(), Hostname: systemLabel(root, "proc/sys/kernel/hostname"),
		Model: systemLabel(root, "sys/firmware/devicetree/base/model"), Kernel: systemLabel(root, "proc/sys/kernel/osrelease"),
		RootDisk: systemRootDisk(statfs),
	}
	if f := strings.Fields(systemText(root, "proc/uptime")); len(f) > 0 {
		if n, err := strconv.ParseFloat(f[0], 64); err == nil && n >= 0 && n < math.MaxInt64 {
			seconds := int64(n)
			s.Uptime = &seconds
		}
	}
	if f := strings.Fields(systemText(root, "proc/loadavg")); len(f) >= 3 {
		var load [3]float64
		s.Load = &load
		for i := range load {
			n, err := strconv.ParseFloat(f[i], 64)
			if err != nil || n < 0 || math.IsNaN(n) || math.IsInf(n, 0) {
				s.Load = nil
				break
			}
			load[i] = n
		}
	}
	// Raspberry Pi thermal_zone0 reports integer millidegrees Celsius.
	if n, err := strconv.ParseInt(systemText(root, "sys/class/thermal/thermal_zone0/temp"), 10, 64); err == nil && n >= -40000 && n <= 150000 {
		c := float64(n) / 1000
		s.Temp = &c
	}
	s.Memory, s.Swap = systemMemory(systemText(root, "proc/meminfo"))
	s.Services = systemServices(ctx, run)
	return s
}

var systemCache struct {
	sync.Mutex
	snapshot systemSnapshot
}

// serveSystemInfo is dispatched only by the TLS management handler after auth.
func serveSystemInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.URL.EscapedPath() != "/manage/api/system" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}
	// Share probes across viewers without starting a background worker.
	systemCache.Lock()
	if time.Since(systemCache.snapshot.At) >= 5*time.Second && r.Context().Err() == nil {
		next := readSystem(r.Context(), os.DirFS("/"), syscall.Statfs, runDiagnostic)
		next.Addresses, next.Platform, next.Classic = systemAddresses(), systemPlatform(r.Context()), classicServices(runningCommands())
		if r.Context().Err() == nil {
			systemCache.snapshot = next
		}
	}
	s := systemCache.snapshot
	systemCache.Unlock()
	if r.Context().Err() != nil {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s)
}

func systemAddresses() []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var out []string
	for _, a := range addrs {
		ip, ok := a.(*net.IPNet)
		if ok && !ip.IP.IsLoopback() && !ip.IP.IsLinkLocalUnicast() {
			out = append(out, ip.IP.String())
		}
	}
	return out
}

var platform struct {
	sync.Once
	text string
}

// systemPlatform runs Pi-Star's platformDetect.sh once; the board does not change.
func systemPlatform(ctx context.Context) string {
	platform.Do(func() {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		out, err := runDiagnostic(ctx, filepath.Join(pistarBinDir, "platformDetect.sh"))
		if err == nil {
			platform.text = strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
		}
	})
	return platform.text
}

// runningCommands lists process names (comm) and command lines.
func runningCommands() (names map[string]bool, cmdlines []string) {
	names = runningProcesses()
	entries, _ := os.ReadDir("/proc")
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		if b, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline")); err == nil && len(b) > 0 {
			cmdlines = append(cmdlines, strings.ReplaceAll(string(b), "\x00", " "))
		}
	}
	return names, cmdlines
}

// classicServices mirrors the Service Status row of system.php.
func classicServices(names map[string]bool, cmdlines []string) []systemService {
	state := func(ok bool) string {
		if ok {
			return "running"
		}
		return "stopped"
	}
	script := func(path string) bool {
		for _, c := range cmdlines {
			if strings.Contains(c, path) {
				return true
			}
		}
		return false
	}
	in := dashInputs{procs: names}
	return []systemService{
		{"MMDVMHost", state(in.running("MMDVMHost"))},
		{"DStarRepeater", state(names["dstarrepeaterd"])},
		{"ircDDBGateway", state(names["ircddbgatewayd"])},
		{"TimeServer", state(names["timeserverd"])},
		{"PiStar-Watchdog", state(script(filepath.Join(pistarSbinDir, "pistar-watchdog")))},
		{"PiStar-Remote", state(script(filepath.Join(pistarSbinDir, "pistar-remote")))},
	}
}
