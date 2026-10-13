package route

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing/common/control"
	"github.com/sagernet/sing/common/x/list"
	"github.com/stretchr/testify/require"
)

type rollbackInterfaceMonitor struct {
	access    sync.Mutex
	started   bool
	closed    bool
	callbacks list.List[func(*control.Interface, int)]
}

func (m *rollbackInterfaceMonitor) Start() error {
	m.access.Lock()
	m.started = true
	m.access.Unlock()
	return nil
}
func (m *rollbackInterfaceMonitor) Close() error {
	m.access.Lock()
	m.closed = true
	m.access.Unlock()
	return nil
}
func (m *rollbackInterfaceMonitor) DefaultInterface() *control.Interface { return nil }
func (m *rollbackInterfaceMonitor) OverrideAndroidVPN() bool             { return false }
func (m *rollbackInterfaceMonitor) AndroidVPNEnabled() bool              { return false }
func (m *rollbackInterfaceMonitor) RegisterCallback(callback func(*control.Interface, int)) *list.Element[func(*control.Interface, int)] {
	m.access.Lock()
	element := m.callbacks.PushBack(callback)
	m.access.Unlock()
	return element
}
func (m *rollbackInterfaceMonitor) UnregisterCallback(element *list.Element[func(*control.Interface, int)]) {
	m.access.Lock()
	m.callbacks.Remove(element)
	m.access.Unlock()
}
func (m *rollbackInterfaceMonitor) RegisterMyInterface(string) {}
func (m *rollbackInterfaceMonitor) MyInterfaces() []string     { return nil }

func TestNetworkProductionFailureRollsBackStartedMonitor(t *testing.T) {
	monitor := &rollbackInterfaceMonitor{}
	injected := errors.New("injected after interface monitor start")
	network := &NetworkManager{
		ctx:              context.Background(),
		logger:           log.NewNOPFactory().NewLogger("network-rollback"),
		interfaceMonitor: monitor,
		startFailure: func(point string) error {
			require.Equal(t, "interface-monitor-callback", point)
			return injected
		},
	}
	scope := adapter.NewScope(context.Background(), log.NewNOPFactory().NewLogger("network-scope"))
	require.ErrorIs(t, scope.Start("network", network, adapter.StartStateInitialize), injected)
	require.True(t, monitor.started, "production monitor Start must run before injection")
	require.False(t, monitor.closed)
	monitor.access.Lock()
	require.Equal(t, 1, monitor.callbacks.Len(), "production callback registration must run before injection")
	monitor.access.Unlock()
	require.NoError(t, scope.Close())
	require.NoError(t, scope.Close(), "rollback is idempotent")
	monitor.access.Lock()
	defer monitor.access.Unlock()
	require.True(t, monitor.closed, "started interface monitor must close on later failure")
	require.Zero(t, monitor.callbacks.Len(), "registered callback must be removed during rollback")
}
