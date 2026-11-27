package main

// A minimal RFC 6455 WebSocket (no extensions, no compression) for the
// Hoseline relay: browsers connect to the dashboard listeners, and the board
// dials the BrandMeister spotter. Only what the relay needs is implemented.

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

const (
	wsContinuation byte = 0
	wsText         byte = 1
	wsBinary       byte = 2
	wsClose        byte = 8
	wsPing         byte = 9
	wsPong         byte = 10
)

const wsWriteTimeout = 10 * time.Second

type wsConn struct {
	conn      net.Conn
	r         *bufio.Reader
	client    bool          // a client masks its frames and receives unmasked ones
	max       int           // largest accepted message
	idle      time.Duration // read deadline renewed before every frame; 0: none
	wmu       sync.Mutex
	closeOnce sync.Once
}

// wsHandshakeError is a refused upgrade: the HTTP status the server answered.
type wsHandshakeError struct{ Status int }

func (e *wsHandshakeError) Error() string {
	return fmt.Sprintf("websocket handshake: HTTP %d", e.Status)
}

func wsAccept(key string) string {
	sum := sha1.Sum([]byte(key + wsGUID))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func headerToken(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for _, t := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(t), token) {
				return true
			}
		}
	}
	return false
}

// wsUpgrade completes the server side of the opening handshake. On failure it
// has already answered the request with an HTTP error.
func wsUpgrade(w http.ResponseWriter, r *http.Request, protocol string, max int) (*wsConn, error) {
	if r.Method != http.MethodGet || !headerToken(r.Header, "Connection", "upgrade") || !headerToken(r.Header, "Upgrade", "websocket") {
		w.Header().Set("Upgrade", "websocket")
		http.Error(w, "WebSocket expected", http.StatusUpgradeRequired)
		return nil, errors.New("not a websocket request")
	}
	if r.Header.Get("Sec-WebSocket-Version") != "13" {
		w.Header().Set("Sec-WebSocket-Version", "13")
		http.Error(w, "Unsupported WebSocket version", http.StatusUpgradeRequired)
		return nil, errors.New("websocket version")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if b, err := base64.StdEncoding.DecodeString(key); err != nil || len(b) != 16 {
		http.Error(w, "Invalid WebSocket key", http.StatusBadRequest)
		return nil, errors.New("websocket key")
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "WebSocket unavailable", http.StatusInternalServerError)
		return nil, errors.New("connection cannot be hijacked")
	}
	conn, rw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}
	// The listener's read and write timeouts must not end a long stream.
	_ = conn.SetDeadline(time.Time{})
	reply := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " + wsAccept(key) + "\r\n"
	if protocol != "" {
		reply += "Sec-WebSocket-Protocol: " + protocol + "\r\n"
	}
	_ = conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
	if _, err := io.WriteString(conn, reply+"\r\n"); err != nil {
		conn.Close()
		return nil, err
	}
	_ = conn.SetWriteDeadline(time.Time{})
	return &wsConn{conn: conn, r: rw.Reader, max: max}, nil
}

// wsDial opens a client connection to a ws:// or wss:// URL. Its errors never
// contain the URL (the spotter URL carries a token).
func wsDial(ctx context.Context, rawURL, origin, protocol string, tlsConfig *tls.Config, max int) (*wsConn, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "ws" && u.Scheme != "wss") || u.Hostname() == "" {
		return nil, errors.New("websocket: invalid URL")
	}
	port := u.Port()
	if port == "" {
		port = map[string]string{"ws": "80", "wss": "443"}[u.Scheme]
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(u.Hostname(), port))
	if err != nil {
		return nil, errors.New("websocket: connect failed")
	}
	ok := false
	defer func() {
		if !ok {
			conn.Close()
		}
	}()
	if dl, has := ctx.Deadline(); has {
		_ = conn.SetDeadline(dl)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Unix(1, 0)) })
	defer stop()
	if u.Scheme == "wss" {
		cfg := &tls.Config{}
		if tlsConfig != nil {
			cfg = tlsConfig.Clone()
		}
		if cfg.ServerName == "" {
			cfg.ServerName = u.Hostname()
		}
		cfg.MinVersion = tls.VersionTLS12
		cfg.NextProtos = []string{"http/1.1"}
		tc := tls.Client(conn, cfg)
		if err := tc.HandshakeContext(ctx); err != nil {
			return nil, errors.New("websocket: TLS handshake failed")
		}
		conn = tc
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	key := base64.StdEncoding.EncodeToString(nonce[:])
	req := "GET " + u.RequestURI() + " HTTP/1.1\r\nHost: " + u.Host + "\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\nSec-WebSocket-Version: 13\r\nUser-Agent: gospot\r\n"
	if origin != "" {
		req += "Origin: " + origin + "\r\n"
	}
	if protocol != "" {
		req += "Sec-WebSocket-Protocol: " + protocol + "\r\n"
	}
	if _, err := io.WriteString(conn, req+"\r\n"); err != nil {
		return nil, errors.New("websocket: request failed")
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		return nil, errors.New("websocket: no handshake reply")
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		resp.Body.Close()
		return nil, &wsHandshakeError{resp.StatusCode}
	}
	if !headerToken(resp.Header, "Upgrade", "websocket") || !headerToken(resp.Header, "Connection", "upgrade") ||
		resp.Header.Get("Sec-WebSocket-Accept") != wsAccept(key) {
		return nil, errors.New("websocket: invalid handshake reply")
	}
	if resp.Header.Get("Sec-WebSocket-Protocol") != protocol {
		return nil, errors.New("websocket: subprotocol not negotiated")
	}
	if !stop() {
		return nil, ctx.Err()
	}
	_ = conn.SetDeadline(time.Time{})
	ok = true
	return &wsConn{conn: conn, r: br, client: true, max: max}, nil
}

