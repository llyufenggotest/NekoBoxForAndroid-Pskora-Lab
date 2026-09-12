package tunnetcontrol

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestLoadOrCreateIdentityPersistsStableRandomIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tunnet", "identity.json")
	first, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	if *first != *second {
		t.Fatalf("identity changed across reload: %#v != %#v", first, second)
	}
	seed, err := base64.RawURLEncoding.DecodeString(first.DevicePrivateSeed)
	if err != nil || len(seed) != ed25519.SeedSize {
		t.Fatalf("invalid generated seed")
	}
	key, err := first.PrivateKey()
	if err != nil || !bytes.Equal(key.Seed(), seed) {
		t.Fatalf("private key is not deterministically derived from seed")
	}
}

func TestLoadOrCreateIdentityIsRaceSafe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tunnet", "identity.json")
	const workers = 8
	identities := make(chan *Identity, workers)
	errors := make(chan error, workers)
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			identity, err := LoadOrCreateIdentity(path)
			identities <- identity
			errors <- err
		}()
	}
	group.Wait()
	close(identities)
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	var expected *Identity
	for identity := range identities {
		if expected == nil {
			expected = identity
		} else if *identity != *expected {
			t.Fatalf("concurrent creation produced multiple identities")
		}
	}
}

func TestCorruptIdentityFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"client_id":"bad","device_private_seed":"bad"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateIdentity(path); err == nil {
		t.Fatal("corrupt identity must not be silently rotated")
	}
}

func TestGeneratedClientIDUsesVersion8AndRFCVariant(t *testing.T) {
	identity, err := LoadOrCreateIdentity(filepath.Join(t.TempDir(), "identity.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !validClientID(identity.ClientID) {
		t.Fatalf("invalid generated client id: %q", identity.ClientID)
	}
}
