package clashapi

import (
	"context"
	"errors"
	"net"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	O "github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/urltest"
	M "github.com/sagernet/sing/common/metadata"
)

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
