package dns

import (
	"context"
	"os"
	"strings"
	"sync"

	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
)

var _ adapter.DNSTransportManager = (*TransportManager)(nil)

type TransportManager struct {
	logger                   log.ContextLogger
	registry                 adapter.DNSTransportRegistry
	outbound                 adapter.OutboundManager
	defaultTag               string
	access                   sync.RWMutex
	started                  bool
	stage                    adapter.StartStage
	scope                    *adapter.Scope
	transports               []adapter.DNSTransport
	transportByTag           map[string]adapter.DNSTransport
	dependByTag              map[string][]string
	defaultTransport         adapter.DNSTransport
	defaultTransportFallback func() (adapter.DNSTransport, error)
	fakeIPTransport          adapter.FakeIPTransport
}

func NewTransportManager(registry adapter.DNSTransportRegistry, outbound adapter.OutboundManager, defaultTag string) *TransportManager {
	return &TransportManager{
		registry:       registry,
		outbound:       outbound,
		defaultTag:     defaultTag,
		transportByTag: make(map[string]adapter.DNSTransport),
		dependByTag:    make(map[string][]string),
	}
}

func (m *TransportManager) Initialize(defaultTransportFallback func() (adapter.DNSTransport, error)) {
	m.defaultTransportFallback = defaultTransportFallback
}

func (m *TransportManager) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	m.access.Lock()
	if m.started && m.stage >= stage {
		panic("already started")
	}
	m.started = true
	m.stage = stage
	m.scope = scope
	if stage == adapter.StartStateStart {
		if m.defaultTag != "" && m.defaultTransport == nil {
			m.access.Unlock()
			return E.New("default DNS server not found: ", m.defaultTag)
		}
		if m.defaultTransport == nil {
			defaultTransport, err := m.defaultTransportFallback()
			if err != nil {
				m.access.Unlock()
				return E.Cause(err, "default DNS server fallback")
			}
			m.transports = append(m.transports, defaultTransport)
			m.transportByTag[defaultTransport.Tag()] = defaultTransport
			m.defaultTransport = defaultTransport
		}
		transports := m.transports
		m.access.Unlock()
		return m.startTransports(scope, transports)
	} else {
		transports := m.transports
		m.access.Unlock()
		for _, outbound := range transports {
			err := adapter.LegacyStart(outbound, stage, scope)
			if err != nil {
				return E.Cause(err, stage, " dns/", outbound.Type(), "[", outbound.Tag(), "]")
			}
		}
	}
	return nil
}

func (m *TransportManager) startTransports(scope *adapter.Scope, transports []adapter.DNSTransport) error {
	started := make(map[string]bool)
	for {
		canContinue := false
	startOne:
		for _, transportToStart := range transports {
			transportTag := transportToStart.Tag()
			if started[transportTag] {
				continue
			}
			dependencies := transportToStart.Dependencies()
			for _, dependency := range dependencies {
				if !started[dependency] {
					continue startOne
				}
			}
			started[transportTag] = true
			canContinue = true
			name := "dns/" + transportToStart.Type() + "[" + transportTag + "]"
			err := scope.Start(name, transportToStart, adapter.StartStateStart)
			if err != nil {
				return err
			}
		}
		if len(started) == len(transports) {
			break
		}
		if canContinue {
			continue
		}
		currentTransport := common.Find(transports, func(it adapter.DNSTransport) bool {
			return !started[it.Tag()]
		})
		var lintTransport func(oTree []string, oCurrent adapter.DNSTransport) error
		lintTransport = func(oTree []string, oCurrent adapter.DNSTransport) error {
			problemTransportTag := common.Find(oCurrent.Dependencies(), func(it string) bool {
				return !started[it]
			})
			if common.Contains(oTree, problemTransportTag) {
				return E.New("circular server dependency: ", strings.Join(oTree, " -> "), " -> ", problemTransportTag)
			}
			m.access.Lock()
			problemTransport := m.transportByTag[problemTransportTag]
			m.access.Unlock()
			if problemTransport == nil {
				return E.New("dependency[", problemTransportTag, "] not found for server[", oCurrent.Tag(), "]")
			}
			return lintTransport(append(oTree, problemTransportTag), problemTransport)
		}
		return lintTransport([]string{currentTransport.Tag()}, currentTransport)
	}
	return nil
}

