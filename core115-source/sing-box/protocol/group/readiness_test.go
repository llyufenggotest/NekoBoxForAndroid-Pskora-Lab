package group

import (
	"context"
	"errors"
	"github.com/sagernet/sing-box/common/dialer"
	M "github.com/sagernet/sing/common/metadata"
	"net"
	"testing"
)

func TestLocalReadinessDoesNotCoolCandidates(t *testing.T) {
	s, all := fixture(t, 3)
	enableFallback(s)
	for _, o := range all {
		o.dial = func(context.Context) (net.Conn, error) { return nil, dialer.ErrNoAvailableInterface }
	}
	_, err := s.DialContext(context.Background(), "tcp", M.Socksaddr{})
	if !errors.Is(err, dialer.ErrNoAvailableInterface) {
		t.Fatal(err)
	}
	for _, state := range s.group.fallback.nodes {
		if !state.retryAfter.IsZero() {
			t.Fatal("local interface failure polluted remote cooldown")
		}
	}
	all[0].dial = func(context.Context) (net.Conn, error) { a, b := net.Pipe(); b.Close(); return a, nil }
	c, err := s.DialContext(context.Background(), "tcp", M.Socksaddr{})
	if err != nil {
		t.Fatalf("network recovery waited for cooldown: %v", err)
	}
	c.Close()
}
