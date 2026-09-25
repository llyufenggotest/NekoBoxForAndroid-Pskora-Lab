package libcore

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"libcore/boxapi"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sagernet/sing-box/adapter"
)

const (
	ipQualityMaxBodyBytes = 256 << 10
	ipQualityTimeout      = 12 * time.Second
)

type ipQualityEndpoints struct {
	ipv4Discovery string
	official      string
	basic         string

	risk        string
	botClass    string
	ip2Location string
	ipWhoIs     string
	dbIP        string
}

var activeIPQualityEndpoints = ipQualityEndpoints{
	ipv4Discovery: "https://ipv4.icanhazip.com",
	official:      "https://my.ippure.com/v1/info",
	basic:         "https://api.123169.xyz/api/info/ip-basic/%s",
	risk:          "https://api.123169.xyz/api/info/ip-risk/%s",
	botClass:      "https://api.123169.xyz/api/info/asn/botclass/%s",
	ip2Location:   "https://api.ip2location.io/?ip=%s",
	ipWhoIs:       "https://ipwho.is/%s",
	dbIP:          "https://api.db-ip.com/v2/free/%s",
}

var newIPQualityHTTPClient = boxapi.CreateProxyHttpClientIPv4

type ipPureSession struct {
	mutex      sync.Mutex
	key        string
	timeOffset int64
}

func (s *ipPureSession) getJSON(ctx context.Context, client *http.Client, endpoint string, target any) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	for attempt := 0; attempt < 4; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return errors.New("invalid source URL")
		}
		request.Header.Set("Accept", "application/json")
		request.Header.Set("User-Agent", "Mozilla/5.0 (Linux; Android 12) AppleWebKit/537.36 Chrome/120 Mobile Safari/537.36")
		request.Header.Set("Origin", "https://ippure.com")
		request.Header.Set("Referer", "https://ippure.com/")
		if s.key != "" {
			timestamp := time.Now().UnixMilli() + s.timeOffset
			payload := strings.Join([]string{request.Method, request.URL.String(), "", strconv.FormatInt(timestamp, 10)}, "-")
			mac := hmac.New(sha256.New, []byte(s.key))
			_, _ = mac.Write([]byte(payload))
			request.Header.Set("x-k", s.key)
			request.Header.Set("x-t", strconv.FormatInt(timestamp, 10)+"-"+hex.EncodeToString(mac.Sum(nil)))
		}
		response, err := client.Do(request)
		if err != nil {
			return fmt.Errorf("request failed: %w", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, ipQualityMaxBodyBytes+1))
		response.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read response: %w", readErr)
		}
		if len(body) > ipQualityMaxBodyBytes {
			return errors.New("response too large")
		}
		if nextKey := response.Header.Get("x-k"); nextKey != "" {
			s.key = nextKey
			if serverTime, parseErr := strconv.ParseInt(response.Header.Get("x-t"), 10, 64); parseErr == nil {
				s.timeOffset = serverTime - time.Now().UnixMilli()
			}
			continue
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return fmt.Errorf("HTTP status %d", response.StatusCode)
		}
		if err := json.Unmarshal(body, target); err != nil {
			return errors.New("invalid JSON response")
		}
		return nil
	}
	return errors.New("IPPure authentication retry limit reached")
}

type ipQualityResult struct {
	IP           string              `json:"ip"`
	ASN          string              `json:"asn"`
	ASDomain     string              `json:"asDomain,omitempty"`
	IPRangeStart string              `json:"ipRangeStart,omitempty"`
	IPRangeEnd   string              `json:"ipRangeEnd,omitempty"`
	HumanTraffic float64             `json:"humanTraffic"`
	BotTraffic   float64             `json:"botTraffic"`
	TrafficKnown bool                `json:"trafficKnown"`
	Locations    []ipQualityLocation `json:"locations"`
	IPSource     string              `json:"ipSource"`
	IPAttribute  string              `json:"ipAttribute"`
	Score        int                 `json:"score"`
	ScoreTier    string              `json:"scoreTier"`
	Errors       map[string]string   `json:"errors,omitempty"`
}

