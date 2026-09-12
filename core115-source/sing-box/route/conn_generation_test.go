package route

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sagernet/sing-box/log"
)

type closeProbe struct {
	net.Conn
	abortCalls atomic.Int32
	closeCalls atomic.Int32
	abortErr   error
}

func (c *closeProbe) AbortiveClose() error {
	c.abortCalls.Add(1)
	_ = c.Conn.Close()
	return c.abortErr
}
func (c *closeProbe) Close() error {
	c.closeCalls.Add(1)
	return c.Conn.Close()
}

type fallbackCloseProbe struct {
	net.Conn
	closeCalls atomic.Int32
}

func (c *fallbackCloseProbe) Close() error {
	c.closeCalls.Add(1)
	return c.Conn.Close()
}

func newTestManager() *ConnectionManager {
	return NewConnectionManager(log.NewNOPFactory().NewLogger("connection"))
}

func TestTrackTUNTCPCapturesCurrentGeneration(t *testing.T) {
	manager := newTestManager()
	if got := manager.AdvanceNetworkGeneration(); got != 1 {
		t.Fatalf("first generation=%d", got)
	}
	core, peer := net.Pipe()
	defer peer.Close()
	tracked := manager.TrackTUNTCP(&closeProbe{Conn: core})
	defer tracked.Close()
	if got := tracked.(*generationTUNTCP).generation; got != 1 {
		t.Fatalf("captured generation=%d want=1", got)
	}
}

func TestCloseOldTUNTCPSafeModesAndIdempotency(t *testing.T) {
	manager := newTestManager()
	oldAbortCore, oldAbortPeer := net.Pipe()
	oldFallbackCore, oldFallbackPeer := net.Pipe()
	defer oldAbortPeer.Close()
	defer oldFallbackPeer.Close()
	abort := &closeProbe{Conn: oldAbortCore}
	fallback := &fallbackCloseProbe{Conn: oldFallbackCore}
	manager.TrackTUNTCP(abort)
	manager.TrackTUNTCP(fallback)
	manager.AdvanceNetworkGeneration()

	got := manager.CloseOldTUNTCP(1)
	if got.Closed != 2 || got.Abort != 1 || got.Fallback != 1 || got.Action != TUNTCPRecoveryActionFallback || got.Result != TUNTCPRecoveryResultSuccess || got.Active != 0 {
		t.Fatalf("unexpected result: %+v", got)
	}
	if abort.abortCalls.Load() != 1 || abort.closeCalls.Load() != 0 || fallback.closeCalls.Load() != 1 {
		t.Fatalf("abort=%d normal-on-abort=%d fallback=%d", abort.abortCalls.Load(), abort.closeCalls.Load(), fallback.closeCalls.Load())
	}
	if duplicate := manager.CloseOldTUNTCP(1); duplicate.Closed != 0 || abort.abortCalls.Load() != 1 || fallback.closeCalls.Load() != 1 {
		t.Fatalf("duplicate result=%+v abort=%d fallback=%d", duplicate, abort.abortCalls.Load(), fallback.closeCalls.Load())
	}
}

func TestNormalCloseNeverUsesAbort(t *testing.T) {
	manager := newTestManager()
	core, peer := net.Pipe()
	defer peer.Close()
	probe := &closeProbe{Conn: core}
	tracked := manager.TrackTUNTCP(probe)
	if err := tracked.Close(); err != nil {
		t.Fatal(err)
	}
	if probe.abortCalls.Load() != 0 || probe.closeCalls.Load() != 1 {
		t.Fatalf("abort=%d close=%d", probe.abortCalls.Load(), probe.closeCalls.Load())
	}
}

func TestConcurrentCloseAndOldGenerationPurgeIsSingleOwner(t *testing.T) {
	for round := 0; round < 100; round++ {
		manager := newTestManager()
		core, peer := net.Pipe()
		probe := &closeProbe{Conn: core}
		tracked := manager.TrackTUNTCP(probe)
		manager.AdvanceNetworkGeneration()
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); <-start; _ = tracked.Close() }()
		go func() { defer wg.Done(); <-start; _ = manager.CloseOldTUNTCP(1) }()
		close(start)
		wg.Wait()
		_ = peer.Close()
		if total := probe.abortCalls.Load() + probe.closeCalls.Load(); total < 1 || total > 2 {
			t.Fatalf("round %d close calls=%d", round, total)
		}
		if probe.abortCalls.Load() > 1 {
			t.Fatalf("round %d duplicate abort", round)
		}
	}
}

