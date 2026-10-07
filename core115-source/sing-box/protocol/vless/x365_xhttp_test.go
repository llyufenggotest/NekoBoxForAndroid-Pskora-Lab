package vless

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"
	"time"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	M "github.com/sagernet/sing/common/metadata"
)

const referenceX365UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

func TestX365XHTTPOutboundRequestUserAgent(t *testing.T) {
	headers := make(chan http.Header, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers <- r.Header.Clone()
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
	}))
	defer server.Close()
	host, portText, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	options := option.VLESSOutboundOptions{
		ServerOptions: option.ServerOptions{Server: host, ServerPort: uint16(port)},
		UUID:          "00112233-4455-6677-8899-aabbccddeeff#x365",
		Transport:     &option.V2RayTransportOptions{Type: C.V2RayTransportTypeXHTTP, XHTTPOptions: option.V2RayXHTTPOptions{Mode: "packet-up"}},
	}
	outbound, err := NewOutbound(context.Background(), nil, log.NewNOPFactory().Logger(), "test", options)
	if err != nil {
		t.Fatal(err)
	}
	defer outbound.(*Outbound).Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := outbound.DialContext(ctx, "tcp", M.ParseSocksaddr("example.com:443"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	select {
	case got := <-headers:
		if got.Get("User-Agent") != referenceX365UserAgent {
			t.Fatalf("actual XHTTP User-Agent = %q, want reference Chrome/120", got.Get("User-Agent"))
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if options.Transport.XHTTPOptions.Headers != nil {
		t.Fatal("caller transport options mutated")
	}
}

func TestX365XHTTPHeaderIsolation(t *testing.T) {
	for _, tc := range []struct {
		name, id, transport string
		headers             map[string]string
		wantUA              bool
	}{
		{"x365", "fixture#x365", "xhttp", nil, true},
		{"markerOnly", "#x365", "xhttp", nil, true},
		{"ordinary", "fixture", "xhttp", nil, false},
		{"juzi", "fixture#juzi", "xhttp", nil, false},
		{"juziUpper", "fixture#JUZI", "xhttp", nil, false},
		{"lastJuziWins", "fixture#x365#juzi", "xhttp", nil, false},
		{"uppercaseNotX365", "fixture#X365", "xhttp", nil, false},
		{"nonSuffix", "fixture#x365-extra", "xhttp", nil, false},
		{"websocket", "fixture#x365", "ws", nil, false},
		{"explicit", "fixture#x365", "xhttp", map[string]string{"User-Agent": "custom"}, false},
		{"explicitLower", "fixture#x365", "xhttp", map[string]string{"user-agent": "custom"}, false},
		{"explicitEmpty", "fixture#x365", "xhttp", map[string]string{"USER-AGENT": ""}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := option.V2RayTransportOptions{Type: tc.transport, XHTTPOptions: option.V2RayXHTTPOptions{
				V2RayXHTTPBaseOptions: option.V2RayXHTTPBaseOptions{Headers: tc.headers},
				Download:              &option.V2RayXHTTPDownloadOptions{V2RayXHTTPBaseOptions: option.V2RayXHTTPBaseOptions{Headers: map[string]string{"X-Download": "unchanged"}}},
			}}
			got := prepareX365XHTTPTransport(original, tc.id)
			if tc.wantUA {
				if got.XHTTPOptions.Headers["User-Agent"] != referenceX365UserAgent {
					t.Fatal("missing reference UA")
				}
				if original.XHTTPOptions.Headers != nil {
					t.Fatal("original mutated")
				}
			} else if !reflect.DeepEqual(got, original) {
				t.Fatal("unrelated/explicit configuration changed")
			}
			if got.XHTTPOptions.Download != original.XHTTPOptions.Download {
				t.Fatal("split download modified")
			}
		})
	}
}

func TestX365XHTTPHeadersClone(t *testing.T) {
	original := option.V2RayTransportOptions{Type: "xhttp", XHTTPOptions: option.V2RayXHTTPOptions{
		V2RayXHTTPBaseOptions: option.V2RayXHTTPBaseOptions{Headers: map[string]string{"X-Fixture": "retained"}},
	}}
	got := prepareX365XHTTPTransport(original, "fixture#x365")
	if got.XHTTPOptions.Headers["X-Fixture"] != "retained" {
		t.Fatal("custom header lost")
	}
	got.XHTTPOptions.Headers["X-Fixture"] = "changed"
	if original.XHTTPOptions.Headers["X-Fixture"] != "retained" || len(original.XHTTPOptions.Headers) != 1 {
		t.Fatal("source headers mutated")
	}
}
