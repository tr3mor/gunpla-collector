# gunpla-collector

Tracks Gunpla model kit inventory and prices at online shops over time,
sends a daily Telegram summary of what's new, removed, or changed price,
and serves a small web UI for searching the current catalog.

Supported shops:
- [GeeksHeaven](https://www.geeksheaven.nl/gundam-model-kits/)
- [Gundam Store](https://gundam-store.com/collections/mg-master-grade)
- [PlamoDX](https://plamodx.nl/product-category/gunpla/)
- [Zeonmarket](https://www.zeonmarket.nl/MG) (including pre-orders)

## Commands

| Command | What it does |
|---------|--------------|
| `collect [--shop=<slug>] [--force]` | Fetch each shop's current catalog and store a price snapshot. Refuses a run that drops more than half the catalog unless `--force` is given. |
| `report [--shop=<slug>]` | Send a Telegram message with new, removed, and re-priced sets (plus newly opened pre-orders) since the last report. Warns if the latest `collect` failed. |
| `serve [--addr=:8080]` | Serve a read-only search UI over the database. No authentication — trusted networks only. |
| `match` | Group the same kit across shops into products. Runs automatically at the end of `collect`. |
| `link <a> <b>` / `unlink <id>` | Manually fix a wrong or missed grouping. |

Without `--shop`, commands run against every active shop. Run any command
with `--help` for details.

## Telegram bot setup

1. Message **@BotFather** → `/newbot` and copy the **bot token**.
2. Send your new bot any message.
3. Open `https://api.telegram.org/bot<TOKEN>/getUpdates` and read
   `message.chat.id` — that's your **chat ID**.
4. Put both in `.env` (copy `.env.example`).

## Configuration

| Variable                    | Default                         | Notes                                          |
|-----------------------------|---------------------------------|------------------------------------------------|
| `GUNPLA_DB_PATH`            | `/data/gunpla.db`               | SQLite file path                               |
| `GUNPLA_TELEGRAM_BOT_TOKEN` | —                               | required for `report`                          |
| `GUNPLA_TELEGRAM_CHAT_ID`   | —                               | required for `report`                          |
| `GUNPLA_SHOPS`              | all active shops                | comma-separated slugs, e.g. `geeksheaven,plamodx` |
| `GUNPLA_RUN_TIMEOUT`        | `60m` (collect) / `5m` (report) | Go duration string                             |
| `GUNPLA_FORCE`              | unset                           | same as `collect --force`                      |
| `GUNPLA_UI_ADDR`            | `:8080`                         | `serve` listen address                         |
| `GUNPLA_USD_EUR_RATE`       | `0.89`                          | fallback rate if the live ECB rate is unreachable |

## Running with Docker

```sh
cp .env.example .env   # fill in your bot token + chat id
docker compose up -d
```

This runs `collect` at 18:00 and `report` at 18:15 daily (Europe/Amsterdam)
via cron inside the container, and serves the search UI at
`http://localhost:8080` (`GUNPLA_UI_PORT` to change). Set
`GUNPLA_IMAGE_TAG` to pin a [release](https://github.com/tr3mor/gunpla-collector/releases).

To run manually:

```sh
docker compose exec gunpla-collector gunpla-collector collect
docker compose exec gunpla-collector gunpla-collector report
```

For a machine that isn't on 24/7, see [`windows/README.md`](windows/README.md).

## Running locally

```sh
go build -o gunpla-collector ./cmd/gunpla-collector
export GUNPLA_DB_PATH=./gunpla.db
./gunpla-collector collect
./gunpla-collector serve   # http://localhost:8080
```

## Development

```sh
go test ./...
```

To add a shop, implement the `scraper.Scraper` interface — the existing
scrapers in `internal/scraper/` serve as templates for JSON-API and
HTML-scraping shops. See [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)
for how collecting, reporting, and cross-shop matching work.
