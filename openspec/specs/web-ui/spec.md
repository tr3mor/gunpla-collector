# web-ui Specification

## Purpose

Defines the web interface served by `serve`: searching the current catalog, comparing one kit across shops, managing watches and near-miss review, and the protection that stops other websites or untrusted clients from changing data.

## Requirements

### Requirement: Single page with three views
`serve` SHALL serve one page at `/` with three views: Search, Watches and Near misses. The selected view SHALL be reflected in the address fragment so it survives a reload, and the page SHALL work without any external service.

#### Scenario: Open the page
- **WHEN** a browser requests `/`
- **THEN** it receives the HTML page

#### Scenario: Reload keeps the view
- **WHEN** a person is on the Near misses view and reloads
- **THEN** the Near misses view is shown again

### Requirement: Search returns active listings
Search SHALL return only listings their shop still lists, each with its shop, name, grade, URL, current price, lowest recorded price, availability and the time it was last collected, ordered by name and then shop. Results SHALL be filterable by a case-insensitive text in the name, by a case-insensitive regular expression on the name, by shop and by grade. Leading and trailing spaces in filters SHALL be ignored.

#### Scenario: Name filter
- **WHEN** the text filter is " gundam "
- **THEN** only listings whose name contains "gundam" in any case are returned

#### Scenario: Grade filter
- **WHEN** the grade filter is "hg"
- **THEN** only HG listings are returned, and the value "none" returns only listings with no grade

#### Scenario: Regular expression
- **WHEN** the regular expression is "^RX"
- **THEN** only names that start with RX in any case are returned

#### Scenario: Invalid regular expression
- **WHEN** the regular expression does not compile
- **THEN** the request is answered as a bad request with the error message, and other failures are answered as server errors

#### Scenario: Removed listing
- **WHEN** a shop no longer lists a set
- **THEN** search does not return it

### Requirement: Out-of-stock listings are hidden by default
Search and product results SHALL omit listings the shop reports as out of stock unless the request asks for them with `show_out_of_stock` set to `1` or `true`. Listings of unknown stock and pre-orders SHALL be included.

#### Scenario: Default
- **WHEN** no stock option is given
- **THEN** out-of-stock listings are omitted

#### Scenario: Show them
- **WHEN** `show_out_of_stock=1` or `TRUE` is given
- **THEN** out-of-stock listings are included

### Requirement: Prices are shown in EUR
Search and product results SHALL show current and lowest recorded prices in EUR when the listing has a EUR value, together with the shop's own price and currency when that differs. A listing without a EUR value SHALL be shown in its own currency.

#### Scenario: Gundam Store listing
- **WHEN** a USD listing has a stored EUR value
- **THEN** its prices are returned in EUR and the original USD price is returned beside it

#### Scenario: No rate
- **WHEN** a listing's currency had no rate
- **THEN** its prices are returned in its own currency with no original price

### Requirement: Products compare one kit across shops
The products view SHALL group the listings of one kit, returning a kit when any of its listings passes the filters together with every listing that does. Kits sold by more shops SHALL come first, then by name, and each kit's listings SHALL be sorted cheapest first by EUR price with buyable listings before listings of unknown price and out-of-stock listings last. A listing not yet grouped SHALL appear as a kit of its own. Each listing SHALL say how it was matched.

#### Scenario: Cheapest offer flagged
- **WHEN** two shops list a kit in stock at €40 and €45
- **THEN** the €40 listing is flagged cheapest

#### Scenario: Tie
- **WHEN** two shops list a kit in stock at the same EUR price
- **THEN** both are flagged cheapest

#### Scenario: One shop only
- **WHEN** a kit is listed by one shop
- **THEN** its listing is not flagged cheapest

#### Scenario: Out of stock cannot win
- **WHEN** the lowest price belongs to an out-of-stock listing
- **THEN** it is not flagged cheapest, and the cheapest buyable listing is

#### Scenario: Listing without a EUR value
- **WHEN** a listing has no EUR value
- **THEN** it can never be flagged cheapest

#### Scenario: Empty result
- **WHEN** nothing matches
- **THEN** an empty list is returned, not null

