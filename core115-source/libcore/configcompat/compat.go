// Package configcompat adapts Android-owned legacy configuration at the bridge boundary.
package configcompat

import (
	"encoding/json"
	"fmt"
	"github.com/sagernet/sing-box/option"
	"strings"
)

type GeoLoader func(string) ([]option.HeadlessRule, error)

func Convert(data []byte, geoip, geosite GeoLoader) ([]byte, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	if raw, ok := root["route"]; ok {
		var route map[string]json.RawMessage
		if err := json.Unmarshal(raw, &route); err != nil {
			return nil, err
		}
		enabled := false
		if value, ok := route["concurrent_dial"]; ok {
			if err := json.Unmarshal(value, &enabled); err != nil {
				return nil, fmt.Errorf("route.concurrent_dial: %w", err)
			}
			delete(route, "concurrent_dial")
		}
		if raw, ok := route["rule_set"]; ok {
			var sets []map[string]json.RawMessage
			if err := json.Unmarshal(raw, &sets); err != nil {
				return nil, err
			}
			for _, set := range sets {
				var kind, path string
				json.Unmarshal(set["type"], &kind)
				json.Unmarshal(set["path"], &path)
				if kind != "local" {
					continue
				}
				var loader GeoLoader
				var name string
				if strings.HasPrefix(path, "geoip:") {
					loader = geoip
					name = strings.TrimPrefix(path, "geoip:")
				} else if strings.HasPrefix(path, "geosite:") {
					loader = geosite
					name = strings.TrimPrefix(path, "geosite:")
				} else {
					continue
				}
				if loader == nil {
					return nil, fmt.Errorf("geo loader unavailable: %s", path)
				}
				rules, err := loader(name)
				if err != nil {
					return nil, fmt.Errorf("load %s: %w", path, err)
				}
				set["type"] = json.RawMessage(`"inline"`)
				set["rules"], err = json.Marshal(rules)
				if err != nil {
					return nil, err
				}
				delete(set, "path")
				delete(set, "format")
			}
			route["rule_set"], _ = json.Marshal(sets)
		}
		root["route"], _ = json.Marshal(route)
	}
	if raw, ok := root["outbounds"]; ok {
		var outbounds, endpoints []map[string]json.RawMessage
		if err := json.Unmarshal(raw, &outbounds); err != nil {
			return nil, err
		}
		if raw, ok := root["endpoints"]; ok {
			if err := json.Unmarshal(raw, &endpoints); err != nil {
				return nil, err
			}
		}
		kept := make([]map[string]json.RawMessage, 0, len(outbounds))
		for _, out := range outbounds {
			var kind string
			json.Unmarshal(out["type"], &kind)
			if kind != "wireguard" {
				kept = append(kept, out)
				continue
			}
			var legacyPeers []map[string]json.RawMessage
			if rawPeers, ok := out["peers"]; ok {
				if err := json.Unmarshal(rawPeers, &legacyPeers); err != nil {
					return nil, fmt.Errorf("wireguard peers: %w", err)
				}
			}
			if len(legacyPeers) > 0 {
				peers := legacyPeers
				converted := make([]map[string]json.RawMessage, 0, len(peers))
				for _, oldPeer := range peers {
					peer := map[string]json.RawMessage{}
					for k, v := range oldPeer {
						peer[k] = v
					}
					for old, newKey := range map[string]string{"server": "address", "server_port": "port", "peer_public_key": "public_key"} {
						if v, ok := peer[old]; ok {
							peer[newKey] = v
							delete(peer, old)
						}
					}
					converted = append(converted, peer)
				}
				out["peers"], _ = json.Marshal(converted)
				// Old runtime ignores single-peer shorthand whenever peers is non-empty.
				for _, k := range []string{"server", "server_port", "peer_public_key", "pre_shared_key", "reserved"} {
					delete(out, k)
				}
			} else {
				peer := map[string]json.RawMessage{"allowed_ips": json.RawMessage(`["0.0.0.0/0","::/0"]`)}
				for old, newKey := range map[string]string{"server": "address", "server_port": "port", "peer_public_key": "public_key", "pre_shared_key": "pre_shared_key", "reserved": "reserved"} {
					if v, ok := out[old]; ok {
						peer[newKey] = v
						delete(out, old)
					}
				}
				out["peers"], _ = json.Marshal([]map[string]json.RawMessage{peer})
			}
			if v, ok := out["local_address"]; ok {
				out["address"] = v
				delete(out, "local_address")
			}
			if v, ok := out["system_interface"]; ok {
				out["system"] = v
				delete(out, "system_interface")
			}
			if v, ok := out["interface_name"]; ok {
				out["name"] = v
				delete(out, "interface_name")
			}
			endpoints = append(endpoints, out)
		}
		root["outbounds"], _ = json.Marshal(kept)
		if len(endpoints) > 0 {
			root["endpoints"], _ = json.Marshal(endpoints)
		}
	}
	if err := convertInbounds(root); err != nil {
		return nil, err
	}
	if err := convertDNS(root); err != nil {
		return nil, err
	}
	return json.Marshal(root)
}
