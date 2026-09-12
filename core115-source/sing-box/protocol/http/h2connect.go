package http

import (
	"context"
	stdtls "crypto/tls"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sagernet/sing/common/baderror"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	"golang.org/x/net/http2"
)

type connectionStateConn interface {
	ConnectionState() stdtls.ConnectionState
}

type h2TunnelConn struct {
	rawConn    net.Conn
	pipeWriter *io.PipeWriter
	reader     io.ReadCloser
	closeOnce  sync.Once
}

var _ net.Conn = (*h2TunnelConn)(nil)

func dialH2Connect(
	ctx context.Context,
	tlsConn net.Conn,
	destination M.Socksaddr,
	host string,
	username string,
	password string,
	headers http.Header,
) (net.Conn, error) {
	transport := &http2.Transport{
		DisableCompression: true,
	}
	var connUsed atomic.Bool
	transport.DialTLSContext = func(ctx context.Context, network, addr string, cfg *stdtls.Config) (net.Conn, error) {
		if !connUsed.CompareAndSwap(false, true) {
			return nil, E.New("http: h2 tunnel connection already in use")
		}
		return tlsConn, nil
	}
	pipeReader, pipeWriter := io.Pipe()
	request := (&http.Request{
		Method: http.MethodConnect,
		URL: &url.URL{
			Scheme: "https",
			Host:   destination.String(),
		},
		Header: http.Header{},
		Body:   pipeReader,
	}).WithContext(ctx)
	if host != "" && host != destination.Fqdn {
		request.Host = host
	}
	if username != "" {
		auth := username + ":" + password
		request.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(auth)))
	}
	for key, valueList := range headers {
		switch strings.ToLower(key) {
		case "connection", "proxy-connection", "keep-alive", "upgrade", "transfer-encoding":
			continue
		}
		for _, value := range valueList {
			request.Header.Add(key, value)
		}
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		_ = pipeReader.Close()
		_ = pipeWriter.Close()
		_ = tlsConn.Close()
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		_ = response.Body.Close()
		_ = pipeWriter.Close()
		_ = tlsConn.Close()
		switch response.StatusCode {
		case http.StatusProxyAuthRequired:
			return nil, E.New("authentication required")
		case http.StatusMethodNotAllowed:
			return nil, E.New("method not allowed")
		default:
			return nil, E.New("http: unexpected status: ", response.Status)
		}
	}
	return &h2TunnelConn{
		rawConn:    tlsConn,
		pipeWriter: pipeWriter,
		reader:     response.Body,
	}, nil
}

func (c *h2TunnelConn) Read(p []byte) (n int, err error) {
	n, err = c.reader.Read(p)
	return n, baderror.WrapH2(err)
}

func (c *h2TunnelConn) Write(p []byte) (n int, err error) {
	n, err = c.pipeWriter.Write(p)
	return n, baderror.WrapH2(err)
}

func (c *h2TunnelConn) Close() error {
	c.closeOnce.Do(func() {
		_ = c.pipeWriter.Close()
		_ = c.reader.Close()
		_ = c.rawConn.Close()
	})
	return nil
}

func (c *h2TunnelConn) LocalAddr() net.Addr {
	return c.rawConn.LocalAddr()
}

func (c *h2TunnelConn) RemoteAddr() net.Addr {
	return c.rawConn.RemoteAddr()
}

func (c *h2TunnelConn) SetDeadline(t time.Time) error {
	return os.ErrInvalid
}

func (c *h2TunnelConn) SetReadDeadline(t time.Time) error {
	return os.ErrInvalid
}

func (c *h2TunnelConn) SetWriteDeadline(t time.Time) error {
	return os.ErrInvalid
}

type singleConnDialer struct {
	conn net.Conn
}

var _ N.Dialer = (*singleConnDialer)(nil)

func (d *singleConnDialer) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	conn := d.conn
	if conn == nil {
		return nil, E.New("http: connection already used")
	}
	d.conn = nil
	return conn, nil
}

func (d *singleConnDialer) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	return nil, os.ErrInvalid
}
