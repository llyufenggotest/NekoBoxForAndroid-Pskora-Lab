package group

import (
	"context"
	"sync"
	"testing"
	"time"
)

type fakeURLTestTicker struct {
	ch      chan time.Time
	stopped bool
}

func (t *fakeURLTestTicker) Chan() <-chan time.Time { return t.ch }
func (t *fakeURLTestTicker) Stop()                  { t.stopped = true }

type fakeURLTestClock struct {
	mu      sync.Mutex
	now     time.Time
	tickers []*fakeURLTestTicker
	periods []time.Duration
}

func (c *fakeURLTestClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeURLTestClock) NewTicker(period time.Duration) urlTestTicker {
	c.mu.Lock()
	defer c.mu.Unlock()
	ticker := &fakeURLTestTicker{ch: make(chan time.Time, 8)}
	c.tickers = append(c.tickers, ticker)
	c.periods = append(c.periods, period)
	return ticker
}

func (c *fakeURLTestClock) Tick(index int, advance time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(advance)
	now := c.now
	ticker := c.tickers[index]
	c.mu.Unlock()
	ticker.ch <- now
}

func waitProbeCount(t *testing.T, probes <-chan time.Time, want int) []time.Time {
	t.Helper()
	var got []time.Time
	deadline := time.After(time.Second)
	for len(got) < want {
		select {
		case at := <-probes:
			got = append(got, at)
		case <-deadline:
			t.Fatalf("probe count=%d want=%d", len(got), want)
		}
	}
	return got
}

func TestURLTestConnectedPeriodRunsWithoutTouchAndStops(t *testing.T) {
	clock := &fakeURLTestClock{now: time.Unix(1000, 0)}
	probes := make(chan time.Time, 8)
	group := &URLTestGroup{
		ctx: context.Background(), interval: 300 * time.Second, close: make(chan struct{}),
		now: clock.Now, newTicker: clock.NewTicker,
		periodicCheck: func(context.Context) { probes <- clock.Now() },
	}

	group.PostStart() // Initial batch is retained and starts the periodic owner immediately.
	first := waitProbeCount(t, probes, 1)
	if len(clock.periods) != 1 || clock.periods[0] != 300*time.Second {
		t.Fatalf("periods=%v", clock.periods)
	}
	clock.Tick(0, 300*time.Second)
	second := waitProbeCount(t, probes, 1)
	clock.Tick(0, 300*time.Second)
	third := waitProbeCount(t, probes, 1)
	all := append(append(first, second...), third...)
	if all[1].Sub(all[0]) != 300*time.Second || all[2].Sub(all[1]) != 300*time.Second {
		t.Fatalf("probe times=%v", all)
	}

	if err := group.Close(); err != nil { t.Fatal(err) }
	if !clock.tickers[0].stopped { t.Fatal("ticker not stopped") }
	clock.Tick(0, 300*time.Second)
	select {
	case at := <-probes:
		t.Fatalf("probe after disconnect/close at %v", at)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestURLTestIntervalUpdateReplacesSchedule(t *testing.T) {
	clock := &fakeURLTestClock{now: time.Unix(2000, 0)}
	probes := make(chan time.Time, 8)
	group := &URLTestGroup{
		ctx: context.Background(), interval: 300 * time.Second, close: make(chan struct{}),
		now: clock.Now, newTicker: clock.NewTicker,
		periodicCheck: func(context.Context) { probes <- clock.Now() },
	}
	group.PostStart()
	waitProbeCount(t, probes, 1)
	if err := group.SetInterval(60 * time.Second); err != nil { t.Fatal(err) }
	if !clock.tickers[0].stopped { t.Fatal("old ticker still active") }
	if got := clock.periods; len(got) != 2 || got[1] != 60*time.Second { t.Fatalf("periods=%v", got) }
	clock.Tick(1, 60*time.Second)
	waitProbeCount(t, probes, 1)
	group.Close()
}
