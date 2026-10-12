package option

import (
	"context"
	"strings"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/json"
)

// XHttpOutboundOptions is the compatibility storage for the Blackstone contract.
type XHttpOutboundOptions struct {
	DialerOptions `json:",inline"`
	ServerOptions `json:",inline"`
	Name          string             `json:"name,omitempty"`
	Password      string             `json:"password"`
	NodeID        string             `json:"node-id,omitempty"`
	DeviceID      string             `json:"device_id,omitempty"`
	DeviceName    string             `json:"device_name,omitempty"`
	Network       NetworkList        `json:"network,omitempty"`
	UDPOverTCP    *UDPOverTCPOptions `json:"udp_over_tcp,omitempty"`
}

type BlackstoneOutboundOptions struct {
	XHttpOutboundOptions
}

func (o *BlackstoneOutboundOptions) UnmarshalJSONContext(ctx context.Context, content []byte) error {
	if err := o.XHttpOutboundOptions.UnmarshalJSONContext(ctx, content); err != nil {
		return err
	}
	if o.DeviceID == "" || o.DeviceName == "" {
		return E.New("Blackstone requires device_id and device_name")
	}
	return nil
}

func (o *XHttpOutboundOptions) UnmarshalJSONContext(ctx context.Context, content []byte) error {
	type plain XHttpOutboundOptions
	if err := json.UnmarshalContext(ctx, content, (*plain)(o)); err != nil {
		return err
	}
	if (o.DeviceID == "") != (o.DeviceName == "") {
		return E.New("device_id and device_name must be provided together")
	}
	if o.DeviceID != "" && (strings.TrimSpace(o.DeviceID) == "" || strings.TrimSpace(o.DeviceName) == "") {
		return E.New("device_id and device_name must be non-empty strings")
	}
	return nil
}
