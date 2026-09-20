#!/bin/sh
set -e

# busybox crond runs jobs in a minimal environment, not crond's own, so
# docker-compose's env vars would otherwise be invisible to collect/report.
# Persist them to a file the crontab entries source before running.
#
# Values are single-quoted with embedded quotes escaped as '\'' (standard
# POSIX technique), so a token containing $, `, ", or a space round-trips
# correctly instead of breaking the sourced file's syntax.
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
