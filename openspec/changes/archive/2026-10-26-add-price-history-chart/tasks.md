# Tasks

## 1. Prototype

- [x] 1.1 Prototype dropped at the user's request; the modal, FX-drift and single-point choices are written into design.md D6 as defaults to judge on the real page
- [x] 1.2 No prototype review, so no change to the specs; verify `openspec validate add-price-history-chart --strict` passes

## 2. History data

- [x] 2.1 Add a store read that returns the records of a set of listings together with the successful runs of their shops, using a temporary database; verify with a store test covering a record per run, a skipped record without EUR value, and an inactive listing
- [x] 2.2 Build per-run entries (point or not listed) in the catalog read side and verify with tests for: removed and relisted, removed for good, failed run in between, run before the first record, two listings in one shop, out-of-stock and unknown availability points
- [x] 2.3 Resolve a product id or a listing id into its listings, with not-found and bad-request outcomes, and verify with tests for a grouped kit, an ungrouped listing, an unknown id, no identifier, both identifiers and a non-number

## 3. Endpoint

- [x] 3.1 Add `GET /api/history` returning series ordered by shop name then listing name, with the shop's own price and currency on non-EUR points; verify with web tests against a temporary database, including a read with `GUNPLA_UI_TOKEN` set and no token
- [x] 3.2 Document the endpoint and the "not listed" rule in docs/ARCHITECTURE.md and mention the history view in README.md; verify the documented request returns the documented shape

## 4. Chart in the page

- [x] 4.1 Open the history from a grouped kit and from a listing in the ungrouped list (whole product when there is one), without reacting to the shop link or the watch button, and close it back to the Search view with filters kept; verify by driving the page against a temporary database
- [x] 4.2 Draw the SVG chart: one line per listing, solid, dashed and broken segments, a marker for a one-run segment, labels with the listing name when a shop has two lines, a "no longer listed" mark, and the hover details with the shop's own price; verify visually in light and dark themes
- [x] 4.3 Show the empty and error messages; verify with an empty history and a forced failing request
- [x] 4.4 Verify the page makes no request outside the server (no external script, style or font) with the network blocked

## 5. Integration

- [x] 5.1 Run `go test ./...` and `go vet ./...` and verify both pass
- [x] 5.2 Run `serve` against a temporary copy of the data (not the developer's gunpla.db) and walk through a grouped kit, an ungrouped listing and a delisted shop; verify against the web-ui and price-history scenarios

## 6. Grafana-style chart

- [x] 6.1 Rework the chart into a panel with a crosshair and one shared tooltip listing every shown line at the nearest run (cheapest first, "not listed" for a missing shop, shop price beside EUR), dropping the per-change dots and ✕ markers; verify in the browser against the demo data in both themes
- [x] 6.2 Add the legend with last, lowest and highest per line, click to hide or show a line (scale follows the shown lines) and hover to emphasize one; verify in the browser including hiding every line
- [x] 6.3 Add the 7d / 30d / 90d / All buttons with the held-price rule and legend figures following the range; verify in the browser against a history longer and shorter than the range
- [x] 6.4 Re-run `go test ./...`, `go vet ./...` and `openspec validate add-price-history-chart --strict`, and verify the page still makes no request outside the server
