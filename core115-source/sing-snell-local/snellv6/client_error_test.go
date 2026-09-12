package snellv6

import (
	"context"
	"errors"
	"net"
	"strconv"
	"testing"
	"time"

	snell "github.com/sagernet/sing-snell"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

func TestClientReportsServerError(t *testing.T) {
	const message = "IPv6 target is disabled by server configuration"
	psk := []byte("server-error-test-psk")
	for _, mode := range []Mode{ModeDefault, ModeUnshaped, ModeUnsafeRaw} {
		for _, reuseEnabled := range []bool{false, true} {
			t.Run(mode.String()+"/reuse="+strconv.FormatBool(reuseEnabled), func(t *testing.T) {
				clientSide, serverSide := net.Pipe()
				defer clientSide.Close()
				defer serverSide.Close()

				var profile *Profile
				if mode == ModeDefault {
					profile = NewProfile(psk)
				}
				serverDone := make(chan error, 1)
				go func() {
					_, request, err := readFirstRecord(serverSide, mode, psk, profile, N.ReadWaitOptions{})
					if err != nil {
						serverDone <- err
						return
					}
					request.Release()
					reply := append([]byte{snell.ReplyError, 0x01, byte(len(message))}, message...)
					_, err = writeFirstRecord(serverSide, mode, psk, profile, reply)
					serverDone <- err
				}()

				client, err := NewClient(ClientOptions{
					PSK:    psk,
					Mode:   mode,
					Reuse:  reuseEnabled,
					Dialer: singleConnDialer{clientSide},
					Server: M.ParseSocksaddr("127.0.0.1:1080"),
				})
				if err != nil {
					t.Fatal(err)
				}
				defer client.Close()

				conn, err := client.DialContext(context.Background(), M.ParseSocksaddr("[2001:db8::1]:443"))
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				if err = conn.SetDeadline(time.Now().Add(time.Second)); err != nil {
					t.Fatal(err)
				}

				_, err = conn.Read(make([]byte, 1))
				wantError := "snell: server error 1: " + message
				if err == nil || err.Error() != wantError {
					t.Fatalf("Read error = %v, want %q", err, wantError)
				}
				if err = <-serverDone; err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

type singleConnDialer struct {
	conn net.Conn
}

func (d singleConnDialer) DialContext(context.Context, string, M.Socksaddr) (net.Conn, error) {
	return d.conn, nil
}

func (singleConnDialer) ListenPacket(context.Context, M.Socksaddr) (net.PacketConn, error) {
	return nil, errors.New("unused")
}
