//go:build linux || resolved_lifecycle_test

package local

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"

	mDNS "github.com/miekg/dns"
	"github.com/stretchr/testify/require"
)

type resolvedLifecycleTransport struct {
	startErr   error
	startCount int
	closeCount int
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
func (*resolvedLifecycleTransport) Exchange(context.Context, *mDNS.Msg) (*mDNS.Msg, error) {
	return nil, errors.New("unused")
}
func (*resolvedLifecycleTransport) ExchangeAsync(context.Context, *mDNS.Msg, func(*mDNS.Msg, error)) {
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
