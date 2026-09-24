package libcore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"libcore/boxapi"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sagernet/sing-box/adapter"
)

const (
	ipQualityMaxBodyBytes = 256 << 10
	ipQualityTimeout      = 12 * time.Second
)

type ipQualityEndpoints struct {
	official    string
	ip2Location string
	ipWhoIs     string
	dbIP        string
}

var activeIPQualityEndpoints = ipQualityEndpoints{
	official:    "https://my.ippure.com/v1/info",
	ip2Location: "https://api.ip2location.io/?ip=%s",
	ipWhoIs:     "https://ipwho.is/%s",
	dbIP:        "https://api.db-ip.com/v2/free/%s",
}

var newIPQualityHTTPClient = boxapi.CreateProxyHttpClient

type ipQualityResult struct {
	IP          string              `json:"ip"`
	ASN         string              `json:"asn"`
	Locations   []ipQualityLocation `json:"locations"`
	IPSource    string              `json:"ipSource"`
	IPAttribute string              `json:"ipAttribute"`
	Score       int                 `json:"score"`
	ScoreTier   string              `json:"scoreTier"`
	Errors      map[string]string   `json:"errors,omitempty"`
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
	if official.IP == "" {
		return ipQualityResult{}, errors.New("IPPure query failed: response has no IP")
	}
	if official.FraudScore == nil {
		return ipQualityResult{}, errors.New("IPPure query failed: response has no fraud score")
	}
	if *official.FraudScore < 0 || *official.FraudScore > 100 {
		return ipQualityResult{}, errors.New("IPPure query failed: score is outside 0-100")
	}

	result := ipQualityResult{
		IP:          official.IP,
		ASN:         formatIPQualityASN(official.ASN, official.ASOrganization),
		Locations:   make([]ipQualityLocation, 0, 3),
		IPSource:    "unknown",
		IPAttribute: "unknown",
		Score:       *official.FraudScore,
		ScoreTier:   ipQualityScoreTier(*official.FraudScore),
		Errors:      make(map[string]string),
	}
	if official.IsBroadcast != nil {
		result.IPSource = "native"
		if *official.IsBroadcast {
			result.IPSource = "broadcast"
		}
	}
	if official.IsResidential != nil {
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
