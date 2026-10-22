//go:build with_gvisor

package tun

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sagernet/gvisor/pkg/tcpip/link/channel"
	"github.com/sagernet/gvisor/pkg/tcpip/stack"
	"github.com/sagernet/sing/common/logger"
)

var mixedFixtureID atomic.Uint64
var errMixedStage = errors.New("mixed injected startup failure")

type mixedTestTun struct {
	Tun
	readStarted chan struct{}
	readDone    chan struct{}
	stop        chan struct{}
	once        sync.Once
	readOnce    sync.Once
	closes      atomic.Int32
	writes      chan []byte
	packets     chan []byte
}

func (t *mixedTestTun) Read(p []byte) (int, error) {
	t.readOnce.Do(func() { close(t.readStarted) })
	select {
	case <-t.stop:
		close(t.readDone)
		return 0, os.ErrClosed
	case packet := <-t.packets:
		return copy(p, packet), nil
	}
}
func (t *mixedTestTun) Write(p []byte) (int, error) {
	t.writes <- append([]byte(nil), p...)
	return len(p), nil
}
func (t *mixedTestTun) WritePacket(p *stack.PacketBuffer) (int, error) {
	v := p.ToView()
	defer v.Release()
	return t.Write(v.AsSlice())
}
func (t *mixedTestTun) NewEndpoint() (stack.LinkEndpoint, stack.NICOptions, error) {
	panic("Mixed must not create host endpoint")
}
func (t *mixedTestTun) Close() error {
	t.closes.Add(1)
	t.once.Do(func() { close(t.stop) })
	return nil
}

type mixedTestListener struct {
	net.Listener
	entered     chan struct{}
	exited      chan struct{}
	once        sync.Once
	closes      atomic.Int32
	closeErr    error
	beforeClose func()
}

func (l *mixedTestListener) Accept() (net.Conn, error) {
	l.once.Do(func() { close(l.entered) })
	c, e := l.Listener.Accept()
	if e != nil {
		close(l.exited)
	}
	return c, e
}
func (l *mixedTestListener) Close() error {
	l.closes.Add(1)
	if l.beforeClose != nil {
		l.beforeClose()
	}
	e := l.Listener.Close()
	if l.closeErr != nil {
		return l.closeErr
	}
	return e
}

type mixedFixture struct {
	t             *testing.T
	m             *Mixed
	tun           *mixedTestTun
	listeners     []*mixedTestListener
	label         string
	firewallCalls int
	listenCalls   []string
	stage         string
	ipStack       *stack.Stack
	forwarder     *UDPForwarder
	endpoint      *channel.Endpoint
}

