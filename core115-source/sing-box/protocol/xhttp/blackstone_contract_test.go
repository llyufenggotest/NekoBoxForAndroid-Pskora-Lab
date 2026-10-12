package xhttp

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	M "github.com/sagernet/sing/common/metadata"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/chacha20poly1305"
)

func TestBlackstoneControlHeadersConsumeDevicePair(t *testing.T) {
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.invalid", nil)
	require.NoError(t, err)
	applyControlHeaders(request, "token-fixture", "device-fixture", "tablet-fixture")
	require.Equal(t, "540", request.Header.Get("x-version"))
	raw, err := base64.StdEncoding.DecodeString(request.Header.Get("x-header"))
	require.NoError(t, err)
	for index := range raw {
		raw[index] ^= PUB_KEY[index%len(PUB_KEY)]
	}
	var headers map[string]any
	require.NoError(t, json.Unmarshal(raw, &headers))
	require.Equal(t, "device-fixture", headers["X-IDENTIFIER"])
	require.Equal(t, "tablet-fixture", headers["X-DEVICE-NAME"])
	require.Equal(t, "token-fixture", headers["X-TOKEN"])
}

func TestCanceledControlContextStopsBeforeRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := newControlRequest(ctx, "https://example.invalid", []byte(`{}`), "token-fixture", "device-fixture", "tablet-fixture")
	require.ErrorIs(t, err, context.Canceled)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestFetchDynamicConfigCanceledBeforeFetch(t *testing.T) {
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("unexpected transport call")
	})}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := fetchDynamicConfig(ctx, client, "node-fixture", "node-id-fixture", "token-fixture", "device-fixture", "tablet-fixture")
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, calls.Load())
}

func TestFetchDynamicConfigPreservesInflightCancellation(t *testing.T) {
	started := make(chan struct{})
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		close(started)
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := fetchDynamicConfig(ctx, client, "node-fixture", "node-id-fixture", "token-fixture", "device-fixture", "tablet-fixture")
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("control fetch did not reach transport")
	}
	cancel()
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled)
		for _, secret := range []string{"token-fixture", "device-fixture", "tablet-fixture"} {
			require.NotContains(t, err.Error(), secret)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("inflight control fetch ignored cancellation")
	}
}

