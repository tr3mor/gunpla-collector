-- Migration 8: watches (alerts for a kit) and dismissed near-miss pairs.
--
-- A watch follows a kit across shops by its matcher facets, not by product
-- id, so it keeps working while grouping is imperfect. `spec` is the JSON
-- the catalog package reads and writes (grade, model, words, versions,
-- per-listing pins). Prices are EUR cents.
CREATE TABLE IF NOT EXISTS watches (
    id               INTEGER PRIMARY KEY,
    label            TEXT NOT NULL,
    spec             TEXT NOT NULL,
    max_price_cents  INTEGER NOT NULL DEFAULT 0,  -- 0: no price target
    on_restock       INTEGER NOT NULL DEFAULT 1,
    on_new           INTEGER NOT NULL DEFAULT 0,
    enabled          INTEGER NOT NULL DEFAULT 1,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL
);

-- What the last alert pass saw, so a standing condition isn't re-reported
-- every day. No row means the watch has not been evaluated yet; the first
-- pass records a baseline instead of alerting on everything that exists.
CREATE TABLE IF NOT EXISTS watch_state (
    watch_id          INTEGER PRIMARY KEY REFERENCES watches(id) ON DELETE CASCADE,
    last_alert_cents  INTEGER NOT NULL DEFAULT 0,  -- price at the last "target hit" alert; 0 = armed
    was_buyable       INTEGER NOT NULL DEFAULT 0,
    known_sets        TEXT NOT NULL DEFAULT '[]',  -- JSON array of set ids already seen
    evaluated_at      TEXT NOT NULL
);

-- Near-miss pairs the user reviewed and rejected. set ids, a < b.
CREATE TABLE IF NOT EXISTS match_dismissals (
    a_id        INTEGER NOT NULL,
    b_id        INTEGER NOT NULL,
    created_at  TEXT NOT NULL,
    PRIMARY KEY (a_id, b_id)
);
