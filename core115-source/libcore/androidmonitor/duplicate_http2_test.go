package androidmonitor

import (
	"context"
	"crypto/tls"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/common/control"
	"github.com/sagernet/sing/common/logger"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync"
	"testing"
	"time"
)

type snapshotManager struct {
	fakeManager
	snapshot []adapter.NetworkInterface
}

func (m *snapshotManager) NetworkInterfaces() []adapter.NetworkInterface { return m.snapshot }

// Real TLS+HTTP/2 connection; the callback models core ResetNetwork's CloseAll.
// No retry, traffic-derived success, or timing-based event settling is used.
func TestDuplicateSnapshotPreservesHTTP2AndRealChangesRecover(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "verified-http2-health") }))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	iface := control.Interface{Name: "ccmni1", Index: 10, Addresses: []netip.Prefix{netip.MustParsePrefix("10.0.0.2/24")}}
	nm := &snapshotManager{fakeManager: fakeManager{finder: fakeFinder{interfaces: map[int]*control.Interface{10: &iface}}}, snapshot: []adapter.NetworkInterface{{Interface: iface, DNSServers: []string{"1.1.1.1"}, Gateways: []netip.Addr{netip.MustParseAddr("10.0.0.1")}}}}
	p := &fakePlatform{}
	m := New(p, func() adapter.NetworkManager { return nm }, logger.NOP())
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	var mu sync.Mutex
	var connections []net.Conn
	resets := 0
	m.RegisterCallback(func(_ *control.Interface, _ int) {
		mu.Lock()
		defer mu.Unlock()
		resets++
		for _, c := range connections {
			c.Close()
		}
		connections = nil
	})
	m.UpdateDefaultInterface("ccmni1", 10, false, false)
	tr := &http.Transport{ForceAttemptHTTP2: true, TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, DialContext: func(ctx context.Context, n, a string) (net.Conn, error) {
		c, e := (&net.Dialer{}).DialContext(ctx, n, a)
		if e == nil {
			mu.Lock()
			connections = append(connections, c)
			mu.Unlock()
		}
		return c, e
	}}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 3 * time.Second}
	fetch := func() {
		t.Helper()
		r, e := client.Get(server.URL)
		if e != nil {
			t.Fatal(e)
		}
		defer r.Body.Close()
		body, e := io.ReadAll(r.Body)
		if e != nil || r.ProtoMajor != 2 || string(body) != "verified-http2-health" {
			t.Fatalf("health response: %s %q %v", r.Proto, body, e)
		}
	}
	fetch()
	initial := resets
	for i := 0; i < 3; i++ {
		m.UpdateDefaultInterface("ccmni1", 10, false, false)
	}
	if resets != initial {
		t.Fatalf("duplicate snapshot closed first healthy HTTP/2 connection: resets %d -> %d", initial, resets)
	}
	fetch()
	changes := []struct {
		name   string
		change func()
	}{
		{"IP", func() { nm.snapshot[0].Addresses = []netip.Prefix{netip.MustParsePrefix("10.0.0.3/24")} }},
		{"DNS", func() { nm.snapshot[0].DNSServers = []string{"8.8.8.8"} }},
		{"gateway", func() { nm.snapshot[0].Gateways = []netip.Addr{netip.MustParseAddr("10.0.0.254")} }},
		{"capability", func() { nm.snapshot[0].Constrained = true }},
	}
	for _, change := range changes {
		t.Run(change.name, func(t *testing.T) {
			before := resets
			change.change()
			m.UpdateDefaultInterface("ccmni1", 10, false, false)
			if resets != before+1 {
				t.Fatal("real change did not reset")
			}
			fetch()
			t.Log("real HTTP/2 health recovered after reset")
		})
	}
	before := resets
	m.Close()
	m.UpdateDefaultInterface("ccmni1", 10, true, true)
	if resets != before {
		t.Fatal("expired session notified")
	}
	fetch()
}
