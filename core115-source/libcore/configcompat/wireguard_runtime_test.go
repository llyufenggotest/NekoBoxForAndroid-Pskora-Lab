//go:build with_wireguard

package configcompat

import "testing"

func TestLegacyWireGuardStart(t *testing.T) {
	startLegacy(t, `{"outbounds":[{"type":"wireguard","tag":"wg","server":"127.0.0.1","server_port":51820,"local_address":["10.0.0.2/32"],"private_key":"AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=","peer_public_key":"AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=","reserved":[1,2,3],"mtu":1400},{"type":"direct","tag":"direct"}],"route":{"final":"direct"}}`)
	t.Log("userspace WireGuard endpoint started; no remote handshake claimed")
}