type ipQualityLocation struct {
	Provider    string `json:"provider"`
	CountryCode string `json:"countryCode,omitempty"`
	Country     string `json:"country,omitempty"`
	Region      string `json:"region,omitempty"`
	City        string `json:"city,omitempty"`
}

type ipPureResponse struct {
	IP             string          `json:"ip"`
	ASN            json.RawMessage `json:"asn"`
	ASOrganization string          `json:"asOrganization"`
	CountryCode    string          `json:"countryCode"`
	Country        string          `json:"country"`
	Region         string          `json:"region"`
	City           string          `json:"city"`
	IsBroadcast    *bool           `json:"isBroadcast"`
	IsResidential  *bool           `json:"isResidential"`
	FraudScore     *int            `json:"fraudScore"`
}

type ipPureBasicResponse struct {
	OK   bool `json:"ok"`
	Data struct {
		IP  string `json:"ip"`
		ASN struct {
			Number        int64  `json:"number"`
			Organization  string `json:"organization"`
			Domain        string `json:"domain"`
			IsResidential bool   `json:"is_residential"`
			Type          string `json:"type"`
			Network       struct {
				Range struct {
					Start string `json:"start"`
					End   string `json:"end"`
				} `json:"range"`
			} `json:"network"`
		} `json:"asn"`
		Traits struct {
			IsBroadcast *bool `json:"is_broadcast"`
		} `json:"traits"`
	} `json:"data"`
}

type ipPureRiskResponse struct {
	OK   bool `json:"ok"`
	Data struct {
		RiskScore *int `json:"risk_score"`
	} `json:"data"`
}

type ipPureBotClassResponse struct {
	OK   bool `json:"ok"`
	Data struct {
		Bot   float64 `json:"bot"`
		Human float64 `json:"human"`
	} `json:"data"`
}

// QueryIPQuality returns IPPure's official connected-exit assessment and
// best-effort HTTPS geolocation comparisons as JSON. All requests use the
// active BoxInstance's routed HTTP client.
func (b *BoxInstance) QueryIPQuality() (string, error) {
	b.access.Lock()
	if b.state != 1 || b.Box == nil {
		b.access.Unlock()
		return "", errors.New("box instance is not active")
	}
	activeBox := b.Box
	var tracker adapter.ConnectionTracker
	if b.v2api != nil {
		tracker = b.v2api.StatsService()
	}
	b.access.Unlock()

	client := newIPQualityHTTPClient(activeBox, tracker)
	client.Timeout = ipQualityTimeout
	defer client.CloseIdleConnections()

	result, err := queryIPQuality(client, activeIPQualityEndpoints)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("encode IP quality result: %w", err)
	}
	return string(encoded), nil
}

