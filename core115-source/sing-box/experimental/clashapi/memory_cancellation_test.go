package clashapi

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// saturatedMemoryWriter expands just the test payload while preserving the real
// net/http writer/controller/connection. A non-reading peer must block TCP Write.
type saturatedMemoryWriter struct {
	http.ResponseWriter
	entered chan struct{}
	once    sync.Once
}

func (w *saturatedMemoryWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *saturatedMemoryWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	_, err := w.ResponseWriter.Write(bytes.Repeat([]byte("x"), 8<<20))
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func TestMemoryRealSocketSaturatedCancellation(t *testing.T) {
	for _, owner := range []string{"request", "core"} {
		t.Run(owner, func(t *testing.T) {
			core, stopCore := context.WithCancel(context.Background())
			defer stopCore()
			req, stopRequest := context.WithCancel(context.Background())
			defer stopRequest()
			done, entered := make(chan struct{}), make(chan struct{})
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(done)
				memory(core)(&saturatedMemoryWriter{ResponseWriter: w, entered: entered}, r.WithContext(req))
			}))
			server.Config.ConnContext = func(ctx context.Context, c net.Conn) context.Context {
				if err := c.(*net.TCPConn).SetWriteBuffer(1024); err != nil {
					t.Error(err)
				}
				return ctx
			}
			server.Start()
			defer server.Close()
			conn, err := net.DialTCP("tcp", nil, server.Listener.Addr().(*net.TCPAddr))
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if err = conn.SetReadBuffer(1024); err != nil {
				t.Fatal(err)
			}
			if _, err = conn.Write([]byte("GET /memory HTTP/1.1\r\nHost: localhost\r\n\r\n")); err != nil {
				t.Fatal(err)
			}
			<-entered
			// Verify real net FD Write stack, not merely a mock deadline callback.
			until := time.Now().Add(time.Second)
			blocked := false
			for time.Now().Before(until) {
				buf := make([]byte, 1<<20)
				n := runtime.Stack(buf, true)
				for _, g := range strings.Split(string(buf[:n]), "\n\n") {
					if strings.Contains(g, "saturatedMemoryWriter).Write") && strings.Contains(g, "internal/poll.(*FD).Write") {
						blocked = true
					}
				}
				if blocked {
					break
				}
				time.Sleep(time.Millisecond)
			}
			if !blocked {
				conn.Close()
				t.Fatal("TCP send did not saturate")
			}
			if owner == "request" {
				stopRequest()
			} else {
				stopCore()
			}
			select {
			case <-done:
			case <-time.After(100 * time.Millisecond):
				conn.Close()
				<-done
				t.Fatal("real saturated socket ignored " + owner + " cancellation")
			}
		})
	}
}

func TestMemoryRealWebSocketPeerOnlyDisconnect(t *testing.T) {
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { defer close(done); memory(context.Background())(w, r) }))
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
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil || response.StatusCode != 101 {
		t.Fatalf("upgrade: %v %v", response, err)
	}
	conn.Close()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("peer-only close left hijacked handler/ticker alive")
	}
}

func TestMemoryWebSocketRetirementJoins(t *testing.T) {
	for _, owner := range []string{"request", "core", "peer-close-frame", "peer-eof"} {
		t.Run(owner, func(t *testing.T) {
			for range 12 {
				core, stopCore := context.WithCancel(context.Background())
				req, stopRequest := context.WithCancel(context.Background())
				done := make(chan struct{})
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { defer close(done); memory(core)(w, r.WithContext(req)) }))
				conn, err := net.Dial("tcp", server.Listener.Addr().String())
				if err != nil {
					t.Fatal(err)
				}
				_, err = conn.Write([]byte("GET /memory HTTP/1.1\r\nHost: localhost\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n"))
				if err != nil {
					t.Fatal(err)
				}
				response, err := http.ReadResponse(bufio.NewReader(conn), nil)
				if err != nil || response.StatusCode != 101 {
					t.Fatalf("upgrade: %v %v", response, err)
				}
				switch owner {
				case "request":
					stopRequest()
				case "core":
					stopCore()
				case "peer-close-frame":
					_, err = conn.Write([]byte{0x88, 0x80, 0, 0, 0, 0})
					if err != nil {
						t.Fatal(err)
					}
				case "peer-eof":
					conn.Close()
				}
				select {
				case <-done:
				case <-time.After(100 * time.Millisecond):
					conn.Close()
					stopCore()
					stopRequest()
					server.Close()
					t.Fatal("websocket retirement did not join " + owner)
				}
				conn.Close()
				stopCore()
				stopRequest()
				server.Close()
			}
		})
	}
	// Attribute leaked read pumps to this endpoint, not global runtime counts.
	stack := make([]byte, 1<<20)
	n := runtime.Stack(stack, true)
	for _, g := range strings.Split(string(stack[:n]), "\n\n") {
		if strings.Contains(g, "clashapi/api_meta.go") && strings.Contains(g, "ws.ReadHeader") {
			t.Fatalf("memory peer read pump survived handler return:\n%s", g)
		}
	}
}

type joiningDeadlineWriter struct {
	memoryWriter
	entered, callback, allow chan struct{}
}

func (w *joiningDeadlineWriter) Write([]byte) (int, error) {
	close(w.entered)
	<-w.callback
	return 0, net.ErrClosed
}
func (w *joiningDeadlineWriter) SetWriteDeadline(time.Time) error {
	close(w.callback)
	<-w.allow
	return nil
}
func TestMemoryCancellationCallbackJoined(t *testing.T) {
	core, stop := context.WithCancel(context.Background())
	defer stop()
	w := &joiningDeadlineWriter{memoryWriter: memoryWriter{header: make(http.Header)}, entered: make(chan struct{}), callback: make(chan struct{}), allow: make(chan struct{})}
	done := make(chan struct{})
	go func() { memory(core)(w, httptest.NewRequest("GET", "/memory", nil)); close(done) }()
	<-w.entered
	stop()
	<-w.callback
	select {
	case <-done:
		close(w.allow)
		t.Fatal("handler returned before its callback")
	case <-time.After(20 * time.Millisecond):
	}
	close(w.allow)
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("callback join self-deadlocked")
	}
}

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
