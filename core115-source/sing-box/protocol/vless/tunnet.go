package vless

import (
	std_bufio "bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	Xbadoption "github.com/sagernet/sing-box/common/xray/json/badoption"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/bufio"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"golang.org/x/net/http/httpguts"
)

var _ N.Dialer = (*tunNetDialer)(nil)

type tunNetDialer struct {
	dialer             N.Dialer
	frontProxyEndpoint M.Socksaddr
	routeServer        string
	hostHeader         string
	headers            []tunNetHeader
}

type tunNetHeader struct {
	name  string
	value string
}

func applyTunNetOptions(options *option.VLESSOutboundOptions) (bool, error) {
	tunNet := options.TunNet
	if tunNet == nil {
		return options.ServerIsDomain(), nil
	}
	if tunNet.Snapshot != "" {
		content, err := os.ReadFile(tunNet.Snapshot)
		if err != nil {
			return false, E.Cause(err, "read TunNet snapshot")
		}
		snapshot, err := parseTunNetSnapshotResponse(content)
		if err != nil {
			return false, err
		}
		resolved, err := snapshot.resolve(time.Now())
		if err != nil {
			return false, err
		}
		options.UUID = resolved.UUID
		tunNet.FrontProxyEndpoint = resolved.FrontProxyEndpoint
		tunNet.FrontProxyHeaders = resolved.FrontProxyHeaders
		tunNet.RouteServer = resolved.RouteServer
		tunNet.InnerSNI = resolved.InnerSNI
		tunNet.InnerAuthority = resolved.InnerAuthority
		tunNet.ECHConfig = resolved.ECHConfig
		tunNet.XHTTPPath = resolved.XHTTPPath
		tunNet.VLESSEncryption = resolved.VLESSEncryption
	}
	_, err := parseTunNetFrontProxyEndpoint(tunNet.FrontProxyEndpoint)
	if err != nil {
		return false, err
	}
	routeServer, err := parseTunNetEndpoint("route server", tunNet.RouteServer)
	if err != nil {
		return false, err
	}
	if tunNet.InnerAuthority != "" && !httpguts.ValidHostHeader(tunNet.InnerAuthority) {
		return false, E.New("invalid TunNet inner authority")
	}

	options.Server = routeServer.AddrString()
	options.ServerPort = routeServer.Port
	if options.TLS == nil {
		options.TLS = &option.OutboundTLSOptions{}
	}
	options.TLS.Enabled = true
	if tunNet.InnerSNI != "" {
		options.TLS.ServerName = tunNet.InnerSNI
	}
	if len(tunNet.ECHConfig) > 0 {
		options.TLS.ECH = &option.OutboundECHOptions{
			Enabled: true,
			Config:  tunNet.ECHConfig,
		}
	}
	if options.Transport == nil {
		options.Transport = &option.V2RayTransportOptions{Type: C.V2RayTransportTypeXHTTP}
	} else if options.Transport.Type != C.V2RayTransportTypeXHTTP {
		return false, E.New("TunNet requires XHTTP transport")
	}
	if tunNet.InnerAuthority != "" {
		options.Transport.XHTTPOptions.Host = tunNet.InnerAuthority
	}
	if tunNet.XHTTPPath != "" {
		options.Transport.XHTTPOptions.Path = tunNet.XHTTPPath
	}
	// TunNet's data plane requires the server-verified XHTTP stream-up profile.
	xhttp := &options.Transport.XHTTPOptions
	xhttp.Mode = "stream-up"
	xhttp.SessionIDTable = "Base62"
	xhttp.SessionIDLength = Xbadoption.Range{From: 16, To: 24}
	xhttp.XPaddingBytes = Xbadoption.Range{From: 100, To: 1000}
	xhttp.XPaddingObfsMode = true
	xhttp.XPaddingKey = "cache"
	xhttp.XPaddingHeader = "Referer"
	xhttp.XPaddingPlacement = option.PlacementQueryInHeader
	xhttp.XPaddingMethod = "tokenish"
	if tunNet.VLESSEncryption != "" {
		options.Encryption = tunNet.VLESSEncryption
	}
	options.Flow = "xtls-rprx-vision"
	return routeServer.IsFqdn(), nil
}

