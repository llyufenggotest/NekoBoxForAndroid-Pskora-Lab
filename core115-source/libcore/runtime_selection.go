package libcore

import (
	"encoding/json"
	"github.com/sagernet/sing-box/protocol/group"
)

// RuntimeSelection reads this instance only; it does not enable Clash or probe.
// Holding access excludes teardown and use of a previous closed session.
func (b *BoxInstance) RuntimeSelection(tag string) string {
	b.access.Lock()
	defer b.access.Unlock()
	if b.state != 1 || b.Box == nil {
		return "{}"
	}
	outbound, ok := b.Outbound().Outbound(tag)
	if !ok {
		return "{}"
	}
	test, ok := outbound.(*group.URLTest)
	if !ok {
		return "{}"
	}
	tcp, udp, semantics := test.RuntimeSelection()
	value, _ := json.Marshal(map[string]any{"groupTag": tag, "tcpTag": tcp, "udpTag": udp, "semantics": semantics, "samples": test.RuntimeMemberHistory()})
	return string(value)
}
