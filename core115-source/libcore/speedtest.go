package libcore

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"libcore/boxapi"
)

const (
	defaultSpeedTestTimeout     = 30 * time.Second
	defaultTransferDuration     = 8 * time.Second
	defaultTransferByteLimit    = int64(128 * 1000 * 1000)
	defaultSpeedtestUploadBytes = int64(512 * 1024)
	defaultOoklaUploadBytes     = int64(512 * 1024)
	downloadReadBufferBytes     = 256 * 1024
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

// UI contract: a normal tap requests 8 streams; a long press requests 1.
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
	limits := transferLimits{duration: defaultTransferDuration, maxBytes: defaultTransferByteLimit, reportEvery: 250 * time.Millisecond}
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
	return runTransfer(ctx, "download", streams, limits, listener, false, func(ctx context.Context, allowance int64, report func(int64) int64, _ func(time.Time)) (int64, error) {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, addNoCache(downloadURL), nil)
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
		writer := &transferProgressWriter{report: report}
		_, err = io.CopyBuffer(writer, io.LimitReader(response.Body, allowance), make([]byte, downloadReadBufferBytes))
		if errors.Is(err, errTransferLimit) {
			err = nil
		}
		return writer.bytes, err
	})
}

func runUpload(ctx context.Context, client *http.Client, uploadURL string, streams int, limits transferLimits, listener SpeedTestListener) (transferResult, error) {
	speedtestPayload := isSpeedtestUpload(uploadURL)
	requestBytes := limits.requestBytes
	if requestBytes <= 0 {
		if speedtestPayload {
			requestBytes = defaultSpeedtestUploadBytes
		} else {
			requestBytes = defaultOoklaUploadBytes
		}
	}
	return runUploadWithRequestBytes(ctx, client, uploadURL, streams, limits, listener, speedtestPayload, requestBytes, 0)
}

func runUploadWithRequestBytes(ctx context.Context, client *http.Client, uploadURL string, streams int, limits transferLimits, listener SpeedTestListener, speedtestPayload bool, requestBytes int64, retry int) (transferResult, error) {
	// Multi-stream mode uses conservative per-request payloads: several
	// Speedtest-Custom/Ookla servers close large concurrent POST bodies early.
	if streams > 1 && requestBytes > 256*1024 {
		requestBytes = 256 * 1024
	}
	payload := make([]byte, requestBytes)
	if _, err := rand.Read(payload); err != nil {
		return transferResult{}, fmt.Errorf("create upload payload: %w", err)
	}
	if speedtestPayload {
		copy(payload, "content1=")
	}
	uploadLimits := limits
	uploadLimits.requestBytes = requestBytes
	result, err := runTransfer(ctx, "upload", streams, uploadLimits, listener, true, func(ctx context.Context, allowance int64, report func(int64) int64, begin func(time.Time)) (int64, error) {
		size := min(requestBytes, allowance)
		requestURL := uploadURL
		if !speedtestPayload {
			requestURL = addNoCache(requestURL)
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(payload[:size]))
		if err != nil {
			return 0, err
		}
		setSpeedTestHeaders(request)
		if speedtestPayload {
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		} else {
			request.Header.Set("Content-Type", "application/octet-stream")
		}
		requestStarted := time.Now()
		response, err := client.Do(request)
		if err != nil {
			return 0, err
		}
		defer response.Body.Close()
		responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, 4096))
		if readErr != nil {
			return 0, readErr
		}
		if response.StatusCode != http.StatusOK {
			return 0, fmt.Errorf("HTTP %d", response.StatusCode)
		}
		confirmed := size
		if !speedtestPayload {
			text := strings.TrimSpace(string(responseBody))
			if !strings.HasPrefix(text, "size=") {
				return 0, fmt.Errorf("invalid upload response %q", text)
			}
			confirmed, err = strconv.ParseInt(strings.TrimPrefix(text, "size="), 10, 64)
			if err != nil || confirmed < 0 || confirmed > size {
				return 0, fmt.Errorf("invalid upload size %q", text)
			}
		}
		begin(requestStarted)
		return report(confirmed), nil
	})
	if err != nil && result.bytes == 0 && !speedtestPayload && retry < 3 && ctx.Err() == nil && requestBytes > 64*1024 {
		client.CloseIdleConnections()
		return runUploadWithRequestBytes(ctx, client, uploadURL, max(1, streams/2), limits, listener, false, max(64*1024, requestBytes/2), retry+1)
	}
	return result, err
}

var errTransferLimit = errors.New("transfer byte limit reached")

type transferProgressWriter struct {
	report func(int64) int64
	bytes  int64
}

func (w *transferProgressWriter) Write(p []byte) (int, error) {
	accepted := w.report(int64(len(p)))
	w.bytes += accepted
	if accepted < int64(len(p)) {
		return int(accepted), errTransferLimit
	}
	return len(p), nil
}

func addNoCache(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	query := parsed.Query()
	query.Set("nocache", strconv.FormatInt(time.Now().UnixNano(), 10))
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func isSpeedtestUpload(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	return err == nil && strings.HasSuffix(parsed.Path, "/upload.php")
}

func runTransfer(parentCtx context.Context, phase string, streams int, limits transferLimits, listener SpeedTestListener, reserveRequests bool, request func(context.Context, int64, func(int64) int64, func(time.Time)) (int64, error)) (transferResult, error) {
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
	var firstByte, lastByte atomic.Int64
	begin := func(start time.Time) {
		startNano := start.UnixNano()
		for {
			first := firstByte.Load()
			if first != 0 && first <= startNano {
				return
			}
			if firstByte.CompareAndSwap(first, startNano) {
				return
			}
		}
	}
	report := func(n int64) int64 {
		if n <= 0 {
			return 0
		}
		for {
			total := completed.Load()
			if total >= limits.maxBytes {
				return 0
			}
			accepted := min(n, limits.maxBytes-total)
			if completed.CompareAndSwap(total, total+accepted) {
				now := time.Now().UnixNano()
				begin(time.Unix(0, now))
				lastByte.Store(now)
				return accepted
			}
		}
	}
	var firstErr error
	var firstErrOnce sync.Once
	var workers sync.WaitGroup
	for range streams {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for ctx.Err() == nil {
				if completed.Load() >= limits.maxBytes {
					return
				}
				allowance := limits.maxBytes - completed.Load()
				if reserveRequests {
					preferred := limits.requestBytes
					if preferred <= 0 {
						preferred = allowance
					}
					allowance = reserveTransferBytes(&reserved, limits.maxBytes, preferred)
					if allowance == 0 {
						return
					}
				}
				n, err := request(ctx, allowance, report, begin)
				if reserveRequests && n < allowance {
					reserved.Add(n - allowance)
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
			intervalStart := lastReport
			if lastBytes == 0 {
				if first := firstByte.Load(); first != 0 {
					intervalStart = time.Unix(0, first)
				}
			}
			intervalSeconds := now.Sub(intervalStart).Seconds()
			current := 0.0
			if intervalSeconds > 0 {
				current = float64(total-lastBytes) / intervalSeconds / 1e6
			}
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
			total := completed.Load()
			elapsed := time.Since(started).Seconds()
			first, last := firstByte.Load(), lastByte.Load()
			if first != 0 && last > first {
				elapsed = time.Duration(last - first).Seconds()
			}
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
