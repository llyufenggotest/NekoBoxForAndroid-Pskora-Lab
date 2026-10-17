package vless

import (
	"bytes"
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

	"github.com/sagernet/sing-box/common/vision"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	V "github.com/sagernet/sing-vmess/vless"
	M "github.com/sagernet/sing/common/metadata"
)

// Independent donor-stack oracle (exact donor 21e6f6ff): CommandUDP stream
// consumes private response once, then Vision, then uint16 packet lengths.
// Random padding is parsed rather than compared to a random golden byte string.
func bsOracleContent(r io.Reader, vision bool, first *bool) ([]byte, error) {
	if !vision {
		var b [2]byte
		if _, e := io.ReadFull(r, b[:]); e != nil {
			return nil, e
		}
		p := make([]byte, int(binary.BigEndian.Uint16(b[:])))
		_, e := io.ReadFull(r, p)
		return p, e
	}
	if *first {
		*first = false
		id := make([]byte, 16)
		if _, e := io.ReadFull(r, id); e != nil {
			return nil, e
		}
		want, _ := hex.DecodeString("00112233445566778899aabbccddeeff")
		if !bytes.Equal(id, want) {
			return nil, fmt.Errorf("missing Vision UUID after private Command2 header")
		}
	}
	var h [5]byte
	if _, e := io.ReadFull(r, h[:]); e != nil {
		return nil, e
	}
	if h[0] > 1 {
		return nil, fmt.Errorf("unexpected Vision command")
	}
	p := make([]byte, int(binary.BigEndian.Uint16(h[1:3])))
	if _, e := io.ReadFull(r, p); e != nil {
		return nil, e
	}
	_, e := io.CopyN(io.Discard, r, int64(binary.BigEndian.Uint16(h[3:5])))
	return p, e
}
func bsOracleResponse(vision bool, data []byte) []byte {
	response := []byte{0, 0, 2, 0xdd, 0xee}
	if !vision {
		return append(response, data...)
	}
	id, _ := hex.DecodeString("00112233445566778899aabbccddeeff")
	response = append(response, id...)
	response = append(response, 1, byte(len(data)>>8), byte(len(data)), 0, 0)
	return append(response, data...)
}

type bsHookTransport struct {
	base  *tls.Conn
	outer net.Conn
}

func (s *bsHookTransport) DialContext(ctx context.Context) (net.Conn, error) {
	if e := s.base.HandshakeContext(ctx); e != nil {
		return nil, e
	}
	if hook, ok := vision.HookFromContext(ctx); ok {
		hook(s.base)
	}
	return s.outer, nil
}
func (s *bsHookTransport) Close() error { return s.base.Close() }

type bsOuter struct{ net.Conn } // transport wrapper deliberately hides TLS type

