package tunnetcontrol

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"testing"
	"time"
)

func TestSignRequestGoldenShapeAndVerification(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i)
	}
	identity := &Identity{SchemaVersion: 1, ClientID: "123e4567-e89b-8abc-a456-426614174000", DevicePrivateSeed: base64.RawURLEncoding.EncodeToString(seed)}
	body := []byte(`{"ticket":"fixture","current_url":"https://example.invalid/access"}`)
	req, err := http.NewRequest(http.MethodPost, "https://CONTROL.EXAMPLE:8443/api/v1/client", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	responseKey := bytes.Repeat([]byte{0xA5}, 32)
	created := time.Unix(1_700_000_000, 0)
	const nonce = "AAAAAAAAAAAAAAAAAAAAAA"
	privateKey, _ := identity.PrivateKey()
	publicKey := privateKey.Public().(ed25519.PublicKey)
	if err = signRequestWithNonce(req, body, "access_refresh", identity.ClientID, privateKey, publicKey, responseKey, created, nonce); err != nil {
		t.Fatal(err)
	}

	digest := sha256.Sum256(body)
	wantDigest := "sha-256=:" + base64.StdEncoding.EncodeToString(digest[:]) + ":"
	if got := req.Header.Get("Content-Digest"); got != wantDigest {
		t.Fatalf("content digest mismatch: %q", got)
	}
	if got := req.Header.Get(devicePublicHeader); got != base64.RawURLEncoding.EncodeToString(publicKey) || len(got) != 43 {
		t.Fatalf("device public key encoding mismatch: %q", got)
	}
	if got := req.Header.Get(responseKeyHeader); got != base64.RawURLEncoding.EncodeToString(responseKey) || len(got) != 43 {
		t.Fatalf("response public key encoding mismatch: %q", got)
	}
	wantParams := `("@method" "@authority" "@path" "content-type" "content-digest" "tunnet-public-key" "tunnet-response-key");created=1700000000;expires=1700000120;keyid="123e4567-e89b-8abc-a456-426614174000";alg="ed25519";nonce="AAAAAAAAAAAAAAAAAAAAAA";tag="access_refresh:tunnet-client-v1"`
	if got := req.Header.Get("Signature-Input"); got != "tn="+wantParams {
		t.Fatalf("signature input mismatch:\n%s", got)
	}
	base := "\"@method\": POST\n" +
		"\"@authority\": control.example:8443\n" +
		"\"@path\": /api/v1/client\n" +
		"\"content-type\": application/json\n" +
		"\"content-digest\": " + wantDigest + "\n" +
		"\"tunnet-public-key\": " + req.Header.Get(devicePublicHeader) + "\n" +
		"\"tunnet-response-key\": " + req.Header.Get(responseKeyHeader) + "\n" +
		"\"@signature-params\": " + wantParams
	signatureValue := req.Header.Get("Signature")
	decoded, err := base64.StdEncoding.DecodeString(signatureValue[len("tn=:") : len(signatureValue)-1])
	if err != nil || !ed25519.Verify(publicKey, []byte(base), decoded) {
		t.Fatal("signature does not verify against canonical base")
	}
	if req.Header.Get("Content-Type") != "application/json" || req.Header.Get("Accept-Encoding") != "identity" {
		t.Fatal("required transport headers missing")
	}
}

func TestSignRequestRejectsWrongTargetAndOperation(t *testing.T) {
	path := t.TempDir() + "/identity.json"
	identity, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	key := make([]byte, 32)
	for _, tc := range []struct {
		url string
		op  string
	}{
		{"https://control.example/api/v1/other", "sync"},
		{"https://control.example/api/v1/client?q=1", "sync"},
		{"https://control.example/api/v1/client", "Access"},
		{"https://control.example/api/v1/client", "access:bad"},
	} {
		req, _ := http.NewRequest(http.MethodPost, tc.url, bytes.NewReader(nil))
		if _, err = SignRequest(req, nil, tc.op, identity, key, time.Unix(1, 0)); err == nil {
			t.Fatalf("accepted invalid request: %#v", tc)
		}
	}
}
