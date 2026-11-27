package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

type RadioInfo struct {
	Callsign, ID, RX, TX, Operation, CC, Slots string
	Modes                                      []string
}

func readRadio() RadioInfo {
	f, err := os.Open(mmdvmINI.location())
	if err != nil {
		return RadioInfo{}
	}
	defer f.Close()
	return radioInfo(io.LimitReader(f, 512<<10))
}

// Export only display fields from the host configuration.
func radioInfo(r io.Reader) RadioInfo {
	var v RadioInfo
	var section string
	var slots []string
	s := bufio.NewScanner(r)
	for s.Scan() {
		line := strings.TrimSpace(strings.SplitN(s.Text(), "#", 2)[0])
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.Trim(line, "[]")
			continue
		}
		k, x, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		x = strings.Trim(strings.TrimSpace(x), `"`)
		switch section + "/" + strings.TrimSpace(k) {
		case "General/Callsign":
			v.Callsign = x
		case "General/Id":
			v.ID = x
		case "General/Duplex":
			if x == "0" {
				v.Operation = "Simplex"
			} else if x == "1" {
				v.Operation = "Duplex"
			}
		case "DMR/ColorCode":
			v.CC = x
		case "Modem/RXFrequency", "Modem/TXFrequency":
			hz, e := strconv.ParseUint(x, 10, 32)
			if e == nil && hz > 0 {
				mhz := strings.TrimRight(fmt.Sprintf("%.6f", float64(hz)/1e6), "0")
				if len(mhz)-strings.IndexByte(mhz, '.') <= 3 {
					mhz = fmt.Sprintf("%.3f", float64(hz)/1e6)
				}
				if strings.TrimSpace(k) == "RXFrequency" {
					v.RX = mhz
				} else {
					v.TX = mhz
				}
			}
		case "DMR Network/Slot1", "DMR Network/Slot2":
			if x == "1" {
				slots = append(slots, "TS"+strings.TrimPrefix(strings.TrimSpace(k), "Slot"))
			}
		case "D-Star/Enable", "DMR/Enable", "System Fusion/Enable", "P25/Enable", "NXDN/Enable", "POCSAG/Enable", "FM/Enable":
			if x == "1" {
				v.Modes = append(v.Modes, section)
			}
		}
	}
	v.Slots = strings.Join(slots, " / ")
	return v
}
