package configcompat

import (
	"context"
	"fmt"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json"
)

// These are the action fields accepted by the old DNS action parser.
// Validate before discarding: ignored execution did not mean invalid input was accepted.
func stripLegacyDNSAction(child object) error {
	action, ok := child["action"].(string)
	if child["action"] != nil && !ok {
		return fmt.Errorf("invalid nested DNS action")
	}
	keys := []string{"server", "strategy", "disable_cache", "rewrite_ttl", "client_subnet"}
	var target any
	switch action {
	case "", "route":
		target = &option.DNSRouteActionOptions{}
	case "route-options":
		keys = keys[1:]
		target = &option.DNSRouteOptionsActionOptions{}
	case "reject":
		if method, exists := child["method"]; exists && method != nil && method != "" && method != "default" && method != "drop" {
			return fmt.Errorf("unknown legacy nested DNS reject method %v", method)
		}
		keys = []string{"method", "no_drop"}
		target = &option.RejectActionOptions{}
	case "predefined":
		keys = []string{"rcode", "answer", "ns", "extra"}
		target = &option.DNSRouteActionPredefined{}
	default:
		return fmt.Errorf("unknown legacy nested DNS action %q", action)
	}
	fields := object{}
	for _, key := range keys {
		if v, ok := child[key]; ok {
			fields[key] = v
		}
	}
	for _, key := range []string{"server", "strategy", "disable_cache", "rewrite_ttl", "client_subnet", "method", "no_drop", "rcode", "answer", "ns", "extra"} {
		if _, exists := child[key]; exists {
			if _, allowed := fields[key]; !allowed {
				return fmt.Errorf("unexpected %s for legacy nested DNS action %q", key, action)
			}
		}
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	if err = json.UnmarshalContext(context.Background(), raw, target); err != nil {
		return fmt.Errorf("legacy nested DNS action %q: %w", action, err)
	}
	for _, key := range keys {
		delete(child, key)
	}
	delete(child, "action")
	return nil
}

// Legacy LogicalDNSRule.Match calls only child.Match: child actions never run.
// Preserve the tree and the outer action; discard validated legacy child actions.
func normalizeLegacyDNSChildren(r object) error {
	children, _ := r["rules"].([]any)
	for _, v := range children {
		child, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("invalid nested DNS rule")
		}
		if err := stripLegacyDNSAction(child); err != nil {
			return err
		}
		if _, ok := child["query_type"]; ok {
			child["legacy_android_query_type"] = true
		}
		if err := normalizeLegacyDNSChildren(child); err != nil {
			return err
		}
	}
	return nil
}
