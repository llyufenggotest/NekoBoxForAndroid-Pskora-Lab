package option

import "github.com/sagernet/sing/common/json/badoption"

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
	Flow       string      `json:"flow,omitempty"`
	Encryption string      `json:"encryption,omitempty"`
	Network    NetworkList `json:"network,omitempty"`
	OutboundTLSOptionsContainer
	Multiplex      *OutboundMultiplexOptions `json:"multiplex,omitempty"`
	Transport      *V2RayTransportOptions    `json:"transport,omitempty"`
	PacketEncoding *string                   `json:"packet_encoding,omitempty"`
	TunNet         *VLESSTunNetOptions       `json:"tunnet,omitempty"`
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
