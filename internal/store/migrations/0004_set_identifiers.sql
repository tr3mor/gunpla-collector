-- Migration 4: track each set's EAN/SKU, so a future "same kit at two
-- shops" or "cheapest across shops" feature has a canonical identity to
-- match on without re-scraping. Both are nullable — not every shop
-- exposes both (see each scraper's FetchAll for what it fills in).
ALTER TABLE sets ADD COLUMN ean TEXT;
ALTER TABLE sets ADD COLUMN sku TEXT;
CREATE INDEX IF NOT EXISTS idx_sets_ean ON sets(ean) WHERE ean IS NOT NULL;
