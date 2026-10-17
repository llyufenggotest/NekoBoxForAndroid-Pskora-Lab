package vless

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"

	"github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
)

// BlackstoneIdentity validates original text before any legacy normalization.
// Only this exact selector permits compact UUIDs; all errors are value-free.
func BlackstoneIdentity(id string) (key [16]byte, selected bool, err error) {
	if !strings.Contains(strings.ToLower(id), "#blackstone") {
		return
	}
	selected = true
	invalid := func() ([16]byte, bool, error) {
		return key, true, fmt.Errorf("Blackstone: exact selector and canonical or compact UUID required")
	}
	if !strings.HasSuffix(id, "#blackstone") || strings.Count(id, "#") != 1 {
		return invalid()
	}
	raw := strings.TrimSuffix(id, "#blackstone")
	if len(raw) == 36 && raw[8] == '-' && raw[13] == '-' && raw[18] == '-' && raw[23] == '-' && strings.Count(raw, "-") == 4 {
		raw = strings.ReplaceAll(raw, "-", "")
	}
	if len(raw) != 32 {
		return invalid()
	}
	decoded, e := hex.DecodeString(raw)
	if e != nil {
		return invalid()
	}
	copy(key[:], decoded)
	return
}

// blackstoneState belongs to one Client (one dynamic delegate generation).
// H is externally supplied opaque wire data, never a local HMAC key.
type blackstoneState struct {
	extension bool
	mu        sync.RWMutex
	h         []byte
}

func (s *blackstoneState) snapshot() []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]byte(nil), s.h...)
}
func (s *blackstoneState) replace(h []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.h = append([]byte(nil), h...)
}

// NewClientWithBlackstoneOptions requires explicit H for the exact selector.
// E=false is supported. Unselected legacy modes ignore E/H, unchanged.
func NewClientWithBlackstoneOptions(id, flow string, log logger.Logger, e bool, h string) (*Client, error) {
	key, selected, err := BlackstoneIdentity(id)
	if err != nil {
		return nil, err
	}
	if !selected {
		return NewClient(id, flow, log)
	}
	if flow != "" && flow != FlowVision {
		return nil, fmt.Errorf("Blackstone: unsupported flow")
	}
	// Initial H is opaque and need not have a length prefix or local size limit.
	if len(h) == 0 {
		return nil, fmt.Errorf("Blackstone: nonempty hexadecimal H required")
	}
	decoded, err := hex.DecodeString(h)
	if err != nil {
		return nil, fmt.Errorf("Blackstone: hexadecimal H required")
	}
	return &Client{key: key, flow: flow, logger: log, blackstone: &blackstoneState{extension: e, h: decoded}}, nil
}

func (c *Client) newConn(conn net.Conn, command byte, destination M.Socksaddr) *Conn {
	result := NewConn(conn, c.key, command, destination, c.flow, c.isX365, c.isJuzi)
	result.blackstone = c.blackstone
	return result
}

// BlackstoneVisionNativeUDP selects only the private native packet stack.
// Standard/Pure/Juzi/X365 and XUDP keep their existing initialization paths.
func (c *Client) BlackstoneVisionNativeUDP() bool {
	return c.blackstone != nil && c.flow == FlowVision
}

// DialEarlyBlackstoneVisionPacketConnWithOptions follows the donor stack:
// raw transport -> private CommandUDP stream -> Vision -> packet framing.
// The inner stream alone owns request/response authentication and H updates;
// the outer PacketConn only owns datagram lengths (both handshakes are done
// there). Keeping the private state also preserves atomic writes, short-write
// failure and short-buffer draining without emitting another private header.
func (c *Client) DialEarlyBlackstoneVisionPacketConnWithOptions(conn, baseConn net.Conn, destination M.Socksaddr, canSplice bool) (*PacketConn, error) {
	if !c.BlackstoneVisionNativeUDP() {
		return nil, fmt.Errorf("Blackstone: native Vision UDP initializer requires private Vision")
	}
	stream, err := c.prepareConnWithOptions(c.newConn(&blackstoneUDPCloseConn{Conn: conn}, 2, destination), baseConn, canSplice)
	if err != nil {
		return nil, err
	}
	return &PacketConn{Conn: &blackstoneUDPVisionPacketStream{Conn: stream}, destination: destination, requestWritten: true, responseRead: true, blackstone: c.blackstone}, nil
}

