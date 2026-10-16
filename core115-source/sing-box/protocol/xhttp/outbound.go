package xhttp

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/dialer"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/bufio"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

func RegisterOutbound(registry *outbound.Registry) {
	outbound.Register[option.XHttpOutboundOptions](registry, "xhttp", NewOutbound)
	outbound.Register[option.BlackstoneOutboundOptions](registry, "Blackstone", func(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.BlackstoneOutboundOptions) (adapter.Outbound, error) {
		return newOutbound(ctx, router, logger, tag, "Blackstone", options.XHttpOutboundOptions)
	})
}

type Outbound struct {
	outbound.Adapter
	ctx                                            context.Context
	router                                         adapter.Router
	logger                                         logger.ContextLogger
	dialer                                         N.Dialer
	nodeName, xToken, nodeID, deviceID, deviceName string
	initMu                                         sync.Mutex
	initAttempt                                    *initAttempt
	isInit, closed                                 bool
	initializeFn                                   func(context.Context) error
	bestCfg                                        RealConfig
	generation                                     *dynamicGeneration
	retired                                        []*dynamicGeneration
	closeOnce                                      sync.Once
	closeErr                                       error
}
type initAttempt struct {
	done      chan struct{}
	err       error
	cancel    context.CancelFunc
	waiters   map[*initWaiter]context.Context
	candidate *dynamicGeneration
}
type initWaiter struct{ marker byte }

