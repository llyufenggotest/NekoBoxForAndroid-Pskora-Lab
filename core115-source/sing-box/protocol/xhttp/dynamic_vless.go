package xhttp

import (
	"encoding/json"
	"errors"
	"reflect"
	"strconv"

	"github.com/sagernet/sing-box/option"
)

// Mapping is explicit. Fields with no equivalent in sing are rejected, including
// inactive transport/TLS options; response policy is never merged into routing.
func dynamicVLESSOptions(cfg RealConfig) (option.VLESSOutboundOptions, error) {
	var out option.VLESSOutboundOptions
	fail := func() (option.VLESSOutboundOptions, error) {
		return out, errors.New("unsupported dynamic vless options")
	}
	v := cfg.VLESS
	if v == nil || (v.Flow != "" && v.Flow != "xtls-rprx-vision") {
		return fail()
	}
	// Revalidate original typed UUID before adding the exact private selector.
	id := map[string]any{"type": "vless", "server": cfg.Server, "port": cfg.Port, "uuid": v.UUID, "e": v.E, "h": v.H}
	if _, err := parseSelectedVLESS(id, cfg.Server, cfg.Port); err != nil {
		return fail()
	}
	if v.Server != cfg.Server || v.Port <= 0 || cfg.Port != fmtPort(v.Port) {
		return fail()
	}
	if v.PacketAddr || v.XUDP || (v.PacketEncoding != "" && v.PacketEncoding != "none") {
		return fail()
	}
	if v.Encryption != "" && v.Encryption != "none" {
		return fail()
	} // donor encryption is not sing's MLKEM contract
	if !reflect.ValueOf(v.ShadowTLSOpts).IsZero() || !reflect.ValueOf(v.RestlsOpts).IsZero() || !reflect.ValueOf(v.JLSOpts).IsZero() || v.NameCertVerify != "" || v.Fingerprint != "" || v.RealityOpts.SupportX25519MLKEM768 {
		return fail()
	}
	if !v.TLS && (v.Flow != "" || len(v.ALPN) > 0 || v.SkipCertVerify || v.ServerName != "" || v.Certificate != "" || v.PrivateKey != "" || v.ClientFingerprint != "" || !reflect.ValueOf(v.ECHOpts).IsZero() || !reflect.ValueOf(v.RealityOpts).IsZero()) {
		return fail()
	}
	empty := ""
	out.UUID = v.UUID + "#blackstone"
	out.E = v.E
	out.H = v.H
	out.Flow = v.Flow
	out.PacketEncoding = &empty
	if v.TLS {
		tls := &option.OutboundTLSOptions{Enabled: true, ServerName: v.ServerName, Insecure: v.SkipCertVerify, ALPN: v.ALPN}
		if v.PrivateKey != "" {
			if v.Certificate == "" {
				return fail()
			}
			tls.ClientCertificate = []string{v.Certificate}
			tls.ClientKey = []string{v.PrivateKey}
		} else if v.Certificate != "" {
			tls.Certificate = []string{v.Certificate}
		}
		if v.ClientFingerprint != "" {
			tls.UTLS = &option.OutboundUTLSOptions{Enabled: true, Fingerprint: v.ClientFingerprint}
		}
		if !reflect.ValueOf(v.ECHOpts).IsZero() {
			if !v.ECHOpts.Enable {
				return fail()
			}
			tls.ECH = &option.OutboundECHOptions{Enabled: true, QueryServerName: v.ECHOpts.QueryServerName}
			if v.ECHOpts.Config != "" {
				tls.ECH.Config = []string{v.ECHOpts.Config}
			}
		}
		if !reflect.ValueOf(v.RealityOpts).IsZero() {
			if v.RealityOpts.PublicKey == "" {
				return fail()
			}
			tls.Reality = &option.OutboundRealityOptions{Enabled: true, PublicKey: v.RealityOpts.PublicKey, ShortID: v.RealityOpts.ShortID}
		}
		out.TLS = tls
	}
	// Reject fields belonging to a different transport instead of silently dropping.
	active := *v
	active.Name = ""
	active.Server = ""
	active.Port = 0
	active.UUID = ""
	active.E = false
	active.H = ""
	active.Flow = ""
	active.TLS = false
	active.ALPN = nil
	active.UDP = false
	active.PacketEncoding = ""
	active.Encryption = ""
	active.Network = ""
	active.ECHOpts = BlackstoneECHOptions{}
	active.RealityOpts = BlackstoneRealityOptions{}
	active.SkipCertVerify = false
	active.Certificate = ""
	active.PrivateKey = ""
	active.ServerName = ""
	active.ClientFingerprint = ""
	tr := map[string]any{}
	switch v.Network {
	case "", "tcp":
	case "http":
		if len(v.HTTPOpts.Path) > 1 {
			return fail()
		}
		tr["type"] = "http"
		tr["method"] = v.HTTPOpts.Method
		tr["headers"] = v.HTTPOpts.Headers
		if len(v.HTTPOpts.Path) == 1 {
			tr["path"] = v.HTTPOpts.Path[0]
		}
		active.HTTPOpts = BlackstoneHTTPOptions{}
	case "h2":
		if !v.TLS {
			return fail()
		}
		tr["type"] = "http"
		tr["host"] = v.HTTP2Opts.Host
		tr["path"] = v.HTTP2Opts.Path
		active.HTTP2Opts = BlackstoneHTTP2Options{}
	case "ws":
		w := v.WSOpts
		if w.V2rayHttpUpgradeFastOpen || w.MaxEarlyData < 0 {
			return fail()
		}
		headers := map[string][]string{}
		for k, value := range v.WSHeaders {
			headers[k] = []string{value}
		}
		for k, value := range w.Headers {
			if old, ok := headers[k]; ok && old[0] != value {
				return fail()
			}
			headers[k] = []string{value}
		}
		if w.V2rayHttpUpgrade {
			if w.MaxEarlyData != 0 || w.EarlyDataHeaderName != "" {
				return fail()
			}
			tr["type"] = "httpupgrade"
		} else {
			tr["type"] = "ws"
			tr["max_early_data"] = w.MaxEarlyData
			tr["early_data_header_name"] = w.EarlyDataHeaderName
		}
		tr["path"] = w.Path
		tr["headers"] = headers
		active.WSOpts = BlackstoneWSOptions{}
		active.WSHeaders = nil
	case "grpc":
		g := v.GrpcOpts
		if g.GrpcUserAgent != "" || g.PingInterval != 0 || g.MaxConnections != 0 || g.MinStreams != 0 || g.MaxStreams != 0 {
			return fail()
		}
		tr["type"] = "grpc"
		tr["service_name"] = g.GrpcServiceName
		active.GrpcOpts = BlackstoneGrpcOptions{}
	case "xhttp":
		tr["type"] = "xhttp"
		x := v.XHTTPOpts
		for k, value := range dynamicXHTTPMap(x) {
			tr[k] = value
		}
		if x.DownloadSettings != nil {
			download, err := dynamicDownload(x.DownloadSettings, v)
			if err != nil {
				return fail()
			}
			tr["download"] = download
		}
		active.XHTTPOpts = BlackstoneXHTTPOptions{}
	default:
		return fail()
	}
	if !reflect.ValueOf(active).IsZero() {
		return fail()
	}
	if len(tr) > 0 {
		raw, err := json.Marshal(tr)
		if err != nil {
			return fail()
		}
		out.Transport = &option.V2RayTransportOptions{}
		if json.Unmarshal(raw, out.Transport) != nil {
			return fail()
		}
	}
	return out, nil
}

