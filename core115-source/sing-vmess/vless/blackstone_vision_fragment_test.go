package vless

import (
	"bytes"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	vmess "github.com/sagernet/sing-vmess"
	"testing"

	"github.com/sagernet/sing/common/logger"
)

// Literal oracle: no production serializer or padding function makes this wire.
func fragmentLiteral(t *testing.T) []byte {
	t.Helper()
	b, e := hex.DecodeString("0002929302ddee00112233445566778899aabbccddeeff0000020003000300000001000600026269670001520000")
	if e != nil {
		t.Fatal(e)
	}
	return b
}

type fragmentReader struct{ chunks [][]byte }

func (r *fragmentReader) Read(p []byte) (int, error) {
	if len(r.chunks) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.chunks[0])
	r.chunks[0] = r.chunks[0][n:]
	if len(r.chunks[0]) == 0 {
		r.chunks = r.chunks[1:]
	}
	return n, nil
}
func fragmentPrivate(t *testing.T, r io.Reader, command byte, splice bool) (*VisionConn, *Client, *bsVisionRaw) {
	t.Helper()
	c := bsVisionClient(t)
	raw := &bsVisionRaw{response: r}
	inner := c.newConn(raw, command, bsDst())
	v, e := NewVisionConn(inner, tls.Client(raw, &tls.Config{}), c.key, logger.NOP(), splice)
	if e != nil {
		t.Fatal(e)
	}
	return v, c, raw
}
func TestBlackstoneVisionFragmentUUID(t *testing.T) {
	b := fragmentLiteral(t)
	v, c, _ := fragmentPrivate(t, &fragmentReader{[][]byte{b[:8], b[8:]}}, 2, false)
	got := make([]byte, 8)
	_, e := io.ReadFull(v, got)
	if e != nil || !bytes.Equal(got, []byte{0, 3, 'b', 'i', 'g', 0, 1, 'R'}) {
		t.Fatalf("framing leaked: got=%x err=%v", got, e)
	}
	bsAssert(t, c.blackstone.snapshot(), "02ddee")
}

func TestBlackstoneVisionFragmentEmptyBlockLimit(t *testing.T) {
	b := fragmentLiteral(t)[:23]
	b = append(b, bytes.Repeat([]byte{0, 0, 0, 0, 0}, 101)...)
	b = append(b, 1, 0, 1, 0, 0, 'R')
	v, _, _ := fragmentPrivate(t, bytes.NewReader(b), 2, false)
	if n, e := v.Read(make([]byte, 1)); n != 0 || !errors.Is(e, io.ErrNoProgress) {
		t.Fatalf("unbounded empty control loop %d %v", n, e)
	}
}

func TestBlackstoneVisionFragmentPaddingOnlyLimit(t *testing.T) {
	b := fragmentLiteral(t)[:23]
	b = append(b, bytes.Repeat([]byte{0, 0, 0, 0, 1, 0}, 101)...)
	b = append(b, 1, 0, 1, 0, 0, 'R')
	v, _, _ := fragmentPrivate(t, bytes.NewReader(b), 2, false)
	if n, e := v.Read(make([]byte, 1)); n != 0 || !errors.Is(e, io.ErrNoProgress) {
		t.Fatalf("unbounded padding-only loop %d %v", n, e)
	}
}

func TestBlackstoneVisionFragmentDataEOF(t *testing.T) {
	b := fragmentLiteral(t)
	v, _, _ := fragmentPrivate(t, &fragmentEOFReader{b[7:]}, 2, false)
	v.Conn.(*Conn).responseRead = true
	got, e := io.ReadAll(v)
	if e != nil || !bytes.Equal(got, []byte{0, 3, 'b', 'i', 'g', 0, 1, 'R'}) {
		t.Fatalf("complete padding with data+EOF got=%x err=%v", got, e)
	}
}

type fragmentEOFReader struct{ b []byte }

func (r *fragmentEOFReader) Read(p []byte) (int, error) {
	n := copy(p, r.b)
	r.b = r.b[n:]
	if len(r.b) == 0 {
		return n, io.EOF
	}
	return n, nil
}

