package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/engyii/bozo-cli/internal/zoho"
)

// fakeAccounts serves one accounts host per DC under /<dc>/: the device
// code comes from us, the user is in eu, so us answers other_dc.
func fakeAccounts(t *testing.T) *httptest.Server {
	t.Helper()
	polls := 0
	mux := http.NewServeMux()
	reply := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	mux.HandleFunc("POST /us/oauth/v3/device/code", func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue("access_type") != "offline" || r.FormValue("scope") != "A.READ,B.READ" {
			t.Errorf("device code form = %v", r.Form)
		}
		reply(w, map[string]any{"device_code": "dev-1", "user_code": "ABCD-1234", "interval": 1, "expires_in": 60000})
	})
	mux.HandleFunc("POST /us/oauth/v3/device/token", func(w http.ResponseWriter, r *http.Request) {
		polls++
		if polls == 1 {
			reply(w, map[string]any{"error": "authorization_pending"})
			return
		}
		reply(w, map[string]any{"error": "other_dc", "user_location": "eu"})
	})
	mux.HandleFunc("POST /eu/oauth/v3/device/token", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			t.Errorf("secret material in the URL: %q", r.URL.RawQuery)
		}
		if r.FormValue("client_secret") != "s3cret" || r.FormValue("code") != "dev-1" {
			t.Errorf("token form = %v", r.Form)
		}
		reply(w, map[string]any{"access_token": "at", "refresh_token": "rt", "expires_in": 3600})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestDeviceFlowFollowsOtherDC(t *testing.T) {
	minPollInterval = time.Millisecond
	srv := fakeAccounts(t)
	us, _ := zoho.LookupDC("us")
	f := &DeviceFlow{
		HTTP:         srv.Client(),
		ClientID:     "cid",
		ClientSecret: "s3cret",
		Scopes:       []string{"A.READ", "B.READ"},
		DC:           us,
		AccountsURL:  func(dc zoho.DC) string { return srv.URL + "/" + dc.Code },
	}

	da, err := f.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	tok, err := f.Wait(context.Background(), da)
	if err != nil {
		t.Fatal(err)
	}
	if f.DC.Code != "eu" {
		t.Errorf("DC = %s, want eu", f.DC.Code)
	}
	if tok.AccessToken != "at" || tok.RefreshToken != "rt" || time.Until(tok.Expiry) < 59*time.Minute {
		t.Errorf("token = %+v", tok)
	}
}

func TestDeviceFlowAccessDenied(t *testing.T) {
	minPollInterval = time.Millisecond
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"error":"access_denied"}`))
	}))
	defer srv.Close()
	f := &DeviceFlow{HTTP: srv.Client(), AccountsURL: func(zoho.DC) string { return srv.URL }}

	_, err := f.Wait(context.Background(), &DeviceAuth{DeviceCode: "d", ExpiresInMS: 60000})
	if err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("err = %v, want access denied", err)
	}
}
