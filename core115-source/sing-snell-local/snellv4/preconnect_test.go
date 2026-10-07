package snellv4

import (
	"bytes"
	"context"
	"errors"
	snell "github.com/sagernet/sing-snell"
	M "github.com/sagernet/sing/common/metadata"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type warmDialer struct {
	dial  func(context.Context) (net.Conn, error)
	calls atomic.Int32
}

func (d *warmDialer) DialContext(ctx context.Context, _ string, _ M.Socksaddr) (net.Conn, error) {
	d.calls.Add(1)
	return d.dial(ctx)
}
func (*warmDialer) ListenPacket(context.Context, M.Socksaddr) (net.PacketConn, error) {
	return nil, errors.New("unsupported")
}

func TestPreconnectPingPoolAndExporter(t *testing.T) {
	for _, count := range []int{0, 2, 4} {
		t.Run(string(rune('0'+count)), func(t *testing.T) {
			var peers []net.Conn
			var workers sync.WaitGroup
			defer workers.Wait()
			defer func() {
				for _, p := range peers {
					p.Close()
				}
			}()
			d := &warmDialer{dial: func(context.Context) (net.Conn, error) {
				a, b := net.Pipe()
				peers = append(peers, b)
				index := byte(len(peers))
				workers.Add(1)
				go func() {
					defer workers.Done()
					// Remove only the client identity extension; the salt and subsequent
					// encrypted records are consumed by the real v4 record reader.
					prefix := make([]byte, 56)
					if _, err := io.ReadFull(b, prefix); err != nil {
						return
					}
					exporter := bytes.Repeat([]byte{index}, 32)
					want, authErr := IdentityV2AuthTag([]byte("password"), exporter, prefix[:16])
					if authErr != nil || string(prefix[16:24]) != identityWireMagicV2 || !bytes.Equal(prefix[40:56], want) {
						t.Error("warm identity is not bound to this session")
						return
					}
					r := &reader{upstream: io.MultiReader(bytes.NewReader(prefix[:16]), b), psk: []byte("password")}
					record, err := r.ReadRecord()
					if err != nil {
						return
					}
					request, err := snell.ReadRequest(bytes.NewReader(record.Bytes()))
					record.Release()
					if err != nil || request.Command != snell.CommandPing {
						return
					}
					w := &writer{upstream: b, psk: []byte("password")}
					w.Write([]byte{snell.ReplyPong})
					record, err = r.ReadRecord()
					if err != nil {
						return
					}
					request, err = snell.ReadRequest(bytes.NewReader(record.Bytes()))
					record.Release()
					if err != nil || request.Command != snell.CommandConnectV2 || request.Destination.String() != "example.com:80" {
						t.Error("real request did not reuse warmed cipher/session")
					}
				}()
				return a, nil
			}}
			var exports atomic.Int32
			c, err := NewClient(ClientOptions{PSK: []byte("password"), Reuse: true, RequireExporter: true, Dialer: d, ExporterFromConn: func(conn net.Conn) ([]byte, error) {
				if conn == nil {
					t.Error("nil session")
				}
				n := exports.Add(1)
				return bytes.Repeat([]byte{byte(n)}, 32), nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err = c.Warm(ctx, count); err != nil {
				t.Fatal(err)
			}
			if int(d.calls.Load()) != count || int(exports.Load()) != count {
				t.Fatal("wrong connection/exporter count")
			}
			for i := 0; i < count; i++ {
				s, found, _ := c.pool.Take()
				if !found {
					t.Fatal("warm session not pooled")
				}
				if s.writer == nil || s.reader == nil || s.identityExporter[0] != byte(i+1) {
					t.Fatal("session state lost")
				}
				conn, dialErr := s.DialConn(M.ParseSocksaddr("example.com:80"))
				if dialErr != nil {
					t.Fatal(dialErr)
				}
				if _, writeErr := conn.Write(nil); writeErr != nil {
					t.Fatal(writeErr)
				}
				s.Close()
			}
		})
	}
}

func TestPreconnectFailuresAndIdleClose(t *testing.T) {
	for _, mode := range []string{"bad-pong", "bad-exporter", "idle-close", "close-pong", "close-write"} {
		t.Run(mode, func(t *testing.T) {
			a, b := net.Pipe()
			defer b.Close()
			d := &warmDialer{dial: func(context.Context) (net.Conn, error) { return a, nil }}
			c, err := NewClient(ClientOptions{PSK: []byte("password"), Reuse: true, RequireExporter: true, Dialer: d, ExporterFromConn: func(net.Conn) ([]byte, error) {
				if mode == "bad-exporter" {
					return []byte{1}, nil
				}
				return make([]byte, 32), nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			entered := make(chan struct{})
			peerDone := make(chan struct{})
			go func() {
				defer close(peerDone)
				if mode == "bad-exporter" || mode == "close-write" {
					close(entered)
					return
				}
				prefix := make([]byte, 56)
				if _, err := io.ReadFull(b, prefix); err != nil {
					return
				}
				r := &reader{upstream: io.MultiReader(bytes.NewReader(prefix[:16]), b), psk: []byte("password")}
				record, err := r.ReadRecord()
				if err != nil {
					return
				}
				record.Release()
				close(entered)
				if mode == "close-pong" {
					io.Copy(io.Discard, b)
					return
				}
				w := &writer{upstream: b, psk: []byte("password")}
				pong := byte(snell.ReplyPong)
				if mode == "bad-pong" {
					pong = 99
				}
				w.Write([]byte{pong})
			}()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			result := make(chan error, 1)
			go func() { result <- c.Warm(ctx, 1) }()
			if mode == "close-pong" || mode == "close-write" {
				<-entered
				if err := c.Close(); err != nil {
					t.Fatal(err)
				}
			}
			warmErr := <-result
			if mode == "idle-close" {
				if warmErr != nil {
					t.Fatal(warmErr)
				}
			} else if warmErr == nil {
				t.Fatal("failed warm succeeded")
			}
			c.Close()
			if _, found, _ := c.pool.Take(); found {
				t.Fatal("failed/closed session pooled")
			}
			b.SetReadDeadline(time.Now().Add(time.Second))
			if _, err := b.Read(make([]byte, 1)); err == nil {
				t.Fatal("transport leaked")
			}
			<-peerDone
		})
	}
}

func TestPreconnectCloseCancelsDial(t *testing.T) {
	entered := make(chan struct{})
	d := &warmDialer{dial: func(ctx context.Context) (net.Conn, error) { close(entered); <-ctx.Done(); return nil, ctx.Err() }}
	c, _ := NewClient(ClientOptions{PSK: []byte("password"), Reuse: true, RequireExporter: true, Dialer: d, ExporterFromConn: func(net.Conn) ([]byte, error) { return make([]byte, 32), nil }})
	done := make(chan error, 1)
	go func() { done <- c.Warm(context.Background(), 2) }()
	<-entered
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("warm goroutine leaked")
	}
	if err := c.Warm(context.Background(), 2); !errors.Is(err, net.ErrClosed) {
		t.Fatal("warm after close", err)
	}
}

func TestPreconnectCancelPongAndInvalid(t *testing.T) {
	for _, count := range []int{-1, 5} {
		c, _ := NewClient(ClientOptions{PSK: []byte("password")})
		if c.Warm(context.Background(), count) == nil {
			t.Fatal("invalid count accepted")
		}
		c.Close()
	}
	a, b := net.Pipe()
	defer b.Close()
	d := &warmDialer{dial: func(context.Context) (net.Conn, error) { return a, nil }}
	c, _ := NewClient(ClientOptions{PSK: []byte("password"), Reuse: true, RequireExporter: true, Dialer: d, ExporterFromConn: func(net.Conn) ([]byte, error) { return make([]byte, 32), nil }})
	defer c.Close()
	go io.Copy(io.Discard, b)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := c.Warm(ctx, 2); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if _, found, _ := c.pool.Take(); found {
		t.Fatal("failed ping pooled")
	}
	if d.calls.Load() != 1 {
		t.Fatal("failure should stop warming")
	}
}
