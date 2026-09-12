package route

import (
	"context"
	"testing"
	"time"

	"github.com/sagernet/sing/common/logger"
)

func TestCloseCancelsPendingTUNTCPRecovery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	oldSettle := tunTCPRecoverySettleDuration
	tunTCPRecoverySettleDuration = 100 * time.Millisecond
	defer func() { tunTCPRecoverySettleDuration = oldSettle }()
	connections := &startupConnections{}
	r := &NetworkManager{ctx: ctx, logger: logger.NOP()}
	r.BindConnectionManager(connections)
	generation := connections.AdvanceNetworkGeneration()
	r.scheduleOldTUNTCPRecovery(ctx, generation, 1, false)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * tunTCPRecoverySettleDuration)
	if got := connections.attempted.Load(); got != 0 {
		t.Fatalf("settle executed after Close: attempts=%d", got)
	}
}

func TestNewNetworkManagerDoesNotReuseOldGenerationManager(t *testing.T) {
	ctx := context.Background()
	oldConnections := &startupConnections{}
	old := &NetworkManager{ctx: ctx, logger: logger.NOP()}
	old.BindConnectionManager(oldConnections)
	old.started.Store(true)
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}

	freshConnections := &startupConnections{}
	fresh := &NetworkManager{ctx: ctx, logger: logger.NOP()}
	fresh.BindConnectionManager(freshConnections)
	if got, ok := fresh.generationManager(); !ok || got != freshConnections {
		t.Fatal("fresh NetworkManager did not bind its own generation manager")
	}
	if fresh.started.Load() {
		t.Fatal("fresh NetworkManager inherited old started state")
	}
	if oldConnections.NetworkGeneration() != 0 || freshConnections.NetworkGeneration() != 0 {
		t.Fatal("generation state leaked across NetworkManager instances")
	}
}
