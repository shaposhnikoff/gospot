package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGatewayLinkParsing(t *testing.T) {
	// Newest first, as gatewayLogLines returns them.
	ysf := map[string][]string{
		"Not Linked":    {"M: 2026-11-27 06:00:03.000 Link has failed, polls lost", "M: 2026-11-27 06:00:01.000 Linked to GB SOUTH WEST"},
		"GB SOUTH WEST": {"M: 2026-11-27 06:00:01.000 Linked to GB SOUTH WEST   ", "M: 2026-11-27 06:00:00.000 Opening YSF network connection"},
		"FCS00290":      {"M: 2026-11-27 06:00:01.000 Linked to FCS002-90"},
		"62829":         {"M: 2026-11-27 06:00:01.000 Connect to 62829 has been requested by M1ABC"},
		"16710":         {`M: 2026-11-27 06:00:01.000 Automatic (re-)connection to 16710 - "GB SOUTH WEST   "`},
	}
	for want, lines := range ysf {
		if got := ysfLink(lines); got != want {
			t.Errorf("ysf %q: got %q", want, got)
		}
	}
	tg := map[string][]string{
		"TG 10100":   {"M: 2026-11-27 06:00:01.000 Linked at startup to reflector 10100"},
		"TG 10200":   {"M: 2026-11-27 06:00:02.000 Switched to reflector 10200 by remote command", "M: 2026-11-27 06:00:01.000 Unlinked from reflector 10100 by M1ABC"},
		"Not Linked": {"M: 2026-11-27 06:00:02.000 No response from 10200, unlinking", "M: 2026-11-27 06:00:01.000 Linked to reflector 10200 by M1ABC"},
	}
	for want, lines := range tg {
		if got := reflectorLink(lines, tgReflector, tgUnlinked, "TG "); got != want {
			t.Errorf("tg %q: got %q", want, got)
		}
	}
	if got := reflectorLink([]string{"M: x Linked to reflector M17-M17 C"}, m17Linked, m17NotLinked, ""); got != "M17-M17 C" {
		t.Errorf("m17: %q", got)
	}
}

func TestYSFRoomName(t *testing.T) {
	saved := gospotHostsDir
	defer func() { gospotHostsDir = saved }()
	gospotHostsDir = t.TempDir()
	os.WriteFile(filepath.Join(gospotHostsDir, "YSFHosts.txt"), []byte("16710;GB_SOUTH_WEST_REGIONAL;South West;1.2.3.4;42000;003;\n"), 0o644)
	if got := ysfRoomName("16710"); got != "GB SOUTH WEST REG.." {
		t.Fatalf("%q", got)
	}
	if got := ysfRoomName("99999"); got != "99999" || ysfRoomName("Not Linked") != "Not Linked" {
		t.Fatalf("%q", got)
	}
}
