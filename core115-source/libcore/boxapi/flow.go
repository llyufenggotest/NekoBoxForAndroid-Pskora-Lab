package boxapi

import (
	"context"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-tun"
	"sync/atomic"
)

func (s *SbStatsService) RoutedFlow(ctx context.Context, metadata adapter.InboundContext, matchedRule adapter.Rule, matchOutbound adapter.Outbound) tun.FlowTracker {
	inbound := metadata.Inbound
	user := metadata.User
	outbound := matchOutbound.Tag()
	var uplinkCounter []*atomic.Int64
	var downlinkCounter []*atomic.Int64
	countInbound := inbound != "" && s.inbounds[inbound]
	countOutbound := outbound != "" && s.outbounds[outbound]
	countUser := user != "" && s.users[user]
	if !countInbound && !countOutbound && !countUser {
		return nil
	}
	s.access.Lock()
	if countInbound {
		uplinkCounter = append(uplinkCounter, s.loadOrCreateCounter("inbound>>>"+inbound+">>>traffic>>>uplink"))
		downlinkCounter = append(downlinkCounter, s.loadOrCreateCounter("inbound>>>"+inbound+">>>traffic>>>downlink"))
	}
	if countOutbound {
		uplinkCounter = append(uplinkCounter, s.loadOrCreateCounter("outbound>>>"+outbound+">>>traffic>>>uplink"))
		downlinkCounter = append(downlinkCounter, s.loadOrCreateCounter("outbound>>>"+outbound+">>>traffic>>>downlink"))
	}
	if countUser {
		uplinkCounter = append(uplinkCounter, s.loadOrCreateCounter("user>>>"+user+">>>traffic>>>uplink"))
		downlinkCounter = append(downlinkCounter, s.loadOrCreateCounter("user>>>"+user+">>>traffic>>>downlink"))
	}
	s.access.Unlock()
	return &statsFlowTracker{uplinkCounter: uplinkCounter, downlinkCounter: downlinkCounter}
}

var _ tun.FlowTracker = (*statsFlowTracker)(nil)

type statsFlowTracker struct {
	uplinkCounter   []*atomic.Int64
	downlinkCounter []*atomic.Int64
}

func (t *statsFlowTracker) AttachFlow(handle tun.FlowHandle) {
}

func (t *statsFlowTracker) CountForward(n int) {
	for _, counter := range t.uplinkCounter {
		counter.Add(int64(n))
	}
}

func (t *statsFlowTracker) CountReverse(n int) {
	for _, counter := range t.downlinkCounter {
		counter.Add(int64(n))
	}
}

func (t *statsFlowTracker) FlowEstablished() {
}

func (t *statsFlowTracker) CloseFlow(reason tun.FlowCloseReason) {
}
