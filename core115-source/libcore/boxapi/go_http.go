package boxapi

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"time"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing/common/metadata"
)

func CreateProxyHttpClient(box *box.Box, tracker adapter.ConnectionTracker) *http.Client {
	return createProxyHTTPClient(box, tracker, "")
}

func CreateProxyHttpClientIPv4(box *box.Box, tracker adapter.ConnectionTracker) *http.Client {
	return createProxyHTTPClient(box, tracker, "tcp4")
}

func proxyHTTPNetwork(network, forcedNetwork string) string {
	if forcedNetwork != "" {
		return forcedNetwork
	}
	return network
}

func firstIPv4Address(addresses []netip.Addr) netip.Addr {
	for _, address := range addresses {
		if address.Is4() {
			return address
		}
	}
	return netip.Addr{}
}

func createProxyHTTPClient(box *box.Box, tracker adapter.ConnectionTracker, forcedNetwork string) *http.Client {
	transport := &http.Transport{
		TLSHandshakeTimeout:   time.Second * 3,
		ResponseHeaderTimeout: time.Second * 3,
	}

	if box != nil {
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			destination := metadata.ParseSocksaddr(addr)
			if forcedNetwork == "tcp4" && destination.IsDomain() {
				dnsRouter := box.DNSRouter()
				if dnsRouter != nil {
					addresses, err := dnsRouter.Lookup(ctx, destination.Fqdn, adapter.DNSQueryOptions{
						LookupStrategy: C.DomainStrategyIPv4Only,
					})
					if err != nil {
						return nil, err
					}
					address := firstIPv4Address(addresses)
					if !address.IsValid() {
						return nil, &net.DNSError{Name: destination.Fqdn, Err: "no IPv4 address"}
					}
					destination = metadata.SocksaddrFrom(address, destination.Port)
				}
			}
			return dialSocksaddr(ctx, box, tracker, proxyHTTPNetwork(network, forcedNetwork), destination)
		}
	}

	return &http.Client{Transport: transport}
}
