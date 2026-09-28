package auth

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/engyii/bozo-cli/internal/zoho"
	"golang.org/x/oauth2"
)

// Environment variables that bypass the store, for CI and headless use.
// BOZO_ACCESS_TOKEN wins over BOZO_REFRESH_TOKEN; both use BOZO_DC (default us).
const (
	EnvAccessToken  = "BOZO_ACCESS_TOKEN"
	EnvRefreshToken = "BOZO_REFRESH_TOKEN"
	EnvClientID     = "BOZO_CLIENT_ID"
	EnvClientSecret = "BOZO_CLIENT_SECRET"
	EnvDC           = "BOZO_DC"
)

const httpTimeout = 30 * time.Second

// Session is a resolved identity: where credentials came from and how to
// get a valid access token.
type Session struct {
	Profile string
	Source  string // "keyring", "file" or "env"
	DC      zoho.DC
	Creds   *Credentials // nil with BOZO_ACCESS_TOKEN

	store Store // nil when refreshed tokens must not be persisted
	// Warn reports non-fatal problems, such as a refreshed token that could
	// not be saved.
	Warn func(error)
}

// Load resolves the session: environment first, then the store.
func Load(profile string) (*Session, error) {
	if os.Getenv(EnvAccessToken) != "" || os.Getenv(EnvRefreshToken) != "" {
		return fromEnv(profile)
	}
	store, err := OpenStore()
	if err != nil {
		return nil, err
	}
	creds, err := store.Load(profile)
	if err != nil {
		return nil, err
	}
	dc, err := zoho.LookupDC(creds.DC)
	if err != nil {
		return nil, err
	}
	return &Session{Profile: profile, Source: store.Name(), DC: dc, Creds: creds, store: store}, nil
}

func fromEnv(profile string) (*Session, error) {
	dc, err := zoho.LookupDC(cmp.Or(os.Getenv(EnvDC), "us"))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", EnvDC, err)
	}
	s := &Session{Profile: profile, Source: "env", DC: dc}
	if os.Getenv(EnvAccessToken) != "" {
		return s, nil
	}
	s.Creds = &Credentials{
		DC:           dc.Code,
		ClientID:     os.Getenv(EnvClientID),
		ClientSecret: os.Getenv(EnvClientSecret),
		RefreshToken: os.Getenv(EnvRefreshToken),
	}
	if s.Creds.ClientID == "" || s.Creds.ClientSecret == "" {
		return nil, fmt.Errorf("%s needs %s and %s", EnvRefreshToken, EnvClientID, EnvClientSecret)
	}
	return s, nil
}

// Client returns a Zoho API client that attaches, refreshes and (for stored
// sessions) persists the access token.
func (s *Session) Client(ctx context.Context) *zoho.Client {
	base := &http.Client{Timeout: httpTimeout}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, base) // used for refresh calls
	hc := oauth2.NewClient(ctx, s.tokenSource(ctx))
	hc.Timeout = httpTimeout
	return zoho.NewClient(hc, s.DC)
}

func (s *Session) tokenSource(ctx context.Context) oauth2.TokenSource {
	if s.Creds == nil {
		return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: os.Getenv(EnvAccessToken), TokenType: "Bearer"})
	}
	src := oauthConfig(s.Creds, s.DC).TokenSource(ctx, s.Creds.token())
	return &persistingSource{src: src, s: s, last: s.Creds.AccessToken}
}

func oauthConfig(c *Credentials, dc zoho.DC) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		Scopes:       c.Scopes,
		Endpoint: oauth2.Endpoint{
			TokenURL:  dc.AccountsURL() + "/oauth/v2/token",
			AuthStyle: oauth2.AuthStyleInParams,
		},
	}
}

// persistingSource saves every newly minted access token, so the next run
// reuses it instead of spending one of Zoho's limited refreshes.
type persistingSource struct {
	src  oauth2.TokenSource
	s    *Session
	mu   sync.Mutex
	last string
}

func (p *persistingSource) Token() (*oauth2.Token, error) {
	t, err := p.src.Token()
	if err != nil {
		return nil, fmt.Errorf("refreshing access token (run `bozo auth login` if access was revoked): %w", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if t.AccessToken == p.last {
		return t, nil
	}
	p.last = t.AccessToken
	c := p.s.Creds
	c.AccessToken, c.Expiry = t.AccessToken, t.Expiry
	if t.RefreshToken != "" {
		c.RefreshToken = t.RefreshToken // Zoho does not rotate today; keep up if it starts
	}
	if p.s.store != nil {
		if err := p.s.store.Save(p.s.Profile, c); err != nil && p.s.Warn != nil {
			p.s.Warn(fmt.Errorf("could not cache the refreshed token: %w", err))
		}
	}
	return t, nil
}
