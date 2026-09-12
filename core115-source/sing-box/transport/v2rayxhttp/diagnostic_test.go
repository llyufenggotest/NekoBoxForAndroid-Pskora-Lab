package xhttp

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sagernet/sing-box/option"
	D "github.com/sagernet/sing-tun/diagnostic"
)

type diagnosticCapture struct {
	sync.Mutex
	lines []string
}

type diagnosticErrorTransport struct{}

func (diagnosticErrorTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("PRIVATE https://secret.invalid/UUID")
}

func (c *diagnosticCapture) DiagnosticEnabled() bool { return true }
func (c *diagnosticCapture) Debug(a ...any) {
	c.Lock()
	defer c.Unlock()
	c.lines = append(c.lines, fmt.Sprint(a...))
}
func TestDiagnosticRealHTTP2(t *testing.T) {
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			t.Error("not h2")
		}
		w.Write([]byte("test-body"))
	}))
	s.EnableHTTP2 = true
	s.StartTLS()
	defer s.Close()
	c := &DefaultDialerClient{client: s.Client(), options: &option.V2RayXHTTPBaseOptions{}, httpVersion: "2"}
	l := &diagnosticCapture{}
	for i := 0; i < 2; i++ {
		ctx := D.BeginTUN(context.Background(), l, 1234, []string{"org.telegram.messenger"}, 10537, true)
		ctx, f := D.New(ctx, l, 0, 0, 0, 0, false, 1)
		r, _, _, e := c.OpenStream(ctx, s.URL+"/SECRET-UUID", "", nil, false)
		if e != nil {
			t.Fatal(e)
		}
		b, e := io.ReadAll(r)
		f.IO(false, len(b), e)
		r.Close()
		f.Closed(nil)
		if e != nil || string(b) != "test-body" {
			t.Fatal(string(b), e)
		}
	}
	time.Sleep(10 * time.Millisecond)
	l.Lock()
	defer l.Unlock()
	text := strings.Join(l.lines, "\n")
	for _, v := range []string{"schema=3", "owner=true", "event=14", "sport=1234", "app=1", "subapp=1", "uid=10537", "event=2", "reused=true", "event=3", "value=200", "event=4", "event=5", "event=7"} {
		if !strings.Contains(text, v) {
			t.Fatalf("missing %s: %s", v, text)
		}
	}
	for _, v := range []string{"SECRET", "UUID", "test-body", "127.0.0.1", "https://"} {
		if strings.Contains(text, v) {
			t.Fatal("leak", v)
		}
	}
	t.Log(text)
}

func TestDiagnosticHTTPRequestErrorBoundary(t *testing.T) {
	l := &diagnosticCapture{}
	ctx := D.BeginTUN(context.Background(), l, 4321, []string{"tw.nekomimi.nekogram"}, 10480, true)
	ctx, _ = D.New(ctx, l, 0, 0, 0, 0, false, 1)
	c := &DefaultDialerClient{client: &http.Client{Transport: diagnosticErrorTransport{}}, options: &option.V2RayXHTTPBaseOptions{}, httpVersion: "2", closed: true}
	_, _, _, err := c.OpenStream(ctx, "https://secret.invalid/UUID", "", nil, false)
	if err == nil {
		t.Fatal("expected request error")
	}
	l.Lock()
	defer l.Unlock()
	text := strings.Join(l.lines, "\n")
	for _, want := range []string{"event=15", "error=6", "app=1", "subapp=2", "uid=10480", "sport=4321"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s: %s", want, text)
		}
	}
	for _, secret := range []string{"PRIVATE", "secret.invalid", "UUID", "https://"} {
		if strings.Contains(text, secret) {
			t.Fatalf("leaked %s", secret)
		}
	}
}
