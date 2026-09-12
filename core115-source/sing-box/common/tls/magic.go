package tls

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"io"
	"time"
)

// GenerateMagicSNI 实时计算闪连动态时间戳暗号
func GenerateMagicSNI() string {
	now := time.Now()
	tSec := now.Unix()
	tMs := now.UnixMilli()

	seedBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(seedBytes, uint64(tSec))
	key := sha256.Sum256(seedBytes)

	plainBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(plainBytes, uint64(tMs*10))

	nonce := make([]byte, 12)
	io.ReadFull(rand.Reader, nonce)

	block, _ := aes.NewCipher(key[:])
	aesgcm, _ := cipher.NewGCM(block)
	sealed := aesgcm.Seal(nil, nonce, plainBytes, nil)
	finalPayload := append(sealed, nonce...)

	return base64.StdEncoding.EncodeToString(finalPayload)
}
