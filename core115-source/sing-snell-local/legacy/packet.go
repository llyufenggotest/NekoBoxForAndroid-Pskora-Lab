package legacy

import (
	"errors"
	"io"
	"net"
	"sync"
	"time"

	snell "github.com/sagernet/sing-snell"
	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
)

type packetConn struct {
	net.Conn
	readAccess  sync.Mutex
	writeAccess sync.Mutex
}

func (c *packetConn) WriteTo(p []byte, destination net.Addr) (int, error) {
	c.writeAccess.Lock()
	defer c.writeAccess.Unlock()
	if socksaddr, loaded := destination.(M.Socksaddr); loaded {
		return c.writePacket(p, socksaddr)
	}
	return c.writePacket(p, M.SocksaddrFromNet(destination))
}

func (c *packetConn) writePacket(p []byte, destination M.Socksaddr) (int, error) {
	record := buf.NewSize(1 + 1 + len(destination.AddrString()) + 2 + len(p))
	defer record.Release()
	record.WriteByte(snell.UDPCommandForward)
	if err := snell.WriteUDPRequestAddress(record, destination); err != nil {
		return 0, err
	}
	record.Write(p)
	if _, err := c.Conn.Write(record.Bytes()); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *packetConn) ReadFrom(p []byte) (int, net.Addr, error) {
	c.readAccess.Lock()
	defer c.readAccess.Unlock()
	record := buf.NewSize(maxPayload)
	defer record.Release()
	n, err := c.Conn.Read(record.FreeBytes())
	if errors.Is(err, errZeroChunk) {
		return 0, nil, io.EOF
	}
	if err != nil {
		return 0, nil, err
	}
	record.Truncate(n)
	source, err := snell.ReadUDPResponseAddress(record)
	if err != nil {
		return 0, nil, err
	}
	return copy(p, record.Bytes()), source, nil
}

func (c *packetConn) Close() error {
	_, _ = c.Conn.Write(nil)
	return c.Conn.Close()
}

func (c *packetConn) SetDeadline(t time.Time) error      { return c.Conn.SetDeadline(t) }
func (c *packetConn) SetReadDeadline(t time.Time) error  { return c.Conn.SetReadDeadline(t) }
func (c *packetConn) SetWriteDeadline(t time.Time) error { return c.Conn.SetWriteDeadline(t) }
