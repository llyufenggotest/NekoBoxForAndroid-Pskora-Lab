package xhttp

// Typed donor schema. No adapter import: parent maps this losslessly to its private constructor.
type BlackstoneVLESSOptions struct {
	Name   string `json:"name"`
	Server string `json:"server"`
	Port   int    `json:"port"`
	UUID   string `json:"uuid"`
	// E/H apply only to the exact UUID#blackstone private selector.
	E                 bool                       `json:"e,omitempty"`
	H                 string                     `json:"h,omitempty"`
	Flow              string                     `json:"flow,omitempty"`
	TLS               bool                       `json:"tls,omitempty"`
	ALPN              []string                   `json:"alpn,omitempty"`
	UDP               bool                       `json:"udp,omitempty"`
	PacketAddr        bool                       `json:"packet-addr,omitempty"`
	XUDP              bool                       `json:"xudp,omitempty"`
	PacketEncoding    string                     `json:"packet-encoding,omitempty"`
	Encryption        string                     `json:"encryption,omitempty"`
	Network           string                     `json:"network,omitempty"`
	ECHOpts           BlackstoneECHOptions       `json:"ech-opts,omitempty"`
	ShadowTLSOpts     BlackstoneShadowTLSOptions `json:"shadow-tls-opts,omitempty"`
	RestlsOpts        BlackstoneRestlsOptions    `json:"restls-opts,omitempty"`
	JLSOpts           BlackstoneJLSOptions       `json:"jls-opts,omitempty"`
	RealityOpts       BlackstoneRealityOptions   `json:"reality-opts,omitempty"`
	HTTPOpts          BlackstoneHTTPOptions      `json:"http-opts,omitempty"`
	HTTP2Opts         BlackstoneHTTP2Options     `json:"h2-opts,omitempty"`
	GrpcOpts          BlackstoneGrpcOptions      `json:"grpc-opts,omitempty"`
	WSOpts            BlackstoneWSOptions        `json:"ws-opts,omitempty"`
	XHTTPOpts         BlackstoneXHTTPOptions     `json:"xhttp-opts,omitempty"`
	WSHeaders         map[string]string          `json:"ws-headers,omitempty"`
	SkipCertVerify    bool                       `json:"skip-cert-verify,omitempty"`
	NameCertVerify    string                     `json:"name-cert-verify,omitempty"`
	Fingerprint       string                     `json:"fingerprint,omitempty"`
	Certificate       string                     `json:"certificate,omitempty"`
	PrivateKey        string                     `json:"private-key,omitempty"`
	ServerName        string                     `json:"servername,omitempty"`
	ClientFingerprint string                     `json:"client-fingerprint,omitempty"`
}

type BlackstoneHTTPOptions struct {
	Method  string              `json:"method,omitempty"`
	Path    []string            `json:"path,omitempty"`
	Headers map[string][]string `json:"headers,omitempty"`
}

type BlackstoneHTTP2Options struct {
	Host []string `json:"host,omitempty"`
	Path string   `json:"path,omitempty"`
}

type BlackstoneGrpcOptions struct {
	GrpcServiceName string `json:"grpc-service-name,omitempty"`
	GrpcUserAgent   string `json:"grpc-user-agent,omitempty"`
	PingInterval    int    `json:"ping-interval,omitempty"`
	MaxConnections  int    `json:"max-connections,omitempty"`
	MinStreams      int    `json:"min-streams,omitempty"`
	MaxStreams      int    `json:"max-streams,omitempty"`
}

type BlackstoneWSOptions struct {
	Path                     string            `json:"path,omitempty"`
	Headers                  map[string]string `json:"headers,omitempty"`
	MaxEarlyData             int               `json:"max-early-data,omitempty"`
	EarlyDataHeaderName      string            `json:"early-data-header-name,omitempty"`
	V2rayHttpUpgrade         bool              `json:"v2ray-http-upgrade,omitempty"`
	V2rayHttpUpgradeFastOpen bool              `json:"v2ray-http-upgrade-fast-open,omitempty"`
}

