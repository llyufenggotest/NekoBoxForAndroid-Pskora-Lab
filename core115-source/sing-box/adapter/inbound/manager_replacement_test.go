package inbound

import (
	"context"
	"errors"
	"testing"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/stretchr/testify/require"
)

type replacementTestInbound struct {
	Adapter
	closeErr   error
	closeCount int
}

func (i *replacementTestInbound) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	if stage == adapter.StartStateStart {
		scope.Add(i.Close)
	}
	return nil
}

func (i *replacementTestInbound) Close() error {
	i.closeCount++
	return i.closeErr
}

func TestCreateReplacementRollsBackNewInboundWhenOldCloseFails(t *testing.T) {
	injected := errors.New("injected old inbound close failure")
	oldInbound := &replacementTestInbound{Adapter: NewAdapter("test", "shared"), closeErr: injected}
	newInbound := &replacementTestInbound{Adapter: NewAdapter("test", "shared")}
	inbounds := []adapter.Inbound{oldInbound, newInbound}
	registry := NewRegistry()
	Register[struct{}](registry, "test", func(context.Context, adapter.Router, log.ContextLogger, string, struct{}) (adapter.Inbound, error) {
		inbound := inbounds[0]
		inbounds = inbounds[1:]
		return inbound, nil
	})
	logger := log.NewNOPFactory().NewLogger("test")
	manager := NewManager(logger, registry, nil)
	require.NoError(t, manager.Create(context.Background(), nil, logger, "shared", "test", nil))
	scope := adapter.NewScope(context.Background(), logger)
	for _, stage := range adapter.ListStartStages {
		require.NoError(t, manager.Start(stage, scope))
	}

	err := manager.Create(context.Background(), nil, logger, "shared", "test", nil)
	require.ErrorIs(t, err, injected)
	require.Equal(t, 1, oldInbound.closeCount)
	require.Equal(t, 1, newInbound.closeCount, "the started replacement must be rolled back immediately")
	inbound, loaded := manager.Get("shared")
	require.True(t, loaded)
	require.Same(t, oldInbound, inbound)
	require.Equal(t, []adapter.Inbound{oldInbound}, manager.Inbounds())

	require.NoError(t, scope.Close())
	require.Equal(t, 1, oldInbound.closeCount)
	require.Equal(t, 1, newInbound.closeCount, "scope shutdown must not close the rolled-back replacement twice")
}
