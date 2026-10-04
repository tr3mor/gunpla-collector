# Architecture

Implementation notes that don't belong in the README.

## Scrapers

Each shop implements the `scraper.Scraper` interface. All scrapers embed the
shared rate-limited, size-capped, retrying HTTP client in
`internal/scraper/http.go`, so a new scraper only needs its own URL
construction and response-shape structs. No headless browser is needed for
any shop.

| Shop        | Platform                    | How it's read                                                  | Template file                       |
|-------------|-----------------------------|----------------------------------------------------------------|-------------------------------------|
| GeeksHeaven | Lightspeed eCom             | `?format=json` on every category page                          | `internal/scraper/geeksheaven.go`   |
| Gundam Store| Shopify                     | `/collections/<handle>/products.json`                          | `internal/scraper/gundamstore.go`   |
| PlamoDX     | WooCommerce                 | public Store REST API `/wp-json/wc/store/v1/products` (no auth) | `internal/scraper/plamodx.go`       |
| Zeonmarket  | CCV Shop                    | server-rendered HTML, `/MG?page=N` (12 per page) until an empty page, plus `/Pre-orders` | `internal/scraper/zeonmarket.go` |

All shops are scraped for MG, HG, RG and PG grades. Gundam Store prices are
in USD; the others are in EUR.

Listings that aren't model kits (Figure-Rise figures, Action Bases,
expansion/effect/weapon parts sets, 30MM, the Gundam Assemble card game,
SD/MGSD, ...) are skipped, whichever grade category a shop files them
under; see `scraper.IsKit`.

Each price record carries an availability: `in_stock`, `out_of_stock`,
`preorder`, or unknown.

### Shops evaluated and skipped

- **tf-robots.nl** — the entire site sits behind a Cloudflare managed JS
  challenge (every path but its category-only RSS feed returns a
  "Just a moment..." 403), which the plain HTTP client can't get past.

## collect

`collect` fetches the current catalog (name, price, stock) and stores a
snapshot atomically (`store.ApplyRun`), keeping full price history.

A run returning under half the previous run's set count is refused, treated
as a broken scraper rather than a mass removal. Use `--force` (or
`GUNPLA_FORCE=1`, handy from `docker compose exec`) only after confirming by
hand that a shop genuinely shrank its catalog.

`collect` runs `match` as its last step so `report` and the UI always see
fresh product groups; a matching failure is logged but never fails the
collect.

## report

`report` diffs the latest successful collect run against the last one it
already reported on. It's idempotent: running it again before the next
`collect` finds nothing new and sends nothing. When there is a new run but
nothing changed, it sends "No changes today."

If the most recent `collect` run failed (or crashed without recording a
failure), `report` sends a warning instead, every time it runs, until a
`collect` succeeds again.

Price changes are filtered before reporting:
- sets currently out of stock are dropped (their price isn't actionable);
- moves under 5% are dropped as noise (some shops show small run-to-run
  swings that look like currency-conversion rounding).

Newly opened pre-orders get their own "Pre-orders opened" section.

## match (cross-shop products)

`match` groups listings of the same kit across shops into *products*. It
only reads the database — no network.

A listing joins a product when it shares a barcode (EAN) with it, or when
its normalised name does: same grade, same model number (e.g. RX-78-2),
same variant words (Ver.Ka, Clear, Custom, Premium Bandai, ...), and enough
overlapping words. Two listings from one shop are never merged. Near-misses
are printed instead of linked; fix them with `link <a> <b>` / `unlink <id>`
(ids as shown in the `match` output). Manual choices are never overwritten.

EAN sources:
- GeeksHeaven: included in the listing API.
- Gundam Store: the listing omits them, so `collect` looks each set up via
  the per-product endpoint once (about 20 minutes the first time) and
  re-checks a set with no barcode after 30 days.
- PlamoDX and Zeonmarket: none exposed; they match by name only.

The search UI groups by product by default (untick "Group same kit across
shops" for the flat list).

## serve

Text search (case-insensitive substring), an optional regexp filter on set
name, and a shop filter. Each result shows the current price, the lowest
price ever recorded, and a link to the listing. USD prices are converted to
EUR using the live ECB rate from frankfurter.dev, fetched at startup, with
`GUNPLA_USD_EUR_RATE` as fallback.

## Shop selection

Without `--shop` and with `GUNPLA_SHOPS` unset, `collect` and `report` run
against every active shop in the database that still has a scraper
registered in this binary — so retiring a shop from the code stops it being
collected/reported without a DB change.

Flags accept `--name=value`, `--name value`, and single-dash `-name`.

## Docker deployment

Cron runs inside the `gunpla-collector` container (busybox `crond` as PID 1;
see `crontab` and `docker-entrypoint.sh`). The Europe/Amsterdam timezone is
baked into the image, so schedules stay correct across the CET/CEST switch.
The SQLite file lives on the `gunpla-data` named volume so it survives
rebuilds.

The `gunpla-ui` container is separate from the cron job so it can be
restarted independently; it reads the same SQLite file over the shared
volume.

To build from source instead of pulling the image, run
`docker compose build`, or add `build: .` back to `docker-compose.yml`.

Inspecting the database directly:

```sh
sqlite3 ./gunpla.db "select count(*) from sets where is_active=1"
sqlite3 ./gunpla.db "select name, price_cents from price_history order by id desc limit 5"
```

## Tests and CI

`go test ./...` runs without network access: scrapers are tested against a
local `httptest` server, diff/report/formatting logic against synthetic
data, and the store (atomic runs, foreign keys, migrations) against a real
temporary SQLite file. The search UI's JSON API is covered too.

CI (`.github/workflows/ci.yml`) runs `go vet`, `go test -race`, and
[golangci-lint](https://golangci-lint.run/) (config in `.golangci.yml`) on
every push and PR against `main`.
