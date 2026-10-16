package xhttp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	N "github.com/sagernet/sing/common/network"
)

// ConfigPreflight must construct and close the actual production delegate without
// dialing. Parent owns it to avoid xhttp -> vless -> xhttp import cycles. It must
// preserve the parent's routing policy, never fetched dialer overrides.
type ConfigPreflight func(context.Context, RealConfig) error

// FetchDynamicConfigWithPreflight validates ALL exact members before the caller
// can race/publish any member. Never use the compatibility entry in initialization.
func FetchDynamicConfigWithPreflight(ctx context.Context, dialer N.Dialer, nodeName, nodeID, token, deviceID, deviceName string, preflight ConfigPreflight) ([]RealConfig, error) {
	ctx, cancel := context.WithTimeout(ctx, controlTotalTimeout)
	defer cancel()
	if deviceContext(deviceID, deviceName) && preflight == nil {
		return nil, controlSafe("node-capability", errors.New("missing constructor preflight"))
	}
	configs, err := FetchDynamicConfig(ctx, dialer, nodeName, nodeID, token, deviceID, deviceName)
	if err != nil {
		return nil, err
	}
	if deviceContext(deviceID, deviceName) {
		if err = PreflightConfigs(ctx, configs, preflight); err != nil {
			return nil, err
		}
	}
	return configs, nil
}

func PreflightConfigs(ctx context.Context, configs []RealConfig, preflight ConfigPreflight) error {
	if preflight == nil || len(configs) == 0 {
		return controlSafe("node-capability", errors.New("missing constructor preflight"))
	}
	for _, cfg := range configs {
		if err := ctx.Err(); err != nil {
			return controlSafe("node-capability", err)
		}
		if err := preflight(ctx, cfg); err != nil {
			return controlSafe("node-capability", err)
		}
	}
	return controlSafe("node-capability", ctx.Err())
}

func parseSelectedConfigs(ctx context.Context, plain []byte) ([]RealConfig, error) {
	proxies, err := blackstoneDecodeSelection(plain)
	if err != nil {
		return nil, controlSafe("node-selection", err)
	}
	configs := make([]RealConfig, 0, len(proxies))
	for _, p := range proxies {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		cfg, err := parseSelectedConfig(p, false)
		if err != nil {
			return nil, controlSafe("node-capability", err)
		}
		cfg.Name, _ = p["name"].(string)
		configs = append(configs, cfg)
	}
	return configs, nil
}

