package vless

import (
	"strings"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
)

// Matches mihomo 06e351c0a7536e81eb914d80184435ea13220e25.
const x365UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

func prepareX365XHTTPTransport(transport option.V2RayTransportOptions, userID string) option.V2RayTransportOptions {
	// Keep the literal marker and wire UUID untouched. Juzi and ordinary
	// transports must retain their existing browser defaults.
	if transport.Type != C.V2RayTransportTypeXHTTP || !strings.HasSuffix(userID, "#x365") {
		return transport
	}
	headers := transport.XHTTPOptions.Headers
	for key := range headers {
		if strings.EqualFold(key, "User-Agent") {
			return transport
		}
	}
	prepared := make(map[string]string, len(headers)+1)
	for key, value := range headers {
		prepared[key] = value
	}
	prepared["User-Agent"] = x365UserAgent
	transport.XHTTPOptions.Headers = prepared
	return transport
}
