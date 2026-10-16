package xhttp

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/service"
	"github.com/stretchr/testify/require"
)

type dynamicCertificateStore struct {
	adapter.CertificateStore
	pool *x509.CertPool
}

func (s *dynamicCertificateStore) Pool() *x509.CertPool { return s.pool }

type dynamicSystemDialer struct {
	dial    func(context.Context, string, M.Socksaddr) (net.Conn, error)
	packets atomic.Int32
}

func (d *dynamicSystemDialer) DialContext(ctx context.Context, n string, a M.Socksaddr) (net.Conn, error) {
	return d.dial(ctx, n, a)
}
func (d *dynamicSystemDialer) ListenPacket(ctx context.Context, a M.Socksaddr) (net.PacketConn, error) {
	d.packets.Add(1)
	return (&net.ListenConfig{}).ListenPacket(ctx, "udp", "127.0.0.1:0")
}

func dynamicControlFixture(t *testing.T, document *atomic.Value) (context.Context, *dynamicSystemDialer, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	cert, pool := controlFixtureCertificate(t, controlCDNHost)
	posts, pings := &atomic.Int32{}, &atomic.Int32{}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		require.Equal(t, controlCDNHost, r.Host)
		require.Equal(t, controlCDNHost, r.TLS.ServerName)
		require.Equal(t, 2, r.ProtoMajor)
		_, _ = w.Write(controlDocumentResponse(t, document.Load().(string)))
	}))
	server.EnableHTTP2 = true
	server.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	server.StartTLS()
	t.Cleanup(server.Close)
	ctx := service.ContextWith[adapter.CertificateStore](context.Background(), &dynamicCertificateStore{pool: pool})
	d := &dynamicSystemDialer{dial: func(ctx context.Context, network string, a M.Socksaddr) (net.Conn, error) {
		if a.Fqdn == controlCDNHost {
			return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
		}
		pings.Add(1)
		left, right := net.Pipe()
		right.Close()
		return left, nil
	}}
	return ctx, d, posts, pings
}
func dynamicDocument(entries, members string) string {
	return "mode: rule\nrules: [\"MATCH,Main\"]\nproxy-groups: [{name: Main, type: url-test, proxies: [" + members + "]}]\nproxies:\n" + entries
}

func TestDynamicProductionFullPreflightBeforePing(t *testing.T) {
	var doc atomic.Value
	doc.Store(dynamicDocument(" - {name: good, type: xhttp, server: 127.0.0.1, port: 443, password: 'synthetic:0102'}\n - {name: bad, type: ss, server: 127.0.0.1, port: 444, password: synthetic, cipher: invalid-cipher}\n", "good, bad"))
	ctx, d, posts, pings := dynamicControlFixture(t, &doc)
	created, err := NewOutbound(ctx, nil, log.NewNOPFactory().Logger(), "parent-route", option.XHttpOutboundOptions{DeviceID: "synthetic-device", DeviceName: "synthetic-tablet"})
	require.NoError(t, err)
	h := created.(*Outbound)
	h.dialer = d
	defer h.Close()
	require.ErrorContains(t, h.lazyInit(ctx), "stage=node-capability")
	require.Equal(t, int32(1), posts.Load())
	require.Zero(t, pings.Load())
	require.Nil(t, h.generation)
	doc.Store(dynamicDocument(" - {name: good, type: xhttp, server: 127.0.0.1, port: 443, password: 'synthetic:0102'}\n - {name: v, type: vless, server: 127.0.0.1, port: 444, uuid: '"+dynamicUUID+"', e: true, h: '0102', udp: true}\n", "good, v"))
	require.NoError(t, h.lazyInit(ctx))
	require.Equal(t, int32(2), posts.Load())
	require.Equal(t, int32(2), pings.Load())
	require.NotNil(t, h.generation)
}

