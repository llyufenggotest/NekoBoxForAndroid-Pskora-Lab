package clashapi

import (
	"context"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/protocol/group"
	"github.com/sagernet/sing/service"
)

type testCertificates struct {
	adapter.CertificateStore
	pool *x509.CertPool
}

func (c *testCertificates) Pool() *x509.CertPool { return c.pool }
func TestManualDelayLateSuccessfulDialDoesNotPublish(t *testing.T) {
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer target.Close()
	pool := x509.NewCertPool()
	pool.AddCert(target.Certificate())
	for _, owner := range []string{"request", "core"} {
		t.Run(owner, func(t *testing.T) {
			entered := make(chan struct{})
			release := make(chan struct{})
			connClosed := make(chan struct{})
			s, o := cancellationFixture(func(ctx context.Context) (net.Conn, error) {
				// Real socket established before cancellation, deliberately held by a
				// non-cooperative dialer until after the owner has retired.
				c, err := net.Dial("tcp", target.Listener.Addr().String())
				if err != nil {
					return nil, err
				}
				close(entered)
				<-release
				return &observedCloseConn{Conn: c, closed: connClosed}, nil
			})
			core, stopCore := context.WithCancel(context.Background())
			defer stopCore()
			s.ctx = core
			request, stopRequest := context.WithCancel(context.Background())
			defer stopRequest()
			request = service.ContextWith[adapter.CertificateStore](request, &testCertificates{pool: pool})
			r := httptest.NewRequest("GET", "/delay?timeout=500&url="+url.QueryEscape(target.URL), nil).WithContext(context.WithValue(request, CtxKeyProxy, adapter.Outbound(o)))
			w := httptest.NewRecorder()
			done := make(chan struct{})
			go func() { getProxyDelay(s)(w, r); close(done) }()
			<-entered
			if owner == "request" {
				stopRequest()
			} else {
				stopCore()
			}
			successor := &adapter.URLTestHistory{Time: time.Now(), Delay: 88}
			s.urlTestHistory.StoreURLTestHistory(o.Tag(), successor)
			close(release)
			<-done
			<-connClosed
			if w.Body.Len() != 0 || s.urlTestHistory.LoadURLTestHistory(o.Tag()) != successor {
				t.Fatal("late successful dial polluted retired owner")
			}
		})
	}
}

type observedCloseConn struct {
	net.Conn
	closed chan struct{}
}

func (c *observedCloseConn) Close() error {
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
	return c.Conn.Close()
}

func TestManualDelaySuccessfulConcurrentURLs(t *testing.T) {
	entered := make(chan string, 2)
	release := make(chan struct{})
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { entered <- r.URL.Path; <-release; w.WriteHeader(204) }))
	defer target.Close()
	pool := x509.NewCertPool()
	pool.AddCert(target.Certificate())
	ctx := service.ContextWith[adapter.CertificateStore](context.Background(), &testCertificates{pool: pool})
	s, o := cancellationFixture(func(ctx context.Context) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", target.Listener.Addr().String())
	})
	automatic := make(chan map[string]uint16, 1)
	go func() {
		automatic <- group.URLTestOutbounds(ctx, s.outbound, s.urlTestHistory, log.NewNOPFactory().Logger(), []adapter.Outbound{o}, target.URL+"/automatic", time.Minute, true)
	}()
	<-entered
	done := make(chan *httptest.ResponseRecorder, 2)
	for _, path := range []string{"/manual-a", "/manual-b"} {
		r := httptest.NewRequest("GET", "/delay?timeout=500&url="+url.QueryEscape(target.URL+path), nil).WithContext(context.WithValue(ctx, CtxKeyProxy, adapter.Outbound(o)))
		go func() { w := httptest.NewRecorder(); getProxyDelay(s)(w, r); done <- w }()
	}
	<-entered
	select {
	case <-entered:
	case <-time.After(100 * time.Millisecond):
		close(release)
		<-done
		<-done
		t.Fatal("different manual URLs serialized")
	}
	time.Sleep(5 * time.Millisecond)
	close(release)
	for range 2 {
		w := <-done
		if w.Code != 200 || !strings.Contains(w.Body.String(), "delay") {
			t.Fatalf("success contract: %d %s", w.Code, w.Body.String())
		}
	}
	if result := <-automatic; result[o.Tag()] == 0 {
		t.Fatal("overlapping automatic probe did not succeed")
	}
	if h := s.urlTestHistory.LoadURLTestHistory(o.Tag()); h == nil || h.Delay == 0 {
		t.Fatal("successful real HTTPS not published")
	}
}
func TestManualDelayTimeoutQueryContract(t *testing.T) {
	for _, test := range []struct {
		query string
		code  int
	}{{"", 400}, {"x", 400}, {"32768", 400}, {"-32769", 400}, {"0", 504}, {"-1", 504}, {"-32768", 504}, {"30", 504}, {"32767", 503}} {
		t.Run("timeout="+test.query, func(t *testing.T) {
			s, o := cancellationFixture(func(ctx context.Context) (net.Conn, error) {
				if test.query == "32767" {
					return nil, net.ErrClosed
				}
				<-ctx.Done()
				return nil, ctx.Err()
			})
			r := httptest.NewRequest("GET", "/delay?timeout="+url.QueryEscape(test.query), nil).WithContext(context.WithValue(context.Background(), CtxKeyProxy, adapter.Outbound(o)))
			w := httptest.NewRecorder()
			getProxyDelay(s)(w, r)
			if w.Code != test.code {
				t.Fatalf("got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}
func TestRequestContextCleanupAndEpochs(t *testing.T) {
	owner, stopOwner := context.WithCancel(context.Background())
	defer stopOwner()
	for range 200 {
		request, stopRequest := context.WithCancel(context.WithValue(context.Background(), "sentinel", "kept"))
		ctx, release := requestContext(request, owner)
		if ctx.Value("sentinel") != "kept" {
			t.Fatal("request values lost")
		}
		stopRequest()
		<-ctx.Done()
		release()
	}
	old, release := requestContext(context.Background(), owner)
	stopOwner()
	<-old.Done()
	release()
	nextOwner, stopNext := context.WithCancel(context.Background())
	defer stopNext()
	next, releaseNext := requestContext(context.Background(), nextOwner)
	defer releaseNext()
	if next.Err() != nil {
		t.Fatal("old callback polluted successor")
	}
}
