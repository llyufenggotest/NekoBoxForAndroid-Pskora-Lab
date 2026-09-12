package libcore

import (
	"context"
	"fmt"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	"net/netip"
	"os"
)

// Optional host facilities are explicitly disabled, not advertised as working.
func (*boxPlatformInterfaceWrapper) UsePlatformInterface() bool { return true }
func (*boxPlatformInterfaceWrapper) ProcessPlatformOptions(options option.TunPlatformOptions) error {
	return nil
}                                                                           // passed to Android by OpenInterface
func (*boxPlatformInterfaceWrapper) RequestPermissionForWIFIState() error   { return nil } // Android app owns permission UI
func (*boxPlatformInterfaceWrapper) UsePlatformConnectionOwnerFinder() bool { return true }
func (w *boxPlatformInterfaceWrapper) FindConnectionOwner(r *adapter.FindConnectionOwnerRequest) (*adapter.ConnectionOwner, error) {
	src, err := netip.ParseAddr(r.SourceAddress)
	if err != nil {
		return nil, err
	}
	dst, err := netip.ParseAddr(r.DestinationAddress)
	if err != nil {
		return nil, err
	}
	network := "tcp"
	if r.IpProtocol == 17 {
		network = "udp"
	} else if r.IpProtocol != 6 {
		return nil, fmt.Errorf("unsupported IP protocol %d", r.IpProtocol)
	}
	info, err := w.FindProcessInfo(context.Background(), network, netip.AddrPortFrom(src, uint16(r.SourcePort)), netip.AddrPortFrom(dst, uint16(r.DestinationPort)))
	if err != nil {
		return nil, err
	}
	return &adapter.ConnectionOwner{UserId: info.UserId, PackageNames: info.PackageNames}, nil
}
func (*boxPlatformInterfaceWrapper) UsePlatformWIFIMonitor() bool           { return false }
func (*boxPlatformInterfaceWrapper) UsePlatformNotification() bool          { return false }
func (*boxPlatformInterfaceWrapper) CancelNotification(string, int32) error { return os.ErrInvalid }
func (w *boxPlatformInterfaceWrapper) MyInterfaceAddress() []netip.Addr {
	w.tunAddressAccess.RLock()
	defer w.tunAddressAccess.RUnlock()
	return append([]netip.Addr(nil), w.tunAddresses...)
}
func (*boxPlatformInterfaceWrapper) UsePlatformNeighborResolver() bool { return false }
func (*boxPlatformInterfaceWrapper) StartNeighborMonitor(adapter.NeighborUpdateListener) error {
	return os.ErrInvalid
}
func (*boxPlatformInterfaceWrapper) CloseNeighborMonitor(adapter.NeighborUpdateListener) error {
	return os.ErrInvalid
}
func (*boxPlatformInterfaceWrapper) UsePlatformShell() bool    { return false }
func (*boxPlatformInterfaceWrapper) CheckPlatformShell() error { return os.ErrInvalid }
func (*boxPlatformInterfaceWrapper) OpenShellSession(*adapter.PlatformUser, string, []string, string, int32, int32) (adapter.ShellSession, error) {
	return nil, os.ErrInvalid
}
func (*boxPlatformInterfaceWrapper) LookupUser(string) (*adapter.PlatformUser, error) {
	return nil, os.ErrInvalid
}
func (*boxPlatformInterfaceWrapper) LookupSFTPServer() (string, error)     { return "", os.ErrInvalid }
func (*boxPlatformInterfaceWrapper) ReadSystemSSHHostKey() ([]byte, error) { return nil, os.ErrInvalid }
func (*boxPlatformInterfaceWrapper) TailscaleHostname() string             { v, _ := os.Hostname(); return v }
func (*boxPlatformInterfaceWrapper) UsePlatformBridge() bool               { return false }
func (*boxPlatformInterfaceWrapper) CreateBridge(adapter.BridgeOptions) (adapter.BridgeSession, error) {
	return nil, os.ErrInvalid
}
func (*boxPlatformInterfaceWrapper) UsePlatformAutoRedirect() bool { return false }
func (*boxPlatformInterfaceWrapper) CreateAutoRedirect(adapter.AutoRedirectOptions) (adapter.AutoRedirectSession, error) {
	return nil, os.ErrInvalid
}