func TestCloseOldTUNTCPOnlyClosesPreviousGenerationAndLeavesUDP(t *testing.T) {
	manager := newTestManager()
	manager.AdvanceNetworkGeneration()
	oldApp, oldCore := net.Pipe()
	oldProbe := &closeProbe{Conn: oldCore}
	oldTracked := manager.TrackTUNTCP(oldProbe)
	defer oldApp.Close()
	defer oldTracked.Close()

	manager.AdvanceNetworkGeneration()
	newApp, newCore := net.Pipe()
	newProbe := &closeProbe{Conn: newCore}
	newTracked := manager.TrackTUNTCP(newProbe)
	defer newApp.Close()
	defer newTracked.Close()
	udpApp, udpCore := net.Pipe()
	udpProbe := &packetConnForGenerationTest{Conn: udpCore}
	udpTracked := manager.TrackPacketConn(udpProbe)
	defer udpApp.Close()
	defer udpTracked.Close()

	got := manager.CloseOldTUNTCP(2)
	if got.Closed != 1 || got.Abort != 1 || got.Fallback != 0 || got.Action != TUNTCPRecoveryActionAbort {
		t.Fatalf("result=%+v", got)
	}
	if newProbe.abortCalls.Load() != 0 || udpProbe.closeCalls.Load() != 0 {
		t.Fatalf("new abort=%d udp close=%d", newProbe.abortCalls.Load(), udpProbe.closeCalls.Load())
	}
	assertPipeAlive(t, newApp, newTracked)
	assertPacketPipeAlive(t, udpApp, udpTracked)
}

func TestThreeLifecycleRoundsAndTwentyRealHTTPReconnects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "generation-ok") }))
	defer server.Close()
	manager := newTestManager()
	for round := 1; round <= 20; round++ {
		old, err := net.Dial("tcp", server.Listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		oldProbe := &closeProbe{Conn: old}
		manager.TrackTUNTCP(oldProbe)
		generation := manager.AdvanceNetworkGeneration()
		fresh, err := net.Dial("tcp", server.Listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		freshProbe := &closeProbe{Conn: fresh}
		freshTracked := manager.TrackTUNTCP(freshProbe)
		got := manager.CloseOldTUNTCP(generation)
		if got.Closed != 1 || got.Abort != 1 || oldProbe.abortCalls.Load() != 1 || freshProbe.abortCalls.Load() != 0 {
			t.Fatalf("round %d result=%+v oldAbort=%d freshAbort=%d", round, got, oldProbe.abortCalls.Load(), freshProbe.abortCalls.Load())
		}
		if _, err = fmt.Fprintf(freshTracked, "GET / HTTP/1.1\r\nHost: test\r\nConnection: close\r\n\r\n"); err != nil {
			t.Fatal(err)
		}
		response, err := http.ReadResponse(bufio.NewReader(freshTracked), nil)
		if err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		_ = freshTracked.Close()
		if err != nil || string(body) != "generation-ok" {
			t.Fatalf("round %d body=%q err=%v", round, body, err)
		}
	}
}

func assertPipeAlive(t *testing.T, app net.Conn, core net.Conn) {
	t.Helper()
	go func() { _, _ = core.Write([]byte{7}) }()
	_ = app.SetReadDeadline(time.Now().Add(time.Second))
	b := []byte{0}
	if _, err := app.Read(b); err != nil || b[0] != 7 {
		t.Fatalf("new TCP affected: byte=%v err=%v", b, err)
	}
}

func assertPacketPipeAlive(t *testing.T, app net.Conn, core net.PacketConn) {
	t.Helper()
	go func() { _, _ = core.WriteTo([]byte{9}, nil) }()
	_ = app.SetReadDeadline(time.Now().Add(time.Second))
	b := []byte{0}
	if _, err := app.Read(b); err != nil || b[0] != 9 {
		t.Fatalf("UDP affected: byte=%v err=%v", b, err)
	}
}

type packetConnForGenerationTest struct {
	net.Conn
	closeCalls atomic.Int32
}

func (c *packetConnForGenerationTest) ReadFrom(p []byte) (int, net.Addr, error) {
	n, err := c.Read(p)
	return n, c.RemoteAddr(), err
}
func (c *packetConnForGenerationTest) WriteTo(p []byte, _ net.Addr) (int, error) { return c.Write(p) }
func (c *packetConnForGenerationTest) Close() error                              { c.closeCalls.Add(1); return c.Conn.Close() }
