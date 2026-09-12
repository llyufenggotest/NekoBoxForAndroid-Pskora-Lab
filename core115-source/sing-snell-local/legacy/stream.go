package legacy

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

const maxPayload = 0x3fff

var errZeroChunk = errors.New("snell: zero chunk")

type aeadConstructor struct {
	keySize int
	new     func([]byte) (cipher.AEAD, error)
}

func cipherForVersion(version int) aeadConstructor {
	if version == 1 {
		return aeadConstructor{32, chacha20poly1305.New}
	}
	return aeadConstructor{16, func(key []byte) (cipher.AEAD, error) {
		block, err := aes.NewCipher(key)
		if err != nil {
			return nil, err
		}
		return cipher.NewGCM(block)
	}}
}

type streamConn struct {
	net.Conn
	psk    []byte
	ctor   aeadConstructor
	reader *streamReader
	writer *streamWriter
	rOnce  sync.Once
	wOnce  sync.Once
	rErr   error
	wErr   error
}

func newStreamConn(conn net.Conn, psk []byte, version int) net.Conn {
	return &streamConn{Conn: conn, psk: psk, ctor: cipherForVersion(version)}
}

func (c *streamConn) initReader() {
	salt := make([]byte, 16)
	if _, c.rErr = io.ReadFull(c.Conn, salt); c.rErr != nil {
		return
	}
	key := argon2.IDKey(c.psk, salt, 3, 8, 1, 32)[:c.ctor.keySize]
	var aead cipher.AEAD
	aead, c.rErr = c.ctor.new(key)
	if c.rErr == nil {
		c.reader = newStreamReader(c.Conn, aead)
	}
}

func (c *streamConn) initWriter() {
	salt := make([]byte, 16)
	if _, c.wErr = rand.Read(salt); c.wErr != nil {
		return
	}
	if _, c.wErr = c.Conn.Write(salt); c.wErr != nil {
		return
	}
	key := argon2.IDKey(c.psk, salt, 3, 8, 1, 32)[:c.ctor.keySize]
	var aead cipher.AEAD
	aead, c.wErr = c.ctor.new(key)
	if c.wErr == nil {
		c.writer = newStreamWriter(c.Conn, aead)
	}
}

func (c *streamConn) Read(p []byte) (int, error) {
	c.rOnce.Do(c.initReader)
	if c.rErr != nil {
		return 0, c.rErr
	}
	return c.reader.Read(p)
}

func (c *streamConn) Write(p []byte) (int, error) {
	c.wOnce.Do(c.initWriter)
	if c.wErr != nil {
		return 0, c.wErr
	}
	return c.writer.Write(p)
}

type streamReader struct {
	source io.Reader
	aead   cipher.AEAD
	nonce  []byte
	cache  []byte
}

func newStreamReader(source io.Reader, aead cipher.AEAD) *streamReader {
	return &streamReader{source: source, aead: aead, nonce: make([]byte, aead.NonceSize())}
}

func (r *streamReader) Read(p []byte) (int, error) {
	if len(r.cache) > 0 {
		n := copy(p, r.cache)
		r.cache = r.cache[n:]
		return n, nil
	}
	header := make([]byte, 2+r.aead.Overhead())
	if _, err := io.ReadFull(r.source, header); err != nil {
		return 0, err
	}
	plainHeader, err := r.aead.Open(header[:0], r.nonce, header, nil)
	if err != nil {
		return 0, err
	}
	increaseNonce(r.nonce)
	length := int(binary.BigEndian.Uint16(plainHeader))
	if length == 0 {
		return 0, errZeroChunk
	}
	body := make([]byte, length+r.aead.Overhead())
	if _, err = io.ReadFull(r.source, body); err != nil {
		return 0, err
	}
	body, err = r.aead.Open(body[:0], r.nonce, body, nil)
	if err != nil {
		return 0, err
	}
	increaseNonce(r.nonce)
	n := copy(p, body)
	r.cache = body[n:]
	return n, nil
}

type streamWriter struct {
	destination io.Writer
	aead        cipher.AEAD
	nonce       []byte
	access      sync.Mutex
}

func newStreamWriter(destination io.Writer, aead cipher.AEAD) *streamWriter {
	return &streamWriter{destination: destination, aead: aead, nonce: make([]byte, aead.NonceSize())}
}

func (w *streamWriter) Write(p []byte) (int, error) {
	w.access.Lock()
	defer w.access.Unlock()
	if len(p) == 0 {
		return 0, w.writeChunk(nil)
	}
	written := 0
	for len(p) > 0 {
		chunk := p
		if len(chunk) > maxPayload {
			chunk = chunk[:maxPayload]
		}
		if err := w.writeChunk(chunk); err != nil {
			return written, err
		}
		written += len(chunk)
		p = p[len(chunk):]
	}
	return written, nil
}

func (w *streamWriter) writeChunk(p []byte) error {
	header := make([]byte, 2)
	binary.BigEndian.PutUint16(header, uint16(len(p)))
	header = w.aead.Seal(header[:0], w.nonce, header, nil)
	increaseNonce(w.nonce)
	body := w.aead.Seal(nil, w.nonce, p, nil)
	increaseNonce(w.nonce)
	_, err := w.destination.Write(append(header, body...))
	return err
}

func increaseNonce(nonce []byte) {
	for index := range nonce {
		nonce[index]++
		if nonce[index] != 0 {
			return
		}
	}
}
