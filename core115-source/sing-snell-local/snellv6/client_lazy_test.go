package snellv6

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	M "github.com/sagernet/sing/common/metadata"
)

func TestDialContextDefersRequestHandshake(t *testing.T) {
	for _, testCase := range []struct {
		name string
		mode Mode
	}{
		{name: "default", mode: ModeDefault},
		{name: "unshaped", mode: ModeUnshaped},
		{name: "unsafe-raw", mode: ModeUnsafeRaw},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			for _, reuse := range []bool{false, true} {
				t.Run(map[bool]string{false: "direct", true: "reuse"}[reuse], func(t *testing.T) {
					upstream := newCaptureConn()
					client, err := NewClient(ClientOptions{
						PSK:    []byte("lazy-handshake-test-psk"),
						Mode:   testCase.mode,
						Reuse:  reuse,
						Dialer: captureDialer{conn: upstream},
						Server: M.ParseSocksaddr("127.0.0.1:1"),
					})
					if err != nil {
						t.Fatal(err)
					}
					defer client.Close()
					conn, err := client.DialContext(context.Background(), M.ParseSocksaddr("example.com:443"))
					if err != nil {
						t.Fatal(err)
					}
					defer conn.Close()
					assertLazyWriteHandshake(t, conn, upstream)
				})
			}
		})
	}
}

type earlyWriteConn interface {
	NeedHandshakeForWrite() bool
}

func assertLazyWriteHandshake(t *testing.T, conn net.Conn, upstream *captureConn) {
	t.Helper()
	earlyWriter, ok := conn.(earlyWriteConn)
	if !ok {
		t.Fatal("connection does not expose NeedHandshakeForWrite")
	}
	if !earlyWriter.NeedHandshakeForWrite() {
		t.Fatal("new connection is not marked as needing a write handshake")
	}
	if written := upstream.WrittenLen(); written != 0 {
		t.Fatalf("DialContext wrote %d bytes before first write", written)
	}
	n, err := conn.Write(nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("Write(nil) returned %d, want 0", n)
	}
	firstWriteLen := upstream.WrittenLen()
	if firstWriteLen == 0 {
		t.Fatal("Write(nil) did not flush request handshake")
	}
	if earlyWriter.NeedHandshakeForWrite() {
		t.Fatal("connection still needs a write handshake after Write(nil)")
	}
	n, err = conn.Write(nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("second Write(nil) returned %d, want 0", n)
	}
	if written := upstream.WrittenLen(); written != firstWriteLen {
		t.Fatalf("second Write(nil) wrote %d extra bytes", written-firstWriteLen)
	}
}

type captureDialer struct {
	conn *captureConn
}

func (d captureDialer) DialContext(context.Context, string, M.Socksaddr) (net.Conn, error) {
	return d.conn, nil
}

func (d captureDialer) ListenPacket(context.Context, M.Socksaddr) (net.PacketConn, error) {
	return nil, errors.New("unused")
}

type captureConn struct {
	access sync.Mutex
	writes [][]byte
}

func newCaptureConn() *captureConn {
	return &captureConn{}
}

func (c *captureConn) Read([]byte) (int, error) {
	return 0, io.EOF
}

func (c *captureConn) Write(p []byte) (int, error) {
	c.access.Lock()
	c.writes = append(c.writes, append([]byte(nil), p...))
	c.access.Unlock()
	return len(p), nil
}

func (c *captureConn) Close() error {
	return nil
}

func (c *captureConn) LocalAddr() net.Addr {
	return captureAddr("local")
}

func (c *captureConn) RemoteAddr() net.Addr {
	return captureAddr("remote")
}

func (c *captureConn) SetDeadline(time.Time) error {
	return nil
}

func (c *captureConn) SetReadDeadline(time.Time) error {
	return nil
}

func (c *captureConn) SetWriteDeadline(time.Time) error {
	return nil
}

func (c *captureConn) WrittenLen() int {
	c.access.Lock()
	defer c.access.Unlock()
	var written int
	for _, write := range c.writes {
		written += len(write)
	}
	return written
}

type captureAddr string

func (a captureAddr) Network() string {
	return "tcp"
}

func (a captureAddr) String() string {
	return string(a)
}
