//go:build linux || resolved_lifecycle_test

package local

import (
	"bufio"
	"context"
	"errors"
	"net/netip"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/dialer"
	"github.com/sagernet/sing-box/common/tls"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/dns"
	dnsTransport "github.com/sagernet/sing-box/dns/transport"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/service/resolved"
	"github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/control"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/common/x/list"
	"github.com/sagernet/sing/service"

	"github.com/godbus/dbus/v5"
	mDNS "github.com/miekg/dns"
)

func isSystemdResolvedManaged() bool {
	resolvContent, err := os.Open("/etc/resolv.conf")
	if err != nil {
		return false
	}
	defer resolvContent.Close()
	scanner := bufio.NewScanner(resolvContent)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line[0] != '#' {
			return false
		}
		if strings.Contains(line, "systemd-resolved") {
			return true
		}
	}
	return false
}

type DBusResolvedResolver struct {
	ctx               context.Context
	logger            logger.ContextLogger
	interfaceMonitor  tun.DefaultInterfaceMonitor
	interfaceCallback *list.Element[tun.DefaultInterfaceUpdateCallback]
	systemBus         *dbus.Conn
	savedServerSet    atomic.Pointer[resolvedServerSet]
	updateAccess      sync.Mutex
	updateCancel      context.CancelFunc
	updateRunAccess   sync.Mutex
	generation        uint64
	closed            bool
	checkResolvedFunc func(context.Context) (*resolvedServerSet, error)
	closeOnce         sync.Once
}

type resolvedServerSet struct {
	servers     []resolvedServer
	signature   []string
	serverScope *adapter.Scope

	leaseAccess sync.Mutex
	retired     bool
	leases      int
	drained     chan struct{}
	closeOnce   sync.Once
	closeErr    error
}

type resolvedServerSetLease struct {
	serverSet *resolvedServerSet
	once      sync.Once
}

func (l *resolvedServerSetLease) Release() {
	if l == nil || l.serverSet == nil {
		return
	}
	l.once.Do(l.serverSet.release)
}

func (s *resolvedServerSet) acquire() (*resolvedServerSetLease, bool) {
	if s == nil {
		return nil, false
	}
	s.leaseAccess.Lock()
	defer s.leaseAccess.Unlock()
	if s.retired {
		return nil, false
	}
	s.leases++
	return &resolvedServerSetLease{serverSet: s}, true
}

func (s *resolvedServerSet) release() {
	s.leaseAccess.Lock()
	s.leases--
	if s.retired && s.leases == 0 {
		close(s.drained)
	}
	s.leaseAccess.Unlock()
}

func (s *resolvedServerSet) retire() <-chan struct{} {
	s.leaseAccess.Lock()
	defer s.leaseAccess.Unlock()
	if !s.retired {
		s.retired = true
		s.drained = make(chan struct{})
		if s.leases == 0 {
			close(s.drained)
		}
	}
	return s.drained
}

type resolvedServer struct {
	primaryTransport  adapter.DNSTransport
	fallbackTransport adapter.DNSTransport
}

type resolvedServerSpecification struct {
	address    netip.Addr
	port       uint16
	serverName string
}

func NewResolvedResolver(ctx context.Context, logger logger.ContextLogger) (ResolvedResolver, error) {
	interfaceMonitor := service.FromContext[adapter.NetworkManager](ctx).InterfaceMonitor()
	if interfaceMonitor == nil {
		return nil, os.ErrInvalid
	}
	systemBus, err := dbus.SystemBus()
	if err != nil {
		return nil, err
	}
	return &DBusResolvedResolver{
		ctx:              ctx,
		logger:           logger,
		interfaceMonitor: interfaceMonitor,
		systemBus:        systemBus,
	}, nil
}

func (t *DBusResolvedResolver) Start() error {
	t.updateStatus(t.ctx)
	t.interfaceCallback = t.interfaceMonitor.RegisterCallback(t.updateDefaultInterface)
	err := t.systemBus.BusObject().AddMatchSignal(
		"org.freedesktop.DBus",
		"NameOwnerChanged",
		dbus.WithMatchSender("org.freedesktop.DBus"),
		dbus.WithMatchArg(0, "org.freedesktop.resolve1"),
	).Err
	if err != nil {
		return E.Cause(err, "configure resolved restart listener")
	}
	err = t.systemBus.BusObject().AddMatchSignal(
		"org.freedesktop.DBus.Properties",
		"PropertiesChanged",
		dbus.WithMatchSender("org.freedesktop.resolve1"),
		dbus.WithMatchArg(0, "org.freedesktop.resolve1.Manager"),
	).Err
	if err != nil {
		return E.Cause(err, "configure resolved properties listener")
	}
	go t.loopUpdateStatus()
	return nil
}

