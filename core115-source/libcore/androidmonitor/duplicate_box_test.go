package androidmonitor

import (
	"context"
	"fmt"
	"github.com/miekg/dns"
	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	tun "github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common/control"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/service"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// Mirrors the captured independent URLTest order: synchronous network snapshot,
// box.Start, outbound HTTP, node DNS pending, repeated identical notifications.
// A second live box remains open while the test box is constructed and closed.
type synchronousRecoveryPlatform struct{ recoveryPlatform }

func (p *synchronousRecoveryPlatform) CreateDefaultInterfaceMonitor(l logger.Logger) tun.DefaultInterfaceMonitor {
	p.monitor = New(p, func() adapter.NetworkManager { return p.nm }, l)
	return p.monitor
}
func (p *synchronousRecoveryPlatform) StartDefaultInterfaceMonitor(l Listener) error {
	p.listener = l
	p.event(true)
	return nil
}

func TestIndependentBoxDuplicateDuringRealDNS(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "independent-real-http") }))
	defer target.Close()
	entered := make(chan struct{}, 8)
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	dnsServer := &dns.Server{PacketConn: pc, Handler: dns.HandlerFunc(func(w dns.ResponseWriter, q *dns.Msg) {
		entered <- struct{}{}
		<-release
		answer := new(dns.Msg)
		answer.SetReply(q)
		for _, question := range q.Question {
			if question.Qtype == dns.TypeA {
				answer.Answer = append(answer.Answer, &dns.A{Hdr: dns.RR_Header{Name: question.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60}, A: net.IPv4(127, 0, 0, 1)})
			}
		}
		w.WriteMsg(answer)
	})}
	go dnsServer.ActivateAndServe()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
		dnsServer.Shutdown()
	}()
	create := func() (*box.Box, *synchronousRecoveryPlatform, context.Context) {
		p := &synchronousRecoveryPlatform{recoveryPlatform: recoveryPlatform{online: true}}
		ctx := service.ContextWithDefaultRegistry(include.Context(context.Background()))
		ctx = service.ContextWith[adapter.PlatformInterface](ctx, p)
		config := fmt.Sprintf(`{"dns":{"servers":[{"type":"udp","tag":"resolver","server":"127.0.0.1","server_port":%d}]},"outbounds":[{"type":"direct","tag":"ordinary"}],"route":{"auto_detect_interface":true,"default_domain_resolver":{"server":"resolver","strategy":"ipv4_only"}}}`, pc.LocalAddr().(*net.UDPAddr).Port)
		var options option.Options
		if e := options.UnmarshalJSONContext(ctx, []byte(config)); e != nil {
			t.Fatal(e)
		}
		b, e := box.New(box.Options{Context: ctx, Options: options})
		if e != nil {
			t.Fatal(e)
		}
		// Android StartDefaultInterfaceMonitor publishes synchronously in production.
		// Initialize stage installs the monitor; explicitly publish before full Start.
		if e = b.Start(); e != nil {
			t.Fatal(e)
		}
		p.event(true)
		return b, p, ctx
	}
	main, mainPlatform, _ := create()
	defer main.Close()
	testBox, p, ctx := create()
	defer testBox.Close()
	if mainPlatform.monitor.NetworkMonitorID() == p.monitor.NetworkMonitorID() {
		t.Fatal("box IDs collide")
	}
	outbound, _ := service.FromContext[adapter.OutboundManager](ctx).Outbound("ordinary")
	tr := &http.Transport{DialContext: func(c context.Context, n, a string) (net.Conn, error) {
		return outbound.DialContext(c, n, M.ParseSocksaddr(a))
	}}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 3 * time.Second}
	var callbacks atomic.Int32
	p.monitor.RegisterCallback(func(_ *control.Interface, _ int) { callbacks.Add(1) })
	result := make(chan error, 1)
	go func() {
		r, e := client.Get(fmt.Sprintf("http://health.test:%d", target.Listener.Addr().(*net.TCPAddr).Port))
		if e == nil {
			defer r.Body.Close()
			body, readErr := io.ReadAll(r.Body)
			e = readErr
			if e == nil && string(body) != "independent-real-http" {
				e = fmt.Errorf("wrong body %q", body)
			}
		}
		result <- e
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("real DNS request never arrived")
	}
	for i := 0; i < 3; i++ {
		p.event(true)
	}
	if callbacks.Load() != 0 {
		t.Fatalf("duplicate snapshot forwarded %d resets during independent box DNS", callbacks.Load())
	}
	close(release)
	select {
	case e := <-result:
		if e != nil {
			t.Fatal("independent HTTP after real DNS", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("HTTP recovery timeout")
	}
	if e := testBox.Close(); e != nil {
		t.Fatal(e)
	}
	if mainPlatform.monitor.DefaultInterface() == nil {
		t.Fatal("closing test box stopped main monitor")
	}
	t.Log("independent production box DNS + HTTP succeeds across duplicate snapshots; other box remains live")
}
