#!/bin/sh
set -e

# busybox crond runs each job in a minimal environment, not the one crond
# itself was started with — so docker-compose's `environment:` vars
# (GUNPLA_DB_PATH, GUNPLA_TELEGRAM_BOT_TOKEN, ...) would otherwise be
# invisible to `collect`/`report`. Persist them to a file the crontab
# entries source before running the binary.
printenv | grep -E '^GUNPLA_' | sed -E 's/^([^=]+)=(.*)$/export \1="\2"/' > /etc/gunpla.env || true

exec crond -f -l 2
