# Runs gunpla-collector's collect + report once via Docker Compose.
# Meant to be triggered by Windows Task Scheduler on user logon (shop
# catalogs don't change daily, so login-triggered is frequent enough) --
# see windows/README.md for how to register that trigger.
#
# Expects to live in a `windows\` subfolder next to docker-compose.yml
# and .env (repo root) -- it cd's up one level before running compose.

$ErrorActionPreference = "Stop"
Set-Location -Path (Join-Path $PSScriptRoot "..")

$logDir = Join-Path $PSScriptRoot "logs"
New-Item -ItemType Directory -Force -Path $logDir | Out-Null
$logFile = Join-Path $logDir ("run-{0}.log" -f (Get-Date -Format "yyyy-MM-dd"))

function Log([string]$message) {
    $line = "[{0}] {1}" -f (Get-Date -Format "yyyy-MM-dd HH:mm:ss"), $message
    Add-Content -Path $logFile -Value $line
}

Log "run starting"

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

Log "docker compose pull"
docker compose pull *>> $logFile

# --entrypoint overrides the image's crond-launching entrypoint so the
# binary runs directly and exits, instead of starting the daemon.
Log "collect"
docker compose run --rm --entrypoint gunpla-collector gunpla-collector collect *>> $logFile
Log "collect exited with code $LASTEXITCODE"

Log "report"
docker compose run --rm --entrypoint gunpla-collector gunpla-collector report *>> $logFile
Log "report exited with code $LASTEXITCODE"

Log "run finished"
