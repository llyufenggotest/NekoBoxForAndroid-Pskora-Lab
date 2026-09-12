package boxapi

import (
	"context"
	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/group"
	"github.com/sagernet/sing/service"
	"testing"
)

type selectionRecorder struct{ calls []string }

func (r *selectionRecorder) OnSelectorChanged(selector, tag string) {
	r.calls = append(r.calls, selector+":"+tag)
}
func TestSelectorCoreCallback(t *testing.T) {
	ctx := include.Context(context.Background())
	r := new(selectionRecorder)
	service.MustRegister[group.SelectionListener](ctx, r)
	var opts option.Options
	if err := opts.UnmarshalJSONContext(ctx, []byte(`{"outbounds":[{"type":"direct","tag":"a"},{"type":"direct","tag":"b"},{"type":"selector","tag":"proxy","outbounds":["a","b"]}]}`)); err != nil {
		t.Fatal(err)
	}
	b, err := box.New(box.Options{Context: ctx, Options: opts})
	if err != nil {
		t.Fatal(err)
	}
	if err = b.Start(); err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	o, _ := b.Outbound().Outbound("proxy")
	s := o.(*group.Selector)
	if !s.SelectOutbound("b") || !s.SelectOutbound("b") || s.SelectOutbound("missing") {
		t.Fatal("selection results")
	}
	if len(r.calls) != 1 || r.calls[0] != "proxy:b" {
		t.Fatalf("callbacks %v", r.calls)
	}
}
