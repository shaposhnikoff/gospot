package main

import (
	"bytes"
	"encoding/hex"
	"math"
	"reflect"
	"strings"
	"testing"
)

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Vectors produced by python3-msgpack 1.0.3 (msgpack.packb(v, use_bin_type=True)).
func TestMsgpackDecodeVectors(t *testing.T) {
	audio := make([]byte, 160)
	for i := range audio {
		audio[i] = byte(i)
	}
	for _, c := range []struct {
		name, hex string
		want      any
	}{
		{"subscribe", "9201915b", []any{int64(1), []any{int64(91)}}},
		{"call start", "9a0bcf0000011f71fb04cbce0023b4a15ba54d3041424381a4736c6f7402c0c3ffcb3ff8000000000000",
			[]any{int64(11), int64(1234567890123), int64(2340001), int64(91), "M0ABC", mpMap{{"slot", int64(2)}}, nil, true, int64(-1), 1.5}},
		{"call end", "920dcf0000011f71fb04cb", []any{int64(13), int64(1234567890123)}},
		{"audio", "9214c4a0" + hex.EncodeToString(audio), []any{int64(20), audio}},
		{"integers", "dc0014007fcc80ccffcd0100cdffffce00010000ceffffffffcf0000000100000000cfffffffffffffffffffe0d0dfd080d1ff7fd18000d2ffff7fffd280000000d3ffffffff7fffffffd38000000000000000",
			[]any{int64(0), int64(127), int64(128), int64(255), int64(256), int64(65535), int64(65536), int64(4294967295), int64(4294967296),
				uint64(math.MaxUint64), int64(-1), int64(-32), int64(-33), int64(-128), int64(-129), int64(-32768), int64(-32769),
				int64(math.MinInt32), int64(math.MinInt32 - 1), int64(math.MinInt64)}},
		{"array16", "dc0010000102030405060708090a0b0c0d0e0f", []any{int64(0), int64(1), int64(2), int64(3), int64(4), int64(5), int64(6), int64(7),
			int64(8), int64(9), int64(10), int64(11), int64(12), int64(13), int64(14), int64(15)}},
		{"map", "82a16101029103", mpMap{{"a", int64(1)}, {int64(2), []any{int64(3)}}}},
		{"float32", "ca3f000000", 0.5},
		{"fixext4", "d60561626364", mpExt{5, []byte("abcd")}},
		{"str8", "d920" + strings.Repeat("62", 32), strings.Repeat("b", 32)},
		{"bin16", "c50100" + strings.Repeat("78", 256), bytes.Repeat([]byte("x"), 256)},
		{"utf8", "a2c3a9", "é"},
	} {
		got, err := msgpackDecode(unhex(t, c.hex))
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %#v %v, want %#v", c.name, got, err, c.want)
		}
	}
}

func TestMsgpackRejectsMalformed(t *testing.T) {
	for name, h := range map[string]string{
		"empty": "", "truncated array": "92", "truncated bin": "c405aa", "reserved c1": "c1",
		"trailing": "9201915b00", "huge array": "ddffffffff", "huge map": "dfffffffff",
		"truncated uint32": "ce0001", "deep": strings.Repeat("91", 40) + "00",
	} {
		if v, err := msgpackDecode(unhex(t, h)); err == nil {
			t.Errorf("%s accepted: %#v", name, v)
		}
	}
}

func TestHoselineSubscribeEncoding(t *testing.T) {
	for tg, want := range map[int]string{91: "9201915b", 200: "920191ccc8", 2504: "920191cd09c8", 99999999: "920191ce05f5e0ff"} {
		if got := hex.EncodeToString(hoselineSubscribe(tg)); got != want {
			t.Errorf("TG %d: %s, want %s", tg, got, want)
		}
	}
}

// RFC 6455 section 1.3 and 5.7 examples.
func TestWebSocketVectors(t *testing.T) {
	if got := wsAccept("dGhlIHNhbXBsZSBub25jZQ=="); got != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" {
		t.Fatalf("accept key %s", got)
	}
	if got := hex.EncodeToString(wsFrame(wsText, []byte("Hello"), nil)); got != "810548656c6c6f" {
		t.Errorf("unmasked text %s", got)
	}
	mask := [4]byte{0x37, 0xfa, 0x21, 0x3d}
	if got := hex.EncodeToString(wsFrame(wsText, []byte("Hello"), &mask)); got != "818537fa213d7f9f4d5158" {
		t.Errorf("masked text %s", got)
	}
	if got := hex.EncodeToString(wsFrame(wsBinary, make([]byte, 256), nil)[:4]); got != "827e0100" {
		t.Errorf("256-byte binary header %s", got)
	}
	if got := hex.EncodeToString(wsFrame(wsBinary, make([]byte, 65536), nil)[:10]); got != "827f0000000000010000" {
		t.Errorf("64 KiB binary header %s", got)
	}
}
