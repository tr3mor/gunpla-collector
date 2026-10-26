# collection Specification

## Purpose

Defines the `collect` run: fetching each shop's catalog, storing it as a snapshot with full price history, retiring listings that vanish, guarding against a broken scraper wiping the catalog, and backfilling missing barcodes.

## Requirements

### Requirement: Run record per shop
Each collect run against a shop SHALL be recorded with its start time and end with a status of success or failed. A successful run SHALL record how many kit listings it found; a failed run SHALL record the error message.

#### Scenario: Successful run
- **WHEN** a shop's catalog is fetched and stored
- **THEN** the run is marked success with the number of kit listings found

#### Scenario: Failed fetch
- **WHEN** the fetch from a shop returns an error
- **THEN** the run is marked failed with that error and the collect command reports the failure

### Requirement: Snapshot with full price history
Every successful run SHALL store, for each kit listing found, a price record holding the shop's own price and currency, the availability, the EUR value and the rate used. A record SHALL be written every run whether or not the price changed, and earlier records SHALL be kept.

#### Scenario: Unchanged price still recorded
- **WHEN** two consecutive runs both find a listing at the same price
- **THEN** the listing has a price record from each run

#### Scenario: One record per listing per run
- **WHEN** a run is applied
- **THEN** each listing has exactly one price record for that run

### Requirement: Run is applied atomically
The system SHALL apply a run's listing updates, price records, removals and success status as one all-or-nothing change.

#### Scenario: Storing fails midway
- **WHEN** an error occurs while storing a run's results
- **THEN** none of that run's listings, price records or removals are kept, and the run is marked failed

#### Scenario: Fetch error leaves the catalog untouched
- **WHEN** a shop fetch fails
- **THEN** no listing is created, updated or deactivated

### Requirement: Listing lifecycle
The system SHALL identify a listing by its shop and external id. A listing found by a run SHALL be created or updated with its latest URL, name and grade and marked active. An active listing absent from a successful run SHALL be marked inactive with a removal time. A listing that returns SHALL become active again with its removal time cleared.

#### Scenario: Renamed listing
- **WHEN** a shop changes a listing's name between runs
- **THEN** the same listing is kept with the new name

#### Scenario: Listing disappears
- **WHEN** a successful run no longer includes an active listing
- **THEN** the listing is marked inactive, keeps its history, and records when it was removed

#### Scenario: Listing returns
- **WHEN** a previously removed listing appears in a later run
- **THEN** it is active again and its removal time is cleared

### Requirement: Non-kit listings are not stored
The system SHALL drop non-kit listings before applying a run, and the run's recorded listing count SHALL exclude them.

#### Scenario: Accessories dropped
- **WHEN** a fetch returns two kits, a display base, a figure and a kit named "Type89 Base Jabber"
- **THEN** three listings are stored and the run records three listings found

### Requirement: Shrink guard
The system SHALL refuse a run that finds fewer than half as many kit listings as the shop's previous successful run, treating it as a broken scraper rather than a mass removal. A refused run SHALL be marked failed with a message mentioning `--force`, and SHALL NOT change any listing. A run with no previous successful run SHALL NOT be guarded.

#### Scenario: Catalog collapses
- **WHEN** the previous successful run found 100 listings and this run finds 10
- **THEN** the run fails with a guard message, nothing is deactivated, and the previous catalog stays active

#### Scenario: Exactly half is allowed
- **WHEN** the previous run found 100 listings and this run finds 50
- **THEN** the run is applied normally

#### Scenario: First run is not guarded
- **WHEN** a shop has never had a successful run
- **THEN** any number of listings is accepted

#### Scenario: Force overrides the guard
- **WHEN** collect runs with `--force` or `GUNPLA_FORCE=1` and the shrink is below half
- **THEN** a warning is logged and the run is applied normally, deactivating the missing listings

### Requirement: Barcodes are kept and backfilled
A barcode already stored for a listing SHALL NOT be erased by a later run whose listing omits it. For shops that support a per-product barcode lookup, the system SHALL look up the barcode of every listing that has none and was never looked up, or was last looked up more than 30 days ago, after the run is stored.

#### Scenario: Listing omits a known barcode
- **WHEN** a run's listing has no barcode but a barcode is already stored for it
- **THEN** the stored barcode is kept

#### Scenario: New listing without a barcode
- **WHEN** a Gundam Store listing has no barcode and was never looked up
- **THEN** it is looked up once, and the barcode, or the fact that none exists, is stored

#### Scenario: Recently checked listing is skipped
- **WHEN** a listing with no barcode was looked up less than 30 days ago
- **THEN** it is not looked up again

#### Scenario: Old empty result is rechecked
- **WHEN** a listing with no barcode was last looked up more than 30 days ago
- **THEN** it is looked up again

### Requirement: Barcode lookups never fail the run
Barcode lookups SHALL run after the run is stored. A failed lookup SHALL be logged and SHALL NOT fail the run. The system SHALL stop looking up after three failures in a row, SHALL save results in batches of 25, and SHALL keep what it saved when the run is cut short.

#### Scenario: Lookups fail
- **WHEN** a barcode lookup returns an error
- **THEN** the collect run still succeeds

#### Scenario: Shop is down
- **WHEN** three lookups fail in a row
- **THEN** remaining lookups for this run are skipped

#### Scenario: Run interrupted during a long backfill
- **WHEN** a run is cancelled partway through lookups
- **THEN** barcodes already looked up are saved and the next run continues with the rest

### Requirement: Shops are collected independently
When collect covers several shops, a failure in one SHALL NOT stop the others. The command SHALL finish all shops and then exit with the first error encountered. A shop with no scraper in this build SHALL be skipped with a warning.

#### Scenario: One shop fails
- **WHEN** one of four shops fails and the other three succeed
- **THEN** the three succeed and are stored, and collect exits with the failing shop's error

### Requirement: Products are regrouped after collecting
As its last step, collect SHALL regroup listings into cross-shop products so reports and the web UI see current groups. A grouping failure SHALL be logged and SHALL NOT fail collect. Grouping SHALL run even if some shops failed.

#### Scenario: Grouping fails
- **WHEN** the grouping step returns an error after listings were stored
- **THEN** the error is logged, the listings stay stored, and collect does not fail because of it

#### Scenario: Grouping is bounded
- **WHEN** grouping runs after a long collect
- **THEN** it gets its own time limit and is not starved by the collect run's budget
