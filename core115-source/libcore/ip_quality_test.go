package libcore

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
)

func TestIPPureSessionSignsRetryWithIssuedKey(t *testing.T) {
	const key = "test-session-key"
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("x-k", key)
			w.Header().Set("x-t", strconv.FormatInt(time.Now().UnixMilli(), 10))
			_, _ = io.WriteString(w, `{"ok":false}`)
			return
		}
		signed := r.Header.Get("x-t")
		parts := strings.SplitN(signed, "-", 2)
		if len(parts) != 2 {
			t.Fatalf("missing signed timestamp: %q", signed)
		}
		payload := strings.Join([]string{r.Method, "http://" + r.Host + r.URL.RequestURI(), "", parts[0]}, "-")
		mac := hmac.New(sha256.New, []byte(key))
		_, _ = mac.Write([]byte(payload))
		if r.Header.Get("x-k") != key || parts[1] != hex.EncodeToString(mac.Sum(nil)) {
			t.Fatalf("invalid signed request headers: x-k=%q x-t=%q", r.Header.Get("x-k"), signed)
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer server.Close()

	var response struct {
		OK bool `json:"ok"`
	}
	if err := new(ipPureSession).getJSON(context.Background(), server.Client(), server.URL+"/basic", &response); err != nil {
		t.Fatal(err)
	}
	if !response.OK || calls != 2 {
		t.Fatalf("response=%+v calls=%d", response, calls)
	}
}

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

func TestIPQualityUsesRichIPPureDataWhenMyIPResponseOmitsScore(t *testing.T) {
	server := newIPQualityServer(t, map[string]serverReply{
		"/official":           {body: `{"ip":"198.51.100.9","asn":64496,"isBroadcast":false,"isResidential":false}`},
		"/basic/198.51.100.9": {body: `{"ok":true,"data":{"ip":"198.51.100.9","asn":{"number":64496,"organization":"Example Transit","domain":"example.net","network":{"range":{"start":"198.51.100.0","end":"198.51.100.255"}},"is_residential":false,"type":"hosting"}}}`},
		"/risk/198.51.100.9":  {body: `{"ok":true,"data":{"risk_score":39}}`},
		"/bot/64496":          {body: `{"ok":true,"data":{"bot":37.400209,"human":62.599791}}`},
		"/ip2location":        {body: `{}`},
		"/ipwhois":            {body: `{}`},
		"/dbip":               {body: `{}`},
	})
	defer server.Close()

	result, err := queryIPQuality(server.Client(), testIPQualityEndpoints(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	if result.IP != "198.51.100.9" || result.Score != 39 || result.ScoreTier != "yellow" {
		t.Fatalf("unexpected score result: %+v", result)
	}
	if result.ASDomain != "example.net" || result.IPRangeStart != "198.51.100.0" || result.IPRangeEnd != "198.51.100.255" {
		t.Fatalf("missing ASN details: %+v", result)
	}
	if result.HumanTraffic != 62.599791 || result.BotTraffic != 37.400209 || !result.TrafficKnown {
		t.Fatalf("missing traffic split: %+v", result)
	}
}

func TestIPQualityKeepsOfficialIPWhenRichIPPureDataIsUnavailable(t *testing.T) {
	server := newIPQualityServer(t, map[string]serverReply{
		"/official":    {body: `{"ip":"198.51.100.9","asn":64496,"isBroadcast":false,"isResidential":false}`},
		"/ip2location": {body: `{}`},
		"/ipwhois":     {body: `{}`},
		"/dbip":        {body: `{}`},
	})
	defer server.Close()

	result, err := queryIPQuality(server.Client(), testIPQualityEndpoints(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	if result.IP != "198.51.100.9" || result.Score != -1 || result.ScoreTier != "unknown" {
		t.Fatalf("unexpected partial result: %+v", result)
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
		basic:       base + "/basic/%s",
		risk:        base + "/risk/%s",
		botClass:    base + "/bot/%s",
		ip2Location: base + "/ip2location",
		ipWhoIs:     base + "/ipwhois",
		dbIP:        base + "/dbip",
	}
}
