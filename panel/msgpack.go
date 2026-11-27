package main

// A small MessagePack codec for the Hoseline spotter protocol: the board sends
// [1,[TG]] and reads [11, …, source, …], [13, …] and [20, <bin μ-law>].
// Integers decode to int64 (uint64 above MaxInt64), str to string, bin to
// []byte, arrays to []any and maps to mpMap; ext values decode to mpExt.

import (
	"encoding/binary"
	"errors"
	"math"
)

type mpMap [][2]any

type mpExt struct {
	Type int8
	Data []byte
}

var errMsgpack = errors.New("msgpack: malformed data")

const mpMaxDepth = 16

type mpDecoder struct{ b []byte }

// msgpackDecode decodes exactly one value occupying all of b.
func msgpackDecode(b []byte) (any, error) {
	d := mpDecoder{b}
	v, err := d.value(0)
	if err != nil {
		return nil, err
	}
	if len(d.b) != 0 {
		return nil, errMsgpack
	}
	return v, nil
}

func (d *mpDecoder) take(n uint64) ([]byte, error) {
	if n > uint64(len(d.b)) {
		return nil, errMsgpack
	}
	out := d.b[:n]
	d.b = d.b[n:]
	return out, nil
}

func (d *mpDecoder) uint(size int) (uint64, error) {
	b, err := d.take(uint64(size))
	if err != nil {
		return 0, err
	}
	switch size {
	case 1:
		return uint64(b[0]), nil
	case 2:
		return uint64(binary.BigEndian.Uint16(b)), nil
	case 4:
		return uint64(binary.BigEndian.Uint32(b)), nil
	}
	return binary.BigEndian.Uint64(b), nil
}

func (d *mpDecoder) bytes(n uint64) ([]byte, error) {
	b, err := d.take(n)
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), b...), nil
}

func (d *mpDecoder) array(n uint64, depth int) (any, error) {
	if n > uint64(len(d.b)) { // every element takes at least one byte
		return nil, errMsgpack
	}
	out := make([]any, 0, n)
	for i := uint64(0); i < n; i++ {
		v, err := d.value(depth + 1)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (d *mpDecoder) mapping(n uint64, depth int) (any, error) {
	if n > uint64(len(d.b))/2 {
		return nil, errMsgpack
	}
	out := make(mpMap, 0, n)
	for i := uint64(0); i < n; i++ {
		k, err := d.value(depth + 1)
		if err != nil {
			return nil, err
		}
		v, err := d.value(depth + 1)
		if err != nil {
			return nil, err
		}
		out = append(out, [2]any{k, v})
	}
	return out, nil
}

func (d *mpDecoder) ext(n uint64) (any, error) {
	t, err := d.take(1)
	if err != nil {
		return nil, err
	}
	data, err := d.bytes(n)
	if err != nil {
		return nil, err
	}
	return mpExt{int8(t[0]), data}, nil
}

func (d *mpDecoder) value(depth int) (any, error) {
	if depth > mpMaxDepth {
		return nil, errMsgpack
	}
	head, err := d.take(1)
	if err != nil {
		return nil, err
	}
	c := head[0]
	switch {
	case c <= 0x7f:
		return int64(c), nil
	case c >= 0xe0:
		return int64(int8(c)), nil
	case c&0xf0 == 0x80:
		return d.mapping(uint64(c&0x0f), depth)
	case c&0xf0 == 0x90:
		return d.array(uint64(c&0x0f), depth)
	case c&0xe0 == 0xa0:
		b, err := d.take(uint64(c & 0x1f))
		return string(b), err
	}
	sized := func(size int) (uint64, error) { return d.uint(size) }
	switch c {
	case 0xc0:
		return nil, nil
	case 0xc2:
		return false, nil
	case 0xc3:
		return true, nil
	case 0xc4, 0xc5, 0xc6:
		n, err := sized(1 << (c - 0xc4))
		if err != nil {
			return nil, err
		}
		return d.bytes(n)
	case 0xc7, 0xc8, 0xc9:
		n, err := sized(1 << (c - 0xc7))
		if err != nil {
			return nil, err
		}
		return d.ext(n)
	case 0xca:
		n, err := sized(4)
		return float64(math.Float32frombits(uint32(n))), err
	case 0xcb:
		n, err := sized(8)
		return math.Float64frombits(n), err
	case 0xcc, 0xcd, 0xce, 0xcf:
		n, err := sized(1 << (c - 0xcc))
		if err != nil {
			return nil, err
		}
		if n > math.MaxInt64 {
			return n, nil
		}
		return int64(n), nil
	case 0xd0, 0xd1, 0xd2, 0xd3:
		size := 1 << (c - 0xd0)
		n, err := sized(size)
		if err != nil {
			return nil, err
		}
		shift := 64 - 8*size
		return int64(n<<shift) >> shift, nil
	case 0xd4, 0xd5, 0xd6, 0xd7, 0xd8:
		return d.ext(1 << (c - 0xd4))
	case 0xd9, 0xda, 0xdb:
		n, err := sized(1 << (c - 0xd9))
		if err != nil {
			return nil, err
		}
		b, err := d.take(n)
		return string(b), err
	case 0xdc, 0xdd:
		n, err := sized(2 << (c - 0xdc))
		if err != nil {
			return nil, err
		}
		return d.array(n, depth)
	case 0xde, 0xdf:
		n, err := sized(2 << (c - 0xde))
		if err != nil {
			return nil, err
		}
		return d.mapping(n, depth)
	}
	return nil, errMsgpack // 0xc1 is never used
}

// mpInt reads an integer value decoded by msgpackDecode.
func mpInt(v any) (int64, bool) {
	n, ok := v.(int64)
	return n, ok
}

// msgpackUint appends the shortest encoding of n.
func msgpackUint(out []byte, n uint64) []byte {
	switch {
	case n <= 0x7f:
		return append(out, byte(n))
	case n <= 0xff:
		return append(out, 0xcc, byte(n))
	case n <= 0xffff:
		return binary.BigEndian.AppendUint16(append(out, 0xcd), uint16(n))
	case n <= 0xffffffff:
		return binary.BigEndian.AppendUint32(append(out, 0xce), uint32(n))
	}
	return binary.BigEndian.AppendUint64(append(out, 0xcf), n)
}

// hoselineSubscribe is the spotter subscription [1, [tg]].
func hoselineSubscribe(tg int) []byte {
	return msgpackUint([]byte{0x92, 0x01, 0x91}, uint64(tg))
}
