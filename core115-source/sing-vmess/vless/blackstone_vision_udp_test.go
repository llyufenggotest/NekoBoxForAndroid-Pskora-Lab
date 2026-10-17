package vless

import (
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/logger"
)

// A known real TLS base is independent of the outer transport stream, exactly
// as the Reality transport hook supplies its underlying supported TLS object.
type bsVisionRaw struct {
	net.Conn
	wire     bytes.Buffer
	response io.Reader
	short    bool
	closes   atomic.Int32
	deadline time.Time
}

func (c *bsVisionRaw) Write(p []byte) (int, error) {
	if c.short {
		return len(p) - 1, nil
	}
	return c.wire.Write(p)
}
func (c *bsVisionRaw) Read(p []byte) (int, error)         { return c.response.Read(p) }
func (c *bsVisionRaw) Close() error                       { c.closes.Add(1); return nil }
func (c *bsVisionRaw) SetDeadline(d time.Time) error      { c.deadline = d; return nil }
func (c *bsVisionRaw) SetReadDeadline(d time.Time) error  { return c.SetDeadline(d) }
func (c *bsVisionRaw) SetWriteDeadline(d time.Time) error { return c.SetDeadline(d) }
func bsVisionClient(t *testing.T) *Client {
	t.Helper()
	c, e := NewClientWithBlackstoneOptions(bsTestID+"#blackstone", FlowVision, logger.NOP(), true, "03aabbcc")
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func bsVisionPacket(t *testing.T, c *Client, raw *bsVisionRaw) *PacketConn {
	t.Helper()
	base := tls.Client(raw, &tls.Config{InsecureSkipVerify: true})
	pc, e := c.DialEarlyBlackstoneVisionPacketConnWithOptions(raw, base, bsDst(), false)
	if e != nil {
		t.Fatal(e)
	}
	return pc
}
func bsVisionReply(h, body []byte) []byte {
	p := append([]byte{0, 0}, h...)
	id, _, _ := BlackstoneIdentity(bsTestID + "#blackstone")
	p = append(p, id[:]...)
	p = append(p, 1, byte(len(body)>>8), byte(len(body)), 0, 0)
	return append(p, body...)
}
func TestBlackstoneVisionPacketBufferedAndResponseCache(t *testing.T) {
	c := bsVisionClient(t)
	raw := &bsVisionRaw{response: bytes.NewReader(bsVisionReply([]byte{2, 0xdd, 0xee}, []byte{0, 3, 'b', 'i', 'g', 0, 1, 'R'}))}
	pc := bsVisionPacket(t, c, raw)
	b := buf.NewSize(32)
	b.Write([]byte("P"))
	if e := pc.WritePacket(b, bsDst()); e != nil {
		t.Fatal(e)
	}
	// Verify literal private header, Command2 and flow, followed immediately by
	// Vision UUID, then exactly one UDP length inside the first Vision content.
	req := Request{UUID: c.key, Command: 2, Destination: bsDst(), Flow: FlowVision}
	offset := RequestLen(req) + 16 + 4
	p := raw.wire.Bytes()
	if len(p) < offset+24 {
		t.Fatal("truncated wire")
	}
	id := c.key
	if !bytes.Equal(p[offset:offset+16], id[:]) {
		t.Fatal("Vision is not inside private stream")
	}
	length := int(binary.BigEndian.Uint16(p[offset+17 : offset+19]))
	if length != 2 || !bytes.Equal(p[offset+21:offset+23], []byte{0, 1}) {
		t.Fatal("donor writes the UDP length alone into the first Vision record")
	}
	next := offset + 21 + length + int(binary.BigEndian.Uint16(p[offset+19:offset+21]))
	if len(p) < next+6 || binary.BigEndian.Uint16(p[next+1:next+3]) != 1 || p[next+5] != 'P' {
		t.Fatal("donor writes payload in the next Vision record, without a second UUID")
	}
	var small [1]byte
	if _, e := pc.Read(small[:]); !errors.Is(e, io.ErrShortBuffer) {
		t.Fatal(e)
	}
	if n, a, e := pc.ReadFrom(small[:]); e != nil || n != 1 || small[0] != 'R' || a.String() != bsDst().String() {
		t.Fatalf("next packet/domain: %d %v %v", n, a, e)
	}
	bsAssert(t, c.blackstone.snapshot(), "02ddee")
	d := time.Unix(123, 456)
	if e := pc.SetDeadline(d); e != nil || raw.deadline != d {
		t.Fatal("deadline lost")
	}
}
func TestBlackstoneVisionPacketResponseFailureAndZero(t *testing.T) {
	for _, r := range [][]byte{{0}, {0, 0, 3, 1, 2}, {0, 2, 9}} {
		c := bsVisionClient(t)
		raw := &bsVisionRaw{response: bytes.NewReader(r)}
		pc := bsVisionPacket(t, c, raw)
		if _, e := pc.Read(make([]byte, 16)); e == nil {
			t.Fatal("partial response accepted")
		}
		bsAssert(t, c.blackstone.snapshot(), "03aabbcc")
	}
	c := bsVisionClient(t)
	raw := &bsVisionRaw{response: bytes.NewReader(bsVisionReply([]byte{0}, []byte{0, 1, 'R'}))}
	pc := bsVisionPacket(t, c, raw)
	if n, e := pc.Read(make([]byte, 1)); e != nil || n != 1 {
		t.Fatal(e)
	}
	bsAssert(t, c.blackstone.snapshot(), "03aabbcc")
}
func TestBlackstoneVisionPacketShortWriteCloseOnce(t *testing.T) {
	c := bsVisionClient(t)
	raw := &bsVisionRaw{response: bytes.NewReader(nil), short: true}
	pc := bsVisionPacket(t, c, raw)
	if _, e := pc.Write([]byte("P")); !errors.Is(e, io.ErrShortWrite) {
		t.Fatalf("short write %v", e)
	}
	if _, e := pc.Write([]byte("Q")); !errors.Is(e, io.ErrShortWrite) {
		t.Fatal("partial private header replayed")
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = pc.Close() }()
	}
	wg.Wait()
	if n := raw.closes.Load(); n != 1 {
		t.Fatalf("private packet raw close count=%d want 1", n)
	}
}
func TestBlackstoneVisionPacketInitializerIsolation(t *testing.T) {
	for _, suffix := range []string{"", "#juzi", "#x365", "#pure"} {
		c, e := NewClientWithBlackstoneOptions(bsTestID+suffix, "", nil, true, "not hex ignored")
		if e != nil {
			t.Fatal(e)
		}
		if c.BlackstoneVisionNativeUDP() {
			t.Fatal("private UDP enabled for legacy client")
		}
		raw := &bsVisionRaw{}
		if _, e = c.DialEarlyBlackstoneVisionPacketConnWithOptions(raw, tls.Client(raw, &tls.Config{}), bsDst(), false); e == nil {
			t.Fatal("private initializer accepted unrelated mode")
		}
	}
	if bsClient(t, true).BlackstoneVisionNativeUDP() {
		t.Fatal("flow-empty changed")
	}
}
