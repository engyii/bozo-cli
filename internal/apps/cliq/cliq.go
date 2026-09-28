package cliq

import (
	"errors"

	"github.com/engyii/bozo-cli/internal/app"
	"github.com/spf13/cobra"
)

var App = app.App{
	Scopes: []string{
		"ZohoCliq.Channels.READ",
		"ZohoCliq.Messages.READ",
	},
	Command: command,
}

func command(env *app.Env) *cobra.Command {
	cmd := &cobra.Command{Use: "cliq", Short: "Zoho Cliq"}
	cmd.AddCommand(unreadCommand(env))
	return cmd
}

func unreadCommand(env *app.Env) *cobra.Command {
	opt := UnreadOptions{}
	cmd := &cobra.Command{
		Use:   "unread",
		Short: "List joined channels with unread messages",
		Long: `List joined channels with unread messages, and the messages themselves.
Read-only: nothing is marked as read.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if opt.MaxChannels < 0 || opt.MaxMessages < 1 {
				return errors.New("--max-channels must be >= 0 and --max-messages >= 1")
			}
			c, err := env.Client(cmd.Context())
			if err != nil {
				return err
			}
			res, err := FetchUnread(cmd.Context(), c, opt)
			if err != nil {
				return err
			}
			return env.Render(res, res.WriteText)
		},
	}
	f := cmd.Flags()
	f.BoolVar(&opt.CountOnly, "count-only", false, "only count unread messages (one API call)")
	f.IntVar(&opt.MaxChannels, "max-channels", 10, "fetch messages for at most this many channels (Zoho allows 15 message calls/min)")
	f.IntVar(&opt.MaxMessages, "max-messages", 20, "show at most this many messages per channel")
	return cmd
}