func TestDynamicFullControlToVLESSData(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	port := M.SocksaddrFromNet(ln.Addr()).Port
	var doc atomic.Value
	doc.Store(dynamicDocument(fmt.Sprintf(" - {name: v, type: vless, server: 127.0.0.1, port: %d, uuid: '%s', e: true, h: '03aabbcc', udp: true}\n", port, dynamicUUID), "v"))
	ctx, d, posts, _ := dynamicControlFixture(t, &doc)
	controlDial := d.dial
	d.dial = func(ctx context.Context, n string, a M.Socksaddr) (net.Conn, error) {
		if a.Fqdn == controlCDNHost {
			return controlDial(ctx, n, a)
		}
		return (&net.Dialer{}).DialContext(ctx, n, a.String())
	}
	expected, _ := hex.DecodeString("0000112233445566778899aabbccddeeff000101bb020b6578616d706c652e636f6d6e8311168ee16d6aa1aa48c64145003c03aabbcc50")
	done := make(chan []byte, 1)
	go func() {
		c, e := ln.Accept()
		if e != nil {
			done <- nil
			return
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(2 * time.Second))
		got := make([]byte, len(expected))
		if _, e = io.ReadFull(c, got); e != nil {
			done <- nil
			return
		}
		c.Write([]byte{0, 0, 0, 'R'})
		done <- got
	}()
	h := &Outbound{ctx: ctx, dialer: d, deviceID: "synthetic-device", deviceName: "synthetic-tablet"}
	defer h.Close()
	conn, err := h.DialContext(ctx, "tcp", M.ParseSocksaddr("example.com:443"))
	require.NoError(t, err)
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	_, err = conn.Write([]byte("P"))
	require.NoError(t, err)
	p := make([]byte, 1)
	_, err = io.ReadFull(conn, p)
	require.NoError(t, err)
	require.Equal(t, "R", string(p))
	require.Equal(t, expected, <-done)
	require.Equal(t, int32(1), posts.Load())
}

func TestDynamicProductionColdUDPAndRefreshGeneration(t *testing.T) {
	var doc atomic.Value
	entry := " - {name: wire, type: xhttp, server: 127.0.0.1, port: 443, password: 'synthetic:0102', udp: true}\n"
	doc.Store(dynamicDocument(entry, "wire"))
	ctx, d, posts, pings := dynamicControlFixture(t, &doc)
	h := &Outbound{ctx: ctx, dialer: d, deviceID: "synthetic-device", deviceName: "synthetic-tablet"}
	_ = h.Network()
	require.Zero(t, posts.Load())
	pc, err := h.ListenPacket(ctx, M.ParseSocksaddr("route.example:53"))
	require.NoError(t, err)
	defer pc.Close()
	require.Equal(t, int32(1), posts.Load())
	require.Equal(t, int32(1), d.packets.Load())
	require.Zero(t, pings.Load())
	old := h.generation
	old.client.publish([]byte{1, 9})
	doc.Store(dynamicDocument(strings.Replace(entry, "synthetic:0102", "synthetic:0304", 1), "wire"))
	require.NoError(t, h.Refresh(ctx))
	fresh := h.generation
	require.NotSame(t, old, fresh)
	require.Equal(t, []byte{3, 4}, fresh.client.snapshot())
	require.Equal(t, []byte{1, 9}, old.client.snapshot())
	old.client.publish([]byte{1, 8})
	require.Equal(t, []byte{3, 4}, fresh.client.snapshot())
	doc.Store(dynamicDocument(strings.Replace(entry, "type: xhttp", "type: ss, cipher: invalid-cipher", 1), "wire"))
	require.Error(t, h.Refresh(ctx))
	require.Same(t, fresh, h.generation)
	require.Len(t, h.retired, 1)
	require.NoError(t, h.Close())
	require.ErrorIs(t, h.lazyInit(ctx), net.ErrClosed)
	require.Equal(t, int32(3), posts.Load())
}

