package clashapi

import (
	"context"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/sagernet/sing-box/adapter"
	O "github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/urltest"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/service"
)

// Test-only access to the real serialized storage boundary; no production hook.
func historyCommitLock(h *urltest.HistoryStorage) *sync.RWMutex {
	return (*sync.RWMutex)(unsafe.Pointer(reflect.ValueOf(h).Elem().FieldByName("access").UnsafeAddr()))
}

func waitHistoryCommit(t *testing.T, done <-chan struct{}) {
	t.Helper()
	until := time.Now().Add(time.Second)
	for time.Now().Before(until) {
		stack := make([]byte, 1<<20)
		n := runtime.Stack(stack, true)
		for _, g := range strings.Split(string(stack[:n]), "\n\n") {
			if strings.Contains(g, "getProxyDelay") && strings.Contains(g, "sync.(*RWMutex).Lock") && strings.Contains(g, "URLTestHistory") {
				return
			}
		}
		select {
		case <-done:
			t.Fatal("handler returned before storage boundary")
		default:
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("handler never blocked at real history commit lock")
}

func TestManualHistoryCommitLockCancellation(t *testing.T) {
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(5 * time.Millisecond); w.WriteHeader(204) }))
	defer target.Close()
	pool := x509.NewCertPool()
	pool.AddCert(target.Certificate())
	for _, result := range []string{"success", "failure"} {
		for _, owner := range []string{"request", "core"} {
			t.Run(result+"/"+owner, func(t *testing.T) {
				entered, releaseDial := make(chan struct{}), make(chan struct{})
				s, o := cancellationFixture(func(ctx context.Context) (net.Conn, error) {
					close(entered)
					<-releaseDial
					if result == "failure" {
						return nil, errors.New("measurement failed")
					}
					return (&net.Dialer{}).DialContext(ctx, "tcp", target.Listener.Addr().String())
				})
				core, stopCore := context.WithCancel(context.Background())
				defer stopCore()
				s.ctx = core
				req, stopRequest := context.WithCancel(context.Background())
				defer stopRequest()
				req = service.ContextWith[adapter.CertificateStore](req, &testCertificates{pool: pool})
				r := httptest.NewRequest("GET", "/delay?timeout=32767&url="+url.QueryEscape(target.URL), nil).WithContext(context.WithValue(req, CtxKeyProxy, adapter.Outbound(o)))
				done := make(chan struct{})
				w := httptest.NewRecorder()
				go func() { getProxyDelay(s)(w, r); close(done) }()
				<-entered
				// A successor publishes after the old measurement has started.
				successor := &adapter.URLTestHistory{Time: time.Now(), Delay: 91}
				s.urlTestHistory.StoreURLTestHistory(o.Tag(), successor)
				lock := historyCommitLock(s.urlTestHistory)
				lock.Lock()
				close(releaseDial)
				// Failing here must still release the genuine storage lock.
				func() {
					defer lock.Unlock()
					waitHistoryCommit(t, done)
					if owner == "request" {
						stopRequest()
					} else {
						stopCore()
					}
				}()
				<-done
				if h := s.urlTestHistory.LoadURLTestHistory(o.Tag()); h != successor {
					t.Fatalf("canceled lock waiter changed successor: %+v", h)
				}
			})
		}
	}
}

func TestHistoryPublicationAdmissionContract(t *testing.T) {
	for _, operation := range []string{"store", "delete"} {
		for _, boundary := range []string{"cancel-first", "commit-wins", "legacy-unconditional"} {
			t.Run(operation+"/"+boundary, func(t *testing.T) {
				h := urltest.NewHistoryStorage()
				old := &adapter.URLTestHistory{Time: time.Now(), Delay: 1}
				next := &adapter.URLTestHistory{Time: time.Now().Add(time.Second), Delay: 2}
				h.StoreURLTestHistory("leaf", old)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if boundary == "cancel-first" {
					cancel()
				}
				guard := func() bool {
					lock := historyCommitLock(h)
					if lock.TryLock() {
						lock.Unlock()
						t.Error("admission ran outside commit lock")
					}
					admitted := ctx.Err() == nil
					// The publication was admitted before cancellation; it may win.
					if boundary == "commit-wins" {
						cancel()
					}
					return admitted
				}
				var committed bool
				if boundary == "legacy-unconditional" {
					cancel()
					if operation == "store" {
						h.StoreURLTestHistory("leaf", next)
					} else {
						h.DeleteURLTestHistory("leaf")
					}
					committed = true
				} else if operation == "store" {
					committed = h.StoreURLTestHistoryIf("leaf", next, guard)
				} else {
					committed = h.DeleteURLTestHistoryIf("leaf", guard)
				}
				want := next
				if operation == "delete" {
					want = nil
				}
				if boundary == "cancel-first" {
					want = old
				}
				if committed != (boundary != "cancel-first") || h.LoadURLTestHistory("leaf") != want {
					t.Fatal("admission contract changed")
				}
			})
		}
	}
}

