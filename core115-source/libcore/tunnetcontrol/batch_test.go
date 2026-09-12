package tunnetcontrol

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type countingProber struct {
	mu    sync.Mutex
	calls map[string]int
}

func (p *countingProber) Probe(_ context.Context, address string) (time.Duration, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls[address]++
	return 7 * time.Millisecond, nil
}

func testBatchCatalog(t *testing.T) (*batchCatalog, *countingProber) {
	t.Helper()
	identity := testIdentity(t)
	bootstrap := json.RawMessage(`{"schema_version":2,"access":{"state":"ready"},"runtime":{"client_id":"` + identity.ClientID + `","network":{"root_domains":["edge.example"]},"entry_nodes":[{"name":"entry-a","ipv4":["203.0.113.10"],"front_proxy":{"endpoint":"http://127.0.0.1:8080","headers":{"Host":"front.example","X-T5-Auth":"auth"}}}],"hosts":[{"slug":"sin-03","online":true,"vless_encryption_key":"key"}]}}`)
	synced := json.RawMessage(`{"schema_version":2,"access":{"state":"ready"},"runtime":{"client_id":"` + identity.ClientID + `","entry_nodes":[{"name":"entry-a","ipv4":["203.0.113.10"],"front_proxy":{"endpoint":"http://127.0.0.1:8080","headers":{"Host":"front.example","X-T5-Auth":"auth"}}}],"hosts":[{"slug":"sin-03","online":true,"vless_encryption_key":"key"}]}}`)
	prober := &countingProber{calls: map[string]int{}}
	return &batchCatalog{
		root:       t.TempDir(),
		bootstrap:  bootstrap,
		synced:     synced,
		ech:        ECHConfig{QueryName: echQueryName, ConfigList: "AQID", ExpiresAt: time.Now().Add(time.Hour).UnixMilli()},
		identity:   identity,
		routeProbe: newCachedRouteProber(prober),
	}, prober
}

func TestBatchMaterializationCreatesUniqueImmutableSnapshotsAndReusesProbe(t *testing.T) {
	catalog, prober := testBatchCatalog(t)
	token, err := registerBatch(catalog)
	if err != nil {
		t.Fatal(err)
	}
	defer ReleaseBatch(token)

	first, err := MaterializeBatch(token, "", "entry-a", "sin-03")
	if err != nil {
		t.Fatal(err)
	}
	second, err := MaterializeBatch(token, "", "entry-a", "sin-03")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("batch materializations reused a mutable path")
	}
	for _, path := range []string{first, second} {
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatal(statErr)
		}
		if info.Mode().Perm()&0222 != 0 {
			t.Fatalf("snapshot is writable: %s", info.Mode())
		}
	}
	if prober.calls["203.0.113.10"] != 1 {
		t.Fatalf("route was probed %d times", prober.calls["203.0.113.10"])
	}
}

func TestResolveBatchIngressReturnsIPAndLatencyAndReleaseCleansUp(t *testing.T) {
	catalog, prober := testBatchCatalog(t)
	token, err := registerBatch(catalog)
	if err != nil {
		t.Fatal(err)
	}
	resultJSON, err := ResolveBatchIngress(token, "entry-a")
	if err != nil {
		t.Fatal(err)
	}
	var result IngressProbeResult
	if err := json.Unmarshal([]byte(resultJSON), &result); err != nil {
		t.Fatal(err)
	}
	if result.Address != "203.0.113.10" || result.Port != 443 || result.LatencyMillis != 7 {
		t.Fatalf("unexpected ingress result: %#v", result)
	}
	path, err := MaterializeBatch(token, "", "entry-a", "sin-03")
	if err != nil {
		t.Fatal(err)
	}
	if prober.calls["203.0.113.10"] != 1 {
		t.Fatalf("ingress route cache was not reused: %#v", prober.calls)
	}
	if err = ReleaseBatch(token); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("released snapshot still exists: %v", err)
	}
	if _, err = MaterializeBatch(token, "", "entry-a", "sin-03"); err == nil {
		t.Fatal("released batch token remained usable")
	}
}

func TestMaterializeBatchSnapshotUsesCallerPathOnce(t *testing.T) {
	catalog, _ := testBatchCatalog(t)
	token, err := registerBatch(catalog)
	if err != nil {
		t.Fatal(err)
	}
	defer ReleaseBatch(token)
	path := filepath.Join(t.TempDir(), "entry-a-sin-03.json")
	if err = MaterializeBatchSnapshot(token, "entry-a", "sin-03", path); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if err = MaterializeBatchSnapshot(token, "entry-a", "sin-03", path); err == nil {
		t.Fatal("immutable snapshot path was overwritten")
	}
}

func TestBatchRejectsTraversalSelectors(t *testing.T) {
	catalog, _ := testBatchCatalog(t)
	token, err := registerBatch(catalog)
	if err != nil {
		t.Fatal(err)
	}
	defer ReleaseBatch(token)
	if _, err = MaterializeBatch(token, "", filepath.Join("..", "entry-a"), "sin-03"); err == nil {
		t.Fatal("accepted traversal-like selector")
	}
}
