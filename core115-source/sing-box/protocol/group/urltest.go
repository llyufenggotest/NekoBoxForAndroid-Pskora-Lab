package group

import (
	"context"
	"errors"
	"maps"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/dialer"
	"github.com/sagernet/sing-box/common/interrupt"
	"github.com/sagernet/sing-box/common/urltest"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/batch"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/common/x/list"
	"github.com/sagernet/sing/service"
	"github.com/sagernet/sing/service/pause"
)

func RegisterURLTest(registry *outbound.Registry) {
	outbound.Register[option.URLTestOutboundOptions](registry, C.TypeURLTest, NewURLTest)
}

var (
	_ adapter.OutboundGroup           = (*URLTest)(nil)
	_ adapter.InterfaceUpdateListener = (*URLTest)(nil)
	_ adapter.Referrer                = (*URLTest)(nil)
)

type URLTest struct {
	dialFallback bool
	fallbackMode string
	outbound.Adapter
	ctx                          context.Context
	outbound                     adapter.OutboundManager
	connection                   adapter.ConnectionManager
	logger                       log.ContextLogger
	tags                         []string
	link                         string
	interval                     time.Duration
	tolerance                    uint16
	idleTimeout                  time.Duration
	group                        *URLTestGroup
	checkAccess                  sync.Mutex
	interruptExternalConnections bool
}

func NewURLTest(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.URLTestOutboundOptions) (adapter.Outbound, error) {
	outbound := &URLTest{
		dialFallback:                 options.DialFallback,
		fallbackMode:                 options.FallbackMode,
		Adapter:                      outbound.NewAdapter(C.TypeURLTest, tag, []string{N.NetworkTCP, N.NetworkUDP}, options.Outbounds),
		ctx:                          ctx,
		outbound:                     service.FromContext[adapter.OutboundManager](ctx),
		connection:                   service.FromContext[adapter.ConnectionManager](ctx),
		logger:                       logger,
		tags:                         options.Outbounds,
		link:                         options.URL,
		interval:                     time.Duration(options.Interval),
		tolerance:                    options.Tolerance,
		idleTimeout:                  time.Duration(options.IdleTimeout),
		interruptExternalConnections: options.InterruptExistConnections,
	}
	if options.FallbackMode != "" && options.FallbackMode != "stable" && options.FallbackMode != "latency" {
		return nil, E.New("invalid fallback_mode: ", options.FallbackMode)
	}
	if !options.DialFallback && options.FallbackMode != "" {
		return nil, E.New("fallback_mode requires dial_fallback")
	}
	if options.DialFallback && options.InterruptExistConnections {
		return nil, E.New("dial_fallback requires interrupt_exist_connections=false")
	}
	if len(outbound.tags) == 0 {
		return nil, E.New("missing tags")
	}
	return outbound, nil
}

func (s *URLTest) Start() error {
	outbounds := make([]adapter.Outbound, 0, len(s.tags))
	for i, tag := range s.tags {
		detour, loaded := s.outbound.Outbound(tag)
		if !loaded {
			return E.New("outbound ", i, " not found: ", tag)
		}
		if s.dialFallback {
			if _, isGroup := detour.(adapter.OutboundGroup); isGroup {
				return E.New("dial_fallback requires leaf outbounds")
			}
		}
		outbounds = append(outbounds, detour)
	}
	group, err := NewURLTestGroup(s.ctx, s.outbound, s.logger, outbounds, s.link, s.interval, s.tolerance, s.idleTimeout, s.interruptExternalConnections)
	if err != nil {
		return err
	}
	if s.dialFallback {
		group.fallback = &fallbackState{mode: s.fallbackMode}
	}
	s.group = group
	return nil
}

func (s *URLTest) PostStart() error {
	s.group.PostStart()
	return nil
}

func (s *URLTest) Close() error {
	return common.Close(
		common.PtrOrNil(s.group),
	)
}

func (s *URLTest) Now() string {
	if s.group == nil {
		return ""
	}
	s.group.selectionAccess.RLock()
	defer s.group.selectionAccess.RUnlock()
	if s.dialFallback {
		return s.group.fallbackNow()
	}
	if s.group.selectedOutboundTCP != nil {
		return s.group.selectedOutboundTCP.Tag()
	} else if s.group.selectedOutboundUDP != nil {
		return s.group.selectedOutboundUDP.Tag()
	}
	return ""
}

