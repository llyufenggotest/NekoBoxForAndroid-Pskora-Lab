package tunnetcontrol

import (
	"crypto/ecdh"
	"errors"
	"fmt"

	"github.com/cloudflare/circl/hpke"
)

var responseInfo = []byte("TunNet/client-response/v1")

var responseSuite = hpke.NewSuite(hpke.KEM_X25519_HKDF_SHA256, hpke.KDF_HKDF_SHA256, hpke.AEAD_AES256GCM)

func responseContextInfo(operation, clientID string, nonce []byte) ([]byte, error) {
	if !operationPattern.MatchString(operation) || !clientIDPattern.MatchString(clientID) || len(nonce) != 16 {
		return nil, errors.New("invalid TunNet response context")
	}
	info := make([]byte, 0, len(responseInfo)+len(operation)+len(clientID)+len(nonce)+3)
	info = append(info, responseInfo...)
	info = append(info, 0)
	info = append(info, operation...)
	info = append(info, 0)
	info = append(info, clientID...)
	info = append(info, 0)
	info = append(info, nonce...)
	return info, nil
}

type HPKEResponseOpener struct{}

func (HPKEResponseOpener) Open(privateKey *ecdh.PrivateKey, sealed []byte, operation, clientID string, nonce []byte) ([]byte, error) {
	if privateKey == nil || len(sealed) < 32+16 {
		return nil, errors.New("invalid TunNet sealed response")
	}
	info, err := responseContextInfo(operation, clientID, nonce)
	if err != nil {
		return nil, err
	}
	enc := sealed[:32]
	ciphertext := sealed[32:]

	scheme := hpke.KEM_X25519_HKDF_SHA256.Scheme()
	priv, err := scheme.UnmarshalBinaryPrivateKey(privateKey.Bytes())
	if err != nil {
		return nil, fmt.Errorf("parse TunNet HPKE recipient key: %w", err)
	}
	receiver, err := responseSuite.NewReceiver(priv, info)
	if err != nil {
		return nil, fmt.Errorf("create TunNet HPKE receiver: %w", err)
	}
	opener, err := receiver.Setup(enc)
	if err != nil {
		return nil, fmt.Errorf("setup TunNet HPKE opener: %w", err)
	}
	plaintext, err := opener.Open(ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("open TunNet HPKE response: %w", err)
	}
	return plaintext, nil
}
