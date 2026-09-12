package tunnetcontrol

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"testing"
	"time"

	mDNS "github.com/miekg/dns"
)

func echDNSClient(t *testing.T, owner string) *http.Client {
	t.Helper()
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		queryWire, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		var query mDNS.Msg
		if err := query.Unpack(queryWire); err != nil {
			t.Fatal(err)
		}
		echo := base64.StdEncoding.EncodeToString([]byte{1, 2, 3})
		rr, err := mDNS.NewRR(owner + " 300 IN HTTPS 1 . ech=" + echo)
		if err != nil {
			t.Fatal(err)
		}
		answer := new(mDNS.Msg)
		answer.SetReply(&query)
		answer.Answer = []mDNS.RR{rr}
		wire, err := answer.Pack()
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(wire)), Header: make(http.Header)}, nil
	})}
}

func TestFetchECHConfigDNSAcceptsMatchingOwner(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	config, err := FetchECHConfigDNS(context.Background(), echDNSClient(t, echQueryName), echQueryName, now)
	if err != nil {
		t.Fatal(err)
	}
	if config.QueryName != echQueryName || config.ExpiresAt != now.Add(300*time.Second).UnixMilli() {
		t.Fatalf("unexpected ECH config metadata: %#v", config)
	}
}

func TestFetchECHConfigDNSRejectsUnrelatedOwner(t *testing.T) {
	_, err := FetchECHConfigDNS(context.Background(), echDNSClient(t, "unrelated.example."), echQueryName, time.Now())
	if err == nil {
		t.Fatal("accepted ECH config from unrelated DNS owner")
	}
}
