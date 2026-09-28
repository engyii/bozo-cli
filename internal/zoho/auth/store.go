package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/zalando/go-keyring"
	"golang.org/x/oauth2"
)

// ErrNotLoggedIn means the profile has no stored credentials.
var ErrNotLoggedIn = errors.New("not logged in: run `bozo auth login`")

// Credentials is everything bozo keeps about one profile. It is stored as a
// single secret; no part of it is written anywhere else.
type Credentials struct {
	DC           string   `json:"dc"`        // the user's data center: API calls go here
	ClientDC     string   `json:"client_dc"` // where the client is registered: login starts here
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	RefreshToken string   `json:"refresh_token"`
	Scopes       []string `json:"scopes"`

	// Cached access token. Zoho limits how many access tokens one refresh
	// token may mint, so frequent runs must reuse it rather than refresh.
	AccessToken string    `json:"access_token,omitempty"`
	Expiry      time.Time `json:"expiry,omitzero"`
}

func (c *Credentials) token() *oauth2.Token {
	return &oauth2.Token{
		AccessToken:  c.AccessToken,
		RefreshToken: c.RefreshToken,
		TokenType:    "Bearer",
		Expiry:       c.Expiry,
	}
}

// Store persists Credentials per profile.
type Store interface {
	Load(profile string) (*Credentials, error)
	Save(profile string, c *Credentials) error
	Delete(profile string) error
	Name() string
}

var profileRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func checkProfile(p string) error {
	if !profileRe.MatchString(p) {
		return fmt.Errorf("invalid profile name %q: use letters, digits, - and _", p)
	}
	return nil
}

// OpenStore returns the OS keyring, or a 0600 file store when
// BOZO_TOKEN_STORE=file. The file store is opt-in: falling back to it
// silently would downgrade security without anyone noticing.
func OpenStore() (Store, error) {
	switch v := os.Getenv("BOZO_TOKEN_STORE"); v {
	case "", "keyring":
		return keyringStore{}, nil
	case "file":
		dir, err := os.UserConfigDir()
		if err != nil {
			return nil, err
		}
		return fileStore{dir: filepath.Join(dir, "bozo", "credentials")}, nil
	default:
		return nil, fmt.Errorf("BOZO_TOKEN_STORE=%q: want keyring or file", v)
	}
}

const keyringService = "bozo"

type keyringStore struct{}

func (keyringStore) Name() string { return "keyring" }

func (keyringStore) Load(profile string) (*Credentials, error) {
	if err := checkProfile(profile); err != nil {
		return nil, err
	}
	s, err := keyring.Get(keyringService, profile)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil, ErrNotLoggedIn
	}
	if err != nil {
		return nil, keyringErr(err)
	}
	var c Credentials
	if err := json.Unmarshal([]byte(s), &c); err != nil {
		return nil, fmt.Errorf("keyring entry %s/%s is corrupt: %w", keyringService, profile, err)
	}
	return &c, nil
}

func (keyringStore) Save(profile string, c *Credentials) error {
	if err := checkProfile(profile); err != nil {
		return err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return keyringErr(keyring.Set(keyringService, profile, string(b)))
}

func (keyringStore) Delete(profile string) error {
	if err := checkProfile(profile); err != nil {
		return err
	}
	if err := keyring.Delete(keyringService, profile); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return keyringErr(err)
	}
	return nil
}

func keyringErr(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("OS keyring unavailable (%w); on a headless machine set BOZO_TOKEN_STORE=file", err)
}

type fileStore struct{ dir string }

func (fileStore) Name() string { return "file" }

func (s fileStore) path(profile string) (string, error) {
	if err := checkProfile(profile); err != nil {
		return "", err
	}
	return filepath.Join(s.dir, profile+".json"), nil
}

func (s fileStore) Load(profile string) (*Credentials, error) {
	p, err := s.path(profile)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotLoggedIn
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// Refuse a readable-by-others file, like ssh does with private keys.
	if fi, err := f.Stat(); err != nil {
		return nil, err
	} else if fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s is accessible by other users (mode %v): run chmod 600 on it", p, fi.Mode().Perm())
	}
	var c Credentials
	if err := json.NewDecoder(f).Decode(&c); err != nil {
		return nil, fmt.Errorf("%s is corrupt: %w", p, err)
	}
	return &c, nil
}

// Save writes atomically: a crash never leaves a truncated credentials file.
func (s fileStore) Save(profile string, c *Credentials) error {
	p, err := s.path(profile)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

func (s fileStore) Delete(profile string) error {
	p, err := s.path(profile)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
