package androidmonitor

import (
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/common/control"
	"github.com/sagernet/sing/common/logger"
	"testing"
	"time"
)

type synchronousPlatform struct{ fakePlatform }

func (p *synchronousPlatform) StartDefaultInterfaceMonitor(l Listener) error {
	if err := p.fakePlatform.StartDefaultInterfaceMonitor(l); err != nil {
		return err
	}
	l.UpdateDefaultInterface("wlan0", 10, false, false)
	return nil
}
func TestSynchronousStartSnapshotDoesNotDeadlock(t *testing.T) {
	p := &synchronousPlatform{}
	nm := &fakeManager{finder: fakeFinder{interfaces: map[int]*control.Interface{10: {Name: "wlan0", Index: 10}}}}
	m := New(p, func() adapter.NetworkManager { return nm }, logger.NOP())
	callback := make(chan struct{}, 1)
	m.RegisterCallback(func(i *control.Interface, _ int) {
		if m.DefaultInterface() == nil {
			t.Error("snapshot not published before callback")
		}
		callback <- struct{}{}
	})
	done := make(chan error, 1)
	go func() { done <- m.Start() }()
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("synchronous callback deadlocked Start")
	}
	select {
	case <-callback:
	default:
		t.Fatal("Start returned before synchronous snapshot")
	}
	if m.DefaultInterface() == nil {
		t.Fatal("snapshot lost")
	}
	m.Close()
}