type BlackstoneXHTTPOptions struct {
	Path                 string                           `json:"path,omitempty"`
	Host                 string                           `json:"host,omitempty"`
	Mode                 string                           `json:"mode,omitempty"`
	Headers              map[string]string                `json:"headers,omitempty"`
	NoGRPCHeader         bool                             `json:"no-grpc-header,omitempty"`
	XPaddingBytes        string                           `json:"x-padding-bytes,omitempty"`
	XPaddingObfsMode     bool                             `json:"x-padding-obfs-mode,omitempty"`
	XPaddingKey          string                           `json:"x-padding-key,omitempty"`
	XPaddingHeader       string                           `json:"x-padding-header,omitempty"`
	XPaddingPlacement    string                           `json:"x-padding-placement,omitempty"`
	XPaddingMethod       string                           `json:"x-padding-method,omitempty"`
	UplinkHTTPMethod     string                           `json:"uplink-http-method,omitempty"`
	SessionPlacement     string                           `json:"session-placement,omitempty"`
	SessionKey           string                           `json:"session-key,omitempty"`
	SessionTable         string                           `json:"session-table,omitempty"`
	SessionLength        string                           `json:"session-length,omitempty"`
	SeqPlacement         string                           `json:"seq-placement,omitempty"`
	SeqKey               string                           `json:"seq-key,omitempty"`
	UplinkDataPlacement  string                           `json:"uplink-data-placement,omitempty"`
	UplinkDataKey        string                           `json:"uplink-data-key,omitempty"`
	UplinkChunkSize      string                           `json:"uplink-chunk-size,omitempty"`
	ScMaxEachPostBytes   string                           `json:"sc-max-each-post-bytes,omitempty"`
	ScMinPostsIntervalMs string                           `json:"sc-min-posts-interval-ms,omitempty"`
	ReuseSettings        *BlackstoneXHTTPReuseSettings    `json:"reuse-settings,omitempty"` // aka XMUX
	DownloadSettings     *BlackstoneXHTTPDownloadSettings `json:"download-settings,omitempty"`
}

type BlackstoneXHTTPReuseSettings struct {
	MaxConcurrency   string `json:"max-concurrency,omitempty"`
	MaxConnections   string `json:"max-connections,omitempty"`
	CMaxReuseTimes   string `json:"c-max-reuse-times,omitempty"`
	HMaxRequestTimes string `json:"h-max-request-times,omitempty"`
	HMaxReusableSecs string `json:"h-max-reusable-secs,omitempty"`
	HKeepAlivePeriod int    `json:"h-keep-alive-period,omitempty"`
}

type BlackstoneXHTTPDownloadSettings struct {
	// xhttp part
	Path          *string                       `json:"path,omitempty"`
	Host          *string                       `json:"host,omitempty"`
	Headers       *map[string]string            `json:"headers,omitempty"`
	ReuseSettings *BlackstoneXHTTPReuseSettings `json:"reuse-settings,omitempty"` // aka XMUX
	// proxy part
	Server            *string                     `json:"server,omitempty"`
	Port              *int                        `json:"port,omitempty"`
	TLS               *bool                       `json:"tls,omitempty"`
	ALPN              *[]string                   `json:"alpn,omitempty"`
	ECHOpts           *BlackstoneECHOptions       `json:"ech-opts,omitempty"`
	ShadowTLSOpts     *BlackstoneShadowTLSOptions `json:"shadow-tls-opts,omitempty"`
	RestlsOpts        *BlackstoneRestlsOptions    `json:"restls-opts,omitempty"`
	JLSOpts           *BlackstoneJLSOptions       `json:"jls-opts,omitempty"`
	RealityOpts       *BlackstoneRealityOptions   `json:"reality-opts,omitempty"`
	SkipCertVerify    *bool                       `json:"skip-cert-verify,omitempty"`
	NameCertVerify    *string                     `json:"name-cert-verify,omitempty"`
	Fingerprint       *string                     `json:"fingerprint,omitempty"`
	Certificate       *string                     `json:"certificate,omitempty"`
	PrivateKey        *string                     `json:"private-key,omitempty"`
	ServerName        *string                     `json:"servername,omitempty"`
	ClientFingerprint *string                     `json:"client-fingerprint,omitempty"`
}

type BlackstoneECHOptions struct {
	Enable bool   `json:"enable,omitempty"`
	Config string `json:"config,omitempty"`

	QueryServerName string `json:"query-server-name,omitempty"`
}

type BlackstoneRealityOptions struct {
	PublicKey string `json:"public-key"`
	ShortID   string `json:"short-id,omitempty"`

	SupportX25519MLKEM768 bool `json:"support-x25519mlkem768,omitempty"`
}

type BlackstoneJLSOptions struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type BlackstoneShadowTLSOptions struct {
	Password string `json:"password,omitempty"`
	Version  int    `json:"version,omitempty"`
}

type BlackstoneRestlsOptions struct {
	Password     string `json:"password,omitempty"`
	VersionHint  string `json:"version-hint,omitempty"`
	RestlsScript string `json:"restls-script,omitempty"`
}