func (s *URLTest) All() []string {
	return s.tags
}

func (s *URLTest) References() []string {
	group := s.group
	if group == nil {
		return nil
	}
	group.selectionAccess.RLock()
	defer group.selectionAccess.RUnlock()
	var references []string
	if group.selectedOutboundTCP != nil {
		references = append(references, group.selectedOutboundTCP.Tag())
	}
	if group.selectedOutboundUDP != nil && group.selectedOutboundUDP != group.selectedOutboundTCP {
		references = append(references, group.selectedOutboundUDP.Tag())
	}
	return references
}

func (s *URLTest) URLTest(ctx context.Context) (map[string]uint16, error) {
	return s.group.URLTest(ctx)
}

func (s *URLTest) CheckOutbounds() {
	s.group.CheckOutbounds(s.ctx, true)
}

func (s *URLTest) PerformUpdateCheck() {
	s.group.performUpdateCheck()
}

func (s *URLTest) InterfaceUpdated(ctx context.Context) {
	group := s.group
	if group == nil {
		return
	}
	if group.pause.IsDevicePaused() || group.pause.IsNetworkPaused() {
		return
	}
	group.requestRecovery(ctx)
}

func (s *URLTest) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	s.group.Touch()
	if s.dialFallback && N.NetworkName(network) == N.NetworkTCP {
		return s.dialWithFallback(ctx, network, destination)
	}
	var outbound adapter.Outbound
	switch N.NetworkName(network) {
	case N.NetworkTCP:
		outbound = s.group.selectedForNetwork(N.NetworkTCP)
	case N.NetworkUDP:
		if s.dialFallback {
			outbound, _ = s.group.Select(network)
		} else {
			outbound = s.group.selectedForNetwork(N.NetworkUDP)
		}
	default:
		return nil, E.Extend(N.ErrUnknownNetwork, network)
	}
	if outbound == nil {
		outbound, _ = s.group.Select(network)
	}
	if outbound == nil {
		return nil, E.New("missing supported outbound")
	}
	conn, err := outbound.DialContext(ctx, network, destination)
	if err == nil {
		return s.group.interruptGroup.NewConn(conn, interrupt.IsExternalConnectionFromContext(ctx)), nil
	}
	s.logger.ErrorContext(ctx, err)
	s.group.history.DeleteURLTestHistory(outbound.Tag())
	return nil, err
}

func (s *URLTest) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	s.group.Touch()
	outbound := s.group.selectedForNetwork(N.NetworkUDP)
	if s.dialFallback {
		outbound, _ = s.group.Select(N.NetworkUDP)
	}
	if outbound == nil {
		outbound, _ = s.group.Select(N.NetworkUDP)
	}
	if outbound == nil {
		return nil, E.New("missing supported outbound")
	}
	conn, err := outbound.ListenPacket(ctx, destination)
	if err == nil {
		return s.group.interruptGroup.NewPacketConn(conn, interrupt.IsExternalConnectionFromContext(ctx)), nil
	}
	s.logger.ErrorContext(ctx, err)
	s.group.history.DeleteURLTestHistory(outbound.Tag())
	return nil, err
}

func (s *URLTest) NewConnection(ctx context.Context, conn net.Conn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	ctx = interrupt.ContextWithIsExternalConnection(ctx)
	s.connection.NewConnection(ctx, s, conn, metadata, onClose)
}

func (s *URLTest) NewPacketConnection(ctx context.Context, conn N.PacketConn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	ctx = interrupt.ContextWithIsExternalConnection(ctx)
	s.connection.NewPacketConnection(ctx, s, conn, metadata, onClose)
}

type urlTestTicker interface {
	Chan() <-chan time.Time
	Stop()
}

type realURLTestTicker struct {
	*time.Ticker
}

func (t *realURLTestTicker) Chan() <-chan time.Time { return t.C }

func newRealURLTestTicker(interval time.Duration) urlTestTicker {
	return &realURLTestTicker{Ticker: time.NewTicker(interval)}
}

