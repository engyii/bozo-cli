// Package cli is the command tree: global flags, auth commands, and the
// registered apps.
package cli

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"slices"

	"github.com/engyii/bozo-cli/internal/app"
	"github.com/engyii/bozo-cli/internal/apps/cliq"
	"github.com/engyii/bozo-cli/internal/zoho"
	"github.com/engyii/bozo-cli/internal/zoho/auth"
	"github.com/spf13/cobra"
)

// apps is the registry. Adding a Zoho product = one entry here.
var apps = []app.App{cliq.App}

// Execute runs bozo and returns the process exit code.
func Execute(ctx context.Context, version string) int {
	env := &app.Env{Out: os.Stdout, Err: os.Stderr}
	env.Client = func(ctx context.Context) (*zoho.Client, error) {
		s, err := auth.Load(env.Profile)
		if err != nil {
			return nil, err
		}
		s.Warn = func(err error) { fmt.Fprintln(env.Err, "bozo: warning:", err) }
		return s.Client(ctx), nil
	}

	root := &cobra.Command{
		Use:           "bozo",
		Short:         "Command-line client for Zoho apps",
		Version:       version,
		SilenceUsage:  true, // an API error is not a usage error
		SilenceErrors: true, // printed once, below
	}
	pf := root.PersistentFlags()
	pf.StringVar(&env.Profile, "profile", cmp.Or(os.Getenv("BOZO_PROFILE"), "default"), "credentials profile [$BOZO_PROFILE]")
	pf.BoolVar(&env.JSON, "json", false, "print JSON")

	root.AddCommand(authCommand(env))
	for _, a := range apps {
		root.AddCommand(a.Command(env))
	}

	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(env.Err, "bozo:", err)
		return 1
	}
	return 0
}

// scopes is the union of every app's scopes.
func scopes() []string {
	var all []string
	for _, a := range apps {
		all = append(all, a.Scopes...)
	}
	slices.Sort(all)
	return slices.Compact(all)
}
