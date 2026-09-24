package libcore

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	box "github.com/sagernet/sing-box"
)

type progressRecorder struct {
	mu       sync.Mutex
	progress []speedProgress
	complete int
	errors   []string
}

type speedProgress struct {
	phase         string
	current, peak float64
	bytes         int64
}

func (r *progressRecorder) OnSpeedTestProgress(phase string, currentMBps, peakMBps float64, transferredBytes int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.progress = append(r.progress, speedProgress{phase, currentMBps, peakMBps, transferredBytes})
}
func (r *progressRecorder) OnSpeedTestComplete(downloadMBps, uploadMBps float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.complete++
}
func (r *progressRecorder) OnSpeedTestError(message string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errors = append(r.errors, message)
}

func TestStartSpeedTestRequiresStartedBox(t *testing.T) {
	instance := &BoxInstance{Box: new(box.Box)}
	if err := instance.StartSpeedTest(1, &progressRecorder{}); err == nil {
		t.Fatal("expected stopped box error")
	}
}

func TestCanceledSpeedTestHasOneTerminalCallbackAndCannotClearReplacement(t *testing.T) {
	firstStarted := make(chan struct{})
	firstRelease := make(chan struct{})
	secondStarted := make(chan struct{})
	instance := &BoxInstance{Box: new(box.Box), state: 1}
	var calls atomic.Int32
	instance.speedTestRun = func(ctx context.Context, _ int, listener SpeedTestListener, _ speedTestEndpoints) error {
		switch calls.Add(1) {
		case 1:
			close(firstStarted)
			<-ctx.Done()
			<-firstRelease
			return ctx.Err()
		case 2:
			close(secondStarted)
			<-ctx.Done()
			return ctx.Err()
		default:
			return errors.New("unexpected run")
		}
	}

	first := &progressRecorder{}
	if err := instance.StartSpeedTest(1, first); err != nil {
		t.Fatal(err)
	}
	<-firstStarted
	instance.CancelSpeedTest()
	second := &progressRecorder{}
	if err := instance.StartSpeedTest(1, second); err != nil {
		t.Fatalf("replacement start: %v", err)
	}
	<-secondStarted
	close(firstRelease)
	waitForTerminal(t, first)
	if err := instance.StartSpeedTest(1, &progressRecorder{}); err == nil {
		t.Fatal("finished first run cleared replacement token")
	}
	instance.CancelSpeedTest()
	waitForTerminal(t, second)
	first.mu.Lock()
	defer first.mu.Unlock()
	if first.complete != 0 || len(first.errors) != 1 {
		t.Fatalf("first terminal callbacks: complete=%d errors=%v", first.complete, first.errors)
	}
}

func TestSpeedTestDoesNotCallbackAfterBoxClosed(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	instance := &BoxInstance{Box: new(box.Box), state: 1}
	instance.speedTestRun = func(context.Context, int, SpeedTestListener, speedTestEndpoints) error {
		close(started)
		<-release
		return errors.New("late failure")
	}
	recorder := &progressRecorder{}
	if err := instance.StartSpeedTest(1, recorder); err != nil {
		t.Fatal(err)
	}
	<-started
	instance.access.Lock()
	instance.state = 2
	instance.access.Unlock()
	close(release)
	time.Sleep(50 * time.Millisecond)
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if recorder.complete != 0 || len(recorder.errors) != 0 || len(recorder.progress) != 0 {
		t.Fatalf("callbacks after close: %+v", recorder)
	}
}

func waitForTerminal(t *testing.T, recorder *progressRecorder) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		recorder.mu.Lock()
		terminals := recorder.complete + len(recorder.errors)
		recorder.mu.Unlock()
		if terminals != 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for terminal callback")
}

func TestNormalizeSpeedTestStreams(t *testing.T) {
	for input, want := range map[int32]int{0: 1, 1: 1, 4: 4, 8: 8, 9: 8, -1: 1} {
		if got := normalizeSpeedTestStreams(input); got != want {
			t.Fatalf("normalizeSpeedTestStreams(%d) = %d, want %d", input, got, want)
		}
	}
}

