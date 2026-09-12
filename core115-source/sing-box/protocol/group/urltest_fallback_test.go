package group

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	O "github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/interrupt"
	"github.com/sagernet/sing-box/common/urltest"
	"github.com/sagernet/sing-box/log"
	M "github.com/sagernet/sing/common/metadata"
)

type fallbackTestOutbound struct {
	O.Adapter
	dial  func(context.Context) (net.Conn, error)
	calls int
}

func (o *fallbackTestOutbound) DialContext(ctx context.Context, _ string, _ M.Socksaddr) (net.Conn, error) {
	o.calls++
	return o.dial(ctx)
}
func (o *fallbackTestOutbound) ListenPacket(context.Context, M.Socksaddr) (net.PacketConn, error) {
	return nil, errors.New("udp")
}
func fixture(t *testing.T, count int) (*URLTest, []*fallbackTestOutbound) {
	t.Helper()
	s := &URLTest{logger: log.NewNOPFactory().Logger()}
	s.group = &URLTestGroup{history: urltest.NewHistoryStorage(), interruptGroup: interrupt.NewGroup()}
	var all []*fallbackTestOutbound
	for i := 0; i < count; i++ {
		o := &fallbackTestOutbound{Adapter: O.NewAdapter("test", string(rune('a'+i)), []string{"tcp", "udp"}, nil), dial: func(context.Context) (net.Conn, error) { return nil, errors.New("dial failed") }}
		all = append(all, o)
		s.group.outbounds = append(s.group.outbounds, adapter.Outbound(o))
	}
	s.group.selectedOutboundTCP = all[0]
	s.group.selectedOutboundUDP = all[0]
	return s, all
}
func TestDialFallbackHealthy(t *testing.T) {
	s, all := fixture(t, 2)
	enableFallback(s)
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	all[1].dial = func(context.Context) (net.Conn, error) { return a, nil }
	c, e := s.DialContext(context.Background(), "tcp", M.Socksaddr{})
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	b.SetDeadline(time.Now().Add(time.Second))
	go b.Write([]byte("x"))
	buf := make([]byte, 1)
	if _, e = c.Read(buf); e != nil || buf[0] != 'x' {
		t.Fatalf("connection not usable: %v", e)
	}
	if all[0].calls != 1 || all[1].calls != 1 {
		t.Fatal("wrong attempts")
	}
}
func TestDialFallbackBounds(t *testing.T) {
	s, all := fixture(t, 8)
	enableFallback(s)
	_, e := s.DialContext(context.Background(), "tcp", M.Socksaddr{})
	if e == nil {
		t.Fatal("expected failure")
	}
	total := 0
	for _, o := range all {
		total += o.calls
	}
	if total != 3 {
		t.Fatalf("attempts=%d want 3", total)
	}
}
func TestDialFallbackCancel(t *testing.T) {
	s, all := fixture(t, 2)
	enableFallback(s)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, e := s.DialContext(ctx, "tcp", M.Socksaddr{})
	if !errors.Is(e, context.Canceled) || all[0].calls != 0 {
		t.Fatalf("err=%v calls=%d", e, all[0].calls)
	}
}
func TestDialFallbackIsolation(t *testing.T) {
	for _, network := range []string{"tcp", "udp"} {
		s, all := fixture(t, 2)
		if network == "udp" {
			enableFallback(s)
		}
		s.DialContext(context.Background(), network, M.Socksaddr{})
		if all[0].calls != 1 || all[1].calls != 0 {
			t.Fatal("standard or UDP changed")
		}
	}
}

// Explicitly enable fallback; standard URLTest remains unchanged.
func enableFallback(s *URLTest) { s.dialFallback = true; s.group.fallback = &fallbackState{} }

