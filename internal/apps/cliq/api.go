// Package cliq is the Zoho Cliq app: `bozo cliq ...`.
package cliq

import (
	"context"
	"errors"
	"net/url"
	"strconv"

	"github.com/engyii/bozo-cli/internal/zoho"
)

// Wire types mirror Zoho's JSON and stay private: the JSON bozo prints is
// defined in unread.go, so a Zoho change never silently changes --json.

type apiChannel struct {
	ChatID string `json:"chat_id"`
	Name   string `json:"name"`
	Unread int    `json:"unread_message_count"`
}

type apiMessage struct {
	ID     string `json:"id"`
	Time   int64  `json:"time"` // epoch milliseconds
	Type   string `json:"type"`
	Sender struct {
		Name string `json:"name"`
	} `json:"sender"`
	Content struct {
		Text string `json:"text"`
		File struct {
			Name string `json:"name"`
		} `json:"file"`
	} `json:"content"`
}

const maxPages = 50 // guards against a next_token that never ends

// listJoinedChannels returns joined channels, most recently active first.
// GET /api/v2/channels, scope ZohoCliq.Channels.READ.
func listJoinedChannels(ctx context.Context, c *zoho.Client) ([]apiChannel, error) {
	var all []apiChannel
	q := url.Values{"joined": {"true"}, "order_by": {"-last_modified_time"}}
	for range maxPages {
		var page struct {
			Channels  []apiChannel `json:"channels"`
			NextToken string       `json:"next_token"`
		}
		if err := c.Get(ctx, "cliq", "/api/v2/channels", q, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Channels...)
		if page.NextToken == "" || page.NextToken == q.Get("next_token") {
			return all, nil
		}
		q.Set("next_token", page.NextToken)
	}
	return nil, errors.New("cliq: channel list did not end after 50 pages")
}

// listMessages returns up to limit messages of a chat.
// GET /api/v2/chats/{id}/messages, scope ZohoCliq.Messages.READ, 15 req/min.
func listMessages(ctx context.Context, c *zoho.Client, chatID string, limit int) ([]apiMessage, error) {
	var resp struct {
		Data []apiMessage `json:"data"`
	}
	path := "/api/v2/chats/" + url.PathEscape(chatID) + "/messages"
	err := c.Get(ctx, "cliq", path, url.Values{"limit": {strconv.Itoa(limit)}}, &resp)
	return resp.Data, err
}
