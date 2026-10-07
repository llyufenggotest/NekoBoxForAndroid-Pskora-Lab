package snell

import (
	"context"
	"testing"

	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json"
	"github.com/stretchr/testify/require"
)

func TestSnellOIXAppConstruct(t *testing.T) {
	for _, tc := range []struct{ name, fields, wantError string }{
		{"old profile", "", ""},
		{"identity enabled", `,"oix_identity":true`, ""},
		{"identity explicitly disabled", `,"oix_identity":false`, "identity cannot be disabled"},
		{"certificate verification bypass", `,"oix_skip_cert_verify":true`, "requires certificate verification"},
		{"raw path metadata", `,"oix_identity":true,"oix_path":"/not-a-websocket","oix_skip_cert_verify":false`, ""},
		{"legacy still rejected", `,"oix_legacy_fallback":true`, "legacy fallback is unsupported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var options option.SnellOutboundOptions
			require.NoError(t, json.Unmarshal([]byte(`{"version":4,"server":"127.0.0.1","server_port":443,"psk":"synthetic","obfs_mode":"oix-ech-tls","oix_ech":true,"oix_identity_version":2,"oix_alpn":"snell-ech/1","oix_sni":"front.example","oix_config":"AQID"`+tc.fields+`}`), &options))
			created, err := NewOutbound(context.Background(), nil, log.NewNOPFactory().NewLogger("snell"), "test", options)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				require.Nil(t, created)
				return
			}
			require.NoError(t, err)
			ob := created.(*Outbound)
			d, ok := ob.tcpDialer.(*oixECHDialer)
			require.True(t, ok, "path must not switch raw ECH-TLS to HTTP/WS")
			require.Equal(t, "front.example", d.config.ServerName())
			require.NoError(t, ob.Close())
		})
	}
}

func TestSnellOIXAppMetadataRequiresTransport(t *testing.T) {
	for _, fields := range []string{`,"oix_identity":false`, `,"oix_identity":true`, `,"oix_path":"/metadata"`, `,"oix_skip_cert_verify":true`} {
		var options option.SnellOutboundOptions
		require.NoError(t, json.Unmarshal([]byte(`{"version":4,"psk":"synthetic"`+fields+`}`), &options))
		require.ErrorContains(t, validateSnellOIXOptions(4, options.ObfsOptions), "OIX options require")
	}
}

func TestSnellOIXUnsafeFieldsRejectAtTransportBoundary(t *testing.T) {
	for _, fields := range []string{`,"oix_identity":false`, `,"oix_skip_cert_verify":true`} {
		var options option.SnellOutboundOptions
		require.NoError(t, json.Unmarshal([]byte(`{"version":4,"psk":"synthetic","obfs_mode":"oix-ech-tls","oix_ech":true,"oix_identity_version":2,"oix_alpn":"snell-ech/1","oix_sni":"front.example","oix_config":"AQID"`+fields+`}`), &options))
		_, err := buildSnellOutboundTransport(context.Background(), log.NewNOPFactory().NewLogger("snell"), nil, options.ServerOptions.Build(), options.ObfsOptions)
		require.Error(t, err)
	}
}
