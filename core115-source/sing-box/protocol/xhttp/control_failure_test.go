package xhttp

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestControlPostAdmissionFailuresNeverFallback(t *testing.T) {
	for name, fixture := range map[string]struct {
		status   int
		body     string
		redirect bool
	}{
		"http401":     {401, "fixture private response", false},
		"http500":     {500, "fixture private response", false},
		"redirect":    {307, "", true},
		"malformed":   {200, "fixture malformed response", false},
		"business401": {200, string(encryptedControlResponse(t, []byte(`{"code":401,"message":"fixture-private-business"}`))), false},
		"decryptEOF":  {200, `{"data":"AA=="}`, false},
	} {
		t.Run(name, func(t *testing.T) {
			cert, pool := controlFixtureCertificate(t, controlCDNHost)
			var calls, posts atomic.Int32
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				posts.Add(1)
				_, _ = io.Copy(io.Discard, r.Body)
				if fixture.redirect {
					w.Header().Set("Location", "https://untrusted.invalid/private")
				}
				w.WriteHeader(fixture.status)
				_, _ = io.WriteString(w, fixture.body)
			}))
			server.EnableHTTP2 = true
			server.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
			server.StartTLS()
			defer server.Close()
			dialer := controlFixtureDialer{dial: func(ctx context.Context, _ string) (net.Conn, error) {
				calls.Add(1)
				return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
			}}
			client := newControlClient(dialer)
			client.Transport.(*http.Transport).TLSClientConfig.RootCAs = pool
			cfg, err := fetchDynamicConfig(context.Background(), client, "ignored", "id", "token", "device", "tablet")
			require.Error(t, err)
			require.Nil(t, cfg)
			require.Equal(t, int32(1), calls.Load())
			require.Equal(t, int32(1), posts.Load())
			require.NotContains(t, err.Error(), "fixture-private")
		})
	}
}

func TestControlReadyConnectionTimeoutNeverFallback(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		calls := 0
		serverConn := make(chan net.Conn, 1)
		dialer := controlFixtureDialer{dial: func(ctx context.Context, _ string) (net.Conn, error) {
			calls++
			left, right := net.Pipe()
			serverConn <- right
			return left, nil
		}}
		client := newControlClient(dialer)
		// Keep TLS peer open without replying: socket admission permanently revokes retry.
		done := make(chan struct{})
		go func() { right := <-serverConn; _, _ = io.Copy(io.Discard, right); _ = right.Close(); close(done) }()
		_, err := fetchDynamicConfig(context.Background(), client, "ignored", "id", "token", "device", "tablet")
		require.Error(t, err)
		require.Equal(t, 1, calls)
		require.LessOrEqual(t, time.Since(start), 15*time.Second)
		require.True(t, errors.Is(err, context.DeadlineExceeded) || time.Since(start) == 10*time.Second)
		<-done
	})
}