func TestBlackstoneVisionFragmentEveryByte(t *testing.T) {
	b := fragmentLiteral(t)
	chunks := make([][]byte, len(b))
	for i := range b {
		chunks[i] = b[i : i+1]
	}
	v, _, _ := fragmentPrivate(t, &fragmentReader{chunks}, 2, false)
	pc := &PacketConn{Conn: v, responseRead: true, requestWritten: true, blackstone: &blackstoneState{}}
	if _, e := pc.Read(make([]byte, 1)); !errors.Is(e, io.ErrShortBuffer) {
		t.Fatal(e)
	}
	var p [1]byte
	if n, e := pc.Read(p[:]); n != 1 || e != nil || p[0] != 'R' {
		t.Fatalf("next packet %x %v", p, e)
	}
}
func TestBlackstoneVisionFragmentTruncation(t *testing.T) {
	b := fragmentLiteral(t)
	for end := 7; end < len(b); end++ {
		t.Run(fmt.Sprint(end), func(t *testing.T) {
			v, _, _ := fragmentPrivate(t, bytes.NewReader(b[:end]), 2, false)
			got, e := io.ReadAll(v)
			want := []byte{0, 3, 'b', 'i', 'g', 0, 1, 'R'}
			if len(got) > len(want) || !bytes.Equal(got, want[:len(got)]) {
				t.Fatalf("header leaked: %x", got)
			}
			// EOF at a valid continue-block boundary is a clean stream end; any
			// partially received UUID/header/content/padding is unexpected EOF.
			if end != 7 && end != 33 && !errors.Is(e, io.ErrUnexpectedEOF) {
				t.Fatalf("end=%d err=%v", end, e)
			}
			if n, e2 := v.Read(make([]byte, 1)); n != 0 || e2 == nil {
				t.Fatalf("terminal not latched %d %v", n, e2)
			}
		})
	}
}
func TestBlackstoneVisionFragmentMismatchAndInvalidCommand(t *testing.T) {
	for _, kind := range []string{"mismatch", "partial-prefix", "invalid-command"} {
		t.Run(kind, func(t *testing.T) {
			b := fragmentLiteral(t)
			if kind == "mismatch" {
				b[7] ^= 1
			}
			if kind == "partial-prefix" {
				b = b[:15]
			}
			if kind == "invalid-command" {
				b[23] = 3
			}
			v, _, _ := fragmentPrivate(t, bytes.NewReader(b), 2, false)
			got, e := io.ReadAll(v)
			if e == nil || len(got) != 0 {
				t.Fatalf("accepted literal false positive %x %v", got, e)
			}
		})
	}
}
func TestBlackstoneVisionFragmentZeroAndProgress(t *testing.T) {
	v, _, raw := fragmentPrivate(t, bytes.NewReader(fragmentLiteral(t)), 2, false)
	if n, e := v.Read(nil); n != 0 || e != nil || v.Conn.(*Conn).responseRead || raw.wire.Len() != 0 {
		t.Fatal("zero read consumed handshake")
	}
	// An empty Continue block must not recurse or spin on no-progress reads.
	b := fragmentLiteral(t)[:23]
	b = append(b, 0, 0, 0, 0, 0, 1, 0, 1, 0, 0, 'R')
	v, _, _ = fragmentPrivate(t, bytes.NewReader(b), 2, false)
	p := make([]byte, 1)
	if n, e := v.Read(p); n != 1 || e != nil || p[0] != 'R' {
		t.Fatalf("zero continue %x %v", p, e)
	}
	v, _, _ = fragmentPrivate(t, &fragmentZeroReader{}, 2, false)
	v.Conn.(*Conn).responseRead = true
	if _, e := v.Read(p); !errors.Is(e, io.ErrNoProgress) {
		t.Fatal(e)
	}
}

type fragmentZeroReader struct{}

