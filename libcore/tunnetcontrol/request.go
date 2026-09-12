package tunnetcontrol

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	controlPath        = "/api/v1/client"
	signatureLifetime  = 120 * time.Second
	devicePublicHeader = "Tunnet-Public-Key"
	responseKeyHeader  = "Tunnet-Response-Key"
)

var (
	operationPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	clientIDPattern  = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-8[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	randomReader     = rand.Reader
)

// SignRequest applies the TunNet RFC 9421 profile to an already-created request.
// body must be the exact bytes assigned to req.Body; callers must not re-marshal it.
func SignRequest(req *http.Request, body []byte, operation string, identity *Identity, responsePublicKey []byte, now time.Time) ([]byte, error) {
	if req == nil || identity == nil {
		return nil, errors.New("missing TunNet request or identity")
	}
	if req.Method != http.MethodPost || req.URL == nil || req.URL.EscapedPath() != controlPath || req.URL.RawQuery != "" {
		return nil, errors.New("invalid TunNet control request target")
	}
	if !operationPattern.MatchString(operation) {
		return nil, errors.New("invalid TunNet operation")
	}
	if len(responsePublicKey) != 32 {
		return nil, errors.New("invalid TunNet response public key")
	}
	privateKey, err := identity.PrivateKey()
	if err != nil {
		return nil, err
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	clientID := strings.ToLower(identity.ClientID)
	if !clientIDPattern.MatchString(clientID) {
		return nil, errors.New("invalid TunNet client ID")
	}

	nonceBytes := make([]byte, 16)
	if _, err = rand.Read(nonceBytes); err != nil {
		return nil, fmt.Errorf("generate TunNet signature nonce: %w", err)
	}
	if err = signRequestWithNonce(req, body, operation, clientID, privateKey, publicKey, responsePublicKey, now, base64.RawURLEncoding.EncodeToString(nonceBytes)); err != nil {
		return nil, err
	}
	return nonceBytes, nil
}

func signRequestWithNonce(req *http.Request, body []byte, operation, clientID string, privateKey ed25519.PrivateKey, publicKey, responsePublicKey []byte, now time.Time, nonce string) error {
	if len(nonce) != 22 || strings.ContainsAny(nonce, `"\\`) {
		return errors.New("invalid TunNet signature nonce")
	}
	authority, err := canonicalAuthority(req.URL)
	if err != nil {
		return err
	}
	created := now.Unix()
	expires := created + int64(signatureLifetime/time.Second)
	devicePublic := base64.RawURLEncoding.EncodeToString(publicKey)
	responsePublic := base64.RawURLEncoding.EncodeToString(responsePublicKey)
	digestBytes := sha256.Sum256(body)
	contentDigest := "sha-256=:" + base64.StdEncoding.EncodeToString(digestBytes[:]) + ":"

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.tunnet.hpke, application/json")
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set(devicePublicHeader, devicePublic)
	req.Header.Set(responseKeyHeader, responsePublic)
	req.Header.Set("Content-Digest", contentDigest)

	params := fmt.Sprintf(`("@method" "@authority" "@path" "content-type" "content-digest" "tunnet-public-key" "tunnet-response-key");created=%d;expires=%d;keyid="%s";alg="ed25519";nonce="%s";tag="%s:tunnet-client-v1"`, created, expires, clientID, nonce, operation)
	base := strings.Join([]string{
		`"@method": POST`,
		`"@authority": ` + authority,
		`"@path": ` + controlPath,
		`"content-type": application/json`,
		`"content-digest": ` + contentDigest,
		`"tunnet-public-key": ` + devicePublic,
		`"tunnet-response-key": ` + responsePublic,
		`"@signature-params": ` + params,
	}, "\n")
	signature := ed25519.Sign(privateKey, []byte(base))
	req.Header.Set("Signature-Input", "tn="+params)
	req.Header.Set("Signature", "tn=:"+base64.StdEncoding.EncodeToString(signature)+":")
	return nil
}

func canonicalAuthority(value *url.URL) (string, error) {
	if value == nil || value.User != nil || value.Host == "" {
		return "", errors.New("invalid TunNet control authority")
	}
	return strings.ToLower(value.Host), nil
}
