package xhttp

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"errors"
	M "github.com/sagernet/sing/common/metadata"
	"io"
	"net"
	"testing"
	"time"
)

// A real local peer checks exact decrypted bytes using a literal key and frame
// layout; it never calls production decode/encode helpers to build expectations.
func TestWireTCPAndUOTRealLoopback(t *testing.T) {
	for _, version := range []int{-1, 0, 1, 2} {
		name := "TCP"
		if version >= 0 {
			name = "UOT" + string(rune('0'+version))
		}
		t.Run(name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			address := append([]byte{3, 13}, []byte("route.example")...)
			address = append(address, 1, 187)
			expected := append(append([]byte(nil), address...), []byte("hello")...)
			response := []byte("answer")
			target := M.ParseSocksaddr("route.example:443")
			if version >= 0 {
				target, err = WireUOTDestination(version)
				if err != nil {
					t.Fatal(err)
				}
				magic := "sp.v2.udp-over-tcp.arpa"
				if version == 1 {
					magic = "sp.udp-over-tcp.arpa"
				}
				expected = append([]byte{3, byte(len(magic))}, []byte(magic)...)
				expected = append(expected, 0, 0)
				native := append([]byte(nil), address...)
				native[0] = 2
				if version != 1 {
					expected = append(expected, 0)
					expected = append(expected, native...)
				}
				expected = append(expected, native...)
				expected = append(expected, 0, 5)
				expected = append(expected, []byte("hello")...)
				response = []byte{0, 192, 0, 2, 1, 0, 53, 0, 6, 'a', 'n', 's', 'w', 'e', 'r'}
			}
			key, _ := hex.DecodeString("5f06b85e1619b2b8b56f7b758a0a310a")
			done := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					done <- err
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
				header := make([]byte, 163)
				if _, err = io.ReadFull(conn, header); err != nil {
					done <- err
					return
				}
				if !bytes.Equal(header[144:147], []byte{1, 2, 16}) {
					done <- errors.New("uplink changed auth framing")
					return
				}
				ct := make([]byte, len(expected))
				if _, err = io.ReadFull(conn, ct); err != nil {
					done <- err
					return
				}
				block, _ := aes.NewCipher(key)
				cipher.NewCTR(block, header[147:163]).XORKeyStream(ct, ct)
				if !bytes.Equal(ct, expected) {
					done <- errors.New("loopback peer rejected plaintext")
					return
				}
				_, err = conn.Write(wireServer(key, []byte{9, 8}, response))
				done <- err
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			raw, err := (&net.Dialer{}).DialContext(ctx, "tcp", listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			client, _ := NewWireClient(WireConfig{Password: "synthetic:0102"})
			stream, err := client.OpenStream(ctx, raw, target)
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			_ = stream.SetDeadline(time.Now().Add(3 * time.Second))
			p := make([]byte, 6)
			if version < 0 {
				if _, err = stream.Write([]byte("hello")); err != nil {
					t.Fatal(err)
				}
				if _, err = io.ReadFull(stream, p); err != nil {
					t.Fatal(err)
				}
			} else {
				pc, err := NewWireUOT(stream, version, M.ParseSocksaddr("route.example:443"))
				if err != nil {
					t.Fatal(err)
				}
				if _, err = pc.WriteTo([]byte("hello"), M.ParseSocksaddr("route.example:443")); err != nil {
					t.Fatal(err)
				}
				if n, source, err := pc.ReadFrom(p); err != nil || n != 6 || source.String() != "192.0.2.1:53" {
					t.Fatal("loopback UOT response", n, source, err)
				}
			}
			if string(p) != "answer" {
				t.Fatal("loopback response mismatch")
			}
			if err = <-done; err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(client.snapshot(), []byte{2, 9, 8}) {
				t.Fatal("loopback did not publish Hlen+body")
			}
		})
	}
}