func queryIPQuality(client *http.Client, endpoints ipQualityEndpoints) (ipQualityResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ipQualityTimeout)
	defer cancel()

	var official ipPureResponse
	if err := getIPQualityJSON(ctx, client, endpoints.official, &official); err != nil {
		return ipQualityResult{}, fmt.Errorf("IPPure query failed: %w", err)
	}
	if endpoints.ipv4Discovery != "" {
		var discoveredIPv4 string
		if err := getIPQualityText(ctx, client, endpoints.ipv4Discovery, &discoveredIPv4); err != nil {
			return ipQualityResult{}, fmt.Errorf("IPv4 discovery failed: %w", err)
		}
		parsedIPv4 := net.ParseIP(strings.TrimSpace(discoveredIPv4))
		if parsedIPv4 == nil || parsedIPv4.To4() == nil {
			return ipQualityResult{}, errors.New("IPv4 discovery failed: response has no IPv4 address")
		}
		discoveredIPv4 = parsedIPv4.To4().String()
		officialIP := net.ParseIP(strings.TrimSpace(official.IP))
		sameIP := officialIP != nil && officialIP.To4() != nil && officialIP.To4().String() == discoveredIPv4
		if !sameIP {
			official.ASN = nil
			official.ASOrganization = ""
			official.IsBroadcast = nil
			official.IsResidential = nil
			official.FraudScore = nil
		}
		official.IP = discoveredIPv4
	}
	if official.IP == "" {
		return ipQualityResult{}, errors.New("IPPure query failed: response has no IP")
	}
	result := ipQualityResult{
		IP:          official.IP,
		ASN:         formatIPQualityASN(official.ASN, official.ASOrganization),
		Locations:   make([]ipQualityLocation, 0, 3),
		IPSource:    "unknown",
		IPAttribute: "unknown",
		Score:       -1,
		ScoreTier:   "unknown",
		Errors:      make(map[string]string),
	}
	if official.FraudScore != nil {
		if *official.FraudScore < 0 || *official.FraudScore > 100 {
			return ipQualityResult{}, errors.New("IPPure query failed: score is outside 0-100")
		}
		result.Score = *official.FraudScore
		result.ScoreTier = ipQualityScoreTier(*official.FraudScore)
	} else {
		result.Errors["IPPure"] = "response has no fraud score"
	}

	ippure := new(ipPureSession)
	var basic ipPureBasicResponse
	if endpoints.basic != "" {
		if err := ippure.getJSON(ctx, client, endpointForIP(endpoints.basic, official.IP), &basic); err != nil || !basic.OK {
			if err != nil {
				result.Errors["IPPure basic"] = err.Error()
			} else {
				result.Errors["IPPure basic"] = "source returned an error"
			}
		} else {
			if basic.Data.ASN.Number != 0 {
				result.ASN = fmt.Sprintf("AS%d", basic.Data.ASN.Number)
				if basic.Data.ASN.Organization != "" {
					result.ASN += " " + basic.Data.ASN.Organization
				}
			}
			result.ASDomain = basic.Data.ASN.Domain
			result.IPRangeStart = basic.Data.ASN.Network.Range.Start
			result.IPRangeEnd = basic.Data.ASN.Network.Range.End
			if basic.Data.ASN.IsResidential {
				result.IPAttribute = "residential"
			} else if basic.Data.ASN.Type == "hosting" {
				result.IPAttribute = "datacenter"
			}
			if basic.Data.Traits.IsBroadcast != nil {
				result.IPSource = "native"
				if *basic.Data.Traits.IsBroadcast {
					result.IPSource = "broadcast"
				}
			}
		}
	}

	if endpoints.risk != "" {
		var risk ipPureRiskResponse
		if err := ippure.getJSON(ctx, client, endpointForIP(endpoints.risk, official.IP), &risk); err != nil || !risk.OK || risk.Data.RiskScore == nil {
			if err != nil {
				result.Errors["IPPure risk"] = err.Error()
			} else {
				result.Errors["IPPure risk"] = "source returned no score"
			}
		} else if *risk.Data.RiskScore >= 0 && *risk.Data.RiskScore <= 100 {
			result.Score = *risk.Data.RiskScore
			result.ScoreTier = ipQualityScoreTier(result.Score)
			delete(result.Errors, "IPPure")
		}
	}

	asnNumber := ""
	if fields := strings.Fields(result.ASN); len(fields) > 0 {
		asnNumber = strings.TrimPrefix(strings.ToUpper(fields[0]), "AS")
	}
	if endpoints.botClass != "" && asnNumber != "" {
		var botClass ipPureBotClassResponse
		if err := ippure.getJSON(ctx, client, endpointForIP(endpoints.botClass, asnNumber), &botClass); err == nil && botClass.OK {
			result.HumanTraffic = botClass.Data.Human
			result.BotTraffic = botClass.Data.Bot
			result.TrafficKnown = true
		} else if err != nil {
			result.Errors["IPPure traffic"] = err.Error()
		}
	}

	if official.IsBroadcast != nil && result.IPSource == "unknown" {
		result.IPSource = "native"
		if *official.IsBroadcast {
			result.IPSource = "broadcast"
		}
	}
	if official.IsResidential != nil && result.IPAttribute == "unknown" {
		result.IPAttribute = "datacenter"
		if *official.IsResidential {
			result.IPAttribute = "residential"
		}
	}

	sources := []struct {
		name string
		url  string
		read func(json.RawMessage) (ipQualityLocation, error)
	}{
		{name: "IP2Location", url: endpointForIP(endpoints.ip2Location, official.IP), read: readIP2Location},
		{name: "ipwho.is", url: endpointForIP(endpoints.ipWhoIs, official.IP), read: readIPWhoIs},
		{name: "DB-IP", url: endpointForIP(endpoints.dbIP, official.IP), read: readDBIP},
	}
	for _, source := range sources {
		var raw json.RawMessage
		if err := getIPQualityJSON(ctx, client, source.url, &raw); err != nil {
			result.Errors[source.name] = err.Error()
			continue
		}
		location, err := source.read(raw)
		if err != nil {
			result.Errors[source.name] = err.Error()
			continue
		}
		if location.CountryCode != "" || location.Country != "" || location.Region != "" || location.City != "" {
			result.Locations = append(result.Locations, location)
		}
	}
	if len(result.Errors) == 0 {
		result.Errors = nil
	}
	return result, nil
}

