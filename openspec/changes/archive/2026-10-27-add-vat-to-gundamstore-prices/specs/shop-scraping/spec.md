# Spec Delta

## REMOVED Requirements

### Requirement: Listing fields
**Reason**: Replaced by "Listing fields are recorded": Gundam Store prices are now in Danish kroner instead of US dollars; the other fields are unchanged.
**Migration**: None; see the replacement requirement.

## ADDED Requirements

### Requirement: Listing fields are recorded
The system SHALL record for each kit listing a shop-stable external id, its URL, name, grade, price in the shop's own currency, that currency, and availability. It SHALL record the barcode (EAN) and SKU where the shop exposes them.

#### Scenario: GeeksHeaven price includes VAT
- **WHEN** GeeksHeaven reports a product price
- **THEN** the recorded price is the VAT-inclusive price in EUR

#### Scenario: Gundam Store prices are in Danish kroner
- **WHEN** a Gundam Store listing is scraped
- **THEN** its currency is `DKK`, while GeeksHeaven, PlamoDX and Zeonmarket listings are `EUR`

#### Scenario: Barcodes per shop
- **WHEN** listings are scraped from GeeksHeaven
- **THEN** each carries the barcode the shop reports, whereas Gundam Store collection listings, PlamoDX and Zeonmarket carry none from the listing itself

#### Scenario: Zeonmarket id is stable across stock changes
- **WHEN** a Zeonmarket product goes out of stock and loses its add-to-cart button
- **THEN** its external id, taken from its URL path, is unchanged

#### Scenario: HTML entities in names are decoded
- **WHEN** PlamoDX or Zeonmarket returns a name containing HTML entities
- **THEN** the recorded name has them decoded

### Requirement: Gundam Store is priced for the Netherlands
The system SHALL read Gundam Store listings, and look up their barcodes, as a customer in the Netherlands, so the recorded price is the one a Dutch buyer sees, with Dutch VAT included. It SHALL NOT add or remove VAT itself.

#### Scenario: Price matches the store's Dutch page
- **WHEN** the store's Dutch price for a kit is 696.94 DKK
- **THEN** the recorded price is 69694 DKK cents

#### Scenario: Barcode lookup uses the same market
- **WHEN** a barcode lookup is made for a Gundam Store listing
- **THEN** it reads the listing in the Netherlands market, like the catalog read

### Requirement: A shop's Retry-After is honored
When a shop answers a request with a 429 or a 5xx response that says how long to wait, given in seconds or as a date, the system SHALL wait at least that long before retrying, up to a limit of 2 minutes. The retry limit of the fetching requirement SHALL still apply.

#### Scenario: Shop asks for a wait
- **WHEN** a shop answers 429 with a Retry-After of 1 second and the retry wait would be shorter
- **THEN** the retry is made no earlier than 1 second after the answer

#### Scenario: Unreasonable wait is capped
- **WHEN** a shop answers 429 with a Retry-After of one day
- **THEN** the system waits only the 2-minute limit before retrying

#### Scenario: Missing or invalid header
- **WHEN** the Retry-After header is absent, not a number or date, zero, or a date in the past
- **THEN** the usual doubling retry wait applies
