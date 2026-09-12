package include

import (
	"context"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPrivateProtocolSchemaAndRegistration(t *testing.T) {
	ctx := Context(context.Background())
	for _, raw := range []string{
		`{"type":"xhttp","tag":"blackstone","server":"127.0.0.1","server_port":443,"password":"fixture","node-id":"1"}`,
		`{"type":"trojan","server":"127.0.0.1","server_port":443,"password":"fixture#fastup","mpw":"per-node"}`,
		`{"type":"vless","server":"127.0.0.1","server_port":443,"uuid":"00000000-0000-0000-0000-000000000001#juzi","transport":{"type":"xhttp","mode":"stream-up"},"tunnet":{"snapshot":"fixture.json"}}`,
		`{"type":"vless","server":"127.0.0.1","server_port":443,"uuid":"00000000-0000-0000-0000-000000000001#sl","transport":{"type":"kcp"}}`,
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
