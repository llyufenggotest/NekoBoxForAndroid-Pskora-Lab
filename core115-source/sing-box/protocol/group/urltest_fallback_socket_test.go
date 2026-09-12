package group

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	SOCKS "github.com/sagernet/sing-box/protocol/socks"
	M "github.com/sagernet/sing/common/metadata"
)

func localSOCKS(t *testing.T, fail bool) (net.Listener, *atomic.Int32) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	calls := new(atomic.Int32)
	go func() {
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			calls.Add(1)
			go func() {
				var e error
				defer c.Close()
				c.SetDeadline(time.Now().Add(10 * time.Second))
				r := bufio.NewReader(c)
				h := make([]byte, 2)
				if _, e = io.ReadFull(r, h); e != nil {
					return
				}
				methods := make([]byte, int(h[1]))
				if _, e = io.ReadFull(r, methods); e != nil {
					return
				}
				c.Write([]byte{5, 0})
				req := make([]byte, 4)
				if _, e = io.ReadFull(r, req); e != nil {
					return
				}
				if req[3] != 1 {
					return
				}
				addr := make([]byte, 6)
				if _, e = io.ReadFull(r, addr); e != nil {
					return
				}
				if fail {
					c.Write([]byte{5, 1, 0, 1, 0, 0, 0, 0, 0, 0})
					return
				}
				host := net.IP(addr[:4])
				if !host.IsLoopback() {
					return
				}
				upstream, e := net.DialTimeout("tcp", net.JoinHostPort(host.String(), fmt.Sprint(binary.BigEndian.Uint16(addr[4:]))), time.Second)
				if e != nil {
					return
				}
				defer upstream.Close()
				c.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0})
				go io.Copy(upstream, r)
				io.Copy(c, upstream)
			}()
		}
	}()
	return l, calls
}

func TestFallbackRealSOCKSHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "fallback-real-socket-ok") }))
	defer server.Close()
	bad, badCalls := localSOCKS(t, true)
	good, goodCalls := localSOCKS(t, false)
	s, _ := fixture(t, 2)
	enableFallback(s)
	var leaves []adapter.Outbound
	for i, l := range []net.Listener{bad, good} {
		a := l.Addr().(*net.TCPAddr)
		o, e := SOCKS.NewOutbound(context.Background(), nil, s.logger, fmt.Sprint("real-socks-", i), option.SOCKSOutboundOptions{ServerOptions: option.ServerOptions{Server: "127.0.0.1", ServerPort: uint16(a.Port)}, Version: "5"})
		if e != nil {
			t.Fatal(e)
		}
		leaves = append(leaves, o)
	}
	s.group.outbounds = leaves
	destination := M.ParseSocksaddr(server.Listener.Addr().String())
	dial := func() net.Conn {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		c, e := s.DialContext(ctx, "tcp", destination)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { c.Close() })
		return c
	}
	request := func(c net.Conn) {
		t.Helper()
		c.SetDeadline(time.Now().Add(3 * time.Second))
		if _, e := fmt.Fprintf(c, "GET / HTTP/1.1\r\nHost: localhost\r\nConnection: keep-alive\r\n\r\n"); e != nil {
			t.Fatal(e)
		}
		response, e := http.ReadResponse(bufio.NewReader(c), nil)
		if e != nil {
			t.Fatal(e)
		}
		b, e := io.ReadAll(response.Body)
		response.Body.Close()
		if e != nil || string(b) != "fallback-real-socket-ok" {
			t.Fatalf("body=%q err=%v", b, e)
		}
	}
	c := dial()
	request(c)
	if badCalls.Load() != 1 || goodCalls.Load() != 1 {
		t.Fatalf("initial attempts bad=%d good=%d", badCalls.Load(), goodCalls.Load())
	}
	second := dial()
	request(second)
	if badCalls.Load() != 1 || goodCalls.Load() != 2 {
		t.Fatalf("cooldown attempts bad=%d good=%d", badCalls.Load(), goodCalls.Load())
	}
	// Mark selected proxy unhealthy and perform selection update while its existing TCP tunnel remains open.
	s.group.recordFallback(leaves[1], s.group.beginFallbackObservation(), false, nil)
	s.group.performUpdateCheck()
	request(c)
	t.Logf("real SOCKS5 handshakes: bad=%d good=%d; HTTP success on first, second and preserved established tunnel", badCalls.Load(), goodCalls.Load())
}
