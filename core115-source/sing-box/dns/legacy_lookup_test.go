package dns

import (
	"context"
	mdns "github.com/miekg/dns"
	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
	"net/netip"
	"testing"
	"time"
)

type preferredTransport struct{ fakeDNSTransport }

func (t *preferredTransport) Exchange(ctx context.Context, m *mdns.Msg) (*mdns.Msg, error) {
	if m.Question[0].Qtype == mdns.TypeAAAA {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return FixedResponse(m.Id, m.Question[0], []netip.Addr{netip.MustParseAddr("192.0.2.1")}, 30), nil
}
func TestLegacyPreferredLookupReturnsEarly(t *testing.T) {
	c := &Client{disableCache: true, timeout: 2 * time.Second}
	start := time.Now()
	a, e := c.Lookup(context.Background(), &preferredTransport{}, "example.org", adapter.DNSQueryOptions{Strategy: C.DomainStrategyPreferIPv4}, nil)
	if e != nil || len(a) != 1 {
		t.Fatal(a, e)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("preferred A waited for AAAA")
	}
}
