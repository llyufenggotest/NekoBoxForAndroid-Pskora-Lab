//go:build linux || resolved_lifecycle_test

package local

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"

	mDNS "github.com/miekg/dns"
	"github.com/stretchr/testify/require"
)

type resolvedLifecycleTransport struct {
	startErr        error
	startCount      int
	closeCount      int
	exchangeEntered chan struct{}
	exchangeRelease chan struct{}
	async           bool
}

func (t *resolvedLifecycleTransport) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	if stage != adapter.StartStateStart {
		return nil
	}
	t.startCount++
	scope.Add(func() error {
		t.closeCount++
		return nil
	})
	return t.startErr
}

func (*resolvedLifecycleTransport) Type() string           { return "test" }
func (*resolvedLifecycleTransport) Tag() string            { return "" }
func (*resolvedLifecycleTransport) Dependencies() []string { return nil }
func (*resolvedLifecycleTransport) Reset()                 {}
func (t *resolvedLifecycleTransport) Exchange(context.Context, *mDNS.Msg) (*mDNS.Msg, error) {
	if t.exchangeEntered == nil {
		return nil, errors.New("unused")
	}
	close(t.exchangeEntered)
	<-t.exchangeRelease
	return new(mDNS.Msg), nil
}
func (t *resolvedLifecycleTransport) ExchangeAsync(ctx context.Context, message *mDNS.Msg, callback func(*mDNS.Msg, error)) {
	if !t.async {
		return
	}
	go func() {
		response, err := t.Exchange(ctx, message)
		callback(response, err)
	}()
}

func newResolvedLifecycleSet(t *testing.T, transports ...adapter.DNSTransport) *resolvedServerSet {
	t.Helper()
	serverScope := adapter.NewScope(context.Background(), log.NewNOPFactory().NewLogger("resolved-lifecycle"))
	for _, transport := range transports {
		require.NoError(t, startResolvedTransport(serverScope, transport))
	}
	return &resolvedServerSet{serverScope: serverScope}
}

func TestResolvedServerSetLifecycleAdd(t *testing.T) {
	transport := &resolvedLifecycleTransport{}
	serverSet := newResolvedLifecycleSet(t, transport)
	var saved atomic.Pointer[resolvedServerSet]

	require.NoError(t, replaceResolvedServerSet(&saved, serverSet))
	require.Same(t, serverSet, saved.Load())
	require.Equal(t, 1, transport.startCount)
	require.Zero(t, transport.closeCount)

	require.NoError(t, replaceResolvedServerSet(&saved, nil))
	require.Equal(t, 1, transport.closeCount)
}

func TestResolvedServerSetLifecycleReplace(t *testing.T) {
	oldTransport := &resolvedLifecycleTransport{}
	newTransport := &resolvedLifecycleTransport{}
	oldSet := newResolvedLifecycleSet(t, oldTransport)
	newSet := newResolvedLifecycleSet(t, newTransport)
	var saved atomic.Pointer[resolvedServerSet]
	saved.Store(oldSet)

	require.NoError(t, replaceResolvedServerSet(&saved, newSet))
	require.Same(t, newSet, saved.Load())
	require.Equal(t, 1, oldTransport.closeCount)
	require.Zero(t, newTransport.closeCount)

	require.NoError(t, replaceResolvedServerSet(&saved, nil))
	require.Equal(t, 1, newTransport.closeCount)
}

func TestResolvedServerSetLifecycleRemove(t *testing.T) {
	transport := &resolvedLifecycleTransport{}
	serverSet := newResolvedLifecycleSet(t, transport)
	var saved atomic.Pointer[resolvedServerSet]
	saved.Store(serverSet)

	require.NoError(t, replaceResolvedServerSet(&saved, nil))
	require.Nil(t, saved.Load())
	require.Equal(t, 1, transport.closeCount)
	require.NoError(t, replaceResolvedServerSet(&saved, nil))
	require.Equal(t, 1, transport.closeCount)
}

func TestResolvedServerSetLifecycleFailedStartRollsBackChild(t *testing.T) {
	injected := errors.New("injected start failure")
	transport := &resolvedLifecycleTransport{startErr: injected}
	serverScope := adapter.NewScope(context.Background(), log.NewNOPFactory().NewLogger("resolved-lifecycle"))

	require.ErrorIs(t, startResolvedTransport(serverScope, transport), injected)
	require.Equal(t, 1, transport.startCount)
	require.Equal(t, 1, transport.closeCount, "failed child must be removed immediately")
	require.NoError(t, serverScope.Close())
	require.Equal(t, 1, transport.closeCount, "failed child must not remain owned by the parent scope")
}

