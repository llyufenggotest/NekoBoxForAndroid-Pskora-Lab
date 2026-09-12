package dialer

import (
	"context"
	"errors"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/common/control"
	"time"
)

// ErrNoAvailableInterface is local readiness, not evidence about a remote node.
var ErrNoAvailableInterface = errors.New("no available network interface")

func waitInterface(ctx context.Context, manager adapter.NetworkManager) error {
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrNoAvailableInterface, err)
	}
	monitor := manager.InterfaceMonitor()
	if monitor == nil || monitor.DefaultInterface() != nil {
		return nil
	}
	ready := make(chan struct{}, 1)
	token := monitor.RegisterCallback(func(i *control.Interface, _ int) {
		if i != nil {
			select {
			case ready <- struct{}{}:
			default:
			}
		}
	})
	defer monitor.UnregisterCallback(token)
	// Register then recheck to close the snapshot/notification race.
	if monitor.DefaultInterface() != nil {
		return nil
	}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-ready:
		return nil
	case <-ctx.Done():
		return errors.Join(ErrNoAvailableInterface, ctx.Err())
	case <-timer.C:
		return ErrNoAvailableInterface
	}
}
