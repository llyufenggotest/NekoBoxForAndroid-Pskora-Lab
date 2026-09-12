package androidmonitor

import (
	"context"
	"fmt"
	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	tun "github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common/control"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/service"
	"golang.org/x/net/proxy"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Only the Android OS boundary is supplied by the harness. Monitor, network
// manager, default dialer, URLTest fallback, SOCKS inbound and HTTP are real.
type recoveryPlatform struct {
	adapter.PlatformInterface
	mu       sync.Mutex
	nm       adapter.NetworkManager
	monitor  *Monitor
	listener Listener
	online   bool
}

func (p *recoveryPlatform) Initialize(n adapter.NetworkManager) error { p.nm = n; return nil }
func (p *recoveryPlatform) UsePlatformDefaultInterfaceMonitor() bool  { return true }
func (p *recoveryPlatform) CreateDefaultInterfaceMonitor(l logger.Logger) tun.DefaultInterfaceMonitor {
	p.monitor = New(p, func() adapter.NetworkManager { return p.nm }, l)
	return p.monitor
}
func (p *recoveryPlatform) StartDefaultInterfaceMonitor(l Listener) error { p.listener = l; return nil }
func (p *recoveryPlatform) CloseDefaultInterfaceMonitor(l Listener) error { return nil }
func (p *recoveryPlatform) UsePlatformNetworkInterfaces() bool            { return true }
func (p *recoveryPlatform) NetworkInterfaces() ([]adapter.NetworkInterface, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.online {
		return nil, nil
	}
	return []adapter.NetworkInterface{{Interface: control.Interface{Name: "loopback-test", Index: 1, Flags: net.FlagUp, Addresses: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/8")}}}}, nil
}
func (p *recoveryPlatform) UsePlatformAutoDetectInterfaceControl() bool { return false }
func (p *recoveryPlatform) UsePlatformInterface() bool                  { return false }
func (p *recoveryPlatform) UsePlatformWIFIMonitor() bool                { return false }
func (p *recoveryPlatform) UsePlatformConnectionOwnerFinder() bool      { return false }
func (p *recoveryPlatform) UsePlatformNeighborResolver() bool           { return false }
func (p *recoveryPlatform) UsePlatformNotification() bool               { return false }
func (p *recoveryPlatform) UnderNetworkExtension() bool                 { return false }
func (p *recoveryPlatform) ClearDNSCache()                              {}
func (p *recoveryPlatform) MyInterfaceAddress() []netip.Addr            { return nil }
func (p *recoveryPlatform) event(up bool) {
	p.mu.Lock()
	p.online = up
	p.mu.Unlock()
	if up {
		p.listener.UpdateDefaultInterface("loopback-test", 1, false, false)
	} else {
		p.listener.UpdateDefaultInterface("", -1, false, false)
	}
}

func TestUnifiedReadinessRecovery(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "real-recovery-body") }))
	defer target.Close()
	badListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer badListener.Close()
	var badCalls atomic.Int32
	go func() {
		for {
			c, e := badListener.Accept()
			if e != nil {
				return
			}
			badCalls.Add(1)
			c.Close()
		}
	}()
	badPort := badListener.Addr().(*net.TCPAddr).Port
	reserve, _ := net.Listen("tcp", "127.0.0.1:0")
	port := reserve.Addr().(*net.TCPAddr).Port
	reserve.Close()
	p := &recoveryPlatform{}
	ctx, cancel := context.WithCancel(include.Context(context.Background()))
	defer cancel()
	ctx = service.ContextWithDefaultRegistry(ctx)
	ctx = service.ContextWith[adapter.PlatformInterface](ctx, p)
	config := fmt.Sprintf(`{"log":{"level":"trace"},"inbounds":[{"type":"mixed","tag":"in","listen":"127.0.0.1","listen_port":%d}],"outbounds":[{"type":"urltest","tag":"preferred","outbounds":["bad","good"],"url":%q,"interval":"10m","idle_timeout":"10m","dial_fallback":true,"fallback_mode":"stable"},{"type":"direct","tag":"good"}],"route":{"auto_detect_interface":true,"final":"preferred"}}`, port, target.URL)
	config = strings.Replace(config, `"outbounds":[{"type":"urltest"`, fmt.Sprintf(`"outbounds":[{"type":"socks","tag":"bad","server":"127.0.0.1","server_port":%d},{"type":"urltest","tag":"bad-only","outbounds":["bad"],"url":%q,"interval":"10m","idle_timeout":"10m","dial_fallback":true},{"type":"urltest"`, badPort, target.URL), 1)
	var o option.Options
	if err := o.UnmarshalJSONContext(ctx, []byte(config)); err != nil {
		t.Fatal(err)
	}
	b, err := box.New(box.Options{Context: ctx, Options: o})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	start := time.Now()
	if err = b.Start(); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("offline Start blocked")
	}
	if p.monitor.DefaultInterface() != nil {
		t.Fatal("initial state not offline")
	}
	s, err := proxy.SOCKS5("tcp", fmt.Sprintf("127.0.0.1:%d", port), nil, &net.Dialer{Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	tr := &http.Transport{DisableKeepAlives: true, DialContext: s.(proxy.ContextDialer).DialContext}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 7 * time.Second}
	fetch := func() error {
		r, e := client.Get(target.URL)
		if e != nil {
			return e
		}
		defer r.Body.Close()
		v, e := io.ReadAll(r.Body)
		if e == nil && string(v) != "real-recovery-body" {
			return fmt.Errorf("body %q", v)
		}
		return e
	}
	pending := make(chan error, 1)
	start = time.Now()
	go func() { pending <- fetch() }()
	select {
	case e := <-pending:
		t.Fatalf("request did not await delayed interface: %v", e)
	case <-time.After(250 * time.Millisecond):
	}
	p.event(true)
	select {
	case e := <-pending:
		if e != nil {
			t.Fatal("same request failed after event:", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("recovery stuck")
	}
	t.Logf("same SOCKS+HTTP request recovered through production monitor/dialer/fallback in %v", time.Since(start))
	bad, _ := service.FromContext[adapter.OutboundManager](ctx).Outbound("bad-only")
	until := time.Now().Add(2 * time.Second)
	for {
		c, e := bad.DialContext(ctx, "tcp", M.ParseSocksaddr(target.Listener.Addr().String()))
		if c != nil {
			c.Close()
		}
		if e != nil && strings.Contains(e.Error(), "cooling down") {
			break
		}
		if time.Now().After(until) {
			t.Fatal("real failed SOCKS node did not cool", e)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if badCalls.Load() == 0 {
		t.Fatal("no real bad-node network traffic")
	}
	before := badCalls.Load()
	for i := 0; i < 3; i++ {
		_, e := bad.DialContext(ctx, "tcp", M.ParseSocksaddr(target.Listener.Addr().String()))
		if e == nil || !strings.Contains(e.Error(), "cooling down") {
			t.Fatal("bad node not cooling", e)
		}
	}
	if badCalls.Load() != before {
		t.Fatal("cooldown redialed bad server")
	}
	t.Logf("real bad SOCKS handshake cooled; %d accepts, next 3 dials excluded", before)
	p.event(false)
	start = time.Now()
	if e := fetch(); e == nil {
		t.Fatal("offline request succeeded")
	}
	elapsed := time.Since(start)
	if elapsed > 6*time.Second {
		t.Fatal("offline wait unbounded", elapsed)
	}
	t.Logf("offline request bounded: %v", elapsed)
	p.event(true)
	start = time.Now()
	if e := fetch(); e != nil {
		t.Fatal("local failure caused cooldown:", e)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("recovery waited cooldown")
	}
	t.Logf("subsequent request recovered without health action/cooldown: %v", time.Since(start))
}
