package configcompat

import (
	"context"
	"fmt"
	"github.com/miekg/dns"
	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/service"
	"strings"
	"testing"
)

var nestedActions = []string{
	`"action":"reject","method":"default","no_drop":true`,
	`"action":"reject","method":"drop"`,
	`"action":"route-options","strategy":"ipv4_only","disable_cache":true,"rewrite_ttl":7,"client_subnet":"192.0.2.0/24"`,
	`"action":"predefined","rcode":"NOERROR","answer":"hit.invalid. 60 IN A 192.0.2.7","ns":"invalid. 60 IN NS ns.invalid.","extra":"ns.invalid. 60 IN A 192.0.2.8"`,
}

func nestedConfig(action string, logical bool) string {
	child := fmt.Sprintf(`{"domain":"hit.invalid",%s}`, action)
	if logical {
		child = fmt.Sprintf(`{"type":"logical","mode":"or","rules":[{"domain":"hit.invalid"},{"domain":"other.invalid"}],%s}`, action)
	}
	return fmt.Sprintf(`{"dns":{"servers":[{"tag":"fallback","address":"local"}],"rules":[{"type":"logical","mode":"and","rules":[%s,{"query_type":["A","AAAA"]},{"domain":"excluded.invalid","invert":true}],"action":"predefined","rcode":"REFUSED"},{"action":"predefined","rcode":"NXDOMAIN"}],"final":"fallback"},"outbounds":[{"type":"direct"}]}`, child)
}
func TestNestedNonRouteExchange(t *testing.T) {
	for i, action := range nestedActions {
		for _, logical := range []bool{false, true} {
			t.Run(fmt.Sprintf("action%d/logical%v", i, logical), func(t *testing.T) {
				_, ctx := startLegacyContext(t, nestedConfig(action, logical))
				for _, tc := range []struct {
					name string
					typ  uint16
					code int
				}{{"hit.invalid", dns.TypeA, 5}, {"hit.invalid", dns.TypeAAAA, 5}, {"miss.invalid", dns.TypeA, 3}, {"hit.invalid", dns.TypeMX, 3}, {"excluded.invalid", dns.TypeA, 3}} {
					q := new(dns.Msg)
					q.SetQuestion(tc.name+".", tc.typ)
					r, e := service.FromContext[adapter.DNSRouter](ctx).Exchange(ctx, q, adapter.DNSQueryOptions{})
					if e != nil {
						t.Fatal(e)
					}
					if r.Rcode != tc.code || len(r.Answer) != 0 {
						t.Fatalf("%s/%d: %v", tc.name, tc.typ, r)
					}
					t.Logf("%s/%d rcode=%d answers=%d", tc.name, tc.typ, r.Rcode, len(r.Answer))
				}
			})
		}
	}
}
func TestNestedNonRouteBoundaries(t *testing.T) {
	for _, a := range []string{`"action":"unknown"`, `"action":"reject","method":"reply"`, `"action":"reject","method":"invalid"`, `"action":"reject","method":"drop","no_drop":true`, `"action":"reject","server":"x"`, `"action":"route-options"`, `"action":"route-options","rewrite_ttl":-1`, `"action":"predefined","rcode":"INVALID"`, `"action":"predefined","answer":"not a DNS record"`, `"action":"predefined","unknown":true`} {
		if raw, e := Convert([]byte(nestedConfig(a, false)), nil, nil); e == nil {
			var o option.Options
			if e = o.UnmarshalJSONContext(include.Context(context.Background()), raw); e == nil {
				t.Fatalf("accepted %s", a)
			}
		}
	}
	for _, a := range nestedActions {
		raw := strings.Replace(nestedConfig(a, false), `"address":"local"`, `"type":"local"`, 1)
		converted, e := Convert([]byte(raw), nil, nil)
		if e != nil {
			t.Fatal(e)
		}
		if !strings.Contains(string(converted), `"action":"`+strings.Split(strings.TrimPrefix(a, `"action":"`), `"`)[0]+`"`) {
			t.Fatal("typed action removed")
		}
		ctx := service.ContextWithDefaultRegistry(include.Context(context.Background()))
		var o option.Options
		if e = o.UnmarshalJSONContext(ctx, converted); e == nil {
			var b *box.Box
			b, e = box.New(box.Options{Context: ctx, Options: o})
			if b != nil {
				b.Close()
			}
		}
		if e == nil {
			t.Fatal("typed nested action accepted")
		}
		t.Log(e)
	}
}
