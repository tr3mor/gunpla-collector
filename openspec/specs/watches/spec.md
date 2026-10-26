# watches Specification

## Purpose

Lets a person watch a kit and be alerted when a buyable listing reaches a target price, comes back in stock, or when a new listing appears, without repeating an alert for a condition that merely continues.

## Requirements

### Requirement: A watch follows listings by what they are, not by product
A watch SHALL follow listings through a spec made of a grade, a model number, descriptive words, accepted versions, and per-listing include and exclude pins, and SHALL NOT depend on how listings are grouped into products. A listing with no grade SHALL be compatible with any grade.

#### Scenario: Grouping is imperfect
- **WHEN** two shops' listings of one kit were not grouped into one product
- **THEN** a watch on that kit still follows both

#### Scenario: Pre-order without a grade
- **WHEN** a watch is for grade MG and a pre-order listing has no grade
- **THEN** the pre-order can still be followed

### Requirement: Which listings a spec follows
A listing SHALL be followed when its grade agrees, its suit matches, and its version is accepted. Model numbers SHALL be compared only when both sides print one; otherwise the descriptive words SHALL decide, requiring near-identical words, or, once the person has trimmed words, requiring that every spec word appears in the listing. A listing added by hand SHALL always be followed and an excluded listing never.

#### Scenario: Model on only one side
- **WHEN** the spec has a model number and a listing's name prints none
- **THEN** the listing is followed if its descriptive words are near-identical to the spec's

#### Scenario: Different model numbers
- **WHEN** both the spec and a listing print model numbers that differ
- **THEN** the listing is not followed

#### Scenario: Trimmed words
- **WHEN** a person removes words from the spec so only some remain
- **THEN** a listing is followed if it contains all the remaining words

#### Scenario: Pinned and excluded listings
- **WHEN** a listing is pinned by hand and another matching listing is excluded
- **THEN** the pinned one is followed even if its name does not match, and the excluded one is not followed

### Requirement: Versions are chosen separately
The system SHALL split the watched suit into versions (Ver.Ka, Clear, Premium, and so on, with a plain "base release"), and a spec SHALL accept the versions it lists, or every version when it lists none. It SHALL report accepted versions that no listing has any more.

#### Scenario: Plain release versus Ver.Ka
- **WHEN** a spec accepts only the base release
- **THEN** listings of the same suit with Ver.Ka in the name are not followed

#### Scenario: Version disappears
- **WHEN** a spec accepts a version that no active listing has
- **THEN** that version is reported as missing

### Requirement: Watching a kit from a listing
Starting a watch from a listing SHALL propose a spec with that listing's grade, model number, words and version, labelled with its product name or, if none, its listing name. When those facets could not even match the listing itself, the listing SHALL be pinned.

#### Scenario: Name with nothing distinctive
- **WHEN** a listing's name has no model number and no distinctive words
- **THEN** the proposed spec pins that listing so the watch still works

#### Scenario: Unknown listing
- **WHEN** the listing does not exist
- **THEN** no spec is proposed and the request reports not found

### Requirement: Best offer
A watch SHALL identify its best offer as the cheapest listing that can be bought now, compared in EUR, among those it follows and has not excluded. Pre-orders SHALL count as buyable. Out-of-stock listings and listings with no EUR value SHALL NOT be buyable. Tied listings SHALL all be best.

#### Scenario: Cheapest buyable
- **WHEN** a watch follows a €40 out-of-stock listing and a €45 in-stock one
- **THEN** the €45 listing is the best offer

#### Scenario: Foreign currency compared in EUR
- **WHEN** a Gundam Store listing is priced in USD
- **THEN** its EUR value is used to compare it with EUR listings

#### Scenario: Unpriced listing
- **WHEN** a listing's currency has no rate
- **THEN** it is never the best offer and never triggers an alert

### Requirement: Alert types and required settings
A watch SHALL alert on a target price (a buyable listing at or under a EUR target), on restock, and on new listings, as chosen. A watch MUST have a name of at most 120 characters, at least one alert enabled, a target between 0 and €100,000, and a spec with a model number, a word or a pinned listing.

#### Scenario: No alert chosen
- **WHEN** a watch has no target price and neither restock nor new-listing alerts
- **THEN** it is rejected as invalid

#### Scenario: Empty spec
- **WHEN** a spec has no model number, no word and no pinned listing
- **THEN** it is rejected as invalid

#### Scenario: Missing name
- **WHEN** a watch has a blank name
- **THEN** it is rejected as invalid

### Requirement: Size limits
The system SHALL reject a spec with more than 30 words or versions, more than 500 pinned or excluded listings, or a word longer than 60 bytes, and SHALL NOT allow more than 200 watches. The name limit of 120 SHALL be counted in characters.

