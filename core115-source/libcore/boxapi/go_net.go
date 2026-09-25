package boxapi

import (
	"context"
	"net"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/dialer"
	"github.com/sagernet/sing/common/metadata"
)

func DialContext(ctx context.Context, box *box.Box, tracker adapter.ConnectionTracker, network, addr string) (net.Conn, error) {
	return dialSocksaddr(ctx, box, tracker, network, metadata.ParseSocksaddr(addr))
}

func dialSocksaddr(ctx context.Context, box *box.Box, tracker adapter.ConnectionTracker, network string, destination metadata.Socksaddr) (net.Conn, error) {
	defOutboundTag := box.Outbound().Default().Tag()
	conn, err := dialer.NewDetour(box.Outbound(), defOutboundTag, true).DialContext(ctx, network, destination)
	if err != nil {
		return nil, err
	}
	if ss, ok := tracker.(*SbStatsService); ok {
		conn = ss.RoutedConnectionInternal("", defOutboundTag, "", conn, false)
	}
	return conn, nil
}
