package route

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/common/control"
	"github.com/sagernet/sing/common/logger"
	"github.com/sagernet/sing/service/pause"
)

type startupRouter struct {
	adapter.Router
	resets chan struct{}
}

func (r *startupRouter) ResetNetwork() { r.resets <- struct{}{} }

type startupEndpoints struct{ adapter.EndpointManager }

func (startupEndpoints) Endpoints() []adapter.Endpoint { return nil }

type startupInbounds struct{ adapter.InboundManager }

func (startupInbounds) Inbounds() []adapter.Inbound { return nil }

type startupOutbounds struct{ adapter.OutboundManager }

func (startupOutbounds) Outbounds() []adapter.Outbound { return nil }

type startupConnections struct {
	adapter.ConnectionManager
	generation atomic.Uint64
	closed     atomic.Int32
	attempted  atomic.Int32
}

func (c *startupConnections) CloseAll()                          { c.closed.Add(1000) }
func (c *startupConnections) AdvanceNetworkGeneration() uint64   { return c.generation.Add(1) }
func (c *startupConnections) NetworkGeneration() uint64          { return c.generation.Load() }
func (c *startupConnections) TrackTUNTCP(conn net.Conn) net.Conn { return conn }
func (c *startupConnections) CloseOldTUNTCP(generation uint64) TUNTCPRecoveryResult {
	c.attempted.Add(1)
	if generation == c.generation.Load() {
		c.closed.Add(1)
		return TUNTCPRecoveryResult{Closed: 1}
	}
	return TUNTCPRecoveryResult{}
}

type startupPlatform struct {
	adapter.PlatformInterface
	entered chan struct{}
	release chan struct{}
}

func (*startupPlatform) UsePlatformNetworkInterfaces() bool { return false }
func (*startupPlatform) UsePlatformWIFIMonitor() bool       { return true }
func (p *startupPlatform) ReadWIFIState(ctx context.Context) adapter.WIFIState {
	p.entered <- struct{}{}
	select {
	case <-p.release:
	case <-ctx.Done():
	}
	return adapter.WIFIState{}
}

// A real asynchronous notification is held in WIFI discovery across PostStart.
// Unlike timing-only tests this deterministically recreates the causal ordering.
func TestLatestGenerationReschedulesSettleAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	oldSettle := tunTCPRecoverySettleDuration
	tunTCPRecoverySettleDuration = 20 * time.Millisecond
	defer func() { tunTCPRecoverySettleDuration = oldSettle }()
	connections := &startupConnections{}
	r := &NetworkManager{ctx: ctx, logger: logger.NOP(), connectionManager: connections}

	firstCtx, firstCancel := context.WithCancel(ctx)
	first := connections.AdvanceNetworkGeneration()
	r.scheduleOldTUNTCPRecovery(firstCtx, first, 0, false)
	firstCancel()
	latest := connections.AdvanceNetworkGeneration()
	r.scheduleOldTUNTCPRecovery(ctx, latest, 0, false)

	deadline := time.Now().Add(time.Second)
	for connections.attempted.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if connections.attempted.Load() != 1 || connections.closed.Load() != 1 {
		t.Fatalf("latest settle attempts=%d closed=%d", connections.attempted.Load(), connections.closed.Load())
	}
}

func TestInitialNotificationDoesNotResetLiveConnections(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	plat := &startupPlatform{entered: make(chan struct{}, 1), release: make(chan struct{})}
	router := &startupRouter{resets: make(chan struct{}, 2)}
	connections := &startupConnections{}
	r := &NetworkManager{ctx: ctx, logger: logger.NOP(), platformInterface: plat,
		pauseManager: pause.ManagerFromContext(pause.WithDefaultManager(ctx)), router: router, connectionManager: connections,
		endpoint: startupEndpoints{}, inbound: startupInbounds{}, outbound: startupOutbounds{}}
	r.notifyInterfaceUpdate(&control.Interface{Name: "initial", Index: 1}, 0)
	select {
	case <-plat.entered:
	case <-time.After(time.Second):
		t.Fatal("initial callback not running")
	}
	if err := r.Start(adapter.StartStatePostStart); err != nil {
		t.Fatal(err)
	}
	close(plat.release)
	r.interfaceUpdateRunAccess.Lock()
	r.interfaceUpdateRunAccess.Unlock()
	if connections.closed.Load() != 0 {
		t.Fatal("initial notification closed live connections after PostStart")
	}
	oldSettle := tunTCPRecoverySettleDuration
	tunTCPRecoverySettleDuration = time.Millisecond
	defer func() { tunTCPRecoverySettleDuration = oldSettle }()
	// Post-start notification must still refresh and retire only stale TUN TCP.
	r.notifyInterfaceUpdate(&control.Interface{Name: "changed", Index: 2}, 0)
	select {
	case <-router.resets:
	case <-time.After(time.Second):
		t.Fatal("network change did not reset")
	}
	deadline := time.Now().Add(time.Second)
	for connections.closed.Load() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if connections.closed.Load() != 1 {
		t.Fatalf("stale TUN TCP close count = %d", connections.closed.Load())
	}
}