func TestFetchDynamicConfigPreservesInflightDeadline(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := fetchDynamicConfig(ctx, client, "node-fixture", "node-id-fixture", "token-fixture", "device-fixture", "tablet-fixture")
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

type blackstoneBlockingDialer struct {
	started chan struct{}
	calls   atomic.Int32
}

func (d *blackstoneBlockingDialer) DialContext(ctx context.Context, _ string, _ M.Socksaddr) (net.Conn, error) {
	d.calls.Add(1)
	select {
	case d.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (d *blackstoneBlockingDialer) ListenPacket(context.Context, M.Socksaddr) (net.PacketConn, error) {
	return nil, errors.New("not implemented")
}

func TestFetchDynamicConfigRealTransportPreservesInflightCancellation(t *testing.T) {
	dialer := &blackstoneBlockingDialer{started: make(chan struct{}, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := FetchDynamicConfig(ctx, dialer, "node-fixture", "node-id-fixture", "token-fixture", "device-fixture", "tablet-fixture")
		result <- err
	}()
	select {
	case <-dialer.started:
	case <-time.After(5 * time.Second):
		t.Fatal("real transport did not begin dialing")
	}
	cancel()
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("real transport ignored cancellation")
	}
}

type blackstoneRaceDialer struct {
	winnerStarted chan struct{}
	loserStarted  chan struct{}
	loserCanceled chan struct{}
}

func (d *blackstoneRaceDialer) DialContext(ctx context.Context, _ string, destination M.Socksaddr) (net.Conn, error) {
	if destination.Port == 1 {
		close(d.winnerStarted)
		<-d.loserStarted
		left, right := net.Pipe()
		right.Close()
		return left, nil
	}
	close(d.loserStarted)
	<-ctx.Done()
	close(d.loserCanceled)
	return nil, ctx.Err()
}

func (d *blackstoneRaceDialer) ListenPacket(context.Context, M.Socksaddr) (net.PacketConn, error) {
	return nil, errors.New("not implemented")
}

func TestPingRaceCancelsLosers(t *testing.T) {
	dialer := &blackstoneRaceDialer{
		winnerStarted: make(chan struct{}),
		loserStarted:  make(chan struct{}),
		loserCanceled: make(chan struct{}),
	}
	outbound := &Outbound{dialer: dialer}
	winner, err := outbound.pingRace(context.Background(), []RealConfig{
		{Server: "127.0.0.1", Port: "1"},
		{Server: "127.0.0.1", Port: "2"},
	})
	require.NoError(t, err)
	require.Equal(t, "1", winner.Port)
	select {
	case <-dialer.loserCanceled:
	case <-time.After(time.Second):
		t.Fatal("race loser did not inherit winner cancellation")
	}
}

func TestLazyInitWaiterInheritsCancellation(t *testing.T) {
	done := make(chan struct{})
	outbound := &Outbound{initDone: done}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- outbound.lazyInit(ctx) }()
	cancel()
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("lazy initialization waiter ignored cancellation")
	}
	outbound.initMu.Lock()
	outbound.initErr = errors.New("initializer fixture failure")
	outbound.isInit = true
	outbound.initDone = nil
	close(done)
	outbound.initMu.Unlock()
}

func encryptedControlResponse(t *testing.T, layer1 []byte) []byte {
	t.Helper()
	raw := append([]byte(nil), layer1...)
	for i := range raw {
		raw[i] ^= PRIV_KEY[i%len(PRIV_KEY)]
	}
	wire, err := json.Marshal(map[string]string{"data": base64.StdEncoding.EncodeToString(raw)})
	require.NoError(t, err)
	return wire
}

func encryptBlackstonePayloadFixture(t *testing.T, plaintext []byte) []byte {
	t.Helper()
	seed := []byte("fixture-seed")
	key := vmessKDF(seed, "CHACHA 20 POLY 1305")
	aead, err := chacha20poly1305.New(key)
	require.NoError(t, err)
	nonce := make([]byte, aead.NonceSize())
	_, err = rand.Read(nonce)
	require.NoError(t, err)
	ciphertext := aead.Seal(nil, nonce, plaintext, nil)
	payload := append(append([]byte(nil), nonce...), ciphertext...)
	return append(append([]byte{1, 0, 0, 0, 0, 0, 0, 0, byte(len(seed))}, seed...), payload...)
}

func responseClient(body []byte) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(string(body))),
			Header:     make(http.Header),
		}, nil
	})}
}

func TestFetchDynamicConfigRejectsMalformedCanonicalEnvelopeWithoutPanic(t *testing.T) {
	body := encryptedControlResponse(t, []byte(`{"data":"not-an-object"}`))
	require.NotPanics(t, func() {
		_, err := fetchDynamicConfig(context.Background(), responseClient(body), "node-fixture", "node-id-fixture", "token-fixture", "device-fixture", "tablet-fixture")
		require.Error(t, err)
	})
}

func TestFetchDynamicConfigRejectsMalformedCanonicalYAML(t *testing.T) {
	smart := encryptBlackstonePayloadFixture(t, []byte("proxies: ["))
	layer1, err := json.Marshal(map[string]any{"data": map[string]any{"smart": base64.StdEncoding.EncodeToString(smart)}})
	require.NoError(t, err)
	body := encryptedControlResponse(t, layer1)
	_, err = fetchDynamicConfig(context.Background(), responseClient(body), "node-fixture", "node-id-fixture", "token-fixture", "device-fixture", "tablet-fixture")
	require.Error(t, err)
}
