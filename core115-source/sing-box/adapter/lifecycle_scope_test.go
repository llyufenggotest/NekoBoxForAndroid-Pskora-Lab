package adapter

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/sagernet/sing-box/log"
	"github.com/stretchr/testify/require"
)

type scopeLifecycle struct {
	start func(StartStage, *Scope) error
}

func (l *scopeLifecycle) Start(stage StartStage, scope *Scope) error { return l.start(stage, scope) }

func TestScopeChildCleanupReverseOrderAndIdempotent(t *testing.T) {
	var access sync.Mutex
	var order []string
	component := &scopeLifecycle{start: func(stage StartStage, scope *Scope) error {
		if stage == StartStateInitialize {
			scope.Add(func() error { access.Lock(); order = append(order, "resource-1"); access.Unlock(); return nil })
			scope.Add(func() error { access.Lock(); order = append(order, "resource-2"); access.Unlock(); return nil })
		}
		return nil
	}}
	parent := NewScope(context.Background(), log.NewNOPFactory().NewLogger("scope"))
	require.NoError(t, parent.Start("component", component, StartStateInitialize))
	require.NoError(t, parent.Start("component", component, StartStateStart))
	require.NoError(t, parent.Close())
	require.NoError(t, parent.Close())
	require.Equal(t, []string{"resource-2", "resource-1"}, order)
}

func TestScopeFailedProductionStageKeepsChildRollback(t *testing.T) {
	var closed int
	injected := errors.New("injected")
	component := &scopeLifecycle{start: func(stage StartStage, scope *Scope) error {
		scope.Add(func() error { closed++; return nil })
		return injected
	}}
	parent := NewScope(context.Background(), log.NewNOPFactory().NewLogger("scope"))
	require.ErrorIs(t, parent.Start("component", component, StartStateStart), injected)
	require.NoError(t, parent.Close())
	require.Equal(t, 1, closed)
}