func (t *DBusResolvedResolver) Close() error {
	var closeErr error
	t.closeOnce.Do(func() {
		t.updateAccess.Lock()
		updateCancel := t.updateCancel
		t.updateCancel = nil
		t.updateAccess.Unlock()
		if updateCancel != nil {
			updateCancel()
		}
		t.updateRunAccess.Lock()
		t.closed = true
		t.generation++
		serverSet := t.savedServerSet.Swap(nil)
		t.updateRunAccess.Unlock()
		if serverSet != nil {
			closeErr = closeResolvedServerSet(serverSet)
		}
		if t.interfaceCallback != nil {
			t.interfaceMonitor.UnregisterCallback(t.interfaceCallback)
		}
		if t.systemBus != nil {
			_ = t.systemBus.Close()
		}
	})
	return closeErr
}

func (t *DBusResolvedResolver) Reset() {
	serverSet := t.savedServerSet.Load()
	if serverSet == nil {
		return
	}
	for _, server := range serverSet.servers {
		server.primaryTransport.Reset()
		if server.fallbackTransport != nil {
			server.fallbackTransport.Reset()
		}
	}
}

func (t *DBusResolvedResolver) Environment() []string {
	serverSet := t.savedServerSet.Load()
	if serverSet == nil {
		return nil
	}
	return serverSet.signature
}

func (t *DBusResolvedResolver) acquireCurrentServerSet() (*resolvedServerSet, *resolvedServerSetLease) {
	for attempts := 0; attempts < 2; attempts++ {
		serverSet := t.savedServerSet.Load()
		if serverSet == nil {
			return nil, nil
		}
		lease, acquired := serverSet.acquire()
		if !acquired {
			continue
		}
		if t.savedServerSet.Load() == serverSet {
			return serverSet, lease
		}
		lease.Release()
	}
	return nil, nil
}

func (t *DBusResolvedResolver) Exchange(ctx context.Context, message *mDNS.Msg) (*mDNS.Msg, error) {
	t.updateRunAccess.Lock()
	if t.closed {
		t.updateRunAccess.Unlock()
		return nil, os.ErrClosed
	}
	serverSet, lease := t.acquireCurrentServerSet()
	t.updateRunAccess.Unlock()
	if serverSet == nil {
		var err error
		serverSet, err = t.initializeServerSet(ctx)
		if err != nil {
			return nil, err
		}
		lease, _ = serverSet.acquire()
		if lease == nil {
			return nil, os.ErrClosed
		}
	}
	response, err := t.exchangeServerSet(ctx, message, serverSet)
	lease.Release()
	if err == nil {
		return response, nil
	}
	t.updateStatus(t.ctx)
	refreshedServerSet, refreshedLease := t.acquireCurrentServerSet()
	if refreshedServerSet == nil || refreshedServerSet == serverSet {
		if refreshedLease != nil {
			refreshedLease.Release()
		}
		return nil, err
	}
	defer refreshedLease.Release()
	return t.exchangeServerSet(ctx, message, refreshedServerSet)
}

func (t *DBusResolvedResolver) ExchangeAsync(ctx context.Context, message *mDNS.Msg, callback func(response *mDNS.Msg, err error)) {
	t.updateRunAccess.Lock()
	if t.closed {
		t.updateRunAccess.Unlock()
		callback(nil, os.ErrClosed)
		return
	}
	serverSet, lease := t.acquireCurrentServerSet()
	t.updateRunAccess.Unlock()
	if serverSet == nil {
		go func() {
			callback(t.Exchange(ctx, message))
		}()
		return
	}
	t.exchangeServerSetAsync(ctx, message, serverSet, func(response *mDNS.Msg, err error) {
		if err == nil {
			callback(response, nil)
			lease.Release()
			return
		}
		lease.Release()
		go func() {
			t.updateStatus(t.ctx)
			refreshedServerSet, refreshedLease := t.acquireCurrentServerSet()
			if refreshedServerSet == nil || refreshedServerSet == serverSet {
				if refreshedLease != nil {
					refreshedLease.Release()
				}
				callback(nil, err)
				return
			}
			t.exchangeServerSetAsync(ctx, message, refreshedServerSet, func(response *mDNS.Msg, err error) {
				callback(response, err)
				refreshedLease.Release()
			})
		}()
	})
}

