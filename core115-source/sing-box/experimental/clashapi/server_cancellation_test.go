package clashapi

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/trafficcontrol"
	"github.com/sagernet/sing-box/common/urltest"
	"github.com/sagernet/sing-box/experimental/clashmode"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/service"
)

func lifecycleServer(t *testing.T, ctx context.Context) *Server {
	t.Helper()
	ctx = service.ContextWithPtr(ctx, trafficcontrol.NewManager(nil))
	ctx = service.ContextWithPtr(ctx, urltest.NewHistoryStorage())
	ctx = service.ContextWithPtr(ctx, clashmode.NewManager(ctx, log.NewNOPFactory().Logger(), "", nil))
	instance, err := NewServer(ctx, log.NewNOPFactory(), option.ClashAPIOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return instance.(*Server)
}
func TestClashScopeRemoveRetiresHandlers(t *testing.T) {
	root, stop := context.WithCancel(context.Background())
	defer stop()
	manager := adapter.NewScope(root, log.NewNOPFactory().Logger())
	defer manager.Close()
	s := lifecycleServer(t, root)
	if err := manager.Start(s.Name(), s, adapter.StartStateInitialize); err != nil {
		t.Fatal(err)
	}
	w := &memoryWriter{header: make(map[string][]string), ready: make(chan struct{})}
	done := make(chan struct{})
	go func() { s.httpServer.Handler.ServeHTTP(w, httptest.NewRequest("GET", "/memory", nil)); close(done) }()
	<-w.ready
	if err := manager.Remove(s); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		stop()
		<-done
		t.Fatal("Clash child removal retained root-owned memory handler")
	}
	if root.Err() != nil {
		t.Fatal("child retirement canceled core")
	}
	// A rebuilt component is a fresh epoch, not the canceled predecessor.
	next := lifecycleServer(t, root)
	if err := manager.Start(next.Name(), next, adapter.StartStateInitialize); err != nil {
		t.Fatal(err)
	}
	if next.ctx.Err() != nil {
		t.Fatal("new server inherited retired epoch")
	}
}