func TestDialFallbackTotalBudget(t *testing.T) {
	s, all := fixture(t, 4)
	enableFallback(s)
	for _, o := range all {
		o.dial = func(ctx context.Context) (net.Conn, error) { <-ctx.Done(); return nil, ctx.Err() }
	}
	start := time.Now()
	_, err := s.DialContext(context.Background(), "tcp", M.Socksaddr{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if elapsed < 4*time.Second || elapsed > 6*time.Second {
		t.Fatalf("budget %v", elapsed)
	}
	if all[0].calls != 1 || all[1].calls != 1 || all[2].calls != 1 || all[3].calls != 0 {
		t.Fatal("attempt budget")
	}
}
func TestDialFallbackInFlightCancel(t *testing.T) {
	s, all := fixture(t, 2)
	enableFallback(s)
	ctx, cancel := context.WithCancel(context.Background())
	all[0].dial = func(ctx context.Context) (net.Conn, error) { cancel(); <-ctx.Done(); return nil, ctx.Err() }
	_, err := s.DialContext(ctx, "tcp", M.Socksaddr{})
	if !errors.Is(err, context.Canceled) || all[1].calls != 0 {
		t.Fatal(err)
	}
}
func TestDialFallbackStableSelection(t *testing.T) {
	s, all := fixture(t, 2)
	enableFallback(s)
	all[1].dial = func(context.Context) (net.Conn, error) { a, b := net.Pipe(); b.Close(); return a, nil }
	for i := 0; i < 2; i++ {
		c, err := s.DialContext(context.Background(), "tcp", M.Socksaddr{})
		if err != nil {
			t.Fatal(err)
		}
		c.Close()
	}
	if all[0].calls != 1 {
		t.Fatalf("failed node retried %d times", all[0].calls)
	}
	if s.Now() != "b" {
		t.Fatalf("snapshot=%q want b", s.Now())
	}
}

func TestFallbackRankingModesAndHealth(t *testing.T) {
	s, all := fixture(t, 6)
	enableFallback(s)
	for _, i := range []int{4, 5} {
		s.group.history.StoreURLTestHistory(all[i].Tag(), &adapter.URLTestHistory{Time: time.Now(), Delay: uint16(100 - i*10)})
	}
	f := s.group.fallback
	f.mu.Lock()
	c := s.group.candidatesLocked()
	f.mu.Unlock()
	if c[0] != all[4] || c[1] != all[5] {
		t.Fatal("healthy candidates not ahead of unknowns")
	}
	f.mode = "latency"
	f.mu.Lock()
	c = s.group.candidatesLocked()
	f.mu.Unlock()
	if c[0] != all[5] {
		t.Fatal("latency mode did not choose fastest")
	}
	f.mode = "stable"
	s.group.recordFallback(all[5], s.group.beginFallbackObservation(), true, nil)
	s.group.performUpdateCheck()
	if s.Now() != "f" {
		t.Fatal("monitor preempted stable selection")
	}
	s.group.recordFallback(all[5], s.group.beginFallbackObservation(), false, nil)
	s.group.performUpdateCheck()
	if s.Now() != "e" {
		t.Fatal("monitor did not replace failed selection")
	}
}

func TestFallbackCooldownAndStaleProbe(t *testing.T) {
	s, all := fixture(t, 1)
	enableFallback(s)
	old := s.group.beginFallbackObservation()
	s.DialContext(context.Background(), "tcp", M.Socksaddr{})
	s.group.recordFallback(all[0], old, true, &adapter.URLTestHistory{Time: time.Now(), Delay: 1})
	s.DialContext(context.Background(), "tcp", M.Socksaddr{})
	if all[0].calls != 1 {
		t.Fatal("cooldown/stale probe resurrected failure")
	}
	f := s.group.fallback
	f.mu.Lock()
	state := f.nodes["a"]
	state.retryAfter = time.Now().Add(-time.Second)
	f.nodes["a"] = state
	f.mu.Unlock()
	s.DialContext(context.Background(), "tcp", M.Socksaddr{})
	if all[0].calls != 2 {
		t.Fatal("cooldown never expired")
	}
	s.group.recordFallback(all[0], s.group.beginFallbackObservation(), true, &adapter.URLTestHistory{Time: time.Now(), Delay: 1})
	s.group.performUpdateCheck()
	if s.Now() != "a" {
		t.Fatal("fresh healthy probe did not recover")
	}
}

func TestFallbackCancellationPreservesHealth(t *testing.T) {
	s, all := fixture(t, 1)
	enableFallback(s)
	s.group.recordFallback(all[0], s.group.beginFallbackObservation(), true, &adapter.URLTestHistory{Time: time.Now(), Delay: 1})
	s.group.refreshFallback()
	ctx, cancel := context.WithCancel(context.Background())
	all[0].dial = func(context.Context) (net.Conn, error) { cancel(); return nil, context.Canceled }
	s.DialContext(ctx, "tcp", M.Socksaddr{})
	if s.Now() != "a" || s.group.history.LoadURLTestHistory("a") == nil {
		t.Fatal("cancellation poisoned health")
	}
}

func TestFallbackConcurrentSnapshotAndProbe(t *testing.T) {
	s, all := fixture(t, 2)
	enableFallback(s)
	var wg sync.WaitGroup
	var invalid atomic.Bool
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				o := all[i%2]
				s.group.recordFallback(o, s.group.beginFallbackObservation(), j%3 != 0, &adapter.URLTestHistory{Time: time.Now(), Delay: 1})
				s.group.performUpdateCheck()
				tag := s.Now()
				if tag != "" && tag != "a" && tag != "b" {
					invalid.Store(true)
				}
				s.group.fallback.mu.Lock()
				s.group.candidatesLocked()
				s.group.fallback.mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if invalid.Load() {
		t.Fatal("invalid snapshot")
	}
}

func TestDialFallbackEstablishedSurvives(t *testing.T) {
	s, all := fixture(t, 2)
	enableFallback(s)
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	all[0].dial = func(context.Context) (net.Conn, error) { return a, nil }
	c, err := s.DialContext(context.Background(), "tcp", M.Socksaddr{})
	if err != nil {
		t.Fatal(err)
	}
	// A later dial failure and a global URLTest reselection cannot close c.
	all[0].dial = func(context.Context) (net.Conn, error) { return nil, errors.New("later fail") }
	s.DialContext(context.Background(), "tcp", M.Socksaddr{})
	s.group.interruptGroup.Interrupt(true)
	c.SetDeadline(time.Now().Add(time.Second))
	go b.Write([]byte("z"))
	buf := make([]byte, 1)
	if _, err = c.Read(buf); err != nil {
		t.Fatal(err)
	}
	// A read failure after establishment does not dial a spare or replay payload.
	before := all[1].calls
	b.Close()
	c.Read(buf)
	if all[1].calls != before {
		t.Fatal("read triggered retry")
	}
}
