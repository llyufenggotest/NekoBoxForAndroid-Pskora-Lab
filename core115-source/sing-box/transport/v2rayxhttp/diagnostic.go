package xhttp

import (
	"context"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	D "github.com/sagernet/sing-tun/diagnostic"
	"github.com/sagernet/sing/service"
	"net/http/httptrace"
)

func diagnosticFlow(ctx context.Context, l log.ContextLogger) (context.Context, *D.Flow) {
	if l == nil {
		if factory := service.FromContext[log.Factory](ctx); factory != nil {
			l = factory.NewLogger("diagnostic")
		}
	}

	var source uint16
	var app, subApp uint8
	var uid int32
	var ownerKnown bool
	if m := adapter.ContextFrom(ctx); m != nil {
		source = m.Source.Port
		if m.ProcessInfo != nil {
			app, subApp = D.ClassifyPackages(m.ProcessInfo.PackageNames)
			uid = m.ProcessInfo.UserId
			ownerKnown = true
		}
	}
	return D.New(ctx, l, source, app, uid, subApp, ownerKnown, 1)
}
func diagnosticHTTP(ctx context.Context) context.Context {
	f := D.From(ctx)
	if f == nil {
		return ctx
	}
	return httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		GotConn:              func(i httptrace.GotConnInfo) { f.Event(D.Record{Event: D.GotConn, Reused: i.Reused, Idle: i.WasIdle}) },
		GotFirstResponseByte: func() { f.Event(D.Record{Event: D.FirstByte}) },
	})
}
