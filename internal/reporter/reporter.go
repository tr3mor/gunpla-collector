// Package reporter diffs a shop's unreported scrape runs against the
// last-reported one and sends a Telegram summary, alerting instead if the
// most recent collect run failed or appears stuck.
package reporter

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"gunpla-collector/internal/store"
	"gunpla-collector/internal/telegram"
)

// stuckRunThreshold is how long a run can sit in status "running" before
// reporter.Run treats it as crashed (rather than still in progress) and
// alerts on it. Chosen well above the ~30 min collect normally takes.
const stuckRunThreshold = 2 * time.Hour

// minReportablePricePct is the minimum |percentage change| a price change
// must clear to be reported. Below this, changes are indistinguishable from
// currency-conversion rounding noise (shops that price in a foreign
// currency have been seen swinging ~0.5% run to run with no real price
// change) rather than an actual discount.
const minReportablePricePct = 5.0

type Store interface {
	// LatestRun returns the most recent run regardless of status, so a
	// failed or stuck collect can be caught even with nothing new to report.
	LatestRun(ctx context.Context, shopID int64) (run *store.Run, ok bool, err error)
	// LatestUnreportedRun returns the newest successful run not yet
	// reported (current, nil if there's nothing new) and the newest one
	// that has been (previous, nil on the first-ever report).
	LatestUnreportedRun(ctx context.Context, shopID int64) (current *store.Run, previous *store.Run, err error)
	// MarkReported stamps every unreported successful run as reported, so
	// running report again right away finds nothing new.
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
		reportableChanges := filterReportableChanges(changes)
		logger.Info("reporting diff", "shop", shop.Slug, "new", len(newSets), "removed", len(removedSets),
			"changed", len(reportableChanges), "changed_filtered_out", len(changes)-len(reportableChanges))
		msg = FormatDiff(shop.Name, newSets, removedSets, reportableChanges)
	}

	if err := telegram.SendLong(ctx, sender, msg); err != nil {
		return fmt.Errorf("send report: %w", err)
	}
	// Only stamp once the send succeeds, so a failed send is retried next time.
	now := time.Now().UTC().Format(time.RFC3339)
	if err := db.MarkReported(ctx, shop.ID, now); err != nil {
		return fmt.Errorf("mark reported: %w", err)
	}
	return nil
}

// filterReportableChanges drops price changes that aren't worth alerting
// on: sets currently out of stock (not something anyone can actually buy at
// the new price) and moves under minReportablePricePct (rounding/currency
// conversion noise rather than a real price change). A change with an old
// price of 0 has no defined percentage and is always kept, matching
// FormatDiff's "n/a" handling.
func filterReportableChanges(changes []store.PriceChange) []store.PriceChange {
	var out []store.PriceChange
	for _, c := range changes {
		if c.InStock != nil && !*c.InStock {
			continue
		}
		if pct, ok := pricePctChange(c.OldCents, c.NewCents); ok && math.Abs(pct) < minReportablePricePct {
			continue
		}
		out = append(out, c)
	}
	return out
}

// failureReason reports whether run counts as failed — status "failed"
// outright, or "running" past stuckRunThreshold (a crash that never
// reached FinishRunFailed) — and the text to put in the alert.
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
