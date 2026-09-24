package boxapi

import (
	"context"
	"net"
	"net/http"
	"time"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
)

func CreateProxyHttpClient(box *box.Box, tracker adapter.ConnectionTracker) *http.Client {
	return createProxyHTTPClient(box, tracker, "")
}

func CreateProxyHttpClientIPv4(box *box.Box, tracker adapter.ConnectionTracker) *http.Client {
	return createProxyHTTPClient(box, tracker, "tcp4")
}

func proxyHTTPNetwork(network, forcedNetwork string) string {
	if forcedNetwork != "" {
		return forcedNetwork
	}
	return network
}

func createProxyHTTPClient(box *box.Box, tracker adapter.ConnectionTracker, forcedNetwork string) *http.Client {
	transport := &http.Transport{
		TLSHandshakeTimeout:   time.Second * 3,
		ResponseHeaderTimeout: time.Second * 3,
	}

	if box != nil {
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return DialContext(ctx, box, tracker, proxyHTTPNetwork(network, forcedNetwork), addr)
		}
	}

	return &http.Client{Transport: transport}
}
