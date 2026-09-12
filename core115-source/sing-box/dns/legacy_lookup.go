package dns

import (
	"context"
	mdns "github.com/miekg/dns"
	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
	"net/netip"
)

// Buffered results keep cancelled loser goroutines independent of the caller's return.
func (c *Client) lookupPreferred(ctx context.Context, t adapter.DNSTransport, name string, strategy C.DomainStrategy, o adapter.DNSQueryOptions, check func(*mdns.Msg) bool) ([]netip.Addr, error) {
	return lookupPreferredFamilies(ctx, strategy, func(ctx context.Context, q uint16) ([]netip.Addr, error) {
		return c.lookupToExchange(ctx, t, name, q, o, check)
	})
}
func lookupPreferredFamilies(ctx context.Context, strategy C.DomainStrategy, lookup func(context.Context, uint16) ([]netip.Addr, error)) ([]netip.Addr, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type result struct {
		q uint16
		a []netip.Addr
		e error
	}
	results := make(chan result, 2)
	for _, q := range []uint16{mdns.TypeA, mdns.TypeAAAA} {
		go func(q uint16) { a, e := lookup(ctx, q); results <- result{q, a, e} }(q)
	}
	var a4, a6 []netip.Addr
	var last error
	for range 2 {
		select {
		case <-ctx.Done():
			return sortAddresses(a4, a6, strategy), ctx.Err()
		case r := <-results:
			if r.e != nil {
				last = r.e
			}
			if r.q == mdns.TypeA {
				a4 = r.a
			} else {
				a6 = r.a
			}
			if len(r.a) > 0 && ((r.q == mdns.TypeA && strategy == C.DomainStrategyPreferIPv4) || (r.q == mdns.TypeAAAA && strategy == C.DomainStrategyPreferIPv6)) {
				return sortAddresses(a4, a6, strategy), nil
			}
		}
	}
	if len(a4)+len(a6) > 0 {
		return sortAddresses(a4, a6, strategy), nil
	}
	return nil, last
}
