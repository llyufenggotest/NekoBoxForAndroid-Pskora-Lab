package xhttp

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	M "github.com/sagernet/sing/common/metadata"
	"github.com/stretchr/testify/require"
)

func TestDynamicProductionSSTCPAndUOTLoopback(t *testing.T) {
	for _, version := range []int{-1, 0, 1, 2} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			defer ln.Close()
			endpoint := M.SocksaddrFromNet(ln.Addr())
			done := make(chan error, 1)
			go func() {
				c, e := ln.Accept()
				if e != nil {
					done <- e
					return
				}
				defer c.Close()
				c.SetDeadline(time.Now().Add(2 * time.Second))
				header := make([]byte, 35)
				if _, e = io.ReadFull(c, header); e != nil {
					done <- e
					return
				}
				token, _ := hex.DecodeString("087ec9feca6a625e4bb45d643d9538bb")
				if !bytes.Equal(header[:2], []byte{1, 0}) || !bytes.Equal(header[2:18], token) || header[18] != 16 {
					done <- fmt.Errorf("SS private header mismatch")
					return
				}
				key := dynamicLiteralKey()
				block, _ := aes.NewCipher(key)
				reader := &cipher.StreamReader{S: cipher.NewCTR(block, header[19:]), R: c}
				dst, e := M.SocksaddrSerializer.ReadAddrPort(reader)
				if e != nil {
					done <- e
					return
				}
				target := "route.example"
				if version >= 0 {
					target = "sp.v2.udp-over-tcp.arpa"
					if version == 1 {
						target = "sp.udp-over-tcp.arpa"
					}
				}
				if dst.Fqdn != target {
					done <- fmt.Errorf("SS destination mismatch")
					return
				}
				want := []byte{'P'}
				response := []byte{'R'}
				if version >= 0 {
					native := append([]byte{2, 13}, []byte("route.example")...)
					native = append(native, 1, 187)
					want = append(append([]byte{}, native...), 0, 1, 'P')
					if version != 1 {
						request := append([]byte{0, 3, 13}, []byte("route.example")...)
						request = append(request, 1, 187)
						want = append(request, want...)
					}
					response = []byte{0, 192, 0, 2, 1, 0, 53, 0, 1, 'R'}
				}
				got := make([]byte, len(want))
				_, e = io.ReadFull(reader, got)
				if e == nil && !bytes.Equal(got, want) {
					e = fmt.Errorf("SS payload/UOT mismatch")
				}
				if e == nil {
					iv := bytes.Repeat([]byte{7}, 16)
					encrypted := make([]byte, len(response))
					cipher.NewCTR(block, iv).XORKeyStream(encrypted, response)
					frame := append([]byte{1, 0}, make([]byte, 17)...)
					frame = append(frame, iv...)
					frame = append(frame, encrypted...)
					_, e = c.Write(frame)
				}
				done <- e
			}()
			cfg := RealConfig{Type: "ss", Server: "127.0.0.1", Port: fmt.Sprint(endpoint.Port), Password: "synthetic", Cipher: "aes-128-ctr", UDP: true, UDPOverTCP: version >= 0}
			if version >= 0 {
				cfg.UDPOverTCPVersion = version
			}
			h := &Outbound{isInit: true, bestCfg: cfg, dialer: &dynamicSystemDialer{dial: func(ctx context.Context, n string, a M.Socksaddr) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, n, a.String())
			}}}
			defer h.Close()
			destination := M.ParseSocksaddr("route.example:443")
			if version < 0 {
				c, e := h.DialContext(context.Background(), "tcp", destination)
				require.NoError(t, e)
				defer c.Close()
				c.SetDeadline(time.Now().Add(2 * time.Second))
				_, e = c.Write([]byte("P"))
				require.NoError(t, e)
				p := make([]byte, 1)
				_, e = io.ReadFull(c, p)
				require.NoError(t, e)
				require.Equal(t, "R", string(p))
			} else {
				pc, e := h.ListenPacket(context.Background(), destination)
				require.NoError(t, e)
				defer pc.Close()
				pc.SetDeadline(time.Now().Add(2 * time.Second))
				_, e = pc.WriteTo([]byte("P"), destination)
				require.NoError(t, e)
				p := make([]byte, 1)
				n, src, e := pc.ReadFrom(p)
				require.NoError(t, e)
				require.Equal(t, 1, n)
				require.Equal(t, "R", string(p))
				require.Equal(t, "192.0.2.1:53", src.String())
			}
			require.NoError(t, <-done)
		})
	}
}

func TestDynamicProductionSSNativeUDPLoopback(t *testing.T) {
	peer, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	defer peer.Close()
	endpoint := M.SocksaddrFromNet(peer.LocalAddr())
	done := make(chan error, 1)
	go func() {
		peer.SetDeadline(time.Now().Add(2 * time.Second))
		frame := make([]byte, 2048)
		n, src, e := peer.ReadFrom(frame)
		if e != nil {
			done <- e
			return
		}
		key := dynamicLiteralKey()
		block, _ := aes.NewCipher(key)
		plain := make([]byte, n-16)
		cipher.NewCTR(block, frame[:16]).XORKeyStream(plain, frame[16:n])
		want := append([]byte{3, 13}, []byte("route.example")...)
		want = append(want, 0, 53, 'P')
		if !bytes.Equal(want, plain) {
			done <- fmt.Errorf("SS native UDP mismatch")
			return
		}
		iv := bytes.Repeat([]byte{8}, 16)
		payload := []byte{1, 192, 0, 2, 1, 0, 53, 'R'}
		cipher.NewCTR(block, iv).XORKeyStream(payload, payload)
		_, e = peer.WriteTo(append(iv, payload...), src)
		done <- e
	}()
	h := &Outbound{isInit: true, bestCfg: RealConfig{Type: "ss", Server: "127.0.0.1", Port: fmt.Sprint(endpoint.Port), Password: "synthetic", Cipher: "aes-128-ctr", UDP: true}, dialer: &dynamicSystemDialer{dial: func(ctx context.Context, n string, a M.Socksaddr) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, n, a.String())
	}}}
	defer h.Close()
	pc, err := h.ListenPacket(context.Background(), M.ParseSocksaddr("route.example:53"))
	require.NoError(t, err)
	defer pc.Close()
	pc.SetDeadline(time.Now().Add(2 * time.Second))
	_, err = pc.WriteTo([]byte("P"), M.ParseSocksaddr("route.example:53"))
	require.NoError(t, err)
	p := make([]byte, 2048)
	n, src, err := pc.ReadFrom(p)
	require.NoError(t, err)
	require.Equal(t, "R", string(p[:n]))
	require.Equal(t, "192.0.2.1:53", src.String())
	require.NoError(t, <-done)
}
