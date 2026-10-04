-- Migration 7: when a per-product barcode lookup last ran for a set, for
-- shops whose listing endpoint leaves barcodes out (see
-- scraper.BarcodeLookup). NULL = never looked up. Lets a set with no
-- barcode be re-checked occasionally instead of on every run.
ALTER TABLE sets ADD COLUMN ean_checked_at TEXT;
