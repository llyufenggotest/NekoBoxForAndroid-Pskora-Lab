package group

import (
	"context"
	"fmt"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	SOCKS "github.com/sagernet/sing-box/protocol/socks"
	M "github.com/sagernet/sing/common/metadata"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestRuntimeSelectionRealFallbackSwitch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") }))
	defer server.Close()
	bad, _ := localSOCKS(t, true)
	good, _ := localSOCKS(t, false)
	next, _ := localSOCKS(t, false)
	s, _ := fixture(t, 3)
	enableFallback(s)
	var leaves []adapter.Outbound
	for i, l := range []net.Listener{bad, good, next} {
		a := l.Addr().(*net.TCPAddr)
		o, e := SOCKS.NewOutbound(context.Background(), nil, s.logger, fmt.Sprint("same-name-", i), option.SOCKSOutboundOptions{ServerOptions: option.ServerOptions{Server: "127.0.0.1", ServerPort: uint16(a.Port)}, Version: "5"})
		if e != nil {
			t.Fatal(e)
		}
		leaves = append(leaves, o)
	}
	s.group.outbounds = leaves
	if tcp, _, _ := s.RuntimeSelection(); tcp != "" {
		t.Fatal("fabricated initial node", tcp)
	}
	var readers sync.WaitGroup
	readers.Add(1)
	done := make(chan struct{})
	go func() {
		defer readers.Done()
		for {
			select {
			case <-done:
				return
			default:
				s.RuntimeSelection()
				s.Now()
				s.References()
			}
		}
	}()
	defer func() { close(done); readers.Wait() }()
	dial := func(want string) {
		c, e := s.DialContext(context.Background(), "tcp", M.ParseSocksaddr(server.Listener.Addr().String()))
		if e != nil {
			t.Fatal(e)
		}
		defer c.Close()
		tcp, udp, sem := s.RuntimeSelection()
		if tcp != want || udp != "" || sem != "recent_tcp_success" {
			t.Fatalf("snapshot %q %q %q", tcp, udp, sem)
		}
	}
	dial("same-name-1")
	good.Close()
	dial("same-name-2")
	fresh, _ := fixture(t, 1)
	enableFallback(fresh)
	if tcp, _, _ := fresh.RuntimeSelection(); tcp != "" {
		t.Fatal("new session inherited", tcp)
	}
	t.Log("real bad→good→next TCP selection, concurrent reads, fresh-session empty verified")
}
