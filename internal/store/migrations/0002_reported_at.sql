-- Migration 2: track which successful runs have been reported to Telegram
-- already, so `report` becomes idempotent. See Store.LatestUnreportedRun.
ALTER TABLE scrape_runs ADD COLUMN reported_at TEXT;
