package group

import (
	"context"
	"errors"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/dialer"
	"github.com/sagernet/sing/common"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

const fallbackMaxAttempts = 3
const fallbackTotalTimeout = 5 * time.Second
const fallbackAttemptTimeout = 2 * time.Second
const fallbackCooldown = 30 * time.Second

// All mutable fallback selection/health state is guarded together. Legacy URLTest
// selections are not read or written by the TCP fallback path.
type fallbackState struct {
	mu             sync.Mutex
	mode           string
	selected       adapter.Outbound
	lastTCPSuccess string // actual completed dial; never populated by probes
	nodes          map[string]fallbackHealth
	sequence       uint64
}
type fallbackHealth struct {
	observed   uint64
	retryAfter time.Time
	healthy    bool
}

func (g *URLTestGroup) beginFallbackObservation() uint64 {
	f := g.fallback
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sequence++
	return f.sequence
}

// recordFallback ignores results started before a newer observation completed.
// In particular, an in-flight old probe cannot resurrect a recently failed node.
func (g *URLTestGroup) recordFallback(o adapter.Outbound, started uint64, ok bool, history *adapter.URLTestHistory) {
	f := g.fallback
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.nodes == nil {
		f.nodes = make(map[string]fallbackHealth)
	}
	old := f.nodes[o.Tag()]
	if started <= old.observed {
		return
	}
	state := fallbackHealth{observed: started, healthy: ok}
	if !ok {
		state.retryAfter = time.Now().Add(fallbackCooldown)
		g.history.DeleteURLTestHistory(o.Tag())
		if f.selected != nil && f.selected.Tag() == o.Tag() {
			f.selected = nil
		}
	} else {
		if history != nil {
			g.history.StoreURLTestHistory(o.Tag(), history)
		}
		// Probe successes must not preempt a stable healthy selection.
		if history == nil && (f.selected == nil || f.mode == "latency") {
			f.selected = o
		}
	}
	f.nodes[o.Tag()] = state
}

// candidatesLocked ranks every measured/observed healthy node before unknowns,
// rather than taking the first three configuration entries. Cooldown excludes
// failed nodes even when other groups share and repopulate the history storage.
func (g *URLTestGroup) candidatesLocked() []adapter.Outbound {
	f := g.fallback
	type candidate struct {
		o        adapter.Outbound
		healthy  bool
		delay    uint16
		measured bool
	}
	var ranked []candidate
	seen := make(map[string]bool)
	for _, o := range g.outbounds {
		if seen[o.Tag()] || !common.Contains(o.Network(), N.NetworkTCP) {
			continue
		}
		seen[o.Tag()] = true
		state := f.nodes[o.Tag()]
		if time.Now().Before(state.retryAfter) {
			continue
		}
		h := g.history.LoadURLTestHistory(o.Tag())
		c := candidate{o: o, healthy: state.healthy}
		// A failed node becomes unknown after cooldown, not healthy from stale history.
		if h != nil && (state.observed == 0 || state.healthy) {
			c.healthy = true
			c.delay = h.Delay
			c.measured = true
		}
		ranked = append(ranked, c)
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		a, b := ranked[i], ranked[j]
		if f.mode != "latency" && f.selected != nil {
			if a.healthy && a.o.Tag() == f.selected.Tag() {
				return true
			}
			if b.healthy && b.o.Tag() == f.selected.Tag() {
				return false
			}
		}
		if a.healthy != b.healthy {
			return a.healthy
		}
		if f.mode == "latency" && a.healthy {
			if a.measured != b.measured {
				return a.measured
			}
			if a.measured && a.delay != b.delay {
				return a.delay < b.delay
			}
		}
		return false
	})
	result := make([]adapter.Outbound, 0, len(ranked))
	for _, c := range ranked {
		result = append(result, c.o)
	}
	return result
}

func (g *URLTestGroup) refreshFallback() {
	f := g.fallback
	f.mu.Lock()
	defer f.mu.Unlock()
	candidates := g.candidatesLocked()
	f.selected = nil
	for _, o := range candidates {
		state := f.nodes[o.Tag()]
		if state.healthy || (state.observed == 0 && g.history.LoadURLTestHistory(o.Tag()) != nil) {
			f.selected = o
			break
		}
	}
}
func (g *URLTestGroup) fallbackNow() string {
	f := g.fallback
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.selected != nil {
		return f.selected.Tag()
	}
	return ""
}

// Only DialContext failures are retried. No application bytes are buffered or
// replayed. Lazy handshake/read/write failures after return cannot fail over.
// Leaf dialers must honor cancellation; UDP has no retry and no dial goroutines
// are created to simulate timeouts.
func (s *URLTest) dialWithFallback(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, fallbackTotalTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f := s.group.fallback
	f.mu.Lock()
	candidates := s.group.candidatesLocked()
	f.mu.Unlock()
	var failures []error
	attempts := 0
	for _, o := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if attempts == fallbackMaxAttempts {
			break
		}
		// Recheck snapshots: another dial/probe may have failed this node meanwhile.
		f.mu.Lock()
		cooling := time.Now().Before(f.nodes[o.Tag()].retryAfter)
		f.mu.Unlock()
		if cooling {
			continue
		}
		attempts++
		started := s.group.beginFallbackObservation()
		attempt, stop := context.WithTimeout(ctx, fallbackAttemptTimeout)
		conn, err := o.DialContext(attempt, network, destination)
		attemptErr := attempt.Err()
		stop()
		if err == nil && conn != nil && attemptErr == nil && ctx.Err() == nil {
			s.group.recordFallback(o, started, true, nil)
			f.mu.Lock()
			f.lastTCPSuccess = o.Tag()
			f.mu.Unlock()
			// Never register established fallback connections for reselection interrupts.
			return conn, nil
		}
		if conn != nil {
			conn.Close()
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err == nil {
			err = attemptErr
		}
		if err == nil {
			err = errors.New("dial returned nil connection")
		}
		failures = append(failures, err)
		// A parent cancellation/deadline is not a node failure. A private per-attempt
		// timeout is a failure; an independently canceled leaf dial is not evidence.
		if !errors.Is(err, context.Canceled) && !errors.Is(err, dialer.ErrNoAvailableInterface) {
			s.group.recordFallback(o, started, false, nil)
		}
	}
	if len(failures) == 0 {
		return nil, errors.New("no eligible TCP outbound (missing or cooling down)")
	}
	return nil, errors.Join(failures...)
}
