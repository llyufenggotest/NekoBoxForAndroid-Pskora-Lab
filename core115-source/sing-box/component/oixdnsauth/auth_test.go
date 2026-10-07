package oixdnsauth_test

import (
	"crypto/ed25519"
	"encoding/base32"
	"encoding/base64"
	"testing"

	"github.com/sagernet/sing-box/component/oixdnsauth"
)

func TestHostVector(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i)
	}
	oixdnsauth.BuildSeed = base64.StdEncoding.EncodeToString(seed)
	got, err := oixdnsauth.Host("A.example.cloud-nodes.com.", 1700000000)
	if err != nil {
		t.Fatal(err)
	}
	parts := split(got)
	if len(parts) != 6 || len(parts[0]) != 52 || len(parts[1]) != 52 {
		t.Fatalf("bad qname %q", got)
	}
	enc := base32.StdEncoding.WithPadding(base32.NoPadding)
	a, _ := enc.DecodeString(upper(parts[0]))
	b, _ := enc.DecodeString(upper(parts[1]))
	if !ed25519.Verify(ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey), []byte("a.example.cloud-nodes.com|5666666"), append(a, b...)) {
		t.Fatal("signature mismatch")
	}
}

func TestHostLeavesUnmanagedUnchanged(t *testing.T) {
	oixdnsauth.BuildSeed = ""
	for _, host := range []string{"example.org", "cloud-nodes.com.evil.test", "127.0.0.1"} {
		got, err := oixdnsauth.Host(host, 0)
		if err != nil || got != host {
			t.Fatalf("host changed: %q", host)
		}
	}
}

func split(value string) []string {
	out := make([]string, 0, 5)
	for len(value) > 0 {
		i := 0
		for i < len(value) && value[i] != '.' {
			i++
		}
		out = append(out, value[:i])
		if i == len(value) {
			break
		}
		value = value[i+1:]
	}
	return out
}
func upper(value string) string {
	b := []byte(value)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - ('a' - 'A')
		}
	}
	return string(b)
}
