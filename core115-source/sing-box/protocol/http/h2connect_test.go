package http

import (
	"bytes"
	"context"
	stdtls "crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	boxtls "github.com/sagernet/sing-box/common/tls"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json/badoption"
	M "github.com/sagernet/sing/common/metadata"

	"github.com/stretchr/testify/require"

	"golang.org/x/net/http2"
)

type h2TestCheck func(r *http.Request) int

func startH2ConnectTestServer(t *testing.T, check h2TestCheck) (addr M.Socksaddr, certPool *x509.CertPool) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })

	cert, err := boxtls.GenerateKeyPair(nil, nil, time.Now, "example.org")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodConnect {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			if check != nil {
				if status := check(r); status != 0 {
					w.WriteHeader(status)
					return
				}
			}
			w.WriteHeader(http.StatusOK)
			flusher, canFlush := w.(http.Flusher)
			if canFlush {
				flusher.Flush()
			}
			// echo tunnel data back
			buffer := make([]byte, 8192)
			for {
				n, err := r.Body.Read(buffer)
				if n > 0 {
					if _, wErr := w.Write(buffer[:n]); wErr != nil {
						return
					}
					if canFlush {
						flusher.Flush()
					}
				}
				if err != nil {
					return
				}
			}
		}),
		TLSConfig: &stdtls.Config{
			Certificates: []stdtls.Certificate{*cert},
			NextProtos:   []string{"h2", "http/1.1"},
		},
	}
	if err = http2.ConfigureServer(server, &http2.Server{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
	go server.ServeTLS(listener, "", "")

	socksaddr := M.ParseSocksaddr(listener.Addr().String())
	pool := x509.NewCertPool()
	pool.AddCert(cert.Leaf)
	return socksaddr, pool
}

func dialTestTLS(t *testing.T, server M.Socksaddr, pool *x509.CertPool) *stdtls.Conn {
	t.Helper()
	rawConn, err := net.Dial("tcp", server.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rawConn.Close() })
	tlsConn := stdtls.Client(rawConn, &stdtls.Config{
		ServerName: "example.org",
		RootCAs:    pool,
		NextProtos: []string{"h2", "http/1.1"},
	})
	if err = tlsConn.HandshakeContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	return tlsConn
}

func TestDialH2Connect(t *testing.T) {
	server, certPool := startH2ConnectTestServer(t, nil)

	tlsConn := dialTestTLS(t, server, certPool)
	if tlsConn.ConnectionState().NegotiatedProtocol != "h2" {
		t.Fatal("expected h2 negotiation, got ", tlsConn.ConnectionState().NegotiatedProtocol)
	}

	destination := M.ParseSocksaddr("example.com:443")
	conn, err := dialH2Connect(context.Background(), tlsConn, destination, "", "user", "pass", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if _, err = conn.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	readErr := make(chan error, 1)
	go func() {
		buffer := make([]byte, 8192)
		n, err := conn.Read(buffer)
		if err != nil {
			readErr <- err
			return
		}
		if !bytes.Equal(buffer[:n], []byte("ping")) {
			readErr <- io.ErrUnexpectedEOF
			return
		}
		readErr <- nil
	}()
	select {
	case err = <-readErr:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for echo")
	}
}

func TestDialH2ConnectRejected(t *testing.T) {
	server, certPool := startH2ConnectTestServer(t, func(r *http.Request) int {
		if r.Header.Get("Proxy-Authorization") != "Basic dXNlcjpwYXNz" {
			return http.StatusProxyAuthRequired
		}
		return 0
	})

	tlsConn := dialTestTLS(t, server, certPool)

	destination := M.ParseSocksaddr("example.com:443")
	_, err := dialH2Connect(context.Background(), tlsConn, destination, "", "user", "wrong", nil)
	if err == nil {
		t.Fatal("expected authentication error")
	}
	if err.Error() != "authentication required" {
		t.Fatal("unexpected error: ", err)
	}

	tlsConn2 := dialTestTLS(t, server, certPool)
	conn, err := dialH2Connect(context.Background(), tlsConn2, destination, "", "user", "pass", nil)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
}

func TestDialH2ConnectHostOverride(t *testing.T) {
	server, certPool := startH2ConnectTestServer(t, func(r *http.Request) int {
		if r.Host != "override.example.org" {
			return http.StatusForbidden
		}
		return 0
	})

	tlsConn := dialTestTLS(t, server, certPool)

	destination := M.ParseSocksaddr("example.com:443")
	conn, err := dialH2Connect(context.Background(), tlsConn, destination, "", "", "", nil)
	if err == nil {
		conn.Close()
		t.Fatal("expected rejection without host override")
	}

	tlsConn2 := dialTestTLS(t, server, certPool)
	conn, err = dialH2Connect(context.Background(), tlsConn2, destination, "override.example.org", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
}

func TestDialH2ConnectHopByHopHeaders(t *testing.T) {
	server, certPool := startH2ConnectTestServer(t, func(r *http.Request) int {
		if r.Header.Get("X-Custom") != "value" {
			return http.StatusForbidden
		}
		return 0
	})

	tlsConn := dialTestTLS(t, server, certPool)

	destination := M.ParseSocksaddr("example.com:443")
	headers := http.Header{
		"Connection": []string{"keep-alive"},
		"Keep-Alive": []string{"timeout=60"},
		"X-Custom":   []string{"value"},
	}
	conn, err := dialH2Connect(context.Background(), tlsConn, destination, "", "", "", headers)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
}

func newTestHTTPOutboundOptions(tlsOptions *option.OutboundTLSOptions) option.HTTPOutboundOptions {
	return option.HTTPOutboundOptions{
		ServerOptions: option.ServerOptions{
			Server:     "127.0.0.1",
			ServerPort: 1080,
		},
		OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
			TLS: tlsOptions,
		},
	}
}

func TestOutboundDefaultALPN(t *testing.T) {
	tlsOptions := &option.OutboundTLSOptions{Enabled: true}
	_, err := NewOutbound(context.Background(), nil, log.NewNOPFactory().Logger(), "http-out", newTestHTTPOutboundOptions(tlsOptions))
	require.NoError(t, err)
	require.Equal(t, badoption.Listable[string]{"h2", "http/1.1"}, tlsOptions.ALPN)

	tlsOptions = &option.OutboundTLSOptions{Enabled: true, ALPN: badoption.Listable[string]{"h3"}}
	_, err = NewOutbound(context.Background(), nil, log.NewNOPFactory().Logger(), "http-out", newTestHTTPOutboundOptions(tlsOptions))
	require.NoError(t, err)
	require.Equal(t, badoption.Listable[string]{"h3"}, tlsOptions.ALPN)

	options := newTestHTTPOutboundOptions(nil)
	_, err = NewOutbound(context.Background(), nil, log.NewNOPFactory().Logger(), "http-out", options)
	require.NoError(t, err)
	require.Nil(t, options.TLS)
}
