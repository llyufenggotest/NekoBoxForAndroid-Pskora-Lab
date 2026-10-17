package xhttp

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestControlOwnedConnectAndAggregateBudgets(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		var timesMu sync.Mutex
		var times []time.Duration
		dialer := controlFixtureDialer{dial: func(ctx context.Context, _ string) (net.Conn, error) {
			deadline, ok := ctx.Deadline()
			require.True(t, ok)
			timesMu.Lock()
			times = append(times, deadline.Sub(start))
			timesMu.Unlock()
			<-ctx.Done()
			return nil, ctx.Err()
		}}
		_, err := FetchDynamicConfig(context.Background(), dialer, "ignored", "id", "token", "device", "tablet")
		require.ErrorIs(t, err, context.DeadlineExceeded)
		timesMu.Lock()
		gotTimes := append([]time.Duration(nil), times...)
		timesMu.Unlock()
		require.Equal(t, []time.Duration{4 * time.Second, 15 * time.Second}, gotTimes)
		require.Equal(t, 15*time.Second, time.Since(start))
	})
}

func TestControlCallerBudgetDoesNotAuthorizeFallback(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		dialer := controlFixtureDialer{dial: func(ctx context.Context, _ string) (net.Conn, error) {
			calls.Add(1)
			<-ctx.Done()
			return nil, ctx.Err()
		}}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, err := FetchDynamicConfig(ctx, dialer, "ignored", "id", "token", "device", "tablet")
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Equal(t, int32(1), calls.Load())
	})
}

func TestControlNilPreflightCannotFetchIdentity(t *testing.T) {
	calls := 0
	dialer := controlFixtureDialer{dial: func(context.Context, string) (net.Conn, error) { calls++; return nil, errors.New("must not dial") }}
	configs, err := FetchDynamicConfigWithPreflight(context.Background(), dialer, "ignored", "id", "token", "device", "tablet", nil)
	require.Error(t, err)
	require.Nil(t, configs)
	require.Zero(t, calls)
}

func TestControlProductionStrictTLSAndH2Defaults(t *testing.T) {
	client := newControlClient(controlFixtureDialer{})
	for _, host := range []string{controlCDNHost, controlLegacyHost} {
		owned := controlOriginClient(client, context.Background(), host, &blackstoneOriginAttempt{})
		tr := owned.Transport.(*http.Transport)
		require.False(t, tr.TLSClientConfig.InsecureSkipVerify)
		require.Equal(t, host, tr.TLSClientConfig.ServerName)
		require.True(t, tr.ForceAttemptHTTP2)
		require.NotNil(t, owned.CheckRedirect)
	}
}