func newMixedFixture(t *testing.T, stage string) *mixedFixture {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	tun := &mixedTestTun{readStarted: make(chan struct{}), readDone: make(chan struct{}), stop: make(chan struct{}), writes: make(chan []byte, 4), packets: make(chan []byte, 4)}
	raw, e := NewMixed(StackOptions{Context: ctx, Tun: tun, Logger: logger.NOP(), UDPTimeout: time.Hour,
		TunOptions: Options{MTU: 1500, Inet4Address: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/8")}, Inet6Address: []netip.Prefix{netip.MustParsePrefix("::1/64")}}})
	if e != nil {
		t.Fatal(e)
	}
	f := &mixedFixture{t: t, m: raw.(*Mixed), tun: tun, label: fmt.Sprintf("mixed-%d", mixedFixtureID.Add(1)), stage: stage}
	// No host TUN, route, bind control or firewall implementation is reachable.
	f.m.factories = &mixedStartFactories{
		system: &systemStartFactories{
			fixFirewall: func() error { f.firewallCalls++; return nil },
			listen: func(ctx context.Context, ns string, c net.ListenConfig, network, address string) (net.Listener, error) {
				f.listenCalls = append(f.listenCalls, network)
				if ns != "" || c.Control != nil {
					t.Fatal("unexpected host namespace/bind control")
				}
				if stage == "listen4" || (stage == "listen6" && network == "tcp6") {
					return nil, errMixedStage
				}
				// The IPv6 production branch is exercised using a second IPv4 loopback
				// listener so the fixture does not depend on host IPv6 configuration.
				l, e := net.Listen("tcp4", "127.0.0.1:0")
				if e != nil {
					return nil, e
				}
				tracked := &mixedTestListener{Listener: l, entered: make(chan struct{}), exited: make(chan struct{})}
				f.listeners = append(f.listeners, tracked)
				return tracked, nil
			},
			newUDPNat: func(o UDPNatOptions) *UDPNat {
				n := NewUDPNat(o)
				if stage == "system_udp" {
					n.Close()
				}
				return n
			},
		},
		newStack: func(ep stack.LinkEndpoint) (*stack.Stack, error) {
			f.endpoint = ep.(*channel.Endpoint)
			f.awaitSystemReady()
			if stage == "gvisor" {
				return nil, errMixedStage
			}
			s, e := NewGVisorStack(ep)
			f.ipStack = s
			return s, e
		},
		startUDPForwarder: func(u *UDPForwarder) error { f.forwarder = u; return u.Start() },
	}
	t.Cleanup(func() {
		// Safety net is deliberately after assertions; never used to make them pass.
		if f.forwarder != nil {
			f.forwarder.Close()
		}
		if f.ipStack != nil {
			f.ipStack.Close()
			f.ipStack.Wait()
		}
		f.m.System.Close()
		for _, l := range f.listeners {
			l.Listener.Close()
		}
		tun.Close()
		cancel()
	})
	return f
}
func mixedAwait(t *testing.T, c <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-c:
	case <-time.After(time.Second):
		t.Fatalf("timeout: %s", what)
	}
}
func (f *mixedFixture) workerCount(function string) int {
	var b bytes.Buffer
	pprof.Lookup("goroutine").WriteTo(&b, 1)
	count := 0
	for _, block := range strings.Split(b.String(), "\n\n") {
		if !strings.Contains(block, `"mixed-fixture":"`+f.label+`"`) || !strings.Contains(block, function) {
			continue
		}
		for _, line := range strings.Split(block, "\n") {
			fields := strings.Fields(line)
			if len(fields) > 1 && fields[1] == "@" {
				n, e := strconv.Atoi(fields[0])
				if e == nil {
					count += n
				}
				break
			}
		}
	}
	return count
}
func (f *mixedFixture) awaitWorkers(function string, want int) {
	f.t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		n := f.workerCount(function)
		if n == want {
			return
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("owned %s workers=%d want=%d", function, n, want)
		}
		time.Sleep(time.Millisecond)
	}
}
func (f *mixedFixture) awaitSystemReady() {
	for _, l := range f.listeners {
		mixedAwait(f.t, l.entered, "real accept worker readiness")
	}
	f.awaitWorkers("(*UDPNat).cleanupLoop", 1)
}
func (f *mixedFixture) start() error {
	var e error
	pprof.Do(f.m.ctx, pprof.Labels("mixed-fixture", f.label), func(context.Context) { e = f.m.Start() })
	return e
}
func (f *mixedFixture) assertSystemClosed() {
	f.t.Helper()
	if f.m.udpNat != nil && f.m.udpNat.state.Load() != udpNatStateClosed {
		f.t.Error("System UDP cleanup still live after failed Start")
	}
	f.awaitWorkers("(*UDPNat).cleanupLoop", 0)
	for _, l := range f.listeners {
		if l.closes.Load() != 1 {
			f.t.Errorf("listener closes=%d want=1", l.closes.Load())
			continue
		}
		mixedAwait(f.t, l.exited, "real accept worker retirement")
	}
	if f.tun.closes.Load() != 0 {
		f.t.Error("Mixed stole externally owned TUN")
	}
	if f.m.ctx.Err() != nil {
		f.t.Error("Mixed canceled parent context")
	}
}
func (f *mixedFixture) assertStackClosed() {
	f.t.Helper()
	// Stack.Wait joins actual gVisor transport workers; it is not a mocked Close.
	done := make(chan struct{})
	go func() { f.ipStack.Wait(); close(done) }()
	mixedAwait(f.t, done, "gVisor worker retirement")
	if f.m.endpoint != nil && f.m.endpoint.IsAttached() {
		f.t.Error("gVisor endpoint still attached")
	}
}
func TestMixedStartForwarderFailureOwnsStack(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(fmt.Sprintf("started=%v", partial), func(t *testing.T) {
			f := newMixedFixture(t, "forwarder")
			f.m.factories.startUDPForwarder = func(u *UDPForwarder) error {
				f.forwarder = u
				if partial {
					if e := u.Start(); e != nil {
						return e
					}
					f.awaitWorkers("(*UDPNat).cleanupLoop", 2)
					return errMixedStage
				}
				u.Close() // force the real UDPForwarder.Start -> UDPNat.Start failure boundary
				return u.Start()
			}
			e := f.start()
			if partial && !errors.Is(e, errMixedStage) || !partial && !errors.Is(e, os.ErrClosed) {
				t.Fatalf("Start error=%v", e)
			}
			if f.ipStack == nil || f.forwarder == nil {
				t.Fatal("did not reach real gVisor/UDP forwarder")
			}
			// Check stack before System so a nil-stack-only fix cannot hide leaked gVisor workers.
			f.assertStackClosed()
			f.assertSystemClosed()
			if f.forwarder.udpNat.state.Load() != udpNatStateClosed {
				t.Error("partial forwarder still live")
			}
			if e := f.m.Close(); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestMixedCloseWakesOwnedPacketLoop(t *testing.T) {
	f := newMixedFixture(t, "")
	if e := f.start(); e != nil {
		t.Fatal(e)
	}
	mixedAwait(t, f.tun.readStarted, "real Mixed tunLoop readiness")
	f.awaitWorkers("(*Mixed).packetLoop", 1)
	f.awaitWorkers("(*UDPNat).cleanupLoop", 2)
	if e := f.m.Close(); e != nil {
		t.Fatal(e)
	}
	f.assertStackClosed()
	f.assertSystemClosed()
	// Context is still live: only closing our channel endpoint can retire this worker.
	f.awaitWorkers("(*Mixed).packetLoop", 0)
	if f.workerCount("(*Mixed).tunLoop") != 1 {
		t.Error("externally owned TUN loop ended prematurely")
	}
	f.tun.Close()
	mixedAwait(t, f.tun.readDone, "owner closes TUN")
	f.awaitWorkers("(*Mixed).tunLoop", 0)
}

func TestMixedStartFailureClosesUnattachedEndpoint(t *testing.T) {
	f := newMixedFixture(t, "gvisor")
	if e := f.start(); !errors.Is(e, errMixedStage) {
		t.Fatal(e)
	}
	done := make(chan struct{})
	go func() { f.endpoint.ReadContext(f.m.ctx); close(done) }()
	mixedAwait(t, done, "unattached channel endpoint released by rollback")
}

func TestMixedSystemStartFailureStages(t *testing.T) {
	for _, stage := range []string{"listen4", "listen6", "system_udp"} {
		t.Run(stage, func(t *testing.T) {
			f := newMixedFixture(t, stage)
			e := f.start()
			want := errMixedStage
			if stage == "system_udp" {
				want = os.ErrClosed
			}
			if !errors.Is(e, want) {
				t.Fatalf("Start=%v want=%v", e, want)
			}
			if f.firewallCalls != 1 || f.ipStack != nil || f.forwarder != nil {
				t.Fatal("wrong failure boundary")
			}
			f.assertSystemClosed()
			if e := f.m.Close(); e != nil {
				t.Fatal(e)
			}
			f.assertSystemClosed()
		})
	}
}
func TestMixedCloseErrorPreservation(t *testing.T) {
	for _, stage := range []string{"", "gvisor", "listen6"} {
		t.Run("stage="+stage, func(t *testing.T) {
			f := newMixedFixture(t, stage)
			closeErr := errors.New("loopback close sentinel")
			listen := f.m.factories.system.listen
			f.m.factories.system.listen = func(ctx context.Context, ns string, c net.ListenConfig, n, a string) (net.Listener, error) {
				l, e := listen(ctx, ns, c, n, a)
				if l != nil {
					tracked := l.(*mixedTestListener)
					tracked.closeErr = closeErr
					tracked.beforeClose = func() {
						if f.forwarder != nil && f.forwarder.udpNat.state.Load() != udpNatStateClosed {
							t.Error("System closed before forwarder")
						}
						if f.endpoint != nil && f.endpoint.IsAttached() {
							t.Error("System closed before gVisor detach")
						}
					}
				}
				return l, e
			}
			e := f.start()
			if stage == "" {
				if e != nil {
					t.Fatal(e)
				}
				f.awaitWorkers("(*UDPNat).cleanupLoop", 2)
			} else if !errors.Is(e, errMixedStage) || !errors.Is(e, closeErr) {
				t.Fatalf("rollback must retain both errors: %v", e)
			}
			for i := 0; i < 3; i++ {
				if e = f.m.Close(); !errors.Is(e, closeErr) {
					t.Fatalf("Close #%d lost error: %v", i, e)
				}
			}
			f.assertSystemClosed()
		})
	}
}
func TestMixedClosePartialAndUnstarted(t *testing.T) {
	t.Run("unstarted", func(t *testing.T) {
		f := newMixedFixture(t, "")
		for i := 0; i < 3; i++ {
			if e := f.m.Close(); e != nil {
				t.Fatal(e)
			}
		}
		if f.firewallCalls != 0 || len(f.listeners) != 0 {
			t.Fatal("Close started host resources")
		}
	})
	t.Run("partial", func(t *testing.T) {
		f := newMixedFixture(t, "")
		pprof.Do(f.m.ctx, pprof.Labels("mixed-fixture", f.label), func(context.Context) {
			if e := f.m.System.startWithFactories(f.m.factories.system); e != nil {
				t.Fatal(e)
			}
		})
		f.awaitSystemReady()
		if f.m.stack != nil {
			t.Fatal("fixture must retain nil gVisor")
		}
		for i := 0; i < 3; i++ {
			if e := f.m.Close(); e != nil {
				t.Fatal(e)
			}
		}
		f.assertSystemClosed()
	})
}

func TestMixedStartConstructorReturnsOwnedStackAndError(t *testing.T) {
	f := newMixedFixture(t, "")
	create := f.m.factories.newStack
	f.m.factories.newStack = func(ep stack.LinkEndpoint) (*stack.Stack, error) {
		s, e := create(ep)
		if e != nil {
			return s, e
		}
		return s, errMixedStage
	}
	if e := f.start(); !errors.Is(e, errMixedStage) {
		t.Fatal(e)
	}
	f.assertStackClosed()
	f.assertSystemClosed()
	if f.forwarder != nil {
		t.Fatal("forwarder created after constructor failure")
	}
}

func TestMixedStartGVisorFailureRollback(t *testing.T) {
	f := newMixedFixture(t, "gvisor")
	if e := f.start(); !errors.Is(e, errMixedStage) {
		t.Fatalf("Start error=%v", e)
	}
	if f.firewallCalls != 1 || len(f.listenCalls) != 2 {
		t.Fatalf("host substitute coverage firewall=%d listens=%v", f.firewallCalls, f.listenCalls)
	}
	f.assertSystemClosed()
	if e := f.m.Close(); e != nil {
		t.Fatal(e)
	}
	f.assertSystemClosed()
}
