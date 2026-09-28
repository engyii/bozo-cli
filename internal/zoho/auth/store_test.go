package auth

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestFileStoreRoundTrip(t *testing.T) {
	s := fileStore{dir: filepath.Join(t.TempDir(), "creds")}
	if _, err := s.Load("default"); !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("Load before Save: %v", err)
	}
	want := &Credentials{DC: "eu", ClientID: "cid", RefreshToken: "rt"}
	if err := s.Save("default", want); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(s.dir, "default.json"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
	got, err := s.Load("default")
	if err != nil || got.RefreshToken != "rt" || got.DC != "eu" {
		t.Fatalf("Load = %+v, %v", got, err)
	}
	if err := s.Delete("default"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load("default"); !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("Load after Delete: %v", err)
	}
}

func TestFileStoreRefusesOpenPermissions(t *testing.T) {
	s := fileStore{dir: t.TempDir()}
	if err := s.Save("default", &Credentials{}); err != nil {
		t.Fatal(err)
	}
	os.Chmod(filepath.Join(s.dir, "default.json"), 0o644)
	if _, err := s.Load("default"); err == nil {
		t.Fatal("Load accepted a world-readable credentials file")
	}
}

func TestProfileNameCannotEscapeDir(t *testing.T) {
	s := fileStore{dir: t.TempDir()}
	for _, p := range []string{"../x", "a/b", "", "."} {
		if err := s.Save(p, &Credentials{}); err == nil {
			t.Errorf("Save(%q) accepted", p)
		}
	}
}

type memStore struct{ saved *Credentials }

func (m *memStore) Load(string) (*Credentials, error)   { return m.saved, nil }
func (m *memStore) Save(_ string, c *Credentials) error { cp := *c; m.saved = &cp; return nil }
func (m *memStore) Delete(string) error                 { return nil }
func (m *memStore) Name() string                        { return "mem" }

type fixedSource struct{ tok *oauth2.Token }

func (f fixedSource) Token() (*oauth2.Token, error) { return f.tok, nil }

func TestRefreshedTokenIsPersistedOnce(t *testing.T) {
	store := &memStore{}
	s := &Session{Profile: "default", Creds: &Credentials{RefreshToken: "rt", AccessToken: "old"}, store: store}
	fresh := &oauth2.Token{AccessToken: "new", Expiry: time.Now().Add(time.Hour)}
	p := &persistingSource{src: fixedSource{fresh}, s: s, last: "old"}

	for range 3 {
		if _, err := p.Token(); err != nil {
			t.Fatal(err)
		}
	}
	if store.saved == nil || store.saved.AccessToken != "new" || store.saved.RefreshToken != "rt" {
		t.Fatalf("saved = %+v, want new access token and kept refresh token", store.saved)
	}
}
