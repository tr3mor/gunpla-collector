# Starts the search UI (the `gunpla-ui` compose service) as a detached,
# long-running container.
#
# This is separate from run-on-login.ps1 on purpose: collect/report are
# one-shot jobs (`docker compose run --rm`) that exit as soon as they're
# done, but the UI needs to keep running so you can open it in a browser.
# `docker compose up -d` is idempotent, so this is safe to run again if
# the container is already up (e.g. from a previous login) -- it's a
# no-op in that case.
#
# Expects to live in a `windows\` subfolder next to docker-compose.yml
# and .env (repo root) -- it cd's up one level before running compose.

$ErrorActionPreference = "Continue"
Set-Location -Path (Join-Path $PSScriptRoot "..")

$logDir = Join-Path $PSScriptRoot "logs"
New-Item -ItemType Directory -Force -Path $logDir | Out-Null
$logFile = Join-Path $logDir ("ui-{0}.log" -f (Get-Date -Format "yyyy-MM-dd"))

function Log([string]$message) {
    $line = "[{0}] {1}" -f (Get-Date -Format "yyyy-MM-dd HH:mm:ss"), $message
    Add-Content -Path $logFile -Value $line
}

Log "start-ui starting"

# Docker Desktop needs time to finish starting after logon, so poll
# instead of relying on a fixed scheduler delay.
$dockerReady = $false
for ($i = 0; $i -lt 24; $i++) {
    docker info *> $null
    if ($LASTEXITCODE -eq 0) {
        $dockerReady = $true
        break
    }
    Start-Sleep -Seconds 5
}

if (-not $dockerReady) {
    Log "Docker did not become ready within 2 minutes; aborting."
    exit 1
}

Log "docker compose pull gunpla-ui"
docker compose pull gunpla-ui *>> $logFile

Log "docker compose up -d gunpla-ui"
docker compose up -d gunpla-ui *>> $logFile
Log "up exited with code $LASTEXITCODE"

Log "start-ui finished"
