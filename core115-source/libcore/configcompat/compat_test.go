package configcompat

import (
	"context"
	"encoding/json"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	"testing"
)

func TestLegacyWireGuard(t *testing.T) {
	result, err := Convert([]byte(`{"outbounds":[{"type":"wireguard","tag":"wg","server":"127.0.0.1","server_port":51820,"local_address":["10.0.0.2/32"],"private_key":"key","peer_public_key":"public","reserved":[1,2,3],"mtu":1400},{"type":"direct","tag":"direct"}]}`), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var parsed option.Options
	if err := parsed.UnmarshalJSONContext(include.Context(context.Background()), result); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Endpoints) != 1 {
		t.Fatal("endpoint schema not consumed")
	}
	var root map[string]any
	json.Unmarshal(result, &root)
	ep, ok := root["endpoints"].([]any)
	if !ok || len(ep) != 1 {
		t.Fatalf("wireguard not migrated: %s", result)
	}
	wg := ep[0].(map[string]any)
	peer := wg["peers"].([]any)[0].(map[string]any)
	if wg["tag"] != "wg" || peer["public_key"] != "public" || peer["port"] != float64(51820) || len(root["outbounds"].([]any)) != 1 {
		t.Fatalf("bad migration %s", result)
	}
}

func TestLegacyGeoAndConcurrent(t *testing.T) {
	called := ""
	loader := func(name string) ([]option.HeadlessRule, error) {
		called = name
		return []option.HeadlessRule{{Type: "default", DefaultOptions: option.DefaultHeadlessRule{Domain: []string{"example.org"}}}}, nil
	}
	result, err := Convert([]byte(`{"route":{"concurrent_dial":true,"rule_set":[{"type":"local","tag":"geosite:test","format":"binary","path":"geosite:test"}]}}`), loader, loader)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err = json.Unmarshal(result, &m); err != nil {
		t.Fatal(err)
	}
	route := m["route"].(map[string]any)
	if _, ok := route["concurrent_dial"]; ok {
		t.Fatal("legacy concurrent_dial not consumed")
	}
	rs := route["rule_set"].([]any)[0].(map[string]any)
	if called != "test" || rs["type"] != "inline" || rs["tag"] != "geosite:test" {
		t.Fatalf("geo not converted: %s", result)
	}
	if _, ok := rs["path"]; ok {
		t.Fatal("legacy geo path retained")
	}
}
