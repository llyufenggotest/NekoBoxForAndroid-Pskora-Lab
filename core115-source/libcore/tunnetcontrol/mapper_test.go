package tunnetcontrol

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func mapperFixtures(id string) (json.RawMessage, json.RawMessage) {
	bootstrap := json.RawMessage(`{"schema_version":2,"access":{"state":"ready"},"runtime":{"client_id":"` + id + `","entry_nodes":[{"name":"entry-a","ipv4":["203.0.113.10"],"front_proxy":{"endpoint":"http://127.0.0.1:8080","headers":{"Host":"front.example","X-T5-Auth":"auth"}}}],"hosts":[{"slug":"sin-03","online":true,"vless_encryption_key":"key"}]}}`)
	sync := json.RawMessage(`{"schema_version":2,"access":{"state":"ready"},"runtime":{"client_id":"` + id + `","entry_nodes":[{"name":"entry-a","ipv4":["203.0.113.10"],"front_proxy":{"endpoint":"http://127.0.0.1:8080","headers":{"Host":"front.example","X-T5-Auth":"auth"}}}],"hosts":[{"slug":"sin-03","online":true,"vless_encryption_key":"key"}]}}`)
	return bootstrap, sync
}

func TestResolveDataAuthorityUsesIndexedRuntimeRootDomain(t *testing.T) {
	synced := json.RawMessage(`{"schema_version":2,"runtime":{"network":{"root_domains":["jp.edge.example","us.edge.example"]},"hosts":[{"slug":"jp-01","online":true,"load_percent":10,"vless_encryption_key":"key-a"},{"slug":"us-01","online":true,"load_percent":90,"vless_encryption_key":"key-b"}]}}`)
	authority, err := ResolveDataAuthority(synced, "", "us-01")
	if err != nil {
		t.Fatal(err)
	}
	if authority != "us-01.us.edge.example" {
		t.Fatalf("unexpected indexed authority: %q", authority)
	}
}

