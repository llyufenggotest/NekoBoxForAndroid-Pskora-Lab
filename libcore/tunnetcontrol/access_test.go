package tunnetcontrol

import (
	"context"
	"crypto/ecdh"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAccessMachineCompletesAuthorizeAndPoll(t *testing.T) {
	controlIndex := 0
	controlResponses := []string{
		`{"schema_version":2,"access":{"state":"required","ticket":"ticket-1","retry_after_seconds":9,"authorization_url":"https://auth.example/start"}}`,
		`{"state":"completed","bootstrap":{"schema_version":2,"access":{"state":"ready"},"runtime":{}}}`,
	}
	client := &Client{
		Endpoint: mustURL(t, "https://control.example/api/v1/client"),
		Identity: testIdentity(t),
		Opener: openerFunc(func(_ *ecdh.PrivateKey, sealed []byte, _ string, _ string, _ []byte) ([]byte, error) {
			return sealed, nil
		}),
	}
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(request.Body)
		if request.URL.Host == "auth.example" {
			if request.URL.Path != "/api/v1/access/authorize" || request.Header.Get("Origin") != "https://auth.example" || string(body) != `{"ticket":"ticket-1"}` {
				t.Fatalf("unexpected authorize request: %s %s %s", request.URL, request.Header.Get("Origin"), body)
			}
			return jsonResponse(http.StatusOK, `{"state":"completed"}`), nil
		}
		if controlIndex >= len(controlResponses) {
			t.Fatal("unexpected extra control request")
		}
		if controlIndex == 1 && string(body) != `{"ticket":"ticket-1","app_version":"0.2.6"}` {
			t.Fatalf("unexpected poll body: %s", body)
		}
		response := controlResponses[controlIndex]
		controlIndex++
		return sealedResponse(response), nil
	})
	client.HTTPClient = &http.Client{Transport: transport}
	var waits []time.Duration
	var updates []string
	machine := &AccessMachine{
		Client:       client,
		AppVersion:   "0.2.6",
		Platform:     "android",
		HTTPClient:   client.HTTPClient,
		PollInterval: 5 * time.Second,
		Wait: func(_ context.Context, delay time.Duration) error {
			waits = append(waits, delay)
			return nil
		},
	}
	result, err := machine.BootstrapAndAwaitReady(context.Background(), func(access Access) { updates = append(updates, access.State) })
	if err != nil {
		t.Fatal(err)
	}
	if result.Access.State != "ready" || len(waits) != 1 || waits[0] != 9*time.Second {
		t.Fatalf("unexpected result or waits: %#v %#v", result.Access, waits)
	}
	if strings.Join(updates, ",") != "required,ready" {
		t.Fatalf("unexpected updates: %#v", updates)
	}
}

func TestAccessMachineRejectsRequiredWithoutTicket(t *testing.T) {
	client := &Client{
		Endpoint: mustURL(t, "https://control.example/api/v1/client"), Identity: testIdentity(t),
		Opener: openerFunc(func(_ *ecdh.PrivateKey, sealed []byte, _ string, _ string, _ []byte) ([]byte, error) {
			return sealed, nil
		}),
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return sealedResponse(`{"schema_version":2,"access":{"state":"required"}}`), nil
		})},
	}
	machine := &AccessMachine{Client: client, AppVersion: "0.2.6", Platform: "android"}
	if _, err := machine.BootstrapAndAwaitReady(context.Background(), nil); err == nil {
		t.Fatal("accepted required access without ticket")
	}
}

func TestAuthorizeAcceptsHTTPSPageFragmentAndStripsItFromAPIRequest(t *testing.T) {
	var received *http.Request
	machine := &AccessMachine{HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		received = request
		return jsonResponse(http.StatusOK, `{"state":"completed"}`), nil
	})}}
	if err := machine.authorize(context.Background(), Access{
		State:             "required",
		Ticket:            "ticket",
		AuthorizationURL: "https://auth.example/#browser-token",
	}); err != nil {
		t.Fatalf("rejected valid browser authorization URL: %v", err)
	}
	if received == nil {
		t.Fatal("authorization request was not sent")
	}
	if got, want := received.URL.String(), "https://auth.example/api/v1/access/authorize"; got != want {
		t.Fatalf("authorization target = %q, want %q", got, want)
	}
}

func TestAuthorizeFailsClosed(t *testing.T) {
	machine := &AccessMachine{}
	for _, rawURL := range []string{"http://auth.example/start", "https://user@auth.example/start", "https://auth.example:444/start", "not a url"} {
		if err := machine.authorize(context.Background(), Access{State: "required", Ticket: "ticket", AuthorizationURL: rawURL}); err == nil {
			t.Fatalf("accepted unsafe authorization URL: %q", rawURL)
		}
	}
	machine.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusFound, `{"state":"completed"}`), nil
	})}
	if err := machine.authorize(context.Background(), Access{State: "required", Ticket: "ticket", AuthorizationURL: "https://auth.example/start"}); err == nil {
		t.Fatal("accepted redirect authorization response")
	}
	machine.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(`{"state":"completed"}`))}, nil
	})}
	if err := machine.authorize(context.Background(), Access{State: "required", Ticket: "ticket", AuthorizationURL: "https://auth.example/start"}); err == nil {
		t.Fatal("accepted wrong authorization content type")
	}
}

func sealedResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{successMediaType}}, Body: io.NopCloser(strings.NewReader(body))}
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}
