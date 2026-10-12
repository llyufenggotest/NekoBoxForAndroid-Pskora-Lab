package include

import (
	"context"
	"testing"

	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/xhttp"
	"github.com/sagernet/sing/common/json"
	"github.com/stretchr/testify/require"
)

func TestBlackstoneCanonicalLegacyRegistrationAndValidation(t *testing.T) {
	registry := OutboundRegistry()
	for _, name := range []string{"Blackstone", "xhttp"} {
		_, ok := registry.CreateOptions(name)
		require.True(t, ok, name)
	}
	_, lower := registry.CreateOptions("blackstone")
	require.False(t, lower)

	ctx := Context(context.Background())
	canonical := `{"type":"Blackstone","server":"127.0.0.1","server_port":443,"password":"fixture","node-id":"1","device_id":"device-fixture","device_name":"tablet-fixture"}`
	var outbound option.Outbound
	require.NoError(t, json.UnmarshalContext(ctx, []byte(canonical), &outbound))
	options := outbound.Options.(*option.BlackstoneOutboundOptions)
	require.Equal(t, "device-fixture", options.DeviceID)
	require.Equal(t, "tablet-fixture", options.DeviceName)
	created, err := xhttp.NewOutbound(context.Background(), nil, nil, "fixture", options.XHttpOutboundOptions)
	require.NoError(t, err)
	require.Equal(t, "xhttp", created.Type())
	created, err = registry.CreateOutbound(context.Background(), nil, nil, "fixture", "Blackstone", options)
	require.NoError(t, err)
	require.Equal(t, "Blackstone", created.Type())

	invalid := []string{
		`{"type":"Blackstone","server":"127.0.0.1","server_port":443,"password":"fixture","node-id":"1"}`,
		`{"type":"Blackstone","server":"127.0.0.1","server_port":443,"password":"fixture","node-id":"1","device_id":"device-fixture","device_name":" "}`,
		`{"type":"xhttp","server":"127.0.0.1","server_port":443,"password":"fixture","node-id":"1","device_id":"device-fixture"}`,
	}
	for _, raw := range invalid {
		var value option.Outbound
		require.Error(t, json.UnmarshalContext(ctx, []byte(raw), &value), raw)
	}
}
