//go:build resolved_lifecycle_test && !linux

package local

import (
	"sync/atomic"

	"github.com/sagernet/sing-box/adapter"
	E "github.com/sagernet/sing/common/exceptions"
)

type resolvedServerSet struct {
	serverScope *adapter.Scope
}

func startResolvedTransport(serverScope *adapter.Scope, serverTransport adapter.DNSTransport) error {
	err := serverScope.Start("dns/resolved/"+serverTransport.Type(), serverTransport, adapter.StartStateStart)
	if err != nil {
		return E.Errors(err, serverScope.Remove(serverTransport))
	}
	return nil
}

func closeResolvedServerSet(serverSet *resolvedServerSet) error {
	if serverSet == nil || serverSet.serverScope == nil {
		return nil
	}
	return serverSet.serverScope.Close()
}

func replaceResolvedServerSet(savedServerSet *atomic.Pointer[resolvedServerSet], serverSet *resolvedServerSet) error {
	oldServerSet := savedServerSet.Swap(serverSet)
	return closeResolvedServerSet(oldServerSet)
}
