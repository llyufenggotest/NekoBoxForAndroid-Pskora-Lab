package dialer

import (
	"context"
	"net"
	"syscall"
	"testing"
	"time"
)

func TestPrivateRetryDial(t *testing.T) {
	n := 0
	_, e := retryDial(context.Background(), func() (net.Conn, error) { n++; return nil, syscall.ECONNRESET })
	if n != 4 || e != syscall.ECONNRESET {
		t.Fatal(n, e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	retryDial(ctx, func() (net.Conn, error) { return nil, syscall.ECONNRESET })
	if time.Since(start) > 50*time.Millisecond {
		t.Fatal("cancel ignored")
	}
}
func TestPrivateConcurrentDialLocal(t *testing.T) {
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	go func() {
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			c.Close()
		}
	}()
	c, e := dialContextConcurrently(net.Dialer{}, WithConcurrentDial(context.Background(), true), "tcp", l.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	c.Close()
}
