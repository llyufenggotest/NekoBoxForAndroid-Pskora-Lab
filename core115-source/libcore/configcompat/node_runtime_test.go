package configcompat

import (
	"context"
	"fmt"
	"github.com/miekg/dns"
	M "github.com/sagernet/sing/common/metadata"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestNodeDomainDNSRouting(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var hits atomic.Int32
	srv := &dns.Server{PacketConn: pc, Handler: dns.HandlerFunc(func(w dns.ResponseWriter, q *dns.Msg) {
		hits.Add(1)
		r := new(dns.Msg)
		r.SetReply(q)
		if q.Question[0].Qtype == dns.TypeA {
			r.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: q.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60}, A: net.IPv4(127, 0, 0, 1)}}
		}
		w.WriteMsg(r)
	})}
	go srv.ActivateAndServe()
	defer srv.Shutdown()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	received := make(chan bool, 1)
	go func() {
		c, e := l.Accept()
		if e == nil {
			received <- true
			c.Close()
		}
	}()
	cfg := fmt.Sprintf(`{"dns":{"servers":[{"address":"udp://%s","tag":"node-dns","detour":"direct","strategy":"ipv4_only"},{"type":"hosts","tag":"wrong","predefined":{}}],"rules":[{"outbound":["any"],"server":"node-dns"}],"final":"wrong"},"outbounds":[{"type":"socks","tag":"proxy","server":"node.invalid","server_port":%d},{"type":"direct","tag":"direct"}],"route":{"final":"proxy"}}`, pc.LocalAddr(), l.Addr().(*net.TCPAddr).Port)
	b := startLegacy(t, cfg)
	out, _ := b.Outbound().Outbound("proxy")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, _ := out.DialContext(ctx, "tcp", M.ParseSocksaddr("127.0.0.1:80"))
	if c != nil {
		c.Close()
	}
	select {
	case <-received:
	case <-ctx.Done():
		t.Fatal("node DNS did not reach local SOCKS peer")
	}
	if hits.Load() == 0 {
		t.Fatal("outbound:any node DNS not used")
	}
}
