# Spec Delta

## MODIFIED Requirements

### Requirement: Exchange-rate source order
For each collect run the system SHALL use today's ECB DKK to EUR reference rate from frankfurter.dev. If that cannot be fetched, it SHALL reuse the rate most recently stored. Only if no rate was ever stored SHALL it use the rate from `GUNPLA_DKK_EUR_RATE`, default 0.134. A failure to fetch SHALL be logged and SHALL NOT fail the run.

#### Scenario: Live rate available
- **WHEN** the live lookup succeeds
- **THEN** the run converts with that rate

#### Scenario: Live rate unavailable, history exists
- **WHEN** the lookup fails and an earlier run stored a DKK rate
- **THEN** the run reuses that stored rate

#### Scenario: First ever run without a live rate
- **WHEN** the lookup fails and no DKK rate was ever stored
- **THEN** the run uses the configured fallback rate

#### Scenario: Invalid fallback setting
- **WHEN** `GUNPLA_DKK_EUR_RATE` is empty, not a number or not positive
- **THEN** the default fallback of 0.134 is used

### Requirement: Implausible live rates are rejected
The system SHALL reject a live DKK to EUR rate outside a plausible range, a missing rate, a non-200 response or an undecodable response, and SHALL give up on the lookup after 5 seconds, treating each as an unavailable live rate.

#### Scenario: Out-of-range rate
- **WHEN** the rate service returns a DKK to EUR rate of 0.9 or 0.01
- **THEN** the live rate is treated as unavailable and the fallback order applies

#### Scenario: Garbled response
- **WHEN** the rate service returns a response without a EUR rate
- **THEN** the live rate is treated as unavailable and the fallback order applies

#### Scenario: Slow rate service
- **WHEN** the rate service does not answer within 5 seconds
- **THEN** collect continues with the fallback order

### Requirement: A price change is a change of the shop's own price
The system SHALL treat a listing as re-priced only when the shop's own price differs between two runs. A change in the exchange rate alone SHALL NOT be a price change.

#### Scenario: Rate drift only
- **WHEN** a DKK price is the same in two runs but the rate moved from 0.133 to 0.135
- **THEN** no price change is reported

#### Scenario: Real change shown in EUR
- **WHEN** a listing's own price changes and both runs have EUR values
- **THEN** the reported old and new prices are the stored EUR values

#### Scenario: Change without EUR values
- **WHEN** a listing's own price changes and either run lacks a EUR value
- **THEN** the reported old and new prices are the shop's own prices in its own currency

## REMOVED Requirements

### Requirement: Prices are converted to EUR once, at collect time
**Reason**: Replaced by "Prices are converted to EUR when collected", whose examples use Danish kroner instead of US dollars; the behavior is unchanged.
**Migration**: None; see the replacement requirement.

### Requirement: Older rows are filled in
**Reason**: Replaced by "Missing EUR values are filled in", whose examples use Danish kroner instead of US dollars; the behavior is unchanged.
**Migration**: None; see the replacement requirement.

## ADDED Requirements

### Requirement: Prices are converted to EUR when collected
The system SHALL convert each price to EUR when a collect run stores it, and SHALL store the shop's own price and currency, the EUR value, and the rate used. A EUR price SHALL be stored with rate 1 and an exact value. Everything that reads prices afterwards SHALL use the stored EUR value and SHALL NOT convert again.

#### Scenario: Danish kroner price is converted
- **WHEN** a run with a DKK to EUR rate of 0.134 stores a Gundam Store price of 69694 cents
- **THEN** the record holds 69694 DKK cents, a EUR value of 9339 cents, and rate 0.134

#### Scenario: Conversion rounds to the nearest cent
- **WHEN** a converted amount has a fraction of a cent
- **THEN** the stored EUR value is rounded to the nearest cent

#### Scenario: EUR price is stored exactly
- **WHEN** a EUR price is stored
- **THEN** its EUR value equals the shop price and its rate is 1

#### Scenario: Reading needs no network
- **WHEN** search, a report or a watch check reads prices
- **THEN** no exchange rate is fetched

### Requirement: Missing EUR values are filled in
At the start of each collect run the system SHALL give a EUR value to stored price records that lack one, using that run's rate for the record's currency and not the rate of their day. Records already holding a EUR value SHALL NOT be changed.

#### Scenario: Records written while the rate was missing
- **WHEN** a collect runs with a DKK rate of 0.134 and DKK records exist without EUR values
- **THEN** those records get EUR values converted at 0.134 and their shop prices are unchanged

#### Scenario: Existing EUR values are kept
- **WHEN** a record already has a EUR value and rate
- **THEN** a later run leaves them as stored

### Requirement: Gundam Store dollar history is dropped
The system SHALL remove every stored Gundam Store price record in US dollars, and the Gundam Store collect runs they belong to, when the database is upgraded. Gundam Store listings and everything else about them (barcode, matches, near-miss decisions) SHALL be kept, and so SHALL the price history and runs of the other shops. The next Gundam Store collect SHALL be treated as that shop's first run.

#### Scenario: Old dollar prices are gone
- **WHEN** the database holding Gundam Store records in USD is upgraded
- **THEN** no Gundam Store price record in USD remains, and the other shops' price records are unchanged

#### Scenario: Listings and decisions survive
- **WHEN** the database is upgraded
- **THEN** Gundam Store listings keep their barcodes, matches and near-miss decisions

#### Scenario: First collect afterwards is a baseline
- **WHEN** Gundam Store is collected after the upgrade
- **THEN** it is not refused by the mass-removal guard and its report is a baseline with no new-listing or price-change alerts
