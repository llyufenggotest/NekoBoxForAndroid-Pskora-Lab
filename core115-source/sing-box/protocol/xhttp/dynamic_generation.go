package xhttp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"strconv"
	"sync"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/shadowsocks"
	"github.com/sagernet/sing-box/protocol/vless"
	"github.com/sagernet/sing/common"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/service"
)

// A generation owns its immutable selection, wire H cache, delegate and staged
// resources. Refresh never closes it: existing early connections still use H.
// Parent Close joins the worker and retires all generations exactly once.
type dynamicGeneration struct {
	cfg      RealConfig
	client   *WireClient
	delegate adapter.Outbound
	scope    *adapter.Scope
	once     sync.Once
	closeErr error
}

func (g *dynamicGeneration) Close() error {
	if g == nil {
		return nil
	}
	g.once.Do(func() {
		if g.scope != nil {
			g.closeErr = g.scope.Close()
		}
		g.closeErr = errors.Join(g.closeErr, common.Close(g.delegate))
	})
	return g.closeErr
}

// Never reconstruct an independent default dialer for a dynamic delegate. This
// local manager exposes only the already-owned parent data dialer; server options
// cannot supply routing overrides. Both SS and VLESS use this identical seam.
type dynamicDialerManager struct {
	adapter.OutboundManager
	data adapter.Outbound
}

func (m *dynamicDialerManager) Outbound(tag string) (adapter.Outbound, bool) {
	return m.data, tag == "blackstone-owned-data"
}

type dynamicDataOutbound struct {
	outbound.Adapter
	N.Dialer
}

func (h *Outbound) constructGeneration(ctx context.Context, cfg RealConfig) (*dynamicGeneration, error) {
	fail := func(err error) (*dynamicGeneration, error) { return nil, dynamicSafe("node-capability", err) }
	// Own nested maps/slices as well as the outer value; refresh input cannot
	// mutate a published delegate's selected configuration.
	raw, cloneErr := json.Marshal(cfg)
	if cloneErr != nil {
		return fail(cloneErr)
	}
	var owned RealConfig
	if cloneErr = json.Unmarshal(raw, &owned); cloneErr != nil {
		return fail(cloneErr)
	}
	cfg = owned
	port, err := strconv.ParseUint(cfg.Port, 10, 16)
	if err != nil || port == 0 || cfg.Server == "" {
		return fail(errors.New("endpoint"))
	}
	if cfg.UDPOverTCP && (!cfg.UDP || cfg.UDPOverTCPVersion < 0 || cfg.UDPOverTCPVersion > 2) {
		return fail(errors.New("uot"))
	}
	if cfg.PaddingLen != "" {
		return fail(errors.New("padding unsupported"))
	}
	if cfg.Type != "xhttp" && (cfg.TcpFake != "" || cfg.UdpFake != "") {
		return fail(errors.New("fake prefix unsupported"))
	}
	if cfg.Type == "vless" && cfg.UDPOverTCP {
		return fail(errors.New("vless uot unsupported"))
	}
	g := &dynamicGeneration{cfg: cfg}
	if cfg.Type == "xhttp" {
		g.client, err = NewWireClient(WireConfig{Password: cfg.Password, Cipher: cfg.Cipher, TCPFake: cfg.TcpFake, UDPFake: cfg.UdpFake})
		if err != nil {
			return fail(err)
		}
		return g, nil
	}
	base := h.ctx
	if base == nil {
		base = context.Background()
	}
	data := &dynamicDataOutbound{Adapter: outbound.NewAdapter("direct", "blackstone-owned-data", []string{N.NetworkTCP, N.NetworkUDP}, nil), Dialer: h.dialer}
	base = service.ContextWith[adapter.OutboundManager](base, &dynamicDialerManager{data: data})
	server := option.ServerOptions{Server: cfg.Server, ServerPort: uint16(port)}
	route := option.DialerOptions{Detour: "blackstone-owned-data"}
	// Delegate logs would otherwise disclose response endpoint/destination details.
	safeLogger := log.NewNOPFactory().Logger()
	switch cfg.Type {
	case "ss":
		opts := option.ShadowsocksOutboundOptions{DialerOptions: route, ServerOptions: server, Method: cfg.Cipher, Password: cfg.Password + "#BLACKSTONE"}
		if cfg.UDPOverTCP {
			opts.UDPOverTCP = &option.UDPOverTCPOptions{Enabled: true, Version: uint8(cfg.UDPOverTCPVersion)}
		}
		g.delegate, err = shadowsocks.NewOutbound(base, h.router, safeLogger, h.Tag(), opts)
	case "vless":
		var opts option.VLESSOutboundOptions
		opts, err = dynamicVLESSOptions(cfg)
		if err == nil {
			opts.DialerOptions = route
			opts.ServerOptions = server
			g.delegate, err = vless.NewOutbound(base, h.router, safeLogger, h.Tag(), opts)
		}
	default:
		err = errors.New("transport")
	}
	if err != nil {
		_ = g.Close()
		return fail(err)
	}
	// NewOutbound alone does NOT register transport/mux cleanup under alpha.12.
	// Own a local scope and run the same stage list as the outbound manager.
	g.scope = adapter.NewScope(base, safeLogger)
	if lifecycle, ok := g.delegate.(adapter.Lifecycle); ok {
		for _, stage := range adapter.ListStartStages {
			if err = lifecycle.Start(stage, g.scope); err != nil {
				_ = g.Close()
				return fail(err)
			}
		}
	}
	if err = ctx.Err(); err != nil {
		_ = g.Close()
		return fail(err)
	}
	return g, nil
}

func (h *Outbound) preflight(ctx context.Context, cfg RealConfig) error {
	g, err := h.constructGeneration(ctx, cfg)
	if err != nil {
		return err
	}
	return dynamicSafe("node-capability", g.Close())
}

// No arbitrary cause/Unwrap: even %#v/errors.As must not recover response secrets.
// Only lifecycle sentinels are retained as fixed flags.
type dynamicFailure struct {
	stage                                                             string
	canceled, deadline, closed, eof, shortBuffer, shortWrite, timeout bool
}

func (e *dynamicFailure) Error() string {
	return "blackstone stage=" + e.stage + " failure (details redacted)"
}
func (e *dynamicFailure) Is(target error) bool {
	return (e.canceled && target == context.Canceled) || (e.deadline && target == context.DeadlineExceeded) || (e.closed && target == net.ErrClosed) || (e.eof && target == io.EOF) || (e.shortBuffer && target == io.ErrShortBuffer) || (e.shortWrite && target == io.ErrShortWrite) || (e.timeout && target == os.ErrDeadlineExceeded)
}
func (e *dynamicFailure) Timeout() bool   { return e.deadline || e.timeout }
func (e *dynamicFailure) Temporary() bool { return e.Timeout() }
func dynamicSafe(stage string, err error) error {
	if err == nil {
		return nil
	}
	return &dynamicFailure{stage: stage, canceled: errors.Is(err, context.Canceled), deadline: errors.Is(err, context.DeadlineExceeded), closed: errors.Is(err, net.ErrClosed), eof: errors.Is(err, io.EOF), shortBuffer: errors.Is(err, io.ErrShortBuffer), shortWrite: errors.Is(err, io.ErrShortWrite), timeout: errors.Is(err, os.ErrDeadlineExceeded)}
}
