package reporter

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"strings"
	"testing"

	"gunpla-collector/internal/store"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakeStore struct {
	current, previous *store.Run
	newSets           []store.ReportItem
	removedSets       []store.ReportItem
	changes           []store.PriceChange
}

func (f *fakeStore) TwoMostRecentSuccessfulRuns(ctx context.Context, shopID int64) (*store.Run, *store.Run, error) {
	return f.current, f.previous, nil
}
func (f *fakeStore) NewSets(ctx context.Context, shopID, currentRunID, previousRunID int64) ([]store.ReportItem, error) {
	return f.newSets, nil
}
func (f *fakeStore) RemovedSets(ctx context.Context, shopID, currentRunID, previousRunID int64) ([]store.ReportItem, error) {
	return f.removedSets, nil
}
func (f *fakeStore) PriceChanges(ctx context.Context, shopID, currentRunID, previousRunID int64) ([]store.PriceChange, error) {
	return f.changes, nil
}

type fakeSender struct {
	sent []string
}

func (f *fakeSender) SendMessage(ctx context.Context, text string) error {
	f.sent = append(f.sent, text)
	return nil
}

func TestRun_BaselineWhenNoPreviousRun(t *testing.T) {
	db := &fakeStore{
		current: &store.Run{ID: 1, SetsFound: sql.NullInt64{Int64: 480, Valid: true}},
	}
	sender := &fakeSender{}
	shop := store.Shop{ID: 1, Slug: "geeksheaven", Name: "GeeksHeaven"}

	if err := Run(context.Background(), db, shop, sender, discardLogger()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(sender.sent) != 1 || !strings.Contains(sender.sent[0], "480") {
		t.Errorf("sent = %v, want one baseline message mentioning 480", sender.sent)
	}
}

func TestRun_DiffWhenPreviousRunExists(t *testing.T) {
	db := &fakeStore{
		current:  &store.Run{ID: 2},
		previous: &store.Run{ID: 1},
		newSets:  []store.ReportItem{{Name: "New Kit", PriceCents: 5000}},
	}
	sender := &fakeSender{}
	shop := store.Shop{ID: 1, Slug: "geeksheaven", Name: "GeeksHeaven"}

	if err := Run(context.Background(), db, shop, sender, discardLogger()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(sender.sent) != 1 || !strings.Contains(sender.sent[0], "New Kit") {
		t.Errorf("sent = %v, want one diff message mentioning New Kit", sender.sent)
	}
}

func TestRun_NoRunsYetIsAnError(t *testing.T) {
	db := &fakeStore{}
	sender := &fakeSender{}
	shop := store.Shop{ID: 1, Slug: "geeksheaven", Name: "GeeksHeaven"}

	if err := Run(context.Background(), db, shop, sender, discardLogger()); err == nil {
		t.Fatal("expected error when there is no successful run yet")
	}
	if len(sender.sent) != 0 {
		t.Errorf("expected no message sent, got %v", sender.sent)
	}
}
