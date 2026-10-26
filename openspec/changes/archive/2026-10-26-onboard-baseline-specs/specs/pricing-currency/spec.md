# Spec Delta

## Purpose

Defines how prices in different currencies become comparable: every price is converted to EUR once, when a collect run stores it, together with the rate used. It also defines what counts as a price change and as the lowest recorded price.

## ADDED Requirements

### Requirement: Prices are converted to EUR once, at collect time
The system SHALL convert each price to EUR when a collect run stores it, and SHALL store the shop's own price and currency, the EUR value, and the rate used. A EUR price SHALL be stored with rate 1 and an exact value. Everything that reads prices afterwards SHALL use the stored EUR value and SHALL NOT convert again.

#### Scenario: US dollar price is converted
- **WHEN** a run with a USD to EUR rate of 0.89 stores a Gundam Store price of 9200 cents
- **THEN** the record holds 9200 USD cents, a EUR value of 8188 cents, and rate 0.89

#### Scenario: Conversion rounds to the nearest cent
- **WHEN** a converted amount has a fraction of a cent
- **THEN** the stored EUR value is rounded to the nearest cent

#### Scenario: EUR price is stored exactly
- **WHEN** a EUR price is stored
- **THEN** its EUR value equals the shop price and its rate is 1

#### Scenario: Reading needs no network
- **WHEN** search, a report or a watch check reads prices
- **THEN** no exchange rate is fetched

### Requirement: Exchange-rate source order
For each collect run the system SHALL use today's ECB USD to EUR reference rate from frankfurter.dev. If that cannot be fetched, it SHALL reuse the rate most recently stored. Only if no rate was ever stored SHALL it use the rate from `GUNPLA_USD_EUR_RATE`, default 0.89. A failure to fetch SHALL be logged and SHALL NOT fail the run.

#### Scenario: Live rate available
- **WHEN** the live lookup succeeds
- **THEN** the run converts with that rate

#### Scenario: Live rate unavailable, history exists
- **WHEN** the lookup fails and an earlier run stored a USD rate
- **THEN** the run reuses that stored rate

#### Scenario: First ever run without a live rate
- **WHEN** the lookup fails and no USD rate was ever stored
- **THEN** the run uses the configured fallback rate

#### Scenario: Invalid fallback setting
- **WHEN** `GUNPLA_USD_EUR_RATE` is empty, not a number or not positive
- **THEN** the default fallback of 0.89 is used

### Requirement: Implausible live rates are rejected
The system SHALL reject a live USD to EUR rate below 0.3 or above 3, a missing rate, a non-200 response or an undecodable response, and SHALL give up on the lookup after 5 seconds, treating each as an unavailable live rate.

#### Scenario: Garbled response
- **WHEN** the rate service returns a response without a EUR rate
- **THEN** the live rate is treated as unavailable and the fallback order applies

#### Scenario: Slow rate service
- **WHEN** the rate service does not answer within 5 seconds
- **THEN** collect continues with the fallback order

### Requirement: Currencies without a rate
A price in a currency with no rate SHALL be stored in its own currency without a EUR value. It SHALL be displayed in its own currency, and it SHALL NOT be compared across shops, be flagged cheapest, or trigger a watch.

#### Scenario: Unknown currency
- **WHEN** a listing is priced in a currency for which no rate exists
- **THEN** its record has no EUR value or rate

### Requirement: Older rows are filled in
At the start of each collect run the system SHALL give a EUR value to stored price records that lack one, using that run's rate for the record's currency and not the rate of their day. Records already holding a EUR value SHALL NOT be changed.

#### Scenario: Records from before EUR storage
- **WHEN** a collect runs with a USD rate of 0.89 and USD records exist without EUR values
- **THEN** those records get EUR values converted at 0.89 and their shop prices are unchanged

#### Scenario: Existing EUR values are kept
- **WHEN** a record already has a EUR value and rate
- **THEN** a later run leaves them as stored

### Requirement: A price change is a change of the shop's own price
The system SHALL treat a listing as re-priced only when the shop's own price differs between two runs. A change in the exchange rate alone SHALL NOT be a price change.

#### Scenario: Rate drift only
- **WHEN** a USD price is the same in two runs but the rate moved from 0.88 to 0.90
- **THEN** no price change is reported

#### Scenario: Real change shown in EUR
- **WHEN** a listing's own price changes and both runs have EUR values
- **THEN** the reported old and new prices are the stored EUR values

#### Scenario: Change without EUR values
- **WHEN** a listing's own price changes and either run lacks a EUR value
- **THEN** the reported old and new prices are the shop's own prices in its own currency

### Requirement: Lowest recorded price
The system SHALL report, for each listing, the lowest price ever recorded, both in the shop's currency and in EUR. The EUR figure SHALL be the lowest stored EUR value as it was when collected, and SHALL ignore records without a EUR value.

#### Scenario: Lowest taken from history
- **WHEN** a listing was recorded at 5000, 4200 and 4600 cents over three runs
- **THEN** its lowest recorded price is 4200 cents

#### Scenario: Not revalued
- **WHEN** the exchange rate changes after a low price was stored
- **THEN** the lowest recorded EUR value stays as stored
