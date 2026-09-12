package vless

import (
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"os"
	"testing"
	"time"

	"github.com/sagernet/sing-box/option"
	"github.com/stretchr/testify/require"
)

func TestTunNetSnapshotResolve(t *testing.T) {
	_ = base64.RawURLEncoding.EncodeToString([]byte("route"))
	echo := base64.RawURLEncoding.EncodeToString([]byte("ech"))
	key := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	now := time.Unix(1_780_000_000, 0)
	snapshot := tunNetSnapshot{
		SchemaVersion:    1,
		ClientID:         "11111111-1111-1111-1111-111111111111",
		ActiveEntryNode:  "entry-a",
		SelectedHostSlug: "sin-03",
		ECHConfig:        tunNetECHConfig{ConfigList: echo, ExpiresAt: tunNetUnixTime{Time: now.Add(time.Hour)}},
		EntryNodes: []tunNetEntryNode{{
			Name: "entry-a",
			IPv4: []string{"203.0.113.10"},
			FrontProxy: tunNetFrontProxy{
				Endpoint: "http://front.example:443",
				Headers:  map[string]string{"Host": "front.example", "X-T5-Auth": "token"},
			},
		}},
		Hosts: []tunNetHost{{
			Slug: "sin-03", Online: true, Authority: "tls.sin-03.example",
			VLESSEncryptionKey: key,
		}},
		RouteServer:    "203.0.113.20",
		XHTTPAuthority: "xhttp.sin-03.example",
		XHTTPPath:      "/api/v1/sync/",
	}

	config, err := snapshot.resolve(now)
	require.NoError(t, err)
	require.Equal(t, "11111111-1111-1111-1111-111111111111", config.UUID)
	require.Equal(t, "front.example:443", config.FrontProxyEndpoint)
	require.Equal(t, "203.0.113.20:443", config.RouteServer)
	require.Equal(t, "front.example", config.FrontProxyHeaders["Host"])
	require.Equal(t, "tls.sin-03.example", config.InnerSNI)
	require.Equal(t, "xhttp.sin-03.example", config.InnerAuthority)
	require.Equal(t, "mlkem768x25519plus.native.0rtt."+key+".100-35-35", config.VLESSEncryption)
	parsedEncryption, err := parseClientEncryption(config.VLESSEncryption)
	require.NoError(t, err)
	require.Equal(t, "100-35-35", parsedEncryption.padding)
	require.Len(t, parsedEncryption.keys, 1)
	require.Equal(t, make([]byte, 32), parsedEncryption.keys[0])
	require.Len(t, config.ECHConfig, 1)
	block, rest := pem.Decode([]byte(config.ECHConfig[0]))
	require.NotNil(t, block)
	require.Equal(t, "ECH CONFIGS", block.Type)
	require.Empty(t, rest)
	require.Equal(t, []byte("ech"), block.Bytes)
	require.Equal(t, "/api/v1/sync/", config.XHTTPPath)
}

func TestTunNetSnapshotRejectsExpiredECH(t *testing.T) {
	snapshot := tunNetSnapshot{
		ClientID:  "11111111-1111-1111-1111-111111111111",
		ECHConfig: tunNetECHConfig{ConfigList: "AA", ExpiresAt: tunNetUnixTime{Time: time.Unix(10, 0)}},
	}
	_, err := snapshot.resolve(time.Unix(11, 0))
	require.ErrorContains(t, err, "ECH configuration expired")
}

func TestTunNetSnapshotRejectsMissingSelection(t *testing.T) {
	snapshot := tunNetSnapshot{
		SchemaVersion:    1,
		ClientID:         "11111111-1111-1111-1111-111111111111",
		ActiveEntryNode:  "missing",
		SelectedHostSlug: "missing",
		ECHConfig:        tunNetECHConfig{ConfigList: "AA"},
	}
	_, err := snapshot.resolve(time.Now())
	require.ErrorContains(t, err, "active entry node")
}

func TestApplyTunNetOptionsFromSnapshot(t *testing.T) {
	echo := base64.RawURLEncoding.EncodeToString([]byte("ech"))
	key := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	snapshot := tunNetSnapshot{
		SchemaVersion:    1,
		ClientID:         "11111111-1111-1111-1111-111111111111",
		ActiveEntryNode:  "entry-a",
		SelectedHostSlug: "sin-03",
		ECHConfig:        tunNetECHConfig{ConfigList: echo},
		EntryNodes: []tunNetEntryNode{{
			Name: "entry-a", IPv4: []string{"203.0.113.10"},
			FrontProxy: tunNetFrontProxy{Endpoint: "http://192.0.2.10:443", Headers: map[string]string{"Host": "front-host.example", "X-T5-Auth": "token"}},
		}},
		Hosts:          []tunNetHost{{Slug: "sin-03", Online: true, Authority: "sin-03.example", VLESSEncryptionKey: key}},
		RouteServer:    "203.0.113.20",
		XHTTPAuthority: "sin-03.example",
		XHTTPPath:      "/api/v1/sync/",
	}
	content, err := json.Marshal(snapshot)
	require.NoError(t, err)
	path := t.TempDir() + "/snapshot.json"
	require.NoError(t, os.WriteFile(path, content, 0o600))
	options := option.VLESSOutboundOptions{TunNet: &option.VLESSTunNetOptions{Snapshot: path, FrontProxyStrict: true}}
	remoteIsDomain, err := applyTunNetOptions(&options)
	require.NoError(t, err)
	require.False(t, remoteIsDomain)
	require.Equal(t, snapshot.ClientID, options.UUID)
	require.Equal(t, "203.0.113.20", options.Server)
	require.Equal(t, uint16(443), options.ServerPort)
	require.Equal(t, "sin-03.example", options.TLS.ServerName)
	require.Equal(t, "/api/v1/sync/", options.Transport.XHTTPOptions.Path)
	require.Equal(t, "sin-03.example", options.Transport.XHTTPOptions.Host)
	require.NotEmpty(t, options.Encryption)
}

