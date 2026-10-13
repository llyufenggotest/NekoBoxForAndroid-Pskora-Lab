package endpoint

import (
	"context"
	"errors"
	"testing"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	N "github.com/sagernet/sing/common/network"
	"github.com/stretchr/testify/require"
)

type replacementTestEndpoint struct {
	Adapter
	N.Dialer
	closeErr   error
	closeCount int
}

func (e *replacementTestEndpoint) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	if stage == adapter.StartStateInitialize {
		scope.Add(e.Close)
	}
	return nil
}

func (e *replacementTestEndpoint) Close() error {
	e.closeCount++
	return e.closeErr
}

func TestCreateReplacementRollsBackNewEndpointWhenOldCloseFails(t *testing.T) {
	injected := errors.New("injected old endpoint close failure")
	oldEndpoint := &replacementTestEndpoint{Adapter: NewAdapter("test", "shared", nil, nil), closeErr: injected}
	newEndpoint := &replacementTestEndpoint{Adapter: NewAdapter("test", "shared", nil, nil)}
	endpoints := []adapter.Endpoint{oldEndpoint, newEndpoint}
	registry := NewRegistry()
	Register[struct{}](registry, "test", func(context.Context, adapter.Router, log.ContextLogger, string, struct{}) (adapter.Endpoint, error) {
		endpoint := endpoints[0]
		endpoints = endpoints[1:]
		return endpoint, nil
	})
	logger := log.NewNOPFactory().NewLogger("test")
	manager := NewManager(logger, registry)
	require.NoError(t, manager.Create(context.Background(), nil, logger, "shared", "test", nil))
	scope := adapter.NewScope(context.Background(), logger)
	for _, stage := range adapter.ListStartStages {
		require.NoError(t, manager.Start(stage, scope))
	}

	err := manager.Create(context.Background(), nil, logger, "shared", "test", nil)
	require.ErrorIs(t, err, injected)
	require.Equal(t, 1, oldEndpoint.closeCount)
	require.Equal(t, 1, newEndpoint.closeCount, "the started replacement must be rolled back immediately")
	endpoint, loaded := manager.Get("shared")
	require.True(t, loaded)
	require.Same(t, oldEndpoint, endpoint)
	require.Equal(t, []adapter.Endpoint{oldEndpoint}, manager.Endpoints())

	require.NoError(t, scope.Close())
	require.Equal(t, 1, oldEndpoint.closeCount)
	require.Equal(t, 1, newEndpoint.closeCount, "scope shutdown must not close the rolled-back replacement twice")
}
