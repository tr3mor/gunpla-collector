// Package telegram is a thin client for the Telegram Bot API's sendMessage
// call — no SDK needed, it's one HTTP call.
package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxMessageLen is Telegram's message length limit; callers should split
// longer text into multiple messages.
const MaxMessageLen = 4096

type Client struct {
	httpClient *http.Client
	token      string
	chatID     string
	apiBase    string // overridable in tests
}

func NewClient(token, chatID string) *Client {
	return &Client{
		// http.DefaultClient has no timeout, so a stalled connection would
		// hang the process indefinitely (the whole-run context timeout in
		// main.go is a backstop, but a request-level timeout catches it
		// much sooner and gives a clearer error).
		httpClient: &http.Client{Timeout: 30 * time.Second},
		token:      token,
		chatID:     chatID,
		apiBase:    "https://api.telegram.org",
	}
}

type sendMessageResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
	Parameters  *struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

// SendMessage sends one message. If Telegram responds 429 Too Many
// Requests with a retry_after hint, it waits that long (context-aware —
// SIGTERM or the whole-run timeout interrupts the wait) and retries
// exactly once before giving up.
func (c *Client) SendMessage(ctx context.Context, text string) error {
	retryAfter, err := c.sendOnce(ctx, text)
	if err == nil || retryAfter <= 0 {
		return err
	}
	if sleepErr := sleepCtx(ctx, time.Duration(retryAfter)*time.Second); sleepErr != nil {
		return fmt.Errorf("waiting out Telegram rate limit: %w", sleepErr)
	}
	_, err = c.sendOnce(ctx, text)
	return err
}

// sendOnce performs a single sendMessage call. On a 429 response with a
// retry_after hint, it returns that (in seconds) alongside the error so
// SendMessage can decide whether to wait and retry.
func (c *Client) sendOnce(ctx context.Context, text string) (retryAfterSeconds int, err error) {
	endpoint := fmt.Sprintf("%s/bot%s/sendMessage", c.apiBase, c.token)

	form := url.Values{}
	form.Set("chat_id", c.chatID)
	form.Set("text", text)
	form.Set("parse_mode", "HTML")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("send telegram message: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var parsed sendMessageResponse
	_ = json.Unmarshal(body, &parsed)

	if resp.StatusCode != http.StatusOK || !parsed.OK {
		desc := parsed.Description
		if desc == "" {
			desc = string(body)
		}
		err := fmt.Errorf("telegram API error (status %d): %s", resp.StatusCode, desc)
		if resp.StatusCode == http.StatusTooManyRequests && parsed.Parameters != nil && parsed.Parameters.RetryAfter > 0 {
			return parsed.Parameters.RetryAfter, err
		}
		return 0, err
	}
	return 0, nil
}

// sleepCtx waits out d, returning ctx's error early if ctx is cancelled
// first. Mirrors internal/scraper's sleepCtx — duplicated rather than
// shared across packages for one small function with no other coupling
// between them.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Sender is satisfied by *Client; it exists so callers can be tested
// against a fake without hitting the network.
type Sender interface {
	SendMessage(ctx context.Context, text string) error
}

// SendLong splits text into chunks that stay under Telegram's 4096-char
// limit, then sends each chunk in order.
func SendLong(ctx context.Context, c Sender, text string) error {
	for _, chunk := range splitMessage(text, MaxMessageLen) {
		if err := c.SendMessage(ctx, chunk); err != nil {
			return err
		}
	}
	return nil
}

// splitMessage splits text into chunks of at most limit bytes, breaking
// only at line boundaries. Every line format.go writes is a complete unit
// — a section header or one "• Name — price" item — so splitting on "\n"
// instead of on bytes means a chunk boundary never lands inside an HTML
// entity like "<b>...</b>" (which would leave a tag unclosed and make
// Telegram reject the whole chunk) or inside a multi-byte UTF-8 rune (a
// "€", "→", "•", or emoji, which would produce invalid UTF-8). A single
// line longer than limit on its own is hard-split on rune boundaries as a
// last resort.
func splitMessage(text string, limit int) []string {
	if len(text) <= limit {
		return []string{text}
	}
	var chunks []string
	lines := strings.Split(text, "\n")
	var current strings.Builder

	flush := func() {
		if current.Len() > 0 {
			chunks = append(chunks, strings.TrimRight(current.String(), "\n"))
			current.Reset()
		}
	}

	for _, line := range lines {
		if current.Len() > 0 && current.Len()+len(line)+1 > limit {
			flush()
		}
		if len(line) > limit {
			flush()
			chunks = append(chunks, splitRunes(line, limit)...)
			continue
		}
		current.WriteString(line)
		current.WriteString("\n")
	}
	flush()
	return chunks
}

// splitRunes hard-splits s into chunks of at most limit bytes each,
// without ever cutting a multi-byte UTF-8 rune in half.
func splitRunes(s string, limit int) []string {
	var chunks []string
	for len(s) > limit {
		cut := limit
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		if cut == 0 {
			// No valid rune boundary within the first `limit` bytes — a
			// single rune wider than limit, which can't happen for valid
			// UTF-8 at any sane limit. Fail safe by cutting at limit
			// anyway rather than looping forever.
			cut = limit
		}
		chunks = append(chunks, s[:cut])
		s = s[cut:]
	}
	if s != "" {
		chunks = append(chunks, s)
	}
	return chunks
}