func (t *DBusResolvedResolver) exchangeServerSetAsync(ctx context.Context, message *mDNS.Msg, serverSet *resolvedServerSet, callback func(response *mDNS.Msg, err error)) {
	if len(serverSet.servers) == 0 {
		callback(nil, E.New("link has no DNS servers configured"))
		return
	}
	serverExchangers := make([]dnsTransport.AsyncExchanger, 0, len(serverSet.servers))
	for _, server := range serverSet.servers {
		serverExchangers = append(serverExchangers, func(exchangeCtx context.Context, exchangeCallback func(response *mDNS.Msg, err error)) {
			server.primaryTransport.ExchangeAsync(exchangeCtx, message, func(response *mDNS.Msg, exchangeErr error) {
				if exchangeErr != nil && server.fallbackTransport != nil {
					server.fallbackTransport.ExchangeAsync(exchangeCtx, message, exchangeCallback)
					return
				}
				exchangeCallback(response, exchangeErr)
			})
		})
	}
	dnsTransport.ExchangeSequential(ctx, serverExchangers, nil, callback)
}

func (t *DBusResolvedResolver) loopUpdateStatus() {
	signalChan := make(chan *dbus.Signal, 1)
	t.systemBus.Signal(signalChan)
	for signal := range signalChan {
		switch signal.Name {
		case "org.freedesktop.DBus.NameOwnerChanged":
			if len(signal.Body) != 3 {
				continue
			}
			newOwner, loaded := signal.Body[2].(string)
			if !loaded || newOwner == "" {
				continue
			}
			t.postUpdateStatus()
		case "org.freedesktop.DBus.Properties.PropertiesChanged":
			if !shouldUpdateResolvedServerSet(signal) {
				continue
			}
			t.postUpdateStatus()
		}
	}
}

func (t *DBusResolvedResolver) postUpdateStatus() {
	updateContext, updateCancel := context.WithCancel(t.ctx)
	t.updateAccess.Lock()
	previousCancel := t.updateCancel
	t.updateCancel = updateCancel
	t.updateAccess.Unlock()
	if previousCancel != nil {
		previousCancel()
	}
	go func() {
		defer updateCancel()
		t.updateStatus(updateContext)
	}()
}

func (t *DBusResolvedResolver) initializeServerSet(ctx context.Context) (*resolvedServerSet, error) {
	for {
		t.updateRunAccess.Lock()
		if t.closed {
			t.updateRunAccess.Unlock()
			return nil, os.ErrClosed
		}
		if serverSet := t.savedServerSet.Load(); serverSet != nil {
			t.updateRunAccess.Unlock()
			return serverSet, nil
		}
		generation := t.generation
		t.updateRunAccess.Unlock()

		serverSet, err := t.loadResolved(ctx)
		if err != nil {
			return nil, err
		}

		t.updateRunAccess.Lock()
		if t.closed || ctx.Err() != nil {
			t.updateRunAccess.Unlock()
			_ = closeResolvedServerSet(serverSet)
			if t.closed {
				return nil, os.ErrClosed
			}
			return nil, ctx.Err()
		}
		if t.generation != generation {
			currentServerSet := t.savedServerSet.Load()
			t.updateRunAccess.Unlock()
			_ = closeResolvedServerSet(serverSet)
			if currentServerSet != nil {
				return currentServerSet, nil
			}
			continue
		}
		if currentServerSet := t.savedServerSet.Load(); currentServerSet != nil {
			t.updateRunAccess.Unlock()
			_ = closeResolvedServerSet(serverSet)
			return currentServerSet, nil
		}
		t.savedServerSet.Store(serverSet)
		t.updateRunAccess.Unlock()
		return serverSet, nil
	}
}

