# Design

## Context

EUR is derived once, at collect time (`pricing-currency`), stored as `eur_cents` with `fx_rate`; readers never convert. Gundam Store is a Shopify shop whose base currency is DKK. It localizes by country: a plain request gets the US market (USD, no VAT), while `?country=NL` gets the Dutch market (DKK, 21% VAT included). Checked live: the collection feed `products.json` and the single-product `.json` both honor the parameter and returned 696.94 DKK for the test kit. The store offers no EUR display. See proposal.md for the motivation.

"New" and "price change" are worked out by comparing a listing's `price_history` rows in the current and previous run; a reported run with no previous run is a baseline (`reporting`), and the mass-removal guard only applies when a previous successful run exists (`collection`).

## Goals / Non-Goals

**Goals:**
- A Gundam Store price in the UI equals what the store charges a Dutch buyer, converted at the ECB rate.

**Non-Goals:**
- Other destination countries or a country setting. The country is a constant in the scraper; making it a setting is easy later.
- Shipping costs.
- Keeping or converting the old USD history.

## Decisions

**Scrape the Dutch market, convert DKK→EUR.** The store's own price is the most accurate source and needs no VAT table or markup guess on our side. Alternatives rejected: adding 21% VAT to the US price (still ~2% off because of the store's USD markup); converting history and keeping USD (needs rules for mixed currencies in change detection and the lowest price).

**Replace the USD rate with a DKK rate in `fx`, not add one.** No other shop prices in USD, so the USD lookup, setting and fallback go away. Same source (frankfurter.dev, ECB), same fallback order. The plausibility bounds change because DKK→EUR is about 0.134 (the krone is pegged to the euro); a narrow range around it is enough to catch a garbled response.

**Drop the history in an SQL migration.** It needs no config, runs once per database through the existing migration mechanism, and deletes in order: Gundam Store `price_history` rows, then that shop's `scrape_runs`. Nothing else references runs. Sets are not touched, so barcodes, matches and near-miss decisions survive. With no previous run, the next collect is unguarded and its report is a baseline, so there is no alert flood and no spurious "re-priced" for every kit.

**The barcode lookup uses the same market.** Both Gundam Store requests carry the country, so the catalog and the single-product reads agree.

## Risks / Trade-offs

- [Irreversible delete in the local DB] → The migration only touches Gundam Store price history and runs. Test it on a temporary database, and take a copy of `gunpla.db` before upgrading. The DB itself is never recreated.
- [The store changes how it localizes] → The existing layout-change guards still apply; a missing or garbled price fails the run loudly. A spec scenario pins the expected NL price against a fake server.
- [Between the upgrade and the next collect the shop has no prices] → Search and the UI show those listings without a price until the next collect, about one run. Acceptable.
- [Pre-orders or items hidden from the Dutch market] → If the NL market omits a kit, it drops out of the catalog like any removed listing; the sanity guard does not apply to the first run, so check the listing count after it.
