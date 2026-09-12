package tunnetcontrol

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strings"
)

type syncWire struct {
	SchemaVersion int64           `json:"schema_version"`
	Release       json.RawMessage `json:"release"`
	Access        Access          `json:"access"`
	Runtime       *runtimeWire    `json:"runtime"`
}

type runtimeWire struct {
	ClientID        string          `json:"client_id"`
	Traffic         json.RawMessage `json:"traffic"`
	Network         networkWire     `json:"network"`
	ActiveEntryNode string          `json:"active_entry_node"`
	EntryNodes      []SnapshotEntry `json:"entry_nodes"`
	Hosts           []SnapshotHost  `json:"hosts"`
}

type networkWire struct {
	RootDomains []string `json:"root_domains"`
}

type MapOptions struct {
	DataAuthority      string
	PreviousEntryNode  string
	RequestedEntryNode string
	RequestedHostSlug  string
	RouteProber        RouteProber
}

func ResolveDataAuthority(syncResponse json.RawMessage, inputAuthority, requestedHostSlug string) (string, error) {
	var wire syncWire
	if err := json.Unmarshal(syncResponse, &wire); err != nil || wire.Runtime == nil {
		return "", errors.New("invalid TunNet sync runtime for authority selection")
	}
	inputAuthority = strings.ToLower(strings.TrimSpace(inputAuthority))
	selected := -1
	requestedHostSlug = strings.TrimSpace(requestedHostSlug)
	if requestedHostSlug != "" && !validDNSLabel(requestedHostSlug) {
		return "", errors.New("invalid requested TunNet host slug")
	}
	for index := range wire.Runtime.Hosts {
		host := wire.Runtime.Hosts[index]
		if !host.Online || strings.TrimSpace(host.Slug) == "" || strings.TrimSpace(host.VLESSEncryptionKey) == "" {
			continue
		}
		if requestedHostSlug != "" {
			if strings.EqualFold(host.Slug, requestedHostSlug) {
				selected = index
				break
			}
			continue
		}
		if selected < 0 || host.LoadPercent < wire.Runtime.Hosts[selected].LoadPercent || host.LoadPercent == wire.Runtime.Hosts[selected].LoadPercent && host.Slug < wire.Runtime.Hosts[selected].Slug {
			selected = index
		}
	}
	if selected < 0 {
		if requestedHostSlug != "" {
			return "", errors.New("requested TunNet host is absent, offline, or unusable")
		}
		return "", errors.New("TunNet runtime has no usable online host")
	}
	var authority string
	if inputAuthority != "" {
		if !validDNSAuthority(inputAuthority) {
			return "", errors.New("invalid TunNet data authority")
		}
		labels := strings.Split(inputAuthority, ".")
		authority = strings.ToLower(wire.Runtime.Hosts[selected].Slug + "." + strings.Join(labels[1:], "."))
	} else {
		if len(wire.Runtime.Network.RootDomains) != len(wire.Runtime.Hosts) {
			return "", errors.New("TunNet runtime host/root-domain shape mismatch")
		}
		baseDomain := strings.ToLower(strings.TrimSpace(wire.Runtime.Network.RootDomains[selected]))
		if !validDNSAuthority(baseDomain) {
			return "", errors.New("TunNet selected host has invalid root domain")
		}
		authority = strings.ToLower(wire.Runtime.Hosts[selected].Slug + "." + baseDomain)
	}
	if !validDNSAuthority(authority) {
		return "", errors.New("TunNet selected host has invalid authority")
	}
	return authority, nil
}

func MapSnapshot(bootstrap, syncResponse json.RawMessage, echConfig ECHConfig, identity *Identity) (*Snapshot, error) {
	return MapSnapshotWithOptions(context.Background(), bootstrap, syncResponse, echConfig, identity, MapOptions{})
}

