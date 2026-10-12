package xhttp

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"golang.org/x/crypto/chacha20poly1305"
	"gopkg.in/yaml.v3"
)

var (
	PRIV_KEY = []byte("NiKNssxJcXp7Mh4yjjFGdrcFoC66wcVu5j1LgBevGCm8utV4qg089xdb20tAKu")
	PUB_KEY  = []byte("jMJaXZXGc1CbTri1UnRdvJ3izp8f0jGhXGr2jjr9nMkBKUZDoh3Avoijc4jQUw")
)

const (
	maxControlResponseBytes = 4 << 20
	maxDecodedControlBytes  = 16 << 20
)

type RealConfig struct {
	Type     string
	Server   string
	Port     string
	Password string
	Cipher   string
	TcpFake  string
}

func applyControlHeaders(req *http.Request, token string, deviceID string, deviceName string) {
	req.Host = "g.just4test.xyz"
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-version", "520")
	req.Header.Set("x-platform", "android")
	req.Header.Set("user-agent", "okhttp/4.9.2")
	req.Header.Set("x-header", buildXHeader(token, deviceID, deviceName))
	if deviceID != "" && deviceName != "" {
		req.Header.Set("x-version", "540")
		req.Header.Set("accept", "application/json, text/plain, */*")
		req.Header.Set("accept-language", "zh_CN")
	}
}

func newControlRequest(ctx context.Context, url string, payload []byte, token string, deviceID string, deviceName string) (*http.Request, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	applyControlHeaders(req, token, deviceID, deviceName)
	return req, nil
}

func readBounded(reader io.Reader, limit int, label string) ([]byte, error) {
	limited := &io.LimitedReader{R: reader, N: int64(limit) + 1}
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", label, err)
	}
	if len(data) > limit {
		return nil, fmt.Errorf("%s too large", label)
	}
	return data, nil
}

func decodeMaybeGzip(data []byte, limit int, label string) ([]byte, error) {
	if !bytes.HasPrefix(data, []byte("\x1f\x8b")) {
		if len(data) > limit {
			return nil, fmt.Errorf("%s too large", label)
		}
		return data, nil
	}
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("open %s gzip: %w", label, err)
	}
	decoded, readErr := readBounded(reader, limit, label)
	closeErr := reader.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, fmt.Errorf("consume %s gzip: %w", label, err)
	}
	return decoded, nil
}

func FetchDynamicConfig(ctx context.Context, dialer N.Dialer, nodeName string, nodeID string, token string, deviceID string, deviceName string) ([]RealConfig, error) {
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(c context.Context, network, addr string) (net.Conn, error) {
				return dialer.DialContext(c, "tcp", M.ParseSocksaddr(addr))
			},
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
				ServerName:         "g.just4test.xyz",
			},
			ForceAttemptHTTP2: true,
		},
	}
	return fetchDynamicConfig(ctx, client, nodeName, nodeID, token, deviceID, deviceName)
}

