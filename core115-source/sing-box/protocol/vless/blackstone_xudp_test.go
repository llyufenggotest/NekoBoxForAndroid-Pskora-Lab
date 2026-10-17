package vless

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/hex"
	"io"
	"net"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	V "github.com/sagernet/sing-vmess/vless"
	M "github.com/sagernet/sing/common/metadata"
)

// Real constructor, real TLS base and production hook discovery. Initializing
// a private XUDP connection must not emit an empty pre-Vision request: donor
// snapshots generation H only at the first actual packet write.
func TestBlackstoneXUDPProductionLazyKnownBase(t *testing.T) {
	certServer := httptest.NewTLSServer(nil)
	cert := certServer.TLS.Certificates[0]
	certServer.Close()
	for _, entry := range []string{"listen", "dial", "hook-listen", "hook-dial"} {
		t.Run(entry, func(t *testing.T) {
			ln, e := net.Listen("tcp", "127.0.0.1:0")
			if e != nil {
				t.Fatal(e)
			}
			defer ln.Close()
			got := make(chan []byte, 1)
			go func() {
				r, e := ln.Accept()
				if e != nil {
					got <- nil
					return
				}
				defer r.Close()
				r.SetDeadline(time.Now().Add(3 * time.Second))
				tc := tls.Server(r, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13})
				q, e := V.ReadRequest(tc)
				if e != nil || q.Command != 3 || q.Flow != V.FlowVision {
					got <- nil
					return
				}
				private := make([]byte, 20)
				if _, e = io.ReadFull(tc, private); e != nil {
					got <- nil
					return
				}
				first := true
				p, e := bsOracleContent(tc, true, &first)
				if e != nil {
					got <- nil
					return
				}
				got <- append(private, p...)
			}()
			encoding := "xudp"
			o, e := NewOutbound(context.Background(), nil, log.NewNOPFactory().Logger(), "synthetic", option.VLESSOutboundOptions{ServerOptions: option.ServerOptions{Server: "127.0.0.1", ServerPort: uint16(ln.Addr().(*net.TCPAddr).Port)}, UUID: bsID + "#blackstone", E: true, H: "03aabbcc", Flow: V.FlowVision, PacketEncoding: &encoding, OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{TLS: &option.OutboundTLSOptions{Enabled: true, Insecure: true}}})
			if e != nil {
				t.Fatal(e)
			}
			h := o.(*Outbound)
			if entry == "hook-listen" || entry == "hook-dial" {
				r, e := net.Dial("tcp", ln.Addr().String())
				if e != nil {
					t.Fatal(e)
				}
				tc := tls.Client(r, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13})
				h.transport = &bsHookTransport{base: tc, outer: &bsOuter{tc}}
			}
			dst := M.ParseSocksaddr("example.com:443")
			var pc net.PacketConn
			if entry == "dial" || entry == "hook-dial" {
				c, err := o.DialContext(context.Background(), "udp", dst)
				e = err
				if e == nil {
					pc = c.(net.PacketConn)
				}
			} else {
				pc, e = o.ListenPacket(context.Background(), dst)
			}
			if e != nil {
				t.Fatal(e)
			}
			defer pc.Close()
			if early, ok := pc.(interface{ NeedHandshake() bool }); !ok || !early.NeedHandshake() {
				t.Fatal("private XUDP must stay lazy")
			}
			if _, e = pc.WriteTo([]byte("P"), dst); e != nil {
				t.Fatal(e)
			}
			wire := <-got
			want, _ := hex.DecodeString(bsDigest + "03aabbcc0014000001010201bb020b6578616d706c652e636f6d000150")
			if !bytes.Equal(wire, want) {
				t.Fatalf("private Command3/Vision/XUDP wire mismatch: got=%x want=%x", wire, want)
			}
		})
	}
}