func (t *DBusResolvedResolver) updateStatus(ctx context.Context) {
	t.updateRunAccess.Lock()
	if t.closed || ctx.Err() != nil {
		t.updateRunAccess.Unlock()
		return
	}
	generation := t.generation
	t.updateRunAccess.Unlock()

	serverSet, err := t.loadResolved(ctx)

	t.updateRunAccess.Lock()
	if t.closed || t.generation != generation || ctx.Err() != nil {
		t.updateRunAccess.Unlock()
		if serverSet != nil {
			_ = closeResolvedServerSet(serverSet)
		}
		return
	}
	oldServerSet := t.savedServerSet.Load()
	t.generation++
	t.savedServerSet.Store(serverSet)
	if oldServerSet != nil {
		oldServerSet.retire()
	}
	t.updateRunAccess.Unlock()
	_ = closeResolvedServerSet(oldServerSet)
	if err != nil {
		var dbusErr dbus.Error
		if !errors.As(err, &dbusErr) || dbusErr.Name != "org.freedesktop.DBus.Error.NameHasNoOwner" {
			t.logger.Debug(E.Cause(err, "systemd-resolved service unavailable"))
		}
		if oldServerSet != nil {
			t.logger.Debug("systemd-resolved service is gone")
		}
		return
	} else if oldServerSet == nil {
		t.logger.Debug("using systemd-resolved service as resolver")
	}
}

func (t *DBusResolvedResolver) exchangeServerSet(ctx context.Context, message *mDNS.Msg, serverSet *resolvedServerSet) (*mDNS.Msg, error) {
	if serverSet == nil || len(serverSet.servers) == 0 {
		return nil, E.New("link has no DNS servers configured")
	}
	var lastError error
	for _, server := range serverSet.servers {
		response, err := server.primaryTransport.Exchange(ctx, message)
		if err != nil && server.fallbackTransport != nil {
			response, err = server.fallbackTransport.Exchange(ctx, message)
		}
		if err != nil {
			lastError = err
			continue
		}
		return response, nil
	}
	return nil, lastError
}

func (t *DBusResolvedResolver) loadResolved(ctx context.Context) (*resolvedServerSet, error) {
	if t.checkResolvedFunc != nil {
		return t.checkResolvedFunc(ctx)
	}
	return t.checkResolved(ctx)
}

func (t *DBusResolvedResolver) checkResolved(ctx context.Context) (*resolvedServerSet, error) {
	dbusObject := t.systemBus.Object("org.freedesktop.resolve1", "/org/freedesktop/resolve1")
	err := dbusObject.(*dbus.Object).CallWithContext(ctx, "org.freedesktop.DBus.Peer.Ping", 0).Err
	if err != nil {
		return nil, err
	}
	defaultInterface := t.interfaceMonitor.DefaultInterface()
	if defaultInterface == nil {
		return nil, E.New("missing default interface")
	}
	call := dbusObject.(*dbus.Object).CallWithContext(
		ctx,
		"org.freedesktop.resolve1.Manager.GetLink",
		0,
		int32(defaultInterface.Index),
	)
	if call.Err != nil {
		return nil, call.Err
	}
	var linkPath dbus.ObjectPath
	err = call.Store(&linkPath)
	if err != nil {
		return nil, err
	}
	linkObject := t.systemBus.Object("org.freedesktop.resolve1", linkPath)
	if linkObject == nil {
		return nil, E.New("missing link object for default interface")
	}
	dnsOverTLSMode, err := loadResolvedLinkDNSOverTLS(linkObject)
	if err != nil {
		return nil, err
	}
	err = ctx.Err()
	if err != nil {
		return nil, err
	}
	linkDNSEx, err := loadResolvedLinkDNSEx(linkObject)
	if err != nil {
		return nil, err
	}
	err = ctx.Err()
	if err != nil {
		return nil, err
	}
	linkDNS, err := loadResolvedLinkDNS(linkObject)
	if err != nil {
		return nil, err
	}
	if len(linkDNSEx) == 0 && len(linkDNS) == 0 {
		for _, inbound := range service.FromContext[adapter.InboundManager](t.ctx).Inbounds() {
			if inbound.Type() == C.TypeTun {
				return nil, E.New("No appropriate name servers or networks for name found")
			}
		}
		return nil, E.New("link has no DNS servers configured")
	}
	serverDialer, err := dialer.NewDefault(t.ctx, option.DialerOptions{
		AbstractDialerOptions: option.AbstractDialerOptions{
			BindInterface:      defaultInterface.Name,
			UDPFragmentDefault: true,
		},
	})
	if err != nil {
		return nil, err
	}
	var serverSpecifications []resolvedServerSpecification
	if len(linkDNSEx) > 0 {
		for _, entry := range linkDNSEx {
			serverSpecification, loaded := buildResolvedServerSpecification(defaultInterface.Name, entry.Address, entry.Port, entry.Name)
			if !loaded {
				continue
			}
			serverSpecifications = append(serverSpecifications, serverSpecification)
		}
	} else {
		for _, entry := range linkDNS {
			serverSpecification, loaded := buildResolvedServerSpecification(defaultInterface.Name, entry.Address, 0, "")
			if !loaded {
				continue
			}
			serverSpecifications = append(serverSpecifications, serverSpecification)
		}
	}
	if len(serverSpecifications) == 0 {
		return nil, E.New("no valid DNS servers on link")
	}
	serverScope := adapter.NewScope(t.ctx, t.logger)
	serverSet := &resolvedServerSet{
		servers: make([]resolvedServer, 0, len(serverSpecifications)),
		signature: common.Map(serverSpecifications, func(it resolvedServerSpecification) string {
			return M.SocksaddrFrom(it.address, it.port).String()
		}),
		serverScope: serverScope,
	}
	for _, serverSpecification := range serverSpecifications {
		server, createErr := t.createResolvedServer(serverScope, serverDialer, dnsOverTLSMode, serverSpecification)
		if createErr != nil {
			return nil, E.Errors(createErr, serverScope.Close())
		}
		serverSet.servers = append(serverSet.servers, server)
	}
	return serverSet, nil
}

