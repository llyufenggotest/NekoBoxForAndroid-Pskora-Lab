package configcompat

import (
	"context"
	"encoding/json"
	"github.com/miekg/dns"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/service"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
)

func TestDeviceDNSDisableCacheDecode(t *testing.T) {
	raw, err := os.ReadFile("testdata/device_dns_sanitized.json")
	if err != nil {
		t.Fatal(err)
	}
	ctx, data, err := ConvertContext(include.Context(context.Background()), raw, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var opts option.Options
	if err = opts.UnmarshalJSONContext(ctx, data); err != nil {
		t.Fatal(err)
	}
}

// Host-safe Start retains both inbound tags and all rules; only the OS TUN
// device is replaced by a loopback mixed listener. Device VPN is not exercised.
func TestDeviceDNSStartAndExchange(t *testing.T) {
	raw, err := os.ReadFile("testdata/device_dns_sanitized.json")
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err = json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	for i, v := range root["inbounds"].([]any) {
		tag := v.(map[string]any)["tag"]
		root["inbounds"].([]any)[i] = map[string]any{"type": "mixed", "tag": tag, "listen": "127.0.0.1", "listen_port": 0}
	}
	root["experimental"].(map[string]any)["cache_file"].(map[string]any)["path"] = filepath.Join(t.TempDir(), "cache.db")
	// Android-only OS integration is disabled only in this Windows harness.
	root["route"].(map[string]any)["override_android_vpn"] = false
	raw, _ = json.Marshal(root)
	instance, ctx := startLegacyContext(t, string(raw))
	router := service.FromContext[adapter.DNSRouter](ctx)
	for _, qtype := range []uint16{dns.TypeA, dns.TypeAAAA} {
		for n := 0; n < 2; n++ {
			query := new(dns.Msg)
			query.SetQuestion("ads.invalid.", qtype)
			response, err := router.Exchange(adapter.WithContext(ctx, &adapter.InboundContext{Inbound: "tun-in"}), query, adapter.DNSQueryOptions{})
			if err != nil || response == nil {
				t.Fatalf("ads exchange: %v", err)
			}
			if response.Rcode != dns.RcodeSuccess || len(response.Answer) != 0 {
				t.Fatalf("ads changed: %v", response)
			}
			query.SetQuestion("fake-only.invalid.", qtype)
			response, err = router.Exchange(adapter.WithContext(ctx, &adapter.InboundContext{Inbound: "tun-in"}), query, adapter.DNSQueryOptions{})
			if err != nil || response == nil {
				t.Fatalf("fake exchange: %v / %v", response, err)
			}
			if qtype == dns.TypeAAAA { // The device explicitly requests ipv4_only.
				if response.Rcode != dns.RcodeSuccess || len(response.Answer) != 0 {
					t.Fatalf("ipv4_only AAAA changed: %v", response)
				}
				continue
			}
			if len(response.Answer) != 1 {
				t.Fatalf("fake answer: %v", response)
			}
			var address netip.Addr
			switch a := response.Answer[0].(type) {
			case *dns.A:
				address, _ = netip.AddrFromSlice(a.A)
				address = address.Unmap()
			case *dns.AAAA:
				address, _ = netip.AddrFromSlice(a.AAAA)
			}
			prefix := netip.MustParsePrefix("198.18.0.0/15")
			if qtype == dns.TypeAAAA {
				prefix = netip.MustParsePrefix("fc00::/18")
			}
			if !prefix.Contains(address) {
				t.Fatalf("fake address outside range: %v", address)
			}
		}
	}
	_ = instance
}
