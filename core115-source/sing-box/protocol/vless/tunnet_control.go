package vless

import (
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net"
	"strings"
	"time"

	"github.com/sagernet/sing-box/option"
	E "github.com/sagernet/sing/common/exceptions"
	"golang.org/x/net/http/httpguts"
)

type tunNetSnapshot struct {
	SchemaVersion       int               `json:"schema_version"`
	ClientID            string            `json:"client_id"`
	ActiveEntryNode     string            `json:"active_entry_node"`
	SelectedHostSlug    string            `json:"selected_host_slug"`
	ECHConfig           tunNetECHConfig   `json:"ech_config"`
	EntryNodes          []tunNetEntryNode `json:"entry_nodes"`
	Hosts               []tunNetHost      `json:"hosts"`
	RouteServer         string            `json:"validated_route_ip"`
	XHTTPAuthority      string            `json:"xhttp_authority"`
	XHTTPPath           string            `json:"xhttp_path"`
	XHTTPPathCandidates []string          `json:"xhttp_path_candidates"`
}

type tunNetECHConfig struct {
	ConfigList string         `json:"config_list"`
	ExpiresAt  tunNetUnixTime `json:"expires_at"`
}

type tunNetUnixTime struct {
	time.Time
}

func (t *tunNetUnixTime) UnmarshalJSON(content []byte) error {
	if string(content) == "null" || string(content) == `""` {
		t.Time = time.Time{}
		return nil
	}
	var milliseconds int64
	if err := json.Unmarshal(content, &milliseconds); err == nil {
		if milliseconds < 0 {
			return E.New("invalid TunNet timestamp")
		}
		t.Time = time.UnixMilli(milliseconds)
		return nil
	}
	var encoded string
	if err := json.Unmarshal(content, &encoded); err != nil {
		return E.Cause(err, "decode TunNet timestamp")
	}
	parsed, err := time.Parse(time.RFC3339Nano, encoded)
	if err != nil {
		return E.Cause(err, "decode TunNet timestamp")
	}
	t.Time = parsed
	return nil
}

func (t tunNetUnixTime) MarshalJSON() ([]byte, error) {
	if t.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(t.UnixMilli())
}

type tunNetEntryNode struct {
	Name       string           `json:"name"`
	IPv4       []string         `json:"ipv4"`
	FrontProxy tunNetFrontProxy `json:"front_proxy"`
}

type tunNetFrontProxy struct {
	Endpoint string            `json:"endpoint"`
	Headers  map[string]string `json:"headers"`
}

type tunNetHost struct {
	Slug               string `json:"slug"`
	Online             bool   `json:"online"`
	Authority          string `json:"authority"`
	Domain             string `json:"domain"`
	VLESSEncryptionKey string `json:"vless_encryption_key"`
}

func parseTunNetSnapshotResponse(content []byte) (*tunNetSnapshot, error) {
	var direct tunNetSnapshot
	if err := json.Unmarshal(content, &direct); err != nil {
		return nil, E.Cause(err, "decode TunNet snapshot")
	}
	if direct.SchemaVersion != 0 || direct.ClientID != "" || len(direct.EntryNodes) > 0 {
		if direct.SchemaVersion != 1 {
			return nil, E.New("unsupported TunNet snapshot schema_version: ", direct.SchemaVersion)
		}
		return &direct, nil
	}
	var envelope struct {
		Bootstrap struct {
			SchemaVersion int `json:"schema_version"`
			Runtime       struct {
				ActiveEntryNode string            `json:"active_entry_node"`
				EntryNodes      []tunNetEntryNode `json:"entry_nodes"`
				Hosts           []tunNetHost      `json:"hosts"`
			} `json:"runtime"`
		} `json:"bootstrap"`
		Persistent struct {
			ClientID  string          `json:"client_id"`
			ECHConfig tunNetECHConfig `json:"ech_config"`
		} `json:"persistent"`
		Preferences struct {
			EntryNode string `json:"entry_node"`
			HostSlug  string `json:"host_slug"`
		} `json:"preferences"`
	}
	if err := json.Unmarshal(content, &envelope); err != nil {
		return nil, E.Cause(err, "decode TunNet control response")
	}
	if envelope.Bootstrap.SchemaVersion != 0 && envelope.Bootstrap.SchemaVersion != 2 {
		return nil, E.New("unsupported TunNet control schema_version: ", envelope.Bootstrap.SchemaVersion)
	}
	return &tunNetSnapshot{
		SchemaVersion:    1,
		ClientID:         envelope.Persistent.ClientID,
		ActiveEntryNode:  firstNonEmpty(envelope.Bootstrap.Runtime.ActiveEntryNode, envelope.Preferences.EntryNode),
		SelectedHostSlug: envelope.Preferences.HostSlug,
		ECHConfig:        envelope.Persistent.ECHConfig,
		EntryNodes:       envelope.Bootstrap.Runtime.EntryNodes,
		Hosts:            envelope.Bootstrap.Runtime.Hosts,
	}, nil
}

