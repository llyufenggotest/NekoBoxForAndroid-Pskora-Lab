package xhttp

import (
	"errors"
	"gopkg.in/yaml.v3"
	"io"
	"strings"
)

type blackstoneSelectionDocument struct {
	Mode    string           `yaml:"mode"`
	Rules   []string         `yaml:"rules"`
	Proxies []map[string]any `yaml:"proxies"`
	Groups  []map[string]any `yaml:"proxy-groups"`
}

// Decode through yaml.Node first: map decoding must not erase duplicate keys,
// aliases, merge keys, or additional documents before contract validation.
func blackstoneDecodeSelection(plain []byte) ([]map[string]any, error) {
	dec := yaml.NewDecoder(strings.NewReader(string(plain)))
	var root yaml.Node
	if err := dec.Decode(&root); err != nil {
		return nil, errors.New("invalid selection document")
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, errors.New("multiple selection documents")
	}
	if err := blackstoneValidateYAML(&root, 0); err != nil {
		return nil, err
	}
	var doc blackstoneSelectionDocument
	if err := root.Decode(&doc); err != nil {
		return nil, errors.New("invalid selection fields")
	}
	if doc.Mode != "rule" || len(doc.Rules) == 0 {
		return nil, errors.New("unproved selection mode or rules")
	}
	target := ""
	for i, r := range doc.Rules {
		parts := strings.Split(r, ",")
		if strings.TrimSpace(parts[0]) != "MATCH" {
			continue
		}
		if target != "" || i != len(doc.Rules)-1 || len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
			return nil, errors.New("ambiguous final MATCH")
		}
		target = strings.TrimSpace(parts[1])
	}
	if target == "" || blackstoneReservedName(target) {
		return nil, errors.New("missing final MATCH group")
	}
	leaves := make(map[string]map[string]any, len(doc.Proxies))
	for _, p := range doc.Proxies {
		n, ok := p["name"].(string)
		if !ok || n == "" || blackstoneReservedName(n) || leaves[n] != nil {
			return nil, errors.New("ambiguous leaf name")
		}
		leaves[n] = p
	}
	groups := make(map[string]map[string]any, len(doc.Groups))
	for _, g := range doc.Groups {
		n, ok := g["name"].(string)
		if !ok || n == "" || blackstoneReservedName(n) || leaves[n] != nil || groups[n] != nil {
			return nil, errors.New("ambiguous group name")
		}
		groups[n] = g
	}
	main := groups[target]
	if main == nil || main["type"] != "url-test" {
		return nil, errors.New("unproved main group")
	}
	for k, v := range main {
		switch k {
		case "name", "type", "proxies":
		case "url":
			if _, ok := v.(string); !ok {
				return nil, errors.New("invalid health URL")
			}
		case "interval", "tolerance":
			if !blackstoneBoundedInt(v, 86400) {
				return nil, errors.New("invalid health option")
			}
		case "once-check":
			// Official ARM64 OnceCheck (GroupOption+0xb8 -> URLTest+0x28)
			// schedules Sleep(seconds), GroupBase.healthCheck; never expands members.
			if !blackstoneBoundedInt(v, 86400) {
				return nil, errors.New("invalid once-check")
			}
		case "lazy", "disable-udp":
			if _, ok := v.(bool); !ok {
				return nil, errors.New("invalid group boolean")
			}
		default:
			return nil, errors.New("unproved main group option")
		}
	}
	members, ok := main["proxies"].([]any)
	if !ok || len(members) == 0 {
		return nil, errors.New("empty main group")
	}
	selected := make([]map[string]any, 0, len(members))
	seen := make(map[string]bool, len(members))
	for _, v := range members {
		n, ok := v.(string)
		if !ok || n == "" || seen[n] || blackstoneReservedName(n) || groups[n] != nil || leaves[n] == nil {
			return nil, errors.New("invalid exact main-group leaf")
		}
		seen[n] = true
		selected = append(selected, leaves[n])
	}
	return selected, nil
}
func blackstoneBoundedInt(v any, max int) bool { n, ok := v.(int); return ok && n >= 0 && n <= max }
func blackstoneReservedName(n string) bool {
	switch strings.ToUpper(strings.TrimSpace(n)) {
	case "DIRECT", "REJECT", "REJECT-DROP", "PASS", "COMPATIBLE", "GLOBAL":
		return true
	}
	return false
}
func blackstoneValidateYAML(n *yaml.Node, depth int) error {
	if depth > 64 || n.Kind == yaml.AliasNode || n.Anchor != "" {
		return errors.New("unproved YAML indirection")
	}
	if n.Kind == yaml.MappingNode {
		keys := make(map[string]bool, len(n.Content)/2)
		for i := 0; i < len(n.Content); i += 2 {
			key := n.Content[i]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || key.Value == "<<" || keys[key.Value] {
				return errors.New("duplicate or invalid selection key")
			}
			keys[key.Value] = true
		}
	}
	for _, c := range n.Content {
		if err := blackstoneValidateYAML(c, depth+1); err != nil {
			return err
		}
	}
	return nil
}
