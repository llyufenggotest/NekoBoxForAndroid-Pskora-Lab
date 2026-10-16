package xhttp

import (
	"strings"
	"testing"
)

func TestInitialAuthBound(t *testing.T) {
	// Downlink can publish at most u8 length plus 255 body bytes. Permit that
	// full opaque blob, but never allocate an unbounded uplink from config.
	for _, length := range []int{0, 1, 256} {
		if _, err := NewWireClient(WireConfig{Password: "synthetic:" + strings.Repeat("ab", length)}); err != nil {
			t.Fatalf("bounded opaque auth length %d rejected: %v", length, err)
		}
	}
	if _, err := NewWireClient(WireConfig{Password: "synthetic:" + strings.Repeat("ab", 257)}); err == nil {
		t.Fatal("oversized initial auth accepted")
	}
}
