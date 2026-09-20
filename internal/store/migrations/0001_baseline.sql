-- Migration 1: baseline schema. Every statement is IF NOT EXISTS, so this
-- is a safe no-op against a pre-migrations database that already has these
-- tables. See migrate.go.

CREATE TABLE IF NOT EXISTS shops (
    id          INTEGER PRIMARY KEY,
    slug        TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    base_url    TEXT NOT NULL,
    active      INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE IF NOT EXISTS sets (
    id              INTEGER PRIMARY KEY,
    shop_id         INTEGER NOT NULL REFERENCES shops(id),
    external_id     TEXT NOT NULL,
    url             TEXT NOT NULL,
    name            TEXT NOT NULL,
    grade           TEXT,
    first_seen_at   TEXT NOT NULL,
    last_seen_at    TEXT NOT NULL,
    is_active       INTEGER NOT NULL DEFAULT 1,
    removed_at      TEXT,
    UNIQUE (shop_id, external_id)
);

CREATE TABLE IF NOT EXISTS scrape_runs (
    id              INTEGER PRIMARY KEY,
    shop_id         INTEGER NOT NULL REFERENCES shops(id),
    started_at      TEXT NOT NULL,
    finished_at     TEXT,
    status          TEXT NOT NULL,
    sets_found      INTEGER,
    error           TEXT
);

CREATE TABLE IF NOT EXISTS price_history (
    id          INTEGER PRIMARY KEY,
    set_id      INTEGER NOT NULL REFERENCES sets(id),
    run_id      INTEGER NOT NULL REFERENCES scrape_runs(id),
    price_cents INTEGER NOT NULL,
    currency    TEXT NOT NULL DEFAULT 'EUR',
    in_stock    INTEGER,
    scraped_at  TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_price_history_set ON price_history(set_id, scraped_at);
CREATE INDEX IF NOT EXISTS idx_price_history_run ON price_history(run_id);
CREATE INDEX IF NOT EXISTS idx_sets_shop_active ON sets(shop_id, is_active);
