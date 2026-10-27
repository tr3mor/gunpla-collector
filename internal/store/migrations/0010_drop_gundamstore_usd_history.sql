-- Migration 10: Gundam Store is now read as a Dutch customer, which gets the
-- VAT-inclusive price in DKK instead of the VAT-free US price in USD. The
-- USD history is on the wrong basis (about 16% too low in EUR), so it is
-- dropped rather than converted: the price records in USD, then the shop's
-- runs that are left without any record.
--
-- Listings (sets) are untouched, so barcodes, product matches and near-miss
-- decisions survive. With no earlier run, the shop's next collect is a first
-- run: no mass-removal guard, and a baseline report with no new/price-change
-- alerts for every kit.
DELETE FROM price_history
WHERE currency = 'USD'
  AND set_id IN (SELECT s.id FROM sets s JOIN shops sh ON sh.id = s.shop_id WHERE sh.slug = 'gundamstore');

DELETE FROM scrape_runs
WHERE shop_id IN (SELECT id FROM shops WHERE slug = 'gundamstore')
  AND id NOT IN (SELECT run_id FROM price_history);
