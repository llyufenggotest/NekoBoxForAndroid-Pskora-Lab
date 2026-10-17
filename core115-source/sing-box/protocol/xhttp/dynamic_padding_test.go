package xhttp

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	M "github.com/sagernet/sing/common/metadata"
	"github.com/stretchr/testify/require"
)

// Keep the encrypted certificate boundary in this regression: parser-only
// padding preservation previously passed while the production preflight failed.
func dynamicPaddingEntry(t *testing.T, name, padding string, port uint16, path string) string {
	t.Helper()
	inner := map[string]any{"type": "xhttp", "server": "127.0.0.1", "port": port, "password": "synthetic:0102", "udp": true, "padding-len": padding}
	if strings.HasPrefix(path, "uot") {
		inner["udp-over-tcp"] = true
		inner["udp-over-tcp-version"] = int(path[len(path)-1] - '0')
	}
	raw, err := json.Marshal(inner)
	require.NoError(t, err)
	certificate := base64.StdEncoding.EncodeToString(append(make([]byte, 48), encryptBlackstonePayloadFixture(t, raw)...))
	return fmt.Sprintf(" - {name: %s, type: xhttp, certificate: '%s'}\n", name, certificate)
}

func TestDynamicPaddingProductionData(t *testing.T) {
	for _, padding := range []string{"8-64", "opaque-config-only"} {
		for _, path := range []string{"tcp", "udp", "uot0", "uot1", "uot2"} {
			t.Run(padding+"/"+path, func(t *testing.T) {
				var port uint16
				var listener net.Listener
				var packet net.PacketConn
				var err error
				if path == "udp" {
					packet, err = net.ListenPacket("udp", "127.0.0.1:0")
					require.NoError(t, err)
					defer packet.Close()
					port = M.SocksaddrFromNet(packet.LocalAddr()).Port
				} else {
					listener, err = net.Listen("tcp", "127.0.0.1:0")
					require.NoError(t, err)
					defer listener.Close()
					port = M.SocksaddrFromNet(listener.Addr()).Port
				}
				var document atomic.Value
				document.Store(dynamicDocument(dynamicPaddingEntry(t, "wire", padding, port, path), "wire"))
				ctx, d, posts, pings := dynamicControlFixture(t, &document)
				controlDial := d.dial
				d.dial = func(ctx context.Context, n string, a M.Socksaddr) (net.Conn, error) {
					if a.Fqdn == controlCDNHost {
						return controlDial(ctx, n, a)
					}
					return (&net.Dialer{}).DialContext(ctx, n, a.String())
				}
				h := &Outbound{ctx: ctx, dialer: d, deviceID: "synthetic-device", deviceName: "synthetic-tablet"}
				defer h.Close()
				// Exact control selection must pass actual constructor preflight,
				// not an isInit/bestCfg shortcut or a replacement wire client.
				require.NoError(t, h.lazyInit(ctx))
				require.Equal(t, padding, h.generation.cfg.PaddingLen)
				require.Equal(t, int32(1), posts.Load())
				require.Zero(t, pings.Load())
				done := make(chan error, 1)
				go func() { done <- dynamicPaddingPeer(listener, packet, path) }()
				destination := M.ParseSocksaddr("route.example:443")
				if path == "tcp" {
					conn, e := h.DialContext(ctx, "tcp", destination)
					require.NoError(t, e)
					defer conn.Close()
					require.NoError(t, conn.SetDeadline(time.Now().Add(2*time.Second)))
					_, e = conn.Write([]byte("P"))
					require.NoError(t, e)
					response := make([]byte, 1)
					_, e = io.ReadFull(conn, response)
					require.NoError(t, e)
					require.Equal(t, "R", string(response))
				} else {
					conn, e := h.ListenPacket(ctx, destination)
					require.NoError(t, e)
					defer conn.Close()
					require.NoError(t, conn.SetDeadline(time.Now().Add(2*time.Second)))
					_, e = conn.WriteTo([]byte("P"), destination)
					require.NoError(t, e)
					response := make([]byte, 1)
					n, source, e := conn.ReadFrom(response)
					require.NoError(t, e)
					require.Equal(t, 1, n)
					require.Equal(t, "R", string(response))
					require.Equal(t, "192.0.2.1:53", source.String())
				}
				require.NoError(t, <-done)
				require.Equal(t, []byte{1, 9}, h.generation.client.snapshot())
				require.Equal(t, padding, h.generation.cfg.PaddingLen)
				require.NoError(t, h.Close())
			})
		}
	}
}

