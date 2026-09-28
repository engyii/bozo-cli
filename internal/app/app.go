// Package app is the contract between the CLI shell and each Zoho app
// (cliq, later mail, projects, ...). An app never imports the CLI package.
package app

import (
	"context"
	"encoding/json"
	"io"

	"github.com/engyii/bozo-cli/internal/zoho"
	"github.com/spf13/cobra"
)

// App is one Zoho product exposed as `bozo <name> ...`.
type App struct {
	// Scopes are requested at `bozo auth login`, merged across all apps.
	Scopes  []string
	Command func(*Env) *cobra.Command
}

// Env is what commands get from the shell. Flag-backed fields are filled in
// before any RunE runs.
type Env struct {
	Out     io.Writer
	Err     io.Writer
	JSON    bool
	Profile string

	// Client returns an authenticated client for the current profile.
	Client func(context.Context) (*zoho.Client, error)
}

// Render prints v as JSON with --json, otherwise through text. Commands
// build a typed result and never print directly, so --json stays complete.
func (e *Env) Render(v any, text func(io.Writer) error) error {
	if e.JSON {
		enc := json.NewEncoder(e.Out)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	return text(e.Out)
}
