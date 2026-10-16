package xhttp

import (
	"context"
	"errors"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/log"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

type dynamicCountDelegate struct {
	adapter.Outbound
	closes atomic.Int32
}

func (d *dynamicCountDelegate) Close() error { d.closes.Add(1); return nil }

func TestDynamicCloseLateSuccessNoPublication(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	delegate := &dynamicCountDelegate{}
	h := &Outbound{}
	h.initializeFn = func(context.Context) error {
		close(started)
		<-release
		h.initMu.Lock()
		h.initAttempt.candidate = &dynamicGeneration{delegate: delegate}
		h.initMu.Unlock()
		return nil
	}
	result := make(chan error, 1)
	go func() { result <- h.lazyInit(context.Background()) }()
	<-started
	closed := make(chan error, 1)
	go func() { closed <- h.Close() }()
	deadline := time.Now().Add(time.Second)
	for {
		h.initMu.Lock()
		done := h.closed
		h.initMu.Unlock()
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Close did not cancel flight")
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	require.ErrorIs(t, <-result, net.ErrClosed)
	require.NoError(t, <-closed)
	require.False(t, h.isInit)
	require.Nil(t, h.generation)
	require.Equal(t, int32(1), delegate.closes.Load())
}

func TestDynamicRefreshRetiresAndCloseExactlyOnce(t *testing.T) {
	old, newDelegate := &dynamicCountDelegate{}, &dynamicCountDelegate{}
	current := &dynamicGeneration{delegate: old}
	candidate := &dynamicGeneration{delegate: newDelegate}
	h := &Outbound{isInit: true, generation: current}
	h.initializeFn = func(context.Context) error {
		h.initMu.Lock()
		h.initAttempt.candidate = candidate
		h.initMu.Unlock()
		return nil
	}
	require.NoError(t, h.Refresh(context.Background()))
	require.Same(t, candidate, h.generation)
	require.Zero(t, old.closes.Load())
	require.Zero(t, newDelegate.closes.Load())
	require.Len(t, h.retired, 1)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { require.NoError(t, h.Close()) })
	}
	wg.Wait()
	require.Equal(t, int32(1), old.closes.Load())
	require.Equal(t, int32(1), newDelegate.closes.Load())
}

func TestDynamicFrameworkScopeOwnsClose(t *testing.T) {
	d := &dynamicCountDelegate{}
	h := &Outbound{Adapter: outbound.NewAdapter("Blackstone", "parent", nil, nil), isInit: true, generation: &dynamicGeneration{delegate: d}}
	scope := adapter.NewScope(context.Background(), log.NewNOPFactory().Logger())
	require.NoError(t, h.Start(adapter.StartStateInitialize, scope))
	require.NoError(t, scope.Close())
	require.Equal(t, int32(1), d.closes.Load())
	require.ErrorIs(t, h.lazyInit(context.Background()), net.ErrClosed)
}

func TestDynamicWorkerFiniteBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		h := &Outbound{initializeFn: func(ctx context.Context) error {
			deadline, ok := ctx.Deadline()
			require.True(t, ok)
			require.Equal(t, controlTotalTimeout, deadline.Sub(start))
			<-ctx.Done()
			return ctx.Err()
		}}
		require.ErrorIs(t, h.lazyInit(context.Background()), context.DeadlineExceeded)
		require.Equal(t, controlTotalTimeout, time.Since(start))
		require.False(t, h.isInit)
		require.NoError(t, h.Close())
	})
}

func TestDynamicShortOwnerDoesNotPoisonLiveWaiter(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	h := &Outbound{initializeFn: func(ctx context.Context) error {
		close(started)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
			return nil
		}
	}}
	owner, cancel := context.WithCancel(context.Background())
	first, second := make(chan error, 1), make(chan error, 1)
	go func() { first <- h.lazyInit(owner) }()
	<-started
	joined := make(chan struct{})
	live := &dynamicObservedContext{Context: context.Background(), observed: joined}
	go func() { second <- h.lazyInit(live) }()
	<-joined
	cancel()
	require.ErrorIs(t, <-first, context.Canceled)
	close(release)
	require.NoError(t, <-second)
	require.NoError(t, h.Close())
}

type dynamicObservedContext struct {
	context.Context
	observed chan struct{}
}

func (c *dynamicObservedContext) Done() <-chan struct{} {
	select {
	case <-c.observed:
	default:
		close(c.observed)
	}
	return c.Context.Done()
}

func TestDynamicCloseIsTerminal(t *testing.T) {
	calls := 0
	h := &Outbound{initializeFn: func(context.Context) error { calls++; return nil }}
	require.NoError(t, h.Close())
	require.ErrorIs(t, h.lazyInit(context.Background()), net.ErrClosed)
	require.Zero(t, calls)
}

func TestDynamicLastCancelClosesWorker(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	h := &Outbound{initializeFn: func(ctx context.Context) error { close(started); <-ctx.Done(); close(stopped); return ctx.Err() }}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- h.lazyInit(ctx) }()
	<-started
	cancel()
	require.True(t, errors.Is(<-result, context.Canceled))
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("real worker not canceled")
	}
	require.NoError(t, h.Close())
}
