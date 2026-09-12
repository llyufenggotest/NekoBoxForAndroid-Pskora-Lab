package dialer

import (
	"context"
	"github.com/sagernet/sing-box/adapter"
	tun "github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common/control"
	"github.com/sagernet/sing/common/x/list"
	"testing"
	"time"
)

type readinessMonitor struct {
	tun.DefaultInterfaceMonitor
	current *control.Interface
	cb      tun.DefaultInterfaceUpdateCallback
}

func (m *readinessMonitor) DefaultInterface() *control.Interface { return m.current }
func (m *readinessMonitor) RegisterCallback(c tun.DefaultInterfaceUpdateCallback) *list.Element[tun.DefaultInterfaceUpdateCallback] {
	m.cb = c
	c(&control.Interface{Index: 1, Name: "physical"}, 0)
	return nil
}
func (m *readinessMonitor) UnregisterCallback(*list.Element[tun.DefaultInterfaceUpdateCallback]) {}

type readinessManager struct {
	adapter.NetworkManager
	m *readinessMonitor
}

func (m readinessManager) InterfaceMonitor() tun.DefaultInterfaceMonitor { return m.m }
func TestWaitInterfaceDelayedNotification(t *testing.T) {
	m := &readinessMonitor{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Cancellation bounds a genuinely offline wait; no sleeps masquerading as readiness.
	cancel()
	if waitInterface(ctx, readinessManager{m: m}) == nil {
		t.Fatal("offline was ready")
	}
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := waitInterface(ctx, readinessManager{m: m}); err != nil {
		t.Fatal("notification did not release wait:", err)
	}
}
