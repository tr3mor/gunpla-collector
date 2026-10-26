# Design

## Context

See proposal.md for motivation. The data is already there: `price_history` holds one row per listing per run (unique on `(set_id, run_id)`), with the shop's own price, the EUR value converted once at collect time, and the availability. `scrape_runs` holds each shop's runs with a status. `sets.removed_at` is cleared when a listing returns, so it cannot say when a listing was away; only the history can.

The web layer talks to a `Catalog` interface (reads) and a `Watchlist`; `index.html` is one file with no external scripts, and the web-ui spec requires it to work without any external service. A shop is collected once a day, so a kit with five listings gains about five points a day.

## Goals / Non-Goals

**Goals:**
- One read-only endpoint that returns everything the chart needs, so the page does no joining or gap logic of its own.
- The "not listed" decision made on the server, in one place, covered by tests.

**Non-Goals:**
- Zoom, date range pickers, or downsampling. The volume is small.
- Showing history in the Watches or Near misses views.
- Storing anything new or changing collect.

## Decisions

### D1. Gaps come from successful runs, not from consecutive records
A listing is "not listed" in a successful run of its shop that has no record for it, after its first record. Alternatives: use `sets.removed_at` (cleared on return, so it loses past gaps); treat any missing interval between records as a gap (a failed or guarded run would draw false breaks). The query joins the listing's shop's successful runs to its records, which also gives the endpoint the run times to report.

### D2. The endpoint returns runs as points, with the gap runs marked
Each series carries a list of per-run entries, each either a point or "not listed", not two lists the page has to merge. The page then draws a line segment between two neighbouring points only when both are listed, and picks solid or dashed from the availability. The decision of what counts as a break stays on the server, and the chart code is just drawing.

### D3. One endpoint, two ways to name the kit
`GET /api/history?product=<id>` or `?set=<id>`, exactly one. A listing with no product (`product_id = 0` in search results) uses `set`. A listing that has a product opens the product's history, including in the ungrouped list, because the person asked for "all shops that have or had it". The history is read from the store through the catalog's read side, next to `Search` and `Products`, and the web layer stays a thin handler.

### D4. EUR values only, skip records without one
The axis is EUR, as in `lowest` logic (`MIN(eur_cents)`). A record with no EUR value (currency without a rate) gives no point, since a second axis in another currency would mislead. This matches the existing rule that such a listing "is not comparable". The skipped record does not count as "not listed", because the listing was found.

### D5. Draw the chart as hand-written SVG in `index.html`
No charting library: the spec requires no external service, and vendoring a library into a single-file page adds more than the chart is worth. Colors come from the existing theme variables so light and dark both work. Alternative: a `<canvas>`, rejected because SVG gives hover targets and accessible titles for free.

### D6. Entry point and layout: a Grafana-style panel, no prototype
The prototype was dropped at the user's request, and the first chart (per-point tooltips, dots and markers) was rejected on the real page as not Grafana-like. The panel now follows Grafana's idioms:
- The chart opens in the page's existing modal, so the Search view and its filters stay untouched underneath.
- A crosshair snaps to the nearest run and one tooltip lists every shown line at that time, cheapest first; a shop with no listing then says "not listed". The tooltip uses a 6 hour tolerance so shops collected minutes apart on one day line up.
- Lines are plain: no dot per price change and no marker where a shop stopped listing (the gap and the tooltip say it). A dot is kept only for a run that stands alone, since a segment needs two points.
- A legend of last / lowest / highest per line, where selecting an entry hides its line (the scale follows what is shown) and pointing at it emphasizes it. Plain divs are used, not a table, because the page's table styles are global.
- Range buttons (7d, 30d, 90d, All) counted back from the latest run; a price held from before the range is clipped to its left edge.
The specs only require what is observable, as listed under the history requirements of web-ui.

## Risks / Trade-offs

- [USD shops wiggle daily because EUR is converted at each run's rate while the shop price is stable] → EUR remains the axis (it is what a EUR buyer pays); the tooltip shows the shop's own price, and the prototype tries marking real shop-price changes.
- [A shop's run that finds nothing for the kit still counts as "not listed" even if the scraper was missing a page] → the collect guard already refuses runs that find under half the previous count; the chart shows what was collected.
- [Reading every record of every listing of a kit on each click] → bounded by about one row per listing per day and the existing `(set_id, scraped_at)` index; revisit only if a kit's history grows past thousands of points.
- [Two listings of one shop in a product make the legend longer] → labels add the listing name only in that case.

## Open Questions

- Modal versus expanding row, and whether the FX drift needs marking: settled by the prototype, with no effect on specs unless it changes what the chart must show.