### Requirement: Search page behavior
The search page SHALL offer text and regular-expression filters, a shop filter, a grade filter with an option for listings with no grade, a "Hide out of stock" option and a "Group same kit across shops" option, both on by default. Each result SHALL show the current price with a marker saying whether it is the lowest recorded price or how far above it, and a way to start a watch.

#### Scenario: At its lowest
- **WHEN** a listing's current price equals its lowest recorded price
- **THEN** its marker says it is at its lowest price so far

#### Scenario: Above its lowest
- **WHEN** a listing's price is 12% above its lowest recorded price
- **THEN** its marker says so and shows that lowest price

#### Scenario: Ungrouped list
- **WHEN** a person turns off "Group same kit across shops"
- **THEN** results are shown as a flat list of listings

### Requirement: Theme choice
The page SHALL follow the system's light or dark setting by default and SHALL let the person choose System, Light or Dark. The choice SHALL be remembered in the browser when it can be stored, and the page SHALL work when it cannot.

#### Scenario: Explicit choice remembered
- **WHEN** a person picks Dark and reloads
- **THEN** the page is dark

#### Scenario: Storage unavailable
- **WHEN** the browser blocks local storage
- **THEN** the page follows the system setting and still works

### Requirement: Shops list
The interface SHALL list the active shops so results can be filtered by shop.

#### Scenario: Shops endpoint
- **WHEN** the shop list is requested
- **THEN** each active shop is returned with its slug and name

### Requirement: Reading is open, changing data is guarded
Reading SHALL NOT require authentication. Requests that change data (saving or deleting watches, and linking, dismissing or restoring near-miss pairs) SHALL be accepted only when they pass the write guard. Previewing a watch changes nothing and SHALL NOT need the guard. A request body SHALL be limited to 1 MiB.

#### Scenario: Read without a token
- **WHEN** `GUNPLA_UI_TOKEN` is set and a client reads search results without it
- **THEN** the read succeeds

#### Scenario: Oversized body
- **WHEN** a write body exceeds 1 MiB
- **THEN** it is rejected as a bad request

### Requirement: Write token
When `GUNPLA_UI_TOKEN` is set, a change SHALL require the header `Authorization: Bearer <token>` with the exact token, compared in constant time. A missing, wrong or non-Bearer credential SHALL be refused as unauthorized with a `WWW-Authenticate` challenge. The page SHALL be able to learn whether a token is required, and SHALL ask for it once and remember it in the browser.

#### Scenario: No token supplied
- **WHEN** a token is configured and a change arrives without credentials
- **THEN** it is answered 401 with a `WWW-Authenticate: Bearer` header and nothing is saved

#### Scenario: Authentication is checked first
- **WHEN** a token is configured and a change arrives with no credentials and a non-JSON body
- **THEN** it is answered 401, not 403

#### Scenario: Token without the Bearer prefix
- **WHEN** the Authorization header holds the token without "Bearer "
- **THEN** it is refused as unauthorized

#### Scenario: Correct token
- **WHEN** the header is `Bearer <token>`
- **THEN** the change is accepted

#### Scenario: Configuration endpoint
- **WHEN** a token is configured
- **THEN** the configuration endpoint reports that a write token is required

### Requirement: Cross-site protection without a token
A change SHALL be refused with 403 when its body is not JSON (except for delete requests, which carry none), when the browser marks it `Sec-Fetch-Site: cross-site`, or, when no token is configured, when its `Origin` is not the server's own host. Same-origin pages and clients that send no Origin SHALL be accepted.

#### Scenario: Another website's form
- **WHEN** a form on another site posts with content type `text/plain`
- **THEN** it is refused and nothing is saved

#### Scenario: Foreign origin
- **WHEN** no token is set and the Origin is `https://evil.example`
- **THEN** it is refused

#### Scenario: Cross-site marked request with a valid token
- **WHEN** the request has a valid token but `Sec-Fetch-Site: cross-site`
- **THEN** it is refused

#### Scenario: Same-origin page
- **WHEN** the Origin matches the server's host
- **THEN** the change is accepted

### Requirement: Reach without protection is the operator's choice
Without a token, the guard SHALL protect against other websites only; it does not restrict clients that can reach the port. The interface SHALL therefore be documented as for trusted networks unless a token is set.

#### Scenario: Plain client on the network
- **WHEN** no token is set and a non-browser client sends a JSON change with no Origin
- **THEN** the change is accepted

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
