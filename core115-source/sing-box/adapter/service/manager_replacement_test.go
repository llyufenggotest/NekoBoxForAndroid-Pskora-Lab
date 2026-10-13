package service

import (
	"context"
	"errors"
	"testing"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/stretchr/testify/require"
)

type replacementTestService struct {
	Adapter
	closeErr   error
	closeCount int
}

func (s *replacementTestService) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	if stage == adapter.StartStateStart {
		scope.Add(s.Close)
	}
	return nil
}

func (s *replacementTestService) Close() error {
	s.closeCount++
	return s.closeErr
}

func TestCreateReplacementRollsBackNewServiceWhenOldCloseFails(t *testing.T) {
	injected := errors.New("injected old service close failure")
	oldService := &replacementTestService{Adapter: NewAdapter("test", "shared"), closeErr: injected}
	newService := &replacementTestService{Adapter: NewAdapter("test", "shared")}
	services := []adapter.Service{oldService, newService}
	registry := NewRegistry()
	Register[struct{}](registry, "test", func(context.Context, log.ContextLogger, string, struct{}) (adapter.Service, error) {
		service := services[0]
		services = services[1:]
		return service, nil
	})
	logger := log.NewNOPFactory().NewLogger("test")
	manager := NewManager(logger, registry)
	require.NoError(t, manager.Create(context.Background(), logger, "shared", "test", nil))
	scope := adapter.NewScope(context.Background(), logger)
	for _, stage := range adapter.ListStartStages {
		require.NoError(t, manager.Start(stage, scope))
	}

	err := manager.Create(context.Background(), logger, "shared", "test", nil)
	require.ErrorIs(t, err, injected)
	require.Equal(t, 1, oldService.closeCount)
	require.Equal(t, 1, newService.closeCount, "the started replacement must be rolled back immediately")
	service, loaded := manager.Get("shared")
	require.True(t, loaded)
	require.Same(t, oldService, service)
	require.Equal(t, []adapter.Service{oldService}, manager.Services())

	require.NoError(t, scope.Close())
	require.Equal(t, 1, oldService.closeCount)
	require.Equal(t, 1, newService.closeCount, "scope shutdown must not close the rolled-back replacement twice")
}