func configPort(value any) (string, error) {
	var s string
	switch v := value.(type) {
	case int:
		s = strconv.Itoa(v)
	case string:
		s = v
	case json.Number:
		s = string(v)
	default:
		return "", errors.New("invalid port type")
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 65535 {
		return "", errors.New("invalid port")
	}
	return s, nil
}

func parseSelectedConfig(p map[string]any, terminal bool) (RealConfig, error) {
	fail := func() (RealConfig, error) { return RealConfig{}, errors.New("invalid selected transport fields") }
	typ, ok := p["type"].(string)
	if !ok {
		return fail()
	}
	if typ == "xhttp" && !terminal {
		if v, present := p["certificate"]; present {
			cert, ok := v.(string)
			if !ok || cert == "" {
				return fail()
			}
			return crackSelectedCertificate(cert)
		}
	}
	if typ != "vless" {
		if _, present := p["certificate"]; present {
			return fail()
		}
	}
	server, ok := p["server"].(string)
	if !ok || strings.TrimSpace(server) == "" || server != strings.TrimSpace(server) {
		return fail()
	}
	port, err := configPort(p["port"])
	if err != nil {
		return fail()
	}
	if typ == "vless" {
		return parseSelectedVLESS(p, server, port)
	}
	if typ != "ss" && typ != "xhttp" {
		return fail()
	}
	password, ok := p["password"].(string)
	if !ok || strings.TrimSpace(password) == "" {
		return fail()
	}
	if value, present := p["cipher"]; present {
		if _, ok := value.(string); !ok {
			return fail()
		}
	}
	for _, field := range []string{"udp", "udp-over-tcp"} {
		if value, present := p[field]; present {
			if _, ok := value.(bool); !ok {
				return fail()
			}
		}
	}
	var wire struct {
		Cipher     string            `json:"cipher"`
		UDP        bool              `json:"udp"`
		UOT        bool              `json:"udp-over-tcp"`
		Version    int               `json:"udp-over-tcp-version"`
		PaddingLen string            `json:"padding-len"`
		FakeNet    map[string]string `json:"fake-net"`
	}
	raw, err := json.Marshal(p)
	if err != nil || json.Unmarshal(raw, &wire) != nil || wire.Version < 0 || wire.Version > 2 {
		return fail()
	}
	cfg := RealConfig{Type: typ, Server: server, Port: port, Password: password, Cipher: wire.Cipher, UDP: wire.UDP, UDPOverTCP: wire.UOT, UDPOverTCPVersion: wire.Version, PaddingLen: wire.PaddingLen, TcpFake: wire.FakeNet["tcp"], UdpFake: wire.FakeNet["udp"]}
	if typ == "ss" {
		if cfg.Cipher == "" {
			cfg.Cipher = "2022-blake3-aes-128-gcm"
		}
	} else {
		parts := strings.Split(password, ":")
		if len(parts) < 2 || len(parts[1]) > 512 {
			return fail()
		}
		if _, err := hex.DecodeString(parts[1]); err != nil {
			return fail()
		}
		for _, prefix := range []string{cfg.TcpFake, cfg.UdpFake} {
			b, err := hex.DecodeString(prefix)
			if err != nil || len(b) > 128 {
				return fail()
			}
		}
		switch strings.ToUpper(cfg.Cipher) {
		case "", "AES-128-CTR", "AES-192-CTR", "AES-256-CTR":
		default:
			return fail()
		}
	}
	return cfg, nil
}

func parseSelectedVLESS(p map[string]any, server, port string) (RealConfig, error) {
	fail := func() (RealConfig, error) { return RealConfig{}, errors.New("invalid selected vless fields") }
	for _, field := range []string{"tls", "flow", "reality-opts", "servername", "skip-cert-verify", "xhttp-opts"} {
		if value, present := p[field]; present && value == nil {
			return fail()
		}
	}
	id, ok := p["uuid"].(string)
	for _, field := range []string{"network", "flow"} {
		if value, present := p[field]; present {
			if _, valid := value.(string); !valid {
				return fail()
			}
		}
	}
	if !ok {
		return fail()
	}
	switch len(id) {
	case 32:
		if _, err := hex.DecodeString(id); err != nil {
			return fail()
		}
	case 36:
		if id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' || strings.Count(id, "-") != 4 {
			return fail()
		}
		if _, err := hex.DecodeString(strings.ReplaceAll(id, "-", "")); err != nil {
			return fail()
		}
	default:
		return fail()
	}
	if _, ok := p["e"].(bool); !ok {
		return fail()
	}
	h, ok := p["h"].(string)
	if !ok || h == "" {
		return fail()
	}
	if _, err := hex.DecodeString(h); err != nil {
		return fail()
	}
	copyP := make(map[string]any, len(p))
	for k, v := range p {
		copyP[k] = v
	}
	n, _ := strconv.Atoi(port)
	copyP["port"] = n
	delete(copyP, "type") // discriminator is owned by selection, not VLESS options
	raw, err := json.Marshal(copyP)
	var v BlackstoneVLESSOptions
	if err != nil {
		return fail()
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&v) != nil {
		return fail()
	}
	switch v.Network {
	case "", "tcp", "ws", "http", "h2", "grpc", "xhttp":
	default:
		return fail()
	}
	if v.Flow != "" && v.Flow != "xtls-rprx-vision" {
		return fail()
	}
	// Bare identity stays separate: the actual constructor appends #blackstone.
	return RealConfig{Type: "vless", Server: server, Port: port, UDP: v.UDP, VLESS: &v}, nil
}

func crackSelectedCertificate(cert string) (RealConfig, error) {
	if len(cert) > maxControlResponseBytes {
		return RealConfig{}, errors.New("certificate too large")
	}
	b64 := strings.Join(strings.Fields(strings.ReplaceAll(strings.ReplaceAll(cert, "-----BEGIN CERTIFICATE-----", ""), "-----END CERTIFICATE-----", "")), "")
	if pad := len(b64) % 4; pad != 0 {
		b64 += strings.Repeat("=", 4-pad)
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return RealConfig{}, errors.New("invalid certificate encoding")
	}
	for _, offset := range []int{48, 52, 56, 64} {
		if offset >= len(raw) {
			continue
		}
		plain, err := decryptBlackstonePayload(raw[offset:])
		if err != nil {
			continue
		}
		var p map[string]any
		decoder := json.NewDecoder(strings.NewReader(string(plain)))
		decoder.UseNumber()
		if decoder.Decode(&p) != nil {
			continue
		}
		var trailing any
		if decoder.Decode(&trailing) != io.EOF {
			return RealConfig{}, errors.New("invalid certificate trailing data")
		}
		if p["type"] != "xhttp" {
			return RealConfig{}, errors.New("invalid certificate terminal type")
		}
		cfg, err := parseSelectedConfig(p, true)
		if err != nil {
			return RealConfig{}, err
		}
		return cfg, nil
	}
	return RealConfig{}, fmt.Errorf("invalid certificate payload")
}
