package route

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	D "github.com/sagernet/sing-tun/diagnostic"
	"github.com/sagernet/sing/common/logger"
)

type recoveryLog struct {
	logger.ContextLogger
	lines chan string
}

func (l recoveryLog) DiagnosticEnabled() bool         { return true }
func (l recoveryLog) RecoveryDiagnosticEnabled() bool { return true }
func (l recoveryLog) Debug(v ...any)                  { l.lines <- fmt.Sprint(v...) }

type traceConnections struct {
	adapter.ConnectionManager
	generation  atomic.Uint64
	closeTarget chan uint64
}

func (c *traceConnections) AdvanceNetworkGeneration() uint64   { return c.generation.Add(1) }
func (c *traceConnections) NetworkGeneration() uint64          { return c.generation.Load() }
func (c *traceConnections) TrackTUNTCP(conn net.Conn) net.Conn { return conn }
func (c *traceConnections) CloseOldTUNTCP(g uint64) TUNTCPRecoveryResult {
	c.closeTarget <- g
	return TUNTCPRecoveryResult{Result: TUNTCPRecoveryResultSuccess}
}

func TestRecoveryTraceRapidRescheduleMatchesBehavior(t *testing.T) {
	D.ResetRecoveryForTest()
	old := tunTCPRecoverySettleDuration
	tunTCPRecoverySettleDuration = 10 * time.Millisecond
	defer func() { tunTCPRecoverySettleDuration = old }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lines := make(chan string, 16)
	conns := &traceConnections{closeTarget: make(chan uint64, 2)}
	r := &NetworkManager{ctx: ctx, logger: recoveryLog{ContextLogger: logger.NOP(), lines: lines}, connectionManager: conns}
	r.recoveryEpoch.Store(4)
	g1 := conns.AdvanceNetworkGeneration()
	r.scheduleOldTUNTCPRecovery(ctx, g1, 4, false)
	g2 := conns.AdvanceNetworkGeneration()
	r.scheduleOldTUNTCPRecovery(ctx, g2, 4, false)
	select {
	case got := <-conns.closeTarget:
		if got != g2 {
			t.Fatalf("closed generation=%d want=%d", got, g2)
		}
	case <-time.After(time.Second):
		t.Fatal("settle not executed")
	}
	var all []string
	deadline := time.After(100 * time.Millisecond)
loop:
	for {
		select {
		case s := <-lines:
			all = append(all, s)
		case <-deadline:
			break loop
		}
	}
	joined := strings.Join(all, "\n")
	for _, want := range []string{"event=4", "event=5", "event=7", "event=8", "target=2", "action=0", "result=1"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %s", want, joined)
		}
	}
}
