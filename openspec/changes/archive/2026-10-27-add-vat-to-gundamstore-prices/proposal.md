# Proposal

## Why

Gundam Store prices shown in EUR are 16–19% lower than what an EU buyer pays at checkout. The scraper reads the store's US-market price in USD, which has no VAT, and converts it. For the Netherlands, $88.00 shows as €78.65 while the store's NL page asks 696.94 DKK (about €93.2) with 21% Dutch VAT included. The other shops list VAT-inclusive EUR prices, so Gundam Store is not comparable with them, and price-target watches fire too early.

## What Changes

- Scrape Gundam Store as a Netherlands customer (`?country=NL`). The store then returns its VAT-inclusive price in DKK, the only currency it offers there. The price is stored in DKK and converted to EUR with the ECB DKK→EUR rate. No VAT is added by us.
- **BREAKING**: replace the USD→EUR rate (and `GUNPLA_USD_EUR_RATE`) with a DKK→EUR rate (`GUNPLA_DKK_EUR_RATE`). No other shop prices in USD.
- Drop the existing Gundam Store price history (its USD rows and runs). It is on the wrong basis and is not worth converting. Listings, barcodes, matches and near-miss decisions are kept. The next collect is the shop's first run, so it is a baseline: no alert flood.

## Capabilities

### New Capabilities

### Modified Capabilities
- `shop-scraping`: Gundam Store listings are priced for the Netherlands, in DKK, VAT included.
- `pricing-currency`: conversion, rate source, bounds, backfill and change-detection examples move from USD to DKK; the old Gundam Store history is dropped.
- `configuration-and-cli`: `GUNPLA_USD_EUR_RATE` becomes `GUNPLA_DKK_EUR_RATE`.

## Impact

- Code: Gundam Store scraper, `fx` (DKK lookup and bounds), `config`, and a migration that deletes the Gundam Store price history and runs.
- Data: the local `gunpla.db` loses Gundam Store price history only. Other shops and all sets stay. The migration is irreversible, so take a copy of the DB first.
- Docs: README, `.env.example` and `docs/ARCHITECTURE.md` mention the USD rate and need updating.
- Accuracy: the shown price equals the store's NL price. Only the DKK→EUR rate differs from the store's own, and DKK is pegged to EUR, so the gap is small.
- Other specs use USD only as an example of "a non-EUR listing" (`watches`, `web-ui`, `price-history`); they stay valid and are left alone.
