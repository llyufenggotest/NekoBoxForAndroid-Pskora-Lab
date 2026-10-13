package sip003

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509/pkix"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/transport/v2raywebsocket"
	"github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/smux"
	"github.com/sagernet/ws"
	"github.com/sagernet/ws/wsutil"
)

func TestGostOptionsNormalize(t *testing.T) {
	tests := []struct {
		name string
		opts string
		mode gostMode
		tls  bool
		mux  bool
	}{
		{"default", "", gostModeWebsocket, false, false},
		{"websocket", "mode=websocket", gostModeWebsocket, false, false},
		{"ws", "mode=ws", gostModeWebsocket, false, false},
		{"wss", "mode=wss", gostModeWebsocket, true, false},
		{"tls bool", "mode=websocket;tls=true", gostModeWebsocket, true, false},
		{"mws", "mode=mws", gostModeWebsocket, false, true},
		{"mwss", "mode=mwss", gostModeWebsocket, true, true},
		{"mux bool", "mode=websocket;mux=true", gostModeWebsocket, false, true},
		{"tls", "mode=tls", gostModeTLS, true, false},
		{"mtls", "mode=mtls", gostModeTLS, true, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			args, err := ParsePluginOptions(test.opts)
			if err != nil {
				t.Fatal(err)
			}
			options, err := parseGostOptions(args)
			if err != nil {
				t.Fatal(err)
			}
			if options.mode != test.mode || options.tls != test.tls || options.mux != test.mux {
				t.Fatalf("unexpected options: %+v", options)
			}
		})
	}
}

func TestGostOptionsRejectUnsupportedAndUnsafe(t *testing.T) {
	for _, opts := range []string{
		"mode=h2", "mode=quic", "mode=grpc", "mode=unknown",
		"mode=ws;ed=1024", "mode=ws;nocomp=false", "mode=ws;mux=maybe", "mode=ws;tls=maybe",
	} {
		t.Run(opts, func(t *testing.T) {
			args, err := ParsePluginOptions(opts)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = parseGostOptions(args); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestGostWebsocketHandshakeBinaryAndEndpointSeparation(t *testing.T) {
	var gotHost, gotPath string
	var gotExtensions string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		gotHost = request.Host
		gotPath = request.URL.Path
		gotExtensions = request.Header.Get("Sec-WebSocket-Extensions")
		conn, _, _, err := ws.UpgradeHTTP(request, writer)
		if err != nil {
			return
		}
		defer conn.Close()
		message, op, err := wsutil.ReadClientData(conn)
		if err != nil {
			return
		}
		if op != ws.OpBinary {
			return
		}
		_ = wsutil.WriteServerBinary(conn, message)
	}))
	defer server.Close()

	plugin := newGostForServer(t, server.Listener.Addr(), "mode=ws;host=front.invalid;path=/gost")
	conn, err := plugin.DialContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	assertEcho(t, conn, []byte("binary-stream"))
	if gotHost != "front.invalid" || gotPath != "/gost" {
		t.Fatalf("handshake host/path = %q %q", gotHost, gotPath)
	}
	if gotExtensions != "" {
		t.Fatalf("compression extension enabled: %q", gotExtensions)
	}
}

func TestGostWebsocketTLSUsesServerNameNotHost(t *testing.T) {
	certificate := selfSignedCertificate(t, "sni.invalid")
	var gotHost, gotSNI string
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		gotHost = request.Host
		conn, _, _, err := ws.UpgradeHTTP(request, writer)
		if err != nil {
			return
		}
		defer conn.Close()
		message, _, err := wsutil.ReadClientData(conn)
		if err == nil {
			_ = wsutil.WriteServerBinary(conn, message)
		}
	}))
	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{certificate},
		GetConfigForClient: func(info *tls.ClientHelloInfo) (*tls.Config, error) {
			gotSNI = info.ServerName
			return nil, nil
		},
	}
	server.StartTLS()
	defer server.Close()

	plugin := newGostForServer(t, server.Listener.Addr(), "mode=wss;host=front.invalid;serverName=sni.invalid;path=/secure;insecure")
	conn, err := plugin.DialContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	assertEcho(t, conn, []byte("tls-stream"))
	if gotHost != "front.invalid" || gotSNI != "sni.invalid" {
		t.Fatalf("host/SNI = %q %q", gotHost, gotSNI)
	}
}

