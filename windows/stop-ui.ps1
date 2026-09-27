# Stops the search UI container started by start-ui.ps1.
#
# Expects to live in a `windows\` subfolder next to docker-compose.yml
# (repo root) -- it cd's up one level before running compose.

Set-Location -Path (Join-Path $PSScriptRoot "..")
docker compose stop gunpla-ui