func (t *DBusResolvedResolver) createResolvedServer(serverScope *adapter.Scope, serverDialer N.Dialer, dnsOverTLSMode string, serverSpecification resolvedServerSpecification) (resolvedServer, error) {
	if dnsOverTLSMode == "yes" {
		primaryTransport, err := t.createResolvedTransport(serverScope, serverDialer, serverSpecification, true)
		if err != nil {
			return resolvedServer{}, err
		}
		return resolvedServer{
			primaryTransport: primaryTransport,
		}, nil
	}
	if dnsOverTLSMode == "opportunistic" {
		primaryTransport, err := t.createResolvedTransport(serverScope, serverDialer, serverSpecification, true)
		if err != nil {
			return resolvedServer{}, err
		}
		fallbackTransport, err := t.createResolvedTransport(serverScope, serverDialer, serverSpecification, false)
		if err != nil {
			return resolvedServer{}, err
		}
		return resolvedServer{
			primaryTransport:  primaryTransport,
			fallbackTransport: fallbackTransport,
		}, nil
	}
	primaryTransport, err := t.createResolvedTransport(serverScope, serverDialer, serverSpecification, false)
	if err != nil {
		return resolvedServer{}, err
	}
	return resolvedServer{
		primaryTransport: primaryTransport,
	}, nil
}

func (t *DBusResolvedResolver) createResolvedTransport(serverScope *adapter.Scope, serverDialer N.Dialer, serverSpecification resolvedServerSpecification, useTLS bool) (adapter.DNSTransport, error) {
	serverAddress := M.SocksaddrFrom(serverSpecification.address, resolvedServerPort(serverSpecification.port, useTLS))
	if useTLS {
		tlsAddress := serverSpecification.address
		if tlsAddress.Zone() != "" {
			tlsAddress = tlsAddress.WithZone("")
		}
		serverName := serverSpecification.serverName
		if serverName == "" {
			serverName = tlsAddress.String()
		}
		tlsConfig, err := tls.NewClient(t.ctx, t.logger, tlsAddress.String(), option.OutboundTLSOptions{
			Enabled:    true,
			ServerName: serverName,
		})
		if err != nil {
			return nil, err
		}
		serverTransport := dnsTransport.NewTLSRaw(t.logger, dns.NewTransportAdapter(C.DNSTypeTLS, "", nil), serverDialer, serverAddress, tlsConfig)
		err = startResolvedTransport(serverScope, serverTransport)
		if err != nil {
			return nil, err
		}
		return serverTransport, nil
	}
	serverTransport := dnsTransport.NewUDPRaw(t.logger, dns.NewTransportAdapter(C.DNSTypeUDP, "", nil), serverDialer, serverAddress)
	err := startResolvedTransport(serverScope, serverTransport)
	if err != nil {
		return nil, err
	}
	return serverTransport, nil
}

func startResolvedTransport(serverScope *adapter.Scope, serverTransport adapter.DNSTransport) error {
	err := serverScope.Start("dns/resolved/"+serverTransport.Type(), serverTransport, adapter.StartStateStart)
	if err != nil {
		return E.Errors(err, serverScope.Remove(serverTransport))
	}
	return nil
}

