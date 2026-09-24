package libcore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"libcore/boxapi"
)

const (
	defaultSpeedTestTimeout   = 30 * time.Second
	defaultTransferDuration   = 8 * time.Second
	defaultTransferByteLimit  = int64(128 * 1000 * 1000)
	defaultUploadRequestBytes = int64(512 * 1000)
)

// SpeedTestListener is safe for gomobile bindings. Speeds use decimal MB/s.
type SpeedTestListener interface {
	OnSpeedTestProgress(phase string, currentMBps, peakMBps float64, transferredBytes int64)
	OnSpeedTestComplete(downloadMBps, uploadMBps float64)
	OnSpeedTestError(message string)
}

type speedTestSession struct {
	cancel context.CancelFunc
	once   sync.Once
}

type guardedSpeedTestListener struct {
	box      *BoxInstance
	session  *speedTestSession
	listener SpeedTestListener
}

func (l guardedSpeedTestListener) OnSpeedTestProgress(phase string, currentMBps, peakMBps float64, transferredBytes int64) {
	l.box.access.Lock()
	defer l.box.access.Unlock()
	if l.box.state != 1 {
		return
	}
	l.box.speedTestMu.Lock()
	current := l.box.speedTestSession == l.session
	l.box.speedTestMu.Unlock()
	if current {
		l.listener.OnSpeedTestProgress(phase, currentMBps, peakMBps, transferredBytes)
	}
}

func (l guardedSpeedTestListener) OnSpeedTestComplete(downloadMBps, uploadMBps float64) {
	l.box.access.Lock()
	defer l.box.access.Unlock()
	if l.box.state == 1 {
		l.session.once.Do(func() { l.listener.OnSpeedTestComplete(downloadMBps, uploadMBps) })
	}
}

func (l guardedSpeedTestListener) OnSpeedTestError(message string) {
	l.box.access.Lock()
	defer l.box.access.Unlock()
	if l.box.state == 1 {
		l.session.once.Do(func() { l.listener.OnSpeedTestError(message) })
	}
}

type speedTarget struct {
	name        string
	downloadURL string
	uploadURL   string
}

type speedBackend struct {
	name  string
	probe func(context.Context, *http.Client) (speedTarget, error)
}

type transferLimits struct {
	duration     time.Duration
	maxBytes     int64
	requestBytes int64
	reportEvery  time.Duration
}

type transferResult struct {
	bytes int64
	mbps  float64
}

type speedTestEndpoints struct {
	speedtestServers string
	googleServers    string
}

var defaultSpeedTestEndpoints = speedTestEndpoints{
	speedtestServers: "https://www.speedtest.net/api/js/servers?engine=js&limit=5&https_functional=true",
	googleServers:    "https://gfiber.speedtestcustom.com/api/js/servers?engine=js&limit=5",
}

func normalizeSpeedTestStreams(streams int32) int {
	if streams < 1 {
		return 1
	}
	if streams > 8 {
		return 8
	}
	return int(streams)
}

// StartSpeedTest starts a routed test. A tap should pass 1; a long press should pass 8.
func (b *BoxInstance) StartSpeedTest(streams int32, listener SpeedTestListener) error {
	if b == nil {
		return errors.New("speed test requires a running box")
	}
	if listener == nil {
		return errors.New("speed test listener is nil")
	}

	b.access.Lock()
	defer b.access.Unlock()
	if b.state != 1 || b.Box == nil {
		return errors.New("speed test requires a running box")
	}
	b.speedTestMu.Lock()
	if b.speedTestSession != nil {
		b.speedTestMu.Unlock()
		return errors.New("speed test already running")
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultSpeedTestTimeout)
	session := &speedTestSession{cancel: cancel}
	b.speedTestSession = session
	b.speedTestMu.Unlock()

	go func() {
		guarded := guardedSpeedTestListener{box: b, session: session, listener: listener}
		run := b.speedTestRun
		if run == nil {
			run = b.runSpeedTest
		}
		err := run(ctx, normalizeSpeedTestStreams(streams), guarded, defaultSpeedTestEndpoints)
		b.speedTestMu.Lock()
		if b.speedTestSession == session {
			b.speedTestSession = nil
		}
		b.speedTestMu.Unlock()
		cancel()
		if err != nil {
			guarded.OnSpeedTestError(err.Error())
		}
	}()
	return nil
}

// CancelSpeedTest promptly cancels discovery or active transfers.
func (b *BoxInstance) CancelSpeedTest() {
	if b == nil {
		return
	}
	b.speedTestMu.Lock()
	session := b.speedTestSession
	if session != nil {
		b.speedTestSession = nil
	}
	b.speedTestMu.Unlock()
	if session != nil {
		session.cancel()
	}
}

