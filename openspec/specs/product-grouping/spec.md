# product-grouping Specification

## Purpose

Defines how listings of the same kit from different shops are grouped into products, how product identity stays stable between runs, and how a person overrides the grouping by hand with `link` and `unlink`.

## Requirements

### Requirement: Every active listing belongs to one product
The system SHALL assign each active listing to exactly one product, and a listing that matches nothing SHALL form a product of its own. Grouping SHALL read only the local database and SHALL NOT use the network.

#### Scenario: Same kit in three shops
- **WHEN** Gundam Store lists "MG RX-78-2 Gundam Ver. Ka 1/100", PlamoDX "MG – RX-78-2 Gundam Ver.Ka" and Zeonmarket "1/100 MG RX-78-2 Gundam Ver.Ka"
- **THEN** all three are in one product, matched by name

#### Scenario: Unmatched listing
- **WHEN** a listing resembles no other
- **THEN** it is the only listing of its own product

### Requirement: Matching barcodes join a product
The system SHALL put listings with the same barcode into one product, regardless of their names. A barcode SHALL be normalised to 13 digits before comparing: punctuation and spaces removed, 12 digits given a leading zero, a 14-digit code with a leading zero shortened. A code of any other length, or made of one repeated digit, SHALL be ignored.

#### Scenario: Different titles, same barcode
- **WHEN** two shops list entirely different titles with barcodes "4543112060426" and "4543-1120-60426"
- **THEN** they are in one product, matched by barcode

#### Scenario: Placeholder barcode
- **WHEN** two listings both carry the barcode "0000000000000"
- **THEN** the barcode is ignored and they are matched by name only

### Requirement: Names match on suit, grade and version
Without a shared barcode, two listings SHALL join one product only when their grades do not conflict, their version words are identical, and their names refer to the same kit by model number or by near-identical descriptive words. A listing with no grade SHALL NOT conflict with any grade.

#### Scenario: Different grades never match
- **WHEN** "HG RX-78-2 Gundam" has grade HG and "MG RX-78-2 Gundam" has grade MG
- **THEN** they are in separate products

#### Scenario: Different model numbers never match
- **WHEN** two names print different model numbers, such as MS-06 and MS-06FZ, or GP01 and GP01Fb
- **THEN** they are in separate products

#### Scenario: A model number tolerates a suffix
- **WHEN** one name prints "GAT-X105B" and the other "GAT-X105B/FP"
- **THEN** they are treated as the same model number

#### Scenario: Shops that print the model on only one side
- **WHEN** one shop prints "RG GF13-017NJ Shining Gundam" and another "RG Gundam Shining 1/144"
- **THEN** the descriptive words decide and they are in one product

#### Scenario: Too few words to judge
- **WHEN** neither name has a model number and one has fewer than two descriptive words
- **THEN** they are not matched automatically

### Requirement: Version words keep editions apart
Names that differ in version or edition words SHALL be separate products. These include Ver.Ka and other version numbers, Clear, Metallic, Titanium Finish, Gold, EX, Revive, Limited, Special, Premium, Extra, Color, Expo, Event, Coating, Custom, SD, MGSD, MGEX, Entry Grade, Trans-Am Mode and "*NEW MODEL*".

#### Scenario: Ver.Ka versus plain
- **WHEN** one listing is "MG RX-78-2 Gundam Ver.Ka" and another "MG RX-78-2 Gundam"
- **THEN** they are in separate products

#### Scenario: Version numbers differ
- **WHEN** listings are "MG RX-78-2 Gundam Ver. 2.0" and "MG RX-78-2 Gundam Ver. 3.0"
- **THEN** they are in separate products

#### Scenario: Premium Bandai releases
- **WHEN** one listing is "HG XXXG-01D2 Gundam Deathscythe Hell" and another is "PB – HG – XXXG-01D2 Gundam Deathscythe Hell"
- **THEN** they are in separate products

#### Scenario: MGEX versus MG
- **WHEN** one listing is "MG ZGMF-X20A Strike Freedom Gundam" and another "MGEX – ZGMF-X20A Strike Freedom Gundam"
- **THEN** they are in separate products

### Requirement: Shop spelling differences are ignored
The system SHALL ignore differences that only reflect how a shop writes a name: letter case, accents, scale ("1/100"), size ("18cm"), grade words and prefixes (HGUC, HGAW), series tags, shop catalogue numbers, the shop's own labels, and alternative spellings such as "Endless Waltz"/"EW", "P-Bandai"/"Premium Bandai"/"PB", "AGE II"/"AGE-2" and "Ver. Ka"/"Ver.Ka".

#### Scenario: Catalogue numbers and tags
- **WHEN** "Gundam HGUC 1/144 MSN-04II Nightingale Model Kit 240" is compared with "HG – MSN-04II Nightingale"
- **THEN** they are in one product