// wsFrame encodes one final frame; mask is set for client frames.
func wsFrame(op byte, payload []byte, mask *[4]byte) []byte {
	out := make([]byte, 0, 14+len(payload))
	out = append(out, 0x80|op)
	bit := byte(0)
	if mask != nil {
		bit = 0x80
	}
	switch n := len(payload); {
	case n < 126:
		out = append(out, bit|byte(n))
	case n <= 0xffff:
		out = append(out, bit|126)
		out = binary.BigEndian.AppendUint16(out, uint16(n))
	default:
		out = append(out, bit|127)
		out = binary.BigEndian.AppendUint64(out, uint64(n))
	}
	if mask == nil {
		return append(out, payload...)
	}
	out = append(out, mask[:]...)
	for i, b := range payload {
		out = append(out, b^mask[i%4])
	}
	return out
}

func (c *wsConn) writeFrame(op byte, payload []byte) error {
	var mask *[4]byte
	if c.client {
		mask = new([4]byte)
		if _, err := rand.Read(mask[:]); err != nil {
			return err
		}
	}
	frame := wsFrame(op, payload, mask)
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
	_, err := c.conn.Write(frame)
	return err
}

func (c *wsConn) writeJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.writeFrame(wsText, b)
}

func (c *wsConn) readFrame() (fin bool, op byte, payload []byte, err error) {
	if c.idle > 0 {
		_ = c.conn.SetReadDeadline(time.Now().Add(c.idle))
	}
	var h [2]byte
	if _, err = io.ReadFull(c.r, h[:]); err != nil {
		return
	}
	fin, op = h[0]&0x80 != 0, h[0]&0x0f
	masked := h[1]&0x80 != 0
	if h[0]&0x70 != 0 || masked == c.client {
		return false, 0, nil, errors.New("websocket: protocol error")
	}
	n := uint64(h[1] & 0x7f)
	switch n {
	case 126:
		var b [2]byte
		if _, err = io.ReadFull(c.r, b[:]); err != nil {
			return
		}
		n = uint64(binary.BigEndian.Uint16(b[:]))
	case 127:
		var b [8]byte
		if _, err = io.ReadFull(c.r, b[:]); err != nil {
			return
		}
		n = binary.BigEndian.Uint64(b[:])
	}
	if op >= wsClose && (!fin || n > 125) {
		return false, 0, nil, errors.New("websocket: invalid control frame")
	}
	if n > uint64(c.max) {
		return false, 0, nil, errors.New("websocket: frame too large")
	}
	var key [4]byte
	if masked {
		if _, err = io.ReadFull(c.r, key[:]); err != nil {
			return
		}
	}
	payload = make([]byte, n)
	if _, err = io.ReadFull(c.r, payload); err != nil {
		return
	}
	if masked {
		for i := range payload {
			payload[i] ^= key[i%4]
		}
	}
	return fin, op, payload, nil
}

// readMessage returns the next text or binary message, answering pings on
// the way. A close frame is answered and reported as io.EOF.
func (c *wsConn) readMessage() (byte, []byte, error) {
	var msgOp byte
	var msg []byte
	for {
		fin, op, payload, err := c.readFrame()
		if err != nil {
			return 0, nil, err
		}
		switch op {
		case wsPing:
			if err := c.writeFrame(wsPong, payload); err != nil {
				return 0, nil, err
			}
			continue
		case wsPong:
			continue
		case wsClose:
			code := []byte{0x03, 0xe8} // 1000
			if len(payload) >= 2 {
				code = payload[:2]
			}
			_ = c.writeFrame(wsClose, code)
			c.closeOnce.Do(func() { c.conn.Close() })
			return 0, nil, io.EOF
		case wsText, wsBinary:
			if msgOp != 0 {
				return 0, nil, errors.New("websocket: interleaved message")
			}
			msgOp, msg = op, payload
		case wsContinuation:
			if msgOp == 0 {
				return 0, nil, errors.New("websocket: unexpected continuation")
			}
			if len(msg)+len(payload) > c.max {
				return 0, nil, errors.New("websocket: message too large")
			}
			msg = append(msg, payload...)
		default:
			return 0, nil, errors.New("websocket: unknown opcode")
		}
		if fin {
			return msgOp, msg, nil
		}
	}
}

// keepalive pings the peer until ctx ends or a ping cannot be written.
func (c *wsConn) keepalive(ctx context.Context, every time.Duration, failed func()) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if c.writeFrame(wsPing, nil) != nil {
				failed()
				return
			}
		}
	}
}

// close sends a close frame (best effort) and closes the connection.
func (c *wsConn) close(code uint16, reason string) {
	c.closeOnce.Do(func() {
		if len(reason) > 120 {
			reason = reason[:120]
		}
		payload := binary.BigEndian.AppendUint16(nil, code)
		_ = c.writeFrame(wsClose, append(payload, reason...))
		c.conn.Close()
	})
}