func dynamicXHTTPMap(x BlackstoneXHTTPOptions) map[string]any {
	result := map[string]any{"mode": x.Mode, "host": x.Host, "path": x.Path, "headers": x.Headers, "no_grpc_header": x.NoGRPCHeader, "x_padding_obfs_mode": x.XPaddingObfsMode, "x_padding_key": x.XPaddingKey, "x_padding_header": x.XPaddingHeader, "x_padding_placement": x.XPaddingPlacement, "x_padding_method": x.XPaddingMethod, "uplink_http_method": x.UplinkHTTPMethod, "session_placement": x.SessionPlacement, "session_key": x.SessionKey, "session_id_table": x.SessionTable, "seq_placement": x.SeqPlacement, "seq_key": x.SeqKey, "uplink_data_placement": x.UplinkDataPlacement, "uplink_data_key": x.UplinkDataKey}
	for k, value := range map[string]string{"x_padding_bytes": x.XPaddingBytes, "session_id_length": x.SessionLength, "uplink_chunk_size": x.UplinkChunkSize, "sc_max_each_post_bytes": x.ScMaxEachPostBytes, "sc_min_posts_interval_ms": x.ScMinPostsIntervalMs} {
		if value != "" {
			result[k] = value
		}
	}
	if x.ReuseSettings != nil {
		r := x.ReuseSettings
		xmux := map[string]any{"h_keep_alive_period": r.HKeepAlivePeriod}
		for k, v := range map[string]string{"max_concurrency": r.MaxConcurrency, "max_connections": r.MaxConnections, "c_max_reuse_times": r.CMaxReuseTimes, "h_max_request_times": r.HMaxRequestTimes, "h_max_reusable_secs": r.HMaxReusableSecs} {
			if v != "" {
				xmux[k] = v
			}
		}
		result["xmux"] = xmux
	}
	return result
}