func TestParseTunNetSnapshotResponse(t *testing.T) {
	payload := map[string]any{
		"bootstrap": map[string]any{
			"schema_version": 2,
			"runtime": map[string]any{
				"active_entry_node": "entry-a",
				"entry_nodes":       []any{map[string]any{"name": "entry-a"}},
				"hosts":             []any{map[string]any{"slug": "sin-03"}},
			},
		},
		"persistent": map[string]any{
			"client_id":  "11111111-1111-1111-1111-111111111111",
			"ech_config": map[string]any{"config_list": "AA", "expires_at": int64(1780003600000)},
		},
		"preferences": map[string]any{"entry_node": "entry-a", "host_slug": "sin-03"},
	}
	content, err := json.Marshal(payload)
	require.NoError(t, err)
	snapshot, err := parseTunNetSnapshotResponse(content)
	require.NoError(t, err)
	require.Equal(t, "11111111-1111-1111-1111-111111111111", snapshot.ClientID)
	require.Equal(t, "entry-a", snapshot.ActiveEntryNode)
	require.Equal(t, "sin-03", snapshot.SelectedHostSlug)
	require.Equal(t, time.UnixMilli(1780003600000), snapshot.ECHConfig.ExpiresAt.Time)
	require.Len(t, snapshot.EntryNodes, 1)
	require.Len(t, snapshot.Hosts, 1)
}

func TestParseTunNetSnapshotSanitizedGoldenShape(t *testing.T) {
	const payload = `{
		"schema_version": 1,
		"client_id": "11111111-1111-1111-1111-111111111111",
		"active_entry_node": "entry-a",
		"selected_host_slug": "sin-03",
		"ech_config": {"config_list": "ZWNobw"},
		"entry_nodes": [{
			"name": "entry-a",
			"ipv4": ["203.0.113.10"],
			"front_proxy": {
				"endpoint": "http://front.example:8443",
				"headers": {"Host": "front.example", "X-T5-Auth": "sanitized-token"}
			}
		}],
		"hosts": [{
			"slug": "sin-03",
			"online": true,
			"authority": "tls.example",
			"vless_encryption_key": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
		}],
		"validated_route_ip": "2001:db8::20",
		"xhttp_authority": "xhttp.example",
		"xhttp_path": "/api/v1/sync/"
	}`
	snapshot, err := parseTunNetSnapshotResponse([]byte(payload))
	require.NoError(t, err)
	resolved, err := snapshot.resolve(time.Unix(1_780_000_000, 0))
	require.NoError(t, err)
	require.Equal(t, "front.example:8443", resolved.FrontProxyEndpoint)
	require.Equal(t, "[2001:db8::20]:443", resolved.RouteServer)
	require.Equal(t, "tls.example", resolved.InnerSNI)
	require.Equal(t, "xhttp.example", resolved.InnerAuthority)
}

func TestParseTunNetSnapshotRejectsSchemaVersions(t *testing.T) {
	_, err := parseTunNetSnapshotResponse([]byte(`{"schema_version":2,"client_id":"synthetic"}`))
	require.ErrorContains(t, err, "snapshot schema_version: 2")

	_, err = parseTunNetSnapshotResponse([]byte(`{"bootstrap":{"schema_version":3}}`))
	require.ErrorContains(t, err, "control schema_version: 3")
}

func TestTunNetSnapshotRejectsInvalidValidatedRouteIP(t *testing.T) {
	snapshot := validTunNetSnapshotForTest()
	snapshot.RouteServer = "route.example"
	_, err := snapshot.resolve(time.Now())
	require.ErrorContains(t, err, "invalid validated_route_ip")
}

func TestTunNetSnapshotRejectsMalformedFrontProxyURLs(t *testing.T) {
	for _, endpoint := range []string{
		"https://front.example:443",
		"http://front.example:443/",
		"http://front.example:443/path",
		"http://user@front.example:443",
		"http://front.example:443?query=1",
		"http://front.example:443#fragment",
		"http://[2001:db8::1",
	} {
		t.Run(endpoint, func(t *testing.T) {
			snapshot := validTunNetSnapshotForTest()
			snapshot.EntryNodes[0].FrontProxy.Endpoint = endpoint
			_, err := snapshot.resolve(time.Now())
			require.ErrorContains(t, err, "invalid front proxy endpoint")
		})
	}
}

func validTunNetSnapshotForTest() tunNetSnapshot {
	return tunNetSnapshot{
		SchemaVersion:    1,
		ClientID:         "11111111-1111-1111-1111-111111111111",
		ActiveEntryNode:  "entry-a",
		SelectedHostSlug: "sin-03",
		ECHConfig:        tunNetECHConfig{ConfigList: base64.RawURLEncoding.EncodeToString([]byte("ech"))},
		EntryNodes: []tunNetEntryNode{{
			Name: "entry-a",
			IPv4: []string{"203.0.113.10"},
			FrontProxy: tunNetFrontProxy{
				Endpoint: "http://front.example:443",
				Headers:  map[string]string{"Host": "front.example", "X-T5-Auth": "sanitized-token"},
			},
		}},
		Hosts: []tunNetHost{{
			Slug: "sin-03", Online: true, Authority: "tls.example",
			VLESSEncryptionKey: base64.RawURLEncoding.EncodeToString(make([]byte, 32)),
		}},
		RouteServer:    "203.0.113.20",
		XHTTPAuthority: "xhttp.example",
	}
}
