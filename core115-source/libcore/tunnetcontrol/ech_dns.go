package tunnetcontrol

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	mDNS "github.com/miekg/dns"
)

const echQueryName = "cloudflare-ech.com."

var echDoHURLs = []string{
	"https://223.5.5.5/dns-query",
	"https://cloudflare-dns.com/dns-query",
	"https://dns.google/dns-query",
}

func FetchECHConfigDNS(ctx context.Context, client *http.Client, queryName string, now time.Time) (ECHConfig, error) {
	var lastErr error
	for _, endpoint := range echDoHURLs {
		attemptCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		config, err := fetchECHConfigDNSFrom(attemptCtx, client, endpoint, queryName, now)
		cancel()
		if err == nil {
			return config, nil
		}
		lastErr = err
	}
	return ECHConfig{}, fmt.Errorf("TunNet ECH query failed on all resolvers: %w", lastErr)
}

func fetchECHConfigDNSFrom(ctx context.Context, client *http.Client, endpoint, queryName string, now time.Time) (ECHConfig, error) {
	queryName = mDNS.Fqdn(queryName)
	if !validDNSAuthority(strings.TrimSuffix(strings.ToLower(queryName), ".")) {
		return ECHConfig{}, errors.New("invalid TunNet ECH query name")
	}
	message := new(mDNS.Msg)
	message.SetQuestion(queryName, mDNS.TypeHTTPS)
	wire, err := message.Pack()
	if err != nil {
		return ECHConfig{}, fmt.Errorf("encode TunNet ECH query: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(wire))
	if err != nil {
		return ECHConfig{}, fmt.Errorf("create TunNet ECH query: %w", err)
	}
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		baseDialer := &net.Dialer{}
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			return baseDialer.DialContext(ctx, "tcp4", address)
		}
		client = &http.Client{Transport: transport}
	}
	resp, err := client.Do(req)
	if err != nil {
		return ECHConfig{}, fmt.Errorf("perform TunNet ECH query: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ECHConfig{}, fmt.Errorf("TunNet ECH query HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
	if err != nil {
		return ECHConfig{}, fmt.Errorf("read TunNet ECH query: %w", err)
	}
	if len(body) > 64<<10 {
		return ECHConfig{}, errors.New("TunNet ECH query response too large")
	}
	var answer mDNS.Msg
	if err = answer.Unpack(body); err != nil {
		return ECHConfig{}, fmt.Errorf("decode TunNet ECH answer: %w", err)
	}
	if answer.Rcode != mDNS.RcodeSuccess {
		return ECHConfig{}, fmt.Errorf("TunNet ECH DNS rcode %d", answer.Rcode)
	}
	for _, rr := range answer.Answer {
		https, ok := rr.(*mDNS.HTTPS)
		if !ok {
			continue
		}
		if !strings.EqualFold(mDNS.Fqdn(https.Hdr.Name), queryName) {
			continue
		}
		for _, value := range https.Value {
			if value.Key() != mDNS.SVCB_ECHCONFIG {
				continue
			}
			raw, err := base64.StdEncoding.DecodeString(value.String())
			if err != nil || len(raw) == 0 {
				return ECHConfig{}, errors.New("invalid TunNet ECH config list")
			}
			ttl := time.Duration(rr.Header().Ttl) * time.Second
			if ttl <= 0 {
				return ECHConfig{}, errors.New("TunNet ECH answer has zero TTL")
			}
			return ECHConfig{QueryName: queryName, ConfigList: base64.RawURLEncoding.EncodeToString(raw), ExpiresAt: now.Add(ttl).UnixMilli()}, nil
		}
	}
	return ECHConfig{}, errors.New("TunNet ECH answer has no ech parameter")
}
