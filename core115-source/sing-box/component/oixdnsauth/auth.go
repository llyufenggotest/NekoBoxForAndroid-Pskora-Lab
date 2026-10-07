// Package oixdnsauth generates authenticated managed DNS names for Oix.
package oixdnsauth

import (
	"crypto/ed25519"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
)

// BuildSeed is empty in tracked source. Release builds add a private ignored
// init file; putting it in ldflags would expose it in Go build metadata.
var BuildSeed string

// Host returns the authenticated managed name, or the original host when it
// is not in a managed suffix. The caller keeps the original host for TLS/SNI.
func Host(host string, unixSeconds int64) (string, error) {
	normalized := strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "" || (!strings.HasSuffix(normalized, ".cloud-nodes.com") && normalized != "cloud-nodes.com") {
		return host, nil
	}
	seedText := strings.TrimSpace(BuildSeed)
	seed, err := base64.StdEncoding.DecodeString(seedText)
	if err != nil || len(seed) != ed25519.SeedSize {
		return "", errors.New("oix DNS-Auth seed is unavailable")
	}
	message := []byte(normalized + "|" + strconv.FormatInt(unixSeconds/300, 10))
	signature := ed25519.Sign(ed25519.NewKeyFromSeed(seed), message)
	encoding := base32.StdEncoding.WithPadding(base32.NoPadding)
	return strings.ToLower(encoding.EncodeToString(signature[:32])) + "." +
		strings.ToLower(encoding.EncodeToString(signature[32:])) + "." + normalized, nil
}