func (s *tunNetSnapshot) resolve(now time.Time) (*option.VLESSTunNetResolvedOptions, error) {
	if s.ClientID == "" {
		return nil, E.New("TunNet snapshot has no client_id")
	}
	if s.ECHConfig.ConfigList == "" {
		return nil, E.New("TunNet snapshot has no ECH configuration")
	}
	if !s.ECHConfig.ExpiresAt.IsZero() && !now.Before(s.ECHConfig.ExpiresAt.Time) {
		return nil, E.New("TunNet ECH configuration expired")
	}
	var entry *tunNetEntryNode
	for index := range s.EntryNodes {
		if s.EntryNodes[index].Name == s.ActiveEntryNode {
			entry = &s.EntryNodes[index]
			break
		}
	}
	if entry == nil {
		return nil, E.New("TunNet active entry node not found: ", s.ActiveEntryNode)
	}
	var host *tunNetHost
	for index := range s.Hosts {
		if strings.EqualFold(s.Hosts[index].Slug, s.SelectedHostSlug) {
			host = &s.Hosts[index]
			break
		}
	}
	if host == nil {
		return nil, E.New("TunNet selected host not found: ", s.SelectedHostSlug)
	}
	if !host.Online {
		return nil, E.New("TunNet selected host is offline: ", s.SelectedHostSlug)
	}
	if entry.FrontProxy.Endpoint == "" {
		return nil, E.New("TunNet active entry has no front proxy endpoint")
	}
	routeServer := s.RouteServer
	if routeServer != "" {
		if net.ParseIP(routeServer) == nil {
			return nil, E.New("TunNet snapshot has invalid validated_route_ip")
		}
		routeServer = net.JoinHostPort(routeServer, "443")
	}
	if routeServer == "" && len(entry.IPv4) > 0 {
		if net.ParseIP(entry.IPv4[0]) == nil {
			return nil, E.New("TunNet active entry has invalid route target")
		}
		routeServer = net.JoinHostPort(entry.IPv4[0], "443")
	}
	if routeServer == "" {
		return nil, E.New("TunNet active entry has no route target")
	}
	tlsAuthority := firstNonEmpty(host.Authority, host.Domain)
	if tlsAuthority == "" {
		return nil, E.New("TunNet selected host has no authority")
	}
	xhttpAuthority := firstNonEmpty(s.XHTTPAuthority, tlsAuthority)
	if !httpguts.ValidHostHeader(tlsAuthority) || !httpguts.ValidHostHeader(xhttpAuthority) {
		return nil, E.New("TunNet snapshot has invalid authority")
	}
	xhttpPath := s.XHTTPPath
	if xhttpPath == "" {
		xhttpPath = "/api/v1/sync/"
	}
	key, err := decodeBase64Any(host.VLESSEncryptionKey)
	if err != nil || len(key) != 32 {
		return nil, E.New("TunNet selected host has invalid VLESS encryption key")
	}
	ech, err := decodeBase64Any(s.ECHConfig.ConfigList)
	if err != nil || len(ech) == 0 {
		return nil, E.New("TunNet snapshot has invalid ECH configuration")
	}
	echPEM := pem.EncodeToMemory(&pem.Block{Type: "ECH CONFIGS", Bytes: ech})
	if len(echPEM) == 0 {
		return nil, E.New("TunNet snapshot ECH PEM encoding failed")
	}
	endpoint, err := parseTunNetFrontProxyEndpoint(entry.FrontProxy.Endpoint)
	if err != nil || endpoint.Port == 0 {
		return nil, E.New("TunNet active entry has invalid front proxy endpoint")
	}
	return &option.VLESSTunNetResolvedOptions{
		UUID:               s.ClientID,
		FrontProxyEndpoint: endpoint.String(),
		FrontProxyHeaders:  cloneStringMap(entry.FrontProxy.Headers),
		RouteServer:        routeServer,
		InnerSNI:           tlsAuthority,
		InnerAuthority:     xhttpAuthority,
		ECHConfig:          []string{strings.TrimSpace(string(echPEM))},
		XHTTPPath:          xhttpPath,
		VLESSEncryption:    "mlkem768x25519plus.native.0rtt." + base64.RawURLEncoding.EncodeToString(key) + ".100-35-35",
	}, nil
}

func decodeBase64Any(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	for _, encoding := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.StdEncoding} {
		if decoded, err := encoding.DecodeString(value); err == nil {
			return decoded, nil
		}
	}
	return nil, E.New("invalid base64")
}

func cloneStringMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