func TestResolvedExchangeCloseDuringLazyInitialization(t *testing.T) {
	for range 100 {
		created := make(chan *resolvedServerSet, 1)
		release := make(chan struct{})
		resolver := &DBusResolvedResolver{
			ctx:    context.Background(),
			logger: log.NewNOPFactory().NewLogger("resolved-lifecycle"),
		}
		var transport *resolvedLifecycleTransport
		resolver.checkResolvedFunc = func(context.Context) (*resolvedServerSet, error) {
			transport = &resolvedLifecycleTransport{}
			serverSet := newResolvedLifecycleSet(t, transport)
			created <- serverSet
			<-release
			return serverSet, nil
		}

		exchangeDone := make(chan error, 1)
		go func() {
			_, err := resolver.Exchange(context.Background(), new(mDNS.Msg))
			exchangeDone <- err
		}()
		serverSet := <-created
		closeDone := make(chan error, 1)
		go func() { closeDone <- resolver.Close() }()

		select {
		case err := <-closeDone:
			require.NoError(t, err)
		case <-time.After(time.Second):
			t.Fatal("Close blocked behind lazy resolved construction")
		}
		close(release)
		require.ErrorIs(t, <-exchangeDone, os.ErrClosed)
		require.Nil(t, resolver.savedServerSet.Load(), "Close must prevent lazy Exchange publication")
		require.Equal(t, 1, transport.closeCount, "stale lazy server set must be closed immediately")
		require.NoError(t, closeResolvedServerSet(serverSet))
		require.Equal(t, 1, transport.closeCount)
	}
}

func TestResolvedExchangeAfterCloseIsStable(t *testing.T) {
	resolver := &DBusResolvedResolver{ctx: context.Background()}
	require.NoError(t, resolver.Close())
	for range 100 {
		_, err := resolver.Exchange(context.Background(), new(mDNS.Msg))
		require.ErrorIs(t, err, os.ErrClosed)
		require.Nil(t, resolver.savedServerSet.Load())
	}
}

func TestResolvedExchangeLeaseDrainsBeforeClose(t *testing.T) {
	for range 100 {
		entered := make(chan struct{})
		release := make(chan struct{})
		transport := &resolvedLifecycleTransport{exchangeEntered: entered, exchangeRelease: release}
		serverSet := newResolvedLifecycleSet(t, transport)
		serverSet.servers = []resolvedServer{{primaryTransport: transport}}
		resolver := &DBusResolvedResolver{ctx: context.Background()}
		resolver.savedServerSet.Store(serverSet)
		exchangeDone := make(chan error, 1)
		go func() {
			_, err := resolver.Exchange(context.Background(), new(mDNS.Msg))
			exchangeDone <- err
		}()
		<-entered
		closeDone := make(chan error, 1)
		go func() { closeDone <- resolver.Close() }()
		require.Eventually(t, func() bool { return resolver.savedServerSet.Load() == nil }, time.Second, time.Millisecond)
		require.Zero(t, transport.closeCount, "active synchronous exchange transport must remain open")
		select {
		case <-closeDone:
			t.Fatal("Close returned before the synchronous lease drained")
		default:
		}
		close(release)
		require.NoError(t, <-exchangeDone)
		require.NoError(t, <-closeDone)
		require.Equal(t, 1, transport.closeCount)
	}
}

func TestResolvedAsyncExchangeLeaseIncludesCallback(t *testing.T) {
	for range 100 {
		entered := make(chan struct{})
		release := make(chan struct{})
		callbackEntered := make(chan struct{})
		callbackRelease := make(chan struct{})
		transport := &resolvedLifecycleTransport{exchangeEntered: entered, exchangeRelease: release, async: true}
		serverSet := newResolvedLifecycleSet(t, transport)
		serverSet.servers = []resolvedServer{{primaryTransport: transport}}
		resolver := &DBusResolvedResolver{ctx: context.Background()}
		resolver.savedServerSet.Store(serverSet)
		callbackDone := make(chan struct{})
		resolver.ExchangeAsync(context.Background(), new(mDNS.Msg), func(*mDNS.Msg, error) {
			close(callbackEntered)
			<-callbackRelease
			close(callbackDone)
		})
		<-entered
		closeDone := make(chan error, 1)
		go func() { closeDone <- resolver.Close() }()
		require.Eventually(t, func() bool { return resolver.savedServerSet.Load() == nil }, time.Second, time.Millisecond)
		close(release)
		<-callbackEntered
		require.Zero(t, transport.closeCount, "asynchronous lease must include caller callback")
		select {
		case <-closeDone:
			t.Fatal("Close returned before the asynchronous callback completed")
		default:
		}
		close(callbackRelease)
		<-callbackDone
		require.NoError(t, <-closeDone)
		require.Equal(t, 1, transport.closeCount)
	}
}