func TestBlackstoneVisionNativeUDPProduction(t *testing.T) {
	certServer := httptest.NewTLSServer(nil)
	cert := certServer.TLS.Certificates[0]
	certServer.Close()
	for _, flow := range []string{"", V.FlowVision} {
		for _, entry := range []string{"listen", "dial", "hook-listen", "hook-dial"} {
			t.Run(fmt.Sprintf("flow=%s/%s", flow, entry), func(t *testing.T) {
				ln, e := net.Listen("tcp", "127.0.0.1:0")
				if e != nil {
					t.Fatal(e)
				}
				defer ln.Close()
				listener := tls.NewListener(ln, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13})
				dst := M.ParseSocksaddr("example.com:443")
				payload := bytes.Repeat([]byte{'P'}, 32)
				done := make(chan error, 1)
				go func() {
					raw, e := listener.Accept()
					if e != nil {
						done <- e
						return
					}
					defer raw.Close()
					_ = raw.SetDeadline(time.Now().Add(4 * time.Second))
					request, e := V.ReadRequest(raw)
					if e != nil {
						done <- e
						return
					}
					if request.Command != 2 || request.Flow != flow || request.Destination != dst {
						done <- fmt.Errorf("Command2/flow/destination mismatch")
						return
					}
					private := make([]byte, 20)
					_, e = io.ReadFull(raw, private)
					if e != nil {
						done <- e
						return
					}
					if hex.EncodeToString(private) != bsDigest+"03aabbcc" {
						done <- fmt.Errorf("private header mismatch")
						return
					}
					first := true
					for i := 0; i < 2; i++ {
						var packet []byte
						if flow == "" {
							packet, e = bsOracleContent(raw, false, &first)
						} else {
							// Length and payload may occupy different Vision records (donor does
							// two writes); consume a stream rather than requiring write boundaries.
							var stream []byte
							for len(stream) < 2 || len(stream) < 2+int(binary.BigEndian.Uint16(stream[:2])) {
								var part []byte
								part, e = bsOracleContent(raw, true, &first)
								if e != nil {
									break
								}
								stream = append(stream, part...)
							}
							if e == nil {
								if len(stream) != 34 {
									e = fmt.Errorf("double/missing packet length")
								} else {
									packet = stream[2:]
								}
							}
						}
						if e != nil {
							done <- e
							return
						}
						if !bytes.Equal(packet, payload) {
							done <- fmt.Errorf("packet payload mismatch")
							return
						}
					}
					// A short caller buffer must drain only the first packet, preserving the
					// next datagram, and private response H must be consumed exactly once.
					_, e = raw.Write(bsOracleResponse(flow != "", []byte{0, 3, 'b', 'i', 'g', 0, 1, 'R'}))
					done <- e
				}()
				empty := ""
				opts := option.VLESSOutboundOptions{ServerOptions: option.ServerOptions{Server: "127.0.0.1", ServerPort: uint16(ln.Addr().(*net.TCPAddr).Port)}, UUID: bsID + "#blackstone", Flow: flow, E: true, H: "03aabbcc", PacketEncoding: &empty, OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{TLS: &option.OutboundTLSOptions{Enabled: true, Insecure: true, ServerName: "localhost"}}}
				obj, e := NewOutbound(context.Background(), nil, log.NewNOPFactory().Logger(), "synthetic", opts)
				if e != nil {
					t.Fatal(e)
				}
				out := obj.(*Outbound)
				if entry == "hook-listen" || entry == "hook-dial" {
					raw, e := net.Dial("tcp", ln.Addr().String())
					if e != nil {
						t.Fatal(e)
					}
					tc := tls.Client(raw, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13})
					out.transport = &bsHookTransport{base: tc, outer: &bsOuter{tc}}
				}
				ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
				defer cancel()
				var pc net.PacketConn
				if entry == "dial" || entry == "hook-dial" {
					c, err := obj.DialContext(ctx, "udp", dst)
					e = err
					if e == nil {
						pc = c.(net.PacketConn)
					}
				} else {
					pc, e = obj.ListenPacket(ctx, dst)
				}
				if e != nil {
					t.Fatal(e)
				}
				defer pc.Close()
				_ = pc.SetDeadline(time.Now().Add(4 * time.Second))
				for i := 0; i < 2; i++ {
					if _, e = pc.WriteTo(payload, dst); e != nil {
						t.Fatal(e)
					}
				}
				var small [1]byte
				if _, _, e = pc.ReadFrom(small[:]); e != io.ErrShortBuffer {
					serverErr := <-done
					t.Fatalf("short-buffer error=%v; donor stack oracle=%v", e, serverErr)
				}
				n, addr, e := pc.ReadFrom(small[:])
				if e != nil || n != 1 || small[0] != 'R' || addr.String() != dst.String() {
					t.Fatalf("packet/domain response n=%d addr=%v err=%v", n, addr, e)
				}
				if e = <-done; e != nil {
					t.Fatal(e)
				}
				// Shared generation cache: switch flow solely in this test to inspect the
				// next request without a second TLS fixture. No new client is allocated.
				saved := out.client
				nextRaw := &bsRecordingConn{}
				next, e := saved.DialEarlyPacketConn(nextRaw, dst)
				if e != nil {
					t.Fatal(e)
				}
				if _, e = next.Write([]byte("P")); e != nil {
					t.Fatal(e)
				}
				if !bytes.Contains(nextRaw.Bytes(), []byte{2, 0xdd, 0xee, 0, 1, 'P'}) {
					t.Fatal("response cache was not shared with subsequent stream")
				}
			})
		}
	}
}
