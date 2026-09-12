package http

import (
	"context"
	"net"
	"net/http"
	"os"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/dialer"
	"github.com/sagernet/sing-box/common/tls"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	sHTTP "github.com/sagernet/sing/protocol/http"
)

func RegisterOutbound(registry *outbound.Registry) {
	outbound.Register[option.HTTPOutboundOptions](registry, C.TypeHTTP, NewOutbound)
}

type Outbound struct {
	outbound.Adapter
	logger    logger.ContextLogger
	client    *sHTTP.Client
	tlsDialer tls.Dialer
	server    M.Socksaddr
	username  string
	password  string
	host      string
	path      string
	headers   http.Header
}

func NewOutbound(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.HTTPOutboundOptions) (adapter.Outbound, error) {
	outboundDialer, err := dialer.New(ctx, options.DialerOptions, options.ServerIsDomain())
	if err != nil {
		return nil, err
	}
	if options.TLS != nil && options.TLS.Enabled && len(options.TLS.ALPN) == 0 {
		options.TLS.ALPN = []string{"h2", "http/1.1"}
	}
	detour, err := tls.NewDialerFromOptions(ctx, logger, outboundDialer, options.Server, common.PtrValueOrDefault(options.TLS))
	if err != nil {
		return nil, err
	}
	headers := options.Headers.Build()
	var host string
	if headers != nil {
		host = headers.Get("Host")
	}
	var tlsDetour tls.Dialer
	if options.TLS != nil && options.TLS.Enabled {
		if tlsDialer, isTLSDialer := detour.(tls.Dialer); isTLSDialer {
			tlsDetour = tlsDialer
		}
	}
	return &Outbound{
		Adapter: outbound.NewAdapterWithDialerOptions(C.TypeHTTP, tag, []string{N.NetworkTCP}, options.DialerOptions),
		logger:  logger,
		client: sHTTP.NewClient(sHTTP.Options{
			Dialer:   detour,
			Server:   options.ServerOptions.Build(),
			Username: options.Username,
			Password: options.Password,
			Path:     options.Path,
			Headers:  headers,
		}),
		tlsDialer: tlsDetour,
		server:    options.ServerOptions.Build(),
		username:  options.Username,
		password:  options.Password,
		host:      host,
		path:      options.Path,
		headers:   headers,
	}, nil
}

func (h *Outbound) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	ctx, metadata := adapter.ExtendContext(ctx)
	metadata.Outbound = h.Tag()
	metadata.Destination = destination
	h.logger.InfoContext(ctx, "outbound connection to ", destination)
	if h.tlsDialer != nil {
		tlsConn, err := h.tlsDialer.DialTLSContext(ctx, h.server)
		if err != nil {
			return nil, err
		}
		if stateConn, ok := tlsConn.(connectionStateConn); ok {
			if stateConn.ConnectionState().NegotiatedProtocol == "h2" {
				return dialH2Connect(ctx, tlsConn, destination, h.host, h.username, h.password, h.headers)
			}
		}
		fallbackHeaders := h.headers.Clone()
		if h.host != "" {
			fallbackHeaders.Set("Host", h.host)
		}
		return sHTTP.NewClient(sHTTP.Options{
			Dialer:   &singleConnDialer{conn: tlsConn},
			Server:   h.server,
			Username: h.username,
			Password: h.password,
			Path:     h.path,
			Headers:  fallbackHeaders,
		}).DialContext(ctx, network, destination)
	}
	return h.client.DialContext(ctx, network, destination)
}

func (h *Outbound) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	return nil, os.ErrInvalid
}
