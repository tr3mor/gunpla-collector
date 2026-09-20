-- Migration 2: track which successful runs have already been reported to
-- Telegram, so `report` becomes idempotent — running it twice, or after a
-- `collect` failure, no longer re-sends or silently drops a diff. See
-- Store.LatestUnreportedRuns.
ALTER TABLE scrape_runs ADD COLUMN reported_at TEXT;