func (b *BoxInstance) runSpeedTest(ctx context.Context, streams int, listener SpeedTestListener, endpoints speedTestEndpoints) error {
	var tracker adapter.ConnectionTracker
	if b.v2api != nil {
		tracker = b.v2api.StatsService()
	}
	// This is the only client construction path: discovery, probes and payloads all
	// use the selected Box router. There is deliberately no direct fallback.
	client := boxapi.CreateProxyHttpClient(b.Box, tracker)
	defer client.CloseIdleConnections()

	probeCtx, cancelProbe := context.WithTimeout(ctx, 6*time.Second)
	target, err := raceSpeedBackends(probeCtx, client, makeSpeedBackends(endpoints))
	cancelProbe()
	if err != nil {
		return fmt.Errorf("select speed test backend: %w", err)
	}
	limits := transferLimits{duration: defaultTransferDuration, maxBytes: defaultTransferByteLimit, requestBytes: defaultUploadRequestBytes, reportEvery: 250 * time.Millisecond}
	download, err := runDownload(ctx, client, target.downloadURL, streams, limits, listener)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	upload, err := runUpload(ctx, client, target.uploadURL, streams, limits, listener)
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	listener.OnSpeedTestComplete(download.mbps, upload.mbps)
	return nil
}

func makeSpeedBackends(endpoints speedTestEndpoints) []speedBackend {
	return []speedBackend{
		{name: "speedtest.net", probe: serverListProbe(endpoints.speedtestServers, false)},
		{name: "google-fiber", probe: serverListProbe(endpoints.googleServers, true)},
	}
}

func serverListProbe(discoveryURL string, googleFiber bool) func(context.Context, *http.Client) (speedTarget, error) {
	return func(ctx context.Context, client *http.Client) (speedTarget, error) {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
		if err != nil {
			return speedTarget{}, err
		}
		setSpeedTestHeaders(request)
		response, err := client.Do(request)
		if err != nil {
			return speedTarget{}, err
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return speedTarget{}, fmt.Errorf("discovery HTTP %d", response.StatusCode)
		}
		var servers []struct {
			URL  string `json:"url"`
			Host string `json:"host"`
		}
		if err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&servers); err != nil {
			return speedTarget{}, err
		}
		var lastErr error
		for index, server := range servers {
			if index == 3 {
				break
			}
			target, probeURL, parseErr := targetFromServer(server.URL, server.Host, googleFiber)
			if parseErr != nil {
				lastErr = parseErr
				continue
			}
			probeRequest, _ := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
			setSpeedTestHeaders(probeRequest)
			probeResponse, probeErr := client.Do(probeRequest)
			if probeErr == nil {
				_, _ = io.Copy(io.Discard, io.LimitReader(probeResponse.Body, 1024))
				probeResponse.Body.Close()
				if probeResponse.StatusCode == http.StatusOK {
					return target, nil
				}
				probeErr = fmt.Errorf("probe HTTP %d", probeResponse.StatusCode)
			}
			lastErr = probeErr
		}
		if lastErr == nil {
			lastErr = errors.New("no usable servers")
		}
		return speedTarget{}, lastErr
	}
}

func targetFromServer(rawURL, host string, googleFiber bool) (speedTarget, string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" {
		return speedTarget{}, "", errors.New("invalid server URL")
	}
	if parsed.Host == "" {
		parsed.Host = host
	}
	if parsed.Host == "" {
		return speedTarget{}, "", errors.New("server has no host")
	}
	if googleFiber {
		base := parsed.Scheme + "://" + parsed.Host
		return speedTarget{name: "google-fiber", downloadURL: base + "/download?size=25000000", uploadURL: base + "/upload"}, base + "/ping", nil
	}
	path := strings.TrimSuffix(parsed.Path, "/upload.php")
	base := parsed.Scheme + "://" + parsed.Host + strings.TrimSuffix(path, "/")
	return speedTarget{name: "speedtest.net", downloadURL: base + "/random3000x3000.jpg", uploadURL: base + "/upload.php"}, base + "/latency.txt", nil
}

type backendProbeResult struct {
	target speedTarget
	err    error
}

func raceSpeedBackends(ctx context.Context, client *http.Client, backends []speedBackend) (speedTarget, error) {
	if len(backends) == 0 {
		return speedTarget{}, errors.New("no speed test backends")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan backendProbeResult, len(backends))
	for _, backend := range backends {
		go func(backend speedBackend) {
			target, err := backend.probe(ctx, client)
			results <- backendProbeResult{target: target, err: err}
		}(backend)
	}
	var failures []string
	for range backends {
		select {
		case <-ctx.Done():
			return speedTarget{}, ctx.Err()
		case result := <-results:
			if result.err == nil {
				cancel()
				return result.target, nil
			}
			failures = append(failures, result.err.Error())
		}
	}
	return speedTarget{}, errors.New(strings.Join(failures, "; "))
}

