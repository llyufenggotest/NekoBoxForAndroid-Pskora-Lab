package vless

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/hex"
	"fmt"
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

func TestBlackstonePacketAddrDonorProduction(t *testing.T) {
	cs := httptest.NewTLSServer(nil)
	cert := cs.TLS.Certificates[0]
	cs.Close()
	for _, vision := range []bool{false, true} {
		for _, dial := range []bool{false, true} {
			t.Run(fmt.Sprintf("vision=%t/dial=%t", vision, dial), func(t *testing.T) {
				ln, e := net.Listen("tcp", "127.0.0.1:0")
				if e != nil {
					t.Fatal(e)
				}
				defer ln.Close()
				done := make(chan error, 1)
				go func() {
					raw, e := ln.Accept()
					if e != nil {
						done <- e
						return
					}
					defer raw.Close()
					raw.SetDeadline(time.Now().Add(3 * time.Second))
					tc := tls.Server(raw, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13})
					q, e := V.ReadRequest(tc)
					if e != nil {
						done <- e
						return
					}
					if q.Command != 2 || q.Destination.String() != "sp.packet-addr.v2fly.arpa:443" {
						done <- fmt.Errorf("packetaddr donor command/address mismatch: %+v", q)
						return
					}
					p := make([]byte, 20)
					if _, e = io.ReadFull(tc, p); e != nil {
						done <- e
						return
					}
					if hex.EncodeToString(p) != bsDigest+"03aabbcc" {
						done <- fmt.Errorf("E/H mismatch")
						return
					}
					first := true
					var content []byte
					if vision {
						a, e := bsOracleContent(tc, true, &first)
						if e != nil {
							done <- e
							return
						}
						b, e := bsOracleContent(tc, true, &first)
						if e != nil {
							done <- e
							return
						}
						content = append(a, b...)
					} else {
						var n uint16
						if e = binary.Read(tc, binary.BigEndian, &n); e != nil {
							done <- e
							return
						}
						content = make([]byte, 2+int(n))
						binary.BigEndian.PutUint16(content, n)
						_, e = io.ReadFull(tc, content[2:])
						if e != nil {
							done <- e
							return
						}
					}
					if hex.EncodeToString(content) != "000801c00002014b6650" {
						done <- fmt.Errorf("packetaddr frame mismatch %x", content)
						return
					}
					done <- nil
				}()
				enc := "packetaddr"
				flow := ""
				if vision {
					flow = V.FlowVision
				}
				o, e := NewOutbound(context.Background(), nil, log.NewNOPFactory().Logger(), "synthetic", option.VLESSOutboundOptions{ServerOptions: option.ServerOptions{Server: "127.0.0.1", ServerPort: uint16(ln.Addr().(*net.TCPAddr).Port)}, UUID: bsID + "#blackstone", E: true, H: "03aabbcc", Flow: flow, PacketEncoding: &enc, OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{TLS: &option.OutboundTLSOptions{Enabled: true, Insecure: true}}})
				if e != nil {
					t.Fatal(e)
				}
				dst := M.ParseSocksaddr("192.0.2.1:19302")
				var pc net.PacketConn
				if dial {
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
				pc.SetDeadline(time.Now().Add(3 * time.Second))
				if _, e = pc.WriteTo([]byte("P"), dst.UDPAddr()); e != nil {
					t.Fatal(e)
				}
				if e = <-done; e != nil {
					t.Fatal(e)
				}
			})
		}
	}
}
