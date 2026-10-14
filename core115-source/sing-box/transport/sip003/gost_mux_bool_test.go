package sip003

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sagernet/sing-box/transport/v2raywebsocket"
	"github.com/sagernet/ws"
	"github.com/sagernet/ws/wsutil"
)

func TestGostMuxBooleanContract(t *testing.T) {
	for _, test := range []struct {
		name, opts string
		enabled    bool
		capacity   int
	}{
		{"app false", "mode=ws;host=front.invalid;path=/gost;mux=false;tls=false", false, 0},
		{"app true", "mode=ws;host=front.invalid;path=/gost;mux=true;tls=false", true, 1},
		{"false overrides mws", "mode=mws;mux=false", false, 0},
		{"false overrides mwss", "mode=mwss;mux=false", false, 0},
		{"false overrides mtls", "mode=mtls;mux=false", false, 0},
		{"case folded true", "mode=ws;mux=TrUe", true, 1},
		{"yes", "mode=ws;mux=yes", true, 1},
		{"on", "mode=ws;mux=ON", true, 1},
		{"no", "mode=mws;mux=No", false, 0},
		{"off", "mode=mws;mux=OFF", false, 0},
		{"numeric zero remains unlimited", "mode=ws;mux=0", true, 0},
		{"numeric one", "mode=ws;mux=1", true, 1},
		{"numeric two", "mode=ws;mux=2", true, 2},
		{"numeric maximum", "mode=ws;mux=65535", true, 65535},
		{"bare flag", "mode=ws;mux", true, 1},
		{"absent ws", "mode=ws", false, 0},
		{"absent mws", "mode=mws", true, 1},
		{"absent mwss", "mode=mwss", true, 1},
		{"absent mtls", "mode=mtls", true, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			options, err := parseGostOptions(mustArgs(t, test.opts))
			if err != nil {
				t.Fatal(err)
			}
			if options.muxEnabled != test.enabled || options.muxCapacity != test.capacity {
				t.Fatalf("mux = %v/%d, want %v/%d", options.muxEnabled, options.muxCapacity, test.enabled, test.capacity)
			}
		})
	}
}

// A raw WS peer must receive the payload itself (not a smux header). The mux
// peer accepts only smux v1, so a helper-only boolean fix cannot pass this test.
func TestGostMuxBooleanWebsocketWire(t *testing.T) {
	for _, test := range []struct {
		name, mode, mux string
		enabled         bool
	}{
		{"false raw ws", "ws", "false", false},
		{"false overrides mws wire", "mws", "false", false},
		{"true smux v1", "ws", "true", true},
		{"numeric zero smux v1", "ws", "0", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			endpoint := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				endpoint <- r.Host + r.URL.Path
				conn, _, _, err := ws.UpgradeHTTP(r, w)
				if err != nil {
					return
				}
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				if test.enabled {
					serveMuxEcho(v2raywebsocket.NewConn(conn, conn.RemoteAddr(), ws.StateServerSide))
				} else {
					defer conn.Close()
					payload, op, err := wsutil.ReadClientData(conn)
					if err == nil && op == ws.OpBinary {
						_ = wsutil.WriteServerBinary(conn, payload)
					}
				}
			}))
			defer server.Close()
			plugin := newGostForServer(t, server.Listener.Addr(), "mode="+test.mode+";host=front.invalid;path=/gost;mux="+test.mux+";tls=false")
			defer plugin.Close()
			conn, err := plugin.DialContext(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
			_, isStream := conn.(*gostStream)
			if isStream != test.enabled {
				t.Fatalf("smux wrapping = %v, want %v", isStream, test.enabled)
			}
			assertEcho(t, conn, []byte("app-gost-bool-wire-payload"))
			if got := <-endpoint; got != "front.invalid/gost" {
				t.Fatalf("endpoint = %q", got)
			}
			plugin.access.Lock()
			sessions := len(plugin.sessions)
			plugin.access.Unlock()
			if !test.enabled && sessions != 0 {
				t.Fatalf("disabled mux created %d sessions", sessions)
			}
		})
	}
}
