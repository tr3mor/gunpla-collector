-- Migration 3: PriceChanges joins price_history to itself on (set_id,
-- run_id) and assumes exactly one row per pair. Nothing enforced that
-- until now, even though nothing in the write path violates it either.
--
-- Not touched here: price_history.currency's `DEFAULT 'EUR'`. SQLite can't
-- drop a column default without a full table rebuild, and every insert
-- already passes currency explicitly, so it's dead weight, not a risk.
CREATE UNIQUE INDEX IF NOT EXISTS uq_price_history_set_run ON price_history(set_id, run_id);
