package xhttp

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	M "github.com/sagernet/sing/common/metadata"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
)

type controlFixtureDialer struct {
	dial func(context.Context, string) (net.Conn, error)
}

func (d controlFixtureDialer) DialContext(ctx context.Context, _ string, addr M.Socksaddr) (net.Conn, error) {
	return d.dial(ctx, addr.String())
}
func (d controlFixtureDialer) ListenPacket(context.Context, M.Socksaddr) (net.PacketConn, error) {
	return nil, errors.New("fixture unsupported")
}

func controlFixtureCertificate(t *testing.T, names ...string) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "control offline fixture"}, DNSNames: names, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	require.NoError(t, err)
	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})))
	return cert, pool
}

func TestControlStrictTLSOriginAndFallback(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		t.Run(map[bool]string{false: "preferred", true: "safe-fallback"}[fallback], func(t *testing.T) {
			cert, pool := controlFixtureCertificate(t, controlCDNHost, controlLegacyHost)
			var calls, posts atomic.Int32
			body := controlDocumentResponse(t, controlSelectedDocument)
			expected := controlCDNHost
			if fallback {
				expected = controlLegacyHost
			}
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				posts.Add(1)
				require.Equal(t, expected, r.Host)
				require.Equal(t, expected, r.TLS.ServerName)
				require.Equal(t, 2, r.ProtoMajor)
				_, _ = io.Copy(io.Discard, r.Body)
				_, _ = w.Write(body)
			}))
			server.EnableHTTP2 = true
			server.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
			server.StartTLS()
			defer server.Close()
			dialer := controlFixtureDialer{dial: func(ctx context.Context, addr string) (net.Conn, error) {
				n := calls.Add(1)
				deadline, ok := ctx.Deadline()
				require.True(t, ok)
				require.LessOrEqual(t, time.Until(deadline), controlTotalTimeout)
				if fallback && n == 1 {
					require.LessOrEqual(t, time.Until(deadline), controlConnectTimeout)
					return nil, &net.DNSError{Err: "offline fixture", Name: "fixture.invalid", IsNotFound: true}
				}
				require.Equal(t, net.JoinHostPort(expected, "443"), addr)
				return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
			}}
			client := newControlClient(dialer)
			client.Transport.(*http.Transport).TLSClientConfig.RootCAs = pool
			configs, err := fetchDynamicConfig(context.Background(), client, "ignored label", "id-fixture", "token-fixture", "device-fixture", "tablet-fixture")
			require.NoError(t, err)
			require.Len(t, configs, 2)
			require.Equal(t, int32(1), posts.Load())
			expectedCalls := int32(1)
			if fallback {
				expectedCalls = 2
			}
			require.Equal(t, expectedCalls, calls.Load())
		})
	}
}

func TestControlTLSMismatchNeverFallsBack(t *testing.T) {
	cert, pool := controlFixtureCertificate(t, "wrong.invalid")
	var calls, posts atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { posts.Add(1) }))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	server.StartTLS()
	defer server.Close()
	dialer := controlFixtureDialer{dial: func(ctx context.Context, _ string) (net.Conn, error) {
		calls.Add(1)
		return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
	}}
	client := newControlClient(dialer)
	client.Transport.(*http.Transport).TLSClientConfig.RootCAs = pool
	_, err := fetchDynamicConfig(context.Background(), client, "ignored", "id", "token", "device", "tablet")
	require.Error(t, err)
	require.Equal(t, int32(1), calls.Load())
	require.Zero(t, posts.Load())
	require.NotContains(t, err.Error(), "wrong.invalid")
}

func TestControlRedirectNeverForwardsIdentity(t *testing.T) {
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: 307, Header: http.Header{"Location": []string{"https://untrusted.invalid/private"}}, Body: io.NopCloser(strings.NewReader("fixture")), Request: r}, nil
	})}
	_, err := fetchDynamicConfig(context.Background(), client, "ignored", "id", "token", "device", "tablet")
	require.Error(t, err)
	require.Equal(t, int32(1), calls.Load())
}

