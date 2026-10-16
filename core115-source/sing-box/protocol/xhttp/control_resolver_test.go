package xhttp

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/miekg/dns"
	"github.com/sagernet/sing-box/adapter"
	O "github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/dialer"
	C "github.com/sagernet/sing-box/constant"
	D "github.com/sagernet/sing-box/dns"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/control"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/service"
	"github.com/stretchr/testify/require"
)

// Only the DNS transport and platform socket-protection service are synthetic.
// Constructor, resolve dialer, DNS router/client, TLS/H2 and data sockets are real.
// In particular, these tests never replace Outbound.dialer.
type resolverFixtureTransport struct {
	adapter.DNSTransport
	tag       string
	mu        sync.Mutex
	questions []dns.Question
}

func (d *resolverFixtureTransport) Type() string                                   { return "fixture" }
func (d *resolverFixtureTransport) Tag() string                                    { return d.tag }
func (d *resolverFixtureTransport) Dependencies() []string                         { return nil }
func (d *resolverFixtureTransport) Start(adapter.StartStage, *adapter.Scope) error { return nil }
func (d *resolverFixtureTransport) Reset()                                         {}
func (d *resolverFixtureTransport) Exchange(_ context.Context, q *dns.Msg) (*dns.Msg, error) {
	d.mu.Lock()
	d.questions = append(d.questions, q.Question...)
	d.mu.Unlock()
	r := new(dns.Msg)
	r.SetReply(q)
	for _, question := range q.Question {
		if question.Name != dns.Fqdn(controlCDNHost) && question.Name != "data.fixture.invalid." {
			return nil, fmt.Errorf("unexpected resolver destination")
		}
		if question.Qtype == dns.TypeA {
			r.Answer = append(r.Answer, &dns.A{Hdr: dns.RR_Header{Name: question.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 1}, A: net.IPv4(127, 0, 0, 1)})
		}
	}
	return r, nil
}
func (d *resolverFixtureTransport) ExchangeAsync(ctx context.Context, q *dns.Msg, cb func(*dns.Msg, error)) {
	r, e := d.Exchange(ctx, q)
	cb(r, e)
}

type resolverFixtureNetwork struct {
	adapter.NetworkManager
	options  adapter.NetworkOptions
	tcp, udp atomic.Int32
}

func (n *resolverFixtureNetwork) InterfaceFinder() control.InterfaceFinder {
	return control.NewDefaultInterfaceFinder()
}
func (n *resolverFixtureNetwork) DefaultOptions() adapter.NetworkOptions   { return n.options }
func (n *resolverFixtureNetwork) AutoDetectInterface() bool                { return true }
func (n *resolverFixtureNetwork) AutoRedirectOutputMarkFunc() control.Func { return nil }
func (n *resolverFixtureNetwork) ProtectFunc() control.Func {
	return func(network, address string, raw syscall.RawConn) error {
		if strings.HasPrefix(network, "tcp") {
			n.tcp.Add(1)
		} else {
			n.udp.Add(1)
		}
		// Require an actual OS socket, not a mock connection above the protection seam.
		return raw.Control(func(fd uintptr) {
			if fd == ^uintptr(0) {
				panic("invalid socket")
			}
		})
	}
}
func (n *resolverFixtureNetwork) AutoDetectInterfaceFunc() control.Func { return n.ProtectFunc() }

func resolverFixtureContext(t *testing.T) (context.Context, *resolverFixtureTransport, *resolverFixtureTransport, *resolverFixtureNetwork) {
	t.Helper()
	factory := log.NewNOPFactory()
	selected := &resolverFixtureTransport{tag: "group-dns"}
	other := &resolverFixtureTransport{tag: "other-dns"}
	registry := D.NewTransportRegistry()
	D.RegisterTransport[struct{}](registry, "fixture", func(_ context.Context, _ log.ContextLogger, tag string, _ struct{}) (adapter.DNSTransport, error) {
		if tag == selected.tag {
			return selected, nil
		}
		return other, nil
	})
	manager := D.NewTransportManager(registry, nil, selected.tag)
	ctx := service.ContextWith[adapter.DNSTransportManager](context.Background(), manager)
	require.NoError(t, manager.Create(ctx, factory.Logger(), selected.tag, "fixture", nil))
	require.NoError(t, manager.Create(ctx, factory.Logger(), other.tag, "fixture", nil))
	network := &resolverFixtureNetwork{options: adapter.NetworkOptions{DomainResolver: selected.tag, DomainResolveOptions: adapter.DNSQueryOptions{Strategy: C.DomainStrategyIPv4Only, DisableCache: true}}}
	ctx = service.ContextWith[adapter.NetworkManager](ctx, network)
	router, err := D.NewRouter(ctx, factory, option.DNSOptions{})
	require.NoError(t, err)
	ctx = service.ContextWith[adapter.DNSRouter](ctx, router)
	return ctx, selected, other, network
}

