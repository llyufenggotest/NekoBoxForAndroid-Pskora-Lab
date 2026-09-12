package group

import (
	"context"
	"github.com/sagernet/sing/service/pause"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestNetworkRecoveryLatestPendingOnly(t *testing.T) {
	s, all := fixture(t, 1)
	enableFallback(s)
	g := s.group
	g.logger = s.logger
	g.pause = pause.ManagerFromContext(pause.WithDefaultManager(context.Background()))
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer server.Close()
	type generationKey struct{}
	got := make(chan string, 4)
	all[0].dial = func(ctx context.Context) (net.Conn, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		} else {
			got <- ctx.Value(generationKey{}).(string)
		}
		return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
	}
	g.link = server.URL
	g.recordFallback(all[0], g.beginFallbackObservation(), false, nil)
	done := make(chan struct{})
	go func() { g.CheckOutbounds(context.Background(), true); close(done) }()
	<-entered
	b, bc := context.WithCancel(context.WithValue(context.Background(), generationKey{}, "B"))
	s.InterfaceUpdated(b)
	bc()
	c, cc := context.WithCancel(context.WithValue(context.Background(), generationKey{}, "C"))
	defer cc()
	s.InterfaceUpdated(c)
	close(release)
	<-done
	select {
	case v := <-got:
		if v != "C" {
			t.Fatal("intermediate canceled generation probed", v)
		}
	case <-time.After(time.Second):
		t.Fatal("latest C recovery dropped")
	}
	deadline := time.Now().Add(time.Second)
	for g.checking.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if calls.Load() != 2 {
		t.Fatal("pending was not coalesced", calls.Load())
	}
	if g.history.LoadURLTestHistory(all[0].Tag()) == nil {
		t.Fatal("C success missing")
	}
}
