package tunnetcontrol

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type openerFunc func(*ecdh.PrivateKey, []byte, string, string, []byte) ([]byte, error)

func (f openerFunc) Open(key *ecdh.PrivateKey, sealed []byte, operation, clientID string, nonce []byte) ([]byte, error) {
	return f(key, sealed, operation, clientID, nonce)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func testIdentity(t *testing.T) *Identity {
	t.Helper()
	identity, err := LoadOrCreateIdentity(t.TempDir() + "/identity.json")
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

func TestClientCallSignsPlainJSONAndOpensSuccess(t *testing.T) {
	identity := testIdentity(t)
	plaintext := []byte(`{"access":{"state":"ready"}}`)
	sealed := []byte("fixture-sealed-response")
	opened := false
	client := &Client{
		Endpoint: mustURL(t, "https://CONTROL.EXAMPLE/api/v1/ignored"),
		Identity: identity,
		Now:      func() time.Time { return time.Unix(1_700_000_000, 0) },
		Opener: openerFunc(func(key *ecdh.PrivateKey, got []byte, _ string, _ string, _ []byte) ([]byte, error) {
			opened = true
			if key == nil || !bytes.Equal(got, sealed) {
				t.Fatal("opener received wrong key or body")
			}
			return plaintext, nil
		}),
	}
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(request.Body)
		if string(body) != `{"ticket":"fixture"}` {
			t.Fatalf("wire body changed: %s", body)
		}
		if request.URL.Path != controlPath || request.Header.Get("Signature") == "" || request.Header.Get(responseKeyHeader) == "" {
			t.Fatal("request was not signed or target was wrong")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{successMediaType}},
			Body:       io.NopCloser(bytes.NewReader(sealed)),
		}, nil
	})}
	var result struct {
		Access struct {
			State string `json:"state"`
		} `json:"access"`
	}
	if err := client.Call(context.Background(), "access", struct {
		Ticket string `json:"ticket"`
	}{Ticket: "fixture"}, &result); err != nil {
		t.Fatal(err)
	}
	if !opened || result.Access.State != "ready" {
		t.Fatal("response was not opened and decoded")
	}
}

func TestClientCallRejectsWrongContentTypes(t *testing.T) {
	for _, tc := range []struct {
		status int
		media  string
	}{
		{http.StatusOK, errorMediaType},
		{http.StatusBadRequest, successMediaType},
	} {
		client := &Client{
			Endpoint: mustURL(t, "https://control.example/api/v1/client"),
			Identity: testIdentity(t),
			Opener:   openerFunc(func(*ecdh.PrivateKey, []byte, string, string, []byte) ([]byte, error) { return nil, nil }),
			HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": []string{tc.media}}, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
			})},
		}
		if err := client.Call(context.Background(), "sync", struct{}{}, nil); err == nil {
			t.Fatalf("accepted status/content-type mismatch: %#v", tc)
		}
	}
}

func TestClientCallParsesRateLimit(t *testing.T) {
	client := &Client{
		Endpoint: mustURL(t, "https://control.example/api/v1/client"),
		Identity: testIdentity(t),
		Now:      func() time.Time { return time.Unix(100, 0) },
		Opener:   openerFunc(func(*ecdh.PrivateKey, []byte, string, string, []byte) ([]byte, error) { return nil, nil }),
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			body, _ := json.Marshal(ControlError{Code: "rate_limited", Message: "retry"})
			return &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Content-Type": []string{errorMediaType}, "Retry-After": []string{"15"}}, Body: io.NopCloser(bytes.NewReader(body))}, nil
		})},
	}
	err := client.Call(context.Background(), "sync", struct{}{}, nil)
	controlError, ok := err.(*ControlError)
	if !ok || controlError.RetryAfter != 15*time.Second {
		t.Fatalf("unexpected rate-limit error: %#v", err)
	}
}

func TestClientCallRejectsRedirectWithoutForwardingSignedRequest(t *testing.T) {
	forwarded := false
	target := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		forwarded = true
	}))
	defer target.Close()
	redirector := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirector.Close()

	endpoint, err := url.Parse(redirector.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{
		Endpoint:   endpoint,
		Identity:   testIdentity(t),
		Opener:     openerFunc(func(*ecdh.PrivateKey, []byte, string, string, []byte) ([]byte, error) { return nil, nil }),
		HTTPClient: redirector.Client(),
	}
	if err = client.Call(context.Background(), "sync", struct{}{}, nil); err == nil {
		t.Fatal("accepted redirected TunNet control request")
	}
	if forwarded {
		t.Fatal("forwarded signed TunNet request to redirect target")
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	value, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
