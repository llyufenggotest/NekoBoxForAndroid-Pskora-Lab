package vless

import (
	std_bufio "bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/sagernet/sing-box/option"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/stretchr/testify/require"
)

func TestTunNetStrictCONNECTFixture(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() { _ = serverConn.Close() })
	underlying := &tunNetTestDialer{conn: clientConn}
	dialer, err := wrapTunNetDialer(underlying, &option.VLESSTunNetOptions{
		FrontProxyEndpoint: "front.invalid:8443",
		FrontProxyHeaders: map[string]string{
			"Host":      "fixed-host.invalid",
			"X-T5-Auth": "fixture-value",
		},
		FrontProxyStrict: true,
		RouteServer:      "route.invalid:443",
	})
	require.NoError(t, err)

	const fixture = "CONNECT route.invalid:443 HTTP/1.1\r\n" +
		"Host: fixed-host.invalid\r\n" +
		"X-T5-Auth: fixture-value\r\n\r\n"
	serverErr := make(chan error, 1)
	go func() {
		request, err := readTunNetRequest(serverConn)
		if err == nil && request != fixture {
			err = &tunNetFixtureError{got: request, want: fixture}
		}
		if err == nil {
			_, err = io.WriteString(serverConn, "HTTP/1.1 200 Connection Established\r\n\r\n")
		}
		serverErr <- err
	}()

	conn, err := dialer.DialContext(context.Background(), "tcp", M.ParseSocksaddr("ignored.invalid:443"))
	require.NoError(t, err)
	require.NoError(t, <-serverErr)
	require.Equal(t, "tcp", underlying.network)
	require.Equal(t, "front.invalid:8443", underlying.destination.String())
	require.NoError(t, conn.Close())
}

func TestTunNetCONNECTPreservesBufferedResponse(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() { _ = serverConn.Close() })
	dialer, err := wrapTunNetDialer(&tunNetTestDialer{conn: clientConn}, &option.VLESSTunNetOptions{
		FrontProxyEndpoint: "front.invalid:8443",
		FrontProxyHeaders: map[string]string{
			"Host":      "fixed-host.invalid",
			"X-T5-Auth": "fixture-token",
		},
		FrontProxyStrict: true,
		RouteServer:      "route.invalid:443",
	})
	require.NoError(t, err)

	const payload = "prefetched-inner-bytes"
	serverErr := make(chan error, 1)
	go func() {
		reader := std_bufio.NewReader(serverConn)
		var request strings.Builder
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				serverErr <- err
				return
			}
			request.WriteString(line)
			if line == "\r\n" {
				break
			}
		}
		requestText := request.String()
		if !strings.HasPrefix(requestText, "CONNECT route.invalid:443 HTTP/1.1\r\n") || !strings.Contains(requestText, "\r\nHost: fixed-host.invalid\r\n") {
			serverErr <- fmt.Errorf("unexpected CONNECT request")
			return
		}
		_, err := io.WriteString(serverConn, "HTTP/1.1 200 Connection Established\r\nContent-Length: 0\r\n\r\n"+payload)
		serverErr <- err
	}()

	conn, err := dialer.DialContext(context.Background(), "tcp", M.ParseSocksaddr("ignored.invalid:443"))
	require.NoError(t, err)
	actual := make([]byte, len(payload))
	_, err = io.ReadFull(conn, actual)
	require.NoError(t, err)
	require.Equal(t, payload, string(actual))
	require.NoError(t, <-serverErr)
	require.NoError(t, conn.Close())
}

func TestTunNetCONNECTRejectsNon200(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() { _ = serverConn.Close() })
	dialer, err := wrapTunNetDialer(&tunNetTestDialer{conn: clientConn}, &option.VLESSTunNetOptions{
		FrontProxyEndpoint: "front.invalid:8443",
		FrontProxyHeaders: map[string]string{
			"Host":      "fixed-host.invalid",
			"X-T5-Auth": "fixture-token",
		},
		FrontProxyStrict: true,
		RouteServer:      "route.invalid:443",
	})
	require.NoError(t, err)

	go func() {
		reader := std_bufio.NewReader(serverConn)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			if line == "\r\n" {
				break
			}
		}
		_, _ = io.WriteString(serverConn, "HTTP/1.1 407 Proxy Authentication Required\r\nContent-Length: 0\r\n\r\n")
	}()

	conn, err := dialer.DialContext(context.Background(), "tcp", M.ParseSocksaddr("ignored.invalid:443"))
	require.Nil(t, conn)
	require.Error(t, err)
	require.Contains(t, err.Error(), "407 Proxy Authentication Required")
}

