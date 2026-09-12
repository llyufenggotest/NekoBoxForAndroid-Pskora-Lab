package legacy

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	snell "github.com/sagernet/sing-snell"
	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
)

type Client struct {
	psk     []byte
	version int
}

func NewClient(psk []byte, version int) (*Client, error) {
	if len(psk) == 0 {
		return nil, snell.ErrMissingPSK
	}
	if version < 1 || version > 3 {
		return nil, errors.New("snell: legacy client only supports versions 1 through 3")
	}
	return &Client{psk: append([]byte(nil), psk...), version: version}, nil
}

func (c *Client) DialContext(_ context.Context, conn net.Conn, destination M.Socksaddr) net.Conn {
	return &clientConn{conn: newStreamConn(conn, c.psk, c.version), destination: destination, version: c.version}
}

func (c *Client) DialPacketConn(conn net.Conn) (net.PacketConn, error) {
	if c.version < 3 {
		return nil, errors.New("snell: UDP requires version 3 or above")
	}
	stream := newStreamConn(conn, c.psk, c.version)
	request := buf.NewSize(3)
	defer request.Release()
	if err := (snell.Request{Command: snell.CommandUDP}).Write(request); err != nil {
		return nil, err
	}
	if _, err := stream.Write(request.Bytes()); err != nil {
		return nil, err
	}
	if err := readReply(stream); err != nil {
		return nil, err
	}
	return &packetConn{Conn: stream}, nil
}

type clientConn struct {
	conn        net.Conn
	destination M.Socksaddr
	version     int
	requestOnce sync.Once
	requestErr  error
	written     atomic.Bool
	replyOnce   sync.Once
	replyErr    error
}

func (c *clientConn) ensureRequest() error {
	c.requestOnce.Do(func() {
		command := byte(snell.CommandConnect)
		if c.version == 2 {
			command = snell.CommandConnectV2
		}
		request := buf.NewSize(4 + len(c.destination.AddrString()) + 2)
		defer request.Release()
		c.requestErr = (snell.Request{Command: command, Destination: c.destination}).Write(request)
		if c.requestErr == nil {
			_, c.requestErr = c.conn.Write(request.Bytes())
		}
		if c.requestErr == nil {
			c.written.Store(true)
		}
	})
	return c.requestErr
}

func (c *clientConn) ensureReply() error {
	c.replyOnce.Do(func() { c.replyErr = readReply(c.conn) })
	return c.replyErr
}

func (c *clientConn) Read(p []byte) (int, error) {
	if err := c.ensureRequest(); err != nil {
		return 0, err
	}
	if err := c.ensureReply(); err != nil {
		return 0, err
	}
	return c.conn.Read(p)
}

func (c *clientConn) Write(p []byte) (int, error) {
	if err := c.ensureRequest(); err != nil {
		return 0, err
	}
	if len(p) == 0 {
		return 0, nil
	}
	return c.conn.Write(p)
}

func (c *clientConn) NeedHandshakeForWrite() bool        { return !c.written.Load() }
func (c *clientConn) NeedHandshake() bool                { return c.NeedHandshakeForWrite() }
func (c *clientConn) Close() error                       { return c.conn.Close() }
func (c *clientConn) LocalAddr() net.Addr                { return c.conn.LocalAddr() }
func (c *clientConn) RemoteAddr() net.Addr               { return c.destination.TCPAddr() }
func (c *clientConn) SetDeadline(t time.Time) error      { return c.conn.SetDeadline(t) }
func (c *clientConn) SetReadDeadline(t time.Time) error  { return c.conn.SetReadDeadline(t) }
func (c *clientConn) SetWriteDeadline(t time.Time) error { return c.conn.SetWriteDeadline(t) }

func readReply(reader io.Reader) error {
	var reply [1]byte
	if _, err := io.ReadFull(reader, reply[:]); err != nil {
		return err
	}
	switch reply[0] {
	case snell.ReplyTunnel:
		return nil
	case snell.ReplyError:
		var header [2]byte
		if _, err := io.ReadFull(reader, header[:]); err != nil {
			return err
		}
		message := make([]byte, int(header[1]))
		if _, err := io.ReadFull(reader, message); err != nil {
			return err
		}
		return errors.New("snell: server rejected request: " + string(message))
	default:
		return errors.New("snell: unexpected server reply")
	}
}
