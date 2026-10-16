package vless

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	SJ "github.com/sagernet/sing/common/json"
	"github.com/sagernet/sing/service"
)

func TestBlackstoneTypedOptionsStrict(t *testing.T) {
	registry := outbound.NewRegistry()
	RegisterOutbound(registry)
	ctx := service.ContextWith[option.OutboundOptionsRegistry](context.Background(), registry)
	for _, fields := range []string{`"e":null,"h":"01"`, `"e":"true","h":"01"`, `"e":1,"h":"01"`, `"e":true,"h":null`, `"e":false,"h":1`, `"e":false,"h":true`, `"e":false,"h":{"secret-marker":"hidden"}`} {
		t.Run(fields, func(t *testing.T) {
			b := []byte(`{"type":"vless","uuid":"` + bsID + `#blackstone",` + fields + `}`)
			var o option.Outbound
			if err := SJ.UnmarshalContext(ctx, b, &o); err == nil {
				t.Error("invalid private typed value accepted by registry")
			} else if strings.Contains(fmt.Sprintf("%+v %#v", err, err), "hidden") {
				t.Error("private option leaked")
			}
			var direct option.VLESSOutboundOptions
			if err := json.Unmarshal([]byte(`{"uuid":"`+bsID+`#blackstone",`+fields+`}`), &direct); err == nil {
				t.Error("invalid private typed value accepted by direct JSON")
			}
		})
	}
	for _, e := range []string{"true", "false"} {
		var o option.VLESSOutboundOptions
		if err := json.Unmarshal([]byte(`{"uuid":"`+bsID+`#blackstone","e":`+e+`,"h":"01"}`), &o); err != nil {
			t.Fatal(err)
		}
	}
}
func TestBlackstoneProductionRejectsMixedTunNet(t *testing.T) {
	_, err := NewOutbound(context.Background(), nil, log.NewNOPFactory().Logger(), "test", option.VLESSOutboundOptions{UUID: bsID + "#blackstone", H: "01", TunNet: &option.VLESSTunNetOptions{}})
	if err == nil || !strings.Contains(err.Error(), "Blackstone:") {
		t.Fatal("TunNet mixed mode not rejected before legacy normalization")
	}
}

func TestBlackstoneProductionRejectsRawMalformedSelectors(t *testing.T) {
	for _, id := range []string{" " + bsID + "#blackstone", bsID + "#blackstone\u2003", bsID + "#BLACKSTONE", bsID + "#blackstone#x365", bsID + "#juzi#blackstone"} {
		_, err := NewOutbound(context.Background(), nil, log.NewNOPFactory().Logger(), "test", option.VLESSOutboundOptions{UUID: id, E: true, H: "01"})
		if err == nil {
			t.Error("production accepted malformed selector")
		}
	}
	for _, h := range []string{"", "private-secret-marker", "1"} {
		_, err := NewOutbound(context.Background(), nil, log.NewNOPFactory().Logger(), "test", option.VLESSOutboundOptions{UUID: bsID + "#blackstone", H: h})
		if err == nil {
			t.Error("production accepted invalid H")
		} else if h != "" && strings.Contains(fmt.Sprintf("%+v %#v", err, err), h) {
			t.Error("H leaked")
		}
	}
}
