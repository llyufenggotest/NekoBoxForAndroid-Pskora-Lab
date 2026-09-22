package anytls

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
	"strings"
	"testing"
)

const shanlianFixture = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

func TestShanlianPasswordSuffixUsesDirect32ByteAuthentication(t *testing.T) {
	client, err := NewClient(ClientOptions{Password: shanlianFixture + "#sl", DialOut: func(ctx context.Context) (net.Conn, error) { return nil, nil }})
	if err != nil {
		t.Fatal(err)
	}
	want, _ := hex.DecodeString(shanlianFixture)
	if !bytes.Equal(client.password[:], want) {
		t.Fatalf("got %x, want direct bytes %x", client.password, want)
	}
}

func TestShanlianPasswordSuffixIsCaseInsensitive(t *testing.T) {
	client, err := NewClient(ClientOptions{Password: shanlianFixture + "#SL", DialOut: func(ctx context.Context) (net.Conn, error) { return nil, nil }})
	if err != nil {
		t.Fatal(err)
	}
	want, _ := hex.DecodeString(shanlianFixture)
	if !bytes.Equal(client.password[:], want) {
		t.Fatalf("got %x, want direct bytes %x", client.password, want)
	}
}

func TestShanlianPasswordRejectsNon64HexIdentity(t *testing.T) {
	for _, password := range []string{"short#sl", strings.Repeat("z", 64) + "#sl", shanlianFixture + "#sl-extra"} {
		_, err := NewClient(ClientOptions{Password: password, DialOut: func(ctx context.Context) (net.Conn, error) { return nil, nil }})
		if strings.HasSuffix(strings.ToLower(password), "#sl") && err == nil {
			t.Fatalf("accepted invalid Shanlian password %q", password)
		}
		if strings.HasSuffix(password, "#sl-extra") && err != nil {
			t.Fatalf("ordinary password should retain standard behavior: %v", err)
		}
	}
}

func captureHandshakePrefix(t *testing.T, password string) ([]byte, int) {
	t.Helper()
	server, peer := net.Pipe()
	client, err := NewClient(ClientOptions{Password: password, DialOut: func(ctx context.Context) (net.Conn, error) { return server, nil }})
	if err != nil {
		peer.Close()
		t.Fatal(err)
	}
	defer client.Close()
	defer peer.Close()
	result := make(chan struct {
		data []byte
		err  error
	}, 1)
	go func() {
		var captured bytes.Buffer
		_, err := io.Copy(&captured, peer)
		result <- struct {
			data []byte
			err  error
		}{captured.Bytes(), err}
	}()
	created := make(chan error, 1)
	go func() {
		_, err := client.createSession(context.Background())
		created <- err
	}()
	if err := <-created; err != nil {
		t.Fatal(err)
	}
	server.Close()
	got := <-result
	if got.err != nil {
		t.Fatal(got.err)
	}
	if len(got.data) < passwordLen {
		t.Fatalf("short handshake: %d bytes", len(got.data))
	}
	return got.data[:passwordLen], len(got.data)
}

func TestShanlianHandshakeWritesDecodedPasswordAsFirst32Bytes(t *testing.T) {
	got, n := captureHandshakePrefix(t, shanlianFixture+"#sl")
	want, _ := hex.DecodeString(shanlianFixture)
	if n < passwordLen || !bytes.Equal(got, want) {
		t.Fatalf("handshake prefix got %x (%d bytes), want %x", got, n, want)
	}
}

func TestOrdinaryHandshakeWritesSHA256AsFirst32Bytes(t *testing.T) {
	password := "ordinary-password"
	got, n := captureHandshakePrefix(t, password)
	want := sha256.Sum256([]byte(password))
	if n < passwordLen || !bytes.Equal(got, want[:]) {
		t.Fatalf("handshake prefix got %x (%d bytes), want %x", got, n, want)
	}
}

func TestInvalidShanlianPasswordFailsBeforeDial(t *testing.T) {
	dialed := false
	_, err := NewClient(ClientOptions{Password: "short#sl", DialOut: func(ctx context.Context) (net.Conn, error) {
		dialed = true
		return nil, nil
	}})
	if err == nil || dialed {
		t.Fatalf("err=%v dialed=%v, expected validation before dial", err, dialed)
	}
}

func TestOrdinaryAnyTLSPasswordStillUsesSHA256(t *testing.T) {
	password := "ordinary-password"
	client, err := NewClient(ClientOptions{Password: password, DialOut: func(ctx context.Context) (net.Conn, error) { return nil, nil }})
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256([]byte(password))
	if client.password != want {
		t.Fatalf("got %x, want SHA-256 %x", client.password, want)
	}
}
