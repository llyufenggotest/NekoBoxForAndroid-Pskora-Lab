package libcore

import (
	"context"
	mDNS "github.com/miekg/dns"
)

// Android resolver calls are per request and own no persistent connections.
func (p *platformLocalDNSTransport) Reset() {}
func (p *platformLocalDNSTransport) ExchangeAsync(ctx context.Context, m *mDNS.Msg, cb func(*mDNS.Msg, error)) {
	go func() { r, e := p.Exchange(ctx, m); cb(r, e) }()
}
