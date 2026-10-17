package vless

import (
	"crypto/tls"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

func TestBlackstoneVisionPacketCloseInterruptsRead(t *testing.T) {
	left, right := net.Pipe()
	defer right.Close()
	c := bsVisionClient(t)
	pc, e := c.DialEarlyBlackstoneVisionPacketConnWithOptions(left, tls.Client(left, &tls.Config{}), bsDst(), false)
	if e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { _, e := pc.Read(make([]byte, 1)); done <- e }()
	if e = pc.Close(); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-done:
		if e == nil {
			t.Fatal("closed read succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not unblock packet read")
	}
}

func TestBlackstoneVisionPacketSizeAndPayloadShortWrite(t *testing.T) {
	c := bsVisionClient(t)
	raw := &bsVisionRaw{response: &emptyVisionReader{}}
	pc := bsVisionPacket(t, c, raw)
	if _, e := pc.Write(make([]byte, 65536)); e == nil || raw.wire.Len() != 0 {
		t.Fatal("oversize packet emitted wire")
	}
	if n, e := pc.Write(nil); e != nil || n != 0 {
		t.Fatal("empty datagram", e)
	}
	if _, e := pc.Write([]byte("R")); e != nil {
		t.Fatal("oversize incorrectly poisoned stream")
	}
	inner := &bsPayloadShort{Conn: raw}
	splitter := &blackstoneUDPVisionPacketStream{Conn: inner}
	if _, e := splitter.Write([]byte{0, 1, 'P'}); !errors.Is(e, io.ErrShortWrite) {
		t.Fatal("payload short-write lost", e)
	}
	if _, e := splitter.Write([]byte{0, 2, 'P'}); e == nil {
		t.Fatal("invalid frame accepted")
	}
}

type emptyVisionReader struct{}

func (*emptyVisionReader) Read([]byte) (int, error) { return 0, io.EOF }

type bsPayloadShort struct {
	net.Conn
	calls int
}

func (c *bsPayloadShort) Write(p []byte) (int, error) {
	c.calls++
	if c.calls == 2 {
		return len(p) - 1, nil
	}
	return len(p), nil
}
