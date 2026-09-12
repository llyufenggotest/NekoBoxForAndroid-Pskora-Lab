package configcompat

import (
	"context"
	"encoding/json"
	"github.com/sagernet/sing-box/common/dialer"
)

// ConvertContext binds Android's dialing preference to this instance only.
func ConvertContext(ctx context.Context, data []byte, geoip, geosite GeoLoader) (context.Context, []byte, error) {
	var o struct {
		Route struct {
			Concurrent bool `json:"concurrent_dial"`
		} `json:"route"`
	}
	if err := json.Unmarshal(data, &o); err != nil {
		return ctx, nil, err
	}
	converted, err := Convert(data, geoip, geosite)
	return dialer.WithConcurrentDial(ctx, o.Route.Concurrent), converted, err
}