func TestResolvedUpdateDetachesBeforeWaitingForLease(t *testing.T) {
	for range 100 {
		entered := make(chan struct{})
		release := make(chan struct{})
		oldTransport := &resolvedLifecycleTransport{exchangeEntered: entered, exchangeRelease: release}
		oldSet := newResolvedLifecycleSet(t, oldTransport)
		oldSet.servers = []resolvedServer{{primaryTransport: oldTransport}}
		newSet := newResolvedLifecycleSet(t)
		resolver := &DBusResolvedResolver{ctx: context.Background(), logger: log.NewNOPFactory().NewLogger("resolved-lifecycle")}
		resolver.savedServerSet.Store(oldSet)
		resolver.checkResolvedFunc = func(context.Context) (*resolvedServerSet, error) { return newSet, nil }
		exchangeDone := make(chan error, 1)
		go func() {
			_, err := resolver.Exchange(context.Background(), new(mDNS.Msg))
			exchangeDone <- err
		}()
		<-entered
		updateDone := make(chan struct{})
		go func() { resolver.updateStatus(context.Background()); close(updateDone) }()
		require.Eventually(t, func() bool { return resolver.savedServerSet.Load() == newSet }, time.Second, time.Millisecond)
		lockAcquired := make(chan struct{})
		go func() {
			resolver.updateRunAccess.Lock()
			close(lockAcquired)
			resolver.updateRunAccess.Unlock()
		}()
		select {
		case <-lockAcquired:
		case <-time.After(time.Second):
			t.Fatal("update waits for leases while holding updateRunAccess")
		}
		require.Zero(t, oldTransport.closeCount)
		select {
		case <-updateDone:
			t.Fatal("update returned before old server set lease drained")
		default:
		}
		close(release)
		require.NoError(t, <-exchangeDone)
		<-updateDone
		require.Equal(t, 1, oldTransport.closeCount)
		require.NoError(t, resolver.Close())
	}
}

func TestResolvedCloseContextReturnsButStillReclaims(t *testing.T) {
	transport := &resolvedLifecycleTransport{}
	serverSet := newResolvedLifecycleSet(t, transport)
	lease, acquired := serverSet.acquire()
	require.True(t, acquired)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, closeResolvedServerSetContext(ctx, serverSet), context.Canceled)
	require.Zero(t, transport.closeCount)
	lease.Release()
	require.Eventually(t, func() bool { return transport.closeCount == 1 }, time.Second, time.Millisecond)
}

func TestResolvedLazyExchangeAndUpdateUseGenerationGate(t *testing.T) {
	lazyEntered := make(chan struct{})
	releaseLazy := make(chan struct{})
	updateEntered := make(chan struct{})
	resolver := &DBusResolvedResolver{
		ctx:    context.Background(),
		logger: log.NewNOPFactory().NewLogger("resolved-lifecycle"),
	}
	var callCount atomic.Int32
	var updateSet *resolvedServerSet
	resolver.checkResolvedFunc = func(context.Context) (*resolvedServerSet, error) {
		switch callCount.Add(1) {
		case 1:
			close(lazyEntered)
			<-releaseLazy
			return newResolvedLifecycleSet(t), nil
		case 2:
			updateSet = newResolvedLifecycleSet(t)
			updateSet.servers = []resolvedServer{{primaryTransport: &resolvedLifecycleTransport{}}}
			close(updateEntered)
			return updateSet, nil
		case 3:
			return updateSet, nil
		default:
			t.Fatal("unexpected checkResolved call")
			return nil, nil
		}
	}

	exchangeDone := make(chan error, 1)
	go func() {
		_, err := resolver.Exchange(context.Background(), new(mDNS.Msg))
		exchangeDone <- err
	}()
	<-lazyEntered
	updateDone := make(chan struct{})
	go func() {
		resolver.updateStatus(context.Background())
		close(updateDone)
	}()
	select {
	case <-updateEntered:
	case <-time.After(time.Second):
		t.Fatal("update construction blocked behind lazy Exchange construction")
	}
	<-updateDone
	close(releaseLazy)
	require.Error(t, <-exchangeDone)
	require.Equal(t, int32(3), callCount.Load(), "stale lazy initialization must recheck after the update generation")
	require.NoError(t, resolver.Close())
}
