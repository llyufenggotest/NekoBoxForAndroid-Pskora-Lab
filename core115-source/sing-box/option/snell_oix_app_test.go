package option

import (
	"github.com/sagernet/sing/common/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSnellOIXAppFieldsRoundTrip(t *testing.T) {
	for _, identity := range []string{"true", "false"} {
		t.Run(identity, func(t *testing.T) {
			var options SnellOutboundOptions
			require.NoError(t, json.Unmarshal([]byte(`{"version":4,"server":"127.0.0.1","server_port":443,"psk":"synthetic","obfs_mode":"oix-ech-tls","oix_ech":true,"oix_identity_version":2,"oix_alpn":"snell-ech/1","oix_sni":"front.example","oix_config":"AQID","oix_identity":`+identity+`,"oix_path":"/subscription-metadata","oix_skip_cert_verify":true,"tcp_fast_open":true}`), &options))
			encoded, err := json.Marshal(options)
			require.NoError(t, err)
			require.Contains(t, string(encoded), `"oix_identity":`+identity)
			require.Contains(t, string(encoded), `"oix_path":"/subscription-metadata"`)
			require.Contains(t, string(encoded), `"oix_skip_cert_verify":true`)
			require.True(t, options.TCPFastOpen)
		})
	}
}

func TestSnellOIXOldProfileOmitsIdentity(t *testing.T) {
	var options SnellOutboundOptions
	require.NoError(t, json.Unmarshal([]byte(`{"version":4,"server":"127.0.0.1","server_port":443,"psk":"synthetic","obfs_mode":"oix-ech-tls","oix_ech":true}`), &options))
	encoded, err := json.Marshal(options)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), `"oix_identity":`)
}
