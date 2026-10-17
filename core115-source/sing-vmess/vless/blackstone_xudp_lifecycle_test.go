package vless

import (
	"errors"
	"net"
	"os"
	"testing"
	"time"
)

func TestBlackstoneXUDPDeadlineAndCloseInterrupt(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	c := bsClient(t, true)
	pc, e := c.DialEarlyXUDPPacketConn(a, bsDst())
	if e != nil {
		t.Fatal(e)
	}
	if e = pc.SetReadDeadline(time.Now().Add(20 * time.Millisecond)); e != nil {
		t.Fatal(e)
	}
	_, _, e = pc.ReadFrom(make([]byte, 128))
	if !errors.Is(e, os.ErrDeadlineExceeded) {
		t.Fatalf("deadline not forwarded: %v", e)
	}
	if e = pc.SetReadDeadline(time.Time{}); e != nil {
		t.Fatal(e)
	}
	entered := make(chan struct{})
	done := make(chan error, 1)
	go func() { close(entered); _, _, e := pc.ReadFrom(make([]byte, 128)); done <- e }()
	<-entered
	pc.Close()
	select {
	case e = <-done:
		if e == nil {
			t.Fatal("closed read succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not interrupt XUDP read")
	}
}
