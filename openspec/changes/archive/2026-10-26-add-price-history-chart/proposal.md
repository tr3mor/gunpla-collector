# Proposal

## Why

Every collect run stores a price record per listing and keeps the old ones, but the UI only shows the current price and the lowest ever recorded. A person deciding whether a price is good, or when a shop last dropped it, cannot see how a kit's price moved over time or how shops compare across time.

## What Changes

- Clicking a kit in the Search view, grouped or ungrouped, opens its price history as a chart.
- The chart has one line per listing, so a shop that lists the same kit twice gets two labeled lines, and shops that no longer list the kit keep their line up to the point they dropped it.
- Prices are plotted in EUR so shops are comparable; a point shows the shop's own price and currency when it differs.
- Each line shows three states: solid while the listing is buyable, dashed while the shop reports it out of stock, and broken while the shop did not list it at all. A failed run is not a gap.
- A new read-only endpoint returns the history series for one kit or one ungrouped listing.
- The chart is drawn without any external library or service, as the page must keep working offline.

## Capabilities

### New Capabilities
- `price-history`: the price history of a kit across its listings: which points a series holds, EUR values, the buyable / out-of-stock / not-listed states, and how gaps are decided.

### Modified Capabilities
- `web-ui`: Search results open a price history chart for a kit; the chart works without external services.

## Impact

- `internal/store`: a read query for price records of a set of listings, plus the successful run times needed to tell "not listed" from "no run".
- `internal/web`: a new `GET` history endpoint and the chart in `index.html`.
- No schema migration: the data already exists in `price_history` and `scrape_runs`.
- `collection` and `pricing-currency` behavior is unchanged.
