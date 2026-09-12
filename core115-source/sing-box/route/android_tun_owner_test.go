package route

import (
	"context"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing/common/control"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/contrab/freelru"
	"github.com/sagernet/sing/contrab/maphash"
	"net/netip"
	"testing"
)

type ownerTestPlatform struct {
	adapter.PlatformInterface
	addresses []netip.Addr
	calls     int
	request   adapter.FindConnectionOwnerRequest
}

func (p *ownerTestPlatform) MyInterfaceAddress() []netip.Addr       { return p.addresses }
func (p *ownerTestPlatform) UsePlatformConnectionOwnerFinder() bool { return true }
func (p *ownerTestPlatform) FindConnectionOwner(r *adapter.FindConnectionOwnerRequest) (*adapter.ConnectionOwner, error) {
	p.calls++
	p.request = *r
	return &adapter.ConnectionOwner{UserId: 12700, PackageNames: []string{"test.selected"}}, nil
}

type ownerTestNetwork struct{ adapter.NetworkManager }

func (ownerTestNetwork) InterfaceFinder() control.InterfaceFinder { return ownerTestFinder{} }

type ownerTestFinder struct{ control.InterfaceFinder }

func (ownerTestFinder) Interfaces() []control.Interface {
	return []control.Interface{{Name: "wlan0", Addresses: []netip.Prefix{netip.MustParsePrefix("192.168.1.80/24")}}}
}
func TestAndroidTunOwnerLookup(t *testing.T) {
	for _, fixed := range []bool{false, true} {
		p := &ownerTestPlatform{}
		if fixed {
			p.addresses = []netip.Addr{netip.MustParseAddr("172.19.0.1")}
		}
		cache, err := freelru.New[processCacheKey, processCacheEntry](256, maphash.NewHasher[processCacheKey]().Hash32, true)
		if err != nil {
			t.Fatal(err)
		}
		r := &Router{platformInterface: p, network: ownerTestNetwork{}, processSearcher: newPlatformSearcher(p), processCache: cache, logger: log.NewNOPFactory().NewLogger("router")}
		m := adapter.InboundContext{Network: "tcp", Source: M.ParseSocksaddr("172.19.0.1:38428"), Destination: M.ParseSocksaddr("ppc.coouu.cn:443"), OriginDestination: M.ParseSocksaddr("198.18.0.54:443")}
		r.searchProcessInfo(context.Background(), &m)
		if !fixed {
			if m.ProcessInfo != nil || p.calls != 0 {
				t.Fatal("expected old bridge to skip owner")
			}
			t.Log("old bridge: TUN source excluded, owner lookup skipped")
			continue
		}
		if p.calls != 1 || m.ProcessInfo == nil || m.ProcessInfo.UserId != 12700 {
			t.Fatalf("owner missing: calls=%d info=%v", p.calls, m.ProcessInfo)
		}
		if p.request.DestinationAddress != "198.18.0.54" || p.request.SourceAddress != "172.19.0.1" || p.request.SourcePort != 38428 {
			t.Fatalf("wrong socket tuple: %+v", p.request)
		}
		t.Logf("fixed bridge: owner UID=%d; original fakeip socket tuple preserved", m.ProcessInfo.UserId)
	}
}
