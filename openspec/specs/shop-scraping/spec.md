# shop-scraping Specification

## Purpose

Defines how the collector reads each supported shop's catalog and what it records for every listing: which products count as kits, their grade, price, currency, stock status and barcode. It covers what a shop fetch returns, not how it is stored.

## Requirements

### Requirement: Supported shops and tracked grades
The system SHALL read the catalogs of GeeksHeaven, Gundam Store, PlamoDX and Zeonmarket, and SHALL track only Master Grade (MG), High Grade (HG), Real Grade (RG) and Perfect Grade (PG) listings. Each listing SHALL carry the grade of the shop category it was found in.

#### Scenario: Listing takes its category's grade
- **WHEN** a shop lists a product in its Real Grade category
- **THEN** the scraped listing has grade `RG`

#### Scenario: Other product lines are not read
- **WHEN** a shop also has categories for SD, 30MM, merchandise or accessories
- **THEN** no listings are read from those categories

#### Scenario: PlamoDX files MGEX kits separately
- **WHEN** PlamoDX lists a kit in its `mgex` category
- **THEN** the listing has grade `MG`, the same as the other shops use for MGEX

#### Scenario: PlamoDX decal sheets are not read
- **WHEN** PlamoDX has decal products under categories named like the grade categories
- **THEN** those decal products are not read, because only the categories under its Gunpla branch are used

### Requirement: Non-kit listings are excluded
The system SHALL drop listings that are not Gunpla model kits even when a shop files them under a grade category: figures, display bases, decals, effect or parts-only add-ons, expansion sets, weapon add-on sets, 30MM, SD/MGSD, Metal Build and card-game products. The filter SHALL be conservative: a real kit with a similar word in its name is kept.

#### Scenario: Figure-Rise is dropped
- **WHEN** a grade category contains "Figure-rise Standard Son Goku"
- **THEN** that listing is not stored

#### Scenario: Kit names containing similar words are kept
- **WHEN** a listing is named "Type89 Base Jabber", "Perfect Pack Equipped" or "Twin Set"
- **THEN** it is kept as a kit

#### Scenario: Expansion bundled with a kit is kept
- **WHEN** a listing is named "Perfectibility + Divine Expansion Set"
- **THEN** it is kept, while a listing named only "Expansion Set" is dropped

#### Scenario: Weapon add-on versus kit with weapons
- **WHEN** listings are named "Weapon Set 2" and "OZ-06MS Leo (Full Weapon Set)"
- **THEN** the first is dropped and the second is kept

### Requirement: Listing fields
The system SHALL record for each kit listing a shop-stable external id, its URL, name, grade, price in the shop's own currency, that currency, and availability. It SHALL record the barcode (EAN) and SKU where the shop exposes them.

#### Scenario: GeeksHeaven price includes VAT
- **WHEN** GeeksHeaven reports a product price
- **THEN** the recorded price is the VAT-inclusive price in EUR

#### Scenario: Gundam Store prices are in US dollars
- **WHEN** a Gundam Store listing is scraped
- **THEN** its currency is `USD`, while GeeksHeaven, PlamoDX and Zeonmarket listings are `EUR`

#### Scenario: Barcodes per shop
- **WHEN** listings are scraped from GeeksHeaven
- **THEN** each carries the barcode the shop reports, whereas Gundam Store collection listings, PlamoDX and Zeonmarket carry none from the listing itself

#### Scenario: Zeonmarket id is stable across stock changes
- **WHEN** a Zeonmarket product goes out of stock and loses its add-to-cart button
- **THEN** its external id, taken from its URL path, is unchanged

#### Scenario: HTML entities in names are decoded
- **WHEN** PlamoDX or Zeonmarket returns a name containing HTML entities
- **THEN** the recorded name has them decoded

### Requirement: Availability states
The system SHALL record each listing's availability as in stock, out of stock, pre-order, or unknown when the shop does not say.

#### Scenario: Plain stock flag
- **WHEN** a shop reports a product as available
- **THEN** its availability is in stock, and out of stock if it reports it as unavailable

#### Scenario: Zeonmarket limited stock is buyable
- **WHEN** Zeonmarket marks a card "Beperkt op voorraad" (limited stock)
- **THEN** its availability is in stock

#### Scenario: Zeonmarket sold out
- **WHEN** Zeonmarket marks a card "Niet op voorraad"
- **THEN** its availability is out of stock

#### Scenario: Unrecognised stock markup
- **WHEN** a Zeonmarket card has no stock indicator the system recognises
- **THEN** its availability is unknown

