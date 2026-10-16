package vless

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	M "github.com/sagernet/sing/common/metadata"
)

func TestBlackstoneProductionLoopbackTCPUDP(t *testing.T) {
	for _, packet := range []bool{false, true} {
		t.Run(fmt.Sprint("udp=", packet), func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			want := bsStandard + bsDigest + "03aabbcc50"
			response := []byte{0, 0, 2, 0xdd, 0xee, 'R'}
			if packet {
				want = "0000112233445566778899aabbccddeeff000201bb020b6578616d706c652e636f6d" + bsDigest + "03aabbcc000150"
				response = []byte{0, 0, 2, 0xdd, 0xee, 0, 1, 'R'}
			}
			expected, _ := hex.DecodeString(want)
			serverResult := make(chan error, 1)
			go func() {
				raw, e := listener.Accept()
				if e != nil {
					serverResult <- e
					return
				}
				defer raw.Close()
				_ = raw.SetDeadline(time.Now().Add(5 * time.Second))
				got := make([]byte, len(expected))
				_, e = io.ReadFull(raw, got)
				if e == nil && hex.EncodeToString(got) != want {
					e = fmt.Errorf("loopback wire mismatch")
				}
				if e == nil {
					_, e = raw.Write(response)
				}
				serverResult <- e
			}()
			empty := ""
			o := option.VLESSOutboundOptions{ServerOptions: option.ServerOptions{Server: "127.0.0.1", ServerPort: uint16(listener.Addr().(*net.TCPAddr).Port)}, UUID: bsID + "#blackstone", E: true, H: "03aabbcc", PacketEncoding: &empty}
			created, e := NewOutbound(context.Background(), nil, log.NewNOPFactory().Logger(), "synthetic", o)
			if e != nil {
				t.Fatal(e)
			}
			scope := adapter.NewScope(context.Background(), log.NewNOPFactory().Logger())
			if e = scope.Start("vless", created.(*Outbound), adapter.StartStateInitialize); e != nil {
				t.Fatal(e)
			}
			defer scope.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var out [1]byte
			if packet {
				conn, e := created.ListenPacket(ctx, M.ParseSocksaddr("example.com:443"))
				if e != nil {
					t.Fatal(e)
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				if _, e = conn.WriteTo([]byte("P"), M.ParseSocksaddr("example.com:443")); e != nil {
					t.Fatal(e)
				}
				if _, _, e = conn.ReadFrom(out[:]); e != nil {
					t.Fatal(e)
				}
			} else {
				conn, e := created.DialContext(ctx, "tcp", M.ParseSocksaddr("example.com:443"))
				if e != nil {
					t.Fatal(e)
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				if _, e = conn.Write([]byte("P")); e != nil {
					t.Fatal(e)
				}
				if _, e = io.ReadFull(conn, out[:]); e != nil {
					t.Fatal(e)
				}
			}
			if out[0] != 'R' {
				t.Fatal("private response marker leaked")
			}
			if e = <-serverResult; e != nil {
				t.Fatal(e)
			}
			// Verify an actual production stream's updated H is used by the next lazy stream.
			r := &bsRecordingConn{}
			next, e := created.(*Outbound).client.DialEarlyConn(r, M.ParseSocksaddr("example.com:443"))
			if e != nil {
				t.Fatal(e)
			}
			if _, e = next.Write([]byte("P")); e != nil {
				t.Fatal(e)
			}
			if hex.EncodeToString(r.Bytes()) != bsStandard+bsDigest+"02ddee50" {
				t.Fatal("production cache not shared")
			}
		})
	}
}
