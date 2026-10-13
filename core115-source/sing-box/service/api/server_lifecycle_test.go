package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/urltest"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json/badoption"
	"github.com/sagernet/sing/service"
	"github.com/stretchr/testify/require"
)

func TestAPIServerProductionFailureReleasesListener(t *testing.T) {
	reservation, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	port := uint16(reservation.Addr().(*net.TCPAddr).Port)
	require.NoError(t, reservation.Close())

	factory := log.NewNOPFactory()
	ctx := service.ContextWith[log.Factory](context.Background(), factory)
	ctx = service.ContextWithPtr(ctx, urltest.NewHistoryStorage())
	created, err := NewService(ctx, factory.NewLogger("api-rollback"), "test", option.APIServiceOptions{
		ListenOptions: option.ListenOptions{
			Listen:     newLoopbackAddr(),
			ListenPort: port,
		},
	})
	require.NoError(t, err)
	apiService := created.(*Service)
	injected := errors.New("injected after API listener start")
	apiService.startFailure = func(point string) error {
		require.Equal(t, "listener", point)
		return injected
	}
	scope := adapter.NewScope(ctx, factory.NewLogger("api-scope"))
	require.ErrorIs(t, scope.Start("api", apiService, adapter.StartStateStarted), injected)
	require.NoError(t, scope.Close())
	require.NoError(t, scope.Close(), "rollback is idempotent")

	rebound, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", reservationPort(port)))
	require.NoError(t, err, "API listener must be released by rollback")
	require.NoError(t, rebound.Close())
}

func newLoopbackAddr() *badoption.Addr {
	var address badoption.Addr
	_ = address.UnmarshalJSON([]byte(`"127.0.0.1"`))
	return &address
}

func reservationPort(port uint16) string {
	return fmt.Sprint(port)
}
