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

func TestNetworkRecoveryOverlappingRealHTTP(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		t.Run(map[bool]string{true: "fallback", false: "standard"}[fallback], func(t *testing.T) {
			s, all := fixture(t, 1)
			if fallback {
				enableFallback(s)
			}
			g := s.group
			g.logger = s.logger
			g.pause = pause.ManagerFromContext(pause.WithDefaultManager(context.Background()))
			entered := make(chan struct{})
			release := make(chan struct{})
			newHTTP := make(chan struct{}, 1)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { newHTTP <- struct{}{}; w.WriteHeader(204) }))
			defer server.Close()
			all[0].dial = func(ctx context.Context) (net.Conn, error) {
				if calls.Add(1) == 1 {
					close(entered)
					<-release
				}
				return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
			}
			g.link = server.URL
			if fallback {
				g.recordFallback(all[0], g.beginFallbackObservation(), false, nil)
			}
			oldDone := make(chan struct{})
			go func() { g.CheckOutbounds(context.Background(), true); close(oldDone) }()
			<-entered
			newest, cancel := context.WithCancel(context.Background())
			defer cancel()
			s.InterfaceUpdated(newest)
			// Allow the original InterfaceUpdated goroutine to encounter the occupied gate.
			time.Sleep(30 * time.Millisecond)
			close(release)
			<-oldDone
			select {
			case <-newHTTP:
			case <-time.After(time.Second):
				t.Fatal("latest recovery was dropped behind blocked old probe")
			}
			deadline := time.Now().Add(time.Second)
			for g.history.LoadURLTestHistory(all[0].Tag()) == nil && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if calls.Load() != 2 {
				t.Fatalf("dial calls=%d want old + newest recovery", calls.Load())
			}
			if g.history.LoadURLTestHistory(all[0].Tag()) == nil {
				t.Fatal("new real HTTP did not publish measured history")
			}
			if fallback {
				g.fallback.mu.Lock()
				h := g.fallback.nodes[all[0].Tag()]
				g.fallback.mu.Unlock()
				if !h.healthy || !h.retryAfter.IsZero() {
					t.Fatal("successful new probe did not clear cooldown")
				}
			}
			t.Log("blocked old probe superseded; newest generation real HTTP measured and committed")
		})
	}
}
