package snell

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/stretchr/testify/require"
)

type lifecycleWarmer struct {
	snellClient
	entered chan int
	calls   atomic.Int32
	closed  atomic.Bool
	reset   atomic.Bool
}

func (c *lifecycleWarmer) Warm(ctx context.Context, count int) error {
	c.calls.Add(1)
	c.entered <- count
	<-ctx.Done()
	return ctx.Err()
}
func (c *lifecycleWarmer) Close() error { c.closed.Store(true); return nil }
func (c *lifecycleWarmer) Reset()       { c.reset.Store(true) }

func TestOIXPreconnectOutboundLifecycle(t *testing.T) {
	for _, closeBefore := range []bool{false, true} {
		created, err := NewOutbound(context.Background(), nil, log.NewNOPFactory().NewLogger("snell"), "oix", option.SnellOutboundOptions{
			Version: 4, AbstractSnellOutboundOptions: option.AbstractSnellOutboundOptions{ServerOptions: option.ServerOptions{Server: "127.0.0.1", ServerPort: 443}, PSK: "password"},
			ObfsOptions: option.SnellObfsClientOptions{ObfsMode: "oix-ech-tls", OIXECH: true, OIXIdentityVersion: 2, OIXALPN: "snell-ech/1", OIXSNI: "front.example", OIXConfig: "AQID", OIXPreconnect: 2},
		})
		require.NoError(t, err)
		h := created.(*Outbound)
		require.True(t, h.reuse)
		require.Equal(t, 2, h.preconnect)
		require.NoError(t, h.client.Close())
		fake := &lifecycleWarmer{entered: make(chan int, 1)}
		h.client = fake
		require.NoError(t, h.Start(adapter.StartStateInitialize))
		require.Zero(t, fake.calls.Load())
		if closeBefore {
			require.NoError(t, h.Close())
			require.ErrorIs(t, h.Start(adapter.StartStateStarted), net.ErrClosed)
			require.Zero(t, fake.calls.Load())
			continue
		}
		require.NoError(t, h.Start(adapter.StartStateStarted))
		require.NoError(t, h.Start(adapter.StartStateStarted))
		select {
		case count := <-fake.entered:
			require.Equal(t, 2, count)
		case <-time.After(time.Second):
			t.Fatal("warm not started")
		}
		finished := make(chan struct{})
		go func() { h.InterfaceUpdated(context.Background()); close(finished) }()
		select {
		case <-finished:
		case <-time.After(time.Second):
			t.Fatal("reset did not join warm")
		}
		require.True(t, fake.reset.Load())
		require.NoError(t, h.Close())
		require.True(t, fake.closed.Load())
		require.EqualValues(t, 1, fake.calls.Load())
	}
}

func TestStandardSnellDoesNotPreconnect(t *testing.T) {
	fake := &lifecycleWarmer{entered: make(chan int, 1)}
	h := &Outbound{client: fake}
	require.NoError(t, h.Start(adapter.StartStateStarted))
	require.Zero(t, fake.calls.Load())
	require.NoError(t, h.Close())
}
