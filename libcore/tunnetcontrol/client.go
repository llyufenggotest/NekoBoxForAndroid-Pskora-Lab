package tunnetcontrol

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	successMediaType = "application/vnd.tunnet.hpke"
	errorMediaType   = "application/json"
	maxControlBody   = 64 << 10
)

// ResponseOpener decrypts a successful control response using the per-request
// X25519 private key. Its implementation is injected until the build toolchain
// exposes the proven crypto/hpke API.
type ResponseOpener interface {
	Open(responsePrivateKey *ecdh.PrivateKey, sealed []byte, operation, clientID string, nonce []byte) ([]byte, error)
}

type Client struct {
	Endpoint   *url.URL
	HTTPClient *http.Client
	Identity   *Identity
	Opener     ResponseOpener
	Now        func() time.Time
}

type ControlError struct {
	StatusCode int
	Code       string `json:"code"`
	Message    string `json:"message"`
	RetryAfter time.Duration
}

func (e *ControlError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("TunNet control %d %s: %s", e.StatusCode, e.Code, e.Message)
	}
	return fmt.Sprintf("TunNet control HTTP %d", e.StatusCode)
}

func (c *Client) Call(ctx context.Context, operation string, payload any, result any) error {
	if c == nil || c.Endpoint == nil || c.Identity == nil || c.Opener == nil {
		return errors.New("incomplete TunNet control client")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal TunNet control request: %w", err)
	}
	if len(body) > maxControlBody {
		return errors.New("TunNet control request is too large")
	}
	endpoint := *c.Endpoint
	endpoint.Path = controlPath
	endpoint.RawPath = ""
	endpoint.RawQuery = ""
	endpoint.Fragment = ""
	if endpoint.Scheme != "https" || endpoint.User != nil {
		return errors.New("invalid TunNet control endpoint")
	}

	curve := ecdh.X25519()
	responsePrivate, err := curve.GenerateKey(randomReader)
	if err != nil {
		return fmt.Errorf("generate TunNet response key: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create TunNet control request: %w", err)
	}
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	nonce, err := SignRequest(req, body, operation, c.Identity, responsePrivate.PublicKey().Bytes(), now)
	if err != nil {
		return err
	}
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	controlClient := *client
	controlClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	response, err := controlClient.Do(req)
	if err != nil {
		return fmt.Errorf("perform TunNet control request: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxControlBody+1))
	if err != nil {
		return fmt.Errorf("read TunNet control response: %w", err)
	}
	if len(responseBody) > maxControlBody {
		return errors.New("TunNet control response is too large")
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil {
		return fmt.Errorf("parse TunNet response content type: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if mediaType != errorMediaType {
			return fmt.Errorf("unexpected TunNet error content type: %s", mediaType)
		}
		controlError := &ControlError{StatusCode: response.StatusCode}
		if err = json.Unmarshal(responseBody, controlError); err != nil {
			return fmt.Errorf("decode TunNet control error: %w", err)
		}
		if controlError.Code == "" && controlError.Message == "" {
			var envelope struct {
				Error *ControlError `json:"error"`
			}
			if err = json.Unmarshal(responseBody, &envelope); err != nil {
				return fmt.Errorf("decode TunNet control error envelope: %w", err)
			}
			if envelope.Error != nil {
				controlError.Code = envelope.Error.Code
				controlError.Message = envelope.Error.Message
			}
		}
		if response.StatusCode == http.StatusTooManyRequests {
			controlError.RetryAfter = parseRetryAfter(response.Header.Get("Retry-After"), now)
		}
		return controlError
	}
	if mediaType != successMediaType {
		return fmt.Errorf("unexpected TunNet success content type: %s", mediaType)
	}
	plaintext, err := c.Opener.Open(responsePrivate, responseBody, operation, strings.ToLower(c.Identity.ClientID), nonce)
	if err != nil {
		return fmt.Errorf("open TunNet sealed response: %w", err)
	}
	if result == nil {
		return nil
	}
	if err = json.Unmarshal(plaintext, result); err != nil {
		return fmt.Errorf("decode TunNet control response: %w", err)
	}
	return nil
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if parsed, err := http.ParseTime(value); err == nil && parsed.After(now) {
		return parsed.Sub(now)
	}
	return 0
}
