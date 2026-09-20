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
}

func (c *Client) SendMessage(ctx context.Context, text string) error {
	endpoint := fmt.Sprintf("%s/bot%s/sendMessage", c.apiBase, c.token)

	form := url.Values{}
	form.Set("chat_id", c.chatID)
	form.Set("text", text)
	form.Set("parse_mode", "Markdown")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send telegram message: %w", err)
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
		return fmt.Errorf("telegram API error (status %d): %s", resp.StatusCode, desc)
	}
	return nil
}

// Sender is satisfied by *Client; it exists so callers can be tested
// against a fake without hitting the network.
type Sender interface {
	SendMessage(ctx context.Context, text string) error
}

// SendLong splits text on paragraph boundaries so each chunk stays under
// Telegram's 4096-char limit, then sends each chunk in order.
func SendLong(ctx context.Context, c Sender, text string) error {
	for _, chunk := range splitMessage(text, MaxMessageLen) {
		if err := c.SendMessage(ctx, chunk); err != nil {
			return err
		}
	}
	return nil
}

func splitMessage(text string, limit int) []string {
	if len(text) <= limit {
		return []string{text}
	}
	var chunks []string
	paragraphs := strings.Split(text, "\n\n")
	var current strings.Builder
	flush := func() {
		if current.Len() > 0 {
			chunks = append(chunks, strings.TrimRight(current.String(), "\n"))
			current.Reset()
		}
	}
	for _, p := range paragraphs {
		if current.Len()+len(p)+2 > limit {
			flush()
		}
		if len(p) > limit {
			// A single paragraph exceeds the limit on its own — hard-split it.
			flush()
			for len(p) > limit {
				chunks = append(chunks, p[:limit])
				p = p[limit:]
			}
			if p != "" {
				current.WriteString(p)
				current.WriteString("\n\n")
			}
			continue
		}
		current.WriteString(p)
		current.WriteString("\n\n")
	}
	flush()
	return chunks
}