func TestControlOriginProofFailClosed(t *testing.T) {
	for name, err := range map[string]error{
		"dns":    &net.DNSError{Err: "offline", IsNotFound: true},
		"socket": &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED},
		"opaque": errors.New("remote proxy DNS business failure"),
		"cancel": context.Canceled,
	} {
		t.Run(name, func(t *testing.T) {
			a := &blackstoneOriginAttempt{}
			_, _ = a.dial(context.Background(), "tcp", "fixture.invalid:443", func(context.Context, string, string) (net.Conn, error) { return nil, err })
			require.Equal(t, name == "dns" || name == "socket", a.finish(context.Background()))
			require.False(t, a.finish(context.Background()), "proof is single-use")
		})
	}
	for _, event := range []string{"socket", "gotconn", "headers", "request", "active"} {
		t.Run(event, func(t *testing.T) {
			a := &blackstoneOriginAttempt{failed: 1}
			switch event {
			case "socket":
				left, right := net.Pipe()
				defer left.Close()
				defer right.Close()
				_, _ = a.dial(context.Background(), "tcp", "fixture", func(context.Context, string, string) (net.Conn, error) { return left, nil })
			case "gotconn":
				a.trace().GotConn(httptrace.GotConnInfo{})
			case "headers":
				a.trace().WroteHeaders()
			case "request":
				a.trace().WroteRequest(httptrace.WroteRequestInfo{})
			case "active":
				a.active = 1
			}
			require.False(t, a.finish(context.Background()))
		})
	}
}

func TestControlLateDialSocketClosed(t *testing.T) {
	a := &blackstoneOriginAttempt{}
	entered, release := make(chan struct{}), make(chan struct{})
	left, right := net.Pipe()
	defer right.Close()
	done := make(chan error, 1)
	go func() {
		_, err := a.dial(context.Background(), "tcp", "fixture", func(context.Context, string, string) (net.Conn, error) { close(entered); <-release; return left, nil })
		done <- err
	}()
	<-entered
	require.False(t, a.finish(context.Background()))
	close(release)
	require.ErrorIs(t, <-done, context.Canceled)
	_, err := right.Write([]byte{1})
	require.Error(t, err)
}

// The peer independently decodes HPACK and counts completed private POSTs.
// It refuses the fully received request at H2 level: no GetBody means no replay.
func TestControlH2RefusedStreamNoReplay(t *testing.T) {
	cert, pool := controlFixtureCertificate(t, controlCDNHost)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	var posts, bodies, dials atomic.Int32
	done := make(chan error, 1)
	go func() {
		raw, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer raw.Close()
		conn := tls.Server(raw, &tls.Config{Certificates: []tls.Certificate{cert}, NextProtos: []string{"h2"}})
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		if err = conn.Handshake(); err != nil {
			done <- err
			return
		}
		preface := make([]byte, len(http2.ClientPreface))
		if _, err = io.ReadFull(conn, preface); err != nil {
			done <- err
			return
		}
		fr := http2.NewFramer(conn, conn)
		fr.ReadMetaHeaders = hpack.NewDecoder(4096, nil)
		if err = fr.WriteSettings(); err != nil {
			done <- err
			return
		}
		for {
			frame, err := fr.ReadFrame()
			if err != nil {
				done <- nil
				return
			}
			switch f := frame.(type) {
			case *http2.SettingsFrame:
				if !f.IsAck() {
					_ = fr.WriteSettingsAck()
				}
			case *http2.MetaHeadersFrame:
				for _, h := range f.Fields {
					if h.Name == ":method" && h.Value == "POST" {
						posts.Add(1)
					}
				}
				if f.StreamEnded() {
					bodies.Add(1)
					_ = fr.WriteRSTStream(f.StreamID, http2.ErrCodeRefusedStream)
				}
			case *http2.DataFrame:
				if f.StreamEnded() {
					bodies.Add(1)
					_ = fr.WriteRSTStream(f.StreamID, http2.ErrCodeRefusedStream)
				}
			}
		}
	}()
	dialer := controlFixtureDialer{dial: func(ctx context.Context, _ string) (net.Conn, error) {
		dials.Add(1)
		return (&net.Dialer{}).DialContext(ctx, "tcp", listener.Addr().String())
	}}
	client := newControlClient(dialer)
	client.Transport.(*http.Transport).TLSClientConfig.RootCAs = pool
	_, err = fetchDynamicConfig(context.Background(), client, "ignored", "id", "token", "device", "tablet")
	require.Error(t, err)
	require.Equal(t, int32(1), posts.Load())
	require.Equal(t, int32(1), bodies.Load())
	require.Equal(t, int32(1), dials.Load())
	require.NoError(t, <-done)
}