func closeResolvedServerSetContext(ctx context.Context, serverSet *resolvedServerSet) error {
	if serverSet == nil {
		return nil
	}
	drained := serverSet.retire()
	select {
	case <-drained:
	case <-ctx.Done():
		go func() {
			<-drained
			_ = closeResolvedServerSetContext(context.Background(), serverSet)
		}()
		return ctx.Err()
	}
	serverSet.closeOnce.Do(func() {
		if serverSet.serverScope != nil {
			serverSet.closeErr = serverSet.serverScope.Close()
		}
	})
	return serverSet.closeErr
}

func closeResolvedServerSet(serverSet *resolvedServerSet) error {
	return closeResolvedServerSetContext(context.Background(), serverSet)
}

func replaceResolvedServerSet(savedServerSet *atomic.Pointer[resolvedServerSet], serverSet *resolvedServerSet) error {
	oldServerSet := savedServerSet.Swap(serverSet)
	if oldServerSet != nil {
		oldServerSet.retire()
	}
	return closeResolvedServerSet(oldServerSet)
}

func buildResolvedServerSpecification(interfaceName string, rawAddress []byte, port uint16, serverName string) (resolvedServerSpecification, bool) {
	address, loaded := netip.AddrFromSlice(rawAddress)
	if !loaded {
		return resolvedServerSpecification{}, false
	}
	if address.Is6() && address.IsLinkLocalUnicast() && address.Zone() == "" {
		address = address.WithZone(interfaceName)
	}
	return resolvedServerSpecification{
		address:    address,
		port:       port,
		serverName: serverName,
	}, true
}

func resolvedServerPort(port uint16, useTLS bool) uint16 {
	if port > 0 {
		return port
	}
	if useTLS {
		return 853
	}
	return 53
}

func loadResolvedLinkDNS(linkObject dbus.BusObject) ([]resolved.LinkDNS, error) {
	dnsProperty, err := linkObject.GetProperty("org.freedesktop.resolve1.Link.DNS")
	if err != nil {
		if isResolvedUnknownPropertyError(err) {
			return nil, nil
		}
		return nil, err
	}
	var linkDNS []resolved.LinkDNS
	err = dnsProperty.Store(&linkDNS)
	if err != nil {
		return nil, err
	}
	return linkDNS, nil
}

func loadResolvedLinkDNSEx(linkObject dbus.BusObject) ([]resolved.LinkDNSEx, error) {
	dnsProperty, err := linkObject.GetProperty("org.freedesktop.resolve1.Link.DNSEx")
	if err != nil {
		if isResolvedUnknownPropertyError(err) {
			return nil, nil
		}
		return nil, err
	}
	var linkDNSEx []resolved.LinkDNSEx
	err = dnsProperty.Store(&linkDNSEx)
	if err != nil {
		return nil, err
	}
	return linkDNSEx, nil
}

func loadResolvedLinkDNSOverTLS(linkObject dbus.BusObject) (string, error) {
	dnsOverTLSProperty, err := linkObject.GetProperty("org.freedesktop.resolve1.Link.DNSOverTLS")
	if err != nil {
		if isResolvedUnknownPropertyError(err) {
			return "", nil
		}
		return "", err
	}
	var dnsOverTLSMode string
	err = dnsOverTLSProperty.Store(&dnsOverTLSMode)
	if err != nil {
		return "", err
	}
	return dnsOverTLSMode, nil
}

func isResolvedUnknownPropertyError(err error) bool {
	var dbusError dbus.Error
	return errors.As(err, &dbusError) && dbusError.Name == "org.freedesktop.DBus.Error.UnknownProperty"
}

func shouldUpdateResolvedServerSet(signal *dbus.Signal) bool {
	if len(signal.Body) != 3 {
		return true
	}
	changedProperties, loaded := signal.Body[1].(map[string]dbus.Variant)
	if !loaded {
		return true
	}
	for propertyName := range changedProperties {
		switch propertyName {
		case "DNS", "DNSEx", "DNSOverTLS":
			return true
		}
	}
	invalidatedProperties, loaded := signal.Body[2].([]string)
	if !loaded {
		return true
	}
	for _, propertyName := range invalidatedProperties {
		switch propertyName {
		case "DNS", "DNSEx", "DNSOverTLS":
			return true
		}
	}
	return false
}

func (t *DBusResolvedResolver) updateDefaultInterface(defaultInterface *control.Interface, flags int) {
	t.postUpdateStatus()
}
