package boxapi

import (
	"net/netip"
	"testing"
)

func TestProxyHTTPNetworkSelection(t *testing.T) {
	if got := proxyHTTPNetwork("tcp", "tcp4"); got != "tcp4" {
		t.Fatalf("forced network = %q, want tcp4", got)
	}
	if got := proxyHTTPNetwork("tcp", ""); got != "tcp" {
		t.Fatalf("ordinary network = %q, want tcp", got)
	}
}

func TestFirstIPv4Address(t *testing.T) {
	addresses := []netip.Addr{
		netip.MustParseAddr("2001:db8::1"),
		netip.MustParseAddr("203.0.113.7"),
	}
	if got := firstIPv4Address(addresses); got.String() != "203.0.113.7" {
		t.Fatalf("IPv4 selection = %v", got)
	}
	if got := firstIPv4Address(addresses[:1]); got.IsValid() {
		t.Fatalf("unexpected IPv4 selection = %v", got)
	}
}