func TestTunNetOptionsReuseExistingLayers(t *testing.T) {
	options := option.VLESSOutboundOptions{
		ServerOptions: option.ServerOptions{
			Server:     "original.invalid",
			ServerPort: 443,
		},
		OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
			TLS: &option.OutboundTLSOptions{Enabled: true},
		},
		Transport: &option.V2RayTransportOptions{
			Type: "xhttp",
		},
		TunNet: &option.VLESSTunNetOptions{
			FrontProxyEndpoint: "front.invalid:8443",
			FrontProxyHeaders: map[string]string{
				"Host":      "fixed-host.invalid",
				"X-T5-Auth": "fixture-token",
			},
			FrontProxyStrict: true,
			RouteServer:      "route.invalid:443",
			InnerSNI:         "inner.invalid",
			InnerAuthority:   "authority.invalid",
			ECHConfig:        []string{"fixture-ech-config"},
			XHTTPPath:        "/tunnet-fixture",
			VLESSEncryption:  "fixture-encryption-expression",
		},
	}

	remoteIsDomain, err := applyTunNetOptions(&options)
	require.NoError(t, err)
	require.True(t, remoteIsDomain)
	require.Equal(t, "route.invalid", options.Server)
	require.Equal(t, uint16(443), options.ServerPort)
	require.Equal(t, "inner.invalid", options.TLS.ServerName)
	require.True(t, options.TLS.ECH.Enabled)
	require.Equal(t, []string{"fixture-ech-config"}, []string(options.TLS.ECH.Config))
	require.Equal(t, "authority.invalid", options.Transport.XHTTPOptions.Host)
	require.Equal(t, "/tunnet-fixture", options.Transport.XHTTPOptions.Path)
	require.Equal(t, "stream-up", options.Transport.XHTTPOptions.Mode)
	require.Equal(t, "Base62", options.Transport.XHTTPOptions.SessionIDTable)
	require.Equal(t, int32(16), options.Transport.XHTTPOptions.SessionIDLength.From)
	require.Equal(t, int32(24), options.Transport.XHTTPOptions.SessionIDLength.To)
	require.Equal(t, int32(100), options.Transport.XHTTPOptions.XPaddingBytes.From)
	require.Equal(t, int32(1000), options.Transport.XHTTPOptions.XPaddingBytes.To)
	require.True(t, options.Transport.XHTTPOptions.XPaddingObfsMode)
	require.Equal(t, "cache", options.Transport.XHTTPOptions.XPaddingKey)
	require.Equal(t, "Referer", options.Transport.XHTTPOptions.XPaddingHeader)
	require.Equal(t, option.PlacementQueryInHeader, options.Transport.XHTTPOptions.XPaddingPlacement)
	require.Equal(t, "tokenish", options.Transport.XHTTPOptions.XPaddingMethod)
	require.Equal(t, "xtls-rprx-vision", options.Flow)
	require.Equal(t, "fixture-encryption-expression", options.Encryption)
}
func TestTunNetCONNECTRejectsInjectedHeader(t *testing.T) {
	underlying := &tunNetTestDialer{}
	dialer, err := wrapTunNetDialer(underlying, &option.VLESSTunNetOptions{
		FrontProxyEndpoint: "front.invalid:8443",
		FrontProxyHeaders: map[string]string{
			"X-Fixture": "fixture-value\r\nInjected: value",
		},
		FrontProxyStrict: true,
		RouteServer:      "route.invalid:443",
	})
	require.Nil(t, dialer)
	require.ErrorContains(t, err, "invalid TunNet front proxy header value")
	require.Zero(t, underlying.dialCalls)
}
func TestTunNetCONNECTRequiresHostAndAuth(t *testing.T) {
	underlying := &tunNetTestDialer{}
	dialer, err := wrapTunNetDialer(underlying, &option.VLESSTunNetOptions{
		FrontProxyEndpoint: "front.invalid:8443",
		FrontProxyStrict:   true,
		RouteServer:        "route.invalid:443",
	})
	require.Nil(t, dialer)
	require.ErrorContains(t, err, "requires Host and X-T5-Auth headers")
	require.Zero(t, underlying.dialCalls)
}
func TestTunNetStrictModeIsRequired(t *testing.T) {
	underlying := &tunNetTestDialer{}
	dialer, err := wrapTunNetDialer(underlying, &option.VLESSTunNetOptions{
		FrontProxyEndpoint: "front.invalid:8443",
		FrontProxyHeaders: map[string]string{
			"Host":      "fixed-host.invalid",
			"X-T5-Auth": "fixture-token",
		},
		RouteServer: "route.invalid:443",
	})
	require.Nil(t, dialer)
	require.ErrorContains(t, err, "requires strict mode")
	require.Zero(t, underlying.dialCalls)
}
func TestTunNetNilConfigurationDoesNotWrapDialer(t *testing.T) {
	underlying := &tunNetTestDialer{}
	dialer, err := wrapTunNetDialer(underlying, nil)
	require.NoError(t, err)
	require.Same(t, underlying, dialer)

	options := option.VLESSOutboundOptions{
		ServerOptions: option.ServerOptions{Server: "ordinary.invalid", ServerPort: 443},
	}
	before := options
	remoteIsDomain, err := applyTunNetOptions(&options)
	require.NoError(t, err)
	require.True(t, remoteIsDomain)
	require.Equal(t, before, options)
	require.Nil(t, options.TunNet)
	require.Zero(t, underlying.dialCalls)
}

type tunNetTestDialer struct {
	mu          sync.Mutex
	conn        net.Conn
	network     string
	destination M.Socksaddr
	dialCalls   int
}

func (d *tunNetTestDialer) DialContext(_ context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.network = network
	d.destination = destination
	d.dialCalls++
	return d.conn, nil
}

func (d *tunNetTestDialer) ListenPacket(context.Context, M.Socksaddr) (net.PacketConn, error) {
	return nil, net.ErrClosed
}

func readTunNetRequest(conn net.Conn) (string, error) {
	reader := std_bufio.NewReader(conn)
	var request strings.Builder
	for {
		line, err := reader.ReadString('\n')
		request.WriteString(line)
		if err != nil {
			return "", err
		}
		if line == "\r\n" {
			return request.String(), nil
		}
	}
}

type tunNetFixtureError struct {
	got  string
	want string
}

func (e *tunNetFixtureError) Error() string {
	return "CONNECT fixture mismatch: got " + strings.ReplaceAll(e.got, "\r\n", "\\r\\n") +
		", want " + strings.ReplaceAll(e.want, "\r\n", "\\r\\n")
}
