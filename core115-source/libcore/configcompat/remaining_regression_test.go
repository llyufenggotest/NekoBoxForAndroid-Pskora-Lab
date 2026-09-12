package configcompat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/miekg/dns"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/service"
	"reflect"
	"testing"
)

func TestRemainingRcodeExchange(t *testing.T) {
	codes := []string{"success", "format_error", "server_failure", "name_error", "not_implemented", "refused"}
	for code, name := range codes {
		for _, final := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/final=%v", name, final), func(t *testing.T) {
				rules := fmt.Sprintf(`"rules":[{"domain":"rcode.invalid","server":"block"}],"final":"local"`)
				if final {
					rules = `"final":"block"`
				}
				_, ctx := startLegacyContext(t, fmt.Sprintf(`{"dns":{"servers":[{"tag":"block","address":"rcode://%s"},{"tag":"local","address":"local"}],%s},"outbounds":[{"type":"direct"}]}`, name, rules))
				q := new(dns.Msg)
				q.SetQuestion("rcode.invalid.", dns.TypeA)
				response, err := service.FromContext[adapter.DNSRouter](ctx).Exchange(ctx, q, adapter.DNSQueryOptions{})
				if err != nil {
					t.Fatal(err)
				}
				if response.Rcode != code || len(response.Answer) != 0 {
					t.Fatalf("response=%v want rcode=%d", response, code)
				}
			})
		}
	}
}

func TestRemainingNATSchema(t *testing.T) {
	for _, stack := range []string{"gvisor", "system", "mixed"} {
		for _, value := range []bool{false, true} {
			raw, err := Convert([]byte(fmt.Sprintf(`{"inbounds":[{"type":"tun","tag":"tun","stack":"%s","inet4_address":["172.19.0.1/30"],"endpoint_independent_nat":%v}]}`, stack, value)), nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			var o option.Options
			if err = o.UnmarshalJSONContext(include.Context(context.Background()), raw); err != nil {
				t.Fatal(err)
			}
			var root object
			json.Unmarshal(raw, &root)
			in := root["inbounds"].([]any)[0].(map[string]any)
			if in["udp_mapping"] != "endpoint_independent" || in["udp_filtering"] != "endpoint_independent" || in["stack"] != stack {
				t.Fatal(string(raw))
			}
			t.Logf("stack=%s legacy=%v mapping/filtering=endpoint_independent (schema verified; no device TUN)", stack, value)
		}
	}
}

func TestRemainingMultiPeerStart(t *testing.T) {
	key := func(n byte) string {
		b := make([]byte, 32)
		for i := range b {
			b[i] = n
		}
		return base64.StdEncoding.EncodeToString(b)
	}
	peers := []object{
		{"server": "127.0.0.1", "server_port": 51820, "public_key": key(2), "pre_shared_key": key(4), "allowed_ips": []string{"0.0.0.0/1", "::/1"}, "reserved": []int{1, 2, 3}},
		{"server": "127.0.0.1", "server_port": 51821, "public_key": key(3), "pre_shared_key": key(5), "allowed_ips": []string{"128.0.0.0/1", "8000::/1"}, "reserved": []int{4, 5, 6}},
	}
	root := object{"outbounds": []object{{"type": "wireguard", "tag": "wg", "local_address": []string{"10.0.0.1/32"}, "private_key": key(1), "peers": peers}}}
	config, _ := json.Marshal(root)
	raw, err := Convert(config, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var converted object
	json.Unmarshal(raw, &converted)
	actual := converted["endpoints"].([]any)[0].(map[string]any)["peers"]
	expected := []object{}
	for _, peer := range peers {
		p := object{}
		for k, v := range peer {
			p[k] = v
		}
		p["address"] = p["server"]
		p["port"] = p["server_port"]
		delete(p, "server")
		delete(p, "server_port")
		expected = append(expected, p)
	}
	e, _ := json.Marshal(expected)
	var normalized any
	json.Unmarshal(e, &normalized)
	if !reflect.DeepEqual(actual, normalized) {
		t.Fatalf("peers changed: %s", raw)
	}
	startLegacy(t, string(config))
	t.Log("two distinct peers preserved in order with allowed IPs, endpoint, public/PSK/reserved; box.New/Start succeeded")
}

func TestRemainingNestedDNSExchange(t *testing.T) {
	config := `{"dns":{"servers":[{"tag":"outer","address":"rcode://refused"},{"tag":"unused-child","address":"rcode://success"},{"tag":"fallback","address":"rcode://name_error"}],"rules":[{"type":"logical","mode":"and","rules":[{"type":"logical","mode":"or","rules":[{"domain":"hit.invalid","server":"unused-child"},{"domain":"other.invalid"}]},{"domain":"excluded.invalid","invert":true},{"query_type":["A","AAAA"]}],"server":"outer"}],"final":"fallback"},"outbounds":[{"type":"direct"}]}`
	_, ctx := startLegacyContext(t, config)
	for _, tc := range []struct {
		name string
		typ  uint16
		code int
	}{{"hit.invalid", dns.TypeA, 5}, {"other.invalid", dns.TypeAAAA, 5}, {"miss.invalid", dns.TypeA, 3}, {"excluded.invalid", dns.TypeA, 3}, {"hit.invalid", dns.TypeMX, 3}} {
		q := new(dns.Msg)
		q.SetQuestion(tc.name+".", tc.typ)
		response, err := service.FromContext[adapter.DNSRouter](ctx).Exchange(ctx, q, adapter.DNSQueryOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if response.Rcode != tc.code {
			t.Fatalf("%s/%d: got %d want %d", tc.name, tc.typ, response.Rcode, tc.code)
		}
	}
}

func TestRemainingNestedStrategyStart(t *testing.T) {
	startLegacy(t, `{"dns":{"servers":[{"tag":"real","address":"127.0.0.1","strategy":"ipv4_only"}],"rules":[{"type":"logical","mode":"and","rules":[{"type":"logical","mode":"or","rules":[{"domain":"a.invalid"},{"query_type":["A","AAAA"]}]}],"server":"real"}],"final":"real"},"outbounds":[{"type":"direct"}]}`)
}
