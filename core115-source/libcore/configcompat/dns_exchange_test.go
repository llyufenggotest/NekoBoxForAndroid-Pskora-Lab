package configcompat

import (
	"github.com/miekg/dns"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/service"
	"net/netip"
	"testing"
)

func TestLegacyFakeIPAndRcodeExchange(t *testing.T) {
	_, ctx := startLegacyContext(t, `{"dns":{"servers":[{"tag":"block","address":"rcode://success"},{"tag":"real","address":"local"},{"tag":"fake","address":"fakeip","strategy":"ipv4_only"}],"fakeip":{"enabled":true,"inet4_range":"198.18.0.0/15","inet6_range":"fc00::/18"},"rules":[{"domain":["blocked.invalid"],"server":"block"},{"domain":["fake.invalid"],"query_type":["A","AAAA"],"server":"fake","disable_cache":true}],"final":"real"},"outbounds":[{"type":"direct","tag":"direct"}]}`)
	router := service.FromContext[adapter.DNSRouter](ctx)
	for _, tc := range []struct {
		name string
		typ  uint16
	}{{"blocked.invalid", dns.TypeA}, {"fake.invalid", dns.TypeA}, {"fake.invalid", dns.TypeAAAA}} {
		q := new(dns.Msg)
		q.SetQuestion(tc.name+".", tc.typ)
		resp, err := router.Exchange(ctx, q, adapter.DNSQueryOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if resp.Rcode != dns.RcodeSuccess {
			t.Fatal(resp)
		}
		if tc.name == "blocked.invalid" || tc.typ == dns.TypeAAAA {
			if len(resp.Answer) != 0 {
				t.Fatalf("expected empty NOERROR: %v", resp)
			}
		} else {
			if len(resp.Answer) != 1 {
				t.Fatal(resp)
			}
			ip := resp.Answer[0].(*dns.A).A.String()
			if !netip.MustParsePrefix("198.18.0.0/15").Contains(netip.MustParseAddr(ip)) {
				t.Fatal(ip)
			}
		}
	}
}
