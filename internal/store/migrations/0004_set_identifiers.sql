-- Migration 4: track each set's EAN/SKU for a future cross-shop matching
-- feature. Both nullable — not every shop exposes both.
ALTER TABLE sets ADD COLUMN ean TEXT;
ALTER TABLE sets ADD COLUMN sku TEXT;
CREATE INDEX IF NOT EXISTS idx_sets_ean ON sets(ean) WHERE ean IS NOT NULL;
