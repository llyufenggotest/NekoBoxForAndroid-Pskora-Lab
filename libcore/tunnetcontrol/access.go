package tunnetcontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultAccessPollInterval = 15 * time.Second

type Access struct {
	State             string `json:"state"`
	Ticket            string `json:"ticket,omitempty"`
	RetryAfterSeconds int64  `json:"retry_after_seconds,omitempty"`
	AuthorizationURL  string `json:"authorization_url,omitempty"`
}

type BootstrapResponse struct {
	SchemaVersion int             `json:"schema_version"`
	Release       json.RawMessage `json:"release"`
	Access        Access          `json:"access"`
	Runtime       json.RawMessage `json:"runtime"`
}

type AccessResponse struct {
	State     string            `json:"state"`
	Access    Access            `json:"access"`
	Bootstrap BootstrapResponse `json:"bootstrap"`
	Release   json.RawMessage   `json:"release"`
}

type PollAccessRequest struct {
	Ticket     string `json:"ticket"`
	AppVersion string `json:"app_version"`
}

type AccessMachine struct {
	Client       *Client
	AppVersion   string
	Platform     string
	HTTPClient   *http.Client
	PollInterval time.Duration
	Wait         func(context.Context, time.Duration) error
}

// BootstrapAndAwaitReady obtains a fresh ticket, completes the same-origin
// authorization API transaction, then polls the signed control endpoint until
// the returned bootstrap reports ready.
func (m *AccessMachine) BootstrapAndAwaitReady(ctx context.Context, onUpdate func(Access)) (*BootstrapResponse, error) {
	if m == nil || m.Client == nil {
		return nil, errors.New("missing TunNet access client")
	}
	if strings.TrimSpace(m.AppVersion) == "" || strings.TrimSpace(m.Platform) == "" {
		return nil, errors.New("TunNet access requires client version metadata")
	}
	body, err := marshalIdentityRequest(m.Client.Identity.ClientID, m.Platform, m.AppVersion)
	if err != nil {
		return nil, err
	}
	var bootstrap BootstrapResponse
	if err = m.Client.Call(ctx, "bootstrap", json.RawMessage(body), &bootstrap); err != nil {
		return nil, fmt.Errorf("bootstrap TunNet access: %w", err)
	}
	if bootstrap.SchemaVersion != 2 {
		return nil, fmt.Errorf("unsupported TunNet bootstrap schema: %d", bootstrap.SchemaVersion)
	}
	access := bootstrap.Access
	if onUpdate != nil {
		onUpdate(access)
	}
	if access.State == "required" {
		if err = m.authorize(ctx, access); err != nil {
			return nil, err
		}
	}
	for {
		switch access.State {
		case "ready":
			bootstrap.Access = access
			return &bootstrap, nil
		case "required":
			if access.Ticket == "" {
				return nil, errors.New("TunNet access requires a ticket")
			}
		default:
			return nil, fmt.Errorf("unknown TunNet access state: %q", access.State)
		}

		delay := m.PollInterval
		if delay <= 0 {
			delay = defaultAccessPollInterval
		}
		if access.RetryAfterSeconds > 0 {
			serverDelay := time.Duration(access.RetryAfterSeconds) * time.Second
			if serverDelay > delay {
				delay = serverDelay
			}
		}
		if err = m.wait(ctx, delay); err != nil {
			return nil, err
		}
		var polled AccessResponse
		if err = m.Client.Call(ctx, "access", PollAccessRequest{Ticket: access.Ticket, AppVersion: m.AppVersion}, &polled); err != nil {
			return nil, fmt.Errorf("poll TunNet access: %w", err)
		}
		if polled.State != "completed" || polled.Bootstrap.SchemaVersion != 2 {
			return nil, fmt.Errorf("invalid TunNet access completion envelope: %q/%d", polled.State, polled.Bootstrap.SchemaVersion)
		}
		bootstrap = polled.Bootstrap
		access = bootstrap.Access
		if onUpdate != nil {
			onUpdate(access)
		}
	}
}

type ECHConfig struct {
	QueryName  string `json:"query_name"`
	ConfigList string `json:"config_list"`
	ExpiresAt  int64  `json:"expires_at"`
}

func (c ECHConfig) Valid(now time.Time) bool {
	return strings.TrimSpace(c.QueryName) != "" && strings.TrimSpace(c.ConfigList) != "" && c.ExpiresAt > now.UnixMilli()
}

func (c *Client) now() time.Time {
	if c != nil && c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (m *AccessMachine) authorize(ctx context.Context, access Access) error {
	if access.State != "required" || access.Ticket == "" || access.AuthorizationURL == "" {
		return errors.New("TunNet authorization requires ticket and URL")
	}
	page, err := url.Parse(access.AuthorizationURL)
	if err != nil || page.Scheme != "https" || page.Hostname() == "" || page.User != nil || (page.Port() != "" && page.Port() != "443") {
		return errors.New("invalid TunNet authorization URL")
	}
	target := *page
	target.Path = "/api/v1/access/authorize"
	target.RawPath = ""
	target.RawQuery = ""
	target.Fragment = ""
	body, err := json.Marshal(struct {
		Ticket string `json:"ticket"`
	}{Ticket: access.Ticket})
	if err != nil {
		return fmt.Errorf("marshal TunNet authorization request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create TunNet authorization request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", page.Scheme+"://"+page.Host)
	client := m.HTTPClient
	if client == nil {
		client = m.Client.HTTPClient
	}
	if client == nil {
		client = http.DefaultClient
	}
	authorizationClient := *client
	authorizationClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	response, err := authorizationClient.Do(req)
	if err != nil {
		return fmt.Errorf("perform TunNet authorization request: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxControlBody+1))
	if err != nil {
		return fmt.Errorf("read TunNet authorization response: %w", err)
	}
	if len(responseBody) > maxControlBody {
		return errors.New("TunNet authorization response is too large")
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != errorMediaType || response.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected TunNet authorization response: status=%d content-type=%q", response.StatusCode, mediaType)
	}
	var result struct {
		State string `json:"state"`
	}
	if err = json.Unmarshal(responseBody, &result); err != nil {
		return fmt.Errorf("decode TunNet authorization response: %w", err)
	}
	if result.State != "completed" {
		return fmt.Errorf("TunNet authorization did not complete: %q", result.State)
	}
	return nil
}

func (m *AccessMachine) wait(ctx context.Context, delay time.Duration) error {
	if m.Wait != nil {
		return m.Wait(ctx, delay)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
