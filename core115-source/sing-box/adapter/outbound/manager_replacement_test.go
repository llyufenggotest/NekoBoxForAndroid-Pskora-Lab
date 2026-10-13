package outbound

import (
	"context"
	"errors"
	"testing"

	"github.com/sagernet/sing-box/adapter"
	endpointmanager "github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/log"
	N "github.com/sagernet/sing/common/network"
	"github.com/stretchr/testify/require"
)

type replacementTestOutbound struct {
	Adapter
	N.Dialer
	closeErr   error
	closeCount int
}

func (o *replacementTestOutbound) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	if stage == adapter.StartStateStart {
		scope.Add(o.Close)
	}
	return nil
}

func (o *replacementTestOutbound) Close() error {
	o.closeCount++
	return o.closeErr
}

func TestCreateReplacementRollsBackNewOutboundWhenOldCloseFails(t *testing.T) {
	injected := errors.New("injected old outbound close failure")
	oldOutbound := &replacementTestOutbound{
		Adapter:  NewAdapter("test", "shared", nil, nil),
		closeErr: injected,
	}
	newOutbound := &replacementTestOutbound{
		Adapter: NewAdapter("test", "shared", nil, []string{"new-dependency"}),
	}
	outbounds := []adapter.Outbound{oldOutbound, newOutbound}
	registry := NewRegistry()
	Register[struct{}](registry, "test", func(context.Context, adapter.Router, log.ContextLogger, string, struct{}) (adapter.Outbound, error) {
		outbound := outbounds[0]
		outbounds = outbounds[1:]
		return outbound, nil
	})
	logger := log.NewNOPFactory().NewLogger("test")
	endpointManager := endpointmanager.NewManager(logger, endpointmanager.NewRegistry())
	manager := NewManager(logger, registry, endpointManager, "shared")
	require.NoError(t, manager.Create(context.Background(), nil, logger, "shared", "test", nil))
	scope := adapter.NewScope(context.Background(), logger)
	for _, stage := range adapter.ListStartStages {
		require.NoError(t, endpointManager.Start(stage, scope))
		require.NoError(t, manager.Start(stage, scope))
	}
	oldOutbound.dependencies = []string{"old-dependency"}
	manager.dependByTag["old-dependency"] = []string{"shared"}

	err := manager.Create(context.Background(), nil, logger, "shared", "test", nil)
	require.ErrorIs(t, err, injected)
	require.Equal(t, 1, oldOutbound.closeCount)
	require.Equal(t, 1, newOutbound.closeCount, "the started replacement must be rolled back immediately")
	outbound, loaded := manager.Outbound("shared")
	require.True(t, loaded)
	require.Same(t, oldOutbound, outbound)
	require.Equal(t, []adapter.Outbound{oldOutbound}, manager.Outbounds())
	require.Same(t, oldOutbound, manager.Default())
	require.Equal(t, []string{"shared"}, manager.dependByTag["old-dependency"])
	require.NotContains(t, manager.dependByTag, "new-dependency")

	require.NoError(t, scope.Close())
	require.Equal(t, 1, oldOutbound.closeCount)
	require.Equal(t, 1, newOutbound.closeCount, "scope shutdown must not close the rolled-back replacement twice")
}
