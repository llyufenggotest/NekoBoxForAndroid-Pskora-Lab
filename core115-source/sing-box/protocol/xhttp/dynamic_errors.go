package xhttp

import (
	"net"
	"time"

	"github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/bufio"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

// Preserve the discoverable upstream and early-handshake contract, but disallow
// data-plane replacement: optimized copy must not bypass the redaction boundary.
type dynamicConn struct{ N.ExtendedConn }

func wrapDynamicConn(c net.Conn) net.Conn {
	if c == nil {
		return nil
	}
	if _, ok := c.(*dynamicConn); ok {
		return c
	}
	return &dynamicConn{bufio.NewExtendedConn(c)}
}
func (c *dynamicConn) Read(p []byte) (int, error) {
	n, err := c.ExtendedConn.Read(p)
	return n, dynamicSafe("read", err)
}
func (c *dynamicConn) Write(p []byte) (int, error) {
	n, err := c.ExtendedConn.Write(p)
	return n, dynamicSafe("write", err)
}
func (c *dynamicConn) ReadBuffer(b *buf.Buffer) error {
	return dynamicSafe("read", c.ExtendedConn.ReadBuffer(b))
}
func (c *dynamicConn) WriteBuffer(b *buf.Buffer) error {
	return dynamicSafe("write", c.ExtendedConn.WriteBuffer(b))
}
func (c *dynamicConn) Close() error { return dynamicSafe("stream-close", c.ExtendedConn.Close()) }
func (c *dynamicConn) SetDeadline(t time.Time) error {
	return dynamicSafe("deadline", c.ExtendedConn.SetDeadline(t))
}
func (c *dynamicConn) SetReadDeadline(t time.Time) error {
	return dynamicSafe("deadline", c.ExtendedConn.SetReadDeadline(t))
}
func (c *dynamicConn) SetWriteDeadline(t time.Time) error {
	return dynamicSafe("deadline", c.ExtendedConn.SetWriteDeadline(t))
}
func (c *dynamicConn) Upstream() any               { return c.ExtendedConn }
func (c *dynamicConn) ReaderReplaceable() bool     { return false }
func (c *dynamicConn) WriterReplaceable() bool     { return false }
func (c *dynamicConn) NeedHandshake() bool         { return N.NeedHandshakeForWrite(c.ExtendedConn) }
func (c *dynamicConn) NeedHandshakeForRead() bool  { return N.NeedHandshakeForRead(c.ExtendedConn) }
func (c *dynamicConn) NeedHandshakeForWrite() bool { return N.NeedHandshakeForWrite(c.ExtendedConn) }

type dynamicPacketConn struct{ net.PacketConn }

func wrapDynamicPacket(c net.PacketConn) net.PacketConn {
	if c == nil {
		return nil
	}
	if _, ok := c.(*dynamicPacketConn); ok {
		return c
	}
	return &dynamicPacketConn{c}
}
func (c *dynamicPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	n, a, err := c.PacketConn.ReadFrom(p)
	return n, a, dynamicSafe("packet-read", err)
}
func (c *dynamicPacketConn) WriteTo(p []byte, a net.Addr) (int, error) {
	n, err := c.PacketConn.WriteTo(p, a)
	return n, dynamicSafe("packet-write", err)
}
func (c *dynamicPacketConn) ReadPacket(b *buf.Buffer) (M.Socksaddr, error) {
	n, a, err := c.PacketConn.ReadFrom(b.FreeBytes())
	if err != nil {
		return M.Socksaddr{}, dynamicSafe("packet-read", err)
	}
	b.Truncate(n)
	return M.SocksaddrFromNet(a).Unwrap(), nil
}
func (c *dynamicPacketConn) WritePacket(b *buf.Buffer, a M.Socksaddr) error {
	defer b.Release()
	_, err := c.PacketConn.WriteTo(b.Bytes(), a)
	return dynamicSafe("packet-write", err)
}
func (c *dynamicPacketConn) Close() error {
	return dynamicSafe("packet-close", c.PacketConn.Close())
}
func (c *dynamicPacketConn) SetDeadline(t time.Time) error {
	return dynamicSafe("deadline", c.PacketConn.SetDeadline(t))
}
func (c *dynamicPacketConn) SetReadDeadline(t time.Time) error {
	return dynamicSafe("deadline", c.PacketConn.SetReadDeadline(t))
}
func (c *dynamicPacketConn) SetWriteDeadline(t time.Time) error {
	return dynamicSafe("deadline", c.PacketConn.SetWriteDeadline(t))
}
func (c *dynamicPacketConn) Upstream() any           { return c.PacketConn }
func (c *dynamicPacketConn) ReaderReplaceable() bool { return false }
func (c *dynamicPacketConn) WriterReplaceable() bool { return false }
func (c *dynamicPacketConn) NeedHandshake() bool     { return N.NeedHandshakeForWriteAny(c.PacketConn) }
func (c *dynamicPacketConn) NeedHandshakeForRead() bool {
	return N.NeedHandshakeForReadAny(c.PacketConn)
}
func (c *dynamicPacketConn) NeedHandshakeForWrite() bool {
	return N.NeedHandshakeForWriteAny(c.PacketConn)
}
