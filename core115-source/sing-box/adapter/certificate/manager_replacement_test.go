package certificate

import (
	"context"
	"crypto/tls"
	"errors"
	"testing"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/stretchr/testify/require"
)

type replacementTestProvider struct {
	Adapter
	closeErr   error
	closeCount int
}

func (p *replacementTestProvider) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	if stage == adapter.StartStateStart {
		scope.Add(p.Close)
	}
	return nil
}

func (p *replacementTestProvider) Close() error {
	p.closeCount++
	return p.closeErr
}

func (p *replacementTestProvider) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return nil, nil
}

func TestCreateReplacementRollsBackNewProviderWhenOldCloseFails(t *testing.T) {
	injected := errors.New("injected old provider close failure")
	oldProvider := &replacementTestProvider{Adapter: NewAdapter("test", "shared"), closeErr: injected}
	newProvider := &replacementTestProvider{Adapter: NewAdapter("test", "shared")}
	providers := []adapter.CertificateProviderService{oldProvider, newProvider}
	registry := NewRegistry()
	Register[struct{}](registry, "test", func(context.Context, log.ContextLogger, string, struct{}) (adapter.CertificateProviderService, error) {
		provider := providers[0]
		providers = providers[1:]
		return provider, nil
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
	require.Equal(t, 1, oldProvider.closeCount)
	require.Equal(t, 1, newProvider.closeCount, "the started replacement must be rolled back immediately")
	provider, loaded := manager.Get("shared")
	require.True(t, loaded)
	require.Same(t, oldProvider, provider)
	require.Equal(t, []adapter.CertificateProviderService{oldProvider}, manager.CertificateProviders())

	require.NoError(t, scope.Close())
	require.Equal(t, 1, oldProvider.closeCount)
	require.Equal(t, 1, newProvider.closeCount, "scope shutdown must not close the rolled-back replacement twice")
}