func fmtPort(port int) string { return strconv.Itoa(port) }

func dynamicDownload(d *BlackstoneXHTTPDownloadSettings, parent *BlackstoneVLESSOptions) (map[string]any, error) {
	// Inherit TLS policy explicitly. No detour/routing field exists in the schema.
	v := *parent
	v.Network = "tcp"
	v.XHTTPOpts = BlackstoneXHTTPOptions{}
	if d.Server != nil {
		v.Server = *d.Server
	}
	if d.Port != nil {
		v.Port = *d.Port
	}
	if d.TLS != nil {
		v.TLS = *d.TLS
	}
	if d.ALPN != nil {
		v.ALPN = *d.ALPN
	}
	if d.ECHOpts != nil {
		v.ECHOpts = *d.ECHOpts
	}
	if d.ShadowTLSOpts != nil {
		v.ShadowTLSOpts = *d.ShadowTLSOpts
	}
	if d.RestlsOpts != nil {
		v.RestlsOpts = *d.RestlsOpts
	}
	if d.JLSOpts != nil {
		v.JLSOpts = *d.JLSOpts
	}
	if d.RealityOpts != nil {
		v.RealityOpts = *d.RealityOpts
	}
	if d.SkipCertVerify != nil {
		v.SkipCertVerify = *d.SkipCertVerify
	}
	if d.NameCertVerify != nil {
		v.NameCertVerify = *d.NameCertVerify
	}
	if d.Fingerprint != nil {
		v.Fingerprint = *d.Fingerprint
	}
	if d.Certificate != nil {
		v.Certificate = *d.Certificate
	}
	if d.PrivateKey != nil {
		v.PrivateKey = *d.PrivateKey
	}
	if d.ServerName != nil {
		v.ServerName = *d.ServerName
	}
	if d.ClientFingerprint != nil {
		v.ClientFingerprint = *d.ClientFingerprint
	}
	opts, err := dynamicVLESSOptions(RealConfig{Type: "vless", Server: v.Server, Port: fmtPort(v.Port), VLESS: &v})
	if err != nil {
		return nil, err
	}
	x := parent.XHTTPOpts
	x.DownloadSettings = nil
	if d.Path != nil {
		x.Path = *d.Path
	}
	if d.Host != nil {
		x.Host = *d.Host
	}
	if d.Headers != nil {
		x.Headers = *d.Headers
	}
	if d.ReuseSettings != nil {
		x.ReuseSettings = d.ReuseSettings
	}
	result := dynamicXHTTPMap(x)
	result["server"] = v.Server
	result["server_port"] = v.Port
	result["tls"] = opts.TLS
	return result, nil
}
