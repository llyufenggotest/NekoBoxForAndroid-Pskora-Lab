package group

import (
	"github.com/sagernet/sing-box/adapter"
	N "github.com/sagernet/sing/common/network"
)

func (g *URLTestGroup) selectedForNetwork(network string) adapter.Outbound {
	g.selectionAccess.RLock()
	defer g.selectionAccess.RUnlock()
	if network == N.NetworkTCP {
		return g.selectedOutboundTCP
	}
	return g.selectedOutboundUDP
}

// RuntimeSelection is read-only. Fallback reports the most recent successful TCP
// dial, not a claim that all established connections use the same leaf.
func (s *URLTest) RuntimeSelection() (tcp, udp, semantics string) {
	if s.group == nil {
		return "", "", "pending"
	}
	g := s.group
	if s.dialFallback {
		g.fallback.mu.Lock()
		tcp = g.fallback.lastTCPSuccess
		g.fallback.mu.Unlock()
		return tcp, "", "recent_tcp_success"
	}
	g.selectionAccess.RLock()
	defer g.selectionAccess.RUnlock()
	if g.selectedOutboundTCP != nil {
		tcp = g.selectedOutboundTCP.Tag()
	}
	if g.selectedOutboundUDP != nil {
		udp = g.selectedOutboundUDP.Tag()
	}
	return tcp, udp, "current_tcp_selection"
}
