package tunnetcontrol

import (
	"encoding/base64"
	"io"
	"net/http"
	"testing"
	"time"

	mDNS "github.com/miekg/dns"
)

func echDNSResponse(t *testing.T, owner string, config []byte, ttl uint32) *http.Response {
	t.Helper()
	answer := new(mDNS.Msg)
	answer.SetReply(new(mDNS.Msg))
	answer.Rcode = mDNS.RcodeSuccess
	answer.Answer = []mDNS.RR{&mDNS.HTTPS{SVCB: mDNS.SVCB{
		Hdr:      mDNS.RR_Header{Name: owner, Rrtype: mDNS.TypeHTTPS, Class: mDNS.ClassINET, Ttl: ttl},
		Priority: 1,
		Target:   ".",
		Value:    []mDNS.SVCBKeyValue{&mDNS.SVCBECHConfig{ECH: config}},
	}}}
	wire, err := answer.Pack()
	if err != nil {
		t.Fatal(err)
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/dns-message"}}, Body: io.NopCloser(bytesReader(wire))}
}

type byteReader struct{ b []byte }

func bytesReader(value []byte) *byteReader { return &byteReader{b: value} }
func (r *byteReader) Read(p []byte) (int, error) {
	if len(r.b) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.b)
	r.b = r.b[n:]
	return n, nil
}

func TestFetchECHConfigDNSUsesHTTPSRecordAndTTL(t *testing.T) {
	now := time.UnixMilli(1_780_000_000_000)
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != echDoHURLs[0] || request.Method != http.MethodPost || request.Header.Get("Accept") != "application/dns-message" {
			t.Fatalf("unexpected DoH request: %s %s", request.Method, request.URL)
		}
		return echDNSResponse(t, "sin-03.data.example.", []byte{1, 2, 3}, 60), nil
	})}
	config, err := FetchECHConfigDNS(t.Context(), client, "sin-03.data.example", now)
	if err != nil {
		t.Fatal(err)
	}
	if config.QueryName != "sin-03.data.example." || config.ConfigList != base64.RawURLEncoding.EncodeToString([]byte{1, 2, 3}) || config.ExpiresAt != now.Add(time.Minute).UnixMilli() {
		t.Fatalf("unexpected ECH config: %#v", config)
	}
}

func TestFetchECHConfigDNSFailsClosedWithoutECH(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return echDNSResponse(t, "sin-03.data.example.", nil, 60), nil
	})}
	if _, err := FetchECHConfigDNS(t.Context(), client, "sin-03.data.example", time.Now()); err == nil {
		t.Fatal("accepted empty ECH config")
	}
}

func TestFetchECHConfigDNSFallsBackAfterPrimaryTransportFailure(t *testing.T) {
	attempts := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			if request.URL.String() != echDoHURLs[0] {
				t.Fatalf("unexpected primary resolver: %s", request.URL)
			}
			return nil, io.ErrUnexpectedEOF
		}
		if request.URL.String() != echDoHURLs[1] {
			t.Fatalf("unexpected fallback resolver: %s", request.URL)
		}
		return echDNSResponse(t, echQueryName, []byte{1, 2, 3}, 60), nil
	})}
	if _, err := FetchECHConfigDNS(t.Context(), client, echQueryName, time.Now()); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("expected two resolver attempts, got %d", attempts)
	}
}

func TestECHConfigRejectsExpired(t *testing.T) {
	if (ECHConfig{QueryName: echQueryName, ConfigList: "fixture", ExpiresAt: 9}).Valid(time.UnixMilli(10)) {
		t.Fatal("accepted expired ECH config")
	}
}
