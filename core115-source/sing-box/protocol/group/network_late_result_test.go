package group

import (
	"context"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/service/pause"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// Real HTTP on both live paths; old response deliberately arrives after recovery.
func TestNetworkRecoveryLateHTTPDoesNotPublish(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		t.Run(map[bool]string{false: "standard", true: "fallback"}[fallback], func(t *testing.T) {
			s, all := fixture(t, 1)
			if fallback {
				enableFallback(s)
			}
			g := s.group
			g.logger = s.logger
			g.pause = pause.ManagerFromContext(pause.WithDefaultManager(context.Background()))
			oldEntered := make(chan struct{})
			oldRelease := make(chan struct{})
			newEntered := make(chan struct{})
			newRelease := make(chan struct{})
			var hits atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch hits.Add(1) {
				case 1:
					close(oldEntered)
					<-oldRelease
				case 2:
					close(newEntered)
					<-newRelease
				}
				w.WriteHeader(204)
			}))
			defer server.Close()
			all[0].dial = func(ctx context.Context) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
			}
			g.link = server.URL
			original := &adapter.URLTestHistory{Time: time.Now(), Delay: 123}
			if fallback {
				g.recordFallback(all[0], g.beginFallbackObservation(), false, nil)
			} else {
				g.history.StoreURLTestHistory(all[0].Tag(), original)
			}
			done := make(chan struct{})
			go func() { g.CheckOutbounds(context.Background(), true); close(done) }()
			<-oldEntered
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s.InterfaceUpdated(ctx)
			select {
			case <-newEntered:
			case <-time.After(time.Second):
				close(oldRelease)
				close(newRelease)
				t.Fatal("recovery HTTP never started")
			}
			close(oldRelease)
			<-done
			if fallback {
				g.fallback.mu.Lock()
				h := g.fallback.nodes[all[0].Tag()]
				g.fallback.mu.Unlock()
				if h.healthy || h.retryAfter.IsZero() {
					close(newRelease)
					t.Fatal("old response cleared cooldown before new success")
				}
			} else if g.history.LoadURLTestHistory(all[0].Tag()) != original {
				close(newRelease)
				t.Fatal("canceled old generation mutated history")
			}
			close(newRelease)
			deadline := time.Now().Add(time.Second)
			for time.Now().Before(deadline) {
				h := g.history.LoadURLTestHistory(all[0].Tag())
				if h != nil && h != original {
					break
				}
				time.Sleep(time.Millisecond)
			}
			h := g.history.LoadURLTestHistory(all[0].Tag())
			if h == nil || h == original {
				t.Fatal("new real HTTP did not publish")
			}
			t.Log("live A HTTP blocked; live B recovery HTTP started; late A cannot alter history/cooldown; only B success publishes")
		})
	}
}
