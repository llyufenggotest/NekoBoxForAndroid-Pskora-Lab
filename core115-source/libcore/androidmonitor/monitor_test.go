package androidmonitor

import (
	"errors"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/common/control"
	"github.com/sagernet/sing/common/logger"
	"testing"
)

type fakePlatform struct {
	starts, closes int
	listeners      map[int64]Listener
	fail           bool
}

func (p *fakePlatform) StartDefaultInterfaceMonitor(l Listener) error {
	p.starts++
	if p.fail {
		return errors.New("register denied")
	}
	if p.listeners == nil {
		p.listeners = map[int64]Listener{}
	}
	p.listeners[l.NetworkMonitorID()] = l
	return nil
}
func (p *fakePlatform) CloseDefaultInterfaceMonitor(l Listener) error {
	p.closes++
	delete(p.listeners, l.NetworkMonitorID())
	return nil
}

type fakeManager struct {
	adapter.NetworkManager
	finder    control.InterfaceFinder
	updates   int
	updateErr error
}

func (m *fakeManager) NetworkInterfaces() []adapter.NetworkInterface { return nil }
func (m *fakeManager) UpdateInterfaces() error                       { m.updates++; return m.updateErr }
func (m *fakeManager) InterfaceFinder() control.InterfaceFinder      { return m.finder }

type fakeFinder struct {
	control.InterfaceFinder
	interfaces map[int]*control.Interface
}

func (f fakeFinder) ByIndex(i int) (*control.Interface, error) {
	v := f.interfaces[i]
	if v == nil {
		return nil, errors.New("missing")
	}
	return v, nil
}
func TestLifecycleAndRealCallbackContract(t *testing.T) {
	p := &fakePlatform{}
	nm := &fakeManager{finder: fakeFinder{interfaces: map[int]*control.Interface{10: {Name: "wlan0", Index: 10}, 11: {Name: "rmnet0", Index: 11}}}}
	create := func() *Monitor { return New(p, func() adapter.NetworkManager { return nm }, logger.NOP()) }
	a, b := create(), create()
	var got []int
	token := a.RegisterCallback(func(i *control.Interface, _ int) {
		if i == nil {
			got = append(got, -1)
		} else {
			got = append(got, i.Index)
		}
	})
	for _, m := range []*Monitor{a, a, b} {
		if err := m.Start(); err != nil {
			t.Fatal(err)
		}
	}
	if p.starts != 2 || a.NetworkMonitorID() == b.NetworkMonitorID() {
		t.Fatal("registration not isolated/idempotent")
	}
	for _, i := range []int32{10, 10, 11, -1} {
		p.listeners[a.NetworkMonitorID()].UpdateDefaultInterface("network", i, false, false)
	}
	if len(got) != 3 || got[0] != 10 || got[1] != 11 || got[2] != -1 {
		t.Fatal(got)
	}
	a.UnregisterCallback(token)
	a.UpdateDefaultInterface("wlan0", 10, false, false)
	if len(got) != 3 {
		t.Fatal("unregister failed")
	}
	a.Close()
	a.Close()
	before := nm.updates
	a.UpdateDefaultInterface("rmnet0", 11, false, false)
	if nm.updates != before || p.closes != 1 || len(p.listeners) != 1 || a.DefaultInterface() != nil {
		t.Fatal("close/lateness isolation failed")
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
}
func TestRegistrationFailureCanRetry(t *testing.T) {
	p := &fakePlatform{fail: true}
	m := New(p, nil, logger.NOP())
	if m.Start() == nil {
		t.Fatal("expected failure")
	}
	m.Close()
	if p.closes != 0 {
		t.Fatal("closed failed registration")
	}
	p.fail = false
	if m.Start() != nil {
		t.Fatal("retry")
	}
	m.Close()
}