func NewOutbound(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.XHttpOutboundOptions) (adapter.Outbound, error) {
	return newOutbound(ctx, router, logger, tag, "xhttp", options)
}
func newOutbound(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag, outboundType string, options option.XHttpOutboundOptions) (adapter.Outbound, error) {
	data, err := dialer.New(ctx, options.DialerOptions, options.ServerIsDomain())
	if err != nil {
		return nil, err
	}
	name := options.Name
	if name == "" {
		name = tag
	}
	return &Outbound{Adapter: outbound.NewAdapterWithDialerOptions(outboundType, tag, options.Network.Build(), options.DialerOptions), ctx: ctx, router: router, logger: logger, dialer: data, nodeName: name, xToken: options.Password, nodeID: options.NodeID, deviceID: options.DeviceID, deviceName: options.DeviceName}, nil
}
func (h *Outbound) pingRace(ctx context.Context, configs []RealConfig) (RealConfig, error) {
	if err := ctx.Err(); err != nil {
		return RealConfig{}, err
	}
	if len(configs) == 1 {
		return configs[0], nil
	}
	type result struct {
		cfg RealConfig
		err error
	}
	results := make(chan result, len(configs))
	race, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	for _, cfg := range configs {
		wg.Add(1)
		go func(c RealConfig) {
			defer wg.Done()
			dialCtx, stop := context.WithTimeout(race, 3*time.Second)
			defer stop()
			conn, err := h.dialer.DialContext(dialCtx, N.NetworkTCP, M.ParseSocksaddr(net.JoinHostPort(c.Server, c.Port)))
			if conn != nil {
				_ = conn.Close()
			}
			results <- result{c, err}
		}(cfg)
	}
	go func() { wg.Wait(); close(results) }()
	var best *RealConfig
	for res := range results {
		if res.err == nil && best == nil {
			c := res.cfg
			best = &c
			cancel()
		}
	}
	if err := ctx.Err(); err != nil {
		return RealConfig{}, err
	}
	if best != nil {
		return *best, nil
	}
	return RealConfig{}, dynamicSafe("ping", errors.New("all nodes failed"))
}
func (h *Outbound) lazyInit(ctx context.Context) error { return h.joinInit(ctx, false) }
func (h *Outbound) Refresh(ctx context.Context) error  { return h.joinInit(ctx, true) }
func (h *Outbound) joinInit(ctx context.Context, refresh bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	h.initMu.Lock()
	if h.closed {
		h.initMu.Unlock()
		return net.ErrClosed
	}
	if h.isInit && !refresh {
		h.initMu.Unlock()
		return nil
	}
	a := h.initAttempt
	if a == nil {
		worker, cancel := context.WithTimeout(context.WithoutCancel(ctx), controlTotalTimeout)
		a = &initAttempt{done: make(chan struct{}), cancel: cancel, waiters: make(map[*initWaiter]context.Context)}
		h.initAttempt = a
		go h.runInit(worker, a)
	}
	token := &initWaiter{}
	if a.waiters != nil {
		a.waiters[token] = ctx
	}
	h.initMu.Unlock()
	select {
	case <-ctx.Done():
		h.initMu.Lock()
		delete(a.waiters, token)
		live := false
		for _, waiter := range a.waiters {
			if waiter.Err() == nil {
				live = true
				break
			}
		}
		if !live && a.cancel != nil {
			a.cancel()
		}
		h.initMu.Unlock()
		if !live && a.cancel != nil {
			<-a.done
		}
		return ctx.Err()
	case <-a.done:
		if err := ctx.Err(); err != nil {
			return err
		}
		return a.err
	}
}
func (h *Outbound) runInit(ctx context.Context, a *initAttempt) {
	initialize := h.initializeFn
	if initialize == nil {
		initialize = h.initialize
	}
	err := initialize(ctx)
	h.initMu.Lock()
	if err == nil {
		err = ctx.Err()
	}
	live := false
	for _, waiter := range a.waiters {
		if waiter.Err() == nil {
			live = true
			break
		}
	}
	if !live && err == nil {
		err = context.Canceled
	}
	if h.closed {
		err = net.ErrClosed
	}
	candidate := a.candidate
	if err == nil {
		if candidate != nil {
			if h.generation != nil {
				h.retired = append(h.retired, h.generation)
			}
			h.generation = candidate
			h.bestCfg = candidate.cfg
			a.candidate = nil
		}
		h.isInit = true
	}
	h.initMu.Unlock()
	if err != nil && candidate != nil {
		_ = candidate.Close()
	}
	h.initMu.Lock()
	a.err = err
	if h.initAttempt == a {
		h.initAttempt = nil
	}
	a.cancel()
	close(a.done)
	h.initMu.Unlock()
}
func (h *Outbound) initialize(ctx context.Context) error {
	configs, err := FetchDynamicConfigWithPreflight(ctx, h.dialer, h.nodeName, h.nodeID, h.xToken, h.deviceID, h.deviceName, h.preflight)
	if err != nil {
		var failure *dynamicFailure
		if errors.As(err, &failure) {
			return failure
		}
		return dynamicSafe("control", err)
	}
	if !deviceContext(h.deviceID, h.deviceName) {
		if err = PreflightConfigs(ctx, configs, h.preflight); err != nil {
			return dynamicSafe("node-capability", err)
		}
	}
	best, err := h.pingRace(ctx, configs)
	if err != nil {
		return dynamicSafe("ping", err)
	}
	g, err := h.constructGeneration(ctx, best)
	if err != nil {
		return err
	}
	h.initMu.Lock()
	if h.closed || ctx.Err() != nil {
		h.initMu.Unlock()
		_ = g.Close()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return net.ErrClosed
	}
	h.initAttempt.candidate = g
	h.initMu.Unlock()
	return nil
}
func (h *Outbound) selected(ctx context.Context) (*dynamicGeneration, error) {
	if err := h.lazyInit(ctx); err != nil {
		return nil, err
	}
	h.initMu.Lock()
	defer h.initMu.Unlock()
	if h.closed {
		return nil, net.ErrClosed
	}
	if h.generation == nil {
		g, err := h.constructGeneration(ctx, h.bestCfg)
		if err != nil {
			return nil, err
		}
		h.generation = g
	}
	return h.generation, nil
}
func (h *Outbound) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	ctx, metadata := adapter.ExtendContext(ctx)
	metadata.Outbound = h.Tag()
	metadata.Destination = destination
	g, err := h.selected(ctx)
	if err != nil {
		return nil, err
	}
	switch N.NetworkName(network) {
	case N.NetworkTCP:
		if g.delegate != nil {
			c, e := g.delegate.DialContext(ctx, network, destination)
			if e != nil {
				if c != nil {
					_ = c.Close()
				}
				return nil, dynamicSafe("tcp", e)
			}
			return wrapDynamicConn(c), nil
		}
		return h.openStream(ctx, g, destination)
	case N.NetworkUDP:
		p, e := h.listenGeneration(ctx, g, destination)
		if e != nil {
			return nil, e
		}
		return bufio.NewBindPacketConn(p, destination), nil
	default:
		return nil, dynamicSafe("network", N.ErrUnknownNetwork)
	}
}
func (h *Outbound) openStream(ctx context.Context, g *dynamicGeneration, destination M.Socksaddr) (net.Conn, error) {
	raw, err := h.dialer.DialContext(ctx, N.NetworkTCP, M.ParseSocksaddr(net.JoinHostPort(g.cfg.Server, g.cfg.Port)))
	if err != nil {
		if raw != nil {
			_ = raw.Close()
		}
		return nil, dynamicSafe("tcp", err)
	}
	if tcp, ok := raw.(*net.TCPConn); ok {
		_ = tcp.SetNoDelay(true)
	}
	stream, err := g.client.OpenStream(ctx, raw, destination)
	if err != nil {
		return nil, dynamicSafe("stream", err)
	}
	return wrapDynamicConn(stream), nil
}
func (h *Outbound) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	ctx, metadata := adapter.ExtendContext(ctx)
	metadata.Outbound = h.Tag()
	metadata.Destination = destination
	g, err := h.selected(ctx)
	if err != nil {
		return nil, err
	}
	return h.listenGeneration(ctx, g, destination)
}
func (h *Outbound) listenGeneration(ctx context.Context, g *dynamicGeneration, destination M.Socksaddr) (net.PacketConn, error) {
	if !g.cfg.UDP {
		return nil, dynamicSafe("udp-disabled", N.ErrUnknownNetwork)
	}
	if g.delegate != nil {
		p, err := g.delegate.ListenPacket(ctx, destination)
		if err != nil {
			if p != nil {
				_ = p.Close()
			}
			return nil, dynamicSafe("udp", err)
		}
		return wrapDynamicPacket(p), nil
	}
	if g.cfg.UDPOverTCP {
		magic, err := WireUOTDestination(g.cfg.UDPOverTCPVersion)
		if err != nil {
			return nil, dynamicSafe("uot", err)
		}
		stream, err := h.openStream(ctx, g, magic)
		if err != nil {
			return nil, err
		}
		p, err := NewWireUOT(stream, g.cfg.UDPOverTCPVersion, destination)
		if err != nil {
			_ = stream.Close()
			return nil, dynamicSafe("uot", err)
		}
		return wrapDynamicPacket(p), nil
	}
	remote := M.ParseSocksaddr(net.JoinHostPort(g.cfg.Server, g.cfg.Port))
	raw, err := h.dialer.ListenPacket(ctx, remote)
	if err != nil {
		if raw != nil {
			_ = raw.Close()
		}
		return nil, dynamicSafe("udp", err)
	}
	// Resolver dialers return a NAT PacketConn accepting the original domain.
	// Keep that domain so DNS, interface/protect and detour ownership stay intact.
	if !remote.IsFqdn() {
		return wrapDynamicPacket(g.client.PacketConn(raw, remote.UDPAddr())), nil
	}
	return wrapDynamicPacket(g.client.PacketConn(raw, remote)), nil
}
func (h *Outbound) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	if stage == adapter.StartStateInitialize {
		scope.Add(h.Close)
	}
	return nil
}

// Capability inspection never fetches or dials. Before selection preserve the
// configured framework admission; after selection report the immutable policy.
func (h *Outbound) Network() []string {
	h.initMu.Lock()
	defer h.initMu.Unlock()
	if h.closed {
		return nil
	}
	if h.generation != nil {
		if h.generation.cfg.UDP {
			return []string{N.NetworkTCP, N.NetworkUDP}
		}
		return []string{N.NetworkTCP}
	}
	return h.Adapter.Network()
}
func (h *Outbound) InterfaceUpdated() {}
func (h *Outbound) Close() error {
	h.closeOnce.Do(func() {
		h.initMu.Lock()
		h.closed = true
		a := h.initAttempt
		if a != nil && a.cancel != nil {
			a.cancel()
		}
		h.initMu.Unlock()
		if a != nil && a.cancel != nil {
			<-a.done
		}
		h.initMu.Lock()
		all := append(h.retired, h.generation)
		h.retired = nil
		h.generation = nil
		h.initMu.Unlock()
		for _, g := range all {
			h.closeErr = errors.Join(h.closeErr, dynamicSafe("close", g.Close()))
		}
	})
	return h.closeErr
}
