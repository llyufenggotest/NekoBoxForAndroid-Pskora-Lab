package xhttp

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

type wireMemoryConn struct {
	bytes.Buffer
	input         *bytes.Reader
	short, closed bool
	deadlines     []time.Time
}

func (c *wireMemoryConn) Read(p []byte) (int, error) {
	if c.input == nil {
		return 0, io.EOF
	}
	return c.input.Read(p)
}
func (c *wireMemoryConn) Write(p []byte) (int, error) {
	if c.short {
		return len(p) / 2, nil
	}
	return c.Buffer.Write(p)
}
func (c *wireMemoryConn) Close() error                  { c.closed = true; return nil }
func (*wireMemoryConn) LocalAddr() net.Addr             { return &net.TCPAddr{} }
func (*wireMemoryConn) RemoteAddr() net.Addr            { return &net.TCPAddr{} }
func (*wireMemoryConn) SetDeadline(time.Time) error     { return nil }
func (*wireMemoryConn) SetReadDeadline(time.Time) error { return nil }
func (c *wireMemoryConn) SetWriteDeadline(d time.Time) error {
	c.deadlines = append(c.deadlines, d)
	return nil
}

// Independent fixture: no production key derivation/header encoder is called.
func wireServer(key, auth, plain []byte) []byte {
	token := md5.Sum([]byte("syntheticdo not hack this protocol please"))
	frame := append(make([]byte, 128), token[:]...)
	frame = append(frame, byte(len(auth)))
	frame = append(frame, auth...)
	frame = append(frame, 16)
	iv := bytes.Repeat([]byte{0x61}, 16)
	frame = append(frame, iv...)
	b, _ := aes.NewCipher(key)
	ct := make([]byte, len(plain))
	cipher.NewCTR(b, iv).XORKeyStream(ct, plain)
	return append(frame, ct...)
}
func TestLegacyConnAsymmetricDownlink(t *testing.T) {
	key := md5.Sum([]byte("synthetic"))
	block, _ := aes.NewCipher(key[:])
	// Marker-looking H bytes must not be scanned as an IV marker.
	frame := wireServer(key[:], bytes.Repeat([]byte{0x10}, 24), []byte("answer"))
	raw := &wireMemoryConn{input: bytes.NewReader(frame)}
	c := NewXHttpConn(raw, cipher.NewCTR(block, make([]byte, 16)), block, 146)
	p := make([]byte, 6)
	_, err := io.ReadFull(c, p)
	if err != nil || string(p) != "answer" {
		t.Fatalf("asymmetric H framing lost: correct_plaintext=%v error=%v", string(p) == "answer", err)
	}
}
func TestLegacyConnShortWriteTerminal(t *testing.T) {
	key := md5.Sum([]byte("synthetic"))
	block, _ := aes.NewCipher(key[:])
	raw := &wireMemoryConn{short: true}
	c := NewXHttpConn(raw, cipher.NewCTR(block, make([]byte, 16)), block, 146)
	if _, err := c.Write([]byte("payload")); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write not rejected: %v", err)
	}
	if !raw.closed {
		t.Fatal("short write must close advanced stream")
	}
	if _, err := c.Write([]byte("again")); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal("advanced cipher reused")
	}
}
