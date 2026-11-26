package main

import (
	"strings"
	"testing"
)

func TestRadioFieldsBySection(t *testing.T) {
	v := radioInfo(strings.NewReader("[General]\nCallsign=UT3UDX\nId=12\nDuplex=0\n[Modem]\nRXFrequency=433800000\nTXFrequency=433812500\n[POCSAG]\nFrequency=1\nEnable=0\n[DMR]\nEnable=1\nColorCode=1\n[DMR Network]\nSlot1=0\nSlot2=1\nId=888\n"))
	if v.ID != "12" || v.RX != "433.800" || v.TX != "433.8125" || v.Slots != "TS2" || strings.Join(v.Modes, ",") != "DMR" {
		t.Fatalf("unexpected station data: %+v", v)
	}
}
