package snellv4

import (
	"bytes"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

type identityCaptureConn struct{ bytes.Buffer }

func (c *identityCaptureConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (c *identityCaptureConn) Close() error                     { return nil }
func (c *identityCaptureConn) LocalAddr() net.Addr              { return nil }
func (c *identityCaptureConn) RemoteAddr() net.Addr             { return nil }
func (c *identityCaptureConn) SetDeadline(time.Time) error      { return nil }
func (c *identityCaptureConn) SetReadDeadline(time.Time) error  { return nil }
func (c *identityCaptureConn) SetWriteDeadline(time.Time) error { return nil }

func TestOIXPacketConnRequiresSessionExporter(t *testing.T) {
	c, err := NewClient(ClientOptions{PSK: []byte("test-password"), RequireExporter: true, ExporterFromConn: func(net.Conn) ([]byte, error) { return nil, errors.New("export failure") }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.DialPacketConn(&identityCaptureConn{}); err == nil {
		t.Fatal("UDP bypassed required session exporter")
	}
}
func TestOIXPacketConnIdentityWire(t *testing.T) {
	exporter := bytes.Repeat([]byte{0x42}, 32)
	raw := &identityCaptureConn{}
	c, err := NewClient(ClientOptions{PSK: []byte("test-password"), RequireExporter: true, ExporterFromConn: func(conn net.Conn) ([]byte, error) {
		if conn != raw {
			t.Fatal("wrong exporter session")
		}
		return exporter, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	packet, err := c.DialPacketConn(raw)
	if err != nil {
		t.Fatal(err)
	}
	defer packet.Close()
	// Write emits the lazy UDP request before trying to consume the reply.
	_, _ = packet.WriteTo([]byte{1}, &net.UDPAddr{IP: net.IPv4(1, 1, 1, 1), Port: 53})
	wire := raw.Bytes()
	if len(wire) < 56 || string(wire[16:24]) != identityWireMagicV2 {
		t.Fatal("UDP first record lacks OIX identity v2")
	}
	tag, err := IdentityV2AuthTag(c.psk, exporter, wire[:16])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(tag, wire[40:56]) {
		t.Fatal("UDP identity tag does not bind session exporter")
	}
}
