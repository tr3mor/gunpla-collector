# reporting Specification

## Purpose

Defines the `report` command: the Telegram summary of what is new, removed or re-priced at each shop since the last report, the warning when a collect failed, and the rules that make a report safe to re-run.

## Requirements

### Requirement: Report compares against the last reported run
For each shop, `report` SHALL compare the newest successful collect run that has not been reported with the newest run that has, and SHALL describe what is new, removed and re-priced between them. Collect runs that happened in between SHALL be folded into one report and all marked reported.

#### Scenario: Two collects before one report
- **WHEN** collect succeeds twice after the last report
- **THEN** one report compares the latest of them with the last reported run and both are marked reported

#### Scenario: New and removed
- **WHEN** a listing has a price record in the current run but not in the last reported run, or the reverse
- **THEN** it is reported as new, or as removed respectively

#### Scenario: Reactivated listing counts as new
- **WHEN** a listing that was missing from the last reported run is back in the current one
- **THEN** it is reported as new

### Requirement: First report is a baseline
When a shop has never been reported, `report` SHALL send a single baseline message stating how many sets are tracked, and SHALL NOT list individual sets.

#### Scenario: First ever report
- **WHEN** `report` runs for a shop with one successful collect and no earlier report
- **THEN** a message says the baseline was collected and how many sets are tracked

### Requirement: Report requires a collect
`report` SHALL fail for a shop that has no collect run at all, telling the person to run `collect` first.

#### Scenario: Nothing collected
- **WHEN** `report` runs before any collect
- **THEN** it fails with a message naming the shop and suggesting `collect`

### Requirement: Price-change filtering
The report SHALL include a price change only when the listing is not out of stock and the price moved by 5% or more. A change from a zero price SHALL always be included, with "n/a" in place of a percentage.

#### Scenario: Small move dropped
- **WHEN** a price moves by 3%
- **THEN** it is not reported

#### Scenario: Out-of-stock listing dropped
- **WHEN** an out-of-stock listing's price changes by 20%
- **THEN** it is not reported

#### Scenario: Pre-order is still reported
- **WHEN** a pre-order listing's price changes by 10%
- **THEN** it is reported

#### Scenario: Exactly five percent
- **WHEN** a price moves by exactly 5%
- **THEN** it is reported

### Requirement: Message content
A shop's report SHALL have a New section, a Pre-orders opened section for new pre-order listings, a Removed section and a Price changes section, omitting any that is empty. New items SHALL link to the shop's listing with their price. Removed items SHALL show their last seen price. A price change SHALL show the old price, the new price and the signed percentage. Prices SHALL be shown in EUR when known, and otherwise in the shop's own currency.

#### Scenario: Pre-orders separated
- **WHEN** one new listing is in stock and another is a pre-order
- **THEN** the first appears under New and the second under Pre-orders opened

#### Scenario: Price change line
- **WHEN** a kit rises from €50.00 to €55.00
- **THEN** its line shows the old price, the new price and "+10.0%"

#### Scenario: Markup in names
- **WHEN** a set name contains `<`, `>` or `&`
- **THEN** it is escaped so the message still renders

### Requirement: No changes
When a shop has a new run but nothing to report, `report` SHALL send nothing for that shop and still mark the run reported. If every selected shop was compared and none had changes, and nothing failed, it SHALL send one message saying there are no changes for all stores.

#### Scenario: All shops quiet
- **WHEN** every shop has a new run with no reportable differences
- **THEN** one "No changes for all stores" message is sent

#### Scenario: Only some shops quiet
- **WHEN** one shop has changes and another has none
- **THEN** only the shop with changes is reported and no "no changes" message is sent

### Requirement: Reports are idempotent and retried
`report` SHALL mark runs reported only after the message has been sent. Running it again before the next collect SHALL find nothing new and send nothing. If sending fails, the runs SHALL stay unreported so the next `report` sends them.

#### Scenario: Second run
- **WHEN** `report` runs twice with no collect between
- **THEN** the second run sends nothing

#### Scenario: Telegram rejects the message
- **WHEN** sending fails
- **THEN** `report` fails and the next run reports the same changes

### Requirement: Failed or stuck collect warning
If a shop's most recent collect run failed, or has been running for more than two hours, `report` SHALL send a warning naming the shop and the reason, truncated to 500 characters. It SHALL send this on every `report` until a collect succeeds, and SHALL still report any earlier successful run that has not been reported.

#### Scenario: Latest collect failed
- **WHEN** the latest run failed with an error
- **THEN** a warning with that error is sent on every report run until a collect succeeds

#### Scenario: Crashed collect
- **WHEN** the latest run is still marked running after more than two hours
- **THEN** a warning says it is likely to have crashed without recording a failure

#### Scenario: Collect still in progress
- **WHEN** the latest run is marked running for less than two hours
- **THEN** no warning is sent

### Requirement: Long messages are split
The system SHALL split a message longer than Telegram's 4096-byte limit into several messages, breaking only between lines, and SHALL split a single over-long line without cutting a character in half. A rate-limited send that includes a retry hint SHALL be retried once after that wait.

#### Scenario: Large report
- **WHEN** a report exceeds 4096 bytes
- **THEN** it is sent as several messages in order, none breaking a line

#### Scenario: Rate limited
- **WHEN** Telegram answers 429 with a retry-after
- **THEN** the send waits that long and tries once more

### Requirement: Credentials are required
`report` SHALL fail before doing anything when the Telegram bot token or chat id is not configured, naming the missing setting.

#### Scenario: Missing token
- **WHEN** `GUNPLA_TELEGRAM_BOT_TOKEN` is unset
- **THEN** `report` fails saying it is required

### Requirement: Watchlist runs after the shop reports
After all shop reports, `report` SHALL evaluate the watchlist once and send its alerts, as described by the watches capability. A failure there SHALL be reported as the command's error without undoing shop reports.

#### Scenario: Watch alert failure
- **WHEN** the watch alert message cannot be sent
- **THEN** `report` exits with an error, and shop reports already sent stay marked as reported

### Requirement: Shops are reported independently
A failure reporting one shop SHALL NOT stop the others. `report` SHALL finish all shops and then exit with the first error.

#### Scenario: One shop fails to send
- **WHEN** sending one shop's report fails
- **THEN** the remaining shops are still reported and the command exits with an error
