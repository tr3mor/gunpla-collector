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

All shops are scraped for MG, HG, RG and PG grades. Gundam Store is read as a
customer in the Netherlands, so its prices are in DKK with Dutch VAT included;
the others are in EUR.

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

Everything about products lives in `internal/catalog`: `Regroup` (run by
`match` and at the end of `collect`), `Link`/`Unlink`, and the read model
the search UI uses (`Search`, `Products`, with the cheapest listing of a
multi-shop kit flagged). `internal/matcher` is the pure grouping algorithm
it uses internally; `internal/store` only persists (`ProductTx`: load
listings, create/rename products, assign sets, prune). Product assignment —
the id-stability policy, what "manual" means, link and unlink — lives in
`internal/catalog/assign.go`, and each of Regroup/Link/Unlink runs in one
transaction. Listings their shop no longer lists (inactive) are not matched:
they keep their product but can't block a live listing from the same shop or
name the product.

A listing joins a product when it shares a barcode (EAN) with it, or when
its normalised name does: same grade, same model number (e.g. RX-78-2),
same variant words (Ver.Ka, Clear, Custom, Premium Bandai, ...), and enough
overlapping words. Two Real Grade listings that both print the RG series
number (GeeksHeaven's "Model Kit 35", ZeonMarket's "RG35") are the same kit
when the numbers agree and different when they don't. Two listings from one
shop are never merged. Near-misses
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

## Watches

Watches live in `internal/watchlist`, separate from `internal/catalog` (products). It reads active listings through the store and keeps its own listing type.

A watch (created from the ☆ in the search UI) alerts on a kit's price or
stock. It follows listings through their matcher facets, **not** product
ids, so it keeps working where grouping is imperfect. A spec holds the
grade, model number, descriptive words, the accepted versions (`verka`,
`clear`, `~base`, ...), and per-listing pins (`include`/`exclude`).

Which listings a spec follows (`watchlist.Spec`, `matcher.SameSuit`):
- grade must agree, but a listing with no grade (many pre-orders) fits any;
- if both names print a model number they must be equal; shops often print
  it on only one side, so otherwise the descriptive words decide
  (near-identical sets; trimming words in the editor makes them a minimum);
- the version words must be one of the accepted versions, so the MG
  Ver.Ka, the plain MG and a Clear edition are separate choices;
- a listing added by hand is always followed, an excluded one never.

`report` evaluates all watches once, after the per-shop reports, and sends
one message (`reporter.RunWatches`). Alert state lives in `watch_state` and
is saved only after the send succeeds, so a failed send is retried and a
standing condition is not repeated:
- *target*: a buyable listing at or under the EUR target. Alerts once, again
  only after the price left the target, or dropped a further 3%.
- *restock*: nothing was buyable last pass, something is now.
- *new listing*: a followed listing not seen before.
The first pass of a watch only records a baseline, except that a price
already under target is reported. Changing a watch's spec or target (or
re-enabling it) starts it over; renaming does not. Prices in other
currencies are compared in EUR (see Prices below); a listing with no EUR
value never triggers.

The UI saves watches through `POST/PUT/DELETE /api/watches`. Writes need
`GUNPLA_UI_TOKEN` when set; without it they are limited to same-origin JSON
requests (so another website can't drive your browser), which is not a
defence against anyone who can reach the port.

## Near misses

The "Near misses" page (`/api/review`, `catalog.NearMisses`) lists
cross-shop pairs that were not grouped but might be the same kit, so the
matcher can be corrected. It only reads; nothing changes until you act:
- **Close**: same version words, but not accepted. The reason says why:
  different words, a model-number clash, or — often — that another listing
  from the same shop is already in the group (shops sometimes list two
  products under one name).
- **Version**: the same suit where one name has extra version words
  (Premium, Revive, ...). Frequently two real kits.
Highlighted words are what differs between the names: candidates for an
alias or noise word in `internal/matcher/normalize.go`. *Link* forces the
pair into one product (like `link`), *Dismiss* hides it for good (stored in
`match_dismissals`), and *Copy details* puts both names, how the matcher
read them and the reason on the clipboard.

## serve

Search, watches and near-miss review (above). Search is a text
(case-insensitive substring), an optional regexp filter on set
name, and a shop filter. Each result shows the current price, the lowest
price ever recorded, and a link to the listing. Prices are shown in EUR
(see Prices below); the shop's own price is shown next to a converted one.

### Price history

Selecting a result opens a chart of the kit's price over time, one line per
listing (a listing that belongs to a product opens the whole product, so
shops that stopped listing it keep their line). The page draws it itself as
SVG, with no library. The data comes from `GET /api/history`, read-only like
search, with exactly one of `product=<id>` (every listing of a kit) or
`set=<id>` (one listing that is not grouped); neither or both is a 400, an
unknown id a 404.

It returns one series per listing, ordered by shop name then listing name,
each `{set_id, shop_slug, shop_name, name, url, active, entries}`. An entry
is one successful run of the listing's shop, from the listing's first record
on:

- listed: `{at, listed: true, availability, price_cents}` with the price in
  EUR (`eur_cents`), plus `original_price_cents`/`original_currency` when the
  shop's price was not in EUR. `out_of_stock` is drawn dashed; `in_stock`,
  `preorder` and an empty availability count as buyable.
- not listed: `{at, listed: false}`. A successful run of the shop that has no
  record for the listing means the shop did not list it, so the line breaks
  there until a run finds it again. A failed run is not a successful run and
  leaves no gap. `sets.removed_at` cannot be used for this: it is cleared
  when a listing returns.

A record with no EUR value (currency without a rate) gives no entry, and is
not a gap either. Entries for non-EUR shops move with the rate even when the
shop's price does not; the tooltip shows the shop's own price.

## Prices and currencies

Every price is converted to EUR once, by `collect`, and stored with the rate
used: `price_history` keeps the shop's own `price_cents`/`currency` plus
`eur_cents` and `fx_rate`. Search, "cheapest" flags, watches and reports read
`eur_cents` and never convert, so the UI and `report` need no rates and no
network.

- The rate is today's ECB reference rate from frankfurter.dev. If that
  fails, the previous run's rate is reused; `GUNPLA_DKK_EUR_RATE` is only the
  last resort for a first-ever run. Rates are stored with the data, so a bad
  rate can be found and fixed from the `fx_rate` column.
- A price change is a change of the shop's own price. Exchange-rate drift
  between two runs is not a price change, so it never shows up in reports.
  The report then shows the old and new price in EUR.
- "Lowest recorded" is the lowest EUR value ever stored, as it was when it
  was collected.
- A currency with no rate is stored without a EUR value: it is shown in its
  own currency and never compared across shops or triggers a watch.
- Rows that predate EUR storage are filled in by the next `collect`, using
  that run's rate (not the rate of their day).

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
