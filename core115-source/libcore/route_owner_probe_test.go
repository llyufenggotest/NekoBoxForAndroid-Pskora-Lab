//go:build route_owner_probe

package libcore

import (
	"context"
	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/service"
	"libcore/configcompat"
	"libcore/nekoutils"
	"net/netip"
	"testing"
)

// Used only by the explicit-file host probe; full Android package owns this global.
var externalAssetsPath = "../evidence/real-geo-assets"

func TestRealCNApplicationRouting(t *testing.T) {
	config := `{"outbounds":[{"type":"direct","tag":"bypass"},{"type":"direct","tag":"fallback"}],"route":{"rules":[{"rule_set":["geosite:cn"],"user_id":[12700,11918],"outbound":"bypass"}],"rule_set":[{"type":"local","tag":"geosite:cn","format":"binary","path":"geosite:cn"}],"final":"fallback"}}`
	ctx, data, err := configcompat.ConvertContext(include.Context(context.Background()), []byte(config), nil, nekoutils.GetGeoSiteHeadlessRules)
	if err != nil {
		t.Fatal(err)
	}
	ctx = service.ContextWithDefaultRegistry(ctx)
	var opts option.Options
	if err = opts.UnmarshalJSONContext(ctx, data); err != nil {
		t.Fatal(err)
	}
	b, err := box.New(box.Options{Context: ctx, Options: opts})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err = b.Start(); err != nil {
		t.Fatal(err)
	}
	router := service.FromContext[adapter.Router](ctx)
	set, ok := router.RuleSet("geosite:cn")
	if !ok {
		t.Fatal("missing cn")
	}
	for _, tc := range []struct {
		domain      string
		uid         int32
		owner, want bool
	}{
		{"ppc.coouu.cn", 12700, true, true}, {"ppc.coouu.cn", 11918, true, true},
		{"ppc.coouu.cn", 12700, false, false}, {"ppc.coouu.cn", 12345, true, false},
		{"example.invalid", 12700, true, false},
	} {
		m := adapter.InboundContext{Source: M.ParseSocksaddr("172.19.0.1:38428"), Destination: M.ParseSocksaddr(tc.domain + ":443"), Domain: tc.domain, OriginDestination: M.SocksaddrFromNetIP(netip.MustParseAddrPort("198.18.0.54:443"))}
		if tc.owner {
			m.ProcessInfo = &adapter.ConnectionOwner{UserId: tc.uid}
		}
		sm := m
		member := set.Match(&sm)
		got := router.Rules()[0].Match(&m)
		t.Logf("domain=%s bundled_cn=%v owner=%v uid=%d bypass=%v", tc.domain, member, tc.owner, tc.uid, got)
		if got != tc.want {
			t.Fatalf("bypass got %v want %v", got, tc.want)
		}
	}
}
