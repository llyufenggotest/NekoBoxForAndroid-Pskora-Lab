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

	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"golang.org/x/crypto/chacha20poly1305"
	"gopkg.in/yaml.v3"
)

var (
	PRIV_KEY = []byte("NiKNssxJcXp7Mh4yjjFGdrcFoC66wcVu5j1LgBevGCm8utV4qg089xdb20tAKu")
	PUB_KEY  = []byte("jMJaXZXGc1CbTri1UnRdvJ3izp8f0jGhXGr2jjr9nMkBKUZDoh3Avoijc4jQUw")
)

type RealConfig struct {
	Type     string
	Server   string
	Port     string
	Password string
	Cipher   string
	TcpFake  string
}

func FetchDynamicConfig(ctx context.Context, dialer N.Dialer, nodeName string, nodeID string, token string) ([]RealConfig, error) {
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

	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(payloadJson))

	req.Host = "g.just4test.xyz"
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-version", "520")
	req.Header.Set("x-platform", "android")
	req.Header.Set("user-agent", "okhttp/4.9.2")
	req.Header.Set("x-header", buildXHeader(token))

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

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("api request failed: %v", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("api returned status: %d", resp.StatusCode)
	}

	var encryptedB64 string
	var jsonResp map[string]interface{}

	if err := json.Unmarshal(bodyBytes, &jsonResp); err == nil && jsonResp["data"] != nil {
		encryptedB64 = jsonResp["data"].(string)
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
	if bytes.HasPrefix(rawB64, []byte("\x1f\x8b")) {
		gr, _ := gzip.NewReader(bytes.NewReader(rawB64))
		rawB64, _ = io.ReadAll(gr)
		gr.Close()
	}

	var layer1 map[string]interface{}
	json.Unmarshal(rawB64, &layer1)

	if layer1["data"] == nil {
		return nil, errors.New("decrypted data field is missing")
	}

	dataObj := layer1["data"].(map[string]interface{})
	smartB64 := dataObj["smart"].(string)

	smartRaw, _ := base64.StdEncoding.DecodeString(smartB64)
	smartPlain, err := decryptBlackstonePayload(smartRaw)
	if err != nil {
		return nil, err
	}

	var smartData struct {
		Proxies []map[string]interface{} `yaml:"proxies"`
	}
	yaml.Unmarshal(smartPlain, &smartData)

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

func buildXHeader(token string) string {
	headerMap := map[string]interface{}{
		"X-DEVICE-NAME":     "Lenovo - Lenovo TB-J606F",
		"X-IDENTIFIER":      "ed7295e154b50905",
		"X-TOKEN":           token,
		"X-SOURCE":          `{"mvjarp":"gp","sracun":"gp"}`,
		"X-TIMESTAMP":       "1779367490",
		"X-CHECK-MOBILE":    `{"isRoot":true,"isEmulator":false,"bundleID":"com.heysocks.android"}`,
		"X-BUNDLE-ID":       "",
		"X-ABIS":            "arm64-v8a,armeabi-v7a,armeabi",
		"X-INSTALLREFERRER": "unknown",
		"X-OS-VERSION":      "30",
		"X-ISP":             "",
		"X-COUNTRY-CODE":    "CN",
		"X-LOADED":          "false",
		"x-sign":            "2763cb26083e582d60dd016e1a5e0ae9",
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
	isKey := make([]bool, len(payload))
	for _, off := range table {
		for i := off.start; i < off.end; i++ {
			if int(i) < len(isKey) {
				isKey[i] = true
			}
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

	if bytes.HasPrefix(plaintext, []byte("\x1f\x8b")) {
		gr, err := gzip.NewReader(bytes.NewReader(plaintext))
		if err == nil {
			defer gr.Close()
			uncompressed, _ := io.ReadAll(gr)
			return uncompressed, nil
		}
	}

	return plaintext, nil
}
