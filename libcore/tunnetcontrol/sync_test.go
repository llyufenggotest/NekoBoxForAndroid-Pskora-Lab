package tunnetcontrol

import (
	"context"
	"crypto/ecdh"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSyncToSnapshotRunsBootstrapAccessSyncAndAtomicWrite(t *testing.T) {
	var operations []string
	responses := []string{
		`{"schema_version":2,"access":{"state":"required","ticket":"ticket-a","authorization_url":"https://verify.invalid/start"}}`,
		`{"state":"completed","bootstrap":{"schema_version":2,"access":{"state":"ready"},"runtime":{"client_id":""}}}`,
		`{"schema_version":2,"access":{"state":"ready"},"runtime":{"client_id":"","hosts":[{"slug":"sin-03","online":true,"load_percent":1,"vless_encryption_key":"fixture"}]}}`,
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "verify.invalid" {
			bodyBytes, _ := io.ReadAll(request.Body)
			if request.URL.Path != "/api/v1/access/authorize" || string(bodyBytes) != `{"ticket":"ticket-a"}` {
				t.Fatalf("unexpected authorization request: %s %s", request.URL, bodyBytes)
			}
			return jsonResponse(http.StatusOK, `{"state":"completed"}`), nil
		}
		if request.URL.Host == "223.5.5.5" || request.URL.Host == "cloudflare-dns.com" {
			return echDNSResponse(t, echQueryName, []byte{1, 2, 3}, 3600), nil
		}
		input := request.Header.Get("Signature-Input")
		for _, operation := range []string{"bootstrap", "access", "sync"} {
			if strings.Contains(input, `tag="`+operation+`:tunnet-client-v1"`) {
				operations = append(operations, operation)
			}
		}
		bodyBytes, _ := io.ReadAll(request.Body)
		if operations[len(operations)-1] == "bootstrap" || operations[len(operations)-1] == "sync" {
			var identity identityRequest
			if err := json.Unmarshal(bodyBytes, &identity); err != nil || identity.Platform != "android" || identity.AppVersion != "0.2.6" || identity.ClientID == "" {
				t.Fatalf("unexpected identity request: %s", bodyBytes)
			}
		}
		body := responses[len(operations)-1]
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{successMediaType}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	directory := t.TempDir()
	now := time.Now()
	mapper := func(_, _ json.RawMessage, _ ECHConfig, identity *Identity) (*Snapshot, error) {
		snapshot := validSnapshot(now)
		snapshot.ClientID = identity.ClientID
		return snapshot, nil
	}
	err := SyncToSnapshot(context.Background(), SyncOptions{
		Endpoint:      "https://control.example/api/v1/client",
		IdentityPath:  filepath.Join(directory, "identity.json"),
		SnapshotPath:  filepath.Join(directory, "snapshot.json"),
		AppVersion:    "0.2.6",
		DataAuthority: "old.data.example",
		Timeout:       time.Second,
		PollInterval:  time.Millisecond,
		HTTPClient:    client,
		OpenResponse: openerFunc(func(_ *ecdh.PrivateKey, sealed []byte, _ string, _ string, _ []byte) ([]byte, error) {
			return sealed, nil
		}),
		BuildSnapshot: mapper,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(operations, ",") != "bootstrap,access,sync" {
		t.Fatalf("unexpected operations: %#v", operations)
	}
	if _, err := os.Stat(filepath.Join(directory, "snapshot.json")); err != nil {
		t.Fatal("snapshot was not installed")
	}
}

func TestSyncToSnapshotFailsClosedWithIncompleteOptions(t *testing.T) {
	err := SyncToSnapshot(context.Background(), SyncOptions{})
	if err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("expected incomplete-options failure, got %v", err)
	}
}