type URLTestGroup struct {
	fallback                     *fallbackState
	ctx                          context.Context
	outbound                     adapter.OutboundManager
	pause                        pause.Manager
	pauseCallback                *list.Element[pause.Callback]
	logger                       log.Logger
	outbounds                    []adapter.Outbound
	link                         string
	interval                     time.Duration
	tolerance                    uint16
	idleTimeout                  time.Duration
	history                      *urltest.HistoryStorage
	checking                     atomic.Bool
	probeAccess                  sync.Mutex
	probeCancel                  context.CancelFunc
	pendingRecovery              context.Context
	selectionAccess              sync.RWMutex
	selectedOutboundTCP          adapter.Outbound
	selectedOutboundUDP          adapter.Outbound
	interruptGroup               *interrupt.Group
	interruptExternalConnections bool
	access                       sync.Mutex
	updateAccess                 sync.Mutex
	ticker                       urlTestTicker
	tickerClose                  chan struct{}
	close                        chan struct{}
	loopGeneration               uint64
	started                      bool
	now                          func() time.Time
	newTicker                    func(time.Duration) urlTestTicker
	periodicCheck                func(context.Context)
}

func NewURLTestGroup(ctx context.Context, outboundManager adapter.OutboundManager, logger log.Logger, outbounds []adapter.Outbound, link string, interval time.Duration, tolerance uint16, idleTimeout time.Duration, interruptExternalConnections bool) (*URLTestGroup, error) {
	if interval == 0 {
		interval = C.DefaultURLTestInterval
	}
	if tolerance == 0 {
		tolerance = 50
	}
	if idleTimeout == 0 {
		idleTimeout = C.DefaultURLTestIdleTimeout
	}
	if interval > idleTimeout {
		return nil, E.New("interval must be less or equal than idle_timeout")
	}
	history := service.PtrFromContext[urltest.HistoryStorage](ctx)
	if history == nil {
		return nil, E.New("missing URL test history storage")
	}
	return &URLTestGroup{
		ctx:                          ctx,
		outbound:                     outboundManager,
		logger:                       logger,
		outbounds:                    outbounds,
		link:                         link,
		interval:                     interval,
		tolerance:                    tolerance,
		idleTimeout:                  idleTimeout,
		history:                      history,
		close:                        make(chan struct{}),
		now:                          time.Now,
		newTicker:                    newRealURLTestTicker,
		pause:                        service.FromContext[pause.Manager](ctx),
		interruptGroup:               interrupt.NewGroup(),
		interruptExternalConnections: interruptExternalConnections,
	}, nil
}

func (g *URLTestGroup) PostStart() {
	g.access.Lock()
	if g.started {
		g.access.Unlock()
		return
	}
	g.started = true
	if g.close == nil {
		g.close = make(chan struct{})
	}
	if g.now == nil {
		g.now = time.Now
	}
	if g.newTicker == nil {
		g.newTicker = newRealURLTestTicker
	}
	g.startPeriodicLocked()
	check := g.periodicCheck
	ctx := g.ctx
	g.access.Unlock()
	if check != nil {
		go check(ctx)
	} else {
		go g.CheckOutbounds(ctx, false)
	}
}

// Touch remains for callers that report real traffic, but scheduling is owned by
// the connected group lifecycle and never depends on traffic or idle activity.
func (g *URLTestGroup) Touch() {}

func (g *URLTestGroup) startPeriodicLocked() {
	if !g.started || g.ticker != nil || g.interval <= 0 {
		return
	}
	ticker := g.newTicker(g.interval)
	g.ticker = ticker
	g.tickerClose = make(chan struct{})
	g.loopGeneration++
	generation := g.loopGeneration
	if realTicker, ok := ticker.(*realURLTestTicker); ok && g.pause != nil {
		g.pauseCallback = pause.RegisterTicker(g.pause, realTicker.Ticker, g.interval, nil)
	}
	go g.loopCheck(ticker, g.close, g.tickerClose, generation)
}

