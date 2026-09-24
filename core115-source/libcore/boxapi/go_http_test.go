package boxapi

import "testing"

func TestProxyHTTPNetworkSelection(t *testing.T) {
	if got := proxyHTTPNetwork("tcp", "tcp4"); got != "tcp4" {
		t.Fatalf("forced network = %q, want tcp4", got)
	}
	if got := proxyHTTPNetwork("tcp", ""); got != "tcp" {
		t.Fatalf("ordinary network = %q, want tcp", got)
	}
}
