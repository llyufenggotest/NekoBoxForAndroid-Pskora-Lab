package libcore

import (
	"context"
	"github.com/miekg/dns"
	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/service"
	"libcore/configcompat"
	"libcore/nekoutils"
	"net/netip"
	"path/filepath"
	"testing"
)

func TestRealAPKGeoCallbacksRouteDNS(t *testing.T) {
	old := externalAssetsPath
	externalAssetsPath = filepath.Join("..", "evidence", "real-geo-assets")
	defer func() { externalAssetsPath = old }()
	ipRules, err := nekoutils.GetGeoIPHeadlessRules("cn")
	if err != nil {
		t.Fatal(err)
	}
	siteRules, err := nekoutils.GetGeoSiteHeadlessRules("google")
	if err != nil {
		t.Fatal(err)
	}
	if len(ipRules) == 0 || len(siteRules) == 0 {
		t.Fatal("empty real DB rules")
	}
	config := `{"log":{"level":"debug"},"dns":{"servers":[{"tag":"geo-dns","type":"hosts","predefined":{"google.com":["192.0.2.55"]}},{"tag":"wrong","type":"hosts","predefined":{"google.com":["192.0.2.99"]}}],"rules":[{"rule_set":["geosite:google"],"server":"geo-dns"}],"final":"wrong"},"outbounds":[{"type":"direct","tag":"direct"}],"route":{"rules":[{"rule_set":["geoip:cn"],"outbound":"direct"},{"rule_set":["geosite:google"],"outbound":"direct"}],"rule_set":[{"type":"local","tag":"geoip:cn","format":"binary","path":"geoip:cn"},{"type":"local","tag":"geosite:google","format":"binary","path":"geosite:google"}],"final":"direct"}}`
	ctx, data, err := configcompat.ConvertContext(include.Context(context.Background()), []byte(config), nekoutils.GetGeoIPHeadlessRules, nekoutils.GetGeoSiteHeadlessRules)
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
	ipSet, ok := router.RuleSet("geoip:cn")
	if !ok {
		t.Fatal("missing geoip set")
	}
	prefix, err := netip.ParsePrefix(ipRules[0].DefaultOptions.IPCIDR[0])
	if err != nil {
		t.Fatal(err)
	}
	ipMeta := adapter.InboundContext{Destination: M.SocksaddrFromNetIP(netip.AddrPortFrom(prefix.Addr(), 443))}
	if !ipSet.Match(&ipMeta) || !router.Rules()[0].Match(&ipMeta) {
		t.Fatal("real geoip route mismatch")
	}
	siteMeta := adapter.InboundContext{Domain: "google.com", Destination: M.ParseSocksaddr("google.com:443")}
	if !router.Rules()[1].Match(&siteMeta) {
		t.Fatal("real geosite route mismatch")
	}
	q := new(dns.Msg)
	q.SetQuestion("google.com.", dns.TypeA)
	answer, err := service.FromContext[adapter.DNSRouter](ctx).Exchange(ctx, q, adapter.DNSQueryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(answer.Answer) != 1 || answer.Answer[0].(*dns.A).A.String() != "192.0.2.55" {
		t.Fatalf("wrong geo DNS route: %v", answer)
	}
	t.Logf("APK callbacks -> ConvertContext -> inline rule sets -> box.Start -> geoip route (%d CIDRs, sample %s), geosite route + actual DNS exchange PASS", len(ipRules[0].DefaultOptions.IPCIDR), prefix)
}
