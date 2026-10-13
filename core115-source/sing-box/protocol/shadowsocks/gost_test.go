package shadowsocks

import (
	"context"
	"testing"

	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/uot"
)

func TestGostPluginMuxAndUoTLayering(t *testing.T) {
	outbound, err := NewOutbound(context.Background(), nil, log.NewNOPFactory().NewLogger("test"), "gost", option.ShadowsocksOutboundOptions{
		ServerOptions: option.ServerOptions{Server: "127.0.0.1", ServerPort: 443},
		Method:        "none",
		Plugin:        "gost-plugin",
		PluginOptions: "mode=mws;host=front.invalid;path=/gost;mux=true",
		UDPOverTCP:    &option.UDPOverTCPOptions{Enabled: true, Version: uot.Version},
	})
	if err != nil {
		t.Fatal(err)
	}
	ssOutbound := outbound.(*Outbound)
	if ssOutbound.uotClient == nil {
		t.Fatal("UoT must remain above the Shadowsocks/plugin dialer")
	}
	if ssOutbound.multiplexDialer != nil {
		t.Fatal("gost smux must not enable sing-mux")
	}
	if !ssOutbound.plugin.(interface{ MultiplexEnabled() bool }).MultiplexEnabled() {
		t.Fatal("gost smux not enabled")
	}
}

func TestGostPluginMuxRejectsSingMux(t *testing.T) {
	_, err := NewOutbound(context.Background(), nil, log.NewNOPFactory().NewLogger("test"), "gost", option.ShadowsocksOutboundOptions{
		ServerOptions: option.ServerOptions{Server: "127.0.0.1", ServerPort: 443},
		Method:        "none",
		Plugin:        "gost-plugin",
		PluginOptions: "mode=mws;mux=true",
		Multiplex:     &option.OutboundMultiplexOptions{Enabled: true},
	})
	if err == nil {
		t.Fatal("expected double-mux rejection")
	}
}
