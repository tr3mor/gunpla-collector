#!/bin/sh
set -e

# busybox crond runs each job in a minimal environment, not the one crond
# itself was started with — so docker-compose's `environment:` vars
# (GUNPLA_DB_PATH, GUNPLA_TELEGRAM_BOT_TOKEN, ...) would otherwise be
# invisible to `collect`/`report`. Persist them to a file the crontab
# entries source before running the binary.
#
# Each value is single-quoted with embedded single quotes escaped as
# '\'' (the standard POSIX shell technique), so a bot token or chat ID
# containing a "$", backtick, double quote, or space round-trips through
# the sourced file correctly instead of breaking its syntax or being
# re-expanded by the shell that sources it (the previous double-quoted
# `export KEY="VALUE"` form had exactly that problem).
: > /etc/gunpla.env
printenv | grep -E '^GUNPLA_' | while IFS= read -r line; do
	key=${line%%=*}
	value=${line#*=}
	escaped=$(printf '%s' "$value" | sed "s/'/'\\\\''/g")
	printf "export %s='%s'\n" "$key" "$escaped" >> /etc/gunpla.env
done
# Holds the Telegram bot token in plaintext.
chmod 600 /etc/gunpla.env

exec crond -f -l 2
