package zoho

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

var (
	// ErrUnauthorized means the token is invalid, revoked, or lacks a scope.
	ErrUnauthorized = errors.New("not authorized: token revoked or missing a scope, run `bozo auth login`")
	// ErrRateLimited means Zoho locked the endpoint. Retrying extends the lock.
	ErrRateLimited = errors.New("rate limited: Zoho locks the endpoint for several minutes, do not retry immediately")
)

// UserAgent is sent with every request; main sets the version.
var UserAgent = "bozo"

// maxResponse caps how much of a response body is read.
const maxResponse = 10 << 20

// APIError is a non-2xx answer. Zoho error bodies look like
// {"code": "...", "message": "..."}.
type APIError struct {
	Status  int
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *APIError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = http.StatusText(e.Status)
	}
	if e.Code != "" {
		msg = e.Code + ": " + msg
	}
	if hint := e.Unwrap(); hint != nil {
		msg += " (" + hint.Error() + ")"
	}
	return fmt.Sprintf("zoho: %d %s", e.Status, msg)
}

func (e *APIError) Unwrap() error {
	switch e.Status {
	case http.StatusUnauthorized:
		return ErrUnauthorized
	case http.StatusTooManyRequests:
		return ErrRateLimited
	}
	return nil
}

// Client calls Zoho service APIs. Its http.Client carries the OAuth token.
type Client struct {
	http    *http.Client
	baseURL func(service string) string
}

// NewClient returns a client for the services of one data center.
func NewClient(hc *http.Client, dc DC) *Client {
	return &Client{http: hc, baseURL: dc.URL}
}

// NewTestClient sends every service to one base URL (an httptest server).
func NewTestClient(hc *http.Client, baseURL string) *Client {
	return &Client{http: hc, baseURL: func(string) string { return baseURL }}
}

// Get calls GET <service host><path>?<query> and decodes the JSON body into out.
func (c *Client) Get(ctx context.Context, service, path string, query url.Values, out any) error {
	u := c.baseURL(service) + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", UserAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body := io.LimitReader(resp.Body, maxResponse)

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		apiErr := &APIError{Status: resp.StatusCode}
		_ = json.NewDecoder(body).Decode(apiErr) // best effort: the status alone is enough
		return apiErr
	}
	if resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(body).Decode(out); err != nil {
		return fmt.Errorf("zoho: decoding %s: %w", path, err)
	}
	return nil
}
