package dialer

import (
	"context"
	"errors"
	N "github.com/sagernet/sing/common/network"
	"net"
	"syscall"
	"time"
)

type concurrentDialKey struct{}

func WithConcurrentDial(ctx context.Context, enabled bool) context.Context {
	return context.WithValue(ctx, concurrentDialKey{}, enabled)
}
func ConcurrentDialEnabled(ctx context.Context) bool {
	return ctx.Value(concurrentDialKey{}) == true && ctx.Value(ctxKeyNoConcurrentDial) != true
}

const ctxKeyNoConcurrentDial = "nb4a_no_concurrent_dial"

type ConnWithErr struct {
	conn net.Conn
	err  error
}

func getResultFromConnChan(connChan chan ConnWithErr) (net.Conn, error) {
	var i int
	var err error
	defer func() {
		go func(index int) {
			for i := index; i < 3; i++ {
				if conn := <-connChan; conn.err == nil {
					go conn.conn.Close()
				}
			}
			close(connChan)
		}(i + 1)
	}()
	for i = 0; i < 3; i++ {
		conn := <-connChan
		if conn.err == nil {
			return conn.conn, nil
		}
		err = conn.err
	}
	return nil, err
}

func isRetryableDialError(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ECONNABORTED) ||
		errors.Is(err, syscall.EHOSTUNREACH) ||
		errors.Is(err, syscall.ENETUNREACH) ||
		errors.Is(err, syscall.EADDRNOTAVAIL)
}

func retryDial(ctx context.Context, dial func() (net.Conn, error)) (net.Conn, error) {
	var err error
	for i := 0; i < 4; i++ {
		conn, dialErr := dial()
		if dialErr == nil {
			return conn, nil
		}
		err = dialErr
		if !isRetryableDialError(err) {
			break
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if i == 3 {
			break
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, err
}

func dialContextWithRetry(dialer net.Dialer, ctx context.Context, network string, destination string) (net.Conn, error) {
	return retryDial(ctx, func() (net.Conn, error) {
		return dialer.DialContext(ctx, network, destination)
	})
}

func dialContextConcurrently(dialer net.Dialer, ctx context.Context, network string, destination string) (net.Conn, error) {
	if v := ctx.Value(ctxKeyNoConcurrentDial); v == true || !ConcurrentDialEnabled(ctx) || N.NetworkName(network) == N.NetworkUDP {
		return dialer.DialContext(ctx, network, destination)
	}
	raceCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	connChan := make(chan ConnWithErr, 3)
	for i := 0; i < 3; i++ {
		go func() {
			var conn ConnWithErr
			conn.conn, conn.err = dialContextWithRetry(dialer, raceCtx, network, destination)
			connChan <- conn
		}()
	}
	return getResultFromConnChan(connChan)
}
