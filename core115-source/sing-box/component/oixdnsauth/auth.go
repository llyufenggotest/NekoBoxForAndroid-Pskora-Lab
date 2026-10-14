// Package oixdnsauth generates authenticated managed DNS names for Oix.
package oixdnsauth

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
)

const (
	fingerprintDomain         = "nekoboxyf:oix-dns-auth:runtime-provenance:v1\x00"
	fingerprintMarkerPrefix   = "NEKOBOX-OIX-SEED-FINGERPRINT-v1:"
	fingerprintMarkerSuffix   = ":END-NEKOBOX-OIX-SEED-FINGERPRINT-v1"
	fingerprintChecksumDomain = "nekoboxyf:oix-dns-auth:marker-checksum:v1\x00"
	fingerprintChecksumSize   = 16
)

// BuildSeed and BuildSeedFingerprint are empty in tracked source. Release builds
// add a private ignored init file. The seed is a protocol credential embedded in
// the client; CI keeps it out of Git and logs, not secret from the endpoint.
var BuildSeed string
var BuildSeedFingerprint string

// RuntimeSeedFingerprint validates that the embedded marker was derived from
// the same seed used by Host, and returns the bare fingerprint for attestation.
func RuntimeSeedFingerprint() string {
	seedText := strings.TrimSpace(BuildSeed)
	seed, err := base64.StdEncoding.DecodeString(seedText)
	if err != nil || len(seed) != ed25519.SeedSize {
		return ""
	}
	fingerprintBytes := sha256.Sum256(append([]byte(fingerprintDomain), seed...))
	fingerprint := hex.EncodeToString(fingerprintBytes[:])
	checksumBytes := sha256.Sum256([]byte(fingerprintChecksumDomain + fingerprint))
	checksum := hex.EncodeToString(checksumBytes[:])[:fingerprintChecksumSize]
	expected := fingerprintMarkerPrefix + fingerprint + checksum + fingerprintMarkerSuffix
	if BuildSeedFingerprint != expected {
		return ""
	}
	return fingerprint
}

// Host returns the authenticated managed name, or the original host when it
// is not in a managed suffix. The caller keeps the original host for TLS/SNI.
func Host(host string, unixSeconds int64) (string, error) {
	normalized := strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "" || (!strings.HasSuffix(normalized, ".cloud-nodes.com") && normalized != "cloud-nodes.com") {
		return host, nil
	}
	seedText := strings.TrimSpace(BuildSeed)
	seed, err := base64.StdEncoding.DecodeString(seedText)
	if err != nil || len(seed) != ed25519.SeedSize || RuntimeSeedFingerprint() == "" {
		return "", errors.New("oix DNS-Auth seed provenance is unavailable")
	}
	message := []byte(normalized + "|" + strconv.FormatInt(unixSeconds/300, 10))
	signature := ed25519.Sign(ed25519.NewKeyFromSeed(seed), message)
	encoding := base32.StdEncoding.WithPadding(base32.NoPadding)
	return strings.ToLower(encoding.EncodeToString(signature[:32])) + "." +
		strings.ToLower(encoding.EncodeToString(signature[32:])) + "." + normalized, nil
}
