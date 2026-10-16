package xhttp

import (
	"bytes"
	"errors"
	M "github.com/sagernet/sing/common/metadata"
	"io"
	"net"
	"sync"
)

// WirePacketConn snapshots auth once; each datagram has a fresh IV. Downlink
// publication only affects wrappers constructed after the valid full response.
type WirePacketConn struct {
	net.PacketConn
	client          *WireClient
	auth            []byte
	remote          net.Addr
	readMu, writeMu sync.Mutex
}

func (c *WireClient) PacketConn(conn net.PacketConn, remote net.Addr) *WirePacketConn {
	return &WirePacketConn{PacketConn: conn, client: c, auth: c.snapshot(), remote: remote}
}
func (c *WirePacketConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	address, err := wireAddress(addr, false)
	if err != nil {
		return 0, err
	}
	if 161+len(c.auth)+len(address)+len(p) > 65507 {
		return 0, errors.New("blackstone: UDP datagram too large")
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	header, stream, err := c.client.uplink(c.auth, c.client.udpFake)
	if err != nil {
		return 0, err
	}
	plain := append(address, p...)
	ct := make([]byte, len(plain))
	stream.XORKeyStream(ct, plain)
	packet := append(header, ct...)
	n, err := c.PacketConn.WriteTo(packet, c.remote)
	if err == nil && n != len(packet) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return 0, err
	}
	return len(p), nil
}
func (c *WirePacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	packet := make([]byte, 65536)
	n, _, err := c.PacketConn.ReadFrom(packet)
	if err != nil {
		return 0, nil, err
	}
	if n < 0 || n > len(packet) {
		return 0, nil, errors.New("blackstone: invalid packet read length")
	}
	reader := bytes.NewReader(packet[:n])
	stream, auth, err := c.client.downlink(reader, true)
	if err != nil {
		return 0, nil, err
	}
	plain := make([]byte, reader.Len())
	if _, err = io.ReadFull(reader, plain); err != nil {
		return 0, nil, err
	}
	stream.XORKeyStream(plain, plain)
	payload := bytes.NewReader(plain)
	source, err := M.SocksaddrSerializer.ReadAddrPort(payload)
	if err != nil {
		return 0, nil, err
	}
	if !source.IsValid() {
		return 0, nil, errors.New("blackstone: invalid packet source")
	}
	c.client.publish(auth)
	count := copy(p, plain[len(plain)-payload.Len():])
	if count < payload.Len() {
		return count, source, io.ErrShortBuffer
	}
	return count, source, nil
}

var _ net.PacketConn = (*WirePacketConn)(nil)
