-- Migration 9: every price is also stored in EUR, converted once at collect
-- time with the rate recorded next to it. Readers compare and show eur_cents
-- and no longer convert anything; price_cents/currency stay the shop's own
-- price, which is what change detection compares.
--
-- NULL eur_cents means the currency had no rate when the row was written
-- (or it predates this migration and has not been backfilled yet; collect
-- does that). EUR rows convert exactly, so they are filled in here.
ALTER TABLE price_history ADD COLUMN eur_cents INTEGER;
ALTER TABLE price_history ADD COLUMN fx_rate REAL;
UPDATE price_history SET eur_cents = price_cents, fx_rate = 1 WHERE currency = 'EUR';
