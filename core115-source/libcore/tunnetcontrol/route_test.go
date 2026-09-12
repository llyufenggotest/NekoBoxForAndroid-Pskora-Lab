package tunnetcontrol

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fixtureProber map[string]struct {
	latency time.Duration
	err     error
}

func (p fixtureProber) Probe(_ context.Context, address string) (time.Duration, error) {
	value, found := p[address]
	if !found {
		return 0, errors.New("unreachable")
	}
	return value.latency, value.err
}

func TestSelectRouteIPChoosesFastestReachableValidatedIPv4(t *testing.T) {
	selected, err := SelectRouteIP(context.Background(), []string{"invalid", "203.0.113.20", "203.0.113.10", "203.0.113.20"}, fixtureProber{
		"203.0.113.20": {latency: 30 * time.Millisecond},
		"203.0.113.10": {latency: 10 * time.Millisecond},
	})
	if err != nil {
		t.Fatal(err)
	}
	if selected != "203.0.113.10" {
		t.Fatalf("unexpected route: %s", selected)
	}
}

func TestSelectRouteIPFailsClosedWithoutReachableCandidate(t *testing.T) {
	_, err := SelectRouteIP(context.Background(), []string{"203.0.113.10"}, fixtureProber{})
	if err == nil {
		t.Fatal("accepted unreachable route set")
	}
}
