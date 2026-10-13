package http

import (
	"context"
	"errors"
	"testing"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	sHTTP "github.com/sagernet/sing/protocol/http"
	"github.com/stretchr/testify/require"
)

// TestOutboundProductionRollbackAfterClientRegistration drives the production
// Outbound.Start path, then injects a later component failure through Scope.
func TestOutboundProductionRollbackAfterClientRegistration(t *testing.T) {
	outbound := &Outbound{client: newCloseableClient(sHTTP.Options{})}
	original := outbound.client.Client
	scope := adapter.NewScope(context.Background(), log.NewNOPFactory().NewLogger("http-rollback"))
	require.NoError(t, scope.Start("http", outbound, adapter.StartStateInitialize))
	failure := &productionFailureLifecycle{err: errors.New("injected after HTTP client start")}
	require.Error(t, scope.Start("later", failure, adapter.StartStateInitialize))
	require.NoError(t, scope.Close())
	require.NotSame(t, original, outbound.client.Client)
}

type productionFailureLifecycle struct{ err error }

func (f *productionFailureLifecycle) Start(adapter.StartStage, *adapter.Scope) error { return f.err }
