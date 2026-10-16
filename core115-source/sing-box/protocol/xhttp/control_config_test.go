package xhttp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestControlSelectionAdversarialDocuments(t *testing.T) {
	cases := map[string]string{
		"mode":                      strings.Replace(controlSelectedDocument, "mode: rule", "mode: global", 1),
		"missing-match":             strings.Replace(controlSelectedDocument, "rules: [\"MATCH,Main\"]", "rules: [\"DOMAIN,example.invalid,Main\"]", 1),
		"non-final-match":           strings.Replace(controlSelectedDocument, "rules: [\"MATCH,Main\"]", "rules: [\"MATCH,Main\", \"DOMAIN,example.invalid,Main\"]", 1),
		"multiple-match":            strings.Replace(controlSelectedDocument, "rules: [\"MATCH,Main\"]", "rules: [\"MATCH,Main\", \"MATCH,Main\"]", 1),
		"select":                    strings.Replace(controlSelectedDocument, "type: url-test", "type: select", 1),
		"provider":                  strings.Replace(controlSelectedDocument, "type: url-test", "type: url-test\n   use: [provider]", 1),
		"dangling":                  strings.Replace(controlSelectedDocument, "proxies: [second, first]", "proxies: [second, missing]", 1),
		"repeated-member":           strings.Replace(controlSelectedDocument, "proxies: [second, first]", "proxies: [first, first]", 1),
		"reserved":                  strings.Replace(controlSelectedDocument, "proxies: [second, first]", "proxies: [DIRECT]", 1),
		"nested":                    strings.Replace(controlSelectedDocument, "proxies: [second, first]", "proxies: [Main]", 1),
		"duplicate-key":             strings.Replace(controlSelectedDocument, "mode: rule", "mode: rule\nmode: rule", 1),
		"alias":                     strings.Replace(controlSelectedDocument, "mode: rule", "mode: &mode rule", 1),
		"extra-doc":                 controlSelectedDocument + "\n---\nmode: rule\n",
		"unsupported-selected":      strings.Replace(controlSelectedDocument, "type: xhttp", "type: unknown", 1),
		"invalid-selected-password": strings.Replace(controlSelectedDocument, "fixture:0102", "fixture:GG", 1),
		"typed-port":                strings.Replace(controlSelectedDocument, "port: 443", "port: 443.5", 1),
		"null-cipher":               strings.Replace(controlSelectedDocument, "type: xhttp", "type: ss, cipher: null", 1),
		"typed-cipher":              strings.Replace(controlSelectedDocument, "type: xhttp", "type: ss, cipher: 123", 1),
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg, err := fetchDynamicConfig(context.Background(), responseClient(controlDocumentResponse(t, doc)), "ignored", "id", "token", "device", "tablet")
			require.Error(t, err)
			require.Nil(t, cfg)
		})
	}
}

func TestControlLegacySelectionRemainsIsolated(t *testing.T) {
	doc := `proxies: [{name: legacy-node, type: os, server: fixture.invalid, port: 443, password: fixture}]`
	cfg, err := fetchDynamicConfig(context.Background(), responseClient(controlDocumentResponse(t, doc)), "not a matching name", "id", "token", "", "")
	require.NoError(t, err)
	require.Len(t, cfg, 1)
	require.Equal(t, "ss", cfg[0].Type)
	require.Equal(t, "aes-128-ctr", cfg[0].Cipher)
	_, err = fetchDynamicConfig(context.Background(), responseClient(controlDocumentResponse(t, doc)), "ignored", "id", "token", "device", "tablet")
	require.Error(t, err)
}

