// Package collector orchestrates a single scrape run: fetch, upsert sets,
// record price history, and deactivate sets no longer listed.
package collector

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"gunpla-collector/internal/scraper"
	"gunpla-collector/internal/store"
)

// minSetsRatio guards against a broken scraper (e.g. a changed selector)
// silently wiping out the catalog: if a run returns fewer than this
// fraction of the previous successful run's set count, it's treated as a
// failure instead of a mass removal.
const minSetsRatio = 0.5

type Store interface {
	StartRun(ctx context.Context, shopID int64, startedAt string) (int64, error)
	FinishRunFailed(ctx context.Context, runID int64, finishedAt string, errMsg string) error
	LastSuccessfulRunSetsFound(ctx context.Context, shopID int64) (int, bool, error)
	// ApplyRun persists a fetched set of results as a single atomic
	// operation: upsert sets, record price history, deactivate sets no
	// longer seen, and mark the run successful.
	ApplyRun(ctx context.Context, shopID, runID int64, sets []scraper.ScrapedSet, now string) error
}

func Run(ctx context.Context, db Store, shop store.Shop, s scraper.Scraper, logger *slog.Logger) error {
	now := func() string { return time.Now().UTC().Format(time.RFC3339) }

	startedAt := now()
	runID, err := db.StartRun(ctx, shop.ID, startedAt)
	if err != nil {
		return fmt.Errorf("start run: %w", err)
	}

	sets, fetchErr := s.FetchAll(ctx)
	if fetchErr != nil {
		_ = db.FinishRunFailed(ctx, runID, now(), fetchErr.Error())
		return fmt.Errorf("fetch %s: %w", shop.Slug, fetchErr)
	}

	prevCount, ok, err := db.LastSuccessfulRunSetsFound(ctx, shop.ID)
	if err != nil {
		_ = db.FinishRunFailed(ctx, runID, now(), err.Error())
		return fmt.Errorf("check previous run count for %s: %w", shop.Slug, err)
	}
	if ok && float64(len(sets)) < float64(prevCount)*minSetsRatio {
		msg := fmt.Sprintf("sanity guard: fetched %d sets, previous successful run had %d (below %.0f%% threshold) — refusing to treat as mass removal",
			len(sets), prevCount, minSetsRatio*100)
		_ = db.FinishRunFailed(ctx, runID, now(), msg)
		return fmt.Errorf("%s", msg)
	}

	if err := db.ApplyRun(ctx, shop.ID, runID, sets, now()); err != nil {
		_ = db.FinishRunFailed(ctx, runID, now(), err.Error())
		return fmt.Errorf("apply run for %s: %w", shop.Slug, err)
	}

	logger.Info("collect run complete", "shop", shop.Slug, "sets_found", len(sets), "run_id", runID)
	return nil
}
