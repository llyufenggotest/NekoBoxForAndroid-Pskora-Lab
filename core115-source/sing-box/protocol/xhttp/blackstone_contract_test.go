package xhttp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBlackstoneControlHeadersConsumeDevicePair(t *testing.T) {
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.invalid", nil)
	require.NoError(t, err)
	applyControlHeaders(request, "token-fixture", "device-fixture", "tablet-fixture")
	require.Equal(t, "540", request.Header.Get("x-version"))
	raw, err := base64.StdEncoding.DecodeString(request.Header.Get("x-header"))
	require.NoError(t, err)
	for index := range raw {
		raw[index] ^= PUB_KEY[index%len(PUB_KEY)]
	}
	var headers map[string]any
	require.NoError(t, json.Unmarshal(raw, &headers))
	require.Equal(t, "device-fixture", headers["X-IDENTIFIER"])
	require.Equal(t, "tablet-fixture", headers["X-DEVICE-NAME"])
	require.Equal(t, "token-fixture", headers["X-TOKEN"])
}

func TestCanceledControlContextStopsBeforeRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := newControlRequest(ctx, "https://example.invalid", []byte(`{}`), "token-fixture", "device-fixture", "tablet-fixture")
	require.ErrorIs(t, err, context.Canceled)
}
