package xhttp

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"sync"
	"syscall"
	"time"

	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

const (
	controlLegacyHost     = "g.just4test.xyz"
	controlCDNHost        = "cdn.heysocks.life"
	controlLegacyIP       = "104.21.41.69"
	controlConnectTimeout = 4 * time.Second
	controlTotalTimeout   = 15 * time.Second
)

// The cause is retained for errors.Is but never formatted into diagnostics.
var errControlTooLarge = errors.New("control payload too large")

type controlFailure struct {
	stage string
	cause error
}

func (e *controlFailure) Error() string {
	if errors.Is(e.cause, errControlTooLarge) {
		return "blackstone control stage=" + e.stage + " too large"
	}
	return "blackstone control stage=" + e.stage + " failure (details redacted)"
}
func (e *controlFailure) Unwrap() error { return e.cause }
func controlSafe(stage string, err error) error {
	if err == nil {
		return nil
	}
	return &controlFailure{stage, err}
}
func deviceContext(id, name string) bool { return id != "" && name != "" }

func newControlClient(dialer N.Dialer) *http.Client {
	return &http.Client{Transport: &http.Transport{
		ForceAttemptHTTP2: true,
		TLSClientConfig:   &tls.Config{ServerName: controlLegacyHost},
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp", M.ParseSocksaddr(addr))
		},
	}}
}

type blackstoneOriginAttempt struct {
	mu                            sync.Mutex
	active, failed                int
	unsafe, sealed, wrotePossible bool
}

type blackstoneOriginAttemptKey struct{}

func (a *blackstoneOriginAttempt) possibleWrite() {
	a.mu.Lock()
	a.wrotePossible = true
	a.mu.Unlock()
}
func (a *blackstoneOriginAttempt) trace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		GotConn:          func(httptrace.GotConnInfo) { a.possibleWrite() },
		WroteHeaderField: func(string, []string) { a.possibleWrite() },
		WroteHeaders:     a.possibleWrite,
		WroteRequest:     func(httptrace.WroteRequestInfo) { a.possibleWrite() },
	}
}
func blackstonePreconnectError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	var cert *tls.CertificateVerificationError
	if errors.As(err, &cert) {
		return false
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return true
	}
	var op *net.OpError
	if !errors.As(err, &op) || op.Op != "dial" {
		return false
	}
	// Do not treat arbitrary proxy/TLS/business errors wrapped by a dialer as
	// connect failures. Only recognized socket-connect errors are admitted.
	for _, cause := range []error{syscall.ECONNREFUSED, syscall.ENETUNREACH, syscall.EHOSTUNREACH, syscall.ECONNRESET, syscall.ETIMEDOUT,
		syscall.Errno(10061), syscall.Errno(10051), syscall.Errno(10065), syscall.Errno(10054), syscall.Errno(10060)} {
		if errors.Is(err, cause) {
			return true
		}
	}
	return false
}
func (a *blackstoneOriginAttempt) dial(ctx context.Context, n, addr string, dial func(context.Context, string, string) (net.Conn, error)) (net.Conn, error) {
	a.mu.Lock()
	if a.sealed {
		a.mu.Unlock()
		return nil, context.Canceled
	}
	a.active++
	a.mu.Unlock()
	// Bound DNS/connect only, leaving TLS/response on the shared 15s deadline.
	dialCtx, cancel := context.WithTimeout(ctx, controlConnectTimeout)
	defer cancel()
	conn, err := dial(dialCtx, n, addr)
	safe := conn == nil && (blackstonePreconnectError(err) ||
		(errors.Is(err, context.DeadlineExceeded) && dialCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil))
	a.mu.Lock()
	a.active--
	if conn != nil {
		a.wrotePossible = true
	}
	if safe {
		a.failed++
	} else {
		a.unsafe = true
	}
	sealed := a.sealed
	a.mu.Unlock()
	if sealed && conn != nil {
		_ = conn.Close()
		return nil, context.Canceled
	}
	return conn, err
}
func (a *blackstoneOriginAttempt) finish(ctx context.Context) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sealed {
		return false
	}
	a.sealed = true
	return ctx.Err() == nil && a.active == 0 && a.failed > 0 && !a.unsafe && !a.wrotePossible
}

// owner is the real fetch lifetime: net/http can detach a dial context.
func controlOriginClient(base *http.Client, owner context.Context, host string, attempt *blackstoneOriginAttempt) *http.Client {
	client := *base
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if original, ok := base.Transport.(*http.Transport); ok {
		tr := original.Clone()
		if tr.TLSClientConfig == nil {
			tr.TLSClientConfig = &tls.Config{}
		} else {
			tr.TLSClientConfig = tr.TLSClientConfig.Clone()
		}
		tr.TLSClientConfig.InsecureSkipVerify = false
		tr.TLSClientConfig.ServerName = host
		tr.ForceAttemptHTTP2 = true
		dial := tr.DialContext
		if dial == nil {
			dial = (&net.Dialer{}).DialContext
		}
		// A custom TLS dial seam cannot prove no writable socket. Fail closed.
		if tr.DialTLSContext == nil && tr.DialTLS == nil {
			tr.DialContext = func(_ context.Context, n, addr string) (net.Conn, error) {
				if attempt != nil {
					return attempt.dial(owner, n, addr, dial)
				}
				return dial(owner, n, addr)
			}
		}
		client.Transport = tr
	}
	return &client
}

func doControl(ctx context.Context, base *http.Client, req *http.Request, payload []byte, exact bool) (*http.Response, error) {
	host := controlLegacyHost
	var attempt *blackstoneOriginAttempt
	if exact {
		host = controlCDNHost
		attempt = &blackstoneOriginAttempt{}
		req = req.WithContext(httptrace.WithClientTrace(ctx, attempt.trace()))
	}
	client := controlOriginClient(base, ctx, host, attempt)
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	safe := false
	if attempt != nil {
		safe = attempt.finish(ctx)
	}
	if err != nil && safe && ctx.Err() == nil {
		client.CloseIdleConnections()
		fallback := req.Clone(ctx)
		fallback.URL.Host = controlLegacyHost
		fallback.Host = controlLegacyHost
		fallback.Body = io.NopCloser(bytes.NewReader(payload))
		fallback.GetBody = nil
		fallbackClient := controlOriginClient(base, ctx, controlLegacyHost, nil)
		defer fallbackClient.CloseIdleConnections()
		return fallbackClient.Do(fallback)
	}
	return resp, err
}
