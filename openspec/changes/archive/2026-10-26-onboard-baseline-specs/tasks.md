# Tasks

## 1. Confirm the baseline is sound

- [x] 1.1 Run `go test ./...` and `go vet ./...` on `main` and verify both pass; if either fails, add the failing behavior as a caveat in the affected spec before going on
- [x] 1.2 Run `openspec validate onboard-baseline-specs --strict` and verify it reports the change as valid

## 2. Check the least certain specs against the code

- [x] 2.1 `product-grouping`: for each scenario, find the test in `internal/matcher` or `internal/catalog` that covers it; verify by listing any scenario with no covering test and either confirming it by reading the code or removing it from the spec
- [x] 2.2 `reporting`: verify the exact-5% boundary, the zero-old-price case and the pre-order case against `internal/reporter/reporter_test.go`, and fix the spec text if a test says otherwise
- [x] 2.3 `watches`: verify the 3% re-arm figures, the baseline-pass rules and the reset-on-edit rules against `internal/watchlist/watch_test.go`, and fix the spec text if a test says otherwise
- [x] 2.4 `shop-scraping` and `collection`: verify the per-shop grade, availability and pagination scenarios against the scraper tests and `internal/collector/collector_test.go`, and fix the spec text if a test says otherwise
- [x] 2.5 `web-ui` and `configuration-and-cli`: run `serve` with `GUNPLA_DB_PATH` set to a temporary file (never `gunpla.db`), fetch `/api/config` and `/api/shops`, send SIGTERM, and verify the server answers and then exits cleanly

## 3. Settle the specs

- [x] 3.1 Re-run `openspec validate onboard-baseline-specs --strict` after any spec edits from group 2 and verify it is still valid
- [x] 3.2 Confirm `git status` shows no changes outside `openspec/` and `.claude/`, and verify `gunpla.db` was not modified or recreated

## Workflow follow-up

- Archive the change with `/opsx:archive` so the nine specs land in `openspec/specs/`.
- Fix the three doc/code gaps listed in the proposal in a separate change.
- Commit with a conventional message such as `docs(openspec): onboard baseline specs`.
