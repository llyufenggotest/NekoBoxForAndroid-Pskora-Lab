package xhttp

// Synthetic loopback peers construct downlink directly, never Pack(Unpack).
import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/binary"
	M "github.com/sagernet/sing/common/metadata"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

type memoryPacket struct {
	read          []byte
	actual        int
	writes        [][]byte
	short, closed bool
	deadline      time.Time
}

func (c *memoryPacket) ReadFrom(p []byte) (int, net.Addr, error) {
	copy(p, c.read)
	return c.actual, &net.UDPAddr{}, nil
}
func (c *memoryPacket) WriteTo(p []byte, _ net.Addr) (int, error) {
	c.writes = append(c.writes, append([]byte(nil), p...))
	if c.short {
		return len(p) - 1, nil
	}
	return len(p), nil
}
func (c *memoryPacket) Close() error                       { c.closed = true; return nil }
func (*memoryPacket) LocalAddr() net.Addr                  { return &net.UDPAddr{} }
func (c *memoryPacket) SetDeadline(d time.Time) error      { c.deadline = d; return nil }
func (c *memoryPacket) SetReadDeadline(d time.Time) error  { c.deadline = d; return nil }
func (c *memoryPacket) SetWriteDeadline(d time.Time) error { c.deadline = d; return nil }
func TestPacketIndependentPeerFreshIVAndSource(t *testing.T) {
	c, _ := NewWireClient(WireConfig{Password: "synthetic:0102", UDPFake: "face"})
	raw := &memoryPacket{}
	pc := c.PacketConn(raw, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1})
	destinations := []struct {
		addr net.Addr
		wire []byte
	}{
		{&net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 53}, []byte{1, 192, 0, 2, 1, 0, 53}},
		{M.ParseSocksaddr("[2001:db8::1]:53"), []byte{4, 0x20, 1, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 53}},
		{M.ParseSocksaddr("example.invalid:53"), append(append([]byte{3, 15}, []byte("example.invalid")...), 0, 53)},
	}
	key := md5.Sum([]byte("synthetic"))
	block, _ := aes.NewCipher(key[:])
	token := md5.Sum([]byte("syntheticdo not hack this protocol please"))
	for _, d := range destinations {
		for range 2 {
			if n, err := pc.WriteTo([]byte("payload"), d.addr); err != nil || n != 7 {
				t.Fatalf("write %d %v", n, err)
			}
		}
		frames := raw.writes[len(raw.writes)-2:]
		if bytes.Equal(frames[0][147:163], frames[1][147:163]) || bytes.Equal(frames[0][2:128], frames[1][2:128]) {
			t.Fatal("IV or fake filler reused")
		}
		frame := frames[0]
		if !bytes.Equal(frame[:2], []byte{0xfa, 0xce}) || !bytes.Equal(frame[128:144], token[:]) || !bytes.Equal(frame[144:147], []byte{1, 2, 16}) {
			t.Fatal("uplink header mismatch")
		}
		plain := make([]byte, len(frame)-163)
		cipher.NewCTR(block, frame[147:163]).XORKeyStream(plain, frame[163:])
		if !bytes.Equal(plain, append(append([]byte(nil), d.wire...), []byte("payload")...)) {
			t.Fatalf("address or payload mismatch %x", plain)
		}
		raw.read = wireServer(key[:], []byte{0x10, 0x22}, append(append([]byte(nil), d.wire...), []byte("answer")...))
		raw.actual = len(raw.read)
		p := make([]byte, 20)
		n, source, err := pc.ReadFrom(p)
		if err != nil || string(p[:n]) != "answer" || source.String() != d.addr.String() {
			t.Fatalf("source=%v want=%v n=%d err=%v", source, d.addr, n, err)
		}
	}
	deadline := time.Now().Add(time.Second)
	pc.SetDeadline(deadline)
	if raw.deadline != deadline {
		t.Fatal("deadline not forwarded")
	}
	pc.Close()
	if !raw.closed {
		t.Fatal("close not forwarded")
	}
}
func TestPacketRejectsActualBoundsAndInvalidAddress(t *testing.T) {
	c, _ := NewWireClient(WireConfig{Password: "synthetic:0102"})
	key := md5.Sum([]byte("synthetic"))
	frame := wireServer(key[:], nil, []byte{1, 192, 0, 2, 1, 0, 53, 'x'})
	for i := 0; i < 169; i++ {
		raw := &memoryPacket{read: frame, actual: i}
		pc := c.PacketConn(raw, &net.UDPAddr{})
		if _, _, err := pc.ReadFrom(make([]byte, 10)); err == nil {
			t.Fatalf("actual nread=%d accepted", i)
		}
	}
	for _, n := range []int{-1, 65537} {
		raw := &memoryPacket{actual: n}
		pc := c.PacketConn(raw, &net.UDPAddr{})
		if _, _, err := pc.ReadFrom(make([]byte, 1)); err == nil {
			t.Fatal("invalid nread accepted")
		}
	}
	for _, body := range [][]byte{nil, {9}, {3, 20, 'a'}} {
		raw := &memoryPacket{read: wireServer(key[:], []byte{9}, body)}
		raw.actual = len(raw.read)
		pc := c.PacketConn(raw, &net.UDPAddr{})
		if _, _, err := pc.ReadFrom(make([]byte, 10)); err == nil {
			t.Fatal("bad source accepted")
		}
		if !bytes.Equal(c.snapshot(), []byte{1, 2}) {
			t.Fatal("partial packet published cache")
		}
	}
	raw := &memoryPacket{}
	pc := c.PacketConn(raw, &net.UDPAddr{})
	for _, addr := range []net.Addr{nil, M.Socksaddr{Fqdn: strings.Repeat("x", 256)}, &net.UDPAddr{}} {
		if _, err := pc.WriteTo(nil, addr); err == nil {
			t.Fatal("invalid destination accepted")
		}
	}
	if _, err := pc.WriteTo(make([]byte, 65535), M.ParseSocksaddr("192.0.2.1:53")); err == nil {
		t.Fatal("oversize packet accepted")
	}
	if len(raw.writes) != 0 {
		t.Fatal("invalid packet emitted")
	}
	raw.short = true
	if _, err := pc.WriteTo(nil, M.ParseSocksaddr("192.0.2.1:53")); err != io.ErrShortWrite {
		t.Fatalf("short packet write: %v", err)
	}
}
func TestUOTBoundsDrainAndPerPacketAddresses(t *testing.T) {
	for _, version := range []int{0, 1, 2} {
		raw := &wireMemoryConn{}
		pc, err := NewWireUOT(raw, version, M.ParseSocksaddr("192.0.2.1:53"))
		if err != nil {
			t.Fatal(err)
		}
		addresses := []struct {
			addr net.Addr
			wire []byte
		}{{M.ParseSocksaddr("192.0.2.1:53"), []byte{0, 192, 0, 2, 1, 0, 53}}, {M.ParseSocksaddr("[2001:db8::1]:53"), []byte{1, 0x20, 1, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 53}}, {M.ParseSocksaddr("example.invalid:53"), append(append([]byte{2, 15}, []byte("example.invalid")...), 0, 53)}}
		var expected []byte
		if version != 1 {
			expected = []byte{0, 0, 192, 0, 2, 1, 0, 53}
		}
		for _, d := range addresses {
			n, err := pc.WriteTo([]byte("x"), d.addr)
			if err != nil || n != 1 {
				t.Fatal(n, err)
			}
			expected = append(expected, d.wire...)
			expected = append(expected, 0, 1, 'x')
		}
		if !bytes.Equal(raw.Bytes(), expected) {
			t.Fatalf("UOT v%d frame mismatch %x != %x", version, raw.Bytes(), expected)
		}
		raw.Reset()
		if _, err := pc.WriteTo(make([]byte, 65536), addresses[0].addr); err == nil || raw.Len() != 0 {
			t.Fatal("UOT oversize narrowed")
		}
		if n, err := pc.WriteTo(make([]byte, 65535), addresses[0].addr); err != nil || n != 65535 {
			t.Fatal("UOT 65535 rejected", err)
		}
	}
	for _, v := range []int{-1, 3, 256} {
		if _, err := WireUOTDestination(v); err == nil {
			t.Fatal("unsupported UOT version accepted")
		}
	}
	// A short caller buffer must consume the whole datagram, retaining alignment.
	source := []byte{0, 192, 0, 2, 1, 0, 53}
	frame := append(append([]byte(nil), source...), 0, 5, 'a', 'b', 'c', 'd', 'e')
	frame = append(frame, source...)
	frame = append(frame, 0, 2, 'o', 'k')
	raw := &wireMemoryConn{input: bytes.NewReader(frame)}
	pc, _ := NewWireUOT(raw, 1, M.ParseSocksaddr("192.0.2.1:53"))
	p := make([]byte, 2)
	if n, _, err := pc.ReadFrom(p); n != 2 || err != io.ErrShortBuffer {
		t.Fatal(n, err)
	}
	if n, source, err := pc.ReadFrom(p); err != nil || n != 2 || string(p) != "ok" || source.String() != "192.0.2.1:53" {
		t.Fatal(n, source, err)
	}
	short := &wireMemoryConn{short: true}
	broken, _ := NewWireUOT(short, 2, M.ParseSocksaddr("192.0.2.1:53"))
	if _, err := broken.WriteTo([]byte("a"), M.ParseSocksaddr("192.0.2.1:53")); err != io.ErrShortWrite || !short.closed {
		t.Fatal("UOT short write not fatal")
	}
}
func TestNativeUDPRealLoopbackSocket(t *testing.T) {
	server, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	server.SetDeadline(time.Now().Add(2 * time.Second))
	raw, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	c, _ := NewWireClient(WireConfig{Password: "synthetic:0102"})
	pc := c.PacketConn(raw, server.LocalAddr())
	defer pc.Close()
	pc.SetDeadline(time.Now().Add(2 * time.Second))
	done := make(chan error, 1)
	go func() {
		p := make([]byte, 65536)
		_, from, err := server.ReadFrom(p)
		if err == nil {
			key := md5.Sum([]byte("synthetic"))
			_, err = server.WriteTo(wireServer(key[:], nil, []byte{1, 198, 51, 100, 1, 0, 53, 'o', 'k'}), from)
		}
		done <- err
	}()
	if _, err := pc.WriteTo([]byte("query"), M.ParseSocksaddr("192.0.2.1:53")); err != nil {
		t.Fatal(err)
	}
	p := make([]byte, 10)
	n, source, err := pc.ReadFrom(p)
	if err != nil || string(p[:n]) != "ok" || source.String() != "198.51.100.1:53" {
		t.Fatal(n, source, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
func TestUOTTruncations(t *testing.T) {
	frame := []byte{0, 192, 0, 2, 1, 0, 53, 0, 2, 'o', 'k'}
	for i := range len(frame) {
		raw := &wireMemoryConn{input: bytes.NewReader(frame[:i])}
		pc, _ := NewWireUOT(raw, 2, M.ParseSocksaddr("192.0.2.1:53"))
		if _, _, err := pc.ReadFrom(make([]byte, 10)); err == nil {
			t.Fatalf("UOT truncation %d accepted", i)
		}
	}
	// Verify big-endian length from the independent expected prefix.
	raw := &wireMemoryConn{}
	pc, _ := NewWireUOT(raw, 1, M.ParseSocksaddr("192.0.2.1:53"))
	pc.WriteTo(make([]byte, 256), M.ParseSocksaddr("192.0.2.1:53"))
	if binary.BigEndian.Uint16(raw.Bytes()[7:9]) != 256 {
		t.Fatal("not big endian")
	}
}
