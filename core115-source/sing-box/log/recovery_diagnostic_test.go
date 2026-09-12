package log

import (
	"bytes"
	"context"
	D "github.com/sagernet/sing-tun/diagnostic"
	"strings"
	"testing"
	"time"
)

func TestRecoveryDiagnosticEnabledAtDebugAndTrace(t *testing.T) {
	D.ResetRecoveryForTest()
	var b bytes.Buffer
	f := NewDefaultFactory(context.Background(), Formatter{}, &b, "", nil, false)
	f.Start()
	defer f.Close()
	l := f.Logger()
	f.SetLevel(LevelInfo)
	D.EmitRecovery(l, D.RecoveryRecord{Event: D.RecoveryOffline, Epoch: 1})
	f.SetLevel(LevelDebug)
	D.EmitRecovery(l, D.RecoveryRecord{Event: D.RecoveryOffline, Epoch: 2})
	time.Sleep(10 * time.Millisecond)
	output := b.String()
	if strings.Contains(output, "epoch=1") {
		t.Fatalf("recovery diagnostic enabled at info: %q", output)
	}
	if !strings.Contains(output, "RECOVTRACE schema=3") || !strings.Contains(output, "epoch=2") {
		t.Fatalf("recovery diagnostic missing at debug: %q", output)
	}
	if strings.Contains(output, "DTRACE3") {
		t.Fatal("traffic diagnostic restored")
	}
}
