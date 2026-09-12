package log

import (
	"bytes"
	"context"
	D "github.com/sagernet/sing-tun/diagnostic"
	"strings"
	"testing"
	"time"
)

func TestDiagnosticLevelGate(t *testing.T) {
	var b bytes.Buffer
	f := NewDefaultFactory(context.Background(), Formatter{}, &b, "", nil, false)
	f.Start()
	defer f.Close()
	l := f.Logger()
	f.SetLevel(LevelInfo)
	D.Emit(l, D.Record{Event: D.Start})
	if b.Len() != 0 {
		t.Fatal("info leak")
	}
	f.SetLevel(LevelDebug)
	D.Emit(l, D.Record{Event: D.Start})
	if !strings.Contains(b.String(), "DTRACE3 schema=3") {
		t.Fatal("debug diagnostic output disabled")
	}
	n := b.Len()
	f.SetLevel(LevelInfo)
	D.Emit(l, D.Record{Event: D.Start})
	time.Sleep(time.Millisecond)
	if b.Len() != n {
		t.Fatal("disable failed")
	}
}