func TestControlTypedVLESSPreservesSecurityAndTransport(t *testing.T) {
	doc := `mode: rule
rules: ["MATCH,Main"]
proxy-groups: [{name: Main, type: url-test, proxies: [v]}]
proxies:
 - name: v
   type: vless
   server: fixture.invalid
   port: "443"
   uuid: 00000000-0000-0000-0000-000000000001
   e: false
   h: "010203"
   tls: true
   servername: fixture-sni.invalid
   alpn: [h2, http/1.1]
   flow: xtls-rprx-vision
   network: xhttp
   certificate: fixture-certificate
   private-key: fixture-private-key
   skip-cert-verify: false
   client-fingerprint: chrome
   reality-opts: {public-key: fixture-public-key, short-id: "0102"}
   xhttp-opts:
     host: fixture-authority.invalid
     path: /fixture
     mode: packet-up
     headers: {X-Fixture: present}
     download-settings: {server: fixture-download.invalid, port: 8443, tls: true, servername: fixture-download-sni.invalid}
`
	cfg, err := fetchDynamicConfig(context.Background(), responseClient(controlDocumentResponse(t, doc)), "ignored", "id", "token", "device", "tablet")
	require.NoError(t, err)
	require.Len(t, cfg, 1)
	v := cfg[0].VLESS
	require.NotNil(t, v)
	require.False(t, v.E)
	require.Equal(t, "010203", v.H)
	require.True(t, v.TLS)
	require.Equal(t, "fixture-sni.invalid", v.ServerName)
	require.Equal(t, []string{"h2", "http/1.1"}, v.ALPN)
	require.Equal(t, "xtls-rprx-vision", v.Flow)
	require.Equal(t, "xhttp", v.Network)
	require.Equal(t, "fixture-certificate", v.Certificate)
	require.Equal(t, "fixture-private-key", v.PrivateKey)
	require.Equal(t, "fixture-public-key", v.RealityOpts.PublicKey)
	require.Equal(t, "0102", v.RealityOpts.ShortID)
	require.Equal(t, "fixture-authority.invalid", v.XHTTPOpts.Host)
	require.Equal(t, "/fixture", v.XHTTPOpts.Path)
	require.Equal(t, "fixture-download.invalid", *v.XHTTPOpts.DownloadSettings.Server)
	require.NotContains(t, v.UUID, "#", "constructor must explicitly opt into private mode")
	for name, replacement := range map[string]string{"E type": "e: 1", "E missing": "unrelated: false", "H type": "h: 123", "H malformed": "h: GG", "UUID suffix": "uuid: 00000000-0000-0000-0000-000000000001#blackstone", "unknown-network": "network: unknown", "unknown-flow": "flow: xtls-rprx-vision-extra"} {
		t.Run(name, func(t *testing.T) {
			needle := "e: false"
			switch {
			case strings.HasPrefix(name, "H"):
				needle = "h: \"010203\""
			case strings.HasPrefix(name, "UUID"):
				needle = "uuid: 00000000-0000-0000-0000-000000000001"
			case name == "unknown-network":
				needle = "network: xhttp"
			case name == "unknown-flow":
				needle = "flow: xtls-rprx-vision"
			}
			_, err := fetchDynamicConfig(context.Background(), responseClient(controlDocumentResponse(t, strings.Replace(doc, needle, replacement, 1))), "ignored", "id", "token", "device", "tablet")
			require.Error(t, err)
		})
	}
}

func TestControlCertificateInnerPreservesWireFields(t *testing.T) {
	inner := map[string]any{"type": "xhttp", "server": "fixture.invalid", "port": 443, "password": "fixture:0102", "cipher": "aes-256-ctr", "udp": true, "udp-over-tcp": true, "udp-over-tcp-version": 2, "padding-len": "fixture-padding", "fake-net": map[string]string{"tcp": "0304", "udp": "0506"}}
	encode := func(p map[string]any) string {
		raw, err := json.Marshal(p)
		require.NoError(t, err)
		wire := append(make([]byte, 48), encryptBlackstonePayloadFixture(t, raw)...)
		return base64.StdEncoding.EncodeToString(wire)
	}
	cfg, err := parseSelectedConfig(map[string]any{"type": "xhttp", "certificate": encode(inner)}, false)
	require.NoError(t, err)
	require.Equal(t, "0304", cfg.TcpFake)
	require.Equal(t, "0506", cfg.UdpFake)
	require.True(t, cfg.UDP)
	require.True(t, cfg.UDPOverTCP)
	require.Equal(t, 2, cfg.UDPOverTCPVersion)
	require.Equal(t, "aes-256-ctr", cfg.Cipher)
	require.Equal(t, "fixture-padding", cfg.PaddingLen)
	inner["certificate"] = "recursive-fixture"
	_, err = parseSelectedConfig(map[string]any{"type": "xhttp", "certificate": encode(inner)}, false)
	require.Error(t, err)
}

func TestControlAllMembersPreflightBeforeReturn(t *testing.T) {
	configs, err := parseSelectedConfigs(context.Background(), []byte(controlSelectedDocument))
	require.NoError(t, err)
	var seen []string
	private := errors.New("fixture-private-constructor-detail")
	err = PreflightConfigs(context.Background(), configs, func(_ context.Context, cfg RealConfig) error {
		seen = append(seen, cfg.Name)
		if cfg.Name == "first" {
			return private
		}
		return nil
	})
	require.ErrorIs(t, err, private)
	require.NotContains(t, err.Error(), private.Error())
	require.Equal(t, []string{"second", "first"}, seen)
	require.Error(t, PreflightConfigs(context.Background(), configs, nil))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	seen = nil
	require.ErrorIs(t, PreflightConfigs(ctx, configs, func(context.Context, RealConfig) error { seen = append(seen, "unexpected"); return nil }), context.Canceled)
	require.Empty(t, seen)
}