type cancellationOutbound struct {
	O.Adapter
	dial func(context.Context) (net.Conn, error)
}

func (o *cancellationOutbound) DialContext(ctx context.Context, _ string, _ M.Socksaddr) (net.Conn, error) {
	return o.dial(ctx)
}
func (*cancellationOutbound) ListenPacket(context.Context, M.Socksaddr) (net.PacketConn, error) {
	return nil, errors.New("unused")
}

type cancellationManager struct {
	adapter.OutboundManager
	all []adapter.Outbound
}

func (m *cancellationManager) Outbounds() []adapter.Outbound { return m.all }
func (m *cancellationManager) Outbound(tag string) (adapter.Outbound, bool) {
	for _, o := range m.all {
		if o.Tag() == tag {
			return o, true
		}
	}
	return nil, false
}
func cancellationFixture(dial func(context.Context) (net.Conn, error)) (*Server, *cancellationOutbound) {
	o := &cancellationOutbound{Adapter: O.NewAdapter("test", "leaf", []string{"tcp"}, nil), dial: dial}
	return &Server{ctx: context.Background(), outbound: &cancellationManager{all: []adapter.Outbound{o}}, urlTestHistory: urltest.NewHistoryStorage()}, o
}
func TestCanceledManualDelayPreservesNewHistory(t *testing.T) {
	for _, owner := range []string{"request", "core"} {
		t.Run(owner, func(t *testing.T) {
			entered := make(chan struct{})
			release := make(chan struct{})
			s, o := cancellationFixture(func(ctx context.Context) (net.Conn, error) {
				close(entered)
				<-release
				return nil, errors.New("late failure")
			})
			core, stopCore := context.WithCancel(context.Background())
			defer stopCore()
			s.ctx = core
			request, stopRequest := context.WithCancel(context.Background())
			defer stopRequest()
			r := httptest.NewRequest("GET", "http://localhost/delay?timeout=500", nil).WithContext(context.WithValue(request, CtxKeyProxy, adapter.Outbound(o)))
			w := httptest.NewRecorder()
			done := make(chan struct{})
			go func() { getProxyDelay(s)(w, r); close(done) }()
			<-entered
			if owner == "request" {
				stopRequest()
			} else {
				stopCore()
			}
			successor := &adapter.URLTestHistory{Time: time.Now(), Delay: 73}
			s.urlTestHistory.StoreURLTestHistory(o.Tag(), successor)
			close(release)
			<-done
			if s.urlTestHistory.LoadURLTestHistory(o.Tag()) != successor {
				t.Fatal("canceled late result deleted successor history")
			}
			if w.Body.Len() != 0 {
				t.Fatalf("canceled owner emitted late response: %s", w.Body.String())
			}
		})
	}
}
func TestManualDelayCancellation(t *testing.T) {
	for _, owner := range []string{"request", "core"} {
		t.Run(owner, func(t *testing.T) {
			entered := make(chan context.Context, 1)
			s, o := cancellationFixture(func(ctx context.Context) (net.Conn, error) { entered <- ctx; <-ctx.Done(); return nil, ctx.Err() })
			core, stopCore := context.WithCancel(context.Background())
			defer stopCore()
			s.ctx = core
			request, stopRequest := context.WithCancel(context.Background())
			defer stopRequest()
			r := httptest.NewRequest("GET", "http://localhost/delay?url=https://example.invalid&timeout=500", nil).WithContext(context.WithValue(request, CtxKeyProxy, adapter.Outbound(o)))
			w := httptest.NewRecorder()
			done := make(chan struct{})
			go func() { getProxyDelay(s)(w, r); close(done) }()
			probe := <-entered
			if owner == "request" {
				stopRequest()
			} else {
				stopCore()
			}
			retired := false
			select {
			case <-probe.Done():
				retired = true
			case <-time.After(100 * time.Millisecond):
			}
			<-done
			if !retired {
				t.Fatal("manual probe ignored " + owner + " cancellation until independent 500ms timeout")
			}
		})
	}
}
