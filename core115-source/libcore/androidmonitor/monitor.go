// Package androidmonitor implements sing-tun's monitor through the Android Java bridge.
package androidmonitor

import (
	"encoding/json"
	"fmt"
	"github.com/sagernet/sing-box/adapter"
	tun "github.com/sagernet/sing-tun"
	D "github.com/sagernet/sing-tun/diagnostic"
	"github.com/sagernet/sing/common/control"
	"github.com/sagernet/sing/common/logger"
	"github.com/sagernet/sing/common/x/list"
	"sort"
	"sync"
	"sync/atomic"
)

type Listener interface {
	NetworkMonitorID() int64
	UpdateDefaultInterface(name string, index int32, expensive bool, constrained bool)
}
type Platform interface {
	StartDefaultInterfaceMonitor(Listener) error
	CloseDefaultInterfaceMonitor(Listener) error
}
type Monitor struct {
	id            int64
	platform      Platform
	manager       func() adapter.NetworkManager
	logger        logger.Logger
	lifecycle     sync.Mutex
	mu            sync.Mutex
	started       bool
	generation    uint64
	eventAccess   sync.Mutex
	snapshotKey   string
	hasSnapshot   bool
	offlineSeen   bool
	recoveryEpoch uint64
	current       *control.Interface
	callbacks     list.List[tun.DefaultInterfaceUpdateCallback]
	myInterfaces  []string
}

var _ tun.DefaultInterfaceMonitor = (*Monitor)(nil)
var nextID atomic.Int64

func (m *Monitor) NetworkMonitorID() int64 { return m.id }
func New(p Platform, manager func() adapter.NetworkManager, l logger.Logger) *Monitor {
	return &Monitor{id: nextID.Add(1), platform: p, manager: manager, logger: l}
}
func (m *Monitor) Start() error {
	m.lifecycle.Lock()
	defer m.lifecycle.Unlock()
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return nil
	}
	m.started = true
	m.generation++
	m.hasSnapshot = false
	m.offlineSeen = false
	m.mu.Unlock()
	if err := m.platform.StartDefaultInterfaceMonitor(m); err != nil {
		m.mu.Lock()
		m.started = false
		m.mu.Unlock()
		return err
	}
	return nil
}
func (m *Monitor) Close() error {
	m.lifecycle.Lock()
	defer m.lifecycle.Unlock()
	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return nil
	}
	m.started = false
	m.current = nil
	m.mu.Unlock()
	return m.platform.CloseDefaultInterfaceMonitor(m)
}
func (m *Monitor) DefaultInterface() *control.Interface {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.current
}
func (m *Monitor) OverrideAndroidVPN() bool { return false }
func (m *Monitor) AndroidVPNEnabled() bool  { return false }
func (m *Monitor) RegisterCallback(c tun.DefaultInterfaceUpdateCallback) *list.Element[tun.DefaultInterfaceUpdateCallback] {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callbacks.PushBack(c)
}
func (m *Monitor) UnregisterCallback(e *list.Element[tun.DefaultInterfaceUpdateCallback]) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callbacks.Remove(e)
}
func (m *Monitor) RegisterMyInterface(n string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.myInterfaces = append(m.myInterfaces, n)
}
func (m *Monitor) MyInterfaces() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.myInterfaces...)
}
func (m *Monitor) UpdateDefaultInterface(name string, index int32, expensive, constrained bool) {
	m.eventAccess.Lock()
	defer m.eventAccess.Unlock()
	m.mu.Lock()
	active := m.started
	generation := m.generation
	m.mu.Unlock()
	if !active {
		return
	}
	nm := m.manager()
	if nm == nil {
		m.logger.Error("Android network manager not initialized")
		return
	}
	isOfflineEvent := index <= 0 || name == ""
	if err := nm.UpdateInterfaces(); err != nil {
		m.logger.Error("Android update interfaces: ", err)
		if !isOfflineEvent {
			return
		}
	}
	var next *control.Interface
	if index > 0 && name != "" {
		var err error
		next, err = nm.InterfaceFinder().ByIndex(int(index))
		if err != nil {
			m.logger.Error("Android find interface: ", err)
			return
		}
	}
	// Snapshot every advertised interface, including DNS, gateways and capability
	// bits. Comparing only name/index loses real same-interface network changes.
	// JSON freezes slices so later platform mutations cannot alter the baseline.
	parts := make([]string, 0)
	for _, it := range nm.NetworkInterfaces() {
		it.Addresses = append(it.Addresses[:0:0], it.Addresses...)
		it.DNSServers = append([]string(nil), it.DNSServers...)
		it.Gateways = append(it.Gateways[:0:0], it.Gateways...)
		sort.Slice(it.Addresses, func(i, j int) bool { return it.Addresses[i].String() < it.Addresses[j].String() })
		sort.Strings(it.DNSServers)
		sort.Slice(it.Gateways, func(i, j int) bool { return it.Gateways[i].String() < it.Gateways[j].String() })
		encoded, _ := json.Marshal(it)
		parts = append(parts, string(encoded))
	}
	sort.Strings(parts)
	encoded, _ := json.Marshal(parts)
	key := fmt.Sprintf("%s/%d/%t/%t/%s", name, index, expensive, constrained, encoded)
	m.mu.Lock()
	if !m.started || generation != m.generation {
		m.mu.Unlock()
		return
	}
	m.current = next
	isOffline := next == nil
	// A confirmed offline transition opens a recovery epoch. Its first usable
	// snapshot must be delivered even when byte-identical to the pre-loss one;
	// otherwise repeated call hang-ups never advance the core generation. Keep
	// ordinary duplicate suppression before/after that single recovery event.
	forceRecovery := !isOffline && m.offlineSeen
	if m.hasSnapshot && key == m.snapshotKey && !forceRecovery {
		m.mu.Unlock()
		return
	}
	m.snapshotKey, m.hasSnapshot = key, true
	if isOffline && !m.offlineSeen {
		m.recoveryEpoch++
	}
	m.offlineSeen = isOffline
	epoch := m.recoveryEpoch
	callbacks := m.callbacks.Array()
	m.mu.Unlock()
	if isOffline {
		D.EmitRecovery(m.logger, D.RecoveryRecord{Event: D.RecoveryOffline, Epoch: epoch, Online: false})
	} else {
		D.EmitRecovery(m.logger, D.RecoveryRecord{Event: D.RecoveryOnlineSnapshot, Epoch: epoch, Online: true, ForcedSame: forceRecovery})
	}
	// Core resets only on a new snapshot; duplicate startup callbacks must not
	// cancel the initialize update and reclassify it as a post-start reset.
	for _, cb := range callbacks {
		cb(next, 0)
	}
}
