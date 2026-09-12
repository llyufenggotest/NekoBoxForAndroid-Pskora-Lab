package configcompat

import (
	"context"
	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/service"
	"testing"
)

func startLegacy(t *testing.T, config string) *box.Box {
	b, _ := startLegacyContext(t, config)
	return b
}
func startLegacyContext(t *testing.T, config string) (*box.Box, context.Context) {
	t.Helper()
	ctx, data, err := ConvertContext(include.Context(context.Background()), []byte(config), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx = service.ContextWithDefaultRegistry(ctx)
	var o option.Options
	if err = o.UnmarshalJSONContext(ctx, data); err != nil {
		t.Fatalf("decode: %v; %s", err, data)
	}
	b, err := box.New(box.Options{Context: ctx, Options: o})
	if err != nil {
		t.Fatalf("new: %v; %s", err, data)
	}
	t.Cleanup(func() { b.Close() })
	if err = b.Start(); err != nil {
		t.Fatalf("start: %v; %s", err, data)
	}
	return b, ctx
}
func TestLegacyMixedStart(t *testing.T) {
	startLegacy(t, `{"inbounds":[{"type":"mixed","tag":"mixed","listen":"127.0.0.1","listen_port":0,"sniff":true,"sniff_override_destination":true,"domain_strategy":"ipv4_only"}],"dns":{"servers":[{"address":"local","tag":"local"}]},"outbounds":[{"type":"direct","tag":"direct"}]}`)
}
func TestLegacyNodeResolverStart(t *testing.T) {
	startLegacy(t, `{"dns":{"servers":[{"tag":"direct-dns","address":"local"}],"rules":[{"outbound":["any"],"server":"direct-dns"}]},"outbounds":[{"type":"socks","tag":"proxy","server":"node.invalid","server_port":1080}],"route":{"final":"proxy"}}`)
}
func TestLegacyDNSStart(t *testing.T) {
	startLegacy(t, `{"dns":{"independent_cache":true,"servers":[{"tag":"block","address":"rcode://success"},{"tag":"local","address":"local","detour":"direct"},{"tag":"real","address":"udp://127.0.0.1:15353","address_resolver":"local","detour":"direct"}],"rules":[{"domain":["blocked.invalid"],"server":"block"}],"final":"real"},"outbounds":[{"type":"direct","tag":"direct"}],"route":{"final":"direct"}}`)
}