#### Scenario: Long multibyte word
- **WHEN** a word of non-ASCII characters is over the limit
- **THEN** it is rejected with a message that quotes a shortened form of the word without cutting a character in half

#### Scenario: Name counted in characters
- **WHEN** a name has 120 characters that take more than 120 bytes
- **THEN** it is accepted

#### Scenario: Too many watches
- **WHEN** 200 watches exist and another is created
- **THEN** the request is rejected as invalid

### Requirement: First pass records a baseline
The first time an enabled watch is evaluated it SHALL record its state without alerting, except that a buyable listing already at or under the target SHALL be reported at once. Restock and new-listing alerts SHALL NOT fire on the first pass.

#### Scenario: Baseline only
- **WHEN** a new watch with restock and new-listing alerts is first evaluated and the kit is above target
- **THEN** nothing is sent

#### Scenario: Already under target
- **WHEN** a new watch is first evaluated and a buyable listing is already under the target
- **THEN** a target alert is sent

### Requirement: Target alerts do not repeat
A target alert SHALL fire once while the price stays at or under the target. It SHALL fire again only after the best price has gone back above the target or when it falls at least 3% below the price last alerted. A price that rises while still under the target SHALL become the new reference.

#### Scenario: Standing condition
- **WHEN** the best offer stays under the target on consecutive passes
- **THEN** only the first pass alerts

#### Scenario: Re-armed by leaving the target
- **WHEN** the price rises above the target and later drops under it again
- **THEN** a new alert is sent

#### Scenario: Further drop
- **WHEN** the price was last alerted at €100 and drops to €96 while still under the target
- **THEN** a new alert is sent

#### Scenario: Wobble
- **WHEN** the price moves from €100 to €99 while under the target
- **THEN** no alert is sent

### Requirement: Restock alerts
When enabled, a restock alert SHALL fire once on a later pass when nothing followed was buyable on the previous pass and something now is. If the restock also puts the price under the target, only the target alert SHALL fire.

#### Scenario: Back in stock
- **WHEN** the only followed listing goes from out of stock to in stock
- **THEN** one restock alert is sent

#### Scenario: Restock under target
- **WHEN** a restock lands at or under the target
- **THEN** the alert has the target reason only, not a second restock reason

### Requirement: New-listing alerts
When enabled, a new-listing alert SHALL fire for followed listings not seen on any earlier pass. A listing seen once SHALL never be announced as new again, even if it vanishes for a scrape and returns.

#### Scenario: New listing appears
- **WHEN** a shop adds a listing the watch follows
- **THEN** an alert names it

#### Scenario: Listing flaps
- **WHEN** a known listing disappears for one collect and comes back
- **THEN** it is not announced again

### Requirement: Editing restarts alert state
Changing a watch's spec, target price or enabled setting SHALL discard its alert state so the next pass records a fresh baseline. Changing only its name or its restock and new-listing options SHALL NOT. A disabled watch SHALL NOT be evaluated or alert.

#### Scenario: Target changed
- **WHEN** a person changes a watch's target price
- **THEN** the next pass starts from a new baseline

#### Scenario: Rename
- **WHEN** a person only renames a watch
- **THEN** its alert state is kept

#### Scenario: Disabled watch
- **WHEN** a watch is disabled
- **THEN** no alert is raised for it

### Requirement: Alerts are sent once and retried on failure
`report` SHALL evaluate all watches once, after the per-shop reports, and send one Telegram message listing every watch with news, showing at most five new listings per watch followed by how many more there are. It SHALL save alert state only after the message was sent. Running it again with nothing new SHALL send nothing.

#### Scenario: Send fails
- **WHEN** the Telegram send fails
- **THEN** state is not saved and the next `report` raises the same alerts again

#### Scenario: Second run
- **WHEN** `report` runs twice in a row with no new data
- **THEN** the second run sends nothing

#### Scenario: Many new listings
- **WHEN** a watch has seven new listings
- **THEN** five are named followed by "and 2 more"

### Requirement: Managing watches in the web interface
The web interface SHALL let a person preview what a spec follows, list watches with their current best offer, whether the target is hit and which listings are watched, and create, update and delete watches. Invalid watches SHALL be rejected as bad requests and unknown watches reported as not found.

#### Scenario: Preview needs input
- **WHEN** a preview request has neither a listing id nor a spec
- **THEN** it is rejected as a bad request

#### Scenario: Watched listings are marked
- **WHEN** watches are listed
- **THEN** the response includes the ids of listings followed by enabled watches

#### Scenario: Unknown watch
- **WHEN** a person updates or deletes a watch that does not exist
- **THEN** the request is reported as not found
