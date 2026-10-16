package xhttp

import (
	"encoding/binary"
	"errors"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/common/uot"
	"io"
	"net"
	"sync"
)

func WireUOTDestination(version int) (M.Socksaddr, error) {
	if version < 0 || version > 2 {
		return M.Socksaddr{}, errors.New("blackstone: unsupported UOT version")
	}
	return uot.RequestDestination(uint8(version)), nil
}

// WireUOTConn must wrap the Blackstone TCP cipher stream, never raw UDP frames.
// Do not use sing.NewLazyConn: its request uses SOCKS ATYP, not native 0/1/2.
type WireUOTConn struct {
	net.Conn
	initial           []byte
	first             bool
	readMu, writeMu   sync.Mutex
	readErr, writeErr error
}

func NewWireUOT(conn net.Conn, version int, destination net.Addr) (*WireUOTConn, error) {
	if _, err := WireUOTDestination(version); err != nil {
		return nil, err
	}
	initial, err := wireAddress(destination, true)
	if err != nil {
		return nil, err
	}
	return &WireUOTConn{Conn: conn, initial: initial, first: version != 1}, nil
}
func (c *WireUOTConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	if len(p) > 65535 {
		return 0, errors.New("blackstone: UOT payload too large")
	}
	address, err := wireAddress(addr, true)
	if err != nil {
		return 0, err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.writeErr != nil {
		return 0, c.writeErr
	}
	var frame []byte
	if c.first {
		frame = append([]byte{0}, c.initial...)
	}
	frame = append(frame, address...)
	frame = binary.BigEndian.AppendUint16(frame, uint16(len(p)))
	frame = append(frame, p...)
	n, err := c.Conn.Write(frame)
	if err == nil && n != len(frame) {
		err = io.ErrShortWrite
	}
	if err != nil {
		c.writeErr = err
		_ = c.Conn.Close()
		return 0, err
	}
	c.first = false
	return len(p), nil
}
func (c *WireUOTConn) ReadFrom(p []byte) (int, net.Addr, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	if c.readErr != nil {
		return 0, nil, c.readErr
	}
	source, err := uot.AddrParser.ReadAddrPort(c.Conn)
	if err != nil {
		c.readErr = err
		return 0, nil, err
	}
	if !source.IsValid() {
		c.readErr = errors.New("blackstone: invalid UOT source")
		return 0, nil, c.readErr
	}
	var size [2]byte
	if _, err = io.ReadFull(c.Conn, size[:]); err != nil {
		c.readErr = err
		return 0, nil, err
	}
	length := int(binary.BigEndian.Uint16(size[:]))
	n := min(length, len(p))
	if _, err = io.ReadFull(c.Conn, p[:n]); err != nil {
		c.readErr = err
		return 0, nil, err
	}
	if n < length {
		if _, err = io.CopyN(io.Discard, c.Conn, int64(length-n)); err != nil {
			c.readErr = err
			return n, source, err
		}
		return n, source, io.ErrShortBuffer
	}
	return n, source, nil
}

var _ net.PacketConn = (*WireUOTConn)(nil)
