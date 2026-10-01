-- Migration 5: replace price_history.in_stock (nullable bool) with
-- availability ('in_stock' | 'out_of_stock' | 'preorder', NULL = unknown),
-- so pre-orders can be told apart from regular stock.
ALTER TABLE price_history ADD COLUMN availability TEXT;
UPDATE price_history SET availability = CASE in_stock WHEN 1 THEN 'in_stock' WHEN 0 THEN 'out_of_stock' END
    WHERE in_stock IS NOT NULL;
ALTER TABLE price_history DROP COLUMN in_stock;
