package cliq

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/engyii/bozo-cli/internal/zoho"
	"golang.org/x/sync/errgroup"
)

// Unread is the result of `bozo cliq unread`, and its --json schema.
type Unread struct {
	Channels []Channel `json:"channels"`
	Total    int       `json:"total_unread"`
}

type Channel struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Unread   int       `json:"unread"`
	Messages []Message `json:"messages,omitempty"` // absent when not fetched
}

type Message struct {
	ID     string    `json:"id"`
	Time   time.Time `json:"time"`
	Sender string    `json:"sender"`
	Text   string    `json:"text"`
}

type UnreadOptions struct {
	CountOnly   bool
	MaxChannels int // channels whose messages are fetched; Messages API allows 15 req/min
	MaxMessages int // per channel
}

// fetchConcurrency is low on purpose: speed is bounded by Zoho's quota anyway.
const fetchConcurrency = 4

func FetchUnread(ctx context.Context, c *zoho.Client, opt UnreadOptions) (*Unread, error) {
	channels, err := listJoinedChannels(ctx, c)
	if err != nil {
		return nil, err
	}
	res := &Unread{Channels: []Channel{}}
	for _, ch := range channels {
		if ch.Unread > 0 {
			res.Channels = append(res.Channels, Channel{ID: ch.ChatID, Name: ch.Name, Unread: ch.Unread})
			res.Total += ch.Unread
		}
	}
	if opt.CountOnly {
		return res, nil
	}

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(fetchConcurrency)
	for i := range min(opt.MaxChannels, len(res.Channels)) {
		ch := &res.Channels[i] // each goroutine writes only its own element
		g.Go(func() error {
			msgs, err := listMessages(ctx, c, ch.ID, min(ch.Unread, opt.MaxMessages))
			if err != nil {
				return fmt.Errorf("messages of #%s: %w", ch.Name, err)
			}
			ch.Messages = toMessages(msgs)
			return nil
		})
	}
	return res, g.Wait()
}

func toMessages(in []apiMessage) []Message {
	out := make([]Message, 0, len(in))
	for _, m := range in {
		out = append(out, Message{
			ID:     m.ID,
			Time:   time.UnixMilli(m.Time),
			Sender: m.Sender.Name,
			Text:   messageText(m),
		})
	}
	slices.SortFunc(out, func(a, b Message) int { return a.Time.Compare(b.Time) })
	return out
}

func messageText(m apiMessage) string {
	switch {
	case m.Content.Text != "":
		return m.Content.Text
	case m.Type == "file":
		return "[file: " + m.Content.File.Name + "]"
	default:
		return "[" + m.Type + "]"
	}
}

// WriteText renders the human-readable form.
func (u *Unread) WriteText(w io.Writer) error {
	if len(u.Channels) == 0 {
		_, err := fmt.Fprintln(w, "No unread channels.")
		return err
	}
	now := time.Now()
	for _, ch := range u.Channels {
		fmt.Fprintf(w, "#%s  %d unread\n", clean(ch.Name, 60), ch.Unread)
		for _, m := range ch.Messages {
			fmt.Fprintf(w, "  %s  %-16s %s\n", stamp(m.Time, now), clean(m.Sender, 16), clean(m.Text, 120))
		}
	}
	_, err := fmt.Fprintf(w, "\n%d channels, %d unread messages\n", len(u.Channels), u.Total)
	return err
}

func stamp(t, now time.Time) string {
	t = t.Local()
	if y, m, d := t.Date(); y == now.Year() && m == now.Month() && d == now.Day() {
		return t.Format("15:04")
	}
	return t.Format("Jan 02 15:04")
}

// clean makes remote text safe for a terminal: control characters (which
// include ESC, the start of every terminal escape sequence) become spaces,
// whitespace runs collapse, and the result is cut to max runes.
func clean(s string, max int) string {
	s = strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)), " ")
	if r := []rune(s); len(r) > max {
		return string(r[:max-1]) + "…"
	}
	return s
}
