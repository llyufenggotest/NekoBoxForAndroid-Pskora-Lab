package tunnetcontrol

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const identitySchema = 1

var identityCreateMu sync.Mutex

// Identity is local installation state. The seed is never imported from another
// client and is persisted only in the application's private no-backup directory.
type Identity struct {
	SchemaVersion     int    `json:"schema_version"`
	ClientID          string `json:"client_id"`
	DevicePrivateSeed string `json:"device_private_seed"`
}

func LoadOrCreateIdentity(path string) (*Identity, error) {
	identityCreateMu.Lock()
	defer identityCreateMu.Unlock()

	contents, err := os.ReadFile(path)
	if err == nil {
		return parseIdentity(contents)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read TunNet identity: %w", err)
	}
	return createIdentity(path)
}

func parseIdentity(contents []byte) (*Identity, error) {
	var identity Identity
	if err := json.Unmarshal(contents, &identity); err != nil {
		return nil, fmt.Errorf("decode TunNet identity: %w", err)
	}
	if identity.SchemaVersion != identitySchema {
		return nil, fmt.Errorf("unsupported TunNet identity schema: %d", identity.SchemaVersion)
	}
	if !validClientID(identity.ClientID) {
		return nil, errors.New("invalid TunNet client_id")
	}
	seed, err := base64.RawURLEncoding.DecodeString(identity.DevicePrivateSeed)
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, errors.New("invalid TunNet device_private_seed")
	}
	return &identity, nil
}

func createIdentity(path string) (*Identity, error) {
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return nil, fmt.Errorf("generate TunNet device seed: %w", err)
	}
	clientIDBytes := make([]byte, 16)
	if _, err := rand.Read(clientIDBytes); err != nil {
		return nil, fmt.Errorf("generate TunNet client id: %w", err)
	}
	// RFC 4122 variant with the protocol-observed UUID version 8.
	clientIDBytes[6] = (clientIDBytes[6] & 0x0f) | 0x80
	clientIDBytes[8] = (clientIDBytes[8] & 0x3f) | 0x80
	identity := &Identity{
		SchemaVersion:     identitySchema,
		ClientID:          formatUUID(clientIDBytes),
		DevicePrivateSeed: base64.RawURLEncoding.EncodeToString(seed),
	}
	contents, err := json.Marshal(identity)
	if err != nil {
		return nil, fmt.Errorf("encode TunNet identity: %w", err)
	}
	if err = atomicWrite(path, contents, 0600); err != nil {
		return nil, err
	}
	return identity, nil
}

func (i *Identity) PrivateKey() (ed25519.PrivateKey, error) {
	seed, err := base64.RawURLEncoding.DecodeString(i.DevicePrivateSeed)
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, errors.New("invalid TunNet device_private_seed")
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

func atomicWrite(path string, contents []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return fmt.Errorf("create TunNet state directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".tunnet-identity-*")
	if err != nil {
		return fmt.Errorf("create TunNet identity temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err = temporary.Chmod(mode); err == nil {
		_, err = temporary.Write(contents)
	}
	if err == nil {
		err = temporary.Sync()
	}
	closeErr := temporary.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write TunNet identity: %w", err)
	}
	if err = os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace TunNet identity: %w", err)
	}
	return nil
}

func formatUUID(value []byte) string {
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16])
}

func validClientID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' || value[14] != '8' {
		return false
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
			return false
		}
	}
	return value[19] == '8' || value[19] == '9' || value[19] == 'a' || value[19] == 'b'
}
