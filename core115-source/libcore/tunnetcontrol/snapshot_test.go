package tunnetcontrol

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func validSnapshot(now time.Time) *Snapshot {
	return &Snapshot{
		SchemaVersion:   1,
		ClientID:        "123e4567-e89b-8abc-a456-426614174000",
		ActiveEntryNode: "entry-a",
		SelectedHost:    "sin-03",
		ECHConfig:       SnapshotECH{ConfigList: "fixture-ech", ExpiresAt: now.Add(time.Hour).UnixMilli()},
		EntryNodes: []SnapshotEntry{{
			Name: "entry-a", IPv4: []string{"203.0.113.10"},
			FrontProxy: SnapshotFrontProxy{Endpoint: "http://front.example:443", Headers: map[string]string{"Host": "front.example:443", "X-T5-Auth": "fixture"}},
		}},
		Hosts:          []SnapshotHost{{Slug: "sin-03", Online: true, Authority: "sin.example", VLESSEncryptionKey: "fixture-key"}},
		ValidatedRoute: "203.0.113.20",
		XHTTPAuthority: "sin.example",
		XHTTPPath:      "/api/v1/sync/",
	}
}

func TestWriteSnapshotAtomicReplacesValidatedSnapshot(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	path := filepath.Join(t.TempDir(), "tunnet", "snapshot.json")
	if err := WriteSnapshotAtomic(path, validSnapshot(now), now); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	updated := validSnapshot(now)
	updated.ValidatedRoute = "2001:db8::20"
	if err = WriteSnapshotAtomic(path, updated, now); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(path)
	if bytes.Equal(first, second) || !bytes.Contains(second, []byte("2001:db8::20")) {
		t.Fatal("snapshot was not atomically replaced")
	}
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".tunnet-snapshot-*"))
	if len(matches) != 0 {
		t.Fatalf("temporary files left behind: %#v", matches)
	}
}

func TestWriteSnapshotAtomicPreservesPreviousOnValidationFailure(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	path := filepath.Join(t.TempDir(), "snapshot.json")
	if err := WriteSnapshotAtomic(path, validSnapshot(now), now); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	invalid := validSnapshot(now)
	invalid.ECHConfig.ExpiresAt = now.Add(-time.Second).UnixMilli()
	if err := WriteSnapshotAtomic(path, invalid, now); err == nil {
		t.Fatal("accepted expired ECH")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("invalid update damaged previous snapshot")
	}
}

func TestWriteSnapshotAtomicRejectsIncompleteNetworkMaterial(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	cases := []*Snapshot{validSnapshot(now), validSnapshot(now), validSnapshot(now), validSnapshot(now)}
	cases[0].ValidatedRoute = "host.example"
	cases[1].EntryNodes[0].FrontProxy.Headers = nil
	cases[2].Hosts[0].Online = false
	cases[3].XHTTPPath = "relative"
	for index, snapshot := range cases {
		if err := WriteSnapshotAtomic(filepath.Join(t.TempDir(), "snapshot.json"), snapshot, now); err == nil {
			t.Fatalf("accepted invalid snapshot case %d", index)
		}
	}
}
