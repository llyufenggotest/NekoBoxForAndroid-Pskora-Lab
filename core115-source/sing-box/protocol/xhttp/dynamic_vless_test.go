package xhttp

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/vless"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/service"
	"github.com/stretchr/testify/require"
)

const dynamicUUID = "00112233-4455-6677-8899-aabbccddeeff"

func dynamicVLESSFixture() RealConfig {
	return RealConfig{Type: "vless", Server: "127.0.0.1", Port: "443", UDP: true, VLESS: &BlackstoneVLESSOptions{Server: "127.0.0.1", Port: 443, UUID: dynamicUUID, E: true, H: "03aabbcc", UDP: true}}
}

func TestDynamicNullTransportFieldsRejected(t *testing.T) {
	for _, field := range []string{"network", "flow"} {
		for _, value := range []any{nil, true, 1} {
			p := map[string]any{"type": "vless", "server": "127.0.0.1", "port": 443, "uuid": dynamicUUID, "e": true, "h": "0102", field: value}
			_, err := parseSelectedVLESS(p, "127.0.0.1", "443")
			require.Error(t, err, "%s %v", field, value)
		}
	}
}

func TestDynamicUnknownSecurityFieldsFailClosed(t *testing.T) {
	for _, extra := range []map[string]any{{"encryption-extra": "lost"}, {"detour": "untrusted-route"}, {"xhttp-opts": map[string]any{"download-settings": map[string]any{"detour": "untrusted-route"}}}} {
		p := map[string]any{"type": "vless", "server": "127.0.0.1", "port": 443, "uuid": dynamicUUID, "e": true, "h": "0102"}
		for k, v := range extra {
			p[k] = v
		}
		_, err := parseSelectedVLESS(p, "127.0.0.1", "443")
		require.Error(t, err)
	}
}

func TestDynamicTypedVLESSMapping(t *testing.T) {
	cfg := dynamicVLESSFixture()
	o, err := dynamicVLESSOptions(cfg)
	require.NoError(t, err)
	require.Equal(t, dynamicUUID+"#blackstone", o.UUID)
	require.NotNil(t, o.PacketEncoding)
	require.Equal(t, "xudp", *o.PacketEncoding)
	require.True(t, o.E)
	require.Equal(t, "03aabbcc", o.H)
	for _, network := range []string{"", "tcp", "http", "h2", "ws", "grpc", "xhttp"} {
		t.Run(network, func(t *testing.T) {
			c := dynamicVLESSFixture()
			c.VLESS.Network = network
			if network == "h2" {
				c.VLESS.TLS = true
			}
			o, err := dynamicVLESSOptions(c)
			require.NoError(t, err)
			h := &Outbound{}
			g, err := h.constructGeneration(context.Background(), c)
			require.NoError(t, err)
			require.NoError(t, g.Close())
			if network == "xhttp" {
				require.Equal(t, "auto", o.Transport.XHTTPOptions.Mode)
			}
		})
	}
	c := dynamicVLESSFixture()
	v := c.VLESS
	v.TLS = true
	v.ServerName = "sni.example"
	v.ALPN = []string{"h2"}
	v.ClientFingerprint = "chrome"
	v.Flow = "xtls-rprx-vision"
	v.RealityOpts = BlackstoneRealityOptions{PublicKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", ShortID: "0102"}
	v.Network = "xhttp"
	v.XHTTPOpts = BlackstoneXHTTPOptions{Mode: "packet-up", Host: "front.example", Path: "/up", XPaddingBytes: "100-200", ReuseSettings: &BlackstoneXHTTPReuseSettings{MaxConnections: "1-2"}}
	host := "down.example"
	port := 8443
	path := "/down"
	v.XHTTPOpts.DownloadSettings = &BlackstoneXHTTPDownloadSettings{Server: &host, Port: &port, Path: &path}
	o, err = dynamicVLESSOptions(c)
	require.NoError(t, err)
	require.Equal(t, "sni.example", o.TLS.ServerName)
	require.Equal(t, "0102", o.TLS.Reality.ShortID)
	require.Equal(t, "chrome", o.TLS.UTLS.Fingerprint)
	require.Equal(t, "down.example", o.Transport.XHTTPOptions.Download.Server)
	require.Equal(t, uint16(8443), o.Transport.XHTTPOptions.Download.ServerPort)
	require.Empty(t, o.Transport.XHTTPOptions.Download.Detour)
}

func TestDynamicUnsupportedFieldsFailClosed(t *testing.T) {
	for name, change := range map[string]func(*BlackstoneVLESSOptions){
		"flow": func(v *BlackstoneVLESSOptions) { v.Flow = "xtls-rprx-vision-extra" }, "network": func(v *BlackstoneVLESSOptions) { v.Network = "unknown" }, "selector": func(v *BlackstoneVLESSOptions) { v.UUID += "#blackstone" }, "packet": func(v *BlackstoneVLESSOptions) { v.PacketAddr = true; v.XUDP = true }, "fingerprint": func(v *BlackstoneVLESSOptions) { v.Fingerprint = "sha256" }, "inactive-ws": func(v *BlackstoneVLESSOptions) { v.WSOpts.Path = "/lost" }, "grpc-user-agent": func(v *BlackstoneVLESSOptions) { v.Network = "grpc"; v.GrpcOpts.GrpcUserAgent = "lost" }, "encryption": func(v *BlackstoneVLESSOptions) { v.Encryption = "1rtt" }, "restls": func(v *BlackstoneVLESSOptions) { v.RestlsOpts.Password = "secret" }, "tls-disabled": func(v *BlackstoneVLESSOptions) { v.ServerName = "lost" },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := dynamicVLESSFixture()
			change(cfg.VLESS)
			h := &Outbound{}
			g, err := h.constructGeneration(context.Background(), cfg)
			require.Error(t, err)
			require.Nil(t, g)
		})
	}
}