func TestGostTLSStream(t *testing.T) {
	certificate := selfSignedCertificate(t, "tls.invalid")
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{certificate}})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go acceptEcho(listener, false)

	plugin := newGostForServer(t, listener.Addr(), "mode=tls;serverName=tls.invalid;insecure")
	conn, err := plugin.DialContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	assertEcho(t, conn, []byte("tls-only"))
}

func TestGostMuxSmuxV1ReuseAndReset(t *testing.T) {
	certificate := selfSignedCertificate(t, "mux.invalid")
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{certificate}})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	var accepts atomic.Int32
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			accepts.Add(1)
			go serveMuxEcho(conn)
		}
	}()

	plugin := newGostForServer(t, listener.Addr(), "mode=mtls;serverName=mux.invalid;insecure;mux=true")
	// The reference mtls wire is TLS + sagernet/xtaci smux protocol v1.
	for _, payload := range [][]byte{[]byte("one"), []byte("two")} {
		conn, err := plugin.DialContext(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		assertEcho(t, conn, payload)
		_ = conn.Close()
	}
	if accepts.Load() != 1 {
		t.Fatalf("expected one shared session, accepts=%d", accepts.Load())
	}
	plugin.CloseIdleConnections()
	conn, err := plugin.DialContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertEcho(t, conn, []byte("after-reset"))
	_ = conn.Close()
	if accepts.Load() != 2 {
		t.Fatalf("expected reset to redial, accepts=%d", accepts.Load())
	}
	_ = plugin.Close()
}

func TestGostCancelAndClose(t *testing.T) {
	dialer := &blockingDialer{started: make(chan struct{})}
	plugin, err := newGostPlugin(context.Background(), mustArgs(t, "mode=ws"), nil, dialer, metadata.ParseSocksaddr("cancel.invalid:443"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := plugin.DialContext(ctx)
		result <- err
	}()
	<-dialer.started
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("expected cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("dial did not cancel")
	}
	closer := plugin.(interface{ Close() error })
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := plugin.DialContext(context.Background()); err == nil {
		t.Fatal("dial after close should fail")
	}
}

func newGostForServer(t *testing.T, address net.Addr, options string) *gostPlugin {
	t.Helper()
	args := mustArgs(t, options)
	plugin, err := newGostPlugin(context.Background(), args, nil, stdDialer{}, metadata.ParseSocksaddr(address.String()))
	if err != nil {
		t.Fatal(err)
	}
	return plugin.(*gostPlugin)
}

func mustArgs(t *testing.T, value string) Args {
	t.Helper()
	args, err := ParsePluginOptions(value)
	if err != nil {
		t.Fatal(err)
	}
	return args
}

func assertEcho(t *testing.T, conn net.Conn, payload []byte) {
	t.Helper()
	if _, err := conn.Write(payload); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("echo = %q", got)
	}
}

type stdDialer struct{}

func (stdDialer) DialContext(ctx context.Context, network string, destination metadata.Socksaddr) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, network, destination.String())
}
func (stdDialer) ListenPacket(context.Context, metadata.Socksaddr) (net.PacketConn, error) {
	return nil, net.ErrClosed
}

type blockingDialer struct {
	once    sync.Once
	started chan struct{}
}

func (d *blockingDialer) DialContext(ctx context.Context, network string, destination metadata.Socksaddr) (net.Conn, error) {
	d.once.Do(func() { close(d.started) })
	<-ctx.Done()
	return nil, ctx.Err()
}
func (d *blockingDialer) ListenPacket(context.Context, metadata.Socksaddr) (net.PacketConn, error) {
	return nil, net.ErrClosed
}

func acceptEcho(listener net.Listener, mux bool) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		if mux {
			go serveMuxEcho(conn)
		} else {
			go func() { defer conn.Close(); _, _ = io.Copy(conn, conn) }()
		}
	}
}

func serveMuxEcho(conn net.Conn) {
	defer conn.Close()
	config := smux.DefaultConfig()
	config.Version = 1
	config.KeepAliveDisabled = true
	session, err := smux.Server(conn, config)
	if err != nil {
		return
	}
	defer session.Close()
	for {
		stream, err := session.AcceptStream()
		if err != nil {
			return
		}
		go func() { defer stream.Close(); _, _ = io.Copy(stream, stream) }()
	}
}

func selfSignedCertificate(t *testing.T, name string) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: name},
		DNSNames:     []string{name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
	)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

var _ N.Dialer = stdDialer{}
var _ adapter.Router = (adapter.Router)(nil)
var _ = strings.Builder{}
var _ = v2raywebsocket.NewConn
