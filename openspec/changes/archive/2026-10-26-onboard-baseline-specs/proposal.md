# Proposal

## Why

The repo uses OpenSpec but has no specs, so the observable behavior of the collector lives only in prose (`docs/ARCHITECTURE.md`, `README.md`) and in test names. Capturing it as specs gives later changes a baseline to modify instead of a document to re-read, and makes drift between docs and code visible.

## What Changes

- Add baseline specs for the behavior the product has today, written from the code and tests rather than copied from the docs.
- No code, schema, or configuration changes. Where the architecture doc and the code disagree, the spec follows the code and the disagreement is listed under Impact. The gaps themselves are not fixed here.
- Deployment concerns (Docker, cron, Windows scripts, CI) are out of scope.

## Capabilities

### New Capabilities
- `shop-scraping`: reading each supported shop's catalog, filtering out non-kit listings, and recording grade and availability.
- `collection`: the `collect` run — atomic snapshots with price history, the shrink guard and `--force`, and the match step at the end.
- `pricing-currency`: converting prices to EUR once at collect time, the exchange-rate fallback chain, and how price changes and "lowest recorded" are defined.
- `product-grouping`: grouping the same kit across shops into products, id stability, and manual `link` / `unlink`.
- `near-miss-review`: listing cross-shop pairs that were not grouped but might be the same kit, and the link / dismiss actions.
- `watches`: watching a kit by price target, restock, or new listing, and when alerts fire and re-arm.
- `reporting`: the daily Telegram summary of new, removed, and re-priced sets, and the failed-collect warning.
- `web-ui`: search, filters, grouping, and the guard on changes made through the UI.
- `configuration-and-cli`: commands, flags, environment variables, and shop selection.

### Modified Capabilities

None. There are no existing specs.

## Impact

- New files under `openspec/changes/onboard-baseline-specs/specs/`; after archive, `openspec/specs/` gains nine capabilities.
- No runtime, API, dependency, or database impact.
- Doc/code gaps found while writing the specs, left for a separate change:
  - `docs/ARCHITECTURE.md` says an all-quiet report sends "No changes today."; the code sends "No changes for all stores." (`reporting` follows the code).
  - The `internal/web` package comment says the package is read-only, but it saves watches and near-miss decisions (`web-ui` follows the code).
  - The `ParseEuroPriceString` TODO says it has no production caller, but the Zeonmarket scraper uses it.
