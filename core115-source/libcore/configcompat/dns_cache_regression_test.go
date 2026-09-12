package configcompat

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/miekg/dns"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing/service"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestRcodeDisableCacheValidation(t *testing.T) {
	for _, value := range []string{"true", "false", "\"true\"", "null", "1"} {
		raw := []byte(fmt.Sprintf(`{"dns":{"servers":[{"tag":"block","address":"rcode://success"},{"tag":"fake","address":"fakeip"}],"fakeip":{"enabled":true,"inet4_range":"198.18.0.0/15"},"rules":[{"domain":"ads.invalid","server":"block","disable_cache":%s},{"server":"fake","disable_cache":true}]}}`, value))
		_, data, err := ConvertContext(include.Context(context.Background()), raw, nil, nil)
		valid := value == "true" || value == "false"
		if !valid {
			if err == nil {
				t.Fatalf("accepted invalid cache flag %s", value)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		var r map[string]any
		json.Unmarshal(data, &r)
		rules := r["dns"].(map[string]any)["rules"].([]any)
		if _, exists := rules[0].(map[string]any)["disable_cache"]; exists {
			t.Fatal("predefined retained route-only flag")
		}
		if rules[1].(map[string]any)["disable_cache"] != true {
			t.Fatal("fakeip cache flag removed")
		}
	}
}

func TestRcodeAndRouteCacheExchange(t *testing.T) {
	var cached, uncached atomic.Int32
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &dns.Server{PacketConn: pc, Handler: dns.HandlerFunc(func(w dns.ResponseWriter, q *dns.Msg) {
		if q.Question[0].Name == "cache.invalid." {
			cached.Add(1)
		} else {
			uncached.Add(1)
		}
		time.Sleep(100 * time.Millisecond)
		m := new(dns.Msg)
		m.SetReply(q)
		rr, _ := dns.NewRR(q.Question[0].Name + " 600 IN A 192.0.2.8")
		m.Answer = []dns.RR{rr}
		w.WriteMsg(m)
	})}
	go server.ActivateAndServe()
	t.Cleanup(func() { server.Shutdown() })
	for _, flag := range []bool{true, false} {
		t.Run(fmt.Sprint(flag), func(t *testing.T) {
			b, ctx := startLegacyContext(t, fmt.Sprintf(`{"dns":{"independent_cache":true,"servers":[{"tag":"up","address":"udp://%s"},{"tag":"block","address":"rcode://success"}],"rules":[{"domain":"ads.invalid","server":"block","disable_cache":%t},{"domain":"nocache.invalid","server":"up","disable_cache":true}],"final":"up"},"outbounds":[{"type":"direct"}]}`, pc.LocalAddr(), flag))
			_ = b
			router := service.FromContext[adapter.DNSRouter](ctx)
			beforeCached, beforeUncached := cached.Load(), uncached.Load()
			for i := 0; i < 2; i++ {
				for _, name := range []string{"ads.invalid.", "cache.invalid.", "nocache.invalid."} {
					q := new(dns.Msg)
					q.SetQuestion(name, dns.TypeA)
					m, err := router.Exchange(ctx, q, adapter.DNSQueryOptions{})
					if err != nil {
						t.Fatal(err)
					}
					if m.Rcode != dns.RcodeSuccess {
						t.Fatal(m)
					}
					if name == "ads.invalid." && len(m.Answer) != 0 {
						t.Fatal("rcode response acquired answer")
					}
				}
			}
			if cached.Load()-beforeCached != 1 || uncached.Load()-beforeUncached != 2 {
				t.Fatalf("cache semantics changed: cached=%d uncached=%d", cached.Load()-beforeCached, uncached.Load()-beforeUncached)
			}
		})
	}
}
