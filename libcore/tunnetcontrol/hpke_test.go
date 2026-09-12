package tunnetcontrol

import (
	"bytes"
	"crypto/ecdh"
	"testing"

	"github.com/cloudflare/circl/hpke"
)

const testClientID = "123e4567-e89b-8abc-a456-426614174000"

func sealTestResponse(t *testing.T, recipient *ecdh.PrivateKey, operation, clientID string, nonce, plaintext []byte) []byte {
	t.Helper()
	info, err := responseContextInfo(operation, clientID, nonce)
	if err != nil {
		t.Fatal(err)
	}
	scheme := hpke.KEM_X25519_HKDF_SHA256.Scheme()
	recipientPub, err := scheme.UnmarshalBinaryPublicKey(recipient.PublicKey().Bytes())
	if err != nil {
		t.Fatal(err)
	}
	sender, err := responseSuite.NewSender(recipientPub, info)
	if err != nil {
		t.Fatal(err)
	}
	enc, sealer, err := sender.Setup(nil)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := sealer.Seal(plaintext, nil)
	if err != nil {
		t.Fatal(err)
	}
	return append(append([]byte(nil), enc...), ciphertext...)
}

func TestHPKEResponseOpenerRoundTripWithRequestContext(t *testing.T) {
	curve := ecdh.X25519()
	recipient, err := curve.NewPrivateKey(bytes.Repeat([]byte{0x11}, 32))
	if err != nil {
		t.Fatal(err)
	}
	nonce := bytes.Repeat([]byte{0x44}, 16)
	plaintext := []byte(`{"fixture":"response"}`)
	sealed := sealTestResponse(t, recipient, "bootstrap", testClientID, nonce, plaintext)

	opened, err := (HPKEResponseOpener{}).Open(recipient, sealed, "bootstrap", testClientID, nonce)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(opened, plaintext) {
		t.Fatalf("plaintext mismatch: %q", opened)
	}
}

func TestHPKEResponseOpenerRejectsWrongRequestContext(t *testing.T) {
	curve := ecdh.X25519()
	recipient, _ := curve.NewPrivateKey(bytes.Repeat([]byte{0x33}, 32))
	nonce := bytes.Repeat([]byte{0x44}, 16)
	sealed := sealTestResponse(t, recipient, "bootstrap", testClientID, nonce, []byte(`{}`))

	wrongNonce := append([]byte(nil), nonce...)
	wrongNonce[0] ^= 1
	for _, tc := range []struct {
		operation string
		clientID  string
		nonce     []byte
	}{
		{"sync", testClientID, nonce},
		{"bootstrap", "123e4567-e89b-8abc-a456-426614174001", nonce},
		{"bootstrap", testClientID, wrongNonce},
	} {
		if _, err := (HPKEResponseOpener{}).Open(recipient, sealed, tc.operation, tc.clientID, tc.nonce); err == nil {
			t.Fatalf("accepted wrong response context: %#v", tc)
		}
	}
}

func TestHPKEResponseOpenerRejectsMalformedSealedResponse(t *testing.T) {
	curve := ecdh.X25519()
	recipient, _ := curve.NewPrivateKey(bytes.Repeat([]byte{0x33}, 32))
	for _, sealed := range [][]byte{nil, make([]byte, 47), make([]byte, 48)} {
		if _, err := (HPKEResponseOpener{}).Open(recipient, sealed, "bootstrap", testClientID, bytes.Repeat([]byte{0x44}, 16)); err == nil {
			t.Fatalf("accepted malformed sealed response of length %d", len(sealed))
		}
	}
}
