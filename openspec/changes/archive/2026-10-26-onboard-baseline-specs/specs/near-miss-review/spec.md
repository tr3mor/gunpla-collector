# Spec Delta

## Purpose

Lets a person correct the grouping of kits across shops by listing pairs of listings that were not grouped but might be the same kit, with the reason, and by offering to link them or dismiss them for good.

## ADDED Requirements

### Requirement: Near-miss pairs are listed
The system SHALL list pairs of active listings from different shops that were not placed in one product but might be the same kit. Reading the list SHALL NOT change any product. Each pair SHALL carry its kind, a similarity score, the reason it was not linked, and both listings with their price, stock and URL.

#### Scenario: Read-only
- **WHEN** the near-miss list is requested
- **THEN** no listing is regrouped, linked or changed

#### Scenario: Inactive listings are left out
- **WHEN** a listing is no longer on its shop's site
- **THEN** pairs involving it are not listed

### Requirement: Pairs that are excluded
The system SHALL NOT list a pair whose listings come from the same shop, are already in the same product, involve a listing pinned by hand, carry different barcodes, or have conflicting grades.

#### Scenario: Different barcodes
- **WHEN** two listings carry different valid barcodes
- **THEN** they are known to be different kits and are not listed

#### Scenario: Already grouped
- **WHEN** both listings are in one product
- **THEN** the pair is not listed

#### Scenario: Grade conflict
- **WHEN** one listing is HG and the other MG
- **THEN** the pair is not listed

### Requirement: Close and version kinds
A pair SHALL be of kind "close" when its version words are the same but the names were not similar enough to link automatically. A pair SHALL be of kind "version" when the names describe the same suit but one carries version words the other lacks, such as Premium, Revive or Clear. Close pairs SHALL be listed before version pairs, and within a kind the higher score first.

#### Scenario: Close pair
- **WHEN** one listing is "MG – RX-0 Unicorn Gundam" and another is "MG – RX-0 Full Armor Unicorn Gundam"
- **THEN** the pair is listed as close, because the model number agrees but the extra words are too many to link automatically

#### Scenario: Extra version word
- **WHEN** one listing is "HG GN-001 Gundam Exia" and another is "HG GN-001 Gundam Exia (Trans-Am Mode)"
- **THEN** the pair is listed as a version difference, and is never linked automatically

#### Scenario: Ordering
- **WHEN** both kinds are present
- **THEN** close pairs come first, each kind ordered by score descending

### Requirement: Reason and word differences
Each pair SHALL state in words why the matcher did not link it, for example differing words, a model-number clash, or a conflicting RG number. It SHALL show the words that appear in only one name, in only the other, and in both, and how the matcher read each name: model number, version words and descriptive words.

#### Scenario: Blocked by an existing group member
- **WHEN** two names are similar enough to link but a listing from one of the two shops is already in the other's product
- **THEN** the reason says the pair was not linked because it is already grouped with that listing, and names it

#### Scenario: Differing words highlighted
- **WHEN** a pair is shown
- **THEN** the words unique to each name are available separately from the shared words

### Requirement: Dismiss and restore
A person SHALL be able to dismiss a pair so it no longer appears, and later restore it. A dismissal SHALL persist across regrouping and new collects, SHALL not depend on the order of the two listings, and dismissing the same pair twice SHALL be harmless. Dismissed pairs SHALL be shown only when requested.

#### Scenario: Dismissed pair hidden
- **WHEN** a pair is dismissed
- **THEN** it no longer appears in the default list

#### Scenario: Show dismissed
- **WHEN** the list is requested with dismissed pairs included
- **THEN** the pair appears marked as dismissed and can be restored

#### Scenario: Order does not matter
- **WHEN** a pair was dismissed as (5, 9) and is later looked up as (9, 5)
- **THEN** it is recognised as dismissed

#### Scenario: Invalid ids
- **WHEN** a dismiss or restore names the same id twice, or an id that is not positive
- **THEN** the request is rejected as bad and nothing changes

### Requirement: Link from review
A person SHALL be able to link a listed pair from the review, with the same effect as the `link` command: the two listings share a product and are pinned there.

#### Scenario: Confirming a match
- **WHEN** a person links a close pair
- **THEN** the listings share a product, later regrouping keeps them together, and the pair no longer appears

### Requirement: Review filters and limits
The list SHALL be filterable by kind, by text in either name, by shop and grade of either side, and by whether dismissed pairs are included. Pairs with an out-of-stock side SHALL be hidden unless requested. The response SHALL report the total matching pairs, counts per kind and counts per grade, and SHALL return at most 200 pairs by default and never more than 1000.

#### Scenario: Default hides out-of-stock pairs
- **WHEN** one listing of a pair is out of stock and no option is given
- **THEN** the pair is not returned

#### Scenario: Grade of either side
- **WHEN** the grade filter is MG and a pair has an MG listing and a listing with no grade
- **THEN** the pair is returned

#### Scenario: Limit applies to items, not totals
- **WHEN** there are 250 matching pairs and the default limit applies
- **THEN** 200 pairs are returned and the total reports 250

### Requirement: Changes need the write guard
Linking, dismissing and restoring SHALL be subject to the same write protection as other changes made through the web interface.

#### Scenario: Token required
- **WHEN** `GUNPLA_UI_TOKEN` is set and a dismiss arrives without the token
- **THEN** it is refused as unauthorized
