package clashapi

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

type memoryWriter struct {
	header http.Header
	writes atomic.Int32
	ready  chan struct{}
}

func (w *memoryWriter) Header() http.Header { return w.header }
func (*memoryWriter) WriteHeader(int)       {}
func (w *memoryWriter) Write(p []byte) (int, error) {
	if w.writes.Add(1) == 1 && w.ready != nil {
		close(w.ready)
	}
	return len(p), nil
}
func (*memoryWriter) Flush() {}

type blockedMemoryWriter struct {
	memoryWriter
	entered   chan struct{}
	unblock   chan struct{}
	deadlines atomic.Int32
}

func (w *blockedMemoryWriter) Write([]byte) (int, error) {
	close(w.entered)
	<-w.unblock
	return 0, errors.New("connection closed")
}
func (w *blockedMemoryWriter) SetWriteDeadline(time.Time) error {
	if w.deadlines.Add(1) == 1 {
		close(w.unblock)
	}
	return nil
}
func TestMemoryBlockedWriteCancellation(t *testing.T) {
	for _, owner := range []string{"request", "core"} {
		t.Run(owner, func(t *testing.T) {
			core, stopCore := context.WithCancel(context.Background())
			defer stopCore()
			req, stopRequest := context.WithCancel(context.Background())
			defer stopRequest()
			w := &blockedMemoryWriter{memoryWriter: memoryWriter{header: make(http.Header)}, entered: make(chan struct{}), unblock: make(chan struct{})}
			done := make(chan struct{})
			go func() { memory(core)(w, httptest.NewRequest("GET", "/memory", nil).WithContext(req)); close(done) }()
			<-w.entered
			if owner == "request" {
				stopRequest()
			} else {
				stopCore()
			}
			select {
			case <-done:
			case <-time.After(100 * time.Millisecond):
				w.SetWriteDeadline(time.Now())
				<-done
				t.Fatal("memory write remained blocked after " + owner + " cancellation")
			}
		})
	}
}
func TestMemoryRealHTTPDisconnect(t *testing.T) {
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { defer close(done); memory(context.Background())(w, r) }))
	defer server.Close()
	response, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(response.Body).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if line != "{\"inuse\":0,\"oslimit\":0}\n" {
		t.Fatalf("first sample contract: %s", line)
	}
	response.Body.Close()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("disconnected real HTTP handler did not retire")
	}
}
func TestMemoryRealWebSocketCoreCancellation(t *testing.T) {
	core, stop := context.WithCancel(context.Background())
	defer stop()
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { defer close(done); memory(core)(w, r) }))
	defer server.Close()
	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, err = conn.Write([]byte("GET /memory HTTP/1.1\r\nHost: localhost\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, nil)
	if err != nil || response.StatusCode != 101 {
		t.Fatalf("upgrade: %v %v", response, err)
	}
	stop()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("hijacked websocket ignored core cancellation")
	}
	conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if _, err = reader.ReadByte(); err == nil {
		t.Fatal("canceled websocket emitted a late sample")
	}
}

func TestMemoryRequestCancellationRetires(t *testing.T) {
	core, stopCore := context.WithCancel(context.Background())
	defer stopCore()
	req, stopRequest := context.WithCancel(context.Background())
	w := &memoryWriter{header: make(http.Header), ready: make(chan struct{})}
	done := make(chan struct{})
	go func() { memory(core)(w, httptest.NewRequest("GET", "/memory", nil).WithContext(req)); close(done) }()
	<-w.ready
	stopRequest()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		stopCore()
		<-done
		t.Fatal("memory ticker outlived request cancellation")
	}
	before := w.writes.Load()
	time.Sleep(1100 * time.Millisecond)
	if w.writes.Load() != before {
		t.Fatal("retired memory stream wrote after cancellation")
	}
}
