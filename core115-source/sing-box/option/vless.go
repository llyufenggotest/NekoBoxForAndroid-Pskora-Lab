package option

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/sagernet/sing/common/json"
	"github.com/sagernet/sing/common/json/badoption"
)

type VLESSInboundOptions struct {
	ListenOptions
	Users       []VLESSUser `json:"users,omitempty"`
	Decryption  string      `json:"decryption,omitempty"`
	XorMode     uint32      `json:"xor_mode,omitempty"`
	SecondsFrom int64       `json:"seconds_from,omitempty"`
	SecondsTo   int64       `json:"seconds_to,omitempty"`
	Padding     string      `json:"padding,omitempty"`
	InboundTLSOptionsContainer
	Multiplex *InboundMultiplexOptions `json:"multiplex,omitempty"`
	Transport *V2RayTransportOptions   `json:"transport,omitempty"`
}

type VLESSUser struct {
	Name string `json:"name"`
	UUID string `json:"uuid"`
	Flow string `json:"flow,omitempty"`
}

type VLESSOutboundOptions struct {
	DialerOptions
	ServerOptions
	UUID       string      `json:"uuid"`
	E          bool        `json:"e,omitempty"`
	H          string      `json:"h,omitempty"`
	Flow       string      `json:"flow,omitempty"`
	Encryption string      `json:"encryption,omitempty"`
	Network    NetworkList `json:"network,omitempty"`
	OutboundTLSOptionsContainer
	Multiplex      *OutboundMultiplexOptions `json:"multiplex,omitempty"`
	Transport      *V2RayTransportOptions    `json:"transport,omitempty"`
	PacketEncoding *string                   `json:"packet_encoding,omitempty"`
	TunNet         *VLESSTunNetOptions       `json:"tunnet,omitempty"`
}

// UnmarshalJSONContext preserves context-aware nested options. Strict private
// E/H checks occur before decoding can coerce null into a scalar zero value.
func (o *VLESSOutboundOptions) UnmarshalJSONContext(ctx context.Context, content []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(content, &fields); err != nil {
		return err
	}
	private := false
	for key, raw := range fields {
		if strings.EqualFold(key, "uuid") {
			var id string
			_ = json.Unmarshal(raw, &id)
			private = private || strings.Contains(strings.ToLower(id), "#blackstone")
		}
	}
	if private {
		for key, raw := range fields {
			if !strings.EqualFold(key, "e") && !strings.EqualFold(key, "h") {
				continue
			}
			if key != "e" && key != "h" {
				return fmt.Errorf("Blackstone: exact E/H field names required")
			}
			value := bytes.TrimSpace(raw)
			if key == "e" {
				if !bytes.Equal(value, []byte("true")) && !bytes.Equal(value, []byte("false")) {
					return fmt.Errorf("Blackstone: E must be a boolean")
				}
			} else {
				var h string
				if len(value) == 0 || value[0] != '"' || json.Unmarshal(value, &h) != nil {
					return fmt.Errorf("Blackstone: H must be a string")
				}
			}
		}
	}
	type plain VLESSOutboundOptions
	return json.UnmarshalContextDisallowUnknownFields(ctx, content, (*plain)(o))
}

func (o *VLESSOutboundOptions) UnmarshalJSON(content []byte) error {
	return o.UnmarshalJSONContext(context.Background(), content)
}

type VLESSTunNetOptions struct {
	Snapshot           string                     `json:"snapshot,omitempty"`
	FrontProxyEndpoint string                     `json:"front_proxy_endpoint,omitempty"`
	FrontProxyHeaders  map[string]string          `json:"front_proxy_headers,omitempty"`
	FrontProxyStrict   bool                       `json:"front_proxy_strict,omitempty"`
	RouteServer        string                     `json:"route_server,omitempty"`
	InnerSNI           string                     `json:"inner_sni,omitempty"`
	InnerAuthority     string                     `json:"inner_authority,omitempty"`
	ECHConfig          badoption.Listable[string] `json:"ech_config,omitempty"`
	XHTTPPath          string                     `json:"xhttp_path,omitempty"`
	VLESSEncryption    string                     `json:"vless_encryption,omitempty"`
}

type VLESSTunNetResolvedOptions struct {
	UUID               string
	FrontProxyEndpoint string
	FrontProxyHeaders  map[string]string
	RouteServer        string
	InnerSNI           string
	InnerAuthority     string
	ECHConfig          []string
	XHTTPPath          string
	VLESSEncryption    string
}
