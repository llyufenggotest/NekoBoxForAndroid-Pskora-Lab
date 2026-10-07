package snellv4

import (
	"context"
	"net"
	"sync"
	"time"

	snell "github.com/sagernet/sing-snell"
	"github.com/sagernet/sing-snell/internal/reuse"
	"github.com/sagernet/sing/common/buf"
	E "github.com/sagernet/sing/common/exceptions"
	N "github.com/sagernet/sing/common/network"
)

// Warm establishes fresh OIX identity sessions, confirms ping/pong, and parks
// them in the existing reuse pool. It never sends a destination or user data.
// One bounded batch may run at a time; Close cancels and joins it.
func (c *Client) Warm(parent context.Context, count int) error {
	if count < 0 || count > 4 {
		return E.New("snell: preconnect must be between 0 and 4")
	}
	c.warmAccess.Lock()
	if c.warmClosed {
		c.warmAccess.Unlock()
		return net.ErrClosed
	}
	if count == 0 {
		c.warmAccess.Unlock()
		return nil
	}
	if !c.reuse || !c.requireExporter || c.dialer == nil {
		c.warmAccess.Unlock()
		return E.New("snell: preconnect requires OIX identity reuse and dialer")
	}
	if c.warmDone != nil {
		c.warmAccess.Unlock()
		return E.New("snell: preconnect already running")
	}
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	done := make(chan struct{})
	c.warmCancel, c.warmDone = cancel, done
	c.warmAccess.Unlock()
	defer func() {
		cancel()
		c.warmAccess.Lock()
		c.warmCancel, c.warmDone = nil, nil
		close(done)
		c.warmAccess.Unlock()
	}()
	for i := 0; i < count; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := c.warmSession(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) warmSession(ctx context.Context) error {
	conn, err := c.dialer.DialContext(ctx, N.NetworkTCP, c.server)
	if err != nil {
		return err
	}
	// Closing the transport interrupts both a blocked write and a blocked pong
	// read. Wait for the cancellation callback before transferring ownership.
	stopped := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { conn.Close(); close(stopped) })
	var finishOnce sync.Once
	finish := func() {
		finishOnce.Do(func() {
			if !stop() {
				<-stopped
			}
		})
	}
	defer finish()
	fail := func(err error) error {
		conn.Close()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	exporter, err := c.exporterForConn(conn)
	if err != nil {
		return fail(err)
	}
	session := c.newReuseSession(conn, exporter)
	session.writer = &writer{upstream: session.Conn, psk: c.psk, identityExporter: session.identityExporter}
	request := snell.Request{Command: snell.CommandPing, ClientID: c.userKey}
	buffer := buf.NewSize(request.Len())
	err = request.Write(buffer)
	if err == nil {
		_, err = session.writer.Write(buffer.Bytes())
	}
	buffer.Release()
	if err != nil {
		return fail(err)
	}
	session.reader = &reader{upstream: session.Conn, psk: c.psk}
	reply, err := session.reader.ReadRecord()
	if err != nil {
		return fail(err)
	}
	valid := reply.Len() == 1 && reply.Byte(0) == snell.ReplyPong
	reply.Release()
	if !valid {
		return fail(E.New("snell: unexpected preconnect pong"))
	}
	finish()
	if err = ctx.Err(); err != nil {
		return fail(err)
	}
	session.state.Store(uint32(reuse.StateReady))
	if !c.pool.MoveToPool(session, reuse.StateReady, false) {
		return net.ErrClosed
	}
	return nil
}
