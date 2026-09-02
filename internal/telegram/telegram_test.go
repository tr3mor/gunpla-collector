package telegram

import (
	"context"
	"strings"
	"testing"
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

func TestSendLong_SplitsOnParagraphBoundaries(t *testing.T) {
	var paragraphs []string
	for i := 0; i < 100; i++ {
		paragraphs = append(paragraphs, strings.Repeat("x", 100))
	}
	text := strings.Join(paragraphs, "\n\n")

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

func TestSplitMessage_HardSplitsOversizedParagraph(t *testing.T) {
	huge := strings.Repeat("y", MaxMessageLen*2+10)
	chunks := splitMessage(huge, MaxMessageLen)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks for oversized paragraph, got %d", len(chunks))
	}
	for _, c := range chunks {
		if len(c) > MaxMessageLen {
			t.Errorf("chunk length %d exceeds limit %d", len(c), MaxMessageLen)
		}
	}
}