func TestMapSnapshotBuildsValidatedPublication(t *testing.T) {
	identity := testIdentity(t)
	bootstrap, synced := mapperFixtures(identity.ClientID)
	now := time.Now()
	snapshot, err := MapSnapshotWithOptions(context.Background(), bootstrap, synced, ECHConfig{QueryName: echQueryName, ConfigList: "AQID", ExpiresAt: now.Add(time.Hour).UnixMilli()}, identity, MapOptions{
		DataAuthority:     "sin-03.data.example",
		PreviousEntryNode: "entry-a",
		RouteProber:       fixtureProber{"203.0.113.10": {latency: time.Millisecond}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ActiveEntryNode != "entry-a" || snapshot.SelectedHost != "sin-03" || snapshot.XHTTPAuthority != "sin-03.data.example" || snapshot.ValidatedRoute != "203.0.113.10" || !strings.HasPrefix(snapshot.XHTTPPath, "/api/v1/sync/") {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
	if err := validateSnapshot(snapshot, now); err != nil {
		t.Fatal(err)
	}
}

func TestMapSnapshotFailsClosedOnUnknownSchema(t *testing.T) {
	identity := testIdentity(t)
	_, err := MapSnapshotWithOptions(context.Background(), json.RawMessage(`{}`), json.RawMessage(`{"schema_version":3,"access":{"state":"ready"},"runtime":{}}`), ECHConfig{}, identity, MapOptions{})
	if err == nil || !strings.Contains(err.Error(), "schema/runtime") {
		t.Fatalf("expected schema rejection, got %v", err)
	}
}

func TestMapSnapshotUsesBootstrapRootDomainsWhenSyncOmitsNetwork(t *testing.T) {
	identity := testIdentity(t)
	bootstrap := json.RawMessage(`{"schema_version":2,"access":{"state":"ready"},"runtime":{"client_id":"` + identity.ClientID + `","network":{"root_domains":["jp.edge.example"]},"entry_nodes":[{"name":"entry-a","ipv4":["203.0.113.10"],"front_proxy":{"endpoint":"http://127.0.0.1:8080","headers":{"Host":"front.example","X-T5-Auth":"auth"}}}],"hosts":[{"slug":"jp-01","online":true,"vless_encryption_key":"key"}]}}`)
	synced := json.RawMessage(`{"schema_version":2,"access":{"state":"ready"},"runtime":{"client_id":"` + identity.ClientID + `","entry_nodes":[{"name":"entry-a","ipv4":["203.0.113.10"],"front_proxy":{"endpoint":"http://127.0.0.1:8080","headers":{"Host":"front.example","X-T5-Auth":"auth"}}}],"hosts":[{"slug":"jp-01","online":true,"vless_encryption_key":"key"}]}}`)
	now := time.Now()
	snapshot, err := MapSnapshotWithOptions(context.Background(), bootstrap, synced, ECHConfig{QueryName: echQueryName, ConfigList: "AQID", ExpiresAt: now.Add(time.Hour).UnixMilli()}, identity, MapOptions{RouteProber: fixtureProber{"203.0.113.10": {latency: time.Millisecond}}})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.XHTTPAuthority != "jp-01.jp.edge.example" {
		t.Fatalf("unexpected bootstrap authority: %q", snapshot.XHTTPAuthority)
	}
}

func TestMapSnapshotRejectsBootstrapSyncHostReordering(t *testing.T) {
	identity := testIdentity(t)
	bootstrap := json.RawMessage(`{"schema_version":2,"access":{"state":"ready"},"runtime":{"client_id":"` + identity.ClientID + `","network":{"root_domains":["jp.edge.example","us.edge.example"]},"entry_nodes":[{"name":"entry-a","ipv4":["203.0.113.10"],"front_proxy":{"endpoint":"http://127.0.0.1:8080","headers":{"Host":"front.example","X-T5-Auth":"auth"}}}],"hosts":[{"slug":"jp-01","online":true,"vless_encryption_key":"key-a"},{"slug":"us-01","online":true,"vless_encryption_key":"key-b"}]}}`)
	synced := json.RawMessage(`{"schema_version":2,"access":{"state":"ready"},"runtime":{"client_id":"` + identity.ClientID + `","entry_nodes":[{"name":"entry-a","ipv4":["203.0.113.10"],"front_proxy":{"endpoint":"http://127.0.0.1:8080","headers":{"Host":"front.example","X-T5-Auth":"auth"}}}],"hosts":[{"slug":"us-01","online":true,"vless_encryption_key":"key-b"},{"slug":"jp-01","online":true,"vless_encryption_key":"key-a"}]}}`)
	now := time.Now()
	_, err := MapSnapshotWithOptions(context.Background(), bootstrap, synced, ECHConfig{QueryName: echQueryName, ConfigList: "AQID", ExpiresAt: now.Add(time.Hour).UnixMilli()}, identity, MapOptions{RouteProber: fixtureProber{"203.0.113.10": {latency: time.Millisecond}}})
	if err == nil || !strings.Contains(err.Error(), "catalog order mismatch") {
		t.Fatalf("expected fail-closed host reorder error, got %v", err)
	}
}

func TestMapSnapshotHonorsRequestedEntryAndHost(t *testing.T) {
	identity := testIdentity(t)
	bootstrap := json.RawMessage(`{"schema_version":2,"access":{"state":"ready"},"runtime":{"client_id":"` + identity.ClientID + `","active_entry_node":"entry-a"}}`)
	synced := json.RawMessage(`{"schema_version":2,"access":{"state":"ready"},"runtime":{"client_id":"` + identity.ClientID + `","entry_nodes":[{"name":"entry-a","ipv4":["203.0.113.10"],"front_proxy":{"endpoint":"http://127.0.0.1:8080","headers":{"Host":"front.example","X-T5-Auth":"auth"}}},{"name":"entry-b","ipv4":["203.0.113.11"],"front_proxy":{"endpoint":"http://127.0.0.1:8081","headers":{"Host":"front.example","X-T5-Auth":"auth"}}}],"hosts":[{"slug":"jp-01","online":true,"load_percent":10,"vless_encryption_key":"key-a"},{"slug":"us-01","online":true,"load_percent":90,"vless_encryption_key":"key-b"}]}}`)
	now := time.Now()
	snapshot, err := MapSnapshotWithOptions(context.Background(), bootstrap, synced, ECHConfig{QueryName: echQueryName, ConfigList: "AQID", ExpiresAt: now.Add(time.Hour).UnixMilli()}, identity, MapOptions{
		DataAuthority: "jp-01.data.example", RequestedEntryNode: "entry-b", RequestedHostSlug: "us-01",
		RouteProber: fixtureProber{"203.0.113.11": {latency: time.Millisecond}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ActiveEntryNode != "entry-b" || snapshot.SelectedHost != "us-01" || snapshot.XHTTPAuthority != "us-01.data.example" {
		t.Fatalf("requested selection was not honored: %#v", snapshot)
	}
}

func TestMapSnapshotRejectsUnavailableRequestedHost(t *testing.T) {
	identity := testIdentity(t)
	bootstrap, synced := mapperFixtures(identity.ClientID)
	_, err := MapSnapshotWithOptions(context.Background(), bootstrap, synced, ECHConfig{QueryName: echQueryName, ConfigList: "AQID", ExpiresAt: time.Now().Add(time.Hour).UnixMilli()}, identity, MapOptions{
		DataAuthority: "sin-03.data.example", RequestedEntryNode: "entry-a", RequestedHostSlug: "missing",
		RouteProber: fixtureProber{"203.0.113.10": {latency: time.Millisecond}},
	})
	if err == nil || !strings.Contains(err.Error(), "requested TunNet host") {
		t.Fatalf("expected requested-host rejection, got %v", err)
	}
}

func TestMapSnapshotFailsClosedOnInvalidSelectionOrRoute(t *testing.T) {
	identity := testIdentity(t)
	bootstrap, synced := mapperFixtures(identity.ClientID)
	ech := ECHConfig{QueryName: echQueryName, ConfigList: "AQID", ExpiresAt: time.Now().Add(time.Hour).UnixMilli()}
	for _, options := range []MapOptions{
		{DataAuthority: "invalid", PreviousEntryNode: "entry-a", RouteProber: fixtureProber{"203.0.113.10": {latency: time.Millisecond}}},
		{DataAuthority: "sin-03.data.example", PreviousEntryNode: "missing", RouteProber: fixtureProber{"203.0.113.10": {latency: time.Millisecond}}},
		{DataAuthority: "sin-03.data.example", PreviousEntryNode: "entry-a", RouteProber: fixtureProber{}},
	} {
		if _, err := MapSnapshotWithOptions(context.Background(), bootstrap, synced, ech, identity, options); err == nil {
			t.Fatalf("accepted invalid options: %#v", options)
		}
	}
}
