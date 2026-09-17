package snellv4

import (
	"bytes"
	"testing"
)

func TestIdentityV2GoldenVectorsFromMihomo(t *testing.T) {
	psk := []byte("test-snell-ech-identity-v2")
	exporter := make([]byte, IdentityExporterLength)
	salt := make([]byte, snellSaltLen)
	for i := range exporter {
		exporter[i] = byte(i)
	}
	for i := range salt {
		salt[i] = byte(0xf0 + i)
	}

	identity := IdentityV2HeaderFromPSK(psk)
	wantIdentity := []byte{0x39, 0x64, 0x4b, 0x63, 0x12, 0x2b, 0x80, 0x57, 0xa8, 0xf9, 0x14, 0x9f, 0x1d, 0x9c, 0x80, 0x5c}
	if !bytes.Equal(identity, wantIdentity) {
		t.Fatalf("identity = %x, want %x", identity, wantIdentity)
	}

	tag, err := IdentityV2AuthTag(psk, exporter, salt)
	if err != nil {
		t.Fatal(err)
	}
	wantTag := []byte{0xba, 0xbe, 0x8b, 0xba, 0x8b, 0x99, 0x27, 0x9b, 0x49, 0x51, 0x62, 0x7c, 0x50, 0x6d, 0x2d, 0x9d}
	if !bytes.Equal(tag, wantTag) {
		t.Fatalf("tag = %x, want %x", tag, wantTag)
	}
}

func TestIdentityV2FirstFrameAndOrdinaryFallback(t *testing.T) {
	psk := []byte("password")
	exporter := make([]byte, IdentityExporterLength)
	var oix bytes.Buffer
	oixWriter := &writer{upstream: &oix, psk: psk, identityExporter: exporter}
	if _, err := oixWriter.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	wire := oix.Bytes()
	start := snellSaltLen
	if got := string(wire[start : start+len(identityWireMagicV2)]); got != identityWireMagicV2 {
		t.Fatalf("identity magic = %q", got)
	}
	identityEnd := start + len(identityWireMagicV2) + IdentityHeaderLength
	tagEnd := identityEnd + IdentityAuthTagLength
	wantTag, err := IdentityV2AuthTag(psk, exporter, wire[:snellSaltLen])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(wire[identityEnd:tagEnd], wantTag) {
		t.Fatalf("identity authentication tag = %x, want %x", wire[identityEnd:tagEnd], wantTag)
	}

	var ordinary bytes.Buffer
	ordinaryWriter := &writer{upstream: &ordinary, psk: psk}
	if _, err := ordinaryWriter.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ordinary.Bytes()[snellSaltLen:], []byte(identityWireMagicV2)) {
		t.Fatal("ordinary writer emitted OIX identity")
	}
}
