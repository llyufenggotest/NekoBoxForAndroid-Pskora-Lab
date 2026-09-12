package direct

import (
	"context"
	"encoding/binary"
	"fmt"
	"github.com/sagernet/sing-box/option"
	"io"
	"math/rand/v2"
	"net"
	"strconv"
	"strings"
	"time"
)

type Fragment struct {
	MinLength, MaxLength     int
	MinInterval, MaxInterval time.Duration
}

func parseFragment(o *option.Fragment) (*Fragment, error) {
	if o == nil {
		return nil, nil
	}
	parse := func(s string, minimum, maximum int) (int, int, error) {
		p := strings.Split(s, "-")
		if len(p) < 1 || len(p) > 2 {
			return 0, 0, fmt.Errorf("invalid fragment range %q", s)
		}
		a, e := strconv.Atoi(p[0])
		if e != nil {
			return 0, 0, e
		}
		b := a
		if len(p) == 2 {
			b, e = strconv.Atoi(p[1])
			if e != nil {
				return 0, 0, e
			}
		}
		if a > b {
			a, b = b, a
		}
		if a < minimum || b > maximum {
			return 0, 0, fmt.Errorf("fragment range out of bounds: %s", s)
		}
		return a, b, nil
	}
	a, b, e := parse(o.Length, 1, 65535)
	if e != nil {
		return nil, e
	}
	x, y, e := parse(o.Interval, 0, 2147483647)
	if e != nil {
		return nil, e
	}
	return &Fragment{a, b, time.Duration(x) * time.Millisecond, time.Duration(y) * time.Millisecond}, nil
}

type FragmentedClientHelloConn struct {
	net.Conn
	ctx      context.Context
	fragment *Fragment
	written  bool
}

func newFragmentConn(ctx context.Context, c net.Conn, f *Fragment) net.Conn {
	return &FragmentedClientHelloConn{Conn: c, ctx: ctx, fragment: f}
}
func (c *FragmentedClientHelloConn) Upstream() any { return c.Conn }
func (c *FragmentedClientHelloConn) Write(p []byte) (int, error) {
	if c.written {
		return c.Conn.Write(p)
	}
	// Incomplete/non-handshake records must pass through without slicing past input.
	if len(p) < 5 || p[0] != 22 || int(binary.BigEndian.Uint16(p[3:5])) > len(p)-5 || binary.BigEndian.Uint16(p[3:5]) == 0 {
		n, e := c.Conn.Write(p)
		c.written = e == nil
		return n, e
	}
	length := int(binary.BigEndian.Uint16(p[3:5]))
	f := c.fragment
	for pos := 0; pos < length; {
		select {
		case <-c.ctx.Done():
			return 0, c.ctx.Err()
		default:
		}
		size := f.MinLength
		if f.MaxLength > size {
			size += rand.IntN(f.MaxLength - size)
		}
		size = min(size, length-pos)
		record := make([]byte, 5+size)
		copy(record, p[:5])
		binary.BigEndian.PutUint16(record[3:5], uint16(size))
		copy(record[5:], p[5+pos:5+pos+size])
		n, e := c.Conn.Write(record)
		if e == nil && n != len(record) {
			e = io.ErrShortWrite
		}
		if e != nil {
			return 0, e
		}
		pos += size
		if pos < length {
			delay := f.MinInterval
			if f.MaxInterval > delay {
				delay += time.Duration(rand.Int64N(int64(f.MaxInterval - delay)))
			}
			if delay > 0 {
				timer := time.NewTimer(delay)
				select {
				case <-timer.C:
				case <-c.ctx.Done():
					timer.Stop()
					return 0, c.ctx.Err()
				}
			}
		}
	}
	if len(p) > 5+length {
		n, e := c.Conn.Write(p[5+length:])
		if e != nil {
			return 5 + length + n, e
		}
		if n != len(p)-5-length {
			return 5 + length + n, io.ErrShortWrite
		}
	}
	c.written = true
	return len(p), nil
}
func (h *Outbound) wrapFragment(ctx context.Context, network string, conn net.Conn, err error) (net.Conn, error) {
	if err == nil && network == "tcp" && h.fragment != nil {
		return newFragmentConn(ctx, conn, h.fragment), nil
	}
	return conn, err
}
