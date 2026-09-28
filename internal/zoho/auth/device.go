// Package auth implements Zoho OAuth for a CLI: device-code login, token
// storage, and an http.Client that refreshes and persists tokens.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/engyii/bozo-cli/internal/zoho"
	"golang.org/x/oauth2"
)

// DeviceAuth is the answer to a device-code request. Zoho gives Interval and
// ExpiresIn in milliseconds, unlike RFC 8628; that and the other_dc hop are
// why this flow is not golang.org/x/oauth2's DeviceAuth.
type DeviceAuth struct {
	UserCode                string `json:"user_code"`
	DeviceCode              string `json:"device_code"`
	VerificationURL         string `json:"verification_url"`
	VerificationURLComplete string `json:"verification_uri_complete"`
	IntervalMS              int64  `json:"interval"`
	ExpiresInMS             int64  `json:"expires_in"`
}

// DeviceFlow runs Zoho's "Non-browser Applications" OAuth flow.
// Docs: https://www.zoho.com/accounts/protocol/oauth/devices/overview.html
type DeviceFlow struct {
	HTTP         *http.Client
	ClientID     string
	ClientSecret string
	Scopes       []string
	DC           zoho.DC // where the client is registered; updated on other_dc

	// AccountsURL overrides DC.AccountsURL, for tests.
	AccountsURL func(zoho.DC) string
}

// minPollInterval is a var so tests can shorten it.
var minPollInterval = 5 * time.Second // Zoho answers slow_down below this

func (f *DeviceFlow) accounts() string {
	if f.AccountsURL != nil {
		return f.AccountsURL(f.DC)
	}
	return f.DC.AccountsURL()
}

// Start requests a device code and user code.
func (f *DeviceFlow) Start(ctx context.Context) (*DeviceAuth, error) {
	var resp struct {
		DeviceAuth
		Error string `json:"error"`
	}
	err := postForm(ctx, f.HTTP, f.accounts()+"/oauth/v3/device/code", url.Values{
		"grant_type":  {"device_request"},
		"client_id":   {f.ClientID},
		"scope":       {strings.Join(f.Scopes, ",")},
		"access_type": {"offline"}, // required to receive a refresh token
		"prompt":      {"consent"},
	}, &resp)
	if err != nil {
		return nil, fmt.Errorf("requesting device code: %w", err)
	}
	if resp.Error != "" || resp.DeviceCode == "" {
		return nil, fmt.Errorf("requesting device code from %s: %s (the client must be requested from the data center it is registered in: check --dc)", f.accounts(), orUnknown(resp.Error))
	}
	return &resp.DeviceAuth, nil
}

// Wait polls until the user approves, denies, or the code expires. On
// success f.DC is the user's data center.
func (f *DeviceFlow) Wait(ctx context.Context, da *DeviceAuth) (*oauth2.Token, error) {
	expiry := time.Duration(da.ExpiresInMS) * time.Millisecond
	if expiry <= 0 {
		expiry = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, expiry)
	defer cancel()

	interval := max(time.Duration(da.IntervalMS)*time.Millisecond, minPollInterval)
	for {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil, errors.New("login code expired: run `bozo auth login` again")
			}
			return nil, ctx.Err()
		case <-time.After(interval):
		}

		var resp struct {
			Error        string `json:"error"`
			UserLocation string `json:"user_location"`
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			ExpiresIn    int64  `json:"expires_in"` // seconds, unlike the device code
		}
		err := postForm(ctx, f.HTTP, f.accounts()+"/oauth/v3/device/token", url.Values{
			"grant_type":    {"device_token"},
			"client_id":     {f.ClientID},
			"client_secret": {f.ClientSecret},
			"code":          {da.DeviceCode},
		}, &resp)
		if err != nil {
			return nil, fmt.Errorf("polling for token: %w", err)
		}

		switch resp.Error {
		case "authorization_pending":
		case "slow_down":
			interval += 5 * time.Second
		case "other_dc":
			dc, err := zoho.LookupDC(resp.UserLocation)
			if err != nil {
				return nil, err
			}
			f.DC = dc
		case "access_denied":
			return nil, errors.New("access denied in the browser")
		case "expired":
			return nil, errors.New("login code expired: run `bozo auth login` again")
		case "":
			if resp.AccessToken == "" || resp.RefreshToken == "" {
				return nil, errors.New("token response has no access or refresh token")
			}
			return &oauth2.Token{
				AccessToken:  resp.AccessToken,
				RefreshToken: resp.RefreshToken,
				TokenType:    "Bearer",
				Expiry:       time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second),
			}, nil
		default:
			return nil, fmt.Errorf("login failed: %s", resp.Error)
		}
	}
}

// Revoke invalidates a refresh token at Zoho.
func Revoke(ctx context.Context, hc *http.Client, dc zoho.DC, refreshToken string) error {
	var resp struct {
		Error string `json:"error"`
	}
	err := postForm(ctx, hc, dc.AccountsURL()+"/oauth/v2/token/revoke", url.Values{"token": {refreshToken}}, &resp)
	if err == nil && resp.Error != "" {
		err = errors.New(resp.Error)
	}
	return err
}

// postForm sends a form-encoded POST (secrets stay out of URLs and server
// logs) and decodes the JSON answer whatever the status: Zoho reports OAuth
// errors in the body, sometimes with a 200.
func postForm(ctx context.Context, hc *http.Client, endpoint string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", zoho.UserAgent)

	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
		return fmt.Errorf("%s: HTTP %d with an unreadable body", endpoint, resp.StatusCode)
	}
	return nil
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown error"
	}
	return s
}
