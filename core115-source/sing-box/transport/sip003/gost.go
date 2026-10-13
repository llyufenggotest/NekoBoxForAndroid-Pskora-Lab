package sip003

import (
	"context"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/sagernet/sing-box/adapter"
	boxTLS "github.com/sagernet/sing-box/common/tls"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/transport/v2raywebsocket"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/json/badoption"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/smux"
)

func init() {
	RegisterPlugin("gost-plugin", newGostPlugin)
}

type gostMode uint8

const (
	gostModeWebsocket gostMode = iota
	gostModeTLS
)

type gostOptions struct {
	mode       gostMode
	tls        bool
	mux        bool
	host       string
	path       string
	serverName string
	insecure   bool
}

func parseGostOptions(pluginOpts Args) (gostOptions, error) {
	options := gostOptions{
		mode: gostModeWebsocket,
		host: "cloudfront.com",
		path: "/",
	}
	mode := "websocket"
	if value, loaded := pluginOpts.Get("mode"); loaded {
		mode = strings.ToLower(value)
	}
	switch mode {
	case "websocket", "ws":
	case "wss":
		options.tls = true
	case "mws":
		options.mux = true
	case "mwss":
		options.tls = true
		options.mux = true
	case "tls":
		options.mode = gostModeTLS
		options.tls = true
	case "mtls":
		options.mode = gostModeTLS
		options.tls = true
		options.mux = true
	case "h2", "quic", "grpc":
		return options, E.New("gost-plugin: unsupported mode: ", mode)
	default:
		return options, E.New("gost-plugin: unknown mode: ", mode)
	}
	if value, loaded := pluginOpts.Get("host"); loaded {
		options.host = value
	}
	if value, loaded := pluginOpts.Get("path"); loaded {
		options.path = value
	}
	if value, loaded := pluginOpts.Get("serverName"); loaded {
		options.serverName = value
	}
	if options.serverName == "" {
		options.serverName = options.host
	}
	var err error
	if value, loaded := pluginOpts.Get("tls"); loaded {
		options.tls, err = parseGostBool("tls", value)
		if err != nil {
			return options, err
		}
	}
	if value, loaded := pluginOpts.Get("mux"); loaded {
		options.mux, err = parseGostBool("mux", value)
		if err != nil {
			return options, err
		}
	}
	if value, loaded := pluginOpts.Get("insecure"); loaded {
		options.insecure, err = parseGostBool("insecure", value)
		if err != nil {
			return options, err
		}
	}
	if _, loaded := pluginOpts.Get("ed"); loaded {
		return options, E.New("gost-plugin: websocket early data is unsupported")
	}
	if value, loaded := pluginOpts.Get("nocomp"); loaded {
		disabled, parseErr := parseGostBool("nocomp", value)
		if parseErr != nil {
			return options, parseErr
		}
		if !disabled {
			return options, E.New("gost-plugin: websocket compression is unsupported")
		}
	}
	return options, nil
}

func parseGostBool(name string, value string) (bool, error) {
	switch strings.ToLower(value) {
	case "", "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, E.New("gost-plugin: invalid ", name, " value: ", strconv.Quote(value))
	}
}

var (
	_ Plugin                       = (*gostPlugin)(nil)
	_ adapter.IdleConnectionKeeper = (*gostPlugin)(nil)
)

type gostPlugin struct {
	dialer     N.Dialer
	serverAddr M.Socksaddr
	options    gostOptions
	tlsConfig  boxTLS.Config

	access  sync.Mutex
	session *smux.Session
	closed  bool
}

func newGostPlugin(ctx context.Context, pluginOpts Args, _ adapter.Router, dialer N.Dialer, serverAddr M.Socksaddr) (Plugin, error) {
	options, err := parseGostOptions(pluginOpts)
	if err != nil {
		return nil, err
	}
	plugin := &gostPlugin{
		dialer:     dialer,
		serverAddr: serverAddr,
		options:    options,
	}
	if options.tls {
		tlsOptions := option.OutboundTLSOptions{
			Enabled:    true,
			ServerName: options.serverName,
			Insecure:   options.insecure,
		}
		plugin.tlsConfig, err = boxTLS.NewClient(ctx, logger.NOP(), serverAddr.AddrString(), tlsOptions)
		if err != nil {
			return nil, err
		}
	}
	return plugin, nil
}

func (p *gostPlugin) DialContext(ctx context.Context) (net.Conn, error) {
	p.access.Lock()
	if p.closed {
		p.access.Unlock()
		return nil, net.ErrClosed
	}
	if p.options.mux && p.session != nil && !p.session.IsClosed() {
		session := p.session
		p.access.Unlock()
		stream, err := session.OpenStream()
		if err == nil {
			return stream, nil
		}
		p.resetSession(session)
		return p.DialContext(ctx)
	}
	p.access.Unlock()

	conn, err := p.dialTransport(ctx)
	if err != nil {
		return nil, err
	}
	if !p.options.mux {
		return conn, nil
	}
	config := smux.DefaultConfig()
	config.Version = 1
	config.KeepAliveDisabled = true
	session, err := smux.Client(conn, config)
	if err != nil {
		conn.Close()
		return nil, err
	}
	p.access.Lock()
	if p.closed {
		p.access.Unlock()
		session.Close()
		return nil, net.ErrClosed
	}
	if p.session != nil && !p.session.IsClosed() {
		existing := p.session
		p.access.Unlock()
		session.Close()
		return existing.OpenStream()
	}
	p.session = session
	p.access.Unlock()
	stream, err := session.OpenStream()
	if err != nil {
		p.resetSession(session)
		return nil, err
	}
	return stream, nil
}

func (p *gostPlugin) dialTransport(ctx context.Context) (net.Conn, error) {
	switch p.options.mode {
	case gostModeTLS:
		conn, err := p.dialer.DialContext(ctx, N.NetworkTCP, p.serverAddr)
		if err != nil {
			return nil, err
		}
		if p.tlsConfig == nil {
			return conn, nil
		}
		tlsConn, err := boxTLS.ClientHandshake(ctx, conn, p.tlsConfig)
		if err != nil {
			conn.Close()
			return nil, err
		}
		return tlsConn, nil
	case gostModeWebsocket:
		transport, err := v2raywebsocket.NewClient(ctx, p.dialer, p.serverAddr, option.V2RayWebsocketOptions{
			Path: p.options.path,
			Headers: badoption.HTTPHeader{
				"Host": badoption.Listable[string]{p.options.host},
			},
			MaxEarlyData: 0,
		}, p.tlsConfig)
		if err != nil {
			return nil, err
		}
		return transport.DialContext(ctx)
	default:
		return nil, E.New("gost-plugin: invalid internal mode")
	}
}

func (p *gostPlugin) resetSession(session *smux.Session) {
	p.access.Lock()
	if p.session == session {
		p.session = nil
	}
	p.access.Unlock()
	_ = session.Close()
}

func (p *gostPlugin) Close() error {
	p.access.Lock()
	if p.closed {
		p.access.Unlock()
		return nil
	}
	p.closed = true
	session := p.session
	p.session = nil
	p.access.Unlock()
	if session != nil {
		return session.Close()
	}
	return nil
}

func (p *gostPlugin) SetKeepIdleConnections(keep bool) {
	if !keep {
		p.CloseIdleConnections()
	}
}

func (p *gostPlugin) MultiplexEnabled() bool {
	return p.options.mux
}

func (p *gostPlugin) CloseIdleConnections() {
	p.access.Lock()
	session := p.session
	p.session = nil
	p.access.Unlock()
	if session != nil {
		_ = session.Close()
	}
}