func wrapTunNetDialer(dialer N.Dialer, options *option.VLESSTunNetOptions) (N.Dialer, error) {
	if options == nil {
		return dialer, nil
	}
	if !options.FrontProxyStrict {
		return nil, E.New("TunNet front proxy requires strict mode")
	}
	frontProxyEndpoint, err := parseTunNetFrontProxyEndpoint(options.FrontProxyEndpoint)
	if err != nil {
		return nil, err
	}
	routeServer, err := parseTunNetAuthority("route server", options.RouteServer)
	if err != nil {
		return nil, err
	}
	hostHeader := ""
	authHeader := ""
	headerNamesLower := make(map[string]bool, len(options.FrontProxyHeaders))
	for name, value := range options.FrontProxyHeaders {
		nameLower := strings.ToLower(name)
		if !httpguts.ValidHeaderFieldName(name) || headerNamesLower[nameLower] {
			return nil, E.New("invalid TunNet front proxy header name: ", name)
		}
		if !httpguts.ValidHeaderFieldValue(value) {
			return nil, E.New("invalid TunNet front proxy header value for ", name)
		}
		headerNamesLower[nameLower] = true
		switch nameLower {
		case "host":
			if !httpguts.ValidHostHeader(value) {
				return nil, E.New("invalid TunNet front proxy Host header")
			}
			hostHeader = value
		case "x-t5-auth":
			if value == "" {
				return nil, E.New("invalid TunNet front proxy X-T5-Auth header")
			}
			authHeader = value
		default:
			return nil, E.New("unexpected TunNet front proxy header: ", name)
		}
	}
	if hostHeader == "" || authHeader == "" {
		return nil, E.New("TunNet front proxy requires Host and X-T5-Auth headers")
	}
	headers := []tunNetHeader{{name: "X-T5-Auth", value: authHeader}}
	return &tunNetDialer{
		dialer:             dialer,
		frontProxyEndpoint: frontProxyEndpoint,
		routeServer:        routeServer,
		hostHeader:         hostHeader,
		headers:            headers,
	}, nil
}

func parseTunNetFrontProxyEndpoint(endpoint string) (M.Socksaddr, error) {
	if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
		parsed, err := url.Parse(endpoint)
		if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return M.Socksaddr{}, E.New("invalid TunNet front proxy endpoint")
		}
		endpoint = parsed.Host
	}
	return parseTunNetEndpoint("front proxy endpoint", endpoint)
}

func parseTunNetEndpoint(name string, endpoint string) (M.Socksaddr, error) {
	authority := endpoint
	if strings.Contains(endpoint, "://") {
		parsed, err := url.Parse(endpoint)
		if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return M.Socksaddr{}, E.New("invalid TunNet ", name)
		}
		authority = parsed.Host
	}
	authority, err := parseTunNetAuthority(name, authority)
	if err != nil {
		return M.Socksaddr{}, err
	}
	return M.ParseSocksaddr(authority), nil
}

func parseTunNetAuthority(name string, authority string) (string, error) {
	if authority == "" || strings.TrimSpace(authority) != authority || strings.ContainsAny(authority, "\r\n") {
		return "", E.New("invalid TunNet ", name)
	}
	destination := M.ParseSocksaddr(authority)
	if !destination.IsValid() || destination.Port == 0 || destination.String() != authority {
		return "", E.New("invalid TunNet ", name, ": ", authority)
	}
	return authority, nil
}

func (d *tunNetDialer) DialContext(ctx context.Context, network string, _ M.Socksaddr) (net.Conn, error) {
	if N.NetworkName(network) != N.NetworkTCP {
		return nil, os.ErrInvalid
	}
	conn, err := d.dialer.DialContext(ctx, N.NetworkTCP, d.frontProxyEndpoint)
	if err != nil {
		return nil, err
	}
	conn, err = d.connect(ctx, conn)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func (d *tunNetDialer) connect(ctx context.Context, conn net.Conn) (net.Conn, error) {
	var request strings.Builder
	_, _ = fmt.Fprintf(&request, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n", d.routeServer, d.hostHeader)
	for _, header := range d.headers {
		_, _ = fmt.Fprintf(&request, "%s: %s\r\n", header.name, header.value)
	}
	request.WriteString("\r\n")

	cancelled := make(chan struct{})
	stopCancel := context.AfterFunc(ctx, func() {
		_ = conn.SetDeadline(time.Now())
		close(cancelled)
	})
	defer func() {
		if !stopCancel() {
			<-cancelled
		}
		_ = conn.SetDeadline(time.Time{})
	}()
	if _, err := io.Copy(conn, strings.NewReader(request.String())); err != nil {
		return conn, E.Cause(err, "write TunNet CONNECT request")
	}

	reader := std_bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil {
		return conn, E.Cause(err, "read TunNet CONNECT response")
	}
	if response.Body != nil {
		defer response.Body.Close()
	}
	if response.StatusCode != http.StatusOK {
		return conn, E.New("TunNet CONNECT unexpected status: ", response.Status)
	}
	if reader.Buffered() > 0 {
		buffer := buf.NewSize(reader.Buffered())
		if _, err = buffer.ReadFullFrom(reader, buffer.FreeLen()); err != nil {
			buffer.Release()
			return conn, E.Cause(err, "preserve TunNet CONNECT response bytes")
		}
		conn = bufio.NewCachedConn(conn, buffer)
	}
	return conn, nil
}

func (d *tunNetDialer) ListenPacket(context.Context, M.Socksaddr) (net.PacketConn, error) {
	return nil, os.ErrInvalid
}