func getIPQualityText(ctx context.Context, client *http.Client, endpoint string, target *string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return errors.New("invalid source URL")
	}
	request.Header.Set("Accept", "text/plain")
	request.Header.Set("User-Agent", "NekoBox-IPQuality/1.0")
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("HTTP status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 256))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	*target = string(body)
	return nil
}

func getIPQualityJSON(ctx context.Context, client *http.Client, endpoint string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return errors.New("invalid source URL")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "NekoBox-IPQuality/1.0")
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("HTTP status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, ipQualityMaxBodyBytes+1))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if len(body) > ipQualityMaxBodyBytes {
		return errors.New("response too large")
	}
	if err := json.Unmarshal(body, target); err != nil {
		return errors.New("invalid JSON response")
	}
	return nil
}

func endpointForIP(endpoint, ip string) string {
	if strings.Contains(endpoint, "%s") {
		return fmt.Sprintf(endpoint, url.PathEscape(ip))
	}
	return endpoint
}

func ipQualityScoreTier(score int) string {
	switch {
	case score >= 70:
		return "red"
	case score >= 25:
		return "yellow"
	default:
		return "green"
	}
}

func formatIPQualityASN(raw json.RawMessage, organization string) string {
	value := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	if value == "" || value == "null" || value == "0" {
		return ""
	}
	if !strings.HasPrefix(strings.ToUpper(value), "AS") {
		value = "AS" + value
	}
	if organization != "" {
		value += " " + organization
	}
	return value
}

func readIP2Location(raw json.RawMessage) (ipQualityLocation, error) {
	var value struct {
		Error       string `json:"error_message"`
		CountryCode string `json:"country_code"`
		Country     string `json:"country_name"`
		Region      string `json:"region_name"`
		City        string `json:"city_name"`
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return ipQualityLocation{}, errors.New("invalid source response")
	}
	if value.Error != "" {
		return ipQualityLocation{}, errors.New("source returned an error")
	}
	return ipQualityLocation{Provider: "IP2Location", CountryCode: value.CountryCode, Country: value.Country, Region: value.Region, City: value.City}, nil
}

func readIPWhoIs(raw json.RawMessage) (ipQualityLocation, error) {
	var value struct {
		Success     *bool  `json:"success"`
		CountryCode string `json:"country_code"`
		Country     string `json:"country"`
		Region      string `json:"region"`
		City        string `json:"city"`
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return ipQualityLocation{}, errors.New("invalid source response")
	}
	if value.Success != nil && !*value.Success {
		return ipQualityLocation{}, errors.New("source returned an error")
	}
	return ipQualityLocation{Provider: "ipwho.is", CountryCode: value.CountryCode, Country: value.Country, Region: value.Region, City: value.City}, nil
}

func readDBIP(raw json.RawMessage) (ipQualityLocation, error) {
	var value struct {
		Error       string `json:"error"`
		CountryCode string `json:"countryCode"`
		Country     string `json:"countryName"`
		Region      string `json:"stateProv"`
		City        string `json:"city"`
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return ipQualityLocation{}, errors.New("invalid source response")
	}
	if value.Error != "" {
		return ipQualityLocation{}, errors.New("source returned an error")
	}
	return ipQualityLocation{Provider: "DB-IP", CountryCode: value.CountryCode, Country: value.Country, Region: value.Region, City: value.City}, nil
}
