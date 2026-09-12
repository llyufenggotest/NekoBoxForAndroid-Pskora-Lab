package route

import (
	"context"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/common/control"
	"github.com/sagernet/sing/common/logger"
	"github.com/sagernet/sing/service/pause"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type recoveryOutbound struct {
	adapter.Outbound
	contexts chan context.Context
}

func (o *recoveryOutbound) InterfaceUpdated(ctx context.Context) { o.contexts <- ctx }

type recoveryOutbounds struct {
	adapter.OutboundManager
	o adapter.Outbound
}

func (o recoveryOutbounds) Outbounds() []adapter.Outbound { return []adapter.Outbound{o.o} }

// Captured interface order: cellular ccmni3 -> lost -> ccmni1 -> ccmni3.
// Listeners such as URLTest start asynchronous work with the supplied context.
func TestRecoveryContextSurvivesDispatchUntilNextNetwork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	o := &recoveryOutbound{contexts: make(chan context.Context, 3)}
	router := &startupRouter{resets: make(chan struct{}, 3)}
	plat := &startupPlatform{entered: make(chan struct{}, 3), release: make(chan struct{})}
	close(plat.release)
	r := &NetworkManager{ctx: ctx, logger: logger.NOP(), platformInterface: plat,
		pauseManager: pause.ManagerFromContext(pause.WithDefaultManager(ctx)), router: router,
		endpoint: startupEndpoints{}, inbound: startupInbounds{}, outbound: recoveryOutbounds{o: o}}
	r.started.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "recovered") }))
	defer server.Close()
	client := &http.Client{Timeout: time.Second}
	var previous context.Context
	for _, intf := range []*control.Interface{{Name: "ccmni3", Index: 5}, nil, {Name: "ccmni1", Index: 3}, {Name: "ccmni3", Index: 5}} {
		r.notifyInterfaceUpdate(intf, 0)
		if intf == nil {
			if previous != nil && previous.Err() == nil {
				t.Fatal("lost network did not cancel old recovery generation")
			}
			continue
		}
		var got context.Context
		select {
		case got = <-o.contexts:
		case <-time.After(time.Second):
			t.Fatal("missing recovery dispatch")
		}
		<-router.resets
		r.interfaceUpdateRunAccess.Lock()
		r.interfaceUpdateRunAccess.Unlock()
		if got.Err() != nil {
			t.Fatalf("%s recovery context canceled as soon as dispatch returned: %v", intf.Name, got.Err())
		}
		if previous != nil && previous.Err() == nil {
			t.Fatal("superseded network context not canceled")
		}
		request, err := http.NewRequestWithContext(got, http.MethodGet, server.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("post-dispatch real HTTP recovery: %v", err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || string(body) != "recovered" {
			t.Fatalf("recovery body=%q err=%v", body, err)
		}
		t.Logf("%s: real loopback HTTP succeeds after dispatch returns", intf.Name)
		previous = got
	}
	cancel()
	if previous.Err() == nil {
		t.Fatal("shutdown did not cancel recovery context")
	}
}
