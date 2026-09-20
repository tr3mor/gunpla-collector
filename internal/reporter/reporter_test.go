package reporter

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"gunpla-collector/internal/store"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakeStore struct {
	latest         *store.Run
	current        *store.Run
	previous       *store.Run
	newSets        []store.ReportItem
	removedSets    []store.ReportItem
	changes        []store.PriceChange
	markReportedAt []string // one entry per MarkReported call, for assertions
}

func (f *fakeStore) LatestRun(ctx context.Context, shopID int64) (*store.Run, bool, error) {
	return f.latest, f.latest != nil, nil
}
func (f *fakeStore) LatestUnreportedRun(ctx context.Context, shopID int64) (*store.Run, *store.Run, error) {
	return f.current, f.previous, nil
}
func (f *fakeStore) MarkReported(ctx context.Context, shopID int64, now string) error {
	f.markReportedAt = append(f.markReportedAt, now)
	return nil
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

func successRun(id int64) *store.Run {
	return &store.Run{ID: id, Status: "success", StartedAt: "2026-01-01T00:00:00Z"}
}

func TestRun_BaselineWhenNoPreviousRun(t *testing.T) {
	current := &store.Run{ID: 1, Status: "success", SetsFound: sql.NullInt64{Int64: 480, Valid: true}}
	db := &fakeStore{
		latest:  current,
		current: current,
	}
	sender := &fakeSender{}
	shop := store.Shop{ID: 1, Slug: "geeksheaven", Name: "GeeksHeaven"}

	if err := Run(context.Background(), db, shop, sender, discardLogger()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(sender.sent) != 1 || !strings.Contains(sender.sent[0], "480") {
		t.Errorf("sent = %v, want one baseline message mentioning 480", sender.sent)
	}
	if len(db.markReportedAt) != 1 {
		t.Errorf("MarkReported called %d times, want 1", len(db.markReportedAt))
	}
}

func TestRun_DiffWhenPreviousRunExists(t *testing.T) {
	current := successRun(2)
	db := &fakeStore{
		latest:   current,
		current:  current,
		previous: successRun(1),
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
	if len(db.markReportedAt) != 1 {
		t.Errorf("MarkReported called %d times, want 1", len(db.markReportedAt))
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

// Everything already reported means nothing to diff and no message sent —
// this is what makes running `report` twice in a row silent.
func TestRun_NothingNewToReportSendsNothing(t *testing.T) {
	db := &fakeStore{
		latest: successRun(1), // status success, not failed/stuck
		// current is nil: LatestUnreportedRun found nothing unreported.
		previous: successRun(1),
	}
	sender := &fakeSender{}
	shop := store.Shop{ID: 1, Slug: "geeksheaven", Name: "GeeksHeaven"}

	if err := Run(context.Background(), db, shop, sender, discardLogger()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(sender.sent) != 0 {
		t.Errorf("sent = %v, want no messages", sender.sent)
	}
	if len(db.markReportedAt) != 0 {
		t.Errorf("MarkReported called %d times, want 0 (nothing was sent)", len(db.markReportedAt))
	}
}

// The alert must go out even with nothing unreported to diff — a broken
// scraper shouldn't go unnoticed just because there's no diff to send.
func TestRun_AlertsOnFailedCollect(t *testing.T) {
	db := &fakeStore{
		latest: &store.Run{ID: 3, Status: "failed", Error: sql.NullString{String: "site structure changed", Valid: true}},
		// current/previous nil: no unreported successful run to diff.
	}
	sender := &fakeSender{}
	shop := store.Shop{ID: 1, Slug: "geeksheaven", Name: "GeeksHeaven"}

	if err := Run(context.Background(), db, shop, sender, discardLogger()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(sender.sent) != 1 || !strings.Contains(sender.sent[0], "site structure changed") {
		t.Errorf("sent = %v, want one alert mentioning the error", sender.sent)
	}
	if len(db.markReportedAt) != 0 {
		t.Errorf("MarkReported called %d times, want 0 (no diff was sent)", len(db.markReportedAt))
	}
}

// If the latest run failed but an older unreported successful run still
// needs diffing, both messages go out.
func TestRun_AlertsOnFailedCollectThenReportsUnrelatedDiff(t *testing.T) {
	db := &fakeStore{
		latest:   &store.Run{ID: 3, Status: "failed", Error: sql.NullString{String: "boom", Valid: true}},
		current:  successRun(2),
		previous: successRun(1),
	}
	sender := &fakeSender{}
	shop := store.Shop{ID: 1, Slug: "geeksheaven", Name: "GeeksHeaven"}

	if err := Run(context.Background(), db, shop, sender, discardLogger()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(sender.sent) != 2 {
		t.Fatalf("sent = %v, want 2 messages (alert + diff)", sender.sent)
	}
	if !strings.Contains(sender.sent[0], "boom") {
		t.Errorf("sent[0] = %q, want the failure alert first", sender.sent[0])
	}
	if len(db.markReportedAt) != 1 {
		t.Errorf("MarkReported called %d times, want 1", len(db.markReportedAt))
	}
}

// A run stuck "running" long past stuckRunThreshold gets alerted on even
// though it never reached FinishRunFailed — the crashed-process case.
func TestRun_StuckRunningIsTreatedAsFailed(t *testing.T) {
	db := &fakeStore{
		latest: &store.Run{ID: 3, Status: "running", StartedAt: "2020-01-01T00:00:00Z"}, // ancient
	}
	sender := &fakeSender{}
	shop := store.Shop{ID: 1, Slug: "geeksheaven", Name: "GeeksHeaven"}

	if err := Run(context.Background(), db, shop, sender, discardLogger()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(sender.sent) != 1 || !strings.Contains(sender.sent[0], "still running") {
		t.Errorf("sent = %v, want one stuck-run alert", sender.sent)
	}
}

// A run that just started shouldn't trigger a false stuck alert.
func TestRun_RecentlyRunningIsNotStuck(t *testing.T) {
	db := &fakeStore{
		// A fixed timestamp would eventually cross stuckRunThreshold as
		// the test suite ages, so use "now" instead.
		latest: &store.Run{ID: 3, Status: "running", StartedAt: time.Now().UTC().Format(time.RFC3339)},
	}
	sender := &fakeSender{}
	shop := store.Shop{ID: 1, Slug: "geeksheaven", Name: "GeeksHeaven"}

	if err := Run(context.Background(), db, shop, sender, discardLogger()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(sender.sent) != 0 {
		t.Errorf("sent = %v, want no alert for a run that just started", sender.sent)
	}
}