#### Scenario: Franchise tag
- **WHEN** "HG 1/144 MS-09 Challia's Rick Dom (GQ)" is compared with "Gundam HG 1/144 Challia's Rick Dom GQuuuuuuX Model Kit"
- **THEN** they are in one product

### Requirement: Real Grade series numbers
When two Real Grade listings both print the RG series number, whether as "RG35" or as "Model Kit 35" together with the word RG, the system SHALL treat them as the same kit if the numbers agree and as different kits if they differ, whatever the rest of the names say.

#### Scenario: Numbers agree
- **WHEN** "Gundam RG 1/144 God Gundam Model Kit 37" is compared with "1/144 RG GF13-017NJII Burning Gundam RG37"
- **THEN** they are in one product

#### Scenario: Numbers differ
- **WHEN** one name carries RG number 35 and the other RG number 20
- **THEN** they are in separate products

### Requirement: One shop never lists a kit twice
The system SHALL NOT put two listings from the same shop into one automatically grouped product, and a fuzzy match SHALL NOT join two groups that already share a shop.

#### Scenario: Duplicate listings in one shop
- **WHEN** a shop has two identically named listings and another shop has one
- **THEN** the other shop's listing joins at most one of them

### Requirement: Inactive listings do not take part
The system SHALL leave listings that their shop no longer lists out of matching. They SHALL keep their product but SHALL NOT block a live listing from the same shop or give the product its name, and SHALL take part again if the shop lists them again.

#### Scenario: Replacement of a removed listing
- **WHEN** a shop's old listing was removed and it now lists a new one for the same kit
- **THEN** the new listing can join the product that other shops' listings belong to

### Requirement: Product identity is stable
When regrouping, a product SHALL keep its id. A group SHALL take the lowest existing product id among its members that no earlier group has claimed, a group split from a product SHALL get new ids for the parts that do not keep it, and products left with no listings SHALL be removed. A product's name SHALL be its shortest member name and its grade the first non-empty member grade.

#### Scenario: Re-running changes nothing
- **WHEN** grouping runs twice with no new data
- **THEN** product ids and assignments are unchanged

#### Scenario: Two products merge
- **WHEN** a new listing joins two existing products
- **THEN** the merged group keeps the lower id and the other product is removed

#### Scenario: Name is the shortest
- **WHEN** a product has listings "Bandai Model Kit RX-78-2 Gundam" and "RX-78-2 Gundam"
- **THEN** the product is named "RX-78-2 Gundam"

### Requirement: Grouping is transactional
Regrouping, linking and unlinking SHALL each apply as one all-or-nothing change.

#### Scenario: Failure during regroup
- **WHEN** an error occurs partway through regrouping
- **THEN** no assignment from that attempt is kept

### Requirement: Manual link
`link <a> <b>` SHALL move listing b into listing a's product and pin both listings there as manual. Automatic grouping SHALL NOT move a manual listing, and SHALL NOT add other listings to a group that holds manual listings. The command SHALL fail with a message when a listing id does not exist, when listing a has no product yet (telling the person to run `match`), or when it is not given exactly two numeric ids.

#### Scenario: Forcing unrelated names together
- **WHEN** a person links two listings whose names are unrelated
- **THEN** they share a product and later regrouping keeps them together

#### Scenario: Link before any grouping
- **WHEN** listing a has never been assigned a product
- **THEN** the command fails and suggests running `match` first

#### Scenario: Unknown listing
- **WHEN** either id does not exist
- **THEN** the command fails with "not found" and changes nothing

#### Scenario: Wrong arguments
- **WHEN** `link` is given one id, or an id that is not a number
- **THEN** the command fails with an error saying how many ids it expected, or which id is invalid

### Requirement: Manual unlink
`unlink <id>` SHALL move the listing into a product of its own and pin it there as manual, so automatic grouping does not put it back. It SHALL fail when the id does not exist or when not given exactly one numeric id.

#### Scenario: Splitting a wrong match
- **WHEN** a person unlinks one listing of a three-listing product
- **THEN** it becomes its own product and stays apart after later regrouping, while the other two remain together

### Requirement: Manual choices are never overwritten
Regrouping SHALL NOT change any listing pinned as manual, whether by `link`, `unlink` or the near-miss review.

#### Scenario: Pinned apart despite identical names
- **WHEN** two listings with identical names were each pinned to separate products by hand
- **THEN** regrouping keeps them in separate products

### Requirement: Match command output
`match` SHALL regroup all listings and print the number of listings and products (and how many products have two or more shops), how many products were created and removed and listings reassigned, a count of listings per match method, and the near-miss pairs it did not link, best score first, at most 15, followed by how many more there are.

#### Scenario: Near misses listed
- **WHEN** `match` finds pairs that scored close but were not linked
- **THEN** it prints them with both listing ids, shops and names so they can be confirmed with `link`

#### Scenario: More than fifteen near misses
- **WHEN** there are 20 near-miss pairs
- **THEN** 15 are printed followed by "and 5 more"
