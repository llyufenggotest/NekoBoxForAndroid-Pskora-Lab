package xhttp

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/sagernet/sing-box/option"
)

// TestNetworkGenerationRetiresRealHTTP2Pool is an A -> B -> A transport-pool
// characterization. The negative control proves that merely changing the
// logical network generation leaves the same live HTTP/2 connection reusable;
// closing the XHTTP xmux manager (the production InterfaceUpdated hook) retires
// that owner and forces the next generation onto a new TCP/HTTP2 connection.
func TestNetworkGenerationRetiresRealHTTP2Pool(t *testing.T) {
	var accepted atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = io.WriteString(w, "ok")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	server.EnableHTTP2 = true
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			accepted.Add(1)
		}
	}
	server.StartTLS()
	defer server.Close()

	newManagerAndClient := func() (*XmuxManager, DialerClient) {
		manager := NewXmuxManager(option.V2RayXHTTPXmuxOptions{}, func() XmuxConn {
			transport := &http.Transport{
				ForceAttemptHTTP2: true,
				TLSClientConfig:   &tls.Config{InsecureSkipVerify: true}, // local test server
			}
			return &DefaultDialerClient{
				options:     &option.V2RayXHTTPBaseOptions{},
				client:      &http.Client{Transport: transport},
				httpVersion: "2",
			}
		})
		client := manager.GetXmuxClient(context.Background()).XmuxConn.(DialerClient)
		return manager, client
	}
	var bodies []io.ReadCloser
	request := func(label string, client DialerClient) {
		t.Helper()
		body, _, _, err := client.OpenStream(context.Background(), server.URL+"/"+label, "session", nil, false)
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		payload := make([]byte, 2)
		if _, err := io.ReadFull(body, payload); err != nil {
			t.Fatalf("%s read: %v", label, err)
		}
		bodies = append(bodies, body)
		if string(payload) != "ok" {
			t.Fatalf("%s body = %q", label, payload)
		}
	}
	defer func() {
		for _, body := range bodies {
			_ = body.Close()
		}
	}()

	generationA, clientA := newManagerAndClient()
	request("a1", clientA)
	if got := accepted.Load(); got != 1 {
		t.Fatalf("A first request connections = %d, want 1", got)
	}

	// RED control: without the reset hook, a B-generation request reuses A's
	// HTTP/2 transport and underlying TCP connection.
	request("b-without-reset", clientA)
	if got := accepted.Load(); got != 1 {
		t.Fatalf("negative control did not reuse A HTTP/2 connection: %d", got)
	}
	t.Log("RED control: B without InterfaceUpdated reset reused A HTTP/2 connection")

	// GREEN production lifecycle: VLESS/VMess/Trojan InterfaceUpdated calls the
	// XHTTP Client.Close method, whose XmuxManager.Close reaches this operation.
	generationA.Close()
	generationB, clientB := newManagerAndClient()
	defer generationB.Close()
	request("b-after-reset", clientB)
	request("a2-after-second-switch", clientB)
	if got := accepted.Load(); got != 2 {
		t.Fatalf("reset lifecycle connections = %d, want exactly 2 (%s)", got, fmt.Sprint(got))
	}
	t.Log("GREEN: reset retired A pool; B opened a fresh HTTP/2 connection and A2 reused only B's pool")
}
