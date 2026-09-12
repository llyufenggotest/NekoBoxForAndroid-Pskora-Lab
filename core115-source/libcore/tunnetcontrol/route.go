package tunnetcontrol

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"
)

type RouteProber interface {
	Probe(context.Context, string) (time.Duration, error)
}

type TCPRouteProber struct {
	Port    string
	Timeout time.Duration
}

func (p TCPRouteProber) Probe(ctx context.Context, address string) (time.Duration, error) {
	if net.ParseIP(address) == nil {
		return 0, errors.New("TunNet route candidate is not an IP address")
	}
	port := p.Port
	if port == "" {
		port = "443"
	}
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	probeContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	started := time.Now()
	connection, err := (&net.Dialer{}).DialContext(probeContext, "tcp", net.JoinHostPort(address, port))
	if err != nil {
		return 0, err
	}
	_ = connection.Close()
	return time.Since(started), nil
}

func SelectRouteIP(ctx context.Context, candidates []string, prober RouteProber) (string, error) {
	if prober == nil {
		return "", errors.New("missing TunNet route prober")
	}
	unique := make(map[string]struct{}, len(candidates))
	type result struct {
		address string
		latency time.Duration
		err     error
	}
	results := make(chan result, len(candidates))
	count := 0
	for _, candidate := range candidates {
		parsed := net.ParseIP(candidate)
		if parsed == nil || parsed.To4() == nil {
			continue
		}
		address := parsed.String()
		if _, exists := unique[address]; exists {
			continue
		}
		unique[address] = struct{}{}
		count++
		go func() {
			latency, err := prober.Probe(ctx, address)
			results <- result{address: address, latency: latency, err: err}
		}()
	}
	if count == 0 {
		return "", errors.New("TunNet entry has no valid IPv4 route candidates")
	}
	var selected string
	var selectedLatency time.Duration
	for index := 0; index < count; index++ {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case current := <-results:
			if current.err == nil && (selected == "" || current.latency < selectedLatency || current.latency == selectedLatency && current.address < selected) {
				selected = current.address
				selectedLatency = current.latency
			}
		}
	}
	if selected == "" {
		return "", fmt.Errorf("no reachable TunNet route among %d candidates", count)
	}
	return selected, nil
}
