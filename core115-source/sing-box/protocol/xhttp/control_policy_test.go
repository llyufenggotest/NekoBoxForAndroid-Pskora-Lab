package xhttp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func controlDocumentResponse(t *testing.T, document string) []byte {
	t.Helper()
	smart := encryptBlackstonePayloadFixture(t, []byte(document))
	layer, err := json.Marshal(map[string]any{"data": map[string]any{"smart": base64.StdEncoding.EncodeToString(smart)}})
	require.NoError(t, err)
	return encryptedControlResponse(t, layer)
}

const controlSelectedDocument = `mode: rule
rules: ["MATCH,Main"]
proxy-groups:
 - name: Main
   type: url-test
   proxies: [second, first]
proxies:
 - {name: first, type: xhttp, server: first.invalid, port: 443, password: "fixture:0102"}
 - {name: second, type: xhttp, server: second.invalid, port: 444, password: "fixture:0102"}
 - {name: unrelated, type: xhttp, server: other.invalid, port: 445, password: "fixture:0102"}
`

func TestControlDeviceOriginAndNonReplay(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		require.Nil(t, req.GetBody, "configuration POST must not be rewindable")
		require.Nil(t, req.URL.User)
		require.Nil(t, net.ParseIP(req.URL.Hostname()), "device origin must be audited DNS origin, not pinned IP")
		require.Equal(t, req.URL.Host, req.Host, "URL and authority must agree")
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(controlDocumentResponse(t, controlSelectedDocument))))}, nil
	})}
	_, err := fetchDynamicConfig(context.Background(), client, "unrelated label", "id-fixture", "token-fixture", "device-fixture", "tablet-fixture")
	require.NoError(t, err)
}

func TestControlExactMainMembersProduction(t *testing.T) {
	configs, err := fetchDynamicConfig(context.Background(), responseClient(controlDocumentResponse(t, controlSelectedDocument)), "unrelated label", "id-fixture", "token-fixture", "device-fixture", "tablet-fixture")
	require.NoError(t, err)
	require.Len(t, configs, 2, "exact selected members cannot be filtered by display label")
	require.Equal(t, "second.invalid", configs[0].Server)
	require.Equal(t, "first.invalid", configs[1].Server)
}

func TestControlRejectMissingRoutingContract(t *testing.T) {
	doc := `proxies: [{name: only, type: xhttp, server: fixture.invalid, port: 443, password: "fixture:0102"}]`
	configs, err := fetchDynamicConfig(context.Background(), responseClient(controlDocumentResponse(t, doc)), "only", "id-fixture", "token-fixture", "device-fixture", "tablet-fixture")
	require.Error(t, err)
	require.Nil(t, configs)
}

func TestControlErrorsDoNotExposeCause(t *testing.T) {
	secret := errors.New("fixture-private-endpoint-token")
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, secret })}
	_, err := fetchDynamicConfig(context.Background(), client, "fixture-private-name", "id-fixture", "token-fixture", "device-fixture", "tablet-fixture")
	require.ErrorIs(t, err, secret)
	require.NotContains(t, err.Error(), secret.Error())
}
