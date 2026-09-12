package configcompat

import (
	"context"
	"fmt"
	"github.com/miekg/dns"
	"golang.org/x/net/proxy"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

func TestAdapterDNSHTTPDataPath(t *testing.T) {
	var queries atomic.Int32
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &dns.Server{PacketConn: pc, Handler: dns.HandlerFunc(func(w dns.ResponseWriter, q *dns.Msg) {
		queries.Add(1)
		// Keep the first real UDP exchange in flight across monitor initialization.
		time.Sleep(100 * time.Millisecond)
		m := new(dns.Msg)
		m.SetReply(q)
		if q.Question[0].Qtype == dns.TypeA {
			rr, _ := dns.NewRR(q.Question[0].Name + " 60 IN A 127.0.0.1")
			m.Answer = []dns.RR{rr}
		}
		w.WriteMsg(m)
	})}
	go srv.ActivateAndServe()
	t.Cleanup(func() { srv.Shutdown() })
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "adapter-real-http") }))
	defer target.Close()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	config := fmt.Sprintf(`{"log":{"level":"trace"},"inbounds":[{"type":"mixed","tag":"mixed","listen":"127.0.0.1","listen_port":%d,"domain_strategy":"ipv4_only","sniff":true,"sniff_override_destination":true}],"dns":{"independent_cache":true,"servers":[{"tag":"block","address":"rcode://success"},{"tag":"direct-dns","address":"udp://%s","detour":"direct","strategy":"ipv4_only"},{"tag":"hosts","type":"hosts","predefined":{"hosts.invalid":["127.0.0.1"]}}],"rules":[{"outbound":["any"],"server":"direct-dns"},{"domain":["blocked.invalid"],"server":"block"},{"server":"hosts","ip_accept_any":true}],"final":"direct-dns"},"outbounds":[{"type":"direct","tag":"direct"}],"route":{"final":"direct","concurrent_dial":true}}`, port, pc.LocalAddr())
	startLegacy(t, config)
	_, targetPort, _ := net.SplitHostPort(target.Listener.Addr().String())
	socks, err := proxy.SOCKS5("tcp", fmt.Sprintf("127.0.0.1:%d", port), nil, &net.Dialer{Timeout: time.Second * 3})
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, n, a string) (net.Conn, error) {
		return socks.(proxy.ContextDialer).DialContext(ctx, n, a)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second * 5}
	for _, host := range []string{"resolved.invalid", "hosts.invalid"} {
		resp, err := client.Get("http://" + net.JoinHostPort(host, targetPort))
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(body) != "adapter-real-http" {
			t.Fatal(string(body))
		}
	}
	if queries.Load() == 0 {
		t.Fatal("no local DNS exchange")
	}
	pu, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	ht := &http.Transport{Proxy: http.ProxyURL(pu)}
	defer ht.CloseIdleConnections()
	hc := &http.Client{Transport: ht, Timeout: time.Second * 5}
	resp, err := hc.Get("http://resolved.invalid:" + targetPort)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "adapter-real-http" {
		t.Fatal(string(body))
	}
	t.Logf("real adapter -> box.New/Start -> SOCKS5+HTTP mixed inbound -> local DNS+HTTP OK (%d DNS queries)", queries.Load())
}