func (m *TransportManager) Transports() []adapter.DNSTransport {
	m.access.RLock()
	defer m.access.RUnlock()
	return m.transports
}

func (m *TransportManager) Transport(tag string) (adapter.DNSTransport, bool) {
	m.access.RLock()
	outbound, found := m.transportByTag[tag]
	m.access.RUnlock()
	return outbound, found
}

func (m *TransportManager) Default() adapter.DNSTransport {
	m.access.RLock()
	defer m.access.RUnlock()
	return m.defaultTransport
}

func (m *TransportManager) FakeIP() adapter.FakeIPTransport {
	m.access.RLock()
	defer m.access.RUnlock()
	return m.fakeIPTransport
}

func (m *TransportManager) Remove(tag string) error {
	m.access.Lock()
	defer m.access.Unlock()
	transport, found := m.transportByTag[tag]
	if !found {
		return os.ErrInvalid
	}
	delete(m.transportByTag, tag)
	index := common.Index(m.transports, func(it adapter.DNSTransport) bool {
		return it == transport
	})
	if index == -1 {
		panic("invalid inbound index")
	}
	m.transports = append(m.transports[:index], m.transports[index+1:]...)
	started := m.started
	if m.defaultTransport == transport {
		if len(m.transports) > 0 {
			nextTransport := m.transports[0]
			if nextTransport.Type() != C.DNSTypeFakeIP {
				return E.New("default server cannot be fakeip")
			}
			m.defaultTransport = nextTransport
			m.logger.Info("updated default server to ", m.defaultTransport.Tag())
		} else {
			m.defaultTransport = nil
		}
	}
	dependBy := m.dependByTag[tag]
	if len(dependBy) > 0 {
		return E.New("server[", tag, "] is depended by ", strings.Join(dependBy, ", "))
	}
	dependencies := transport.Dependencies()
	for _, dependency := range dependencies {
		if len(m.dependByTag[dependency]) == 1 {
			delete(m.dependByTag, dependency)
		} else {
			m.dependByTag[dependency] = common.Filter(m.dependByTag[dependency], func(it string) bool {
				return it != tag
			})
		}
	}
	if started {
		common.Close(transport)
	}
	return nil
}

func (m *TransportManager) Create(ctx context.Context, logger log.ContextLogger, tag string, transportType string, options any) error {
	if tag == "" {
		return os.ErrInvalid
	}
	transport, err := m.registry.CreateDNSTransport(ctx, logger, tag, transportType, options)
	if err != nil {
		return err
	}
	m.access.Lock()
	defer m.access.Unlock()
	if m.started {
		for _, stage := range adapter.ListStartStages {
			err = adapter.LegacyStart(transport, stage, m.scope)
			if err != nil {
				return E.Cause(err, stage, " dns/", transport.Type(), "[", transport.Tag(), "]")
			}
		}
	}
	if existsTransport, loaded := m.transportByTag[tag]; loaded {
		if m.started {
			err = common.Close(existsTransport)
			if err != nil {
				return E.Cause(err, "close dns/", existsTransport.Type(), "[", existsTransport.Tag(), "]")
			}
		}
		existsIndex := common.Index(m.transports, func(it adapter.DNSTransport) bool {
			return it == existsTransport
		})
		if existsIndex == -1 {
			panic("invalid inbound index")
		}
		m.transports = append(m.transports[:existsIndex], m.transports[existsIndex+1:]...)
	}
	m.transports = append(m.transports, transport)
	m.transportByTag[tag] = transport
	dependencies := transport.Dependencies()
	for _, dependency := range dependencies {
		m.dependByTag[dependency] = append(m.dependByTag[dependency], tag)
	}
	if tag == m.defaultTag || (m.defaultTag == "" && m.defaultTransport == nil) {
		if transport.Type() == C.DNSTypeFakeIP {
			return E.New("default server cannot be fakeip")
		}
		m.defaultTransport = transport
		if m.started {
			m.logger.Info("updated default server to ", transport.Tag())
		}
	}
	if transport.Type() == C.DNSTypeFakeIP {
		if m.fakeIPTransport != nil {
			return E.New("multiple fakeip server are not supported")
		}
		m.fakeIPTransport = transport.(adapter.FakeIPTransport)
	}
	return nil
}