func (g *URLTestGroup) stopPeriodicLocked() {
	if g.tickerClose != nil {
		close(g.tickerClose)
		g.tickerClose = nil
	}
	if g.ticker != nil {
		g.ticker.Stop()
		g.ticker = nil
	}
	if g.pauseCallback != nil {
		g.pause.UnregisterCallback(g.pauseCallback)
		g.pauseCallback = nil
	}
	g.loopGeneration++
}

// SetInterval applies a validated runtime configuration change by replacing the
// active schedule. The next automatic batch is due one complete new interval later.
func (g *URLTestGroup) SetInterval(interval time.Duration) error {
	if interval <= 0 || (g.idleTimeout > 0 && interval > g.idleTimeout) {
		return E.New("interval must be positive and less or equal than idle_timeout")
	}
	g.access.Lock()
	defer g.access.Unlock()
	if g.interval == interval {
		return nil
	}
	g.interval = interval
	g.stopPeriodicLocked()
	g.startPeriodicLocked()
	return nil
}

func (g *URLTestGroup) Close() error {
	g.access.Lock()
	if !g.started {
		g.access.Unlock()
		return nil
	}
	g.started = false
	g.stopPeriodicLocked()
	closeChan := g.close
	g.close = nil
	g.access.Unlock()
	if closeChan != nil {
		close(closeChan)
	}
	g.probeAccess.Lock()
	if g.probeCancel != nil {
		g.probeCancel()
	}
	g.pendingRecovery = nil
	g.probeAccess.Unlock()
	return nil
}

func (g *URLTestGroup) Select(network string) (adapter.Outbound, bool) {
	g.selectionAccess.RLock()
	defer g.selectionAccess.RUnlock()
	return g.selectLocked(network)
}

func (g *URLTestGroup) selectLocked(network string) (adapter.Outbound, bool) {
	var minDelay uint16
	var minOutbound adapter.Outbound
	switch network {
	case N.NetworkTCP:
		if g.selectedOutboundTCP != nil {
			if history := g.history.LoadURLTestHistory(RealTag(g.outbound, g.selectedOutboundTCP)); history != nil {
				minOutbound = g.selectedOutboundTCP
				minDelay = history.Delay
			}
		}
	case N.NetworkUDP:
		if g.selectedOutboundUDP != nil {
			if history := g.history.LoadURLTestHistory(RealTag(g.outbound, g.selectedOutboundUDP)); history != nil {
				minOutbound = g.selectedOutboundUDP
				minDelay = history.Delay
			}
		}
	}
	for _, detour := range g.outbounds {
		if !common.Contains(detour.Network(), network) {
			continue
		}
		history := g.history.LoadURLTestHistory(RealTag(g.outbound, detour))
		if history == nil {
			continue
		}
		if minDelay == 0 || minDelay > history.Delay+g.tolerance {
			minDelay = history.Delay
			minOutbound = detour
		}
	}
	if minOutbound == nil {
		for _, detour := range g.outbounds {
			if !common.Contains(detour.Network(), network) {
				continue
			}
			return detour, false
		}
		return nil, false
	}
	return minOutbound, true
}

func (g *URLTestGroup) loopCheck(ticker urlTestTicker, closeChan, tickerClose <-chan struct{}, generation uint64) {
	for {
		select {
		case <-closeChan:
			return
		case <-tickerClose:
			return
		case <-ticker.Chan():
		}
		g.access.Lock()
		active := g.started && g.ticker == ticker && g.loopGeneration == generation
		check := g.periodicCheck
		ctx := g.ctx
		g.access.Unlock()
		if !active {
			return
		}
		if check != nil {
			check(ctx)
		} else {
			g.CheckOutbounds(ctx, false)
		}
	}
}

func (g *URLTestGroup) CheckOutbounds(ctx context.Context, force bool) {
	_, _ = g.urlTest(ctx, force)
}

func (g *URLTestGroup) URLTest(ctx context.Context) (map[string]uint16, error) {
	return g.urlTest(ctx, true)
}

// requestRecovery coalesces overlapping interface notifications to the latest
// live context. It cancels probes, never clears health or invents a delay.
func (g *URLTestGroup) requestRecovery(ctx context.Context) {
	g.probeAccess.Lock()
	defer g.probeAccess.Unlock()
	if ctx.Err() != nil {
		return
	}
	if g.probeCancel != nil {
		g.probeCancel()
	}
	g.pendingRecovery = ctx
	if !g.checking.Load() {
		g.pendingRecovery = nil
		g.startProbeLocked(ctx, true)
	}
}

