# configuration-and-cli Specification

## Purpose

Defines the command-line interface and configuration of `gunpla-collector`: the available commands and flags, how shops are selected, which environment variables are read and how bad values are treated, run time limits, and how the database is prepared.

## Requirements

### Requirement: Commands
The program SHALL provide the commands `collect`, `report`, `serve`, `match`, `link` and `unlink`. Running it with no command or an unknown command SHALL print the usage text to standard error and exit with a non-zero status. A failing command SHALL print `error: <message>` to standard error and exit non-zero.

#### Scenario: No command
- **WHEN** the program is started with no arguments
- **THEN** the usage text is shown and the exit status is non-zero

#### Scenario: Unknown command
- **WHEN** the program is started with `frobnicate`
- **THEN** the usage text is shown and the exit status is non-zero

#### Scenario: Command failure
- **WHEN** a command returns an error
- **THEN** `error: ` followed by the message is printed and the exit status is non-zero

### Requirement: Help
`-h` or `--help` after a command SHALL print the usage text and exit successfully without opening the database.

#### Scenario: Help
- **WHEN** `collect --help` is run
- **THEN** the usage text is printed, the exit status is zero and the database is not opened

### Requirement: Flags
The program SHALL accept the flags `--shop`, `--force` and `--addr`, each as `--name=value`, `--name value` or with a single dash, `-name`. An unknown flag SHALL be an error shown with the usage text. Unset flags SHALL have empty or false values.

#### Scenario: Equals form
- **WHEN** `collect --shop=plamodx` is run
- **THEN** only PlamoDX is collected

#### Scenario: Space form
- **WHEN** `collect --shop plamodx` is run
- **THEN** only PlamoDX is collected, and the value is not silently ignored

#### Scenario: Single dash
- **WHEN** `collect -shop=plamodx` is run
- **THEN** only PlamoDX is collected

#### Scenario: Unknown flag
- **WHEN** `collect --bogus` is run
- **THEN** it fails with an error and the usage text

### Requirement: Shop selection
`collect` and `report` SHALL choose their shops in this order: the `--shop` flag, then `GUNPLA_SHOPS`, then every active shop that has a scraper in this build. A shop named by flag or variable SHALL be registered in the database on first use. A slug with no scraper SHALL be an error. A shop in the database whose scraper has been removed from the program SHALL be ignored when no shop is named.

#### Scenario: Flag wins
- **WHEN** `GUNPLA_SHOPS` lists two shops and `--shop=zeonmarket` is given
- **THEN** only Zeonmarket is run

#### Scenario: Environment list
- **WHEN** `GUNPLA_SHOPS` is "geeksheaven, plamodx" and no flag is given
- **THEN** those two shops run, with spaces and empty entries ignored

#### Scenario: Default
- **WHEN** neither is set
- **THEN** every active shop with a scraper runs, in a stable order

#### Scenario: Unknown slug
- **WHEN** `--shop=nosuchshop` is given
- **THEN** the command fails saying no scraper is registered for that shop

#### Scenario: Retired shop
- **WHEN** the database holds an active shop with no scraper in this build and no shop is named
- **THEN** that shop is skipped

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

### Requirement: Telegram settings only for report
`report` SHALL require both the Telegram bot token and chat id and SHALL fail naming the missing one. Other commands SHALL NOT require them.

#### Scenario: Collect without Telegram settings
- **WHEN** `collect` runs with no Telegram variables
- **THEN** it does not complain about them

### Requirement: Run time limits
`collect` SHALL be limited to 60 minutes and `report` to 5 minutes, unless `GUNPLA_RUN_TIMEOUT` (a Go duration such as `45m` or `1h`) overrides both. `serve` SHALL have no time limit. Reaching the limit SHALL cancel the run.

#### Scenario: Defaults
- **WHEN** no timeout is set
- **THEN** collect has 60 minutes and report 5 minutes

#### Scenario: Override
- **WHEN** `GUNPLA_RUN_TIMEOUT` is "90m"
- **THEN** both commands get 90 minutes

### Requirement: Interruption
SIGINT and SIGTERM SHALL cancel a running command. `serve` SHALL then stop accepting requests and shut down gracefully, waiting up to 10 seconds for requests in flight.

#### Scenario: Stop the server
- **WHEN** SIGTERM is sent to `serve`
- **THEN** the server shuts down and the program exits

#### Scenario: Stop a long collect
- **WHEN** SIGINT is sent during a collect
- **THEN** waits and requests are cancelled, and progress already stored is kept

### Requirement: Serve address
`serve` SHALL listen on the `--addr` flag if given, otherwise `GUNPLA_UI_ADDR`, otherwise `:8080`.

#### Scenario: Flag overrides environment
- **WHEN** `GUNPLA_UI_ADDR` is ":9000" and `--addr=:7000` is given
- **THEN** the server listens on :7000

### Requirement: Database preparation
Every command SHALL open the SQLite database at `GUNPLA_DB_PATH`, creating it if absent, and bring its schema up to date by applying pending migrations in order, each in its own transaction, before doing anything else. Opening an up-to-date database SHALL change nothing. Foreign keys SHALL be enforced, concurrent readers and a writer SHALL not block each other, and a second process SHALL wait up to 5 seconds for a lock instead of failing at once.

#### Scenario: Fresh database
- **WHEN** a command runs against a path with no database
- **THEN** the database is created with every migration applied

#### Scenario: Older database
- **WHEN** a database lacks the newest migrations
- **THEN** only the missing ones are applied and existing data is kept

#### Scenario: Migration fails
- **WHEN** a migration fails
- **THEN** it is rolled back, the database version is not advanced, and the command fails

#### Scenario: Overlapping processes
- **WHEN** a long collect holds the database while `report` starts
- **THEN** `report` waits for the lock for up to 5 seconds before failing
