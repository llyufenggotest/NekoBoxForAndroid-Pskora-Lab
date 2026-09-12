package vless

import (
	"bytes"
	"encoding/hex"
	M "github.com/sagernet/sing/common/metadata"
	"io"
	"net"
	"testing"
)

type pureRecordingConn struct {
	net.Conn
	writes [][]byte
	r      io.Reader
}

func (c *pureRecordingConn) Write(p []byte) (int, error) {
	c.writes = append(c.writes, append([]byte(nil), p...))
	return len(p), nil
}
func (c *pureRecordingConn) Read(p []byte) (int, error) { return c.r.Read(p) }
func (c *pureRecordingConn) Close() error               { return nil }

func TestPureWireAndResponse(t *testing.T) {
	c, err := NewClient("00112233-4455-6677-8899-aabbccddeeff#PuRe", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ dst, wire string }{{"example.com:443", "a100112233445566778899aabbccddeeff000101bb020b6578616d706c652e636f6d"}, {"1.2.3.4:80", "a100112233445566778899aabbccddeeff000100500101020304"}} {
		raw := &pureRecordingConn{r: bytes.NewReader([]byte{0xa1, 0, 'A', 0xa1, 0, 'B'})}
		conn, e := c.DialEarlyConn(raw, M.ParseSocksaddr(tc.dst))
		if e != nil {
			t.Fatal(e)
		}
		conn.Write([]byte("payload"))
		if len(raw.writes) != 2 || hex.EncodeToString(raw.writes[0]) != tc.wire || string(raw.writes[1]) != "payload" {
			t.Fatalf("wrong separate messages: %x", raw.writes)
		}
		out, e := io.ReadAll(conn)
		if e != nil || !bytes.Equal(out, []byte{'A', 0xa1, 0, 'B'}) {
			t.Fatalf("response %x %v", out, e)
		}
	}
	for _, h := range [][]byte{{0, 0}, {0xa1, 1}, {0xa1}} {
		conn := &pureConn{Conn: &pureRecordingConn{r: bytes.NewReader(h)}}
		if _, err := conn.Read(make([]byte, 4)); err == nil {
			t.Fatal("bad response accepted")
		}
	}
}
func TestPurePacketIsolation(t *testing.T) {
	c, _ := NewClient("00112233-4455-6677-8899-aabbccddeeff#pure", "", nil)
	raw := &pureRecordingConn{}
	d := M.ParseSocksaddr("1.1.1.1:53")
	if _, e := c.DialPacketConn(raw, d); e == nil {
		t.Fatal("UDP accepted")
	}
	if _, e := c.DialEarlyPacketConn(raw, d); e == nil {
		t.Fatal("early UDP accepted")
	}
	if _, e := c.DialXUDPPacketConn(raw, d); e == nil {
		t.Fatal("XUDP accepted")
	}
	if _, e := c.DialEarlyXUDPPacketConn(raw, d); e == nil {
		t.Fatal("early XUDP accepted")
	}
	if len(raw.writes) != 0 {
		t.Fatal("UDP emitted bytes")
	}
	if _, e := c.DialEarlyConn(raw, M.ParseSocksaddr("[::1]:443")); e == nil {
		t.Fatal("IPv6 accepted")
	}
}
