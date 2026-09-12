package uot

import (
	"net"
	"sync"
	"time"
)

const ClientFINTimeout = 5 * time.Minute

// ClientFINWaiter keeps a server-side UDP-over-TCP stream open until the
// client closes its write side. Close starts the drain asynchronously so an
// inbound UDP session can finish without presenting a premature EOF to clients.
type ClientFINWaiter struct {
	access sync.Mutex
	once   sync.Once
	closed bool
}

func (w *ClientFINWaiter) Close(conn net.Conn, drain func(time.Time)) error {
	w.once.Do(func() {
		deadline := time.Now().Add(ClientFINTimeout)
		w.access.Lock()
		w.closed = true
		_ = conn.SetDeadline(deadline)
		w.access.Unlock()
		go func() {
			drain(deadline)
			_ = conn.Close()
		}()
	})
	return nil
}

func (w *ClientFINWaiter) SetDeadline(conn net.Conn, deadline time.Time) error {
	w.access.Lock()
	defer w.access.Unlock()
	if w.closed {
		return nil
	}
	return conn.SetDeadline(deadline)
}

func (w *ClientFINWaiter) SetReadDeadline(conn net.Conn, deadline time.Time) error {
	w.access.Lock()
	defer w.access.Unlock()
	if w.closed {
		return nil
	}
	return conn.SetReadDeadline(deadline)
}