func TestDynamicProductionVLESSLoopback(t *testing.T) {
	for _, udp := range []bool{false, true} {
		t.Run(fmt.Sprint(udp), func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			defer listener.Close()
			expected := "0000112233445566778899aabbccddeeff000101bb020b6578616d706c652e636f6d6e8311168ee16d6aa1aa48c64145003c03aabbcc50"
			response := []byte{0, 0, 2, 0xaa, 0xbb, 'R'}
			if udp {
				expected = "0000112233445566778899aabbccddeeff00036e8311168ee16d6aa1aa48c64145003c03aabbcc0014000001010201bb020b6578616d706c652e636f6d000150"
				response = []byte{0, 0, 2, 0xaa, 0xbb, 0, 4, 0, 0, 2, 1, 0, 1, 'R'}
			}
			want, _ := hex.DecodeString(expected)
			done := make(chan []byte, 1)
			go func() {
				raw, e := listener.Accept()
				if e != nil {
					done <- nil
					return
				}
				defer raw.Close()
				raw.SetDeadline(time.Now().Add(2 * time.Second))
				got := make([]byte, len(want))
				_, e = io.ReadFull(raw, got)
				if e != nil {
					done <- nil
					return
				}
				raw.Write(response)
				done <- got
			}()
			cfg := dynamicVLESSFixture()
			endpoint := M.ParseSocksaddr(listener.Addr().String())
			cfg.Port = fmt.Sprint(endpoint.Port)
			cfg.VLESS.Port = int(endpoint.Port)
			h := &Outbound{ctx: context.Background(), isInit: true, bestCfg: cfg, logger: log.NewNOPFactory().Logger(), dialer: controlFixtureDialer{dial: func(ctx context.Context, address string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "tcp", address)
			}}}
			defer h.Close()
			dst := M.ParseSocksaddr("example.com:443")
			if udp {
				pc, e := h.ListenPacket(context.Background(), dst)
				require.NoError(t, e)
				defer pc.Close()
				pc.SetDeadline(time.Now().Add(2 * time.Second))
				_, e = pc.WriteTo([]byte("P"), dst)
				require.NoError(t, e)
				p := make([]byte, 128)
				n, _, e := pc.ReadFrom(p)
				require.NoError(t, e)
				require.Equal(t, 1, n)
				require.Equal(t, "R", string(p[:n]))
			} else {
				conn, e := h.DialContext(context.Background(), "tcp", dst)
				require.NoError(t, e)
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(2 * time.Second))
				_, e = conn.Write([]byte("P"))
				require.NoError(t, e)
				p := make([]byte, 1)
				_, e = io.ReadFull(conn, p)
				require.NoError(t, e)
				require.Equal(t, "R", string(p))
			}
			require.Equal(t, want, <-done)
		})
	}
}

func TestDynamicAppExportedJSONConstructor(t *testing.T) {
	// Literal copied byte-for-byte from real synthetic YAML→Room→ConfigBuilder
	// export in evidence-app/app-outbound.synthetic.json; no runtime dependency.
	raw := []byte(`{"domain_strategy":"","server":"node.example","server_port":443,"uuid":"12345678-1234-1234-1234-123456789abc#blackstone","packet_encoding":"xudp","e":true,"h":"03aabbcc","type":"vless","tag":"synthetic-blackstone"}`)
	var err error
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &fields))
	delete(fields, "type")
	delete(fields, "tag")
	// Preserve the exact exported server; local detour avoids external DNS.
	raw, err = json.Marshal(fields)
	require.NoError(t, err)
	var opts option.VLESSOutboundOptions
	require.NoError(t, json.Unmarshal(raw, &opts))
	ctx := service.ContextWith[adapter.OutboundManager](context.Background(), &dynamicDialerManager{data: &dynamicDataOutbound{Adapter: outbound.NewAdapter("direct", "blackstone-owned-data", nil, nil)}})
	opts.Detour = "blackstone-owned-data"
	created, err := vless.NewOutbound(ctx, nil, log.NewNOPFactory().Logger(), "synthetic-blackstone", opts)
	require.NoError(t, err)
	require.NotNil(t, created)
}