func (*fragmentZeroReader) Read([]byte) (int, error) { return 0, nil }
func TestBlackstoneVisionFragmentDirect(t *testing.T) {
	for _, splice := range []bool{false, true} {
		t.Run(fmt.Sprint(splice), func(t *testing.T) {
			b := fragmentLiteral(t)[:23]
			b = append(b, 2, 0, 1, 0, 2, 'V', 0, 0)
			v, _, raw := fragmentPrivate(t, bytes.NewReader(b), 2, splice)
			v.input = bytes.NewReader([]byte("I"))
			v.rawInput = bytes.NewBufferString("B")
			v.directConn = &bsVisionRaw{response: bytes.NewReader([]byte("R"))}
			for i, want := range []byte("VIBR") {
				var p [1]byte
				n, e := v.Read(p[:])
				if n != 1 || e != nil || p[0] != want {
					t.Fatalf("index=%d %x %v", i, p, e)
				}
				if i < 3 && v.ReaderReplaceable() {
					t.Fatal("buffered direct bypass")
				}
			}
			if !v.ReaderReplaceable() || v.UpstreamReader() != v.directConn || raw.wire.Len() != 0 {
				t.Fatal("direct state/lazy write changed")
			}
		})
	}
}
func TestBlackstoneVisionFragmentNativeXUDP(t *testing.T) {
	for _, command := range []byte{2, 3} {
		t.Run(fmt.Sprint(command), func(t *testing.T) {
			b := fragmentLiteral(t)
			if command == 3 {
				b = b[:23]
				b = append(b, 1, 0, 20, 0, 0, 0, 4, 0, 0, 2, 1, 0, 3, 'b', 'i', 'g', 0, 4, 0, 0, 2, 1, 0, 1, 'R')
			}
			chunks := make([][]byte, len(b))
			for i := range b {
				chunks[i] = b[i : i+1]
			}
			v, c, _ := fragmentPrivate(t, &fragmentReader{chunks}, command, false)
			var r io.Reader
			if command == 2 {
				r = &PacketConn{Conn: v, responseRead: true, requestWritten: true, blackstone: c.blackstone}
			} else {
				r = vmess.NewXUDPConn(v, bsDst())
			}
			for _, want := range []string{"big", "R"} {
				p := make([]byte, 32)
				n, e := r.Read(p)
				if e != nil || string(p[:n]) != want {
					t.Fatalf("packet=%x err=%v", p[:n], e)
				}
			}
			bsAssert(t, c.blackstone.snapshot(), "02ddee")
		})
	}
}
func TestBlackstoneVisionFragmentCloseAndDeadline(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(fmt.Sprint(deadline), func(t *testing.T) {
			left, right := net.Pipe()
			defer left.Close()
			defer right.Close()
			c := bsVisionClient(t)
			pc, e := c.DialEarlyBlackstoneVisionPacketConnWithOptions(left, tls.Client(left, &tls.Config{}), bsDst(), false)
			if e != nil {
				t.Fatal(e)
			}
			// Enter partial UUID, then leave peer open.
			sent := make(chan struct{})
			go func() { right.Write(fragmentLiteral(t)[:8]); close(sent) }()
			done := make(chan error, 1)
			go func() { _, e := pc.Read(make([]byte, 8)); done <- e }()
			<-sent
			if deadline {
				pc.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
			} else {
				var wg sync.WaitGroup
				for i := 0; i < 8; i++ {
					wg.Add(1)
					go func() { defer wg.Done(); pc.Close() }()
				}
				wg.Wait()
			}
			select {
			case e = <-done:
				if e == nil {
					t.Fatal("blocked read succeeded")
				}
			case <-time.After(time.Second):
				t.Fatal("read stuck")
			}
		})
	}
}
func TestBlackstoneVisionFragmentZeroDatagram(t *testing.T) {
	b := fragmentLiteral(t)[:23]
	b = append(b, 1, 0, 5, 0, 0, 0, 0, 0, 1, 'R')
	v, c, _ := fragmentPrivate(t, bytes.NewReader(b), 2, false)
	pc := &PacketConn{Conn: v, responseRead: true, requestWritten: true, blackstone: c.blackstone}
	p := make([]byte, 1)
	if n, e := pc.Read(p); n != 0 || e != nil {
		t.Fatal(n, e)
	}
	if n, e := pc.Read(p); n != 1 || e != nil || p[0] != 'R' {
		t.Fatal(n, e)
	}
}

func TestBlackstoneVisionFragmentEndRawAndLazyH(t *testing.T) {
	b := fragmentLiteral(t)[:23]
	b = append(b, 1, 0, 1, 0, 1, 'A', 0, 'B')
	v, c, _ := fragmentPrivate(t, bytes.NewReader(b), 2, false)
	raw := &bsVisionRaw{response: bytes.NewReader(nil)}
	next, e := NewVisionConn(c.newConn(raw, 3, bsDst()), tls.Client(raw, &tls.Config{}), c.key, logger.NOP(), false)
	if e != nil {
		t.Fatal(e)
	}
	if raw.wire.Len() != 0 {
		t.Fatal("lazy allocation emitted request")
	}
	got, e := io.ReadAll(v)
	if e != nil || string(got) != "AB" {
		t.Fatalf("End passthrough %x %v", got, e)
	}
	if _, e = next.Write([]byte("P")); e != nil {
		t.Fatal(e)
	}
	want, _ := hex.DecodeString("6e8311168ee16d6aa1aa48c64145003c02ddee")
	start := RequestLen(next.Conn.(*Conn).request)
	if !bytes.Equal(raw.wire.Bytes()[start:start+len(want)], want) {
		t.Fatal("lazy Vision retained stale H")
	}
}

func TestBlackstoneVisionFragmentIsolation(t *testing.T) {
	for _, suffix := range []string{"", "#juzi", "#x365"} {
		t.Run(suffix, func(t *testing.T) {
			c, e := NewClient(bsTestID+suffix, FlowVision, logger.NOP())
			if e != nil {
				t.Fatal(e)
			}
			raw := &bsVisionRaw{response: bytes.NewReader(nil)}
			stream := c.newConn(raw, 2, bsDst())
			v, e := NewVisionConn(stream, tls.Client(raw, &tls.Config{}), c.key, logger.NOP(), false)
			if e != nil || v.blackstoneReader != nil {
				t.Fatal("legacy parser opted in", e)
			}
		})
	}
	c := bsClient(t, true)
	raw := &bsVisionRaw{response: bytes.NewReader(nil)}
	v, e := NewVisionConn(c.newConn(raw, 2, bsDst()), tls.Client(raw, &tls.Config{}), c.key, logger.NOP(), false)
	if e != nil || v.blackstoneReader != nil {
		t.Fatal("flow-empty opted in", e)
	}
}
