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

## Checking it worked

Logs land in `windows\logs\run-YYYY-MM-DD.log` (one file per day, appended
to on every login that triggers a run). You can also check Task
Scheduler's own history for the `gunpla-collector` task, or just wait for
the Telegram message.

To remove the task later: `Unregister-ScheduledTask -TaskName "gunpla-collector"`.
