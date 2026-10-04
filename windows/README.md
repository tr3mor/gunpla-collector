# Running on a Windows PC (trigger: login)

For a machine that isn't on 24/7, running on every login is simpler than a
fixed daily schedule — shop catalogs don't change often enough for the
timing to matter, and this way nothing runs while the PC is off.

## Setup

1. Install [Docker Desktop](https://www.docker.com/products/docker-desktop/)
   and, in its settings, enable **Start Docker Desktop when you log in**.
2. Clone this repo (or just copy `docker-compose.yml`, `.env.example`, and
   the `windows/` folder) to somewhere like `C:\gunpla-collector\`.
3. `cd C:\gunpla-collector`, then `copy .env.example .env` and fill in your
   Telegram bot token + chat id (see the main README's "Telegram bot
   setup").
4. Register the login trigger — open PowerShell and run:

   ```powershell
   $action = New-ScheduledTaskAction -Execute "powershell.exe" `
       -Argument '-NoProfile -ExecutionPolicy Bypass -File "C:\gunpla-collector\windows\run-on-login.ps1"'
   $trigger = New-ScheduledTaskTrigger -AtLogOn -User "$env:USERDOMAIN\$env:USERNAME"
   $settings = New-ScheduledTaskSettingsSet -MultipleInstances IgnoreNew -StartWhenAvailable
   Register-ScheduledTask -TaskName "gunpla-collector" -Action $action -Trigger $trigger `
       -Settings $settings -Description "Runs gunpla-collector collect+report on login"
   ```

   Adjust the path in `-Argument` if you cloned somewhere other than
   `C:\gunpla-collector`. `-MultipleInstances IgnoreNew` skips a run if one
   from a previous login is somehow still going, instead of overlapping.

That's it — no fixed delay is needed before the task fires, since
`run-on-login.ps1` polls `docker info` for up to 2 minutes and waits for
Docker Desktop to actually be ready before doing anything.

## First run takes longer

The first `collect` looks up a barcode for every Gundam Store product (one
request each, about 20 minutes), so the whole first run takes ~25 minutes
instead of a few. Leave the PC on until it finishes: a collect cut short
saves nothing for that shop, and the next login starts the lookups over.
Later runs only look up new products and are back to a few minutes.

## Checking it worked

Logs land in `windows\logs\run-YYYY-MM-DD.log` (one file per day, appended
to on every login that triggers a run). You can also check Task
Scheduler's own history for the `gunpla-collector` task, or just wait for
the Telegram message.

To remove the task later: `Unregister-ScheduledTask -TaskName "gunpla-collector"`.

## Search UI

`run-on-login.ps1` only handles `collect`/`report` — one-shot jobs that
exit as soon as they're done (`docker compose run --rm`). The search UI
(`gunpla-ui`, see the main README) needs to keep running instead, so it's
started separately with `windows\start-ui.ps1`, which brings up just that
one container in detached mode (`docker compose up -d gunpla-ui`) and is
safe to run again if it's already up.

Run it manually whenever you want the UI available:

```powershell
cd C:\gunpla-collector
.\windows\start-ui.ps1
```

Then open `http://localhost:8080` (or whatever `GUNPLA_UI_PORT` is set to
in `.env`). To stop it: `.\windows\stop-ui.ps1`.

To have it come back automatically after a reboot, register a second
login task the same way as above, pointing at `start-ui.ps1`:

```powershell
$action = New-ScheduledTaskAction -Execute "powershell.exe" `
    -Argument '-NoProfile -ExecutionPolicy Bypass -File "C:\gunpla-collector\windows\start-ui.ps1"'
$trigger = New-ScheduledTaskTrigger -AtLogOn -User "$env:USERDOMAIN\$env:USERNAME"
$settings = New-ScheduledTaskSettingsSet -MultipleInstances IgnoreNew -StartWhenAvailable
Register-ScheduledTask -TaskName "gunpla-ui" -Action $action -Trigger $trigger `
    -Settings $settings -Description "Starts the gunpla-collector search UI on login"
```

Since the UI container has no authentication, this is meant for a
trusted home network — don't port-forward `GUNPLA_UI_PORT` to the
internet.
