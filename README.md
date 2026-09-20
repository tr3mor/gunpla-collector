# gunpla-collector

Tracks Gunpla model kit inventory and prices at online shops over time, and
sends a daily Telegram summary of what's new, removed, or changed price.

Currently supports:
- [GeeksHeaven](https://www.geeksheaven.nl/gundam-model-kits/) (MG, HG, RG,
  PG grades, prices in EUR)
- [Gundam Store](https://gundam-store.com/collections/mg-master-grade) (MG,
  HG, RG, PG grades, prices in USD)

More shops can be added by implementing the `scraper.Scraper` interface —
see `internal/scraper/geeksheaven.go` (Lightspeed eCom JSON API) or
`internal/scraper/gundamstore.go` (Shopify `products.json` API) as
templates. Both embed the shared rate-limited, size-capped JSON client in
`internal/scraper/http.go`, so a new scraper only needs its own URL
construction and response-shape structs.

## How it works

- `gunpla-collector collect [--shop=geeksheaven] [--force]` fetches the
  current catalog (name, price, stock) from each shop's category listing
  JSON API and stores a snapshot, keeping full price history. A run
  returning under half the previous run's set count is refused (treated as
  a broken scraper, not a mass removal) unless `--force` (or
  `GUNPLA_FORCE=1`) is set — use that after confirming by hand that a shop
  genuinely shrank its catalog.
- `gunpla-collector report [--shop=geeksheaven]` diffs the latest
  successful collect run against the last one it already reported on, and
  sends a Telegram message: new sets, removed sets, and price changes.
  It's idempotent — running it again before the next `collect` finds
  nothing new and sends nothing. If the most recent `collect` run failed
  (or crashed without recording a failure), it sends a warning instead,
  every time `report` runs, until a `collect` succeeds again.
- Both default to running against every active shop in the database when
  `--shop` is omitted and `GUNPLA_SHOPS` is unset — restricted to shops
  that still have a scraper registered in this binary, so retiring a shop
  from the code stops it from being collected/reported without also
  needing a DB change.
- Flags accept both `--name=value` and `--name value` (and single-dash
  `-name`), and `--help`/`-h` prints usage.

GeeksHeaven runs on Lightspeed eCom (Shoplightspeed), whose storefront
supports a `?format=json` API on every category page. Gundam Store runs on
Shopify, whose storefront exposes the same collection-page data as JSON via
`/collections/<handle>/products.json`. Both scrapers read that JSON
directly instead of parsing HTML or driving a headless browser.

## Telegram bot setup

1. In Telegram, message **@BotFather** → `/newbot` → follow the prompts →
   copy the **bot token** it gives you (looks like
   `123456789:ABCdefGhIJKlmnoPQRstuVWXyz`).
2. Message your new bot anything (e.g. "hi") so it can see you.
3. Visit `https://api.telegram.org/bot<TOKEN>/getUpdates` in a browser and
   read `message.chat.id` from the JSON response — that's your **chat ID**.
4. Put both values in `.env` (copy `.env.example`) or your environment —
   see Configuration below.

## Configuration

Environment variables:

| Variable                     | Default            | Notes                                                    |
|-------------------------------|---------------------|-----------------------------------------------------------|
| `GUNPLA_DB_PATH`               | `/data/gunpla.db`  | SQLite file path — must be on a mounted volume in Docker |
| `GUNPLA_TELEGRAM_BOT_TOKEN`    | —                   | required for `report`                                    |
| `GUNPLA_TELEGRAM_CHAT_ID`      | —                   | required for `report`                                    |
| `GUNPLA_SHOPS`                 | (all active shops) | comma-separated slugs, e.g. `geeksheaven,gundamstore`     |
| `GUNPLA_RUN_TIMEOUT`           | `30m` (collect) / `5m` (report) | whole-run timeout, Go duration string e.g. `45m` |
| `GUNPLA_FORCE`                 | unset (false)       | collect only: equivalent to `--force`, for use from `docker compose exec` |

## Running locally

```sh
go build -o gunpla-collector ./cmd/gunpla-collector
GUNPLA_DB_PATH=./gunpla.db ./gunpla-collector collect --shop=geeksheaven

GUNPLA_DB_PATH=./gunpla.db \
GUNPLA_TELEGRAM_BOT_TOKEN=... \
GUNPLA_TELEGRAM_CHAT_ID=... \
./gunpla-collector report --shop=geeksheaven
```

Inspect the database directly:

```sh
sqlite3 ./gunpla.db "select count(*) from sets where is_active=1"
sqlite3 ./gunpla.db "select name, price_cents from price_history order by id desc limit 5"
```

## Running with Docker

```sh
cp .env.example .env   # fill in your bot token + chat id
docker compose up -d --build
```

Cron runs *inside* the container (busybox `crond` as PID 1, see
`crontab` and `docker-entrypoint.sh`): `collect` at 18:00, `report` at
18:15, daily, both in Europe/Amsterdam local time (baked into the image, so
it stays correct across the CET/CEST switch). The SQLite file lives on the
`gunpla-data` named volume so it survives rebuilds.

`report` sends a "No changes today." message when there's something new to
report but nothing in it actually changed; it sends nothing at all when
there's nothing new to report (e.g. run it twice in a row). If `collect`
failed, `report` sends a warning instead — see "How it works" above.

To trigger a run manually without waiting for cron:

```sh
docker compose exec gunpla-collector gunpla-collector collect --shop=geeksheaven
docker compose exec gunpla-collector gunpla-collector report --shop=geeksheaven
```

## Testing

```sh
go test ./...
```

Covers price parsing (string and float→cents), the GeeksHeaven and Gundam
Store JSON API pagination/grade-filtering logic (against a local `httptest`
server, no network), the new/removed/price-change diff logic and Telegram
message formatting including per-shop currency symbols (synthetic data),
the sanity guard that stops a broken scrape from being interpreted as mass
removal, the report idempotency logic (unreported-run tracking, skipping
already-reported runs, alerting on a failed or stuck collect run), and —
against a real temporary SQLite file — the atomic collect-run transaction
(`store.ApplyRun`), foreign-key enforcement, and schema migrations.