func (g *URLTestGroup) startProbeLocked(ctx context.Context, force bool) {
	ctx, g.probeCancel = context.WithCancel(ctx)
	g.checking.Store(true)
	go g.runProbe(ctx, force)
}

func (g *URLTestGroup) finishProbe() {
	g.probeAccess.Lock()
	defer g.probeAccess.Unlock()
	g.probeCancel()
	g.probeCancel = nil
	g.checking.Store(false)
	if ctx := g.pendingRecovery; ctx != nil {
		g.pendingRecovery = nil
		if ctx.Err() == nil {
			g.startProbeLocked(ctx, true)
		}
	}
}

func (g *URLTestGroup) urlTest(ctx context.Context, force bool) (map[string]uint16, error) {
	g.probeAccess.Lock()
	if g.checking.Load() {
		g.probeAccess.Unlock()
		return make(map[string]uint16), nil
	}
	ctx, g.probeCancel = context.WithCancel(ctx)
	g.checking.Store(true)
	g.probeAccess.Unlock()
	return g.runProbe(ctx, force)
}

func (g *URLTestGroup) runProbe(ctx context.Context, force bool) (map[string]uint16, error) {
	defer g.finishProbe()
	ctx = context.WithValue(ctx, probeCommitKey{}, &g.probeAccess)
	var result map[string]uint16
	if g.fallback == nil {
		result = URLTestOutbounds(ctx, g.outbound, g.history, g.logger, g.outbounds, g.link, g.interval, force)
	} else {
		// Fallback groups contain only leaves. Commit probe health through the
		// observation sequence so an older probe cannot undo a newer dial failure.
		result = make(map[string]uint16)
		b, _ := batch.New(ctx, batch.WithConcurrencyNum[any](10))
		checked := make(map[string]bool)
		var resultAccess sync.Mutex
		for _, detour := range g.outbounds {
			tag := detour.Tag()
			if checked[tag] {
				continue
			}
			history := g.history.LoadURLTestHistory(tag)
			if !force && history != nil && time.Since(history.Time) < g.interval {
				continue
			}
			checked[tag] = true
			b.Go(tag, func() (any, error) {
				started := g.beginFallbackObservation()
				testCtx, cancel := context.WithTimeout(ctx, C.TCPTimeout)
				defer cancel()
				delay, err := urltest.URLTest(testCtx, g.link, detour)
				unlock := lockProbeCommit(ctx)
				defer unlock()
				if ctx.Err() != nil {
					return nil, nil
				}
				if ctx.Err() == nil && !errors.Is(err, context.Canceled) && !errors.Is(err, dialer.ErrNoAvailableInterface) {
					g.recordFallback(detour, started, err == nil, &adapter.URLTestHistory{Time: time.Now(), Delay: delay})
				}
				if err == nil {
					resultAccess.Lock()
					result[tag] = delay
					resultAccess.Unlock()
				}
				return nil, nil
			})
		}
		b.Wait()
	}
	g.performUpdateCheck()
	return result, nil
}

type probeCommitKey struct{}

// Serialize publication with network supersession. Context cancellation alone
// checked outside this lock leaves a check-then-store race.
func lockProbeCommit(ctx context.Context) func() {
	if mu, ok := ctx.Value(probeCommitKey{}).(*sync.Mutex); ok {
		mu.Lock()
		return mu.Unlock
	}
	return func() {}
}

type urlTestResult struct {
	delay uint16
	err   error
}

type urlTestBatch struct {
	ctx      context.Context
	outbound adapter.OutboundManager
	history  *urltest.HistoryStorage
	logger   log.Logger
	batch    *batch.Batch[any]
	checked  map[string]bool
	groups   []adapter.OutboundGroup
	access   sync.Mutex
	result   map[string]uint16
}

