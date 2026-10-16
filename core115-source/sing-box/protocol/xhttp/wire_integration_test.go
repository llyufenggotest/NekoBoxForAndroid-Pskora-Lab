package xhttp

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"errors"
	M "github.com/sagernet/sing/common/metadata"
	"net"
	"os"
	"sync"
	"testing"
	"time"
)

func mustWireStream(t *testing.T, c *WireClient, raw net.Conn, address []byte) *XHttpConn {
	t.Helper()
	dest := M.ParseSocksaddr("192.0.2.1:53")
	if len(address) > 0 {
		var err error
		dest, err = M.SocksaddrSerializer.ReadAddrPort(bytes.NewReader(address))
		if err != nil {
			t.Fatal(err)
		}
	}
	conn, err := c.StreamConn(raw, dest)
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

type wireStartedConn struct {
	net.Conn
	started chan struct{}
	once    sync.Once
}

func (c *wireStartedConn) Write(p []byte) (int, error) {
	c.once.Do(func() { close(c.started) })
	return c.Conn.Write(p)
}
func TestWireOpenStreamCancellationAndDeadline(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "deadline", true: "cancel"}[cancelled], func(t *testing.T) {
			a, b := net.Pipe()
			defer b.Close()
			defer a.Close()
			raw := &wireStartedConn{Conn: a, started: make(chan struct{})}
			client, _ := NewWireClient(WireConfig{Password: "synthetic:0102"})
			var ctx context.Context
			var cancel context.CancelFunc
			if cancelled {
				ctx, cancel = context.WithCancel(context.Background())
			} else {
				ctx, cancel = context.WithTimeout(context.Background(), 50*time.Millisecond)
			}
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := client.OpenStream(ctx, raw, M.ParseSocksaddr("route.example:443")); done <- err }()
			select {
			case <-raw.started:
			case <-time.After(time.Second):
				t.Fatal("no production write")
			}
			if cancelled {
				cancel()
			}
			select {
			case err := <-done:
				if cancelled && !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				if !cancelled && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, os.ErrDeadlineExceeded) {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("blocked initialization leaked")
			}
			if _, err := b.Write([]byte{1}); err == nil {
				t.Fatal("failed initialization did not close")
			}
		})
	}
}
func TestWireHostPreservedAndDeadlineCleared(t *testing.T) {
	c, _ := NewWireClient(WireConfig{Password: "synthetic:0102"})
	streamRaw := &wireMemoryConn{}
	stream, err := c.OpenStream(context.Background(), streamRaw, M.ParseSocksaddr("route.example:443"))
	if err != nil {
		t.Fatal(err)
	}
	if len(streamRaw.deadlines) != 2 || streamRaw.deadlines[0].IsZero() || !streamRaw.deadlines[1].IsZero() {
		t.Fatal("initial write deadline not bounded then cleared")
	}
	if _, err = stream.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	key, _ := hex.DecodeString("5f06b85e1619b2b8b56f7b758a0a310a")
	block, _ := aes.NewCipher(key)
	wire := streamRaw.Bytes()
	plain := make([]byte, len(wire)-163)
	cipher.NewCTR(block, wire[147:163]).XORKeyStream(plain, wire[163:])
	expected := append([]byte{3, 13}, []byte("route.example")...)
	expected = append(expected, 1, 187)
	expected = append(expected, []byte("hello")...)
	if !bytes.Equal(plain, expected) {
		t.Fatal("selected host destination lost")
	}
}
func TestWireUOTInsideCipherStream(t *testing.T) {
	for _, version := range []int{0, 1, 2} {
		t.Run(string(rune('0'+version)), func(t *testing.T) {
			client, _ := NewWireClient(WireConfig{Password: "synthetic:0102"})
			dest, err := WireUOTDestination(version)
			if err != nil {
				t.Fatal(err)
			}
			raw := &wireMemoryConn{}
			stream, err := client.OpenStream(context.Background(), raw, dest)
			if err != nil {
				t.Fatal(err)
			}
			pc, err := NewWireUOT(stream, version, M.ParseSocksaddr("192.0.2.1:53"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = pc.WriteTo([]byte("q"), M.ParseSocksaddr("192.0.2.1:53")); err != nil {
				t.Fatal(err)
			}
			key, _ := hex.DecodeString("5f06b85e1619b2b8b56f7b758a0a310a")
			block, _ := aes.NewCipher(key)
			wire := raw.Bytes()
			plain := make([]byte, len(wire)-163)
			cipher.NewCTR(block, wire[147:163]).XORKeyStream(plain, wire[163:])
			magic := "sp.v2.udp-over-tcp.arpa"
			if version == 1 {
				magic = "sp.udp-over-tcp.arpa"
			}
			expected := append([]byte{3, byte(len(magic))}, []byte(magic)...)
			expected = append(expected, 0, 0)
			address := []byte{0, 192, 0, 2, 1, 0, 53}
			if version != 1 {
				expected = append(expected, 0)
				expected = append(expected, address...)
			}
			expected = append(expected, address...)
			expected = append(expected, 0, 1, 'q')
			if !bytes.Equal(plain, expected) {
				t.Fatal("UOT encrypted wire mismatch")
			}
		})
	}
}
func TestWireUOTAllAddressFamilies(t *testing.T) {
	cases := []struct {
		addr   string
		prefix byte
	}{{"192.0.2.1:53", 0}, {"[2001:db8::1]:53", 1}, {"host.example:53", 2}}
	for _, v := range cases {
		t.Run(v.addr, func(t *testing.T) {
			raw := &wireMemoryConn{}
			pc, err := NewWireUOT(raw, 2, M.ParseSocksaddr(v.addr))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = pc.WriteTo(nil, M.ParseSocksaddr(v.addr)); err != nil {
				t.Fatal(err)
			}
			wire := raw.Bytes()
			if wire[0] != 0 || wire[1] != v.prefix {
				t.Fatal("wrong native request family")
			}
			// Independent read frame copied from literal family-specific address bytes.
			var address []byte
			switch v.prefix {
			case 0:
				address = []byte{0, 192, 0, 2, 1, 0, 53}
			case 1:
				address = []byte{1, 0x20, 1, 0xd, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 53}
			case 2:
				address = append([]byte{2, 12}, []byte("host.example")...)
				address = append(address, 0, 53)
			}
			raw.input = bytes.NewReader(append(address, 0, 0))
			if n, src, err := pc.ReadFrom(nil); n != 0 || err != nil || src.String() != v.addr {
				t.Fatal("family response", n, src, err)
			}
		})
	}
}
func TestWireStreamRejectsInvalidDestinations(t *testing.T) {
	c, _ := NewWireClient(WireConfig{Password: "synthetic:0102"})
	if _, err := c.StreamConn(&wireMemoryConn{}, M.Socksaddr{}); err == nil {
		t.Fatal("empty destination accepted")
	}
	if _, err := c.StreamConn(&wireMemoryConn{}, M.Socksaddr{Fqdn: string(bytes.Repeat([]byte{'x'}, 256))}); err == nil {
		t.Fatal("overlong destination accepted")
	}
}
