package diagnostic

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

type recoveryCapture struct {
	enabled bool
	mu      sync.Mutex
	lines   []string
}

func (c *recoveryCapture) RecoveryDiagnosticEnabled() bool { return c.enabled }
func (c *recoveryCapture) Debug(v ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = append(c.lines, fmt.Sprint(v...))
}

func TestRecoveryTraceSchemaPrivacyAndLimits(t *testing.T) {
	ResetRecoveryForTest()
	c := &recoveryCapture{enabled: true}
	for i := 0; i < 100; i++ {
		EmitRecovery(c, RecoveryRecord{Event: RecoveryOnlineSnapshot, Epoch: 7, Generation: 9, ForcedSame: true, Abort: 1, Fallback: 2, Action: 2, Result: 1})
	}
	if len(c.lines) != RecoveryPerEpochLimit {
		t.Fatalf("per epoch lines=%d", len(c.lines))
	}
	line := strings.Join(c.lines, "\n")
	for _, want := range []string{"RECOVTRACE schema=4", "event=2", "epoch=7", "gen=9", "forced=true", "abort=1", "fallback=2", "action=2", "result=1"} {
		if !strings.Contains(line, want) {
			t.Fatalf("missing %q: %s", want, line)
		}
	}
	for _, forbidden := range []string{"DTRACE3", "uid=", "port=", "address=", "payload"} {
		if strings.Contains(strings.ToLower(line), strings.ToLower(forbidden)) {
			t.Fatalf("privacy leak %q", forbidden)
		}
	}
	c.enabled = false
	EmitRecovery(c, RecoveryRecord{Event: RecoveryOffline, Epoch: 8})
	if len(c.lines) != RecoveryPerEpochLimit {
		t.Fatal("disabled trace emitted")
	}
}
