package inbound

import (
	"context"
	"os"
	"sync"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/taskmonitor"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
)

var _ adapter.InboundManager = (*Manager)(nil)

type Manager struct {
	logger       log.ContextLogger
	registry     adapter.InboundRegistry
	endpoint     adapter.EndpointManager
	access       sync.Mutex
	started      bool
	stage        adapter.StartStage
	scope        *adapter.Scope
	inbounds     []adapter.Inbound
	inboundByTag map[string]adapter.Inbound
}

func NewManager(logger log.ContextLogger, registry adapter.InboundRegistry, endpoint adapter.EndpointManager) *Manager {
	return &Manager{
		logger:       logger,
		registry:     registry,
		endpoint:     endpoint,
		inboundByTag: make(map[string]adapter.Inbound),
	}
}

func (m *Manager) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	m.access.Lock()
	m.started = true
	m.stage = stage
	m.scope = scope
	inbounds := m.inbounds
	m.access.Unlock()
	for _, inbound := range inbounds {
		name := "inbound/" + inbound.Type() + "[" + inbound.Tag() + "]"
		err := scope.Start(name, inbound, stage)
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) Close() error {
	m.access.Lock()
	defer m.access.Unlock()
	if !m.started {
		return nil
	}
	m.started = false
	inbounds := m.inbounds
	m.inbounds = nil
	monitor := taskmonitor.New(m.logger, C.StopTimeout)
	var err error
	for _, inbound := range inbounds {
		name := "inbound/" + inbound.Type() + "[" + inbound.Tag() + "]"
		done := adapter.LogElapsed(m.logger, "close ", name)
		monitor.Start("close ", name)
		err = E.Append(err, common.Close(inbound), func(err error) error {
			return E.Cause(err, "close ", name)
		})
		monitor.Finish()
		done()
	}
	return nil
}

func (m *Manager) Inbounds() []adapter.Inbound {
	m.access.Lock()
	defer m.access.Unlock()
	return m.inbounds
}

func (m *Manager) Get(tag string) (adapter.Inbound, bool) {
	m.access.Lock()
	inbound, found := m.inboundByTag[tag]
	m.access.Unlock()
	if found {
		return inbound, true
	}
	return m.endpoint.Get(tag)
}

func (m *Manager) Remove(tag string) error {
	m.access.Lock()
	inbound, found := m.inboundByTag[tag]
	if !found {
		m.access.Unlock()
		return os.ErrInvalid
	}
	delete(m.inboundByTag, tag)
	index := common.Index(m.inbounds, func(it adapter.Inbound) bool {
		return it == inbound
	})
	if index == -1 {
		panic("invalid inbound index")
	}
	m.inbounds = append(m.inbounds[:index], m.inbounds[index+1:]...)
	started := m.started
	scope := m.scope
	m.access.Unlock()
	if started {
		return scope.Remove(inbound)
	}
	return nil
}

func (m *Manager) Create(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, outboundType string, options any) error {
	inbound, err := m.registry.Create(ctx, router, logger, tag, outboundType, options)
	if err != nil {
		return err
	}
	m.access.Lock()
	defer m.access.Unlock()
	if m.started {
		name := "inbound/" + inbound.Type() + "[" + inbound.Tag() + "]"
		for _, stage := range adapter.ListStartStages {
			err = m.scope.Start(name, inbound, stage)
			if err != nil {
				_ = m.scope.Remove(inbound)
				return err
			}
		}
	}
	if existsInbound, loaded := m.inboundByTag[tag]; loaded {
		if m.started {
			err = m.scope.Remove(existsInbound)
			if err != nil {
				closeErr := E.Cause(err, "close inbound/", existsInbound.Type(), "[", existsInbound.Tag(), "]")
				rollbackErr := m.scope.Remove(inbound)
				return E.Append(closeErr, rollbackErr, func(err error) error {
					return E.Cause(err, "rollback inbound/", inbound.Type(), "[", inbound.Tag(), "]")
				})
			}
		}
		existsIndex := common.Index(m.inbounds, func(it adapter.Inbound) bool {
			return it == existsInbound
		})
		if existsIndex == -1 {
			panic("invalid inbound index")
		}
		m.inbounds = append(m.inbounds[:existsIndex], m.inbounds[existsIndex+1:]...)
	}
	m.inbounds = append(m.inbounds, inbound)
	m.inboundByTag[tag] = inbound
	return nil
}
