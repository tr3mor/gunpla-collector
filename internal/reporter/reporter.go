// Package reporter diffs the two most recent successful scrape runs for a
// shop and sends a Telegram summary.
package reporter

import (
	"context"
	"fmt"
	"log/slog"

	"gunpla-collector/internal/store"
	"gunpla-collector/internal/telegram"
)

type Store interface {
	TwoMostRecentSuccessfulRuns(ctx context.Context, shopID int64) (current *store.Run, previous *store.Run, err error)
	NewSets(ctx context.Context, shopID, currentRunID, previousRunID int64) ([]store.ReportItem, error)
	RemovedSets(ctx context.Context, shopID, currentRunID, previousRunID int64) ([]store.ReportItem, error)
	PriceChanges(ctx context.Context, shopID, currentRunID, previousRunID int64) ([]store.PriceChange, error)
}

func Run(ctx context.Context, db Store, shop store.Shop, sender telegram.Sender, logger *slog.Logger) error {
	current, previous, err := db.TwoMostRecentSuccessfulRuns(ctx, shop.ID)
	if err != nil {
		return fmt.Errorf("find recent runs: %w", err)
	}
	if current == nil {
		return fmt.Errorf("no successful collect run yet for %s — run `collect` first", shop.Slug)
	}

	if previous == nil {
		setsFound := 0
		if current.SetsFound.Valid {
			setsFound = int(current.SetsFound.Int64)
		}
		logger.Info("reporting baseline", "shop", shop.Slug, "sets_found", setsFound)
		return telegram.SendLong(ctx, sender, FormatBaseline(shop.Name, setsFound))
	}

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
	return telegram.SendLong(ctx, sender, FormatDiff(shop.Name, newSets, removedSets, changes))
}
