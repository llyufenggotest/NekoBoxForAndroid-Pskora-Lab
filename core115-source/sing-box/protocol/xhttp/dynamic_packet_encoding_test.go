package xhttp

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestDynamicPacketEncodingDonorBranches(t *testing.T) {
	for _, tc := range []struct {
		encoding     BlackstonePacketEncoding
		packet, xudp bool
		want         string
		valid        bool
	}{
		{"", false, false, "xudp", true}, {"", false, true, "xudp", true}, {"none", false, false, "xudp", true}, {"xudp", false, false, "xudp", true},
		{"packet", false, false, "packetaddr", true}, {"packetaddr", false, false, "packetaddr", true}, {"", true, false, "packetaddr", true},
		{"", true, true, "", false}, {"xudp", true, false, "", false}, {"packet", false, true, "", false}, {"native", false, false, "", false},
	} {
		cfg := dynamicVLESSFixture()
		cfg.VLESS.PacketEncoding = tc.encoding
		cfg.VLESS.PacketAddr = tc.packet
		cfg.VLESS.XUDP = tc.xudp
		o, e := dynamicVLESSOptions(cfg)
		if !tc.valid {
			require.Error(t, e)
			continue
		}
		require.NoError(t, e)
		require.Equal(t, tc.want, *o.PacketEncoding)
	}
}
func TestDynamicPacketEncodingTypedJSON(t *testing.T) {
	for _, field := range []string{"packet-encoding", "packet-addr", "xudp"} {
		values := []any{nil, 1, "true"}
		if field == "packet-encoding" {
			values = []any{nil, 1, true, "native", "XUDP", " xudp"}
		}
		for _, value := range values {
			p := map[string]any{"type": "vless", "server": "127.0.0.1", "port": 443, "uuid": dynamicUUID, "e": true, "h": "0102", field: value}
			_, e := parseSelectedVLESS(p, "127.0.0.1", "443")
			require.Error(t, e, "field=%s", field)
		}
	}
}
