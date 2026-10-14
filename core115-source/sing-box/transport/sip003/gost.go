package sip003

import (
	"context"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

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
	mode        gostMode
	tls         bool
	muxEnabled  bool
	muxCapacity int
	host        string
	path        string
	serverName  string
	insecure    bool
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
		options.muxEnabled = true
		options.muxCapacity = 1
	case "mwss":
		options.tls = true
		options.muxEnabled = true
		options.muxCapacity = 1
	case "tls":
		options.mode = gostModeTLS
		options.tls = true
	case "mtls":
		options.mode = gostModeTLS
		options.tls = true
		options.muxEnabled = true
		options.muxCapacity = 1
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
		// YAML booleans are explicit overrides; numeric zero retains GOST's
		// unlimited-capacity meaning and must not be treated as false.
		switch strings.ToLower(value) {
		case "true", "yes", "on":
			options.muxEnabled = true
			options.muxCapacity = 1
		case "false", "no", "off":
			options.muxEnabled = false
			options.muxCapacity = 0
		default:
			capacity, parseErr := strconv.ParseUint(value, 10, 16)
			if parseErr != nil {
				return options, E.New("gost-plugin: invalid mux value: ", strconv.Quote(value))
			}
			options.muxEnabled = true
			options.muxCapacity = int(capacity)
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

	access    sync.Mutex
	sessions  []*gostSession
	closeIdle bool
	closed    bool
}

type gostSession struct {
	client     *smux.Session
	lastActive time.Time
}

type gostStream struct {
	net.Conn
	plugin  *gostPlugin
	session *gostSession
	once    sync.Once
}

func (s *gostStream) Close() error {
	err := s.Conn.Close()
	s.once.Do(func() {
		s.plugin.closeSessionIfIdle(s.session)
	})
	return err
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
	if !p.options.muxEnabled {
		p.access.Lock()
		closed := p.closed
		p.access.Unlock()
		if closed {
			return nil, net.ErrClosed
		}
		return p.dialTransport(ctx)
	}

	p.access.Lock()
	defer p.access.Unlock()
	if p.closed {
		return nil, net.ErrClosed
	}
	return p.dialMuxLocked(ctx)
}

func (p *gostPlugin) dialMuxLocked(ctx context.Context) (net.Conn, error) {
	p.removeClosedSessionsLocked()
	for _, session := range p.sessions {
		if p.options.muxCapacity > 0 && session.client.NumStreams() >= p.options.muxCapacity {
			continue
		}
		stream, err := session.client.OpenStream()
		session.lastActive = time.Now()
		if err == nil {
			return &gostStream{Conn: stream, plugin: p, session: session}, nil
		}
		p.removeSessionLocked(session)
	}

	conn, err := p.dialTransport(ctx)
	if err != nil {
		return nil, err
	}
	config := smux.DefaultConfig()
	config.Version = 1
	config.KeepAliveDisabled = true
	client, err := smux.Client(conn, config)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	session := &gostSession{client: client, lastActive: time.Now()}
	p.sessions = append(p.sessions, session)
	stream, err := client.OpenStream()
	session.lastActive = time.Now()
	if err != nil {
		p.removeSessionLocked(session)
		return nil, err
	}
	return &gostStream{Conn: stream, plugin: p, session: session}, nil
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

func (p *gostPlugin) closeSessionIfIdle(session *gostSession) {
	p.access.Lock()
	defer p.access.Unlock()
	if p.closeIdle && session.client.NumStreams() == 0 {
		p.removeSessionLocked(session)
	}
}

func (p *gostPlugin) removeClosedSessionsLocked() {
	for index := 0; index < len(p.sessions); {
		if p.sessions[index].client.IsClosed() {
			p.closeAndRemoveSessionLocked(index)
			continue
		}
		index++
	}
}

func (p *gostPlugin) removeSessionLocked(target *gostSession) {
	for index, session := range p.sessions {
		if session == target {
			p.closeAndRemoveSessionLocked(index)
			return
		}
	}
}

func (p *gostPlugin) closeAndRemoveSessionLocked(index int) {
	session := p.sessions[index]
	p.sessions = append(p.sessions[:index], p.sessions[index+1:]...)
	_ = session.client.Close()
}

func (p *gostPlugin) Close() error {
	p.access.Lock()
	if p.closed {
		p.access.Unlock()
		return nil
	}
	p.closed = true
	sessions := p.sessions
	p.sessions = nil
	p.access.Unlock()
	var closeErr error
	for _, session := range sessions {
		if err := session.client.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	return closeErr
}

func (p *gostPlugin) SetKeepIdleConnections(keep bool) {
	p.access.Lock()
	p.closeIdle = !keep
	p.access.Unlock()
	if !keep {
		p.CloseIdleConnections()
	}
}

func (p *gostPlugin) MultiplexEnabled() bool {
	return p.options.muxEnabled
}

func (p *gostPlugin) ResetConnections() {
	p.access.Lock()
	sessions := p.sessions
	p.sessions = nil
	p.access.Unlock()
	for _, session := range sessions {
		_ = session.client.Close()
	}
}

func (p *gostPlugin) CloseIdleConnections() {
	p.access.Lock()
	defer p.access.Unlock()
	for index := 0; index < len(p.sessions); {
		if p.sessions[index].client.NumStreams() == 0 {
			p.closeAndRemoveSessionLocked(index)
			continue
		}
		index++
	}
}
