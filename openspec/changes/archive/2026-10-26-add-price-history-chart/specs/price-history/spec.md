# Spec Delta

## Purpose

Lets a person see how the price of one kit moved over time in every shop that has listed it, including shops that no longer list it, and tell apart periods when it was buyable, out of stock or not sold at all.

## ADDED Requirements

### Requirement: History per kit or per listing
The system SHALL return the price history of one kit, identified by its product, or of one listing that is not grouped into a product. The history SHALL hold one series per listing of the kit, whether or not the shop still lists it, ordered by shop name and then listing name. Each series SHALL name its shop, the listing's name and URL, and whether the listing is still active.

#### Scenario: Grouped kit
- **WHEN** the history of a product listed by three shops is requested
- **THEN** it holds three series, one per listing, each naming its shop

#### Scenario: Ungrouped listing
- **WHEN** the history of a listing with no product is requested
- **THEN** it holds one series for that listing

#### Scenario: Shop no longer lists the kit
- **WHEN** a shop removed its listing of the kit last month
- **THEN** that listing's series is still returned, marked inactive, with its past prices

#### Scenario: Two listings in one shop
- **WHEN** one shop has two listings grouped into the same product
- **THEN** the history holds two series for that shop, each with its own listing name and URL

#### Scenario: Unknown kit
- **WHEN** the requested product or listing does not exist
- **THEN** the request is answered as not found

#### Scenario: Missing or malformed identifier
- **WHEN** the request names neither a product nor a listing, names both, or gives a value that is not a number
- **THEN** the request is answered as a bad request

### Requirement: One point per run with a price
A series SHALL hold, for each successful run of the listing's shop in which the listing was found, one point with the time of the run, the availability the shop reported and the price in EUR. A point whose shop price is not in EUR SHALL also carry the shop's own price and currency. A point with no EUR value SHALL be left out.

#### Scenario: Unchanged price
- **WHEN** a listing was found at the same price in three consecutive runs
- **THEN** the series holds three points

#### Scenario: Price in another currency
- **WHEN** a Gundam Store listing was recorded at 42.00 USD and converted to 38.00 EUR
- **THEN** its point holds 38.00 EUR together with 42.00 USD

#### Scenario: No EUR value
- **WHEN** a record has no EUR value because its currency had no rate
- **THEN** that record gives no point

#### Scenario: Points in time order
- **WHEN** a series is returned
- **THEN** its points are ordered from the oldest run to the newest

### Requirement: Periods when a listing was not listed
For each successful run of the listing's shop after the listing's first record, a series SHALL show either a point or that the listing was not listed. A run that did not find the listing SHALL count as not listed, from the first such run until a later run finds it again. A run that failed, or that has not finished, SHALL NOT count, and neither SHALL a run before the listing's first record.

#### Scenario: Listing removed and relisted
- **WHEN** a listing is found in runs 1 and 2, absent from successful runs 3 and 4, and found in run 5
- **THEN** its series holds points for runs 1, 2 and 5 and shows runs 3 and 4 as not listed

#### Scenario: Listing removed for good
- **WHEN** a listing was last found in run 2 and the shop's successful runs 3 and 4 do not find it
- **THEN** its series shows that it was not listed from run 3 on

#### Scenario: Failed run in between
- **WHEN** the shop's run 3 failed between successful runs 2 and 4 that both found the listing
- **THEN** the series shows no break for run 3

#### Scenario: Run before the listing existed
- **WHEN** a shop ran a successful collect before the listing was first found
- **THEN** that run is not shown for the listing

### Requirement: Out-of-stock points are marked, not hidden
A point SHALL keep its price when the shop reports the listing out of stock, and SHALL say so. A point whose availability is in stock, pre-order or unknown SHALL count as buyable.

#### Scenario: Out of stock keeps its price
- **WHEN** a listing was recorded at 40.00 EUR with the shop reporting it out of stock
- **THEN** its point holds 40.00 EUR marked out of stock

#### Scenario: Unknown availability
- **WHEN** a shop reports no availability for a listing
- **THEN** its point is treated as buyable

#### Scenario: Pre-order
- **WHEN** a shop reports a listing as a pre-order
- **THEN** its point is treated as buyable

### Requirement: History is read-only and open
Reading a history SHALL NOT require authentication and SHALL NOT change any data.

#### Scenario: Token configured
- **WHEN** `GUNPLA_UI_TOKEN` is set and a client reads a history without it
- **THEN** the history is returned
