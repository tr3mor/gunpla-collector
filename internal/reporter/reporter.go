// Package reporter diffs unreported scrape runs for a shop against the
// last-reported one and sends a Telegram summary, plus an alert if the
// most recent collect run failed or appears stuck.
package reporter

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"gunpla-collector/internal/store"
	"gunpla-collector/internal/telegram"
)

// stuckRunThreshold is how long a run can sit in status "running" before
// reporter.Run treats it as crashed (rather than still in progress) and
// alerts on it. Chosen well above the ~30 min collect normally takes.
const stuckRunThreshold = 2 * time.Hour

type Store interface {
	// LatestRun returns the most recent run for shopID regardless of
	// status, so a failed or stuck collect run can be detected and
	// alerted on even when there's nothing new to report.
	LatestRun(ctx context.Context, shopID int64) (run *store.Run, ok bool, err error)
	// LatestUnreportedRun returns the most recent successful run that
	// hasn't been reported yet (current, nil if there's nothing new) and
	// the most recent one that has (previous, nil on the first-ever
	// report).
	LatestUnreportedRun(ctx context.Context, shopID int64) (current *store.Run, previous *store.Run, err error)
	// MarkReported stamps every unreported successful run as reported,
	// making a successful report idempotent: running it again immediately
	// finds nothing new.
	MarkReported(ctx context.Context, shopID int64, now string) error
	NewSets(ctx context.Context, shopID, currentRunID, previousRunID int64) ([]store.ReportItem, error)
	RemovedSets(ctx context.Context, shopID, currentRunID, previousRunID int64) ([]store.ReportItem, error)
	PriceChanges(ctx context.Context, shopID, currentRunID, previousRunID int64) ([]store.PriceChange, error)
}

func Run(ctx context.Context, db Store, shop store.Shop, sender telegram.Sender, logger *slog.Logger) error {
	latest, ok, err := db.LatestRun(ctx, shop.ID)
	if err != nil {
		return fmt.Errorf("find latest run: %w", err)
	}
	if !ok {
		return fmt.Errorf("no collect run yet for %s — run `collect` first", shop.Slug)
	}

	alerted := false
	if reason, stuck := failureReason(latest); stuck {
		logger.Warn("collect run failed or appears stuck", "shop", shop.Slug, "run_id", latest.ID, "status", latest.Status)
		if err := telegram.SendLong(ctx, sender, FormatFailure(shop.Name, reason)); err != nil {
			return fmt.Errorf("send failure alert: %w", err)
		}
		alerted = true
	}

	current, previous, err := db.LatestUnreportedRun(ctx, shop.ID)
	if err != nil {
		return fmt.Errorf("find unreported run: %w", err)
	}
	if current == nil {
		if !alerted {
			logger.Info("nothing new to report", "shop", shop.Slug)
		}
		return nil
	}

	var msg string
	if previous == nil {
		setsFound := 0
		if current.SetsFound.Valid {
			setsFound = int(current.SetsFound.Int64)
		}
		logger.Info("reporting baseline", "shop", shop.Slug, "sets_found", setsFound)
		msg = FormatBaseline(shop.Name, setsFound)
	} else {
		newSets, err := db.NewSets(ctx, shop.ID, current.ID, previous.ID)
		if err != nil {
			return fmt.Errorf("query new sets: %w", err)
		}
		removedSets, err := db.RemovedSets(ctx, shop.ID, current.ID, previous.ID)
		if err != nil {
			return fmt.Errorf("query removed sets: %w", err)
		}
		changes, err := db.PriceChanges(ctx, shop.ID, current.ID, previous.ID)
		if err != nil {
			return fmt.Errorf("query price changes: %w", err)
		}
		logger.Info("reporting diff", "shop", shop.Slug, "new", len(newSets), "removed", len(removedSets), "changed", len(changes))
		msg = FormatDiff(shop.Name, newSets, removedSets, changes)
	}

	if err := telegram.SendLong(ctx, sender, msg); err != nil {
		return fmt.Errorf("send report: %w", err)
	}
	// Only stamp reported_at once the send actually succeeded, so a failed
	// send leaves the run unreported and gets picked up again next time.
	now := time.Now().UTC().Format(time.RFC3339)
	if err := db.MarkReported(ctx, shop.ID, now); err != nil {
		return fmt.Errorf("mark reported: %w", err)
	}
	return nil
}

// failureReason reports whether the shop's latest run counts as failed:
// status "failed" outright, or "running" for longer than
// stuckRunThreshold (a crashed process that never reached
// FinishRunFailed). Returns the text to put in the alert.
func failureReason(run *store.Run) (reason string, failed bool) {
	switch run.Status {
	case "failed":
		if run.Error.Valid && run.Error.String != "" {
			return run.Error.String, true
		}
		return "(no error recorded)", true
	case "running":
		started, err := time.Parse(time.RFC3339, run.StartedAt)
		if err == nil && time.Since(started) > stuckRunThreshold {
			return fmt.Sprintf("still running since %s — likely crashed without recording a failure", run.StartedAt), true
		}
	}
	return "", false
}
