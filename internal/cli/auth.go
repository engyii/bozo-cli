package cli

import (
	"bufio"
	"cmp"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/cli/browser"
	"github.com/engyii/bozo-cli/internal/app"
	"github.com/engyii/bozo-cli/internal/zoho"
	"github.com/engyii/bozo-cli/internal/zoho/auth"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func authCommand(env *app.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Log in to Zoho and manage stored credentials",
		Long: `Credentials are stored in the OS keyring, or in a 0600 file under
$XDG_CONFIG_HOME/bozo when BOZO_TOKEN_STORE=file.

For CI and headless use, skip the store with environment variables:
  BOZO_ACCESS_TOKEN                                     a ready access token
  BOZO_REFRESH_TOKEN + BOZO_CLIENT_ID + BOZO_CLIENT_SECRET   refreshed in memory
Both use BOZO_DC (default: us).`,
	}
	cmd.AddCommand(loginCommand(env), statusCommand(env), logoutCommand(env))
	return cmd
}

func loginCommand(env *app.Env) *cobra.Command {
	var (
		clientID, dcCode       string
		noBrowser, secretStdin bool
	)
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in with a device code, approved in the browser",
		Long: `Log in with Zoho's device-code flow. bozo opens the approval page in your
browser (or prints it with --no-browser, or over SSH) and waits.

Release builds carry bozo's own OAuth client, so no flag is needed. To use
your own "Non-browser Applications" client (required with a local build),
pass --client-id; its secret is read from $BOZO_CLIENT_SECRET, from stdin
with --client-secret-stdin, or prompted for. It is never accepted as a flag,
because flags end up in shell history and in the process list.

Logging in again reuses the stored client and requests any new scopes.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := auth.OpenStore()
			if err != nil {
				return err
			}
			prev, err := store.Load(env.Profile)
			if err != nil && !errors.Is(err, auth.ErrNotLoggedIn) {
				return err
			}
			if prev == nil {
				prev = &auth.Credentials{}
			}

			clientID = cmp.Or(clientID, os.Getenv(auth.EnvClientID), prev.ClientID, builtinClientID)
			if clientID == "" {
				return errors.New("this build has no built-in client: pass --client-id")
			}
			if clientID != prev.ClientID {
				prev = &auth.Credentials{} // another client: its stored secret does not apply
			}
			if clientID == builtinClientID && prev.ClientSecret == "" {
				prev.ClientSecret = builtinClientSecret
			}
			homeDC := cmp.Or(prev.ClientDC, prev.DC)
			if clientID == builtinClientID {
				homeDC = builtinClientDC
			}
			dc, err := zoho.LookupDC(cmp.Or(dcCode, os.Getenv(auth.EnvDC), homeDC, "us"))
			if err != nil {
				return err
			}
			secret, err := clientSecret(cmd, secretStdin, prev)
			if err != nil {
				return err
			}

			flow := &auth.DeviceFlow{
				HTTP:         &http.Client{Timeout: 30 * time.Second},
				ClientID:     clientID,
				ClientSecret: secret,
				Scopes:       scopes(),
				DC:           dc,
			}
			da, err := flow.Start(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Fprintf(env.Err, "Approve bozo at %s\nand enter the code: %s\n", da.VerificationURL, da.UserCode)
			if !noBrowser && os.Getenv("SSH_CONNECTION") == "" && da.VerificationURLComplete != "" {
				browser.Stdout, browser.Stderr = io.Discard, io.Discard
				if browser.OpenURL(da.VerificationURLComplete) == nil {
					fmt.Fprintln(env.Err, "(opened in your browser)")
				}
			}
			fmt.Fprintln(env.Err, "Waiting for approval...")

			tok, err := flow.Wait(cmd.Context(), da)
			if err != nil {
				return err
			}
			creds := &auth.Credentials{
				DC:           flow.DC.Code,
				ClientDC:     dc.Code,
				ClientID:     clientID,
				ClientSecret: secret,
				RefreshToken: tok.RefreshToken,
				Scopes:       flow.Scopes,
				AccessToken:  tok.AccessToken,
				Expiry:       tok.Expiry,
			}
			if err := store.Save(env.Profile, creds); err != nil {
				return err
			}
			fmt.Fprintf(env.Err, "Logged in: profile %s, data center %s, stored in %s.\n", env.Profile, flow.DC.Code, store.Name())
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&clientID, "client-id", "", "OAuth client ID [$BOZO_CLIENT_ID]")
	f.StringVar(&dcCode, "dc", "", "data center where the OAuth client is registered: "+strings.Join(zoho.DCCodes(), ", ")+" [$BOZO_DC] (default: the built-in client's, else us)")
	f.BoolVar(&noBrowser, "no-browser", false, "print the approval URL without opening a browser")
	f.BoolVar(&secretStdin, "client-secret-stdin", false, "read the client secret from stdin")
	return cmd
}

func clientSecret(cmd *cobra.Command, fromStdin bool, prev *auth.Credentials) (string, error) {
	if s := os.Getenv(auth.EnvClientSecret); s != "" {
		return s, nil
	}
	if fromStdin {
		line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		return requireSecret(strings.TrimSpace(line))
	}
	if prev.ClientSecret != "" {
		return prev.ClientSecret, nil
	}
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", errors.New("no client secret: set $BOZO_CLIENT_SECRET or pass --client-secret-stdin")
	}
	fmt.Fprint(cmd.ErrOrStderr(), "Client secret: ")
	b, err := term.ReadPassword(fd) // no echo
	fmt.Fprintln(cmd.ErrOrStderr())
	if err != nil {
		return "", err
	}
	return requireSecret(strings.TrimSpace(string(b)))
}

func requireSecret(s string) (string, error) {
	if s == "" {
		return "", errors.New("empty client secret")
	}
	return s, nil
}

type status struct {
	Profile       string    `json:"profile"`
	Source        string    `json:"source"`
	DC            string    `json:"dc"`
	ClientID      string    `json:"client_id,omitempty"`
	Scopes        []string  `json:"scopes,omitempty"`
	MissingScopes []string  `json:"missing_scopes,omitempty"`
	TokenExpiry   time.Time `json:"access_token_expiry,omitzero"`
}

func statusCommand(env *app.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show where credentials come from (never prints secrets)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := auth.Load(env.Profile)
			if err != nil {
				return err
			}
			st := status{Profile: s.Profile, Source: s.Source, DC: s.DC.Code}
			if c := s.Creds; c != nil {
				st.ClientID, st.Scopes, st.TokenExpiry = c.ClientID, c.Scopes, c.Expiry
				if s.Source != "env" { // env credentials do not record their scopes
					for _, want := range scopes() {
						if !slices.Contains(c.Scopes, want) {
							st.MissingScopes = append(st.MissingScopes, want)
						}
					}
				}
			}
			return env.Render(st, func(w io.Writer) error {
				fmt.Fprintf(w, "Profile:  %s\nSource:   %s\nDC:       %s\n", st.Profile, st.Source, st.DC)
				if st.ClientID != "" {
					fmt.Fprintf(w, "Client:   %s\n", st.ClientID)
				}
				if len(st.Scopes) > 0 {
					fmt.Fprintf(w, "Scopes:   %s\n", strings.Join(st.Scopes, ", "))
				}
				if !st.TokenExpiry.IsZero() {
					fmt.Fprintf(w, "Token:    cached, expires %s\n", st.TokenExpiry.Local().Format(time.DateTime))
				}
				if len(st.MissingScopes) > 0 {
					fmt.Fprintf(w, "Missing:  %s (run `bozo auth login`)\n", strings.Join(st.MissingScopes, ", "))
				}
				return nil
			})
		},
	}
}

func logoutCommand(env *app.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Revoke the refresh token at Zoho and delete stored credentials",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := auth.OpenStore()
			if err != nil {
				return err
			}
			creds, err := store.Load(env.Profile)
			if errors.Is(err, auth.ErrNotLoggedIn) {
				fmt.Fprintf(env.Err, "Profile %s is not logged in.\n", env.Profile)
				return nil
			}
			if err != nil {
				return err
			}
			if dc, err := zoho.LookupDC(creds.DC); err == nil {
				hc := &http.Client{Timeout: 30 * time.Second}
				if err := auth.Revoke(cmd.Context(), hc, dc, creds.RefreshToken); err != nil {
					fmt.Fprintf(env.Err, "bozo: warning: revoking at Zoho failed (%v); revoke it under Connected Apps at %s\n", err, dc.AccountsURL())
				}
			}
			if err := store.Delete(env.Profile); err != nil {
				return err
			}
			fmt.Fprintf(env.Err, "Logged out of profile %s.\n", env.Profile)
			return nil
		},
	}
}
