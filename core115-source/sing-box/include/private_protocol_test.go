package include

import (
	"context"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAnyTLSDetourSchemaAndRegistration(t *testing.T) {
	ctx := Context(context.Background())
	raw := `{"type":"anytls","tag":"sl-exit","server":"183.179.43.253","server_port":443,"password":"000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f#sl","detour":"sl-entry","tls":{"enabled":true,"server_name":"v-thumb.byteimg.com","insecure":true}}`
	var o option.Outbound
	require.NoError(t, json.UnmarshalContext(ctx, []byte(raw), &o))
	anyTLSOptions, ok := o.Options.(*option.AnyTLSOutboundOptions)
	require.True(t, ok)
	require.Equal(t, "sl-entry", anyTLSOptions.Detour)
	_, registered := OutboundRegistry().CreateOptions("anytls")
	require.True(t, registered)
}

func TestPrivateProtocolSchemaAndRegistration(t *testing.T) {
	ctx := Context(context.Background())
	for _, raw := range []string{
		`{"type":"xhttp","tag":"blackstone","server":"127.0.0.1","server_port":443,"password":"fixture","node-id":"1"}`,
		`{"type":"trojan","server":"127.0.0.1","server_port":443,"password":"fixture#fastup","mpw":"per-node"}`,
		`{"type":"vless","server":"127.0.0.1","server_port":443,"uuid":"00000000-0000-0000-0000-000000000001#juzi","transport":{"type":"xhttp","mode":"stream-up"},"tunnet":{"snapshot":"fixture.json"}}`,
		`{"type":"anytls","server":"127.0.0.1","server_port":443,"password":"000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f#sl","tls":{"enabled":true}}`,
		`{"type":"shadowsocksr","server":"127.0.0.1","server_port":443,"method":"aes-128-cfb","password":"fixture"}`,
	} {
		t.Run(raw, func(t *testing.T) {
			var o option.Outbound
			require.NoError(t, json.UnmarshalContext(ctx, []byte(raw), &o))
			_, ok := OutboundRegistry().CreateOptions(o.Type)
			require.True(t, ok)
			_, err := json.MarshalContext(ctx, o)
			require.NoError(t, err)
		})
	}
	for _, name := range []string{"snell", "anytls", "bridge", "vless", "trojan", "xhttp", "shadowsocksr"} {
		_, ok := OutboundRegistry().CreateOptions(name)
		require.True(t, ok, name)
	}
}
