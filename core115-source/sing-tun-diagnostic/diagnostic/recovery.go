package diagnostic

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// Recovery tracing is isolated from DTRACE traffic diagnostics. It accepts only
// numeric/boolean trigger state and is enabled by Debug/Trace through a separate gate.
type recoveryGate interface{ RecoveryDiagnosticEnabled() bool }

const (
	RecoveryProcessLimit  uint64 = 4096
	RecoveryPerEpochLimit        = 32
)

type RecoveryEvent uint8

const (
	RecoveryOffline RecoveryEvent = iota + 1
	RecoveryOnlineSnapshot
	RecoveryGenerationAdvance
	RecoverySettleScheduled
	RecoverySettleRescheduled
	RecoverySettleCanceled
	RecoverySettleExecuted
	RecoveryCloseOldTUNTCP
	RecoveryTrack
	RecoveryUntrack
)

const (
	RecoveryCancelNone uint8 = iota
	RecoveryCancelSuperseded
	RecoveryCancelOffline
	RecoveryCancelShutdown
	RecoveryCancelContext
	RecoveryCancelSnapshotChanged
	RecoveryCancelRouteReadinessLost
)

type RecoveryRecord struct {
	Event                                                  RecoveryEvent
	Epoch, Generation, CurrentGeneration, TargetGeneration uint64
	ForcedSame, Online                                     bool
	Reason                                                 uint8
	Delay                                                  uint16
	Count, Active, Abort, Fallback                         int
	Action, Result                                        uint8
}

var recoveryEmitted atomic.Uint64
var recoveryEpochMu sync.Mutex
var recoveryEpochCounts = map[uint64]uint8{}

func RecoveryEnabled(l Logger) bool {
	g, ok := l.(recoveryGate)
	return ok && g.RecoveryDiagnosticEnabled()
}

func EmitRecovery(l Logger, r RecoveryRecord) {
	if !RecoveryEnabled(l) {
		return
	}
	recoveryEpochMu.Lock()
	if recoveryEpochCounts[r.Epoch] >= RecoveryPerEpochLimit {
		recoveryEpochMu.Unlock()
		return
	}
	for {
		n := recoveryEmitted.Load()
		if n >= RecoveryProcessLimit {
			recoveryEpochMu.Unlock()
			return
		}
		if recoveryEmitted.CompareAndSwap(n, n+1) {
			break
		}
	}
	recoveryEpochCounts[r.Epoch]++
	recoveryEpochMu.Unlock()
	l.Debug(fmt.Sprintf("RECOVTRACE schema=4 event=%d epoch=%d gen=%d current=%d target=%d online=%t forced=%t delay=%d reason=%d count=%d active=%d abort=%d fallback=%d action=%d result=%d", r.Event, r.Epoch, r.Generation, r.CurrentGeneration, r.TargetGeneration, r.Online, r.ForcedSame, r.Delay, r.Reason, r.Count, r.Active, r.Abort, r.Fallback, r.Action, r.Result))
}

func ResetRecoveryForTest() {
	recoveryEmitted.Store(0)
	recoveryEpochMu.Lock()
	recoveryEpochCounts = map[uint64]uint8{}
	recoveryEpochMu.Unlock()
}