func TestRaceBackendsChoosesFirstSuccessfulProbeAndCancelsLoser(t *testing.T) {
	loserCanceled := make(chan struct{})
	backends := []speedBackend{
		{name: "slow", probe: func(ctx context.Context, _ *http.Client) (speedTarget, error) {
			<-ctx.Done()
			close(loserCanceled)
			return speedTarget{}, ctx.Err()
		}},
		{name: "fast", probe: func(context.Context, *http.Client) (speedTarget, error) {
			return speedTarget{name: "winner"}, nil
		}},
	}

	got, err := raceSpeedBackends(context.Background(), &http.Client{}, backends)
	if err != nil || got.name != "winner" {
		t.Fatalf("got %#v, %v", got, err)
	}
	select {
	case <-loserCanceled:
	case <-time.After(time.Second):
		t.Fatal("losing probe was not canceled")
	}
}

func TestRaceBackendsReturnsErrorWhenEveryProbeFails(t *testing.T) {
	fail := func(context.Context, *http.Client) (speedTarget, error) { return speedTarget{}, errors.New("no") }
	_, err := raceSpeedBackends(context.Background(), &http.Client{}, []speedBackend{{name: "a", probe: fail}, {name: "b", probe: fail}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunTransferBoundsStreamsBytesAndReportsCurrentAndPeak(t *testing.T) {
	var active, peakActive atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := active.Add(1)
		defer active.Add(-1)
		for {
			old := peakActive.Load()
			if n <= old || peakActive.CompareAndSwap(old, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		_, _ = io.CopyN(w, zeroReader{}, 1<<20)
	}))
	defer srv.Close()

	recorder := &progressRecorder{}
	limits := transferLimits{duration: 2 * time.Second, maxBytes: 256 * 1024, reportEvery: 10 * time.Millisecond}
	result, err := runDownload(context.Background(), srv.Client(), srv.URL, 4, limits, recorder)
	if err != nil {
		t.Fatal(err)
	}
	if result.bytes <= 0 || result.bytes > limits.maxBytes {
		t.Fatalf("transferred %d bytes, cap %d", result.bytes, limits.maxBytes)
	}
	if peakActive.Load() > 4 || peakActive.Load() < 2 {
		t.Fatalf("peak concurrent requests = %d", peakActive.Load())
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if len(recorder.progress) == 0 {
		t.Fatal("no progress callbacks")
	}
	var previousPeak float64
	for _, p := range recorder.progress {
		if p.current < 0 || p.peak < p.current || p.peak < previousPeak {
			t.Fatalf("invalid current/peak progress: %#v after peak %f", p, previousPeak)
		}
		previousPeak = p.peak
	}
}

func TestRunTransferHonorsCancellationAndTimeLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(30*time.Millisecond, cancel)
	started := time.Now()
	_, err := runDownload(ctx, srv.Client(), srv.URL, 1, transferLimits{duration: time.Second, maxBytes: 1024}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context canceled", err)
	}
	if time.Since(started) > 500*time.Millisecond {
		t.Fatal("cancellation was not prompt")
	}
}

func TestUploadUsesRequestedStreamsAndCapsBodyBytes(t *testing.T) {
	var requests atomic.Int32
	var bytes atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		n, _ := io.Copy(io.Discard, r.Body)
		bytes.Add(n)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	limits := transferLimits{duration: time.Second, maxBytes: 128 * 1024, requestBytes: 32 * 1024}
	result, err := runUpload(context.Background(), srv.Client(), srv.URL, 3, limits, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.bytes != bytes.Load() || result.bytes > limits.maxBytes {
		t.Fatalf("result=%d server=%d cap=%d", result.bytes, bytes.Load(), limits.maxBytes)
	}
	if requests.Load() < 3 {
		t.Fatalf("requests=%d, want at least one per stream", requests.Load())
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}
