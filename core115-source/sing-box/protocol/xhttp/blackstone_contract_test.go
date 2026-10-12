package xhttp

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math"
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

func TestLazyInitCanceledAttemptCanRetry(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	outbound := &Outbound{initializeFn: func(ctx context.Context) error {
		if calls.Add(1) == 1 {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}}
	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { first <- outbound.lazyInit(ctx) }()
	<-started
	cancel()
	require.ErrorIs(t, <-first, context.Canceled)
	require.NoError(t, outbound.lazyInit(context.Background()))
	require.Equal(t, int32(2), calls.Load())
}

func TestLazyInitDeadlineAttemptCanRetry(t *testing.T) {
	var calls atomic.Int32
	outbound := &Outbound{initializeFn: func(ctx context.Context) error {
		if calls.Add(1) == 1 {
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, outbound.lazyInit(ctx), context.DeadlineExceeded)
	require.NoError(t, outbound.lazyInit(context.Background()))
	require.Equal(t, int32(2), calls.Load())
}

func TestLazyInitTemporaryFailureCanRetry(t *testing.T) {
	var calls atomic.Int32
	temporary := errors.New("temporary network failure")
	outbound := &Outbound{initializeFn: func(context.Context) error {
		if calls.Add(1) == 1 {
			return temporary
		}
		return nil
	}}
	require.ErrorIs(t, outbound.lazyInit(context.Background()), temporary)
	require.NoError(t, outbound.lazyInit(context.Background()))
	require.Equal(t, int32(2), calls.Load())
}

func TestLazyInitConcurrentWaitersShareAttemptResult(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	failure := errors.New("shared initialization failure")
	outbound := &Outbound{initializeFn: func(context.Context) error {
		calls.Add(1)
		close(started)
		<-release
		return failure
	}}
	const waiterCount = 64
	results := make(chan error, waiterCount)
	start := make(chan struct{})
	for i := 0; i < waiterCount; i++ {
		go func() {
			<-start
			results <- outbound.lazyInit(context.Background())
		}()
	}
	close(start)
	<-started
	deadline := time.Now().Add(time.Second)
	for {
		outbound.initMu.Lock()
		attemptRunning := outbound.initAttempt != nil
		outbound.initMu.Unlock()
		if attemptRunning && calls.Load() == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("concurrent callers did not join the shared attempt")
		}
		time.Sleep(time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	for i := 0; i < waiterCount; i++ {
		require.ErrorIs(t, <-results, failure)
	}
	require.Equal(t, int32(1), calls.Load())
}

func TestLazyInitWaiterCancellationDoesNotCancelSharedAttempt(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	outbound := &Outbound{initializeFn: func(context.Context) error {
		calls.Add(1)
		close(started)
		<-release
		return nil
	}}
	leader := make(chan error, 1)
	go func() { leader <- outbound.lazyInit(context.Background()) }()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	waiter := make(chan error, 1)
	go func() { waiter <- outbound.lazyInit(ctx) }()
	cancel()
	select {
	case err := <-waiter:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("canceled waiter did not return promptly")
	}
	close(release)
	require.NoError(t, <-leader)
	require.NoError(t, outbound.lazyInit(context.Background()))
	require.Equal(t, int32(1), calls.Load())
}

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

type errorClosingBody struct {
	io.Reader
	err error
}

func (b errorClosingBody) Close() error { return b.err }

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
	attempt := &initAttempt{done: make(chan struct{})}
	outbound := &Outbound{initAttempt: attempt}
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
	attempt.err = errors.New("initializer fixture failure")
	outbound.initAttempt = nil
	close(attempt.done)
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

func TestFetchDynamicConfigRejectsInvalidBase64WithoutPanic(t *testing.T) {
	require.NotPanics(t, func() {
		_, err := fetchDynamicConfig(context.Background(), responseClient([]byte("%%%")), "node-fixture", "node-id-fixture", "token-fixture", "device-fixture", "tablet-fixture")
		require.Error(t, err)
	})
}

func TestFetchDynamicConfigRejectsInvalidDecryptedJSONWithoutPanic(t *testing.T) {
	body := encryptedControlResponse(t, []byte("{"))
	require.NotPanics(t, func() {
		_, err := fetchDynamicConfig(context.Background(), responseClient(body), "node-fixture", "node-id-fixture", "token-fixture", "device-fixture", "tablet-fixture")
		require.Error(t, err)
	})
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

func gzipFixture(t *testing.T, payload []byte) []byte {
	t.Helper()
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, err := writer.Write(payload)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return compressed.Bytes()
}

func TestFetchDynamicConfigReportsBodyCloseError(t *testing.T) {
	closeFailure := errors.New("close fixture failure")
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: errorClosingBody{
				Reader: strings.NewReader("{}"),
				err:    closeFailure,
			},
			Header: make(http.Header),
		}, nil
	})}
	_, err := fetchDynamicConfig(context.Background(), client, "node-fixture", "node-id-fixture", "token-fixture", "device-fixture", "tablet-fixture")
	require.ErrorIs(t, err, closeFailure)
}

func TestFetchDynamicConfigRejectsOversizedHTTPBody(t *testing.T) {
	body := bytes.Repeat([]byte("x"), maxControlResponseBytes+1)
	_, err := fetchDynamicConfig(context.Background(), responseClient(body), "node-fixture", "node-id-fixture", "token-fixture", "device-fixture", "tablet-fixture")
	require.Error(t, err)
	require.Contains(t, err.Error(), "too large")
	for _, secret := range []string{"token-fixture", "device-fixture", "tablet-fixture", string(body[:32])} {
		require.NotContains(t, err.Error(), secret)
	}
}

func TestReadBoundedAcceptsExactLimitAndRejectsLimitPlusOne(t *testing.T) {
	payload := bytes.Repeat([]byte("a"), maxControlResponseBytes)
	actual, err := readBounded(strings.NewReader(string(payload)), maxControlResponseBytes, "control response")
	require.NoError(t, err)
	require.Equal(t, payload, actual)
	_, err = readBounded(strings.NewReader(string(append(payload, 'b'))), maxControlResponseBytes, "control response")
	require.Error(t, err)
}

func TestDecodeMaybeGzipRejectsTruncatedStream(t *testing.T) {
	compressed := gzipFixture(t, []byte(`{"data":{}}`))
	_, err := decodeMaybeGzip(compressed[:len(compressed)-4], maxDecodedControlBytes, "control response")
	require.Error(t, err)
}

func TestDecodeMaybeGzipRejectsBombAndAcceptsBoundary(t *testing.T) {
	boundary := bytes.Repeat([]byte("a"), maxDecodedControlBytes)
	actual, err := decodeMaybeGzip(gzipFixture(t, boundary), maxDecodedControlBytes, "control response")
	require.NoError(t, err)
	require.Equal(t, boundary, actual)
	_, err = decodeMaybeGzip(gzipFixture(t, append(boundary, 'b')), maxDecodedControlBytes, "control response")
	require.Error(t, err)
}

func malformedChunkPayload(start, end uint32, payload []byte) []byte {
	raw := make([]byte, 9+len(payload))
	raw[0] = 1
	binary.BigEndian.PutUint32(raw[1:5], start)
	binary.BigEndian.PutUint32(raw[5:9], end)
	copy(raw[9:], payload)
	return raw
}

func TestDecryptBlackstonePayloadRejectsInvalidOffsetsWithoutPanic(t *testing.T) {
	for name, raw := range map[string][]byte{
		"reversed":      malformedChunkPayload(2, 1, []byte("abc")),
		"out of bounds": malformedChunkPayload(0, 4, []byte("abc")),
		"max uint32":    malformedChunkPayload(math.MaxUint32, math.MaxUint32, []byte("abc")),
	} {
		t.Run(name, func(t *testing.T) {
			require.NotPanics(t, func() {
				_, err := decryptBlackstonePayload(raw)
				require.Error(t, err)
			})
		})
	}
}

func TestDecryptBlackstonePayloadAcceptsChunkBoundary(t *testing.T) {
	plaintext := []byte("proxies: []")
	raw := encryptBlackstonePayloadFixture(t, plaintext)
	actual, err := decryptBlackstonePayload(raw)
	require.NoError(t, err)
	require.Equal(t, plaintext, actual)
}
