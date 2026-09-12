package libcore

import (
	tun "github.com/sagernet/sing-tun"
	"net/netip"
)

// VPN addresses are deliberately excluded from physical dialer interfaces.
// Core 1.15 still needs their exact host addresses to authorize owner lookup.
func (w *boxPlatformInterfaceWrapper) setTunAddresses(options *tun.Options) {
	addresses := make([]netip.Addr, 0, len(options.Inet4Address)+len(options.Inet6Address))
	for _, prefix := range options.Inet4Address {
		addresses = append(addresses, prefix.Addr())
	}
	for _, prefix := range options.Inet6Address {
		addresses = append(addresses, prefix.Addr())
	}
	w.tunAddressAccess.Lock()
	w.tunAddresses = addresses
	w.tunAddressAccess.Unlock()
}
