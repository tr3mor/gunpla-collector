# Tasks

## 1. Exchange rate

- [x] 1.1 Switch `fx` to a DKK→EUR lookup with a narrow plausibility range, and update its tests (ok, server error, missing, out of range 0.9 and 0.01, not JSON); verify `go test ./internal/fx` passes
- [x] 1.2 Replace `GUNPLA_USD_EUR_RATE` with `GUNPLA_DKK_EUR_RATE` (default 0.134, invalid falls back) in `config` and the collect wiring, with tests like `TestParseRate`; verify `go test ./internal/config ./cmd/...` passes
- [x] 1.3 Update the rate wording in README.md, `.env.example` and `docs/ARCHITECTURE.md`; verify no remaining mention of the USD rate with `grep -rni "usd" README.md .env.example docs`

## 2. Dutch market scrape

- [x] 2.1 Send the NL country on the Gundam Store catalog and barcode requests and record currency `DKK`; update `gundamstore_test.go` against the local fake server to assert the country is sent and 696.94 becomes 69694 DKK cents, and verify `go test ./internal/scraper` passes
- [x] 2.2 Verify the conversion end to end in a store test: a DKK price at rate 0.134 stores a EUR value of 9339 cents and rate 0.134, and the backfill fills DKK rows without one

## 3. Dropping the old history

- [x] 3.1 Add a migration deleting Gundam Store `price_history` rows and then that shop's `scrape_runs`, leaving sets and the other shops alone; verify a migration test on a temporary database seeded with Gundam Store and EUR-shop data checks every scenario in the spec (rows gone, other shops unchanged, sets with barcodes and decisions kept)
- [x] 3.2 Verify the next collect after the migration is a first run: a collector test on a temporary database shows the mass-removal guard does not refuse it and the report has no new-listing or price-change entries

## 4. Verification

- [x] 4.1 Run `go test ./...` and `go vet ./...`; both pass
- [x] 4.2 Run the upgrade on a copy of `gunpla.db` (never the original) and verify the other shops' counts are unchanged and no Gundam Store USD rows remain
- [x] 4.3 After the real upgrade and one collect, verify the test kit (MG 00 Qan[T] Full Saber) shows about €93 in the UI instead of €78.65
