package vless

import (
	"fmt"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-vmess/vless"
)

func configurePure(o *option.VLESSOutboundOptions) (bool, error) {
	_, pure, err := vless.PureIdentity(o.UUID)
	if err != nil || !pure {
		return pure, err
	}
	if o.TunNet != nil || o.Flow != "" || (o.Encryption != "" && o.Encryption != "none") || (o.Multiplex != nil && o.Multiplex.Enabled) {
		return true, fmt.Errorf("Pure: mixed private mode, flow, encryption or multiplex unsupported")
	}
	if o.TLS == nil || !o.TLS.Enabled || o.Transport == nil || o.Transport.Type != "ws" {
		return true, fmt.Errorf("Pure: requires TLS WebSocket transport")
	}
	t := *o.TLS
	w := *o.Transport
	if (t.UTLS != nil && t.UTLS.Enabled) || (t.Reality != nil && t.Reality.Enabled) || t.ECH != nil || t.Fragment || t.RecordFragment || t.Spoof != "" || t.DisableSNI || (t.Engine != "" && t.Engine != "go") {
		return true, fmt.Errorf("Pure: unsupported TLS combination")
	}
	if (t.MinVersion != "" && t.MinVersion != "1.3") || (t.MaxVersion != "" && t.MaxVersion != "1.3") || (len(t.ALPN) > 0 && (len(t.ALPN) != 1 || t.ALPN[0] != "http/1.1")) {
		return true, fmt.Errorf("Pure: requires TLS 1.3 and http/1.1")
	}
	if w.WebsocketOptions.MaxEarlyData != 0 || w.WebsocketOptions.EarlyDataHeaderName != "" || (w.WebsocketOptions.Path != "" && w.WebsocketOptions.Path != "/websocket") || len(w.WebsocketOptions.Headers) > 0 {
		return true, fmt.Errorf("Pure: unsupported WebSocket options")
	}
	if o.PacketEncoding != nil && *o.PacketEncoding != "" {
		return true, fmt.Errorf("Pure: UDP/XUDP unsupported")
	}
	t.MinVersion = "1.3"
	t.MaxVersion = "1.3"
	t.ALPN = []string{"http/1.1"}
	w.WebsocketOptions.Path = "/websocket"
	o.TLS = &t
	o.Transport = &w
	empty := ""
	o.PacketEncoding = &empty
	return true, nil
}
