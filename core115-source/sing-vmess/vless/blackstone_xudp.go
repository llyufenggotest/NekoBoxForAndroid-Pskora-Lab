package vless

import (
	"net"

	"github.com/sagernet/sing-vmess"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
)

func (c *Client) IsBlackstone() bool { return c.blackstone != nil }

// DialEarlyXUDPPacketConnWithOptions preserves the real TLS/Reality base and
// transport splice boundary for private Command3. Keep legacy public modes on
// their historical initializer; only Blackstone needs donor first-write timing.
func (c *Client) DialEarlyXUDPPacketConnWithOptions(conn net.Conn, base net.Conn, destination M.Socksaddr, canSplice bool) (vmess.PacketConn, error) {
	if c.blackstone == nil {
		return nil, E.New("Blackstone XUDP initializer requires private selector")
	}
	raw := &blackstoneUDPCloseConn{Conn: conn}
	stream := c.newConn(raw, vmess.CommandMux, M.Socksaddr{})
	wrapped, err := c.prepareConnWithOptions(stream, base, canSplice)
	if err != nil {
		_ = raw.Close()
		return nil, err
	}
	return vmess.NewXUDPConn(wrapped, destination), nil
}
