package libcore

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
)

func TestIPQualityOfficialScoreTiers(t *testing.T) {
	for _, test := range []struct {
		score int
		tier  string
	}{
		{score: 0, tier: "green"},
		{score: 24, tier: "green"},
		{score: 25, tier: "yellow"},
		{score: 69, tier: "yellow"},
		{score: 70, tier: "red"},
		{score: 100, tier: "red"},
	} {
		t.Run(fmt.Sprintf("%d_is_%s", test.score, test.tier), func(t *testing.T) {
			server := newIPQualityServer(t, map[string]serverReply{
				"/official":    {body: fmt.Sprintf(`{"ip":"203.0.113.7","asn":64500,"asOrganization":"Example Transit","countryCode":"US","country":"United States","region":"Virginia","city":"Ashburn","isBroadcast":false,"isResidential":true,"fraudScore":%d}`, test.score)},
				"/ip2location": {body: `{}`},
				"/ipwhois":     {body: `{}`},
				"/dbip":        {body: `{}`},
			})
			defer server.Close()

			result, err := queryIPQuality(server.Client(), testIPQualityEndpoints(server.URL))
			if err != nil {
				t.Fatal(err)
			}
			if result.Score != test.score || result.ScoreTier != test.tier {
				t.Fatalf("score/tier = %d/%q, want %d/%q", result.Score, result.ScoreTier, test.score, test.tier)
			}
			if result.IP != "203.0.113.7" || result.ASN != "AS64500 Example Transit" || result.IPSource != "native" || result.IPAttribute != "residential" {
				t.Fatalf("unexpected official fields: %+v", result)
			}
		})
	}
}

func TestIPQualityKeepsPartialResultsWhenSourceFails(t *testing.T) {
	server := newIPQualityServer(t, map[string]serverReply{
		"/official":    {body: `{"ip":"198.51.100.9","asn":64496,"fraudScore":31,"isBroadcast":true,"isResidential":false}`},
		"/ip2location": {status: http.StatusBadGateway, body: `upstream failed`},
		"/ipwhois":     {body: `{"success":true,"country":"Canada","region":"Ontario","city":"Toronto"}`},
		"/dbip":        {body: `{"countryName":"Canada","stateProv":"Ontario","city":"Toronto"}`},
	})
	defer server.Close()

	result, err := queryIPQuality(server.Client(), testIPQualityEndpoints(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Locations) != 2 {
		t.Fatalf("locations = %+v", result.Locations)
	}
	if result.Errors["IP2Location"] == "" {
		t.Fatalf("missing source error: %+v", result.Errors)
	}
	if result.Score != 31 || result.ScoreTier != "yellow" {
		t.Fatalf("official score lost: %+v", result)
	}
}

func TestIPQualityRejectsMissingOfficialScore(t *testing.T) {
	server := newIPQualityServer(t, map[string]serverReply{
		"/official": {body: `{"ip":"198.51.100.9","isBroadcast":false,"isResidential":false}`},
	})
	defer server.Close()

	_, err := queryIPQuality(server.Client(), testIPQualityEndpoints(server.URL))
	if err == nil || !strings.Contains(err.Error(), "no fraud score") {
		t.Fatalf("error = %v, want missing fraud score", err)
	}
}

func TestIPQualityRepresentsMissingOfficialBooleansAsUnknown(t *testing.T) {
	server := newIPQualityServer(t, map[string]serverReply{
		"/official":    {body: `{"ip":"198.51.100.9","fraudScore":7}`},
		"/ip2location": {body: `{}`},
		"/ipwhois":     {body: `{}`},
		"/dbip":        {body: `{}`},
	})
	defer server.Close()

	result, err := queryIPQuality(server.Client(), testIPQualityEndpoints(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	if result.IPSource != "unknown" || result.IPAttribute != "unknown" {
		t.Fatalf("source/attribute = %q/%q, want unknown/unknown", result.IPSource, result.IPAttribute)
	}
}

func TestIPQualityBoundsResponseBodies(t *testing.T) {
	server := newIPQualityServer(t, map[string]serverReply{
		"/official":    {body: `{"ip":"192.0.2.4","fraudScore":9}`},
		"/ip2location": {body: strings.Repeat("x", ipQualityMaxBodyBytes+1)},
		"/ipwhois":     {body: `{"success":true,"country":"Japan","region":"Tokyo","city":"Tokyo"}`},
		"/dbip":        {body: `{}`},
	})
	defer server.Close()

	result, err := queryIPQuality(server.Client(), testIPQualityEndpoints(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Errors["IP2Location"], "response too large") {
		t.Fatalf("oversized body error = %q", result.Errors["IP2Location"])
	}
	if len(result.Locations) != 1 || result.Locations[0].Provider != "ipwho.is" {
		t.Fatalf("bounded source prevented partial result: %+v", result.Locations)
	}
}

func TestBoxInstanceQueryIPQualityUsesRoutedClient(t *testing.T) {
	server := newIPQualityServer(t, map[string]serverReply{
		"/official":    {body: `{"ip":"203.0.113.8","fraudScore":70}`},
		"/ip2location": {body: `{}`},
		"/ipwhois":     {body: `{}`},
		"/dbip":        {body: `{}`},
	})
	defer server.Close()

	oldEndpoints, oldFactory := activeIPQualityEndpoints, newIPQualityHTTPClient
	activeIPQualityEndpoints = testIPQualityEndpoints(server.URL)
	defer func() {
		activeIPQualityEndpoints = oldEndpoints
		newIPQualityHTTPClient = oldFactory
	}()

	boxValue := new(box.Box)
	instance := &BoxInstance{Box: boxValue, state: 1}
	var gotBox *box.Box
	newIPQualityHTTPClient = func(b *box.Box, tracker adapter.ConnectionTracker) *http.Client {
		gotBox = b
		if tracker != nil {
			t.Fatal("unexpected tracker")
		}
		return server.Client()
	}

	raw, err := instance.QueryIPQuality()
	if err != nil {
		t.Fatal(err)
	}
	if gotBox != boxValue {
		t.Fatal("query did not build its HTTP client from the active BoxInstance")
	}
	var result ipQualityResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("invalid JSON %q: %v", raw, err)
	}
	if result.IP != "203.0.113.8" || result.ScoreTier != "red" {
		t.Fatalf("result = %+v", result)
	}
}

func TestBoxInstanceQueryIPQualityRejectsInactiveInstance(t *testing.T) {
	instance := &BoxInstance{}
	if _, err := instance.QueryIPQuality(); err == nil {
		t.Fatal("expected inactive instance error")
	}
}

type serverReply struct {
	status int
	body   string
}

func newIPQualityServer(t *testing.T, replies map[string]serverReply) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reply, ok := replies[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("Accept = %q", r.Header.Get("Accept"))
		}
		status := reply.status
		if status == 0 {
			status = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, reply.body)
	}))
}

func testIPQualityEndpoints(base string) ipQualityEndpoints {
	return ipQualityEndpoints{
		official:    base + "/official",
		ip2Location: base + "/ip2location",
		ipWhoIs:     base + "/ipwhois",
		dbIP:        base + "/dbip",
	}
}