func resolverFixtureOutbound(t *testing.T, ctx context.Context, options option.DialerOptions, kind string, device bool) *Outbound {
	t.Helper()
	registry := O.NewRegistry()
	RegisterOutbound(registry)
	opts := option.XHttpOutboundOptions{ServerOptions: option.ServerOptions{Server: "192.0.2.1", ServerPort: 443}, DialerOptions: options}
	if device {
		opts.DeviceID = "synthetic-device"
		opts.DeviceName = "synthetic-tablet"
	}
	var raw any = &opts
	if kind == "Blackstone" {
		raw = &option.BlackstoneOutboundOptions{XHttpOutboundOptions: opts}
	}
	result, err := registry.CreateOutbound(ctx, nil, log.NewNOPFactory().Logger(), "fixture", kind, raw)
	require.NoError(t, err)
	h := result.(*Outbound)
	t.Cleanup(func() { require.NoError(t, h.Close()) })
	return h
}

func TestBlackstoneIPPlaceholderConstructorResolver(t *testing.T) {
	for _, kind := range []string{"Blackstone", "xhttp"} {
		t.Run(kind, func(t *testing.T) {
			ctx, selected, other, _ := resolverFixtureContext(t)
			h := resolverFixtureOutbound(t, ctx, option.DialerOptions{}, kind, true)
			resolved, ok := h.dialer.(dialer.ResolveDialer)
			require.True(t, ok, "IP placeholder must not disable DNS for actual control/data domains")
			require.Equal(t, C.DomainStrategyIPv4Only, resolved.QueryOptions().Strategy)
			require.Same(t, selected, resolved.QueryOptions().Transport)
			require.Empty(t, other.questions)
		})
	}
}

func TestBlackstoneIPPlaceholderLiteralDataBypassesDNS(t *testing.T) {
	ctx, selected, other, network := resolverFixtureContext(t)
	h := resolverFixtureOutbound(t, ctx, option.DialerOptions{}, "Blackstone", true)
	peer, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer peer.Close()
	c, err := h.dialer.DialContext(ctx, "tcp", M.ParseSocksaddr(peer.Addr().String()))
	require.NoError(t, err)
	defer c.Close()
	accepted, err := peer.Accept()
	require.NoError(t, err)
	accepted.Close()
	require.Empty(t, selected.questions)
	require.Empty(t, other.questions)
	require.Equal(t, int32(1), network.tcp.Load())
}

func TestBlackstoneIPPlaceholderResolverPolicies(t *testing.T) {
	for _, mode := range []string{"legacy-strategy", "explicit-detour", "remote-detour", "legacy-xhttp"} {
		t.Run(mode, func(t *testing.T) {
			ctx, selected, _, _ := resolverFixtureContext(t)
			opts := option.DialerOptions{}
			if mode == "legacy-xhttp" {
				h := resolverFixtureOutbound(t, ctx, opts, "xhttp", false)
				_, ok := h.dialer.(*dialer.DefaultDialer)
				require.True(t, ok, "legacy fixed-IP xhttp must remain unchanged")
				return
			}
			if mode == "legacy-strategy" {
				opts.DomainStrategy = option.DomainStrategy(C.DomainStrategyIPv6Only)
			}
			if strings.Contains(mode, "detour") {
				upstream := &dynamicDataOutbound{Adapter: O.NewAdapter("fixture", "blackstone-owned-data", []string{"tcp", "udp"}, nil), Dialer: &dynamicSystemDialer{dial: func(_ context.Context, _ string, a M.Socksaddr) (net.Conn, error) {
					if mode == "remote-detour" {
						require.Equal(t, "data.fixture.invalid", a.Fqdn)
					} else {
						require.True(t, a.Addr.Unmap().Is4(), "resolved address: %s", a.String())
					}
					l, r := net.Pipe()
					r.Close()
					return l, nil
				}}}
				ctx = service.ContextWith[adapter.OutboundManager](ctx, &dynamicDialerManager{data: upstream})
				opts.Detour = "blackstone-owned-data"
				if mode == "explicit-detour" {
					opts.DomainResolver = &option.DomainResolveOptions{Server: "group-dns", Strategy: option.DomainStrategy(C.DomainStrategyIPv4Only), DisableCache: true}
				}
			}
			h := resolverFixtureOutbound(t, ctx, opts, "Blackstone", true)
			if mode == "legacy-strategy" {
				r, ok := h.dialer.(dialer.ResolveDialer)
				require.True(t, ok)
				require.Equal(t, C.DomainStrategyIPv6Only, r.QueryOptions().Strategy)
				return
			}
			c, e := h.dialer.DialContext(ctx, "tcp", M.ParseSocksaddr("data.fixture.invalid:443"))
			require.NoError(t, e)
			c.Close()
			selected.mu.Lock()
			defer selected.mu.Unlock()
			if mode == "remote-detour" {
				require.Empty(t, selected.questions)
			} else {
				require.NotEmpty(t, selected.questions)
			}
		})
	}
}