func MapSnapshotWithOptions(ctx context.Context, bootstrapRaw, syncResponse json.RawMessage, echConfig ECHConfig, identity *Identity, options MapOptions) (*Snapshot, error) {
	if identity == nil {
		return nil, errors.New("missing TunNet identity")
	}
	var wire syncWire
	if err := json.Unmarshal(syncResponse, &wire); err != nil {
		return nil, fmt.Errorf("decode TunNet sync envelope: %w", err)
	}
	if wire.SchemaVersion != 2 || wire.Runtime == nil {
		return nil, fmt.Errorf("invalid TunNet sync schema/runtime: %d", wire.SchemaVersion)
	}
	if wire.Access.State != "ready" {
		return nil, fmt.Errorf("TunNet sync access is not ready: %q", wire.Access.State)
	}
	if wire.Runtime.ClientID != "" && !strings.EqualFold(wire.Runtime.ClientID, identity.ClientID) {
		return nil, errors.New("TunNet sync client ID does not match local identity")
	}

	var bootstrap BootstrapResponse
	if err := json.Unmarshal(bootstrapRaw, &bootstrap); err != nil {
		return nil, fmt.Errorf("decode TunNet ready bootstrap: %w", err)
	}
	if bootstrap.SchemaVersion != 2 || bootstrap.Access.State != "ready" {
		return nil, errors.New("TunNet ready bootstrap is invalid")
	}
	var bootRuntime runtimeWire
	if err := json.Unmarshal(bootstrap.Runtime, &bootRuntime); err != nil {
		return nil, fmt.Errorf("decode TunNet bootstrap runtime: %w", err)
	}
	// Sync owns the rotating entry/host catalog. Bootstrap owns network metadata
	// that sync may omit after access completion. The protocol binds root domains
	// to hosts positionally, so fail closed unless both catalogs are identical in
	// length and slug order before borrowing bootstrap network metadata.
	if len(wire.Runtime.Network.RootDomains) == 0 && options.DataAuthority == "" {
		if len(bootRuntime.Hosts) != len(wire.Runtime.Hosts) {
			return nil, errors.New("TunNet bootstrap/sync host catalog shape mismatch")
		}
		for index := range wire.Runtime.Hosts {
			if !strings.EqualFold(strings.TrimSpace(bootRuntime.Hosts[index].Slug), strings.TrimSpace(wire.Runtime.Hosts[index].Slug)) {
				return nil, errors.New("TunNet bootstrap/sync host catalog order mismatch")
			}
		}
		wire.Runtime.Network = bootRuntime.Network
	}
	if len(wire.Runtime.EntryNodes) == 0 || len(wire.Runtime.Hosts) == 0 {
		return nil, errors.New("TunNet sync runtime catalog is empty")
	}
	entries := wire.Runtime.EntryNodes
	hosts := wire.Runtime.Hosts

	mergedSync, err := json.Marshal(wire)
	if err != nil {
		return nil, fmt.Errorf("merge TunNet runtime metadata: %w", err)
	}
	authority, err := ResolveDataAuthority(mergedSync, options.DataAuthority, options.RequestedHostSlug)
	if err != nil {
		return nil, err
	}
	selectedHost := -1
	for index := range hosts {
		if inputAuthority := strings.ToLower(strings.TrimSpace(options.DataAuthority)); inputAuthority != "" {
			if strings.EqualFold(hosts[index].Slug, strings.SplitN(authority, ".", 2)[0]) {
				selectedHost = index
				break
			}
		} else if index < len(wire.Runtime.Network.RootDomains) && strings.EqualFold(hosts[index].Slug+"."+strings.TrimSpace(wire.Runtime.Network.RootDomains[index]), authority) {
			selectedHost = index
			break
		}
	}
	if selectedHost < 0 {
		return nil, errors.New("resolved TunNet host is absent from runtime")
	}
	hosts[selectedHost].Authority = authority

	entryName := strings.TrimSpace(options.RequestedEntryNode)
	if entryName == "" {
		entryName = strings.TrimSpace(options.PreviousEntryNode)
	}
	entryIndex := -1
	if entryName == "" && strings.TrimSpace(bootRuntime.ActiveEntryNode) != "" {
		entryName = bootRuntime.ActiveEntryNode
	}
	if entryName != "" {
		for index := range entries {
			if entries[index].Name == entryName {
				entryIndex = index
				break
			}
		}
		if entryIndex < 0 {
			return nil, errors.New("persisted TunNet entry is absent from runtime")
		}
	} else {
		choice, err := rand.Int(rand.Reader, big.NewInt(int64(len(entries))))
		if err != nil {
			return nil, fmt.Errorf("choose TunNet entry: %w", err)
		}
		entryIndex = int(choice.Int64())
		entryName = entries[entryIndex].Name
	}
	if strings.TrimSpace(entryName) == "" {
		return nil, errors.New("selected TunNet entry has no name")
	}

	prober := options.RouteProber
	if prober == nil {
		prober = TCPRouteProber{}
	}
	route, err := SelectRouteIP(ctx, entries[entryIndex].IPv4, prober)
	if err != nil {
		return nil, err
	}
	path, err := GenerateXHTTPPath()
	if err != nil {
		return nil, fmt.Errorf("generate TunNet XHTTP path: %w", err)
	}
	return &Snapshot{
		SchemaVersion:   snapshotSchema,
		ClientID:        strings.ToLower(identity.ClientID),
		ActiveEntryNode: entryName,
		SelectedHost:    hosts[selectedHost].Slug,
		ECHConfig:       SnapshotECH{ConfigList: echConfig.ConfigList, ExpiresAt: echConfig.ExpiresAt},
		EntryNodes:      entries,
		Hosts:           hosts,
		ValidatedRoute:  route,
		XHTTPAuthority:  authority,
		XHTTPPath:       path,
	}, nil
}

func validDNSLabel(value string) bool {
	if value == "" || len(value) > 63 || value[0] == '-' || value[len(value)-1] == '-' {
		return false
	}
	for _, r := range value {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
			return false
		}
	}
	return true
}

func validDNSAuthority(value string) bool {
	if !validAuthority(value) || len(value) > 253 || net.ParseIP(value) != nil || !strings.Contains(value, ".") {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				return false
			}
		}
	}
	return true
}
