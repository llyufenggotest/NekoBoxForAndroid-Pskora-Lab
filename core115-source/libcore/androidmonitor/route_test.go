package androidmonitor

import (
	"context"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/route"
	tun "github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common/logger"
	"github.com/sagernet/sing/service"
	"testing"
)

// Compile this same test for GOOS=android: no host-safe config substitution.
type routingPlatform struct {
	adapter.PlatformInterface
	p       *fakePlatform
	nm      adapter.NetworkManager
	created int
}

func (p *routingPlatform) UsePlatformDefaultInterfaceMonitor() bool { return true }
func (p *routingPlatform) CreateDefaultInterfaceMonitor(l logger.Logger) tun.DefaultInterfaceMonitor {
	p.created++
	return New(p.p, func() adapter.NetworkManager { return p.nm }, l)
}
func TestActualCoreSelectsJavaMonitorWithAutoDetect(t *testing.T) {
	p := &routingPlatform{p: &fakePlatform{}}
	ctx := service.ContextWith[adapter.PlatformInterface](context.Background(), p)
	nm, err := route.NewNetworkManager(ctx, logger.NOP(), option.RouteOptions{AutoDetectInterface: true}, option.DNSOptions{})
	if err != nil {
		t.Fatal(err)
	}
	p.nm = nm
	if p.created != 1 {
		t.Fatal("core did not select platform monitor")
	}
	if err = nm.Start(adapter.StartStateInitialize); err != nil {
		t.Fatal(err)
	}
	if p.p.starts != 1 {
		t.Fatal("Java registration not reached")
	}
	if err = nm.InterfaceMonitor().Close(); err != nil {
		t.Fatal(err)
	}
	if p.p.closes != 1 {
		t.Fatal("Java cancellation not reached")
	}
}
