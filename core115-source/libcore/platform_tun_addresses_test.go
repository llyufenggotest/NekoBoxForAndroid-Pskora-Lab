package libcore

import (
	tun "github.com/sagernet/sing-tun"
	"net/netip"
	"slices"
	"testing"
)

func TestPlatformTunAddresses(t *testing.T) {
	w := new(boxPlatformInterfaceWrapper)
	options := &tun.Options{Inet4Address: []netip.Prefix{netip.MustParsePrefix("172.19.0.1/30")}, Inet6Address: []netip.Prefix{netip.MustParsePrefix("fdfe:dcba:9876::1/126")}}
	w.setTunAddresses(options)
	want := []netip.Addr{netip.MustParseAddr("172.19.0.1"), netip.MustParseAddr("fdfe:dcba:9876::1")}
	if !slices.Equal(w.MyInterfaceAddress(), want) {
		t.Fatalf("VPN source addresses absent: got %v want %v", w.MyInterfaceAddress(), want)
	}
	got := w.MyInterfaceAddress()
	got[0] = netip.MustParseAddr("192.0.2.1")
	if !slices.Equal(w.MyInterfaceAddress(), want) {
		t.Fatal("caller changed stored addresses")
	}
	w.setTunAddresses(&tun.Options{})
	if len(w.MyInterfaceAddress()) != 0 {
		t.Fatal("old VPN addresses retained")
	}
}