### Requirement: Zeonmarket pre-orders
The system SHALL read Zeonmarket's pre-order list in addition to its grade categories and mark each entry as a pre-order. The leading "PRE-ORDER" marker SHALL be removed from the name, and the grade SHALL be inferred from the remaining name or left empty if it cannot be.

#### Scenario: Pre-order title is cleaned
- **WHEN** Zeonmarket lists "PRE-ORDER MG 1/100 Gundam Barbatos"
- **THEN** the recorded name is "MG 1/100 Gundam Barbatos", availability is pre-order, and the grade is `MG`

#### Scenario: Pre-order with no recognisable grade
- **WHEN** a pre-order name contains no grade
- **THEN** the grade is empty

#### Scenario: Pre-order status wins over a grade category
- **WHEN** the same product appears in both a grade category and the pre-order list
- **THEN** it is recorded once, as a pre-order

### Requirement: Listings are de-duplicated within a shop
The system SHALL return each product of a shop at most once per fetch, even when the shop lists it in more than one category.

#### Scenario: Product in two categories
- **WHEN** a product appears in two of a shop's grade categories
- **THEN** one listing is returned, with the grade of the last category that listed it

### Requirement: Complete paged reads
The system SHALL read every page of each category it tracks, until the shop reports no further pages or returns a short or empty one. A fetch SHALL return all listings or fail; it SHALL NOT return a partial catalog when a page cannot be read.

#### Scenario: Multiple pages
- **WHEN** a category spans several pages
- **THEN** listings from all pages are returned

#### Scenario: A page fails
- **WHEN** one page of a shop cannot be fetched after retries
- **THEN** the whole fetch fails with an error naming the category and page

#### Scenario: Runaway pagination on Zeonmarket
- **WHEN** a Zeonmarket category is still returning products after 100 pages
- **THEN** the fetch fails with an error saying pagination may have changed

### Requirement: Layout changes fail loudly
The system SHALL fail a shop's fetch with an error when the structure it expects is missing, instead of returning an empty or partial catalog.

#### Scenario: GeeksHeaven has no grade categories
- **WHEN** GeeksHeaven's category index contains no MG, HG, RG or PG categories
- **THEN** the fetch fails with an error saying the site structure may have changed

#### Scenario: Shop returns nothing at all
- **WHEN** Gundam Store or PlamoDX returns no products across all tracked categories
- **THEN** the fetch fails with an error saying the site structure may have changed

#### Scenario: Unrecognised Zeonmarket card
- **WHEN** a Zeonmarket product card lacks a recognisable title or price
- **THEN** the fetch fails with an error saying the site structure may have changed

#### Scenario: Unparseable price
- **WHEN** Gundam Store, PlamoDX or Zeonmarket returns a price that cannot be parsed or is negative
- **THEN** the fetch fails rather than recording a wrong price

#### Scenario: Unexpected currency precision
- **WHEN** PlamoDX reports a price whose currency minor unit is not 2
- **THEN** the fetch fails rather than misreading the amount

### Requirement: Polite and resilient fetching
The system SHALL pause between consecutive requests to a shop, honoring a shop's published crawl delay, cap each response body at 8 MiB, and retry transient failures with doubling backoff, three times at most. A 404, an oversized body or another non-transient failure SHALL fail immediately. Waits SHALL end early when the run is cancelled.

#### Scenario: Transient failure is retried
- **WHEN** a request fails with a dropped connection, a timeout, a 429 or a 5xx response
- **THEN** it is retried after a wait that doubles each time, up to three retries

#### Scenario: Non-transient failure is not retried
- **WHEN** a request returns 404
- **THEN** it fails at once without retry

#### Scenario: Oversized response
- **WHEN** a response body exceeds 8 MiB
- **THEN** the request fails with a limit error

#### Scenario: Cancellation interrupts waiting
- **WHEN** the run is cancelled during a delay or retry wait
- **THEN** the fetch stops promptly with the cancellation error

### Requirement: Shop barcode lookup
For a shop whose listing omits barcodes but whose per-product page has them, the system SHALL be able to look a barcode up for a single listing, returning an empty result when the shop has none.

#### Scenario: Gundam Store product with a barcode
- **WHEN** a barcode lookup is made for a Gundam Store listing whose product page has a barcode
- **THEN** that barcode is returned, trimmed of whitespace

#### Scenario: Store exclusive without a barcode
- **WHEN** the product page has no barcode or no variants
- **THEN** an empty barcode is returned and no error is raised