func runDownload(ctx context.Context, client *http.Client, downloadURL string, streams int, limits transferLimits, listener SpeedTestListener) (transferResult, error) {
	return runTransfer(ctx, "download", streams, limits, listener, func(ctx context.Context, allowance int64) (int64, error) {
		separator := "?"
		if strings.Contains(downloadURL, "?") {
			separator = "&"
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s%snocache=%d", downloadURL, separator, time.Now().UnixNano()), nil)
		if err != nil {
			return 0, err
		}
		setSpeedTestHeaders(request)
		response, err := client.Do(request)
		if err != nil {
			return 0, err
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return 0, fmt.Errorf("HTTP %d", response.StatusCode)
		}
		return io.Copy(io.Discard, io.LimitReader(response.Body, allowance))
	})
}

func runUpload(ctx context.Context, client *http.Client, uploadURL string, streams int, limits transferLimits, listener SpeedTestListener) (transferResult, error) {
	requestBytes := limits.requestBytes
	if requestBytes <= 0 {
		requestBytes = defaultUploadRequestBytes
	}
	payload := make([]byte, requestBytes)
	return runTransfer(ctx, "upload", streams, limits, listener, func(ctx context.Context, allowance int64) (int64, error) {
		size := min(requestBytes, allowance)
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, bytes.NewReader(payload[:size]))
		if err != nil {
			return 0, err
		}
		setSpeedTestHeaders(request)
		request.Header.Set("Content-Type", "application/octet-stream")
		response, err := client.Do(request)
		if err != nil {
			return 0, err
		}
		defer response.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		if response.StatusCode != http.StatusOK {
			return 0, fmt.Errorf("HTTP %d", response.StatusCode)
		}
		return size, nil
	})
}

func runTransfer(parentCtx context.Context, phase string, streams int, limits transferLimits, listener SpeedTestListener, request func(context.Context, int64) (int64, error)) (transferResult, error) {
	streams = normalizeSpeedTestStreams(int32(streams))
	if limits.duration <= 0 {
		limits.duration = defaultTransferDuration
	}
	if limits.maxBytes <= 0 {
		limits.maxBytes = defaultTransferByteLimit
	}
	if limits.reportEvery <= 0 {
		limits.reportEvery = 250 * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(parentCtx, limits.duration)
	defer cancel()
	started := time.Now()
	var reserved, completed atomic.Int64
	var firstErr error
	var firstErrOnce sync.Once
	var workers sync.WaitGroup
	for range streams {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for ctx.Err() == nil {
				preferred := limits.requestBytes
				if preferred <= 0 {
					preferred = 64 * 1024
				}
				allowance := reserveTransferBytes(&reserved, limits.maxBytes, preferred)
				if allowance == 0 {
					return
				}
				n, err := request(ctx, allowance)
				if n < allowance {
					reserved.Add(n - allowance)
				}
				if n > 0 {
					completed.Add(n)
				}
				if err != nil && ctx.Err() == nil {
					firstErrOnce.Do(func() { firstErr = err })
					return
				}
			}
		}()
	}

	done := make(chan struct{})
	go func() { workers.Wait(); close(done) }()
	ticker := time.NewTicker(limits.reportEvery)
	defer ticker.Stop()
	var lastBytes int64
	lastReport := started
	var peak float64
	for {
		select {
		case <-ticker.C:
			now := time.Now()
			total := completed.Load()
			current := float64(total-lastBytes) / now.Sub(lastReport).Seconds() / 1e6
			if current > peak {
				peak = current
			}
			if listener != nil {
				listener.OnSpeedTestProgress(phase, current, peak, total)
			}
			lastBytes, lastReport = total, now
		case <-done:
			if err := parentCtx.Err(); err != nil {
				return transferResult{}, err
			}
			elapsed := time.Since(started).Seconds()
			total := completed.Load()
			if elapsed <= 0 {
				elapsed = 1e-9
			}
			average := float64(total) / elapsed / 1e6
			if average > peak {
				peak = average
			}
			if listener != nil {
				listener.OnSpeedTestProgress(phase, average, peak, total)
			}
			if ctx.Err() != nil && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return transferResult{}, ctx.Err()
			}
			if total == 0 {
				if firstErr != nil {
					return transferResult{}, firstErr
				}
				return transferResult{}, errors.New("no data transferred")
			}
			return transferResult{bytes: total, mbps: average}, nil
		}
	}
}

func reserveTransferBytes(reserved *atomic.Int64, limit, preferred int64) int64 {
	for {
		used := reserved.Load()
		if used >= limit {
			return 0
		}
		amount := min(preferred, limit-used)
		if reserved.CompareAndSwap(used, used+amount) {
			return amount
		}
	}
}

func setSpeedTestHeaders(request *http.Request) {
	request.Header.Set("User-Agent", "Mozilla/5.0 Chrome/120 SpeedTest")
	request.Header.Set("Accept", "*/*")
	request.Header.Set("Cache-Control", "no-cache")
	request.Header.Set("Pragma", "no-cache")
}
