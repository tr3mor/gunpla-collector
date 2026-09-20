package telegram

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

type recordingSender struct {
	sent []string
}

func (r *recordingSender) SendMessage(ctx context.Context, text string) error {
	r.sent = append(r.sent, text)
	return nil
}

func TestSendLong_ShortMessageSingleSend(t *testing.T) {
	rec := &recordingSender{}
	if err := SendLong(context.Background(), rec, "hello"); err != nil {
		t.Fatalf("SendLong: %v", err)
	}
	if len(rec.sent) != 1 || rec.sent[0] != "hello" {
		t.Errorf("sent = %v, want [\"hello\"]", rec.sent)
	}
}

func TestSendLong_SplitsOnLineBoundaries(t *testing.T) {
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, strings.Repeat("x", 100))
	}
	text := strings.Join(lines, "\n")

	rec := &recordingSender{}
	if err := SendLong(context.Background(), rec, text); err != nil {
		t.Fatalf("SendLong: %v", err)
	}
	if len(rec.sent) < 2 {
		t.Fatalf("expected message to be split into multiple chunks, got %d", len(rec.sent))
	}
	for i, chunk := range rec.sent {
		if len(chunk) > MaxMessageLen {
			t.Errorf("chunk %d has length %d, exceeds MaxMessageLen %d", i, len(chunk), MaxMessageLen)
		}
	}
	// Reassembling should not lose any of the 'x' characters.
	var total int
	for _, chunk := range rec.sent {
		total += strings.Count(chunk, "x")
	}
	if total != 100*100 {
		t.Errorf("total x count = %d, want %d", total, 100*100)
	}
}

func TestSplitMessage_HardSplitsOversizedLine(t *testing.T) {
	huge := strings.Repeat("y", MaxMessageLen*2+10)
	chunks := splitMessage(huge, MaxMessageLen)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks for oversized line, got %d", len(chunks))
	}
	for _, c := range chunks {
		if len(c) > MaxMessageLen {
			t.Errorf("chunk length %d exceeds limit %d", len(c), MaxMessageLen)
		}
	}
}

// A long line of multi-byte runes ("€", "→", "•") must hard-split into
// valid UTF-8 chunks — the old `p[:limit]` byte slicing could cut one in half.
func TestSplitMessage_NeverSplitsMidRune(t *testing.T) {
	huge := strings.Repeat("€→•", MaxMessageLen) // well over the limit, 3-byte runes throughout
	chunks := splitMessage(huge, MaxMessageLen)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if !utf8.ValidString(c) {
			t.Errorf("chunk %d is not valid UTF-8: %q", i, c)
		}
		if len(c) > MaxMessageLen {
			t.Errorf("chunk %d length %d exceeds limit %d", i, len(c), MaxMessageLen)
		}
	}
}

// A large FormatDiff-shaped output must never split with an unbalanced
// <b>/</b> pair in a chunk.
func TestSplitMessage_NeverSplitsInsideHTMLTag(t *testing.T) {
	var b strings.Builder
	b.WriteString("<b>New</b>\n")
	for i := 0; i < 500; i++ {
		fmt.Fprintf(&b, "• Kit Number %d — €54.99\n", i)
	}
	text := strings.TrimRight(b.String(), "\n")

	chunks := splitMessage(text, MaxMessageLen)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if strings.Count(c, "<b>") != strings.Count(c, "</b>") {
			t.Errorf("chunk %d has unbalanced <b> tags:\n%s", i, c)
		}
	}
}

// A 429 with a retry_after hint gets waited out and retried once.
func TestSendMessage_RetriesOnce429ThenSucceeds(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"ok":false,"description":"Too Many Requests: retry after 1","parameters":{"retry_after":1}}`)
			return
		}
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	c := NewClient("token", "chat")
	c.apiBase = srv.URL

	start := time.Now()
	if err := c.SendMessage(context.Background(), "hello"); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2 (initial + one retry)", calls)
	}
	if elapsed := time.Since(start); elapsed < time.Second {
		t.Errorf("elapsed = %v, want at least the 1s retry_after wait", elapsed)
	}
}

// A second consecutive 429 is an error, not another retry.
func TestSendMessage_DoesNotRetryTwice(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"ok":false,"description":"still limited","parameters":{"retry_after":0}}`)
	}))
	defer srv.Close()

	c := NewClient("token", "chat")
	c.apiBase = srv.URL

	if err := c.SendMessage(context.Background(), "hello"); err == nil {
		t.Fatal("expected error, got nil")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (retry_after=0 means don't retry)", calls)
	}
}

// Guards against regressing back to Telegram's deprecated Markdown mode.
func TestSendMessage_UsesHTMLParseMode(t *testing.T) {
	var gotParseMode string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		gotParseMode = r.FormValue("parse_mode")
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	c := NewClient("token", "chat")
	c.apiBase = srv.URL

	if err := c.SendMessage(context.Background(), "hello"); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if gotParseMode != "HTML" {
		t.Errorf("parse_mode = %q, want HTML", gotParseMode)
	}
}
