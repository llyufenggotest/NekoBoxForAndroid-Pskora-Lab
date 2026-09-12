// Package diagnostic contains bounded, metadata-only lab observations. It never accepts payloads, URLs or raw errors for logging.
package diagnostic

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync/atomic"
)

type Logger interface{ Debug(...any) }
type gate interface{ DiagnosticEnabled() bool }

func Enabled(l Logger) bool { g, ok := l.(gate); return ok && g.DiagnosticEnabled() }

var seq atomic.Uint64
var generation atomic.Uint64

// Hard process-lifetime cap. Restart the core for a new capture; switching levels cannot bypass it.
var emitted atomic.Uint64

const Limit uint64 = 4096

const (
	AppUnknown uint8 = iota
	AppTelegram
	AppChrome
)

const (
	SubAppUnknown uint8 = iota
	SubAppTelegramOfficial
	SubAppTelegramNekogram
)

// ClassifyPackages maps only exact package names to fixed numeric classes.
// UID remains runtime metadata and is never inferred from these enums.
func ClassifyPackages(packages []string) (app, subApp uint8) {
	for _, name := range packages {
		switch name {
		case "org.telegram.messenger":
			return AppTelegram, SubAppTelegramOfficial
		case "tw.nekomimi.nekogram":
			return AppTelegram, SubAppTelegramNekogram
		case "com.android.chrome":
			app, subApp = AppChrome, SubAppUnknown
		}
	}
	return app, subApp
}

func Generation() uint64 { return generation.Load() }
func Advance() uint64    { return generation.Add(1) }
func Next() uint64       { return seq.Add(1) }

type Event uint8

const (
	Start Event = iota + 1
	GotConn
	Response
	FirstByte
	Read
	Write
	Close
	NATCreate
	NATPurge
	NATMiss
	NATExpire
	Network
	Route
	TUNCreate
	HTTPRequestError
	AppFacingClose
)

type Record struct {
	Event                 Event
	Flow, Generation, NAT uint64
	Port, Source          uint16
	App, SubApp           uint8
	UID                   int32
	OwnerKnown            bool
	Value                 int
	Flags                 uint8
	Reused, Idle          bool
	Error                 uint8
}

func ErrorCode(err error) uint8 {
	if err == nil {
		return 0
	}
	if errors.Is(err, io.EOF) {
		return 1
	}
	if errors.Is(err, context.Canceled) {
		return 2
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return 3
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return 4
	}
	if errors.Is(err, net.ErrClosed) || errors.Is(err, io.ErrClosedPipe) {
		return 5
	}
	return 6
}
func Emit(l Logger, r Record) {
	if !Enabled(l) {
		return
	}
	for {
		n := emitted.Load()
		if n >= Limit {
			return
		}
		if emitted.CompareAndSwap(n, n+1) {
			break
		}
	}
	// Schema v3 adds explicit owner provenance. All fields remain typed numeric/boolean;
	// no source/destination address, name, credential or error string is accepted.
	l.Debug(fmt.Sprintf("DTRACE3 schema=3 event=%d flow=%d gen=%d nat=%d port=%d sport=%d app=%d subapp=%d uid=%d owner=%t value=%d flags=%d reused=%t idle=%t error=%d", r.Event, r.Flow, r.Generation, r.NAT, r.Port, r.Source, r.App, r.SubApp, r.UID, r.OwnerKnown, r.Value, r.Flags, r.Reused, r.Idle, r.Error))
}

type key struct{}
type correlationKey struct{}

type Correlation struct {
	ID, Gen    uint64
	Source     uint16
	App        uint8
	SubApp     uint8
	UID        int32
	OwnerKnown bool
}

func WithCorrelation(ctx context.Context, c Correlation) context.Context {
	return context.WithValue(ctx, correlationKey{}, c)
}

func CorrelationFrom(ctx context.Context) (Correlation, bool) {
	c, ok := ctx.Value(correlationKey{}).(Correlation)
	return c, ok
}

func EventFrom(ctx context.Context, l Logger, r Record) {
	if c, ok := CorrelationFrom(ctx); ok && c.OwnerKnown {
		r.Flow, r.Generation, r.Source = c.ID, c.Gen, c.Source
		r.App, r.SubApp, r.UID, r.OwnerKnown = c.App, c.SubApp, c.UID, true
		Emit(l, r)
	}
}

// BeginTUN attaches one immutable connection identity to the existing context.
// The identity follows routing/transport without changing packet, NAT, or wire state.
func BeginTUN(ctx context.Context, l Logger, source uint16, packages []string, uid int32, ownerKnown bool) context.Context {
	if !Enabled(l) || !ownerKnown {
		return ctx
	}
	app, subApp := ClassifyPackages(packages)
	c := Correlation{ID: Next(), Gen: Generation(), Source: source, App: app, SubApp: subApp, UID: uid, OwnerKnown: ownerKnown}
	Emit(l, Record{Event: TUNCreate, Flow: c.ID, Generation: c.Gen, Source: c.Source, App: c.App, SubApp: c.SubApp, UID: c.UID, OwnerKnown: c.OwnerKnown})
	return WithCorrelation(ctx, c)
}

type Flow struct {
	ID, Gen                                    uint64
	Source                                     uint16
	App, SubApp                                uint8
	UID                                        int32
	OwnerKnown                                 bool
	Logger                                     Logger
	count                                      atomic.Uint32
	readOK, readErr, writeOK, writeErr, closed atomic.Bool
}

func New(ctx context.Context, l Logger, source uint16, app uint8, uid int32, subApp uint8, ownerKnown bool, entry int) (context.Context, *Flow) {
	if !Enabled(l) {
		return ctx, nil
	}
	c, correlated := CorrelationFrom(ctx)
	var id, gen uint64
	if correlated && c.OwnerKnown {
		id, gen = c.ID, c.Gen
		source, app, subApp, uid, ownerKnown = c.Source, c.App, c.SubApp, c.UID, c.OwnerKnown
	} else if ownerKnown {
		id, gen = Next(), Generation()
	} else {
		return ctx, nil
	}
	f := &Flow{ID: id, Gen: gen, Source: source, App: app, SubApp: subApp, UID: uid, OwnerKnown: ownerKnown, Logger: l}
	f.Event(Record{Event: Start, Value: entry})
	return context.WithValue(ctx, key{}, f), f
}
func From(ctx context.Context) *Flow { f, _ := ctx.Value(key{}).(*Flow); return f }
func (f *Flow) Event(r Record) {
	if f == nil || !Enabled(f.Logger) {
		return
	}
	if f.count.Add(1) > 24 {
		return
	}
	r.Flow = f.ID
	r.Generation = f.Gen
	r.Source = f.Source
	r.App = f.App
	r.SubApp = f.SubApp
	r.UID = f.UID
	r.OwnerKnown = f.OwnerKnown
	Emit(f.Logger, r)
}
func (f *Flow) IO(write bool, n int, err error) {
	if f == nil {
		return
	}
	ok, e := &f.readOK, &f.readErr
	event := Read
	if write {
		ok, e, event = &f.writeOK, &f.writeErr, Write
	}
	if n > 0 && ok.CompareAndSwap(false, true) {
		f.Event(Record{Event: event, Value: 1})
	}
	if err != nil && e.CompareAndSwap(false, true) {
		f.Event(Record{Event: event, Error: ErrorCode(err)})
	}
}
func (f *Flow) Closed(err error) {
	if f != nil && f.closed.CompareAndSwap(false, true) {
		f.Event(Record{Event: Close, Error: ErrorCode(err)})
	}
}