func TestDynamicProductionNativeUDPLoopback(t *testing.T) {
	peer, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	defer peer.Close()
	endpoint := M.SocksaddrFromNet(peer.LocalAddr())
	done := make(chan error, 1)
	go func() {
		peer.SetDeadline(time.Now().Add(2 * time.Second))
		frame := make([]byte, 2048)
		n, source, e := peer.ReadFrom(frame)
		if e != nil {
			done <- e
			return
		}
		if n < 164 {
			done <- fmt.Errorf("short packet")
			return
		}
		key := dynamicLiteralKey()
		block, _ := aes.NewCipher(key)
		plain := make([]byte, n-163)
		cipher.NewCTR(block, frame[147:163]).XORKeyStream(plain, frame[163:n])
		want := append([]byte{3, 13}, []byte("route.example")...)
		want = append(want, 0, 53, 'P')
		if !bytes.Equal(want, plain) {
			done <- fmt.Errorf("native packet mismatch")
			return
		}
		_, e = peer.WriteTo(wireServer(key, []byte{9}, []byte{1, 192, 0, 2, 1, 0, 53, 'R'}), source)
		done <- e
	}()
	d := &dynamicSystemDialer{dial: func(context.Context, string, M.Socksaddr) (net.Conn, error) {
		t.Error("native UDP used stream dial")
		return nil, fmt.Errorf("unexpected dial")
	}}
	h := &Outbound{isInit: true, bestCfg: RealConfig{Type: "xhttp", Server: "127.0.0.1", Port: fmt.Sprint(endpoint.Port), Password: "synthetic:0102", UDP: true}, dialer: d}
	defer h.Close()
	pc, err := h.ListenPacket(context.Background(), M.ParseSocksaddr("route.example:53"))
	require.NoError(t, err)
	defer pc.Close()
	pc.SetDeadline(time.Now().Add(2 * time.Second))
	_, err = pc.WriteTo([]byte("P"), M.ParseSocksaddr("route.example:53"))
	require.NoError(t, err)
	buf := make([]byte, 1)
	n, src, err := pc.ReadFrom(buf)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, "R", string(buf))
	require.Equal(t, "192.0.2.1:53", src.String())
	require.NoError(t, <-done)
	require.Equal(t, []byte{1, 9}, h.generation.client.snapshot())
}

func TestDynamicProductionUOTLoopback(t *testing.T) {
	for _, version := range []int{0, 1, 2} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			defer ln.Close()
			addr := M.ParseSocksaddr(ln.Addr().String())
			done := make(chan error, 1)
			go func() {
				c, e := ln.Accept()
				if e != nil {
					done <- e
					return
				}
				defer c.Close()
				c.SetDeadline(time.Now().Add(2 * time.Second))
				header := make([]byte, 163)
				if _, e = io.ReadFull(c, header); e != nil {
					done <- e
					return
				}
				key := dynamicLiteralKey()
				block, _ := aes.NewCipher(key)
				reader := &cipher.StreamReader{S: cipher.NewCTR(block, header[147:]), R: c}
				magic, e := M.SocksaddrSerializer.ReadAddrPort(reader)
				if e != nil {
					done <- e
					return
				}
				expected := "sp.v2.udp-over-tcp.arpa"
				if version == 1 {
					expected = "sp.udp-over-tcp.arpa"
				}
				if magic.Fqdn != expected {
					done <- fmt.Errorf("wrong UOT magic")
					return
				}
				native := []byte{2, 13}
				native = append(native, []byte("route.example")...)
				native = append(native, 1, 187)
				want := append([]byte{}, native...)
				want = append(want, 0, 1, 'P')
				if version != 1 {
					want = append(append([]byte{0}, native...), want...)
				}
				got := make([]byte, len(want))
				_, e = io.ReadFull(reader, got)
				if e == nil && !bytes.Equal(want, got) {
					e = fmt.Errorf("wrong private UOT frame")
				}
				if e == nil {
					_, e = c.Write(wireServer(key, []byte{9}, []byte{0, 192, 0, 2, 1, 0, 53, 0, 1, 'R'}))
				}
				done <- e
			}()
			h := &Outbound{isInit: true, bestCfg: RealConfig{Type: "xhttp", Server: "127.0.0.1", Port: fmt.Sprint(addr.Port), Password: "synthetic:0102", UDP: true, UDPOverTCP: true, UDPOverTCPVersion: version}, dialer: &dynamicSystemDialer{dial: func(ctx context.Context, n string, a M.Socksaddr) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, n, a.String())
			}}}
			defer h.Close()
			pc, err := h.ListenPacket(context.Background(), M.ParseSocksaddr("route.example:443"))
			require.NoError(t, err)
			defer pc.Close()
			pc.SetDeadline(time.Now().Add(2 * time.Second))
			_, err = pc.WriteTo([]byte("P"), M.ParseSocksaddr("route.example:443"))
			require.NoError(t, err)
			p := make([]byte, 1)
			n, source, err := pc.ReadFrom(p)
			require.NoError(t, err)
			require.Equal(t, 1, n)
			require.Equal(t, "R", string(p))
			require.Equal(t, "192.0.2.1:53", source.String())
			require.NoError(t, <-done)
		})
	}
}
func dynamicLiteralKey() []byte {
	key, _ := hex.DecodeString("5f06b85e1619b2b8b56f7b758a0a310a")
	return key
}
