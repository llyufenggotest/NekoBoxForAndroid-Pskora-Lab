package boxapi

import (
	"context"
	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDefaultURLAndStats(t *testing.T) {
	ctx := include.Context(context.Background())
	var opts option.Options
	if e := opts.UnmarshalJSONContext(ctx, []byte(`{"outbounds":[{"type":"direct","tag":"direct"}],"route":{"final":"direct"}}`)); e != nil {
		t.Fatal(e)
	}
	b, e := box.New(box.Options{Context: ctx, Options: opts})
	if e != nil {
		t.Fatal(e)
	}
	if e = b.Start(); e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "bridge-stats") }))
	defer srv.Close()
	stats := NewSbV2rayServer(option.V2RayStatsServiceOptions{Enabled: true, Outbounds: []string{"direct"}})
	client := CreateProxyHttpClient(b, stats.StatsService())
	defer client.CloseIdleConnections()
	resp, e := client.Get(srv.URL)
	if e != nil {
		t.Fatal(e)
	}
	body, e := io.ReadAll(resp.Body)
	resp.Body.Close()
	if e != nil || string(body) != "bridge-stats" {
		t.Fatalf("body %q %v", body, e)
	}
	for _, d := range []string{"uplink", "downlink"} {
		key := "outbound>>>direct>>>traffic>>>" + d
		if stats.QueryStats(key) <= 0 {
			t.Fatal("missing " + d)
		}
		if stats.QueryStats(key) != 0 {
			t.Fatal("reset failed")
		}
	}
}
