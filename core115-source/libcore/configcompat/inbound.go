package configcompat

import (
	"encoding/json"
	"fmt"
)

func convertInbounds(root map[string]json.RawMessage) error {
	if len(root["inbounds"]) == 0 {
		return nil
	}
	var ins []object
	if err := json.Unmarshal(root["inbounds"], &ins); err != nil {
		return err
	}
	route := object{}
	if len(root["route"]) > 0 {
		if err := json.Unmarshal(root["route"], &route); err != nil {
			return err
		}
	}
	prefix := []any{}
	for _, in := range ins {
		kind, _ := in["type"].(string)
		if kind != "tun" && kind != "mixed" && kind != "socks" && kind != "http" {
			continue
		}
		tag, _ := in["tag"].(string)
		if kind == "tun" {
			for _, pair := range [][3]string{{"inet4_address", "inet6_address", "address"}, {"inet4_route_address", "inet6_route_address", "route_address"}, {"inet4_route_exclude_address", "inet6_route_exclude_address", "route_exclude_address"}} {
				values := []any{}
				for _, key := range pair {
					if v, ok := in[key]; ok {
						switch x := v.(type) {
						case string:
							values = append(values, x)
						case []any:
							values = append(values, x...)
						default:
							return fmt.Errorf("inbound %s: invalid %s", tag, key)
						}
						delete(in, key)
					}
				}
				if len(values) > 0 {
					in[pair[2]] = values
				}
			}
			if nat, ok := in["endpoint_independent_nat"]; ok {
				if _, valid := nat.(bool); !valid {
					return fmt.Errorf("inbound %s: endpoint_independent_nat must be boolean", tag)
				}
				// The old native core retained this deprecated field only in its schema.
				// Both values used sing-tun's source-only UDP NAT for every stack.
				if _, ok := in["udp_filtering"]; ok {
					return fmt.Errorf("inbound %s: conflicting NAT fields", tag)
				}
				if _, ok := in["udp_mapping"]; ok {
					return fmt.Errorf("inbound %s: conflicting NAT fields", tag)
				}
				in["udp_mapping"] = "endpoint_independent"
				in["udp_filtering"] = "endpoint_independent"
				delete(in, "endpoint_independent_nat")
			}
		}
		sniff := in["sniff"] == true
		strategy, _ := in["domain_strategy"].(string)
		if (sniff || strategy != "" && strategy != "as_is" || in["udp_disable_domain_unmapping"] == true) && tag == "" {
			return fmt.Errorf("legacy inbound actions require explicit tag")
		}
		if sniff {
			r := object{"inbound": []string{tag}, "action": "sniff"}
			if v, ok := in["sniff_timeout"]; ok {
				r["timeout"] = v
			}
			if in["sniff_override_destination"] == true {
				r["override_destination"] = true
			}
			prefix = append(prefix, r)
		}
		if strategy != "" && strategy != "as_is" {
			prefix = append(prefix, object{"inbound": []string{tag}, "action": "resolve", "strategy": strategy})
		}
		if in["udp_disable_domain_unmapping"] == true {
			prefix = append(prefix, object{"inbound": []string{tag}, "action": "route-options", "udp_disable_domain_unmapping": true})
		}
		for _, k := range []string{"sniff", "sniff_override_destination", "sniff_timeout", "domain_strategy", "udp_disable_domain_unmapping"} {
			delete(in, k)
		}
	}
	old, _ := route["rules"].([]any)
	route["rules"] = append(prefix, old...)
	root["inbounds"], _ = json.Marshal(ins)
	root["route"], _ = json.Marshal(route)
	return nil
}
