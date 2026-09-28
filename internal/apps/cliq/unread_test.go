package cliq

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/engyii/bozo-cli/internal/zoho"
)

// The fixtures in testdata/ follow the response shapes in Zoho's v2 docs.
// Replace them with recorded (and anonymised) responses once available.
func fakeCliq(t *testing.T) (*zoho.Client, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	serve := func(file string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			b, err := os.ReadFile("testdata/" + file)
			if err != nil {
				t.Error(err)
			}
			w.Write(b)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v2/channels", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("next_token") == "page2" {
			serve("channels-page2.json")(w, r)
			return
		}
		serve("channels-page1.json")(w, r)
	})
	mux.HandleFunc("GET /api/v2/chats/{id}/messages", func(w http.ResponseWriter, r *http.Request) {
		serve("messages-"+r.PathValue("id")+".json")(w, r)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return zoho.NewTestClient(srv.Client(), srv.URL), &calls
}

func TestFetchUnread(t *testing.T) {
	c, _ := fakeCliq(t)
	got, err := FetchUnread(context.Background(), c, UnreadOptions{MaxChannels: 10, MaxMessages: 20})
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 3 || len(got.Channels) != 2 {
		t.Fatalf("total=%d channels=%d, want 3 and 2 (read channels dropped, both pages read)", got.Total, len(got.Channels))
	}
	eng := got.Channels[0]
	if len(eng.Messages) != 2 || eng.Messages[0].Sender != "Alice" {
		t.Errorf("engineering messages = %+v, want oldest first", eng.Messages)
	}
	if txt := got.Channels[1].Messages[0].Text; txt != "[file: holidays.pdf]" {
		t.Errorf("file message text = %q", txt)
	}
}

func TestFetchUnreadCountOnlyIsCheap(t *testing.T) {
	c, calls := fakeCliq(t)
	got, err := FetchUnread(context.Background(), c, UnreadOptions{CountOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if n := calls.Load(); n != 2 { // two channel pages, no message calls
		t.Errorf("API calls = %d, want 2", n)
	}
	if got.Channels[0].Messages != nil {
		t.Error("messages fetched with CountOnly")
	}
}

func TestFetchUnreadRespectsMaxChannels(t *testing.T) {
	c, _ := fakeCliq(t)
	got, err := FetchUnread(context.Background(), c, UnreadOptions{MaxChannels: 1, MaxMessages: 20})
	if err != nil {
		t.Fatal(err)
	}
	if got.Channels[1].Messages != nil {
		t.Error("fetched messages beyond --max-channels")
	}
}

func TestRateLimitIsReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"message":"Too many requests within a certain time frame."}`))
	}))
	defer srv.Close()
	_, err := FetchUnread(context.Background(), zoho.NewTestClient(srv.Client(), srv.URL), UnreadOptions{})
	if !errors.Is(err, zoho.ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
}

func TestCleanStripsTerminalEscapes(t *testing.T) {
	got := clean("hi\x1b]52;c;ZXZpbA==\x07 there\n\tfriend", 100)
	if strings.ContainsAny(got, "\x1b\x07\n\t") {
		t.Errorf("clean left control characters: %q", got)
	}
	if got := clean("abcdef", 4); got != "abc…" {
		t.Errorf("truncate = %q", got)
	}
}
