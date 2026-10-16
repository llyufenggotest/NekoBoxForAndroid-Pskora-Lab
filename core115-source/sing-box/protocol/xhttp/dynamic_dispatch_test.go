package xhttp

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sagernet/sing-box/log"
	VM "github.com/sagernet/sing-vmess/vless"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/stretchr/testify/require"
)

func TestDynamicProductionAES256Stream(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	key, _ := hex.DecodeString("5f06b85e1619b2b8b56f7b758a0a310a737fb43703acf936c6280204ae5f6506")
	// Independently derive the second EVP block; fixed literal checked below.
	expected := append([]byte{3, 13}, []byte("route.example")...)
	expected = append(expected, 1, 187, 'P')
	done := make(chan []byte, 1)
	go func() {
		c, e := listener.Accept()
		if e != nil {
			done <- nil
			return
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(time.Second))
		header := make([]byte, 163)
		if _, e = io.ReadFull(c, header); e != nil {
			done <- nil
			return
		}
		payload := make([]byte, len(expected))
		_, e = io.ReadFull(c, payload)
		if e != nil {
			done <- nil
			return
		}
		block, _ := aes.NewCipher(key)
		cipher.NewCTR(block, header[147:]).XORKeyStream(payload, payload)
		done <- payload
	}()
	addr := M.ParseSocksaddr(listener.Addr().String())
	h := &Outbound{isInit: true, bestCfg: RealConfig{Type: "xhttp", Server: addr.Addr.String(), Port: fmt.Sprint(addr.Port), Password: "synthetic:0102", Cipher: "AES-256-CTR"}, logger: log.NewNOPFactory().Logger(), dialer: controlFixtureDialer{dial: func(ctx context.Context, s string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", s)
	}}}
	conn, err := h.DialContext(context.Background(), "tcp", M.ParseSocksaddr("route.example:443"))
	require.NoError(t, err)
	defer conn.Close()
	_, err = conn.Write([]byte("P"))
	require.NoError(t, err)
	require.True(t, bytes.Equal(expected, <-done), "production dispatch ignored selected AES-256 cipher")
}

func TestDynamicBufferedPathsPreserveContractsAndRedaction(t *testing.T) {
	raw := &dynamicErrorConn{}
	cfg := dynamicVLESSFixture()
	h := &Outbound{isInit: true, bestCfg: cfg, dialer: controlFixtureDialer{dial: func(context.Context, string) (net.Conn, error) { return raw, nil }}}
	defer h.Close()
	c, err := h.DialContext(context.Background(), "tcp", M.ParseSocksaddr("example.com:443"))
	require.NoError(t, err)
	require.True(t, N.NeedHandshakeForWrite(c))
	_, ok := common.Cast[*VM.Conn](c)
	require.True(t, ok)
	buffer := buf.NewSize(1024)
	buffer.Write([]byte("P"))
	err = c.(N.ExtendedConn).WriteBuffer(buffer)
	require.Error(t, err)
	require.NotContains(t, fmt.Sprintf("%v %+v %#v", err, err, err), "synthetic-secret")
	pc, err := h.ListenPacket(context.Background(), M.ParseSocksaddr("example.com:443"))
	require.NoError(t, err)
	buffer = buf.NewSize(1024)
	buffer.Resize(512, 0)
	buffer.Write([]byte("P"))
	err = pc.(N.PacketConn).WritePacket(buffer, M.ParseSocksaddr("example.com:443"))
	require.Error(t, err)
	require.NotContains(t, fmt.Sprintf("%v %+v %#v", err, err, err), "synthetic-secret")
}

func TestDynamicPostDialErrorsRedacted(t *testing.T) {
	for _, vlessMode := range []bool{false, true} {
		t.Run(fmt.Sprint(vlessMode), func(t *testing.T) {
			cfg := RealConfig{Type: "ss", Server: "127.0.0.1", Port: "443", Cipher: "aes-128-ctr", Password: "synthetic", UDP: true}
			if vlessMode {
				cfg = dynamicVLESSFixture()
			}
			h := &Outbound{isInit: true, bestCfg: cfg, dialer: controlFixtureDialer{dial: func(context.Context, string) (net.Conn, error) { return &dynamicErrorConn{}, nil }}}
			defer h.Close()
			c, err := h.DialContext(context.Background(), "tcp", M.ParseSocksaddr("example.com:443"))
			require.NoError(t, err)
			defer c.Close()
			_, err = c.Write([]byte("P"))
			require.Error(t, err)
			require.False(t, strings.Contains(fmt.Sprintf("%v %+v %#v", err, err, err), "synthetic-secret"))
		})
	}
}

type dynamicErrorConn struct{ net.Conn }

func (*dynamicErrorConn) Write([]byte) (int, error) {
	return 0, errors.New("synthetic-secret-postdial")
}
func (*dynamicErrorConn) Read([]byte) (int, error)         { return 0, errors.New("synthetic-secret-postdial") }
func (*dynamicErrorConn) Close() error                     { return nil }
func (*dynamicErrorConn) SetDeadline(time.Time) error      { return nil }
func (*dynamicErrorConn) SetReadDeadline(time.Time) error  { return nil }
func (*dynamicErrorConn) SetWriteDeadline(time.Time) error { return nil }

func TestDynamicDataErrorsRedacted(t *testing.T) {
	var calls atomic.Int32
	h := &Outbound{isInit: true, bestCfg: RealConfig{Type: "xhttp", Server: "127.0.0.1", Port: "443", Password: "synthetic:0102"}, logger: log.NewNOPFactory().Logger(), dialer: controlFixtureDialer{dial: func(context.Context, string) (net.Conn, error) {
		calls.Add(1)
		return nil, errors.New("synthetic-secret-endpoint")
	}}}
	_, err := h.DialContext(context.Background(), "tcp", M.ParseSocksaddr("route.example:443"))
	require.Error(t, err)
	require.NotContains(t, err.Error(), "synthetic-secret-endpoint")
	require.Equal(t, int32(1), calls.Load())
}