// Independent AES-CTR decode and literal framing checks deliberately have no
// padding interpretation. Metadata must neither insert bytes nor alter framing.
func dynamicPaddingPeer(listener net.Listener, packet net.PacketConn, path string) error {
	key := dynamicLiteralKey()
	block, _ := aes.NewCipher(key)
	address := append([]byte{3, 13}, []byte("route.example")...)
	address = append(address, 1, 187)
	var reader io.Reader
	var conn net.Conn
	var source net.Addr
	if packet != nil {
		packet.SetDeadline(time.Now().Add(2 * time.Second))
		frame := make([]byte, 2048)
		n, from, err := packet.ReadFrom(frame)
		if err != nil {
			return err
		}
		if n < 163 {
			return fmt.Errorf("short native frame")
		}
		plain := make([]byte, n-163)
		cipher.NewCTR(block, frame[147:163]).XORKeyStream(plain, frame[163:n])
		reader, source = bytes.NewReader(plain), from
	} else {
		var err error
		conn, err = listener.Accept()
		if err != nil {
			return err
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(2 * time.Second))
		header := make([]byte, 163)
		if _, err = io.ReadFull(conn, header); err != nil {
			return err
		}
		reader = &cipher.StreamReader{S: cipher.NewCTR(block, header[147:]), R: conn}
	}
	want := append(append([]byte{}, address...), 'P')
	response := []byte{'R'}
	if strings.HasPrefix(path, "uot") {
		magic, err := M.SocksaddrSerializer.ReadAddrPort(reader)
		if err != nil {
			return err
		}
		expected := "sp.v2.udp-over-tcp.arpa"
		if path == "uot1" {
			expected = "sp.udp-over-tcp.arpa"
		}
		if magic.Fqdn != expected {
			return fmt.Errorf("wrong UOT magic")
		}
		native := append([]byte{2}, address[1:]...)
		want = append(append([]byte{}, native...), 0, 1, 'P')
		if path != "uot1" {
			want = append(append([]byte{0}, native...), want...)
		}
		response = []byte{0, 192, 0, 2, 1, 0, 53, 0, 1, 'R'}
	} else if path == "udp" {
		response = []byte{1, 192, 0, 2, 1, 0, 53, 'R'}
	}
	got := make([]byte, len(want))
	if _, err := io.ReadFull(reader, got); err != nil {
		return err
	}
	if !bytes.Equal(want, got) {
		return fmt.Errorf("unexpected data framing")
	}
	if packet != nil {
		if extra, _ := io.ReadAll(reader); len(extra) != 0 {
			return fmt.Errorf("unexpected padding bytes")
		}
		_, err := packet.WriteTo(wireServer(key, []byte{9}, response), source)
		return err
	}
	// A stream has no packet boundary. Before responding, require a bounded
	// idle read rather than accepting trailing bytes after the expected frame.
	if err := conn.SetReadDeadline(time.Now().Add(20 * time.Millisecond)); err != nil {
		return err
	}
	var trailing [1]byte
	n, readErr := reader.Read(trailing[:])
	if n != 0 {
		return fmt.Errorf("unexpected stream padding bytes")
	}
	if timeout, ok := readErr.(net.Error); !ok || !timeout.Timeout() {
		return fmt.Errorf("expected idle stream before response")
	}
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return err
	}
	_, err := conn.Write(wireServer(key, []byte{9}, response))
	return err
}

func TestDynamicPaddingExactSetPreflight(t *testing.T) {
	for _, bad := range []bool{false, true} {
		t.Run(fmt.Sprint(bad), func(t *testing.T) {
			first := dynamicPaddingEntry(t, "first", "8-64", 443, "tcp")
			second := dynamicPaddingEntry(t, "second", "opaque-config-only", 444, "tcp")
			if bad {
				second = " - {name: second, type: ss, server: 127.0.0.1, port: 444, password: synthetic, cipher: invalid-cipher}\n"
			}
			var document atomic.Value
			document.Store(dynamicDocument(first+second, "first, second"))
			ctx, d, posts, pings := dynamicControlFixture(t, &document)
			h := &Outbound{ctx: ctx, dialer: d}
			defer h.Close()
			var seen []string
			configs, err := FetchDynamicConfigWithPreflight(ctx, d, "ignored", "id", "token", "device", "tablet", func(ctx context.Context, cfg RealConfig) error {
				seen = append(seen, cfg.Name)
				return h.preflight(ctx, cfg)
			})
			if bad {
				require.ErrorContains(t, err, "stage=node-capability")
				require.Nil(t, configs, "one unsupported member must reject the whole exact set")
			} else {
				require.NoError(t, err)
				require.Len(t, configs, 2)
				require.Equal(t, "8-64", configs[0].PaddingLen)
				require.Equal(t, "opaque-config-only", configs[1].PaddingLen)
			}
			require.Equal(t, []string{"first", "second"}, seen)
			require.Equal(t, int32(1), posts.Load())
			require.Zero(t, pings.Load())
		})
	}
}

func TestDynamicPaddingOtherTransportsStillReject(t *testing.T) {
	for _, cfg := range []RealConfig{{Type: "ss", Server: "127.0.0.1", Port: "443", Cipher: "aes-128-ctr", Password: "synthetic"}, dynamicVLESSFixture()} {
		t.Run(cfg.Type, func(t *testing.T) {
			cfg.PaddingLen = "8-64"
			g, err := (&Outbound{}).constructGeneration(context.Background(), cfg)
			require.ErrorContains(t, err, "stage=node-capability")
			require.Nil(t, g)
		})
	}
}