func fetchDynamicConfig(ctx context.Context, client *http.Client, nodeName string, nodeID string, token string, deviceID string, deviceName string) ([]RealConfig, error) {
	cfNodeIP := "104.21.41.69"
	url := fmt.Sprintf("https://%s/api/v3/proxy/config", cfNodeIP)

	reqMap := map[string]interface{}{
		"fastest_ping":    map[string]interface{}{},
		"energy":          false,
		"groups":          "",
		"id":              nodeID,
		"nameservers":     nil,
		"strategy":        "smart",
		"isp":             "",
		"include-package": "",
	}
	reqJson, _ := json.Marshal(reqMap)
	encryptedReq := encryptRequestData(string(reqJson))

	payloadMap := map[string]string{"data": encryptedReq}
	payloadJson, _ := json.Marshal(payloadMap)

	req, err := newControlRequest(ctx, url, payloadJson, token, deviceID, deviceName)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, errors.New("missing control HTTP client")
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("api request failed: %w", err)
	}

	bodyBytes, readErr := readBounded(resp.Body, maxControlResponseBytes, "control response")
	closeErr := resp.Body.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, fmt.Errorf("consume control response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api returned status: %d", resp.StatusCode)
	}

	var encryptedB64 string
	var jsonResp map[string]interface{}

	if err := json.Unmarshal(bodyBytes, &jsonResp); err == nil && jsonResp["data"] != nil {
		dataValue, ok := jsonResp["data"].(string)
		if !ok || dataValue == "" {
			return nil, errors.New("api response encrypted data must be a non-empty string")
		}
		encryptedB64 = dataValue
	} else {
		encryptedB64 = strings.Trim(string(bodyBytes), "\"'\n\r\t ")
	}

	if pad := len(encryptedB64) % 4; pad != 0 {
		encryptedB64 += strings.Repeat("=", 4-pad)
	}

	rawB64, err := base64.StdEncoding.DecodeString(encryptedB64)
	if err != nil {
		return nil, fmt.Errorf("base64 decode error: %v", err)
	}
	for i := range rawB64 {
		rawB64[i] ^= PRIV_KEY[i%len(PRIV_KEY)]
	}
	rawB64, err = decodeMaybeGzip(rawB64, maxDecodedControlBytes, "decrypted control response")
	if err != nil {
		return nil, err
	}

	var layer1 map[string]interface{}
	if err := json.Unmarshal(rawB64, &layer1); err != nil {
		return nil, errors.New("invalid decrypted control response JSON")
	}

	if layer1["data"] == nil {
		return nil, errors.New("decrypted data field is missing")
	}

	dataValue, ok := layer1["data"]
	if !ok {
		return nil, errors.New("api response missing data")
	}
	data, ok := dataValue.(map[string]interface{})
	if !ok {
		return nil, errors.New("api response data must be an object")
	}
	smartValue, ok := data["smart"]
	if !ok {
		return nil, errors.New("api response missing smart config")
	}
	smartB64, ok := smartValue.(string)
	if !ok || smartB64 == "" {
		return nil, errors.New("api response smart config must be a non-empty string")
	}

	smartRaw, err := base64.StdEncoding.DecodeString(smartB64)
	if err != nil {
		return nil, errors.New("invalid smart config encoding")
	}
	smartPlain, err := decryptBlackstonePayload(smartRaw)
	if err != nil {
		return nil, err
	}

	var smartData struct {
		Proxies []map[string]interface{} `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(smartPlain, &smartData); err != nil {
		return nil, errors.New("invalid smart config YAML")
	}

	var realConfigs []RealConfig

	// 🔥 判断是否只有一个节点作为保底措施
	isSingleNode := len(smartData.Proxies) == 1

	for _, proxy := range smartData.Proxies {
		pName, _ := proxy["name"].(string)

		// 只有在服务器下发了多个节点时，才进行名称匹配！单节点直接无脑拿走！
		if !isSingleNode && nodeName != "" {
			// 智能关键词模糊匹配机制
			cleanNodeName := strings.ReplaceAll(nodeName, "#", " ")
			cleanNodeName = strings.ReplaceAll(cleanNodeName, "-", " ")
			cleanNodeName = strings.ReplaceAll(cleanNodeName, "|", " ")
			keywords := strings.Fields(cleanNodeName)

			isMatch := true
			for _, kw := range keywords {
				if !strings.Contains(pName, kw) {
					isMatch = false
					break
				}
			}

			if !isMatch {
				continue
			}
		}

		pType, _ := proxy["type"].(string)

		// 🔥 终极补漏：自动识别并补全缺失的 cipher！同时将 os 协议伪装成 ss 协议下发给 outbound
		if pType == "ss" || pType == "os" {
			cipherStr, _ := proxy["cipher"].(string)
			if cipherStr == "" || cipherStr == "<nil>" {
				if pType == "os" {
					cipherStr = "aes-128-ctr"
				} else {
					cipherStr = "2022-blake3-aes-128-gcm"
				}
			}

			cfg := RealConfig{
				Type:     "ss", // 统一标记为 ss，outbound.go 会自动加上 #BLACKSTONE
				Server:   fmt.Sprintf("%v", proxy["server"]),
				Port:     fmt.Sprintf("%v", proxy["port"]),
				Password: fmt.Sprintf("%v", proxy["password"]),
				Cipher:   cipherStr,
			}
			realConfigs = append(realConfigs, cfg)
		} else if pType == "xhttp" {
			certStr, _ := proxy["certificate"].(string)
			if certStr != "" {
				realCfg, err := crackCertificate(certStr)
				if err == nil {
					realConfigs = append(realConfigs, realCfg)
				}
			} else {
				cfg := RealConfig{
					Type:     "xhttp",
					Server:   fmt.Sprintf("%v", proxy["server"]),
					Port:     fmt.Sprintf("%v", proxy["port"]),
					Password: fmt.Sprintf("%v", proxy["password"]),
				}
				if fakeNet, ok := proxy["fake-net"].(map[string]interface{}); ok {
					cfg.TcpFake, _ = fakeNet["tcp"].(string)
				}
				realConfigs = append(realConfigs, cfg)
			}
		}
	}

	if len(realConfigs) == 0 {
		return nil, fmt.Errorf("no target node matched for name: %s", nodeName)
	}

	return realConfigs, nil
}

func crackCertificate(certPem string) (RealConfig, error) {
	re := regexp.MustCompile(`-----BEGIN CERTIFICATE-----|-----END CERTIFICATE-----|\s+`)
	b64Data := re.ReplaceAllString(certPem, "")
	if pad := len(b64Data) % 4; pad != 0 {
		b64Data += strings.Repeat("=", 4-pad)
	}

	raw, err := base64.StdEncoding.DecodeString(b64Data)
	if err != nil {
		return RealConfig{}, err
	}

	offsets := []int{48, 52, 56, 64}
	for _, offset := range offsets {
		if offset >= len(raw) {
			continue
		}
		plain, err := decryptBlackstonePayload(raw[offset:])
		if err == nil {
			var innerData map[string]interface{}
			if json.Unmarshal(plain, &innerData) == nil {
				cfg := RealConfig{
					Type:     "xhttp",
					Server:   fmt.Sprintf("%v", innerData["server"]),
					Port:     fmt.Sprintf("%v", innerData["port"]),
					Password: fmt.Sprintf("%v", innerData["password"]),
				}
				if fakeNet, ok := innerData["fake-net"].(map[string]interface{}); ok {
					cfg.TcpFake, _ = fakeNet["tcp"].(string)
				}
				return cfg, nil
			}
		}
	}
	return RealConfig{}, errors.New("all asn.1 offsets failed")
}

func buildXHeader(token string, deviceID string, deviceName string) string {
	headerMap := map[string]interface{}{
		"X-DEVICE-NAME":  "Lenovo - Lenovo TB-J606F",
		"X-IDENTIFIER":   "ed7295e154b50905",
		"X-TOKEN":        token,
		"X-TIMESTAMP":    "1779367490",
		"X-CHECK-MOBILE": `{"isRoot":true,"isEmulator":false,"bundleID":"com.heysocks.android"}`,
		"X-OS-VERSION":   "30",
	}
	if deviceID != "" && deviceName != "" {
		headerMap = map[string]interface{}{
			"X-IDENTIFIER":      deviceID,
			"X-DEVICE-NAME":     deviceName,
			"X-TOKEN":           token,
			"X-TIMESTAMP":       fmt.Sprintf("%d", time.Now().Unix()),
			"X-SOURCE":          `{"mvjarp":"gp","sracun":"gp"}`,
			"X-CHECK-MOBILE":    `{"isRoot":true,"isEmulator":false,"bundleID":"com.heysocks.android"}`,
			"X-BUNDLE-ID":       "",
			"X-ABIS":            "arm64-v8a,armeabi-v7a,armeabi",
			"X-INSTALLREFERRER": "unknown",
			"X-OS-VERSION":      "30",
			"X-ISP":             "",
			"X-COUNTRY-CODE":    "CN",
			"X-LOADED":          "false",
		}
	}
	headerJson, _ := json.Marshal(headerMap)
	return encryptRequestData(string(headerJson))
}

func encryptRequestData(dataStr string) string {
	rawBytes := []byte(dataStr)
	res := make([]byte, len(rawBytes))
	for i := 0; i < len(rawBytes); i++ {
		res[i] = rawBytes[i] ^ PUB_KEY[i%len(PUB_KEY)]
	}
	return base64.StdEncoding.EncodeToString(res)
}

func vmessKDF(secret []byte, paths ...string) []byte {
	creator := func() hash.Hash { return hmac.New(sha256.New, []byte("VMess AEAD KDF")) }
	for _, path := range paths {
		c := creator
		pBytes := []byte(path)
		creator = func() hash.Hash { return hmac.New(c, pBytes) }
	}
	h := creator()
	h.Write(secret)
	return h.Sum(nil)
}

func decryptBlackstonePayload(rawData []byte) ([]byte, error) {
	if len(rawData) < 1 {
		return nil, errors.New("data too short")
	}
	chunkCount := int(rawData[0])
	headerSize := 1 + chunkCount*8
	if len(rawData) < headerSize {
		return nil, errors.New("header size error")
	}

	type offset struct{ start, end uint32 }
	table := make([]offset, chunkCount)
	for i := 0; i < chunkCount; i++ {
		ptr := 1 + i*8
		table[i].start = binary.BigEndian.Uint32(rawData[ptr : ptr+4])
		table[i].end = binary.BigEndian.Uint32(rawData[ptr+4 : ptr+8])
	}

	payload := rawData[headerSize:]
	payloadLength := uint64(len(payload))
	isKey := make([]bool, len(payload))
	for _, off := range table {
		start := uint64(off.start)
		end := uint64(off.end)
		if start > end || end > payloadLength {
			return nil, errors.New("invalid chunk offsets")
		}
		for i := start; i < end; i++ {
			isKey[int(i)] = true
		}
	}

	var keySeedAscii, ciphertext []byte
	for i, b := range payload {
		if isKey[i] {
			keySeedAscii = append(keySeedAscii, b)
		} else {
			ciphertext = append(ciphertext, b)
		}
	}

	secretBytes, err := hex.DecodeString(string(keySeedAscii))
	if err != nil {
		secretBytes = keySeedAscii
	}

	derivedKey := vmessKDF(secretBytes, "CHACHA 20 POLY 1305")

	if len(ciphertext) < 12 {
		return nil, errors.New("ciphertext too short")
	}
	nonce := ciphertext[:12]
	actualCt := ciphertext[12:]

	aead, err := chacha20poly1305.New(derivedKey)
	if err != nil {
		return nil, err
	}

	plaintext, err := aead.Open(nil, nonce, actualCt, nil)
	if err != nil {
		return nil, err
	}

	return decodeMaybeGzip(plaintext, maxDecodedControlBytes, "decrypted smart config")
}
