# Spec Delta

## MODIFIED Requirements

### Requirement: Environment variables
The program SHALL read these variables: `GUNPLA_DB_PATH` (default `/data/gunpla.db`), `GUNPLA_TELEGRAM_BOT_TOKEN`, `GUNPLA_TELEGRAM_CHAT_ID`, `GUNPLA_SHOPS`, `GUNPLA_RUN_TIMEOUT`, `GUNPLA_FORCE`, `GUNPLA_UI_ADDR` (default `:8080`), `GUNPLA_UI_TOKEN` and `GUNPLA_DKK_EUR_RATE` (default 0.134). Surrounding spaces SHALL be ignored where a value is parsed.

#### Scenario: Defaults
- **WHEN** no variable is set
- **THEN** the database path is `/data/gunpla.db` and the UI address is `:8080`

#### Scenario: Token with spaces
- **WHEN** `GUNPLA_UI_TOKEN` is " abc "
- **THEN** the token is "abc"

### Requirement: Bad values fall back instead of failing
An unparseable or non-positive `GUNPLA_RUN_TIMEOUT` SHALL mean "use the command's default", an unparseable `GUNPLA_FORCE` SHALL mean false, and an unparseable or non-positive `GUNPLA_DKK_EUR_RATE` SHALL mean the default rate. None SHALL fail the run.

#### Scenario: Typo in timeout
- **WHEN** `GUNPLA_RUN_TIMEOUT` is "soon"
- **THEN** the command's default timeout applies

#### Scenario: Boolean forms
- **WHEN** `GUNPLA_FORCE` is "1" or "true"
- **THEN** it is on, and when it is "maybe" or unset it is off
