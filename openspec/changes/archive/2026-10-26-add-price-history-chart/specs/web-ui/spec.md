# Spec Delta

## ADDED Requirements

### Requirement: Results open a price history
A person SHALL be able to open the price history of a kit from the Search view, from a grouped kit or from a listing in the ungrouped list. A listing that belongs to a product SHALL open the history of that whole product, not only of itself, and a listing with no product SHALL open its own. Following a shop link or starting a watch from a result SHALL NOT open the history.

#### Scenario: Grouped kit
- **WHEN** a person selects a kit in the grouped results
- **THEN** the price history of that kit is shown with a line for each of its listings

#### Scenario: Listing in the ungrouped list
- **WHEN** a person selects a listing that belongs to a product while "Group same kit across shops" is off
- **THEN** the history of the whole product is shown, including the other shops

#### Scenario: Listing with no product
- **WHEN** a person selects a listing that is not grouped
- **THEN** the history of that listing alone is shown

#### Scenario: Shop link
- **WHEN** a person follows a result's shop link or its watch button
- **THEN** the shop opens or the watch starts, and no history is shown

#### Scenario: Close
- **WHEN** a person closes the history
- **THEN** the Search view is shown as it was, with its filters and results

### Requirement: History chart lines
The history SHALL be drawn as a chart of price in EUR over time with one line per listing. A line SHALL be solid where the kit was buyable, dashed where the shop reported it out of stock, and absent where the shop did not list it.

#### Scenario: Three states on one line
- **WHEN** a listing was buyable, then out of stock, then not listed, then buyable again
- **THEN** its line is solid, then dashed, then broken, then solid

#### Scenario: Shop dropped the kit
- **WHEN** a shop no longer lists the kit
- **THEN** its line ends where it stopped being listed

#### Scenario: Lone out-of-stock point
- **WHEN** a listing was out of stock in exactly one run between buyable runs
- **THEN** that run is still visible on the chart

### Requirement: History chart labels and details
Each line SHALL be labeled with its shop, and with the listing's name too when one shop has more than one line. A line of a listing the shop no longer lists SHALL be marked as such. Pointing at the chart SHALL mark the time of the nearest run and list, for every shown line, its EUR price and availability at that time, or that it was not listed, with the shop's own price and currency when that differs.

#### Scenario: Two listings in one shop
- **WHEN** one shop has two listings of the kit
- **THEN** the chart has two lines for that shop, labeled with their names

#### Scenario: All lines at one time
- **WHEN** a person points at the chart on a day three shops listed the kit
- **THEN** that day is marked and the three prices are listed together, cheapest first

#### Scenario: Price in another currency
- **WHEN** the pointed time holds a price from a USD listing
- **THEN** the list shows the EUR price and the original USD price

#### Scenario: Not listed at that time
- **WHEN** a shop did not list the kit at the pointed time
- **THEN** its entry says it was not listed and shows no price

#### Scenario: Listing no longer listed
- **WHEN** a shop no longer lists the kit
- **THEN** its label says the shop no longer lists it

### Requirement: History legend figures
Below the chart, each line SHALL have a legend entry showing its latest, lowest and highest EUR price in the shown time range. Selecting an entry SHALL hide or show its line, and the price scale SHALL follow the lines shown. Pointing at an entry SHALL emphasize its line.

#### Scenario: Figures
- **WHEN** a listing was at 40.00, 38.00 and 42.00 EUR in the range
- **THEN** its entry shows latest 42.00 if that was the latest run, lowest 38.00 and highest 42.00

#### Scenario: Hide a line
- **WHEN** a person selects a legend entry
- **THEN** its line disappears, the entry is shown as hidden, and selecting it again brings the line back

#### Scenario: No price in range
- **WHEN** a listing has no price in the shown range
- **THEN** its entry shows no figures

### Requirement: History time range
The chart SHALL let a person choose to show the last 7, 30 or 90 days or all of the history, counted back from the most recent run, with all of it shown first. The legend figures SHALL follow the range, and a price held from before the range SHALL continue into it.

#### Scenario: Narrow the range
- **WHEN** a person chooses 30 days on a history that spans a year
- **THEN** only the last 30 days are drawn and the legend figures cover only them

#### Scenario: Price held into the range
- **WHEN** a listing last changed price 60 days ago and 30 days is chosen
- **THEN** its line is drawn across the whole range at that price

#### Scenario: Short history
- **WHEN** the whole history is shorter than the chosen range
- **THEN** all of it is shown

### Requirement: History with nothing to draw
When the history holds no points, or cannot be loaded, the page SHALL say so instead of drawing an empty chart, and the person SHALL still be able to close the history.

#### Scenario: No priced points
- **WHEN** the history holds no points
- **THEN** the person is told there is no price history for the kit and no empty chart is drawn

#### Scenario: Failed request
- **WHEN** the history cannot be loaded
- **THEN** the person is shown an error message and can close the history

### Requirement: The chart needs no external service
The chart SHALL be drawn by the page itself and SHALL follow the chosen theme, so it works offline and in both light and dark.

#### Scenario: Offline
- **WHEN** the page is served on a network with no internet access
- **THEN** the chart is drawn

#### Scenario: Dark theme
- **WHEN** the theme is dark
- **THEN** the chart, its lines and its labels are legible on the dark background
