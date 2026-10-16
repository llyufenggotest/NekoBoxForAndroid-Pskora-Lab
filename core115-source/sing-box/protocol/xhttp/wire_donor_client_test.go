package xhttp

// Synthetic protocol vectors only; never official credentials or captures.
import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/hex"
	"io"
	"sync"
	"testing"
)

func TestCipherGoldenVectors(t *testing.T) {
	vectors := []struct{ method, key string }{
		{"aes-128-ctr", "5f06b85e1619b2b8b56f7b758a0a310a"},
		{"aes-192-ctr", "5f06b85e1619b2b8b56f7b758a0a310a737fb43703acf936"},
		{"aes-256-ctr", "5f06b85e1619b2b8b56f7b758a0a310a737fb43703acf936c6280204ae5f6506"},
	}
	for _, v := range vectors {
		t.Run(v.method, func(t *testing.T) {
			c, err := NewWireClient(WireConfig{Password: "synthetic:0102", Cipher: v.method})
			if err != nil {
				t.Fatal(err)
			}
			header, stream, err := c.uplink(c.snapshot(), nil)
			if err != nil {
				t.Fatal(err)
			}
			// Key vectors are independently supplied from the Python wire oracle.
			key, _ := hex.DecodeString(v.key)
			block, _ := aes.NewCipher(key)
			expected := make([]byte, 9)
			cipher.NewCTR(block, header[len(header)-16:]).XORKeyStream(expected, []byte("plaintext"))
			actual := make([]byte, 9)
			stream.XORKeyStream(actual, []byte("plaintext"))
			if !bytes.Equal(actual, expected) {
				t.Fatalf("cipher/key mismatch %x != %x", actual, expected)
			}
		})
	}
}
func TestMalformedConfig(t *testing.T) {
	for _, cfg := range []WireConfig{{Password: "no-colon"}, {Password: "p:zz"}, {Password: "p:00", TCPFake: "zz"}, {Password: "p:00", TCPFake: hex.EncodeToString(make([]byte, 129))}, {Password: "p:00", Cipher: "dummy"}, {Password: "p:00", Cipher: "chacha20"}} {
		if _, err := NewWireClient(cfg); err == nil {
			t.Fatalf("malformed/unsupported config accepted: cipher=%s", cfg.Cipher)
		}
	}
}
func TestCacheSnapshotsAndTruncation(t *testing.T) {
	c, _ := NewWireClient(WireConfig{Password: "synthetic:0102"})
	key := md5.Sum([]byte("synthetic"))
	old := mustWireStream(t, c, &wireMemoryConn{}, nil)
	for _, n := range []int{0, 1, 24, 255} {
		frame := wireServer(key[:], bytes.Repeat([]byte{0x10}, n), []byte("reply"))
		for i := 0; i < len(frame)-5; i++ {
			before := c.snapshot()
			stream, _, err := c.downlink(bytes.NewReader(frame[:i]), true)
			if err == nil || stream != nil {
				t.Fatalf("truncation %d accepted", i)
			}
			if !bytes.Equal(c.snapshot(), before) {
				t.Fatal("partial cache published")
			}
		}
		conn := mustWireStream(t, c, &wireMemoryConn{input: bytes.NewReader(frame)}, nil)
		p := make([]byte, 5)
		if _, err := io.ReadFull(conn, p); err != nil || string(p) != "reply" {
			t.Fatalf("read %q %v", p, err)
		}
		expected := append([]byte{byte(n)}, bytes.Repeat([]byte{0x10}, n)...)
		if !bytes.Equal(c.snapshot(), expected) {
			t.Fatal("cache not updated")
		}
	}
	if !bytes.Equal(old.auth, []byte{1, 2}) {
		t.Fatal("existing stream auth changed")
	}
	next := c.snapshot()
	next[0] ^= 1
	if bytes.Equal(next, c.snapshot()) {
		t.Fatal("snapshot alias")
	}
}
func TestRejectTokenAndIV(t *testing.T) {
	c, _ := NewWireClient(WireConfig{Password: "synthetic:0102"})
	key := md5.Sum([]byte("synthetic"))
	frame := wireServer(key[:], nil, nil)
	for _, offset := range []int{128, 145} {
		bad := append([]byte(nil), frame...)
		bad[offset] ^= 1
		conn := mustWireStream(t, c, &wireMemoryConn{input: bytes.NewReader(bad)}, nil)
		if _, err := conn.Read(make([]byte, 1)); err == nil {
			t.Fatal("bad header accepted")
		}
		if !bytes.Equal(c.snapshot(), []byte{1, 2}) {
			t.Fatal("bad frame updated cache")
		}
	}
}
func TestContinuousCTRConcurrentInitAndShortWrite(t *testing.T) {
	c, _ := NewWireClient(WireConfig{Password: "synthetic:0102", TCPFake: "abcd"})
	raw := &wireMemoryConn{}
	conn := mustWireStream(t, c, raw, []byte{1, 192, 0, 2, 1, 0, 53})
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := conn.Write([]byte("x")); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	frame := raw.Bytes()
	if len(frame) != 163+7+20 {
		t.Fatalf("multiple init len=%d", len(frame))
	}
	if !bytes.Equal(frame[:2], []byte{0xab, 0xcd}) {
		t.Fatal("fake prefix")
	}
	key := md5.Sum([]byte("synthetic"))
	block, _ := aes.NewCipher(key[:])
	plain := make([]byte, 27)
	cipher.NewCTR(block, frame[147:163]).XORKeyStream(plain, frame[163:])
	if !bytes.Equal(plain[:7], []byte{1, 192, 0, 2, 1, 0, 53}) || string(plain[7:]) != "xxxxxxxxxxxxxxxxxxxx" {
		t.Fatal("CTR discontinuity")
	}
	short := &wireMemoryConn{short: true}
	broken := mustWireStream(t, c, short, nil)
	if _, err := broken.Write(nil); err != io.ErrShortWrite {
		t.Fatalf("short header %v", err)
	}
	if !short.closed {
		t.Fatal("short write socket not closed")
	}
	if _, err := broken.Write([]byte("more")); err != io.ErrShortWrite {
		t.Fatal("advanced CTR reused")
	}
}