// PacketConn's private writer supplies one complete length+payload frame.
// The exact donor submits those as two Vision writes; preserve that boundary
// without touching flow-empty framing or duplicating the packet length.
type blackstoneUDPVisionPacketStream struct{ net.Conn }

func (c *blackstoneUDPVisionPacketStream) Write(frame []byte) (int, error) {
	if len(frame) < 2 || int(frame[0])<<8|int(frame[1]) != len(frame)-2 {
		return 0, fmt.Errorf("Blackstone: invalid native UDP frame")
	}
	n, err := c.Conn.Write(frame[:2])
	if err == nil && n != 2 {
		err = io.ErrShortWrite
	}
	if err != nil {
		return 0, err
	}
	n, err = c.Conn.Write(frame[2:])
	if err == nil && n != len(frame)-2 {
		err = io.ErrShortWrite
	}
	if err != nil {
		return 0, err
	}
	return len(frame), nil
}

func (c *blackstoneUDPVisionPacketStream) Upstream() any { return c.Conn }

// Only the new private UDP stack needs to coalesce inner handshake failures,
// outer packet failures and simultaneous owner closes onto one raw Close.
// Preserve upstream discovery for TLS/encryption Vision introspection.
type blackstoneUDPCloseConn struct {
	net.Conn
	once sync.Once
	err  error
}

func (c *blackstoneUDPCloseConn) Close() error {
	c.once.Do(func() { c.err = c.Conn.Close() })
	return c.err
}

func (c *blackstoneUDPCloseConn) Upstream() any { return c.Conn }

// Capture current H at first write, not when a lazy connection is allocated.
func (s *blackstoneState) writeRequest(w io.Writer, request Request, payload []byte) error {
	h := s.snapshot()
	extra := len(h)
	if s.extension {
		extra += 16
	}
	b := buf.NewSize(RequestLen(request) + extra + len(payload))
	defer b.Release()
	if err := EncodeRequest(request, b); err != nil {
		return err
	}
	if s.extension {
		digest := md5.Sum(request.UUID[:])
		_, _ = b.Write(digest[:])
	}
	_, _ = b.Write(h)
	_, _ = b.Write(payload)
	n, err := w.Write(b.Bytes())
	if err == nil && n != b.Len() {
		err = io.ErrShortWrite
	}
	return err
}

func (c *PacketConn) writeBlackstonePacket(payload []byte) (int, error) {
	c.access.Lock()
	defer c.access.Unlock()
	if c.blackstoneErr != nil {
		return 0, c.blackstoneErr
	}
	fail := func(err error) (int, error) {
		c.blackstoneErr = err
		_ = c.Conn.Close()
		return 0, err
	}
	if len(payload) > 65535 {
		return 0, fmt.Errorf("Blackstone: packet exceeds wire limit")
	}
	frame := make([]byte, 2+len(payload))
	frame[0] = byte(len(payload) >> 8)
	frame[1] = byte(len(payload))
	copy(frame[2:], payload)
	if !c.requestWritten {
		c.requestWritten = true // never replay a possibly partially emitted private header
		err := c.blackstone.writeRequest(c.Conn, Request{UUID: c.key, Command: 2, Destination: c.destination, Flow: c.flow}, frame)
		if err != nil {
			return fail(err)
		}
		return len(payload), nil
	}
	n, err := c.Conn.Write(frame)
	if err == nil && n != len(frame) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return fail(err)
	}
	return len(payload), nil
}

func readClientResponse(r io.Reader, x365 bool, s *blackstoneState) error {
	if err := ReadResponse(r, x365); err != nil {
		return err
	}
	if s == nil {
		return nil
	}
	var length [1]byte
	if _, err := io.ReadFull(r, length[:]); err != nil {
		return err
	}
	// Verified VLESS rule: zero consumes the marker but retains previous H.
	if length[0] == 0 {
		return nil
	}
	updated := make([]byte, 1+int(length[0]))
	updated[0] = length[0]
	if _, err := io.ReadFull(r, updated[1:]); err != nil {
		return err
	}
	s.replace(updated)
	return nil
}
