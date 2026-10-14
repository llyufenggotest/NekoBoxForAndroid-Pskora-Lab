package shadowsocks

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/transport/v2raywebsocket"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/common/uot"
	"github.com/sagernet/smux"
	"github.com/sagernet/ws"
)

func TestGostAppBooleanUoTWire(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			result := make(chan error, 1)
			destination := M.ParseSocksaddr("127.0.0.1:5353")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Host != "front.invalid" || r.URL.Path != "/gost" {
					result <- fmt.Errorf("wrong endpoint")
					return
				}
				raw, _, _, err := ws.UpgradeHTTP(r, w)
				if err != nil {
					result <- err
					return
				}
				defer raw.Close()
				_ = raw.SetDeadline(time.Now().Add(5 * time.Second))
				var conn net.Conn = v2raywebsocket.NewConn(raw, raw.RemoteAddr(), ws.StateServerSide)
				if enabled {
					cfg := smux.DefaultConfig()
					cfg.Version = 1
					cfg.KeepAliveDisabled = true
					session, err := smux.Server(conn, cfg)
					if err != nil {
						result <- err
						return
					}
					defer session.Close()
					conn, err = session.AcceptStream()
					if err != nil {
						result <- err
						return
					}
					defer conn.Close()
				}
				ssDestination, err := M.SocksaddrSerializer.ReadAddrPort(conn)
				if err != nil {
					result <- err
					return
				}
				if ssDestination.Fqdn != uot.MagicAddress {
					result <- fmt.Errorf("SS destination = %v", ssDestination)
					return
				}
				request, err := uot.ReadRequest(conn)
				if err != nil {
					result <- err
					return
				}
				if !request.IsConnect || request.Destination != destination {
					result <- fmt.Errorf("UoT request = %+v", request)
					return
				}
				packet := uot.NewConn(conn, *request)
				payload := make([]byte, 128)
				n, err := packet.Read(payload)
				if err == nil && string(payload[:n]) != "gost-bool-uot-v2" {
					err = fmt.Errorf("bad UoT payload")
				}
				if err == nil {
					_, err = packet.Write(payload[:n])
				}
				result <- err
			}))
			defer server.Close()
			addr := M.ParseSocksaddr(server.Listener.Addr().String())
			outbound, err := NewOutbound(context.Background(), nil, log.NewNOPFactory().NewLogger("test"), "gost", option.ShadowsocksOutboundOptions{
				ServerOptions: option.ServerOptions{Server: addr.AddrString(), ServerPort: addr.Port}, Method: "none", Plugin: "gost-plugin",
				PluginOptions: fmt.Sprintf("mode=ws;host=front.invalid;path=/gost;mux=%v;tls=false", enabled),
				UDPOverTCP:    &option.UDPOverTCPOptions{Enabled: true, Version: uot.Version},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer outbound.(*Outbound).plugin.(interface{ Close() error }).Close()
			conn, err := outbound.DialContext(context.Background(), "udp", destination)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
			if _, err = conn.Write([]byte("gost-bool-uot-v2")); err != nil {
				t.Fatal(err)
			}
			payload := make([]byte, 128)
			n, err := conn.Read(payload)
			if err != nil {
				t.Fatal(err)
			}
			if string(payload[:n]) != "gost-bool-uot-v2" {
				t.Fatalf("echo = %q", payload[:n])
			}
			if err = <-result; err != nil {
				t.Fatal(err)
			}
		})
	}
}
