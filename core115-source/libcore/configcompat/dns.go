package configcompat

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type object = map[string]any

func convertDNS(root map[string]json.RawMessage) error {
	raw, ok := root["dns"]
	if !ok {
		return nil
	}
	var d object
	if err := json.Unmarshal(raw, &d); err != nil {
		return err
	}
	var outbounds []object
	if err := json.Unmarshal(root["outbounds"], &outbounds); len(root["outbounds"]) > 0 && err != nil {
		return err
	}
	emptyDirect := map[string]bool{}
	for _, o := range outbounds {
		if o["type"] == "direct" && len(o) == 2 {
			tag, _ := o["tag"].(string)
			emptyDirect[tag] = true
		}
	}
	var route object
	if len(root["route"]) > 0 {
		if err := json.Unmarshal(root["route"], &route); err != nil {
			return err
		}
	}
	servers, _ := d["servers"].([]any)
	strategies := map[string]any{}
	rcodes := map[string]string{}
	legacyConfig := false
	for _, v := range servers {
		if s, ok := v.(map[string]any); ok {
			if _, ok := s["address"]; ok {
				legacyConfig = true
			}
		}
	}
	kept := []any{}
	for _, v := range servers {
		s := v.(map[string]any)
		address, legacy := s["address"].(string)
		if !legacy {
			kept = append(kept, s)
			continue
		}
		tag, _ := s["tag"].(string)
		if strategy, ok := s["strategy"]; ok {
			strategies[tag] = strategy
			delete(s, "strategy")
		}
		if strings.HasPrefix(address, "rcode://") {
			code := strings.TrimPrefix(address, "rcode://")
			mapped, supported := map[string]string{"success": "NOERROR", "format_error": "FORMERR", "server_failure": "SERVFAIL", "name_error": "NXDOMAIN", "not_implemented": "NOTIMP", "refused": "REFUSED"}[code]
			if !supported {
				return fmt.Errorf("DNS %s: unsupported legacy rcode %q", tag, code)
			}
			if len(s) != 2 {
				return fmt.Errorf("DNS %s: rcode transport has unsupported fields", tag)
			}
			rcodes[tag] = mapped
			continue
		}
		delete(s, "address")
		switch address {
		case "local":
			s["type"] = "local"
		case "fakeip":
			s["type"] = "fakeip"
			f, _ := d["fakeip"].(map[string]any)
			if f["enabled"] != true {
				return fmt.Errorf("DNS %s: fakeip transport without enabled ranges", tag)
			}
			for _, k := range []string{"inet4_range", "inet6_range"} {
				if v, ok := f[k]; ok {
					s[k] = v
				}
			}
		default:
			if !strings.Contains(address, "://") {
				address = "udp://" + address
			}
			u, err := url.Parse(address)
			if err != nil {
				return fmt.Errorf("DNS %s: %w", tag, err)
			}
			switch u.Scheme {
			case "udp", "tcp", "tls", "https", "quic", "h3":
			default:
				return fmt.Errorf("DNS %s: unsupported scheme %q", tag, u.Scheme)
			}
			if u.User != nil || u.Fragment != "" {
				return fmt.Errorf("DNS %s: unsupported URL credentials/fragment", tag)
			}
			s["type"] = u.Scheme
			s["server"] = u.Hostname()
			if u.Port() != "" {
				p, err := strconv.ParseUint(u.Port(), 10, 16)
				if err != nil {
					return err
				}
				s["server_port"] = p
			}
			if u.Scheme == "https" || u.Scheme == "h3" {
				if u.RequestURI() != "" && u.RequestURI() != "/" {
					s["path"] = u.RequestURI()
				}
			} else if u.Path != "" && u.Path != "/" || u.RawQuery != "" {
				return fmt.Errorf("DNS %s: unexpected URL path/query", tag)
			}
		}
		if resolver, ok := s["address_resolver"]; ok {
			r := object{"server": resolver}
			if st, ok := s["address_strategy"]; ok {
				r["strategy"] = st
				delete(s, "address_strategy")
			}
			if delay, ok := s["address_fallback_delay"]; ok {
				s["fallback_delay"] = delay
				delete(s, "address_fallback_delay")
			}
			s["domain_resolver"] = r
			delete(s, "address_resolver")
		}
		detour, _ := s["detour"].(string)
		if detour == "" && address != "local" && address != "fakeip" {
			if final, ok := route["final"].(string); ok {
				s["detour"] = final
				detour = final
			} else if len(outbounds) > 0 {
				detour, _ = outbounds[0]["tag"].(string)
				if detour != "" {
					s["detour"] = detour
				}
			}
		}
		// An unconfigured direct outbound is exactly the new transport's direct dialer.
		if emptyDirect[detour] {
			delete(s, "detour")
		}
		kept = append(kept, s)
	}
	if f, ok := d["fakeip"].(map[string]any); ok {
		for k := range f {
			if k != "enabled" && k != "inet4_range" && k != "inet6_range" {
				return fmt.Errorf("unknown fakeip option %s", k)
			}
		}
		delete(d, "fakeip")
	}
	rules, _ := d["rules"].([]any)
	for _, v := range rules {
		r := v.(map[string]any)
		if legacyConfig {
			if err := normalizeLegacyDNSChildren(r); err != nil {
				return err
			}
		}
		tag, _ := r["server"].(string)
		if _, ok := r["query_type"]; ok && legacyConfig {
			r["legacy_android_query_type"] = true
		}
		if code, ok := rcodes[tag]; ok {
			// Predefined responses return before DNS client cache lookup/write.
			// The legacy rcode transport also produces an empty response without
			// cacheable records. Consume only the validated legacy cache flag;
			// route/fakeip rules must retain their own disable_cache option.
			if value, exists := r["disable_cache"]; exists {
				if _, valid := value.(bool); !valid {
					return fmt.Errorf("DNS %s: disable_cache must be boolean", tag)
				}
				delete(r, "disable_cache")
			}
			delete(r, "server")
			r["action"] = "predefined"
			r["rcode"] = code
		} else if st, ok := strategies[tag]; ok {
			if _, explicit := r["strategy"]; !explicit {
				r["strategy"] = st
			}
		}
	}
	final, _ := d["final"].(string)
	if code, ok := rcodes[final]; ok {
		rules = append(rules, object{"action": "predefined", "rcode": code})
		delete(d, "final")
	} else if st, ok := strategies[final]; ok {
		rules = append(rules, object{"server": final, "strategy": st})
	}
	d["servers"] = kept
	d["rules"] = rules
	root["dns"], _ = json.Marshal(d)
	return nil
}
