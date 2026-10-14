package shadowsocks

import (
	"context"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/uot"
	"testing"
)

func TestGostAppBooleanConstructor(t *testing.T) {
	for _, mux := range []string{"false", "true", "0", "2"} {
		for _, useUoT := range []bool{false, true} {
			t.Run(mux+map[bool]string{false: "/tcp", true: "/uot"}[useUoT], func(t *testing.T) {
				outbound, err := NewOutbound(context.Background(), nil, log.NewNOPFactory().NewLogger("test"), "gost", option.ShadowsocksOutboundOptions{
					ServerOptions: option.ServerOptions{Server: "127.0.0.1", ServerPort: 443},
					Method:        "none", Plugin: "gost-plugin",
					PluginOptions: "mode=ws;host=front.invalid;path=/gost;mux=" + mux + ";tls=false",
					UDPOverTCP:    &option.UDPOverTCPOptions{Enabled: useUoT, Version: uot.Version},
				})
				if err != nil {
					t.Fatal(err)
				}
				ss := outbound.(*Outbound)
				defer ss.plugin.(interface{ Close() error }).Close()
				if (ss.uotClient != nil) != useUoT {
					t.Fatal("UoT layering changed")
				}
				if ss.multiplexDialer != nil {
					t.Fatal("gost must not create sing-mux")
				}
				if ss.plugin.(interface{ MultiplexEnabled() bool }).MultiplexEnabled() != (mux != "false") {
					t.Fatal("wrong gost mux state")
				}
			})
		}
	}
}