func URLTestOutbounds(ctx context.Context, outboundManager adapter.OutboundManager, history *urltest.HistoryStorage, logger log.Logger, outbounds []adapter.Outbound, link string, interval time.Duration, force bool) map[string]uint16 {
	b, _ := batch.New(ctx, batch.WithConcurrencyNum[any](10))
	testBatch := &urlTestBatch{
		ctx:      ctx,
		outbound: outboundManager,
		history:  history,
		logger:   logger,
		batch:    b,
		checked:  make(map[string]bool),
		result:   make(map[string]uint16),
	}
	testBatch.test(outbounds, link, interval, force)
	b.Wait()
	for _, outboundGroup := range testBatch.groups {
		groupHistory := history.LoadURLTestHistory(RealTag(outboundManager, outboundGroup))
		if groupHistory != nil {
			testBatch.result[outboundGroup.Tag()] = groupHistory.Delay
		}
	}
	return testBatch.result
}

func (b *urlTestBatch) test(outbounds []adapter.Outbound, link string, interval time.Duration, force bool) {
	for _, detour := range outbounds {
		tag := detour.Tag()
		if b.checked[tag] {
			continue
		}
		switch nested := detour.(type) {
		case *URLTest:
			b.checked[tag] = true
			b.groups = append(b.groups, nested)
			b.batch.Go(tag, func() (any, error) {
				nestedResult, _ := nested.group.urlTest(b.ctx, force)
				b.access.Lock()
				maps.Copy(b.result, nestedResult)
				b.access.Unlock()
				return nil, nil
			})
		case adapter.OutboundGroup:
			b.checked[tag] = true
			b.groups = append(b.groups, nested)
			b.test(common.FilterNotNil(common.Map(nested.All(), func(it string) adapter.Outbound {
				member, _ := b.outbound.Outbound(it)
				return member
			})), link, interval, force)
		default:
			history := b.history.LoadURLTestHistory(tag)
			if !force && history != nil && time.Since(history.Time) < interval {
				continue
			}
			b.checked[tag] = true
			b.batch.Go(tag, func() (any, error) {
				testCtx, cancel := context.WithTimeout(b.ctx, C.TCPTimeout)
				defer cancel()
				testChan := make(chan urlTestResult, 1)
				go func() {
					delay, testErr := urltest.URLTest(testCtx, link, detour)
					testChan <- urlTestResult{delay, testErr}
				}()
				var testResult urlTestResult
				select {
				case testResult = <-testChan:
				case <-testCtx.Done():
					testResult.err = testCtx.Err()
				}
				unlock := lockProbeCommit(b.ctx)
				defer unlock()
				if b.ctx.Err() != nil {
					return nil, nil
				}
				if testResult.err != nil {
					b.logger.Debug("outbound ", tag, " unavailable: ", testResult.err)
					b.history.DeleteURLTestHistory(tag)
				} else {
					b.logger.Debug("outbound ", tag, " available: ", testResult.delay, "ms")
					b.history.StoreURLTestHistory(tag, &adapter.URLTestHistory{
						Time:  time.Now(),
						Delay: testResult.delay,
					})
					b.access.Lock()
					b.result[tag] = testResult.delay
					b.access.Unlock()
				}
				return nil, nil
			})
		}
	}
}

func (g *URLTestGroup) performUpdateCheck() {
	g.updateAccess.Lock()
	defer g.updateAccess.Unlock()
	if g.fallback != nil {
		g.refreshFallback()
		return
	}
	g.selectionAccess.Lock()
	defer g.selectionAccess.Unlock()
	var (
		updated  bool
		selected bool
	)
	if outbound, exists := g.selectLocked(N.NetworkTCP); outbound != nil && (g.selectedOutboundTCP == nil || (exists && outbound != g.selectedOutboundTCP)) {
		if g.selectedOutboundTCP != nil {
			updated = true
		}
		g.selectedOutboundTCP = outbound
		selected = true
	}
	if outbound, exists := g.selectLocked(N.NetworkUDP); outbound != nil && (g.selectedOutboundUDP == nil || (exists && outbound != g.selectedOutboundUDP)) {
		if g.selectedOutboundUDP != nil {
			updated = true
		}
		g.selectedOutboundUDP = outbound
		selected = true
	}
	if updated {
		g.interruptGroup.Interrupt(g.interruptExternalConnections)
	}
	if selected {
		g.history.NotifyUpdated()
	}
}
