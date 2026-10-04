-- Migration 6: cross-shop product grouping. A product is one physical kit;
-- each shop listing (a row in sets) points at the product it is an instance
-- of. Filled by `gunpla-collector match`, never by collect, so a matcher
-- bug can't fail a scrape run. match_method records why a listing was
-- grouped: 'ean', 'name', 'manual' (set by hand, never overwritten) or NULL.
CREATE TABLE IF NOT EXISTS products (
    id          INTEGER PRIMARY KEY,
    name        TEXT NOT NULL,
    grade       TEXT,
    created_at  TEXT NOT NULL
);
ALTER TABLE sets ADD COLUMN product_id INTEGER REFERENCES products(id);
ALTER TABLE sets ADD COLUMN match_method TEXT;
CREATE INDEX IF NOT EXISTS idx_sets_product ON sets(product_id) WHERE product_id IS NOT NULL;
