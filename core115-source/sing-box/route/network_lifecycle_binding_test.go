package route

import (
	"context"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/control"
	"github.com/sagernet/sing/common/logger"
	"github.com/sagernet/sing/service"
	"github.com/sagernet/sing/service/pause"
)

// This reproduces Box.New's production registration order: NetworkManager is
// constructed before ConnectionManager is registered. It deliberately uses the
// real constructors and service context rather than injecting a nil mock.
func TestProductionConstructionOrderBindsGenerationManager(t *testing.T) {
	ctx, cancel := context.WithCancel(pause.WithDefaultManager(context.Background()))
	defer cancel()
	service.MustRegister[adapter.EndpointManager](ctx, startupEndpoints{})
	service.MustRegister[adapter.InboundManager](ctx, startupInbounds{})
	service.MustRegister[adapter.OutboundManager](ctx, startupOutbounds{})
	router := &startupRouter{resets: make(chan struct{}, 1)}
	service.MustRegister[adapter.Router](ctx, router)

	network, err := NewNetworkManager(ctx, logger.NOP(), option.RouteOptions{}, option.DNSOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer network.Close()
	connections := NewConnectionManager(logger.NOP())
	service.MustRegister[adapter.ConnectionManager](ctx, connections)
	network.BindConnectionManager(connections)
	if err = network.Start(adapter.StartStateInitialize); err != nil {
		t.Fatal(err)
	}
	if err = network.Start(adapter.StartStatePostStart); err != nil {
		t.Fatal(err)
	}

	network.notifyInterfaceUpdate(&control.Interface{Name: "production-order", Index: 1}, 0)
	deadline := time.Now().Add(time.Second)
	for connections.NetworkGeneration() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := connections.NetworkGeneration(); got != 1 {
		t.Fatalf("generation=%d, NetworkManager retained the pre-registration nil dependency", got)
	}
}