// Full vertical gate: registry constructor with IP placeholder -> framework
// resolver/router/client -> protected sockets -> verified TLS/H2 control ->
// selected-member constructor preflight -> domain xhttp TCP/native UDP/UOT.
func TestBlackstoneIPPlaceholderVertical(t *testing.T) {
	for _, detour := range []bool{false, true} {
		for _, mode := range []string{"tcp", "udp", "uot0", "uot1", "uot2"} {
			t.Run(fmt.Sprintf("detour=%t/%s", detour, mode), func(t *testing.T) {
				ctx, selected, other, network := resolverFixtureContext(t)
				var port uint16
				done := make(chan error, 1)
				target := M.ParseSocksaddr("route.example:443")
				if mode == "udp" {
					peer, e := net.ListenPacket("udp", "127.0.0.1:0")
					require.NoError(t, e)
					defer peer.Close()
					port = M.SocksaddrFromNet(peer.LocalAddr()).Port
					go func() {
						peer.SetDeadline(time.Now().Add(5 * time.Second))
						frame := make([]byte, 2048)
						n, src, e := peer.ReadFrom(frame)
						if e == nil {
							if n < 164 {
								e = fmt.Errorf("short wire UDP")
							} else {
								block, _ := aes.NewCipher(dynamicLiteralKey())
								plain := make([]byte, n-163)
								cipher.NewCTR(block, frame[147:163]).XORKeyStream(plain, frame[163:n])
								want := append([]byte{3, 13}, []byte("route.example")...)
								want = append(want, 1, 187, 'P')
								if !bytes.Equal(plain, want) {
									e = fmt.Errorf("wrong UDP target")
								} else {
									_, e = peer.WriteTo(wireServer(dynamicLiteralKey(), []byte{9}, []byte{1, 192, 0, 2, 1, 0, 53, 'R'}), src)
								}
							}
						}
						done <- e
					}()
				} else {
					peer, e := net.Listen("tcp", "127.0.0.1:0")
					require.NoError(t, e)
					defer peer.Close()
					port = M.SocksaddrFromNet(peer.Addr()).Port
					go func() {
						c, e := peer.Accept()
						if e != nil {
							done <- e
							return
						}
						defer c.Close()
						c.SetDeadline(time.Now().Add(5 * time.Second))
						header := make([]byte, 163)
						if _, e = io.ReadFull(c, header); e != nil {
							done <- e
							return
						}
						block, _ := aes.NewCipher(dynamicLiteralKey())
						reader := &cipher.StreamReader{S: cipher.NewCTR(block, header[147:]), R: c}
						want := append([]byte{3, 13}, []byte("route.example")...)
						want = append(want, 1, 187, 'P')
						response := []byte{'R'}
						if strings.HasPrefix(mode, "uot") {
							magic := "sp.v2.udp-over-tcp.arpa"
							if mode == "uot1" {
								magic = "sp.udp-over-tcp.arpa"
							}
							want = append([]byte{3, byte(len(magic))}, []byte(magic)...)
							want = append(want, 0, 0)
							native := append([]byte{2, 13}, []byte("route.example")...)
							native = append(native, 1, 187)
							if mode != "uot1" {
								want = append(want, 0)
								want = append(want, native...)
							}
							want = append(want, native...)
							want = append(want, 0, 1, 'P')
							response = []byte{0, 192, 0, 2, 1, 0, 53, 0, 1, 'R'}
						}
						got := make([]byte, len(want))
						_, e = io.ReadFull(reader, got)
						if e == nil && !bytes.Equal(got, want) {
							e = fmt.Errorf("wrong TCP/UOT target")
						}
						if e == nil {
							_, e = c.Write(wireServer(dynamicLiteralKey(), []byte{9}, response))
						}
						done <- e
					}()
				}
				extra := ""
				if strings.HasPrefix(mode, "uot") {
					extra = ", udp-over-tcp: true, udp-over-tcp-version: " + strings.TrimPrefix(mode, "uot")
				}
				document := dynamicDocument(fmt.Sprintf(" - {name: wire, type: xhttp, server: data.fixture.invalid, port: %d, password: 'synthetic:0102', udp: true%s}\n", port, extra), "wire")
				response := controlDocumentResponse(t, document)
				cert, pool := controlFixtureCertificate(t, controlCDNHost)
				var posts atomic.Int32
				server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					posts.Add(1)
					require.Equal(t, controlCDNHost, r.Host)
					require.Equal(t, controlCDNHost, r.TLS.ServerName)
					require.Equal(t, 2, r.ProtoMajor)
					io.Copy(io.Discard, r.Body)
					w.Write(response)
				}))
				server.Listener.Close()
				listener, e := net.Listen("tcp", "127.0.0.1:443")
				require.NoError(t, e, "offline canonical fixture requires loopback port 443")
				server.Listener = listener
				server.EnableHTTP2 = true
				server.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
				server.StartTLS()
				defer server.Close()
				ctx = service.ContextWith[adapter.CertificateStore](ctx, &dynamicCertificateStore{pool: pool})
				opts := option.DialerOptions{}
				if detour {
					// Real framework resolver + DefaultDialer behind the detour, not net.Dialer.
					upstream, e := dialer.New(ctx, opts, true)
					require.NoError(t, e)
					data := &dynamicDataOutbound{Adapter: O.NewAdapter("fixture", "blackstone-owned-data", []string{N.NetworkTCP, N.NetworkUDP}, nil), Dialer: upstream}
					ctx = service.ContextWith[adapter.OutboundManager](ctx, &dynamicDialerManager{data: data})
					opts.Detour = "blackstone-owned-data"
				}
				h := resolverFixtureOutbound(t, ctx, opts, "Blackstone", true)
				request, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				if mode == "tcp" {
					c, e := h.DialContext(request, "tcp", target)
					require.NoError(t, e)
					defer c.Close()
					c.SetDeadline(time.Now().Add(3 * time.Second))
					_, e = c.Write([]byte{'P'})
					require.NoError(t, e)
					b := make([]byte, 1)
					_, e = io.ReadFull(c, b)
					require.NoError(t, e)
					require.Equal(t, []byte{'R'}, b)
				} else {
					p, e := h.ListenPacket(request, target)
					require.NoError(t, e)
					defer p.Close()
					p.SetDeadline(time.Now().Add(3 * time.Second))
					_, e = p.WriteTo([]byte{'P'}, target)
					require.NoError(t, e)
					b := make([]byte, 1)
					n, src, e := p.ReadFrom(b)
					require.NoError(t, e)
					require.Equal(t, 1, n)
					require.Equal(t, []byte{'R'}, b)
					require.Equal(t, "192.0.2.1:53", src.String())
				}
				require.NoError(t, <-done)
				require.Equal(t, int32(1), posts.Load())
				require.True(t, h.isInit)
				require.Equal(t, "xhttp", h.generation.cfg.Type)
				require.GreaterOrEqual(t, network.tcp.Load(), int32(1))
				if mode == "udp" {
					require.Equal(t, int32(1), network.udp.Load())
				} else {
					require.Zero(t, network.udp.Load())
					require.GreaterOrEqual(t, network.tcp.Load(), int32(2))
				}
				selected.mu.Lock()
				questions := append([]dns.Question(nil), selected.questions...)
				selected.mu.Unlock()
				names := map[string]bool{}
				for _, q := range questions {
					require.Equal(t, uint16(dns.TypeA), q.Qtype, "preserve IPv4-only policy")
					names[q.Name] = true
				}
				require.True(t, names[dns.Fqdn(controlCDNHost)])
				require.True(t, names["data.fixture.invalid."])
				require.Empty(t, other.questions, "group DNS must not leak to other resolver")
				// Route destination stays on wire and must never be resolved locally.
				require.False(t, names["route.example."])
			})
		}
	}
}
