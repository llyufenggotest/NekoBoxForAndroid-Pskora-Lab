package xhttp

import (
	"bytes"
	"context"
	"fmt"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	D "github.com/sagernet/sing-tun/diagnostic"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/service"
	"io"
	"strings"
	"testing"
)

func TestProductionDiagnosticFactoryEnabledAtDebug(t *testing.T) {
	var b bytes.Buffer
	factory := log.NewDefaultFactory(context.Background(), log.Formatter{}, &b, "", nil, false)
	factory.SetLevel(log.LevelDebug)
	factory.Start()
	defer factory.Close()
	ctx := service.ContextWith[log.Factory](context.Background(), factory)
	ctx = D.BeginTUN(ctx, factory.NewLogger("diagnostic"), 1234, []string{"tw.nekomimi.nekogram"}, 10480, true)
	_, f := diagnosticFlow(ctx, nil)
	if f == nil {
		t.Fatal("debug diagnostic factory did not create a flow")
	}
	f.Event(D.Record{Event: D.GotConn})
	if !strings.Contains(b.String(), "DTRACE3 schema=3") {
		t.Fatal("debug diagnostic factory did not emit")
	}
}

func TestProductionDiagnosticFlowCapturesRoutedOwner(t *testing.T) {
	for _, tc := range []struct {
		name     string
		uid      int32
		packages []string
	}{
		{name: "classified", uid: 10537, packages: []string{"org.telegram.messenger"}},
		{name: "unclassified owner", uid: 10480, packages: []string{""}},
		{name: "nekogram", uid: 10480, packages: []string{"tw.nekomimi.nekogram"}},
		{name: "chrome", uid: 11918, packages: []string{"com.android.chrome"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			factory := log.NewDefaultFactory(context.Background(), log.Formatter{}, &output, "", nil, false)
			factory.SetLevel(log.LevelDebug)
			factory.Start()
			defer factory.Close()
			metadata := &adapter.InboundContext{
				Source: M.ParseSocksaddr("172.19.0.1:38428"),
				ProcessInfo: &adapter.ConnectionOwner{
					UserId:       tc.uid,
					PackageNames: tc.packages,
				},
			}
			ctx := adapter.WithContext(context.Background(), metadata)
			ctx = D.BeginTUN(ctx, factory.NewLogger("diagnostic"), metadata.Source.Port, tc.packages, tc.uid, true)
			_, flow := diagnosticFlow(ctx, factory.NewLogger("diagnostic"))
			if flow == nil {
				t.Fatal("production transport did not capture routed owner diagnostic")
			}
			flow.Event(D.Record{Event: D.GotConn})
			text := output.String()
			if !strings.Contains(text, "DTRACE3 schema=3") || !strings.Contains(text, fmt.Sprintf("uid=%d", tc.uid)) || !strings.Contains(text, "owner=true") {
				t.Fatalf("runtime owner diagnostic missing: %s", text)
			}
		})
	}
}
func TestDiagnosticSplitIO(t *testing.T) {
	l := &diagnosticCapture{}
	_, f := D.New(context.Background(), l, 1, 1, 123, 1, true, 1)
	r, w := io.Pipe()
	c := &splitConn{reader: r, writer: w, diagnostic: f}
	done := make(chan error, 1)
	go func() { _, e := c.Write([]byte("PRIVATE-payload")); done <- e }()
	b := make([]byte, 64)
	n, e := c.Read(b)
	if e != nil || string(b[:n]) != "PRIVATE-payload" {
		t.Fatal("wire changed")
	}
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	c.Close()
	c.Read(b)
	l.Lock()
	defer l.Unlock()
	s := strings.Join(l.lines, "\n")
	for _, want := range []string{"event=5", "event=6", "event=7", "error=5"} {
		if !strings.Contains(s, want) {
			t.Fatal(s)
		}
	}
	if strings.Contains(s, "PRIVATE") {
		t.Fatal("leak")
	}
}
